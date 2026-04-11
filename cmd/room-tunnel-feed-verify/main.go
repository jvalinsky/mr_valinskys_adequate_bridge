package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/db"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/keys"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/muxrpc"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/secretstream"
)

const defaultSHSCap = "1KHLiKZvAvjbY1ziZEHMXawbCEIM6qwjCDm3VYRan/s="

func main() {
	if len(os.Args) < 2 {
		fatalf("usage: room-tunnel-feed-verify <serve|read|probe> [flags]")
	}

	switch strings.ToLower(strings.TrimSpace(os.Args[1])) {
	case "serve":
		if err := runServe(os.Args[2:]); err != nil {
			fatalf("serve: %v", err)
		}
	case "read":
		if err := runRead(os.Args[2:]); err != nil {
			fatalf("read: %v", err)
		}
	case "probe":
		if err := runProbe(os.Args[2:]); err != nil {
			fatalf("probe: %v", err)
		}
	default:
		fatalf("unknown mode %q (expected serve, read, or probe)", os.Args[1])
	}
}

func runServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	var cfg serveConfig
	fs.StringVar(&cfg.RoomAddr, "room-addr", "", "room muxrpc tcp address (host:port)")
	fs.StringVar(&cfg.RoomFeed, "room-feed", "", "room feed ref (@...ed25519)")
	fs.StringVar(&cfg.KeyFile, "key-file", "", "path to peer secret key file")
	fs.StringVar(&cfg.DBPath, "db", "", "bridge sqlite database path")
	fs.StringVar(&cfg.SourceDID, "source-did", "", "source ATProto DID")
	fs.StringVar(&cfg.SourceFeed, "source-feed", "", "expected source SSB feed ref")
	fs.StringVar(&cfg.ExpectedURIs, "expected-uris", "", "comma-separated expected AT URIs")
	fs.StringVar(&cfg.ReadyFile, "ready-file", "", "optional ready marker file path")
	fs.StringVar(&cfg.SHSCap, "shs-cap", defaultSHSCap, "secret-handshake app key (base64)")
	fs.DurationVar(&cfg.Timeout, "timeout", 30*time.Second, "serve timeout")
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
		return fmt.Errorf("parse --room-feed: %v", err)
	}

	var sourceFeed refs.FeedRef
	var hasSourceFeed bool
	if strings.TrimSpace(cfg.SourceFeed) != "" {
		parsed, err := refs.ParseFeedRef(cfg.SourceFeed)
		if err != nil {
			return fmt.Errorf("parse --source-feed: %v", err)
		}
		sourceFeed = *parsed
		hasSourceFeed = true
	}
	expectedURIs := splitCSV(cfg.ExpectedURIs)

	serveDone := make(chan serveResult, 1)
	handler := &tunnelServeHandler{
		keyPair:      keyPair,
		dbPath:       cfg.DBPath,
		sourceDID:    cfg.SourceDID,
		expectedURIs: expectedURIs,
		sourceFeed:   sourceFeed,
		hasSourceFeed: hasSourceFeed,
		serveDone:    serveDone,
	}

	conn, err := openRoomEndpoint(ctx, keyPair, *roomFeed, cfg.RoomAddr, cfg.SHSCap, handler)
	if err != nil {
		return err
	}
	defer conn.Close()

	if _, err := verifyRoomConn(ctx, conn.Endpoint); err != nil {
		return err
	}
	if err := announce(ctx, conn.Endpoint); err != nil {
		return err
	}

	if cfg.ReadyFile != "" {
		if err := writeReadyFile(cfg.ReadyFile, keyPair.FeedRef()); err != nil {
			return err
		}
	}

	select {
	case res := <-serveDone:
		fmt.Printf("served_tunnel_snapshot entries=%d\n", res.EntryCount)
		return nil
	case <-ctx.Done():
		return fmt.Errorf("timed out waiting for tunnel.connect call")
	}
}

func runRead(args []string) error {
	fs := flag.NewFlagSet("read", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	var cfg readConfig
	fs.StringVar(&cfg.RoomAddr, "room-addr", "", "room muxrpc tcp address (host:port)")
	fs.StringVar(&cfg.RoomFeed, "room-feed", "", "room feed ref (@...ed25519)")
	fs.StringVar(&cfg.KeyFile, "key-file", "", "path to peer secret key file")
	fs.StringVar(&cfg.TargetFeed, "target-feed", "", "target announced peer feed ref")
	fs.StringVar(&cfg.ExpectSourceFeed, "expect-source-feed", "", "expected source SSB feed ref in tunnel snapshot")
	fs.StringVar(&cfg.ExpectedURIs, "expected-uris", "", "comma-separated expected AT URIs")
	fs.StringVar(&cfg.SHSCap, "shs-cap", defaultSHSCap, "secret-handshake app key (base64)")
	fs.DurationVar(&cfg.Timeout, "timeout", 30*time.Second, "read timeout")
	fs.IntVar(&cfg.MinCount, "min-count", 1, "minimum number of entries expected in snapshot")
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
		return fmt.Errorf("parse --room-feed: %v", err)
	}
	targetFeed, err := refs.ParseFeedRef(cfg.TargetFeed)
	if err != nil {
		return fmt.Errorf("parse --target-feed: %v", err)
	}

	handler := &whoamiHandler{keyPair: keyPair}

	conn, err := openRoomEndpoint(ctx, keyPair, *roomFeed, cfg.RoomAddr, cfg.SHSCap, handler)
	if err != nil {
		return err
	}
	defer conn.Close()

	if _, err := verifyRoomConn(ctx, conn.Endpoint); err != nil {
		return err
	}

	source, sink, err := conn.Endpoint.Duplex(ctx, muxrpc.TypeBinary, muxrpc.Method{"tunnel", "connect"}, tunnelConnectArg{
		Portal: *roomFeed,
		Target: *targetFeed,
	})
	if err != nil {
		return fmt.Errorf("open tunnel.connect duplex: %w", err)
	}

	if !source.Next(ctx) {
		if err := source.Err(); err != nil {
			return fmt.Errorf("read tunnel snapshot frame: %w", err)
		}
		return fmt.Errorf("read tunnel snapshot frame: stream closed")
	}
	payload, err := source.Bytes()
	if err != nil {
		return fmt.Errorf("decode tunnel snapshot bytes: %w", err)
	}
	_ = sink.Close()

	var snapshot tunnelSnapshot
	if err := json.Unmarshal(payload, &snapshot); err != nil {
		return fmt.Errorf("decode tunnel snapshot json: %w", err)
	}
	if err := validateSnapshot(snapshot, cfg.ExpectSourceFeed, splitCSV(cfg.ExpectedURIs), cfg.MinCount); err != nil {
		return err
	}

	fmt.Printf("read_tunnel_snapshot entries=%d\n", len(snapshot.Entries))
	return nil
}

func runProbe(args []string) error {
	fs := flag.NewFlagSet("probe", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	var cfg probeConfig
	fs.StringVar(&cfg.RoomAddr, "room-addr", "", "room muxrpc tcp address (host:port)")
	fs.StringVar(&cfg.RoomFeed, "room-feed", "", "room feed ref (@...ed25519)")
	fs.StringVar(&cfg.KeyFile, "key-file", "", "path to peer secret key file")
	fs.StringVar(&cfg.TargetFeed, "target-feed", "", "target announced peer feed ref")
	fs.StringVar(&cfg.SHSCap, "shs-cap", defaultSHSCap, "secret-handshake app key (base64)")
	fs.DurationVar(&cfg.Timeout, "timeout", 15*time.Second, "probe timeout")
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
		return fmt.Errorf("parse --room-feed: %v", err)
	}
	targetFeed, err := refs.ParseFeedRef(cfg.TargetFeed)
	if err != nil {
		return fmt.Errorf("parse --target-feed: %v", err)
	}

	handler := &whoamiHandler{keyPair: keyPair}

	conn, err := openRoomEndpoint(ctx, keyPair, *roomFeed, cfg.RoomAddr, cfg.SHSCap, handler)
	if err != nil {
		return err
	}
	defer conn.Close()

	if _, err := verifyRoomConn(ctx, conn.Endpoint); err != nil {
		return err
	}

	source, sink, err := conn.Endpoint.Duplex(ctx, muxrpc.TypeBinary, muxrpc.Method{"tunnel", "connect"}, tunnelConnectArg{
		Portal: *roomFeed,
		Target: *targetFeed,
	})
	if err != nil {
		return fmt.Errorf("probe tunnel.connect duplex: %w", err)
	}
	_ = sink.Close()
	source.Cancel(nil)

	fmt.Printf("probe_tunnel_connect_ok target=%s\n", targetFeed.String())
	return nil
}

type serveConfig struct {
	RoomAddr     string
	RoomFeed     string
	KeyFile      string
	DBPath       string
	SourceDID    string
	SourceFeed   string
	ExpectedURIs string
	ReadyFile    string
	SHSCap       string
	Timeout      time.Duration
}

func (c serveConfig) validate() error {
	if strings.TrimSpace(c.RoomAddr) == "" {
		return fmt.Errorf("--room-addr is required")
	}
	if strings.TrimSpace(c.RoomFeed) == "" {
		return fmt.Errorf("--room-feed is required")
	}
	if strings.TrimSpace(c.KeyFile) == "" {
		return fmt.Errorf("--key-file is required")
	}
	if strings.TrimSpace(c.DBPath) == "" {
		return fmt.Errorf("--db is required")
	}
	if strings.TrimSpace(c.SourceDID) == "" {
		return fmt.Errorf("--source-did is required")
	}
	if c.Timeout <= 0 {
		return fmt.Errorf("--timeout must be > 0")
	}
	return nil
}

type readConfig struct {
	RoomAddr         string
	RoomFeed         string
	KeyFile          string
	TargetFeed       string
	ExpectSourceFeed string
	ExpectedURIs     string
	SHSCap           string
	Timeout          time.Duration
	MinCount         int
}

type probeConfig struct {
	RoomAddr   string
	RoomFeed   string
	KeyFile    string
	TargetFeed string
	SHSCap     string
	Timeout    time.Duration
}

type tunnelConnectArg struct {
	Portal refs.FeedRef `json:"portal"`
	Target refs.FeedRef `json:"target"`
}

func (c readConfig) validate() error {
	if strings.TrimSpace(c.RoomAddr) == "" {
		return fmt.Errorf("--room-addr is required")
	}
	if strings.TrimSpace(c.RoomFeed) == "" {
		return fmt.Errorf("--room-feed is required")
	}
	if strings.TrimSpace(c.KeyFile) == "" {
		return fmt.Errorf("--key-file is required")
	}
	if strings.TrimSpace(c.TargetFeed) == "" {
		return fmt.Errorf("--target-feed is required")
	}
	if c.Timeout <= 0 {
		return fmt.Errorf("--timeout must be > 0")
	}
	if c.MinCount <= 0 {
		return fmt.Errorf("--min-count must be > 0")
	}
	return nil
}

func (c probeConfig) validate() error {
	if strings.TrimSpace(c.RoomAddr) == "" {
		return fmt.Errorf("--room-addr is required")
	}
	if strings.TrimSpace(c.RoomFeed) == "" {
		return fmt.Errorf("--room-feed is required")
	}
	if strings.TrimSpace(c.KeyFile) == "" {
		return fmt.Errorf("--key-file is required")
	}
	if strings.TrimSpace(c.TargetFeed) == "" {
		return fmt.Errorf("--target-feed is required")
	}
	if c.Timeout <= 0 {
		return fmt.Errorf("--timeout must be > 0")
	}
	return nil
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
		c.Endpoint.Terminate()
	}
	if c.netConn != nil {
		return c.netConn.Close()
	}
	return nil
}

// whoamiHandler responds to "whoami" RPC calls
type whoamiHandler struct {
	keyPair *keys.KeyPair
}

func (h *whoamiHandler) Handled(m muxrpc.Method) bool {
	return len(m) == 1 && m[0] == "whoami"
}

func (h *whoamiHandler) HandleCall(ctx context.Context, req *muxrpc.Request) {
	req.Return(ctx, map[string]interface{}{"id": h.keyPair.FeedRef().String()})
}

func (h *whoamiHandler) HandleConnect(ctx context.Context, edp muxrpc.Endpoint) {}

// tunnelServeHandler handles tunnel.connect for the serve command
type tunnelServeHandler struct {
	keyPair      *keys.KeyPair
	dbPath       string
	sourceDID    string
	expectedURIs []string
	sourceFeed   refs.FeedRef
	hasSourceFeed bool
	serveDone    chan serveResult
}

func (h *tunnelServeHandler) Handled(m muxrpc.Method) bool {
	if len(m) == 1 && m[0] == "whoami" {
		return true
	}
	if len(m) == 2 && m[0] == "tunnel" && m[1] == "connect" {
		return true
	}
	return false
}

func (h *tunnelServeHandler) HandleCall(ctx context.Context, req *muxrpc.Request) {
	switch {
	case len(req.Method) == 1 && req.Method[0] == "whoami":
		req.Return(ctx, map[string]interface{}{"id": h.keyPair.FeedRef().String()})
		return
	case len(req.Method) == 2 && req.Method[0] == "tunnel" && req.Method[1] == "connect":
		h.handleTunnelConnect(ctx, req)
		return
	default:
		req.CloseWithError(fmt.Errorf("no such method: %s", req.Method))
	}
}

func (h *tunnelServeHandler) handleTunnelConnect(ctx context.Context, req *muxrpc.Request) {
	sink := req.Sink()
	if sink == nil {
		req.CloseWithError(fmt.Errorf("tunnel.connect requires a sink"))
		return
	}

	entries, err := loadPublishedEntries(ctx, h.dbPath, h.sourceDID, h.expectedURIs)
	if err != nil {
		req.CloseWithError(err)
		return
	}

	snapshot := tunnelSnapshot{
		ExpectedCount: len(h.expectedURIs),
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339Nano),
		Entries:       entries,
	}
	if h.hasSourceFeed {
		snapshot.SourceFeed = h.sourceFeed.String()
	}
	payload, err := json.Marshal(snapshot)
	if err != nil {
		req.CloseWithError(fmt.Errorf("marshal snapshot: %w", err))
		return
	}
	if _, err := sink.Write(payload); err != nil {
		req.CloseWithError(fmt.Errorf("write snapshot payload: %w", err))
		return
	}
	if err := sink.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
		req.CloseWithError(fmt.Errorf("close snapshot sink: %w", err))
		return
	}

	select {
	case h.serveDone <- serveResult{EntryCount: len(entries)}:
	default:
	}
}

func (h *tunnelServeHandler) HandleConnect(ctx context.Context, edp muxrpc.Endpoint) {}

type roomMetadata struct {
	Name       string   `json:"name"`
	Membership bool     `json:"membership"`
	Features   []string `json:"features"`
}

func verifyRoomConn(ctx context.Context, endpoint muxrpc.Endpoint) (*roomMetadata, error) {
	var who struct {
		ID refs.FeedRef `json:"id"`
	}
	if err := endpoint.Async(ctx, &who, muxrpc.TypeJSON, muxrpc.Method{"whoami"}); err != nil {
		return nil, fmt.Errorf("room whoami failed: %w", err)
	}

	var meta roomMetadata
	if err := endpoint.Async(ctx, &meta, muxrpc.TypeJSON, muxrpc.Method{"room", "metadata"}); err != nil {
		return nil, fmt.Errorf("room metadata failed: %w", err)
	}
	if !containsString(meta.Features, "tunnel") {
		return nil, fmt.Errorf("room metadata missing tunnel feature: %+v", meta)
	}
	return &meta, nil
}

func announce(ctx context.Context, endpoint muxrpc.Endpoint) error {
	var announced bool
	err := endpoint.Sync(ctx, &announced, muxrpc.TypeJSON, muxrpc.Method{"tunnel", "announce"})
	if err != nil {
		return fmt.Errorf("room tunnel.announce failed: %w", err)
	}
	if !announced {
		return fmt.Errorf("room tunnel.announce returned false")
	}
	return nil
}

func openRoomEndpoint(
	ctx context.Context,
	localKey *keys.KeyPair,
	roomFeed refs.FeedRef,
	roomAddr string,
	appKeyStr string,
	handler muxrpc.Handler,
) (*roomConn, error) {
	// Parse app key
	appKeyBytes, err := base64.StdEncoding.DecodeString(strings.TrimSpace(appKeyStr))
	if err != nil {
		return nil, fmt.Errorf("decode app key: %w", err)
	}
	var appKey secretstream.AppKey
	copy(appKey[:], appKeyBytes)

	// Dial TCP
	tcpAddr, err := net.ResolveTCPAddr("tcp", strings.TrimSpace(roomAddr))
	if err != nil {
		return nil, fmt.Errorf("resolve room tcp addr: %w", err)
	}
	tcpConn, err := net.DialTCP("tcp", nil, tcpAddr)
	if err != nil {
		return nil, fmt.Errorf("dial room tcp: %w", err)
	}

	// Create internal secretstream client and perform handshake
	client, err := secretstream.NewClient(tcpConn, appKey, localKey.Private(), roomFeed.PubKey())
	if err != nil {
		tcpConn.Close()
		return nil, fmt.Errorf("create secretstream client: %w", err)
	}
	if err := client.Handshake(); err != nil {
		tcpConn.Close()
		return nil, fmt.Errorf("secretstream handshake: %w", err)
	}
	// client now implements net.Conn with boxstream encryption

	// Create internal muxrpc server
	if handler == nil {
		handler = &muxrpc.HandlerMux{}
	}
	srv := muxrpc.NewServer(ctx, client, handler, &muxrpc.Manifest{})

	return &roomConn{
		Endpoint: srv,
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
	// Use errors.Is to handle wrapped errors from keys.Load
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

func writeReadyFile(path string, feed refs.FeedRef) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create ready-file dir: %w", err)
	}
	payload, err := json.Marshal(map[string]string{
		"feed": feed.String(),
	})
	if err != nil {
		return fmt.Errorf("encode ready-file json: %w", err)
	}
	if err := os.WriteFile(path, append(payload, '\n'), 0o600); err != nil {
		return fmt.Errorf("write ready-file: %w", err)
	}
	return nil
}

type bridgeEntry struct {
	ATURI       string `json:"at_uri"`
	SSBMsgRef   string `json:"ssb_msg_ref"`
	Type        string `json:"type"`
	PublishedAt string `json:"published_at,omitempty"`
}

type tunnelSnapshot struct {
	SourceFeed    string        `json:"source_feed"`
	ExpectedCount int           `json:"expected_count"`
	GeneratedAt   string        `json:"generated_at"`
	Entries       []bridgeEntry `json:"entries"`
}

type serveResult struct {
	EntryCount int
}

func loadPublishedEntries(ctx context.Context, dbPath, sourceDID string, expectedURIs []string) ([]bridgeEntry, error) {
	database, err := db.Open(dbPath)
	if err != nil {
		return nil, fmt.Errorf("open bridge db: %w", err)
	}
	defer database.Close()

	trimmedDID := strings.TrimSpace(sourceDID)
	results := make([]bridgeEntry, 0, len(expectedURIs))
	if len(expectedURIs) > 0 {
		for _, atURI := range expectedURIs {
			msg, err := database.GetMessage(ctx, atURI)
			if err != nil {
				return nil, fmt.Errorf("read message %s: %w", atURI, err)
			}
			if msg == nil {
				continue
			}
			if trimmedDID != "" && msg.ATDID != trimmedDID {
				continue
			}
			if msg.MessageState != db.MessageStatePublished {
				continue
			}
			if strings.TrimSpace(msg.SSBMsgRef) == "" {
				continue
			}

			entry := bridgeEntry{
				ATURI:     msg.ATURI,
				SSBMsgRef: msg.SSBMsgRef,
				Type:      msg.Type,
			}
			if msg.PublishedAt != nil {
				entry.PublishedAt = msg.PublishedAt.UTC().Format(time.RFC3339Nano)
			}
			results = append(results, entry)
		}
		sort.Slice(results, func(i, j int) bool { return results[i].ATURI < results[j].ATURI })
		return results, nil
	}

	messages, err := database.GetRecentMessages(ctx, 500)
	if err != nil {
		return nil, fmt.Errorf("list recent messages: %w", err)
	}
	for _, msg := range messages {
		if trimmedDID != "" && msg.ATDID != trimmedDID {
			continue
		}
		if msg.MessageState != db.MessageStatePublished {
			continue
		}
		if strings.TrimSpace(msg.SSBMsgRef) == "" {
			continue
		}
		entry := bridgeEntry{
			ATURI:     msg.ATURI,
			SSBMsgRef: msg.SSBMsgRef,
			Type:      msg.Type,
		}
		if msg.PublishedAt != nil {
			entry.PublishedAt = msg.PublishedAt.UTC().Format(time.RFC3339Nano)
		}
		results = append(results, entry)
	}
	sort.Slice(results, func(i, j int) bool { return results[i].ATURI < results[j].ATURI })
	return results, nil
}

func validateSnapshot(snapshot tunnelSnapshot, expectedSourceFeed string, expectedURIs []string, minCount int) error {
	if strings.TrimSpace(expectedSourceFeed) != "" && strings.TrimSpace(snapshot.SourceFeed) != strings.TrimSpace(expectedSourceFeed) {
		return fmt.Errorf("snapshot source feed mismatch: got %q want %q", snapshot.SourceFeed, expectedSourceFeed)
	}

	if len(snapshot.Entries) < minCount {
		return fmt.Errorf("snapshot entry count %d is below required minimum %d", len(snapshot.Entries), minCount)
	}

	seen := make(map[string]struct{}, len(snapshot.Entries))
	for _, entry := range snapshot.Entries {
		if strings.TrimSpace(entry.ATURI) == "" {
			return fmt.Errorf("snapshot entry has empty at_uri")
		}
		if strings.TrimSpace(entry.SSBMsgRef) == "" {
			return fmt.Errorf("snapshot entry %q has empty ssb_msg_ref", entry.ATURI)
		}
		seen[entry.ATURI] = struct{}{}
	}

	for _, atURI := range expectedURIs {
		if _, ok := seen[atURI]; !ok {
			return fmt.Errorf("expected at_uri %q not present in tunnel snapshot", atURI)
		}
	}
	return nil
}

func splitCSV(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	seen := make(map[string]struct{}, len(parts))
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == needle {
			return true
		}
	}
	return false
}

func fatalf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
