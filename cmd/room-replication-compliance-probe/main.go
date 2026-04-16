package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/formats"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/keys"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/message/legacy"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/muxrpc"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/secretstream"
)

const defaultSHSCap = "1KHLiKZvAvjbY1ziZEHMXawbCEIM6qwjCDm3VYRan/s="

var unsupportedAdvertisedMethods = map[string]string{
	"invite.create": "not supported on the scoped Room+Replication surface",
	"metafeeds":     "out of scope",
	"indexFeeds":    "out of scope",
	"bipfHistory":   "out of scope",
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		emit(os.Stdout, probeEvent{Method: "probe", OK: false, Failure: err.Error()})
		fmt.Fprintf(os.Stderr, "room-replication-compliance-probe: %v\n", err)
		os.Exit(1)
	}
}

type config struct {
	RoomAddr   string
	RoomFeed   string
	KeyFile    string
	TargetFeed string
	ExpectFeed string
	SHSCap     string
	Timeout    time.Duration
	MinHistory int
	Limit      int
}

func run(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("room-replication-compliance-probe", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var cfg config
	fs.StringVar(&cfg.RoomAddr, "room-addr", "", "room muxrpc tcp address (host:port)")
	fs.StringVar(&cfg.RoomFeed, "room-feed", "", "room feed ref (@...ed25519)")
	fs.StringVar(&cfg.KeyFile, "key-file", "", "path to probe secret key file")
	fs.StringVar(&cfg.TargetFeed, "target-feed", "", "optional announced bridge bot feed ref")
	fs.StringVar(&cfg.ExpectFeed, "expect-feed", "", "expected history author feed ref (defaults to selected target)")
	fs.StringVar(&cfg.SHSCap, "shs-cap", defaultSHSCap, "secret-handshake app key (base64)")
	fs.DurationVar(&cfg.Timeout, "timeout", 30*time.Second, "probe timeout")
	fs.IntVar(&cfg.MinHistory, "min-history", 1, "minimum valid tunneled history messages required")
	fs.IntVar(&cfg.Limit, "limit", 10, "createHistoryStream limit")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := cfg.validate(); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
	defer cancel()

	keyPair, err := ensureKeyPair(cfg.KeyFile)
	if err != nil {
		return fmt.Errorf("ensure keypair: %w", err)
	}
	roomFeed, err := refs.ParseFeedRef(cfg.RoomFeed)
	if err != nil {
		return fmt.Errorf("parse --room-feed: %w", err)
	}
	conn, err := openRoomEndpoint(ctx, keyPair, *roomFeed, cfg.RoomAddr, cfg.SHSCap)
	if err != nil {
		return err
	}
	defer conn.Close()

	methods, err := verifyManifest(ctx, out, conn.Endpoint)
	if err != nil {
		return err
	}
	if err := verifyWhoami(ctx, out, conn.Endpoint); err != nil {
		return err
	}
	if err := verifyMetadata(ctx, out, conn.Endpoint); err != nil {
		return err
	}
	attendants, err := collectAttendants(ctx, out, conn.Endpoint)
	if err != nil {
		return err
	}
	endpoints, err := collectEndpoints(ctx, out, conn.Endpoint)
	if err != nil {
		return err
	}
	if containsString(methods, "ebt.replicate") {
		if err := probeEBT(ctx, out, conn.Endpoint); err != nil {
			return err
		}
	} else {
		emit(out, probeEvent{Method: "ebt.replicate", OK: true, Count: 0})
	}

	target, err := selectTarget(cfg.TargetFeed, keyPair.FeedRef().String(), attendants, endpoints)
	if err != nil {
		return err
	}
	expectFeed := strings.TrimSpace(cfg.ExpectFeed)
	if expectFeed == "" {
		expectFeed = target.String()
	}
	if err := probeTunnelHistory(ctx, out, conn, keyPair, *roomFeed, target, expectFeed, cfg); err != nil {
		return err
	}
	emit(out, probeEvent{Method: "probe", OK: true, Peer: target.String()})
	return nil
}

func (c config) validate() error {
	if strings.TrimSpace(c.RoomAddr) == "" {
		return fmt.Errorf("--room-addr is required")
	}
	if strings.TrimSpace(c.RoomFeed) == "" {
		return fmt.Errorf("--room-feed is required")
	}
	if strings.TrimSpace(c.KeyFile) == "" {
		return fmt.Errorf("--key-file is required")
	}
	if c.Timeout <= 0 {
		return fmt.Errorf("--timeout must be > 0")
	}
	if c.MinHistory <= 0 {
		return fmt.Errorf("--min-history must be > 0")
	}
	if c.Limit <= 0 {
		return fmt.Errorf("--limit must be > 0")
	}
	return nil
}

type probeEvent struct {
	Method         string   `json:"method"`
	OK             bool     `json:"ok"`
	Peer           string   `json:"peer,omitempty"`
	Feed           string   `json:"feed,omitempty"`
	Sequence       int64    `json:"sequence,omitempty"`
	MessageRef     string   `json:"message_ref,omitempty"`
	RawSHA256      string   `json:"raw_sha256,omitempty"`
	SignatureValid bool     `json:"signature_valid,omitempty"`
	Failure        string   `json:"failure,omitempty"`
	Count          int      `json:"count,omitempty"`
	IDs            []string `json:"ids,omitempty"`
}

func emit(out io.Writer, ev probeEvent) {
	_ = json.NewEncoder(out).Encode(ev)
}

func verifyManifest(ctx context.Context, out io.Writer, endpoint muxrpc.Endpoint) ([]string, error) {
	var manifest map[string]interface{}
	if err := endpoint.Sync(ctx, &manifest, muxrpc.TypeJSON, muxrpc.Method{"manifest"}); err != nil {
		emit(out, probeEvent{Method: "manifest", OK: false, Failure: err.Error()})
		return nil, fmt.Errorf("manifest: %w", err)
	}
	methods := flattenManifest(manifest)
	for _, method := range methods {
		if reason, unsupported := unsupportedAdvertisedMethods[method]; unsupported {
			err := fmt.Errorf("unsupported method advertised: %s (%s)", method, reason)
			emit(out, probeEvent{Method: "manifest", OK: false, Failure: err.Error()})
			return nil, err
		}
	}
	required := []string{"manifest", "whoami", "room.metadata", "room.attendants", "tunnel.endpoints", "tunnel.connect"}
	for _, method := range required {
		if !containsString(methods, method) {
			err := fmt.Errorf("required method not advertised: %s", method)
			emit(out, probeEvent{Method: "manifest", OK: false, Failure: err.Error()})
			return nil, err
		}
	}
	emit(out, probeEvent{Method: "manifest", OK: true, Count: len(methods)})
	return methods, nil
}

func verifyWhoami(ctx context.Context, out io.Writer, endpoint muxrpc.Endpoint) error {
	var who struct {
		ID string `json:"id"`
	}
	if err := endpoint.Async(ctx, &who, muxrpc.TypeJSON, muxrpc.Method{"whoami"}); err != nil {
		emit(out, probeEvent{Method: "whoami", OK: false, Failure: err.Error()})
		return fmt.Errorf("whoami: %w", err)
	}
	if _, err := refs.ParseFeedRef(who.ID); err != nil {
		emit(out, probeEvent{Method: "whoami", OK: false, Failure: err.Error()})
		return fmt.Errorf("whoami returned invalid feed %q: %w", who.ID, err)
	}
	emit(out, probeEvent{Method: "whoami", OK: true, Peer: who.ID})
	return nil
}

func verifyMetadata(ctx context.Context, out io.Writer, endpoint muxrpc.Endpoint) error {
	var meta struct {
		Name       string   `json:"name"`
		Membership bool     `json:"membership"`
		Features   []string `json:"features"`
	}
	if err := endpoint.Async(ctx, &meta, muxrpc.TypeJSON, muxrpc.Method{"room", "metadata"}); err != nil {
		emit(out, probeEvent{Method: "room.metadata", OK: false, Failure: err.Error()})
		return fmt.Errorf("room.metadata: %w", err)
	}
	if strings.TrimSpace(meta.Name) == "" {
		err := fmt.Errorf("missing room name")
		emit(out, probeEvent{Method: "room.metadata", OK: false, Failure: err.Error()})
		return err
	}
	if !containsString(meta.Features, "tunnel") {
		err := fmt.Errorf("missing tunnel feature")
		emit(out, probeEvent{Method: "room.metadata", OK: false, Failure: err.Error()})
		return err
	}
	emit(out, probeEvent{Method: "room.metadata", OK: true})
	return nil
}

func collectAttendants(ctx context.Context, out io.Writer, endpoint muxrpc.Endpoint) ([]string, error) {
	src, err := endpoint.Source(ctx, muxrpc.TypeJSON, muxrpc.Method{"room", "attendants"})
	if err != nil {
		emit(out, probeEvent{Method: "room.attendants", OK: false, Failure: err.Error()})
		return nil, fmt.Errorf("room.attendants: %w", err)
	}
	defer src.Cancel(nil)
	ids, err := readIDSourceFrame(ctx, src, "room.attendants")
	if err != nil {
		emit(out, probeEvent{Method: "room.attendants", OK: false, Failure: err.Error()})
		return nil, err
	}
	emit(out, probeEvent{Method: "room.attendants", OK: true, Count: len(ids), IDs: ids})
	return ids, nil
}

func collectEndpoints(ctx context.Context, out io.Writer, endpoint muxrpc.Endpoint) ([]string, error) {
	src, err := endpoint.Source(ctx, muxrpc.TypeJSON, muxrpc.Method{"tunnel", "endpoints"})
	if err != nil {
		emit(out, probeEvent{Method: "tunnel.endpoints", OK: false, Failure: err.Error()})
		return nil, fmt.Errorf("tunnel.endpoints: %w", err)
	}
	defer src.Cancel(nil)
	ids, err := readIDSourceFrame(ctx, src, "tunnel.endpoints")
	if err != nil {
		emit(out, probeEvent{Method: "tunnel.endpoints", OK: false, Failure: err.Error()})
		return nil, err
	}
	if len(ids) == 0 {
		err := fmt.Errorf("tunnel.endpoints returned no peers")
		emit(out, probeEvent{Method: "tunnel.endpoints", OK: false, Failure: err.Error()})
		return nil, err
	}
	emit(out, probeEvent{Method: "tunnel.endpoints", OK: true, Count: len(ids), IDs: ids})
	return ids, nil
}

func probeEBT(ctx context.Context, out io.Writer, endpoint muxrpc.Endpoint) error {
	src, sink, err := endpoint.Duplex(ctx, muxrpc.TypeJSON, muxrpc.Method{"ebt", "replicate"}, 3)
	if err != nil {
		emit(out, probeEvent{Method: "ebt.replicate", OK: false, Failure: err.Error()})
		return fmt.Errorf("ebt.replicate: %w", err)
	}
	defer src.Cancel(nil)
	defer sink.Close()

	readCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if !src.Next(readCtx) {
		if err := src.Err(); err != nil {
			emit(out, probeEvent{Method: "ebt.replicate", OK: false, Failure: err.Error()})
			return fmt.Errorf("ebt.replicate initial state: %w", err)
		}
		err := fmt.Errorf("no initial EBT state")
		emit(out, probeEvent{Method: "ebt.replicate", OK: false, Failure: err.Error()})
		return err
	}
	frame, err := src.Bytes()
	if err != nil {
		emit(out, probeEvent{Method: "ebt.replicate", OK: false, Failure: err.Error()})
		return fmt.Errorf("ebt.replicate read: %w", err)
	}
	var state map[string]interface{}
	if err := json.Unmarshal(frame, &state); err != nil {
		emit(out, probeEvent{Method: "ebt.replicate", OK: false, Failure: err.Error()})
		return fmt.Errorf("ebt.replicate state decode: %w", err)
	}
	emit(out, probeEvent{Method: "ebt.replicate", OK: true, Count: len(state)})
	return nil
}

func probeTunnelHistory(ctx context.Context, out io.Writer, conn *roomConn, keyPair *keys.KeyPair, roomFeed refs.FeedRef, target refs.FeedRef, expectFeed string, cfg config) error {
	source, sink, err := conn.Endpoint.Duplex(ctx, muxrpc.TypeBinary, muxrpc.Method{"tunnel", "connect"}, map[string]interface{}{
		"portal": roomFeed,
		"target": target,
	})
	if err != nil {
		emit(out, probeEvent{Method: "tunnel.connect", OK: false, Peer: target.String(), Failure: err.Error()})
		return fmt.Errorf("tunnel.connect: %w", err)
	}
	defer source.Cancel(nil)
	defer sink.Close()

	appKey, err := parseAppKey(cfg.SHSCap)
	if err != nil {
		return err
	}
	streamConn := muxrpc.NewByteStreamConn(ctx, source, sink, conn.netConn.RemoteAddr())
	shsClient, err := secretstream.NewClient(streamConn, appKey, keyPair.Private(), target.PubKey())
	if err != nil {
		streamConn.Close()
		emit(out, probeEvent{Method: "tunnel.connect", OK: false, Peer: target.String(), Failure: err.Error()})
		return fmt.Errorf("inner SHS init: %w", err)
	}
	if err := shsClient.Handshake(); err != nil {
		streamConn.Close()
		emit(out, probeEvent{Method: "tunnel.connect", OK: false, Peer: target.String(), Failure: err.Error()})
		return fmt.Errorf("inner SHS handshake: %w", err)
	}
	defer shsClient.Close()
	emit(out, probeEvent{Method: "tunnel.connect", OK: true, Peer: target.String()})

	endpoint := muxrpc.NewServer(ctx, shsClient, nil, nil)
	defer endpoint.Terminate()
	history, err := endpoint.Source(ctx, muxrpc.TypeJSON, muxrpc.Method{"createHistoryStream"}, map[string]interface{}{
		"id":       expectFeed,
		"sequence": 0,
		"old":      true,
		"keys":     true,
		"live":     false,
		"limit":    cfg.Limit,
	})
	if err != nil {
		emit(out, probeEvent{Method: "createHistoryStream", OK: false, Peer: target.String(), Feed: expectFeed, Failure: err.Error()})
		return fmt.Errorf("createHistoryStream: %w", err)
	}
	defer history.Cancel(nil)

	count := 0
	for history.Next(ctx) {
		payload, err := history.Bytes()
		if err != nil {
			emit(out, probeEvent{Method: "createHistoryStream", OK: false, Peer: target.String(), Feed: expectFeed, Failure: err.Error()})
			return fmt.Errorf("history frame: %w", err)
		}
		frame, err := validateClassicHistoryFrame(payload, expectFeed)
		if err != nil {
			emit(out, probeEvent{Method: "createHistoryStream", OK: false, Peer: target.String(), Feed: expectFeed, Failure: err.Error()})
			return err
		}
		count++
		emit(out, probeEvent{
			Method:         "createHistoryStream",
			OK:             true,
			Peer:           target.String(),
			Feed:           frame.Author,
			Sequence:       frame.Sequence,
			MessageRef:     frame.MessageRef,
			RawSHA256:      frame.RawSHA256,
			SignatureValid: frame.SignatureValid,
		})
	}
	if err := history.Err(); err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, context.Canceled) {
		emit(out, probeEvent{Method: "createHistoryStream", OK: false, Peer: target.String(), Feed: expectFeed, Failure: err.Error()})
		return fmt.Errorf("history source: %w", err)
	}
	if count < cfg.MinHistory {
		err := fmt.Errorf("history returned %d valid messages, want at least %d", count, cfg.MinHistory)
		emit(out, probeEvent{Method: "createHistoryStream", OK: false, Peer: target.String(), Feed: expectFeed, Failure: err.Error()})
		return err
	}
	return nil
}

type historyFrame struct {
	Key            string
	MessageRef     string
	Author         string
	Sequence       int64
	RawSHA256      string
	SignatureValid bool
}

func validateClassicHistoryFrame(payload []byte, expectAuthor string) (historyFrame, error) {
	var wrapped struct {
		Key   string          `json:"key"`
		Value json.RawMessage `json:"value"`
	}
	valuePayload := bytes.TrimSpace(payload)
	key := ""
	if err := json.Unmarshal(payload, &wrapped); err == nil && len(wrapped.Value) > 0 {
		valuePayload = bytes.TrimSpace(wrapped.Value)
		key = strings.TrimSpace(wrapped.Key)
	}
	var value struct {
		Author    string          `json:"author"`
		Sequence  int64           `json:"sequence"`
		Timestamp int64           `json:"timestamp"`
		Hash      string          `json:"hash"`
		Content   json.RawMessage `json:"content"`
		Signature string          `json:"signature"`
	}
	if err := json.Unmarshal(valuePayload, &value); err != nil {
		return historyFrame{}, fmt.Errorf("decode signed value: %w", err)
	}
	value.Author = strings.TrimSpace(value.Author)
	feedFormat := formats.FeedFromString(value.Author)
	messageFormat := formats.MessageFromString(key)
	if messageFormat == "" {
		messageFormat = formats.MessageSHA256
	}
	if feedFormat != formats.FeedEd25519 {
		return historyFrame{}, formats.UnsupportedFeed(feedFormat, "createHistoryStream", "probe")
	}
	if messageFormat != formats.MessageSHA256 {
		return historyFrame{}, formats.UnsupportedMessage(messageFormat, "createHistoryStream", "probe")
	}
	if expectAuthor = strings.TrimSpace(expectAuthor); expectAuthor != "" && value.Author != expectAuthor {
		return historyFrame{}, fmt.Errorf("author mismatch got=%s want=%s", value.Author, expectAuthor)
	}
	if value.Sequence <= 0 {
		return historyFrame{}, fmt.Errorf("invalid sequence %d", value.Sequence)
	}
	if value.Timestamp == 0 || value.Hash == "" || len(bytes.TrimSpace(value.Content)) == 0 {
		return historyFrame{}, fmt.Errorf("missing required classic message field")
	}
	if _, err := legacy.VerifySignedMessageJSON(valuePayload); err != nil {
		return historyFrame{}, fmt.Errorf("signature verification failed: %w", err)
	}
	computedRef, err := legacy.SignedMessageRefFromJSON(valuePayload)
	if err != nil {
		return historyFrame{}, fmt.Errorf("compute message ref: %w", err)
	}
	if key != "" && computedRef.String() != key {
		return historyFrame{}, fmt.Errorf("message key mismatch got=%s computed=%s", key, computedRef.String())
	}
	rawHash := sha256.Sum256(valuePayload)
	return historyFrame{
		Key:            key,
		MessageRef:     computedRef.String(),
		Author:         value.Author,
		Sequence:       value.Sequence,
		RawSHA256:      hex.EncodeToString(rawHash[:]),
		SignatureValid: true,
	}, nil
}

func readIDSourceFrame(ctx context.Context, src *muxrpc.ByteSource, method string) ([]string, error) {
	readCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if !src.Next(readCtx) {
		if err := src.Err(); err != nil {
			return nil, fmt.Errorf("%s: %w", method, err)
		}
		return nil, fmt.Errorf("%s returned no frame", method)
	}
	body, err := src.Bytes()
	if err != nil {
		return nil, fmt.Errorf("%s read: %w", method, err)
	}
	var state struct {
		Type string   `json:"type"`
		IDs  []string `json:"ids"`
		ID   string   `json:"id"`
	}
	if err := json.Unmarshal(body, &state); err != nil {
		return nil, fmt.Errorf("%s decode %q: %w", method, string(body), err)
	}
	ids := append([]string(nil), state.IDs...)
	if state.ID != "" {
		ids = append(ids, state.ID)
	}
	sort.Strings(ids)
	return ids, nil
}

func selectTarget(configured string, self string, attendants []string, endpoints []string) (refs.FeedRef, error) {
	if strings.TrimSpace(configured) != "" {
		ref, err := refs.ParseFeedRef(configured)
		if err != nil {
			return refs.FeedRef{}, fmt.Errorf("parse --target-feed: %w", err)
		}
		return *ref, nil
	}
	candidates := append([]string(nil), endpoints...)
	if len(candidates) == 0 {
		candidates = append(candidates, attendants...)
	}
	for _, id := range candidates {
		if id == "" || id == self {
			continue
		}
		ref, err := refs.ParseFeedRef(id)
		if err == nil {
			return *ref, nil
		}
	}
	return refs.FeedRef{}, fmt.Errorf("no announced bridge bot target found")
}

type roomConn struct {
	Endpoint muxrpc.Endpoint
	netConn  net.Conn
}

func (c *roomConn) Close() error {
	if c == nil {
		return nil
	}
	if c.Endpoint != nil {
		_ = c.Endpoint.Terminate()
	}
	if c.netConn != nil {
		return c.netConn.Close()
	}
	return nil
}

func openRoomEndpoint(ctx context.Context, localKey *keys.KeyPair, roomFeed refs.FeedRef, roomAddr string, appKeyStr string) (*roomConn, error) {
	appKey, err := parseAppKey(appKeyStr)
	if err != nil {
		return nil, err
	}
	tcpAddr, err := net.ResolveTCPAddr("tcp", strings.TrimSpace(roomAddr))
	if err != nil {
		return nil, fmt.Errorf("resolve room tcp addr: %w", err)
	}
	tcpConn, err := net.DialTCP("tcp", nil, tcpAddr)
	if err != nil {
		return nil, fmt.Errorf("dial room tcp: %w", err)
	}
	client, err := secretstream.NewClient(tcpConn, appKey, localKey.Private(), roomFeed.PubKey())
	if err != nil {
		tcpConn.Close()
		return nil, fmt.Errorf("create secretstream client: %w", err)
	}
	if err := client.Handshake(); err != nil {
		tcpConn.Close()
		return nil, fmt.Errorf("secretstream handshake: %w", err)
	}
	return &roomConn{
		Endpoint: muxrpc.NewServer(ctx, client, nil, &muxrpc.Manifest{}),
		netConn:  tcpConn,
	}, nil
}

func ensureKeyPair(path string) (*keys.KeyPair, error) {
	keyPath := strings.TrimSpace(path)
	if keyPath == "" {
		return nil, fmt.Errorf("empty key file path")
	}
	kp, err := keys.Load(keyPath)
	if err == nil {
		return kp, nil
	}
	if !errors.Is(err, os.ErrNotExist) && !os.IsNotExist(err) {
		return nil, fmt.Errorf("load keypair: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(keyPath), 0o700); err != nil {
		return nil, fmt.Errorf("create keypair dir: %w", err)
	}
	kp, err = keys.Generate()
	if err != nil {
		return nil, fmt.Errorf("generate keypair: %w", err)
	}
	if err := keys.Save(kp, keyPath); err != nil {
		return nil, fmt.Errorf("save keypair: %w", err)
	}
	return kp, nil
}

func parseAppKey(appKeyStr string) (secretstream.AppKey, error) {
	var appKey secretstream.AppKey
	appKeyBytes, err := base64.StdEncoding.DecodeString(strings.TrimSpace(appKeyStr))
	if err != nil {
		return appKey, fmt.Errorf("decode app key: %w", err)
	}
	if len(appKeyBytes) != len(appKey) {
		return appKey, fmt.Errorf("invalid app key length (got %d, need %d)", len(appKeyBytes), len(appKey))
	}
	copy(appKey[:], appKeyBytes)
	return appKey, nil
}

func flattenManifest(root map[string]interface{}) []string {
	var methods []string
	var walk func(prefix string, v interface{})
	walk = func(prefix string, v interface{}) {
		switch tv := v.(type) {
		case string:
			methods = append(methods, prefix)
		case map[string]interface{}:
			for key, child := range tv {
				next := key
				if prefix != "" {
					next = prefix + "." + key
				}
				walk(next, child)
			}
		}
	}
	for key, value := range root {
		walk(key, value)
	}
	sort.Strings(methods)
	return methods
}

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == needle {
			return true
		}
	}
	return false
}
