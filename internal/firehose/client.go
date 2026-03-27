// Package firehose streams ATProto repository commits from subscribeRepos.
package firehose

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"

	"github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/events"
	"github.com/bluesky-social/indigo/events/schedulers/sequential"
	"github.com/bluesky-social/indigo/repo"
	"github.com/gorilla/websocket"
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

	con, _, err := c.dialer.DialContext(ctx, streamURL, http.Header{})
	if err != nil {
		return fmt.Errorf("failed to dial: %w", err)
	}
	defer con.Close()

	c.logger.Println("Connected to firehose")

	callbacks := &events.RepoStreamCallbacks{
		RepoCommit: func(evt *atproto.SyncSubscribeRepos_Commit) error {
			if err := c.handler.HandleCommit(ctx, evt); err != nil {
				c.logger.Printf("Error handling commit: %v", err)
			}
			return nil
		},
		RepoInfo: func(info *atproto.SyncSubscribeRepos_Info) error {
			c.logger.Printf("RepoInfo: %v", info.Name)
			return nil
		},
		Error: func(errEvt *events.ErrorFrame) error {
			c.logger.Printf("Error from firehose: %v", errEvt.Message)
			return nil
		},
	}

	sched := sequential.NewScheduler("firehose", callbacks.EventHandler)
	return events.HandleRepoStream(ctx, con, sched, nil)
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
