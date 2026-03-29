// Package firehose streams ATProto repository commits from subscribeRepos.
package firehose

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/events"
	"github.com/bluesky-social/indigo/events/schedulers/sequential"
	"github.com/bluesky-social/indigo/repo"
	"github.com/gorilla/websocket"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/logutil"
)

// EventHandler handles repository commit events emitted by the firehose stream.
type EventHandler interface {
	HandleCommit(ctx context.Context, evt *atproto.SyncSubscribeRepos_Commit) error
}

// Client connects to subscribeRepos and forwards commits to an EventHandler.
type Client struct {
	relayURL string
	handler  EventHandler
	logger   *log.Logger
	dialer   *websocket.Dialer
	cursor   *int64
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

// NewClient creates a firehose Client with optional configuration.
func NewClient(relayURL string, handler EventHandler, logger *log.Logger, opts ...ClientOption) *Client {
	if relayURL == "" {
		relayURL = "wss://bsky.network/xrpc/com.atproto.sync.subscribeRepos"
	}
	logger = logutil.Ensure(logger)
	client := &Client{
		relayURL: relayURL,
		handler:  handler,
		logger:   logger,
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
	c.logger.Printf("Connecting to ATProto firehose at %s", streamURL)

	con, resp, err := c.dialer.DialContext(ctx, streamURL, http.Header{})
	if err != nil {
		if resp != nil {
			return fmt.Errorf("failed to dial (status=%d): %w", resp.StatusCode, err)
		}
		return fmt.Errorf("failed to dial: %w", err)
	}
	defer con.Close()

	c.logger.Println("Connected to firehose")

	callbacks := &events.RepoStreamCallbacks{
		RepoCommit: c.handleRepoCommit,
		RepoInfo:   c.handleRepoInfo,
		Error:      c.handleError,
	}

	sched := sequential.NewScheduler("firehose", callbacks.EventHandler)
	return events.HandleRepoStream(ctx, con, sched, nil)
}

func (c *Client) handleRepoCommit(evt *atproto.SyncSubscribeRepos_Commit) error {
	if err := c.handler.HandleCommit(context.Background(), evt); err != nil {
		c.logger.Printf("Error handling commit: %v", err)
	}
	return nil
}

func (c *Client) handleRepoInfo(info *atproto.SyncSubscribeRepos_Info) error {
	c.logger.Printf("RepoInfo: %v", info.Name)
	return nil
}

func (c *Client) handleError(errEvt *events.ErrorFrame) error {
	c.logger.Printf("Error from firehose: %v", errEvt.Message)
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

	return runWithReconnectLoop(ctx, c.logger, cfg, c.Run)
}

func runWithReconnectLoop(ctx context.Context, logger *log.Logger, cfg ReconnectConfig, runOnce func(context.Context) error) error {
	logger = logutil.Ensure(logger)

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
		logger.Printf("event=firehose_reconnect_retry backoff=%s err=%v", sleepFor, err)
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
func ParseCommit(ctx context.Context, evt *atproto.SyncSubscribeRepos_Commit) (*repo.Repo, error) {
	if evt.Blocks == nil {
		return nil, fmt.Errorf("no blocks in commit")
	}

	rr, err := repo.ReadRepoFromCar(ctx, bytes.NewReader(evt.Blocks))
	if err != nil {
		return nil, fmt.Errorf("reading repo from car: %w", err)
	}

	return rr, nil
}

// ProcessOps validates that create/update operations can be decoded from the CAR.
func ProcessOps(ctx context.Context, rr *repo.Repo, evt *atproto.SyncSubscribeRepos_Commit) error {
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
