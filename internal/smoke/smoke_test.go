package smoke

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/mr_valinskys_adequate_bridge/internal/bots"
	"github.com/mr_valinskys_adequate_bridge/internal/bridge"
	"github.com/mr_valinskys_adequate_bridge/internal/db"
	"github.com/mr_valinskys_adequate_bridge/internal/mapper"
	"github.com/mr_valinskys_adequate_bridge/internal/publishqueue"
	"github.com/mr_valinskys_adequate_bridge/internal/ssbruntime"
	"github.com/mr_valinskys_adequate_bridge/internal/web/handlers"
	websecurity "github.com/mr_valinskys_adequate_bridge/internal/web/security"
)

func TestBridgeSmoke(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	database, err := db.Open(filepath.Join(tmpDir, "bridge.sqlite"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	const (
		didSource = "did:plc:smoketest"
		didTarget = "did:plc:smoke-follow-target"
		seed      = "smoke-seed-001"
	)

	manager := bots.NewManager([]byte(seed), nil, nil, nil)
	for _, did := range []string{didSource, didTarget} {
		feedRef, err := manager.GetFeedID(did)
		if err != nil {
			t.Fatalf("derive feed id for %s: %v", did, err)
		}

		if err := database.AddBridgedAccount(ctx, db.BridgedAccount{
			ATDID:     did,
			SSBFeedID: feedRef.Ref(),
			Active:    true,
		}); err != nil {
			t.Fatalf("add bridged account %s: %v", did, err)
		}
	}

	ssbRuntime, err := ssbruntime.Open(filepath.Join(tmpDir, "ssb-repo"), []byte(seed), nil, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("open ssb runtime: %v", err)
	}
	defer ssbRuntime.Close()

	workerPublisher := publishqueue.New(ssbRuntime, 2, log.New(io.Discard, "", 0))
	defer workerPublisher.Close()

	processor := bridge.NewProcessor(
		database,
		log.New(io.Discard, "", 0),
		bridge.WithPublisher(workerPublisher),
	)

	postURI := "at://did:plc:smoketest/app.bsky.feed.post/1"
	fixtures := []struct {
		collection string
		atURI      string
		atCID      string
		payload    string
	}{
		{
			collection: mapper.RecordTypePost,
			atURI:      postURI,
			atCID:      "bafy-smoke-post",
			payload:    `{"text":"hello smoke","createdAt":"2026-01-01T00:00:00Z"}`,
		},
		{
			collection: mapper.RecordTypeLike,
			atURI:      "at://did:plc:smoketest/app.bsky.feed.like/1",
			atCID:      "bafy-smoke-like",
			payload: `{
				"subject": {
					"uri": "at://did:plc:smoketest/app.bsky.feed.post/1",
					"cid": "bafy-smoke-post"
				},
				"createdAt": "2026-01-01T00:00:01Z"
			}`,
		},
		{
			collection: mapper.RecordTypeRepost,
			atURI:      "at://did:plc:smoketest/app.bsky.feed.repost/1",
			atCID:      "bafy-smoke-repost",
			payload: `{
				"subject": {
					"uri": "at://did:plc:smoketest/app.bsky.feed.post/1",
					"cid": "bafy-smoke-post"
				},
				"createdAt": "2026-01-01T00:00:02Z"
			}`,
		},
		{
			collection: mapper.RecordTypeFollow,
			atURI:      "at://did:plc:smoketest/app.bsky.graph.follow/1",
			atCID:      "bafy-smoke-follow",
			payload: `{
				"subject": "did:plc:smoke-follow-target",
				"createdAt": "2026-01-01T00:00:03Z"
			}`,
		},
	}

	for _, f := range fixtures {
		if err := processor.ProcessRecord(ctx, didSource, f.atURI, f.atCID, f.collection, []byte(f.payload)); err != nil {
			t.Fatalf("process fixture %s: %v", f.collection, err)
		}
	}

	for _, f := range fixtures {
		msg, err := database.GetMessage(ctx, f.atURI)
		if err != nil {
			t.Fatalf("get message %s: %v", f.atURI, err)
		}
		if msg == nil {
			t.Fatalf("expected message for %s", f.atURI)
		}
		if strings.TrimSpace(msg.SSBMsgRef) == "" {
			t.Fatalf("expected published ssb ref for %s", f.atURI)
		}
	}

	if err := database.SetBridgeState(ctx, "firehose_seq", "9001"); err != nil {
		t.Fatalf("set firehose cursor: %v", err)
	}

	if err := database.AddMessage(ctx, db.Message{
		ATURI:           "at://did:plc:smoketest/app.bsky.feed.post/fail",
		ATCID:           "bafy-smoke-fail",
		ATDID:           didSource,
		Type:            mapper.RecordTypePost,
		MessageState:    db.MessageStateFailed,
		RawATJson:       `{"text":"forced fail"}`,
		RawSSBJson:      `{"type":"post","text":"forced fail"}`,
		PublishError:    "forced failure for smoke visibility",
		PublishAttempts: 1,
	}); err != nil {
		t.Fatalf("seed failure row: %v", err)
	}

	router := chi.NewRouter()
	router.Use(websecurity.BasicAuthMiddleware("admin", "smoke-pass"))
	ui := handlers.NewUIHandler(database)
	ui.Mount(router)

	fetch := func(path string) string {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.RemoteAddr = "127.0.0.1:12345"
		req.Header.Set("User-Agent", "smoke-test")
		req.SetBasicAuth("admin", "smoke-pass")
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		resp := rr.Result()
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("request %s expected 200 got %d", path, resp.StatusCode)
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("read response %s: %v", path, err)
		}
		return string(body)
	}

	dashboard := fetch("/")
	if !strings.Contains(dashboard, "Messages Published") {
		t.Fatalf("dashboard missing published stat")
	}
	if !strings.Contains(dashboard, "Publish Failures") {
		t.Fatalf("dashboard missing failure stat")
	}
	if !strings.Contains(dashboard, "9001") {
		t.Fatalf("dashboard missing firehose cursor value")
	}

	failures := fetch("/failures")
	if !strings.Contains(failures, "forced failure for smoke visibility") {
		t.Fatalf("failures page missing seeded failure")
	}

	state := fetch("/state")
	if !strings.Contains(state, "firehose_seq") || !strings.Contains(state, "9001") {
		t.Fatalf("state page missing firehose cursor row")
	}
}
