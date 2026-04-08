package smoke

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/bots"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/bridge"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/db"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/mapper"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/publishqueue"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/room"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssbruntime"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/web/handlers"
	websecurity "github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/web/security"
)

type mockSSBStatus struct{}

func (m *mockSSBStatus) GetPeers() []handlers.PeerStatus          { return nil }
func (m *mockSSBStatus) GetEBTState() map[string]map[string]int64 { return nil }
func (m *mockSSBStatus) ConnectPeer(ctx context.Context, addr string, pubKey []byte) error {
	return nil
}

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

	ssbRuntime, err := ssbruntime.Open(ctx, ssbruntime.Config{
		RepoPath:   filepath.Join(tmpDir, "ssb-repo"),
		MasterSeed: []byte(seed),
		GossipDB:   database,
	}, log.New(io.Discard, "", 0))
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
			collection: mapper.RecordTypeFollow,
			atURI:      "at://did:plc:smoketest/app.bsky.graph.follow/1",
			atCID:      "bafy-smoke-follow",
			payload: `{
				"subject": "did:plc:smoke-follow-target",
				"createdAt": "2026-01-01T00:00:02Z"
			}`,
		},
		{
			collection: mapper.RecordTypeBlock,
			atURI:      "at://did:plc:smoketest/app.bsky.graph.block/1",
			atCID:      "bafy-smoke-block",
			payload: `{
				"subject": "did:plc:smoke-follow-target",
				"createdAt": "2026-01-01T00:00:03Z"
			}`,
		},
		{
			collection: mapper.RecordTypeProfile,
			atURI:      "at://did:plc:smoketest/app.bsky.actor.profile/self",
			atCID:      "bafy-smoke-profile",
			payload: `{
				"displayName":"Smoke Tester",
				"description":"bridge smoke",
				"createdAt":"2026-01-01T00:00:04Z"
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

	postMsg, err := database.GetMessage(ctx, postURI)
	if err != nil {
		t.Fatalf("get post message: %v", err)
	}
	if !strings.Contains(postMsg.RawSSBJson, `"type":"post"`) {
		t.Fatalf("post payload missing native type: %s", postMsg.RawSSBJson)
	}

	profileMsg, err := database.GetMessage(ctx, "at://did:plc:smoketest/app.bsky.actor.profile/self")
	if err != nil {
		t.Fatalf("get profile message: %v", err)
	}
	if !strings.Contains(profileMsg.RawSSBJson, `"type":"about"`) || !strings.Contains(profileMsg.RawSSBJson, `"about":"`) {
		t.Fatalf("profile payload missing about shape: %s", profileMsg.RawSSBJson)
	}

	if err := database.SetBridgeState(ctx, "firehose_seq", "9001"); err != nil {
		t.Fatalf("set legacy firehose cursor: %v", err)
	}
	if err := database.SetBridgeState(ctx, "atproto_event_cursor", "9002"); err != nil {
		t.Fatalf("set bridge replay cursor: %v", err)
	}
	if err := database.UpsertATProtoSource(ctx, db.ATProtoSource{
		SourceKey: "default-relay",
		RelayURL:  "wss://relay.example.test",
		LastSeq:   9003,
	}); err != nil {
		t.Fatalf("set relay source cursor: %v", err)
	}
	if _, err := database.AppendATProtoEvent(ctx, db.ATProtoRecordEvent{
		DID:        didSource,
		Collection: mapper.RecordTypePost,
		RKey:       "cursor-smoke",
		ATURI:      "at://did:plc:smoketest/app.bsky.feed.post/cursor-smoke",
		ATCID:      "bafy-smoke-cursor",
		Action:     "upsert",
		Rev:        "rev-smoke",
		RecordJSON: `{"$type":"app.bsky.feed.post","text":"cursor smoke"}`,
	}); err != nil {
		t.Fatalf("append atproto event: %v", err)
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
	ui := handlers.NewUIHandler(database, log.New(io.Discard, "", 0), nil, nil, &mockSSBStatus{}, nil)
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
	if !strings.Contains(dashboard, "Bridge Replay Cursor") || !strings.Contains(dashboard, "9002") {
		t.Fatalf("dashboard missing bridge replay cursor value")
	}
	if !strings.Contains(dashboard, "Relay Source Cursor") || !strings.Contains(dashboard, "9003") {
		t.Fatalf("dashboard missing relay source cursor value")
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

func TestRoomHTTPSmoke(t *testing.T) {
	tmpDir := t.TempDir()
	database, err := db.Open(filepath.Join(tmpDir, "bridge.sqlite"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	feedRef, err := refs.NewFeedRef(bytes.Repeat([]byte{9}, 32), refs.RefAlgoFeedSSB1)
	if err != nil {
		t.Fatalf("create feed ref: %v", err)
	}
	if err := database.AddBridgedAccount(context.Background(), db.BridgedAccount{
		ATDID:     "did:plc:room-smoke-bot",
		SSBFeedID: feedRef.String(),
		Active:    true,
	}); err != nil {
		t.Fatalf("add bridged account: %v", err)
	}

	rt, err := room.Start(context.Background(), room.Config{
		ListenAddr:            "127.0.0.1:0",
		HTTPListenAddr:        "127.0.0.1:0",
		RepoPath:              filepath.Join(tmpDir, "room-repo"),
		Mode:                  "open",
		BridgeAccountLister:   database,
		BridgeAccountDetailer: database,
	}, log.New(io.Discard, "", 0))
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("sandbox does not allow local listen sockets: %v", err)
		}
		t.Fatalf("start room runtime: %v", err)
	}
	defer rt.Close()

	client := &http.Client{Timeout: 2 * time.Second}
	fetch := func(path string) string {
		resp, err := client.Get("http://" + rt.HTTPAddr() + path)
		if err != nil {
			t.Fatalf("request %s failed: %v", path, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("request %s expected 200 got %d\nbody:\n%s", path, resp.StatusCode, string(body))
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("read response %s: %v", path, err)
		}
		return string(body)
	}

	if body := fetch("/healthz"); strings.TrimSpace(body) != "ok" {
		t.Fatalf("healthz body mismatch: %q", body)
	}

	landing := fetch("/")
	for _, want := range []string{"Create invite", "Browse bridged bots"} {
		if !strings.Contains(landing, want) {
			t.Fatalf("landing page missing %q\nbody:\n%s", want, landing)
		}
	}

	botsPage := fetch("/bots")
	for _, want := range []string{"did:plc:room-smoke-bot"} {
		if !strings.Contains(botsPage, want) {
			t.Fatalf("bots directory missing %q\nbody:\n%s", want, botsPage)
		}
	}

	botDetail := fetch("/bots/did:plc:room-smoke-bot")
	for _, want := range []string{"did:plc:room-smoke-bot", feedRef.String(), (&refs.FeedURI{Ref: feedRef}).String()} {
		if !strings.Contains(botDetail, want) {
			t.Fatalf("bot detail page missing %q", want)
		}
	}

	req, err := http.NewRequest(http.MethodPost, "http://"+rt.HTTPAddr()+"/create-invite", nil)
	if err != nil {
		t.Fatalf("build invite request: %v", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("create invite request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("create invite expected 200 got %d\nbody:\n%s", resp.StatusCode, string(body))
	}

	var payload map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode invite response: %v", err)
	}
	if !strings.Contains(payload["url"], "/join?token=") {
		t.Fatalf("unexpected invite url: %q", payload["url"])
	}
}
