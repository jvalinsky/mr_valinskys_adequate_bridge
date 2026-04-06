// Package firehose streams ATProto repository commits from subscribeRepos.
package firehose

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/pkg/atproto"
	atfirehose "github.com/jvalinsky/mr_valinskys_adequate_bridge/pkg/atproto/firehose"
	atrepo "github.com/jvalinsky/mr_valinskys_adequate_bridge/pkg/atproto/repo"
)

// EventHandler handles repository commit events emitted by the firehose stream.
type EventHandler interface {
	HandleCommit(ctx context.Context, evt *atproto.SyncSubscribeRepos_Commit) error
}

type IdentityHandler interface {
	HandleIdentity(ctx context.Context, evt *atproto.SyncSubscribeRepos_Identity) error
}

type AccountHandler interface {
	HandleAccount(ctx context.Context, evt *atproto.SyncSubscribeRepos_Account) error
}

// Client connects to subscribeRepos and forwards commits to an EventHandler.
type Client struct {
	relayURL          string
	handler           EventHandler
	dialer            *websocket.Dialer
	cursor            *int64
	ConnectedCallback func()
}

type ReconnectConfig struct {
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
	Jitter         time.Duration
}

// ClientOption configures optional Client behavior.
type ClientOption func(*Client)

// WithCursor starts the stream from a specific firehose cursor sequence.
func WithCursor(cursor int64) ClientOption {
	return func(c *Client) {
		c.cursor = &cursor
	}
}

// WithConnectedCallback calls the provided function when the firehose websocket connects.
func WithConnectedCallback(cb func()) ClientOption {
	return func(c *Client) {
		c.ConnectedCallback = cb
	}
}

// NewClient creates a firehose Client with optional configuration.
func NewClient(relayURL string, handler EventHandler, _ interface{}, opts ...ClientOption) *Client {
	if relayURL == "" {
		relayURL = "wss://bsky.network/xrpc/com.atproto.sync.subscribeRepos"
	}
	client := &Client{
		relayURL: relayURL,
		handler:  handler,
		dialer:   websocket.DefaultDialer,
	}
	for _, opt := range opts {
		opt(client)
	}
	return client
}

// Run opens the websocket stream and blocks until the stream exits or ctx is canceled.
func (c *Client) Run(ctx context.Context) error {
	streamURL, err := c.streamURL()
	if err != nil {
		return fmt.Errorf("build stream URL: %w", err)
	}
	slog.Info("firehose: connecting", "url", streamURL)

	con, resp, err := c.dialer.DialContext(ctx, streamURL, http.Header{})
	if err != nil {
		if resp != nil {
			return fmt.Errorf("failed to dial (status=%d): %w", resp.StatusCode, err)
		}
		return fmt.Errorf("failed to dial: %w", err)
	}
	defer con.Close()

	slog.Info("firehose: connected")

	if c.ConnectedCallback != nil {
		go c.ConnectedCallback()
	}

	// Keepalive ping loop
	go func() {
		ticker := time.NewTicker(25 * time.Second)
		defer ticker.Stop()

		failures := 0
		for {
			select {
			case <-ctx.Done():
				_ = con.Close()
				return
			case <-ticker.C:
				if err := con.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(10*time.Second)); err != nil {
					slog.Warn("firehose: ping failed", "error", err)
					failures++
					if failures >= 4 {
						_ = con.Close()
						return
					}
					continue
				}
				failures = 0
			}
		}
	}()

	if c.ConnectedCallback != nil {
		go c.ConnectedCallback()
	}

	return c.handleStream(ctx, con)
}

func (c *Client) handleStream(ctx context.Context, con *websocket.Conn) error {
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		failures := 0
		for {
			select {
			case <-ctx.Done():
				_ = con.Close()
				return
			case <-ticker.C:
				if err := con.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(10*time.Second)); err != nil {
					slog.Warn("firehose: ping failed", "error", err)
					failures++
					if failures >= 4 {
						_ = con.Close()
						return
					}
					continue
				}
				failures = 0
			}
		}
	}()

	con.SetPingHandler(func(message string) error {
		err := con.WriteControl(websocket.PongMessage, []byte(message), time.Now().Add(60*time.Second))
		if err == websocket.ErrCloseSent {
			return nil
		}
		return err
	})
	con.SetPongHandler(func(_ string) error {
		return con.SetReadDeadline(time.Now().Add(time.Minute))
	})
	if err := con.SetReadDeadline(time.Now().Add(time.Minute)); err != nil {
		return fmt.Errorf("set initial read deadline: %w", err)
	}

	lastSeq := int64(-1)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		messageType, reader, err := con.NextReader()
		if err != nil {
			return fmt.Errorf("read firehose frame: %w", err)
		}
		if messageType != websocket.BinaryMessage {
			return fmt.Errorf("expected binary message from subscription endpoint")
		}

		event, err := atfirehose.ReadEvent(reader)
		if err != nil {
			return err
		}

		switch {
		case event.RepoCommit != nil:
			if event.RepoCommit.Seq < lastSeq {
				slog.Warn("firehose: out of order event",
					"seq", event.RepoCommit.Seq,
					"lastSeq", lastSeq,
					"repo", event.RepoCommit.Repo)
			}
			lastSeq = event.RepoCommit.Seq
			if err := c.handleRepoCommit(ctx, event.RepoCommit); err != nil {
				return err
			}
		case event.RepoInfo != nil:
			if err := c.handleRepoInfo(event.RepoInfo); err != nil {
				return err
			}
		case event.RepoIdentity != nil:
			if handler, ok := c.handler.(IdentityHandler); ok {
				if err := handler.HandleIdentity(ctx, event.RepoIdentity); err != nil {
					return err
				}
			}
		case event.RepoAccount != nil:
			if handler, ok := c.handler.(AccountHandler); ok {
				if err := handler.HandleAccount(ctx, event.RepoAccount); err != nil {
					return err
				}
			}
		case event.Error != nil:
			if err := c.handleError(event.Error); err != nil {
				return err
			}
		}
	}
}

func (c *Client) handleRepoCommit(ctx context.Context, evt *atproto.SyncSubscribeRepos_Commit) error {
	slog.Debug("firehose: repo commit",
		"repo", evt.Repo,
		"seq", evt.Seq,
		"ops", len(evt.Ops))
	for i, op := range evt.Ops {
		cidStr := "nil"
		if op.Cid != nil {
			cidStr = op.Cid.String()
		}
		slog.Debug("firehose: commit op",
			"index", i,
			"action", op.Action,
			"path", op.Path,
			"cid", cidStr)
	}
	if err := c.handler.HandleCommit(ctx, evt); err != nil {
		slog.Error("firehose: commit handler error",
			"error", err,
			"repo", evt.Repo,
			"seq", evt.Seq)
	}
	return nil
}

func (c *Client) handleRepoInfo(info *atproto.SyncSubscribeRepos_Info) error {
	slog.Debug("firehose: repo info", "name", info.Name)
	return nil
}

func (c *Client) handleError(errEvt *atfirehose.ErrorFrame) error {
	if errEvt == nil {
		return nil
	}
	if errEvt.Message != nil {
		slog.Error("firehose: error", "message", *errEvt.Message)
		return nil
	}
	slog.Error("firehose: error", "error", errEvt.Error)
	return nil
}

func (c *Client) RunWithReconnect(ctx context.Context, cfg ReconnectConfig) error {
	if cfg.InitialBackoff <= 0 {
		cfg.InitialBackoff = 2 * time.Second
	}
	if cfg.MaxBackoff <= 0 {
		cfg.MaxBackoff = 60 * time.Second
	}
	if cfg.Jitter <= 0 {
		cfg.Jitter = 750 * time.Millisecond
	}

	return runWithReconnectLoop(ctx, cfg, c.Run)
}

func runWithReconnectLoop(ctx context.Context, cfg ReconnectConfig, runOnce func(context.Context) error) error {
	backoff := cfg.InitialBackoff
	for {
		err := runOnce(ctx)
		if err == nil || errors.Is(err, context.Canceled) {
			return err
		}
		if IsFatalStreamError(err) {
			return err
		}

		sleepFor := jitterDuration(backoff, cfg.Jitter)
		slog.Warn("firehose: reconnect retry",
			"backoff", sleepFor,
			"error", err)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(sleepFor):
		}

		backoff *= 2
		if backoff > cfg.MaxBackoff {
			backoff = cfg.MaxBackoff
		}
	}
}

func IsFatalStreamError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if errors.Is(err, io.EOF) {
		return false
	}

	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "build stream url") {
		return true
	}
	if strings.Contains(msg, "status=401") || strings.Contains(msg, "status=403") || strings.Contains(msg, "status=404") {
		return true
	}
	if strings.Contains(msg, "unsupported protocol scheme") {
		return true
	}
	if strings.Contains(msg, "malformed") && strings.Contains(msg, "url") {
		return true
	}

	return false
}

func jitterDuration(base, jitter time.Duration) time.Duration {
	if base <= 0 {
		base = 2 * time.Second
	}
	if jitter <= 0 {
		return base
	}
	n := rand.Int63n(int64(2*jitter) + 1)
	offset := time.Duration(n) - jitter
	d := base + offset
	if d < 250*time.Millisecond {
		return 250 * time.Millisecond
	}
	return d
}

func (c *Client) streamURL() (string, error) {
	if c.cursor == nil || *c.cursor <= 0 {
		return c.relayURL, nil
	}

	parsed, err := url.Parse(c.relayURL)
	if err != nil {
		return "", err
	}

	query := parsed.Query()
	query.Set("cursor", strconv.FormatInt(*c.cursor, 10))
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

// ParseCommit parses the CAR payload embedded in a commit event.
func ParseCommit(ctx context.Context, evt *atproto.SyncSubscribeRepos_Commit) (*atrepo.Repo, error) {
	if evt.Blocks == nil {
		return nil, fmt.Errorf("no blocks in commit")
	}

	rr, err := atrepo.ReadRepoFromCar(ctx, bytes.NewReader(evt.Blocks))
	if err != nil {
		return nil, fmt.Errorf("reading repo from car: %w", err)
	}

	return rr, nil
}

// ProcessOps validates that create/update operations can be decoded from the CAR.
func ProcessOps(ctx context.Context, rr *atrepo.Repo, evt *atproto.SyncSubscribeRepos_Commit) error {
	for _, op := range evt.Ops {
		if op.Action != "create" && op.Action != "update" {
			continue
		}

		if op.Cid == nil {
			continue
		}

		rc, rec, err := rr.GetRecordBytes(ctx, op.Path)
		if err != nil {
			return fmt.Errorf("getting record %s: %w", op.Path, err)
		}

		// For now just verifying we can read it.
		// Real implementation will pass to mapper.
		_ = rc
		_ = rec
	}
	return nil
}
