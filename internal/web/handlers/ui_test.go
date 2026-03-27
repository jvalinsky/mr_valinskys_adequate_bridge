package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/mr_valinskys_adequate_bridge/internal/db"
	"github.com/mr_valinskys_adequate_bridge/internal/mapper"
)

func TestDashboardRendersRuntimeStateFromBridgeState(t *testing.T) {
	database := openTestDB(t)
	defer database.Close()

	ctx := context.Background()
	if err := database.SetBridgeState(ctx, "bridge_runtime_status", "live"); err != nil {
		t.Fatalf("set bridge_runtime_status: %v", err)
	}
	if err := database.SetBridgeState(ctx, "bridge_runtime_last_heartbeat_at", "2026-03-27T09:00:00Z"); err != nil {
		t.Fatalf("set bridge_runtime_last_heartbeat_at: %v", err)
	}
	if err := database.SetBridgeState(ctx, "firehose_seq", "777"); err != nil {
		t.Fatalf("set firehose_seq: %v", err)
	}

	body := fetchUI(t, database, "/")
	if !strings.Contains(body, "Bridge Status") || !strings.Contains(body, "live") {
		t.Fatalf("dashboard missing bridge runtime status: %s", body)
	}
	if !strings.Contains(body, "Last Heartbeat") || !strings.Contains(body, "2026-03-27T09:00:00Z") {
		t.Fatalf("dashboard missing heartbeat state: %s", body)
	}
}

func TestDashboardDefaultsRuntimeStatusToUnknown(t *testing.T) {
	database := openTestDB(t)
	defer database.Close()

	body := fetchUI(t, database, "/")
	if !strings.Contains(body, "Bridge Status") || !strings.Contains(body, "unknown") {
		t.Fatalf("dashboard should render unknown runtime status when unset: %s", body)
	}
}

func TestFailuresAndStatePagesRender(t *testing.T) {
	database := openTestDB(t)
	defer database.Close()

	ctx := context.Background()
	if err := database.AddMessage(ctx, db.Message{
		ATURI:           "at://did:plc:alice/app.bsky.feed.post/failure",
		ATCID:           "bafy-failure",
		ATDID:           "did:plc:alice",
		Type:            mapper.RecordTypePost,
		MessageState:    db.MessageStateFailed,
		RawATJson:       `{"text":"oops"}`,
		RawSSBJson:      `{"type":"post","text":"oops"}`,
		PublishError:    "forced publish failure",
		PublishAttempts: 2,
	}); err != nil {
		t.Fatalf("seed failure message: %v", err)
	}
	if err := database.AddMessage(ctx, db.Message{
		ATURI:              "at://did:plc:alice/app.bsky.graph.follow/deferred",
		ATCID:              "bafy-follow-deferred",
		ATDID:              "did:plc:alice",
		Type:               mapper.RecordTypeFollow,
		MessageState:       db.MessageStateDeferred,
		RawATJson:          `{"subject":"did:plc:bob"}`,
		RawSSBJson:         `{"type":"contact","following":true,"_atproto_contact":"did:plc:bob"}`,
		DeferReason:        "_atproto_contact=did:plc:bob",
		DeferAttempts:      1,
		PublishAttempts:    0,
		LastDeferAttemptAt: nil,
	}); err != nil {
		t.Fatalf("seed deferred message: %v", err)
	}
	if err := database.SetBridgeState(ctx, "bridge_runtime_status", "stopped"); err != nil {
		t.Fatalf("set bridge_runtime_status: %v", err)
	}

	failuresBody := fetchUI(t, database, "/failures")
	if !strings.Contains(failuresBody, "forced publish failure") {
		t.Fatalf("failures page missing publish failure reason: %s", failuresBody)
	}
	if !strings.Contains(failuresBody, "_atproto_contact=did:plc:bob") {
		t.Fatalf("failures page missing deferred reason: %s", failuresBody)
	}

	stateBody := fetchUI(t, database, "/state")
	if !strings.Contains(stateBody, "bridge_runtime_status") || !strings.Contains(stateBody, "stopped") {
		t.Fatalf("state page missing runtime status key/value: %s", stateBody)
	}
}

func openTestDB(t *testing.T) *db.DB {
	t.Helper()

	database, err := db.Open(filepath.Join(t.TempDir(), "bridge.sqlite"))
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	return database
}

func fetchUI(t *testing.T, database *db.DB, path string) string {
	t.Helper()

	router := chi.NewRouter()
	NewUIHandler(database).Mount(router)

	req := httptest.NewRequest(http.MethodGet, path, nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	resp := rr.Result()
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("request %s expected status 200 got %d", path, resp.StatusCode)
	}
	return rr.Body.String()
}
