package firehose

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net/http"

	"github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/events"
	"github.com/bluesky-social/indigo/events/schedulers/sequential"
	"github.com/bluesky-social/indigo/repo"
	"github.com/gorilla/websocket"
)

type EventHandler interface {
	HandleCommit(ctx context.Context, evt *atproto.SyncSubscribeRepos_Commit) error
}

type Client struct {
	relayURL string
	handler  EventHandler
	logger   *log.Logger
	dialer   *websocket.Dialer
}

func NewClient(relayURL string, handler EventHandler, logger *log.Logger) *Client {
	if relayURL == "" {
		relayURL = "wss://bsky.network/xrpc/com.atproto.sync.subscribeRepos"
	}
	return &Client{
		relayURL: relayURL,
		handler:  handler,
		logger:   logger,
		dialer:   websocket.DefaultDialer,
	}
}

func (c *Client) Run(ctx context.Context) error {
	c.logger.Printf("Connecting to ATProto firehose at %s", c.relayURL)

	con, _, err := c.dialer.DialContext(ctx, c.relayURL, http.Header{})
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

// ParseCommit is a helper to parse the CAR blocks inside a commit
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

// ProcessOps processes the ops in a commit and returns parsed records
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
