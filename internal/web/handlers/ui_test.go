package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
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

func TestMessagesPageLinksToDetailView(t *testing.T) {
	database := openTestDB(t)
	defer database.Close()

	ctx := context.Background()
	messageURI := "at://did:plc:alice/app.bsky.feed.post/detail-link"
	if err := database.AddMessage(ctx, db.Message{
		ATURI:        messageURI,
		ATCID:        "bafy-link",
		ATDID:        "did:plc:alice",
		Type:         mapper.RecordTypePost,
		MessageState: db.MessageStatePublished,
		RawATJson:    `{"text":"hello link"}`,
		RawSSBJson:   `{"type":"post","text":"hello link"}`,
	}); err != nil {
		t.Fatalf("seed message: %v", err)
	}

	body := fetchUI(t, database, "/messages")
	expected := "/messages/detail?at_uri=" + url.QueryEscape(messageURI)
	if !strings.Contains(body, expected) {
		t.Fatalf("messages page missing detail link %q: %s", expected, body)
	}
}

func TestMessagesPageFiltersAndSortsResults(t *testing.T) {
	database := openTestDB(t)
	defer database.Close()

	ctx := context.Background()
	seedMessages := []db.Message{
		{
			ATURI:           "at://did:plc:alice/app.bsky.feed.post/alpha",
			ATCID:           "bafy-alpha",
			ATDID:           "did:plc:alice",
			Type:            mapper.RecordTypePost,
			MessageState:    db.MessageStateDeferred,
			RawATJson:       `{"text":"alpha"}`,
			RawSSBJson:      `{"type":"post","text":"alpha"}`,
			DeferReason:     "missing reply root",
			PublishAttempts: 1,
			DeferAttempts:   3,
		},
		{
			ATURI:           "at://did:plc:bob/app.bsky.graph.follow/gamma",
			ATCID:           "bafy-gamma",
			ATDID:           "did:plc:bob",
			Type:            mapper.RecordTypeFollow,
			MessageState:    db.MessageStatePublished,
			RawATJson:       `{"subject":"did:plc:alice"}`,
			RawSSBJson:      `{"type":"contact","following":true}`,
			SSBMsgRef:       "%gamma.sha256",
			PublishAttempts: 1,
		},
		{
			ATURI:           "at://did:plc:alice/app.bsky.feed.post/beta",
			ATCID:           "bafy-beta",
			ATDID:           "did:plc:alice",
			Type:            mapper.RecordTypePost,
			MessageState:    db.MessageStateDeferred,
			RawATJson:       `{"text":"beta"}`,
			RawSSBJson:      `{"type":"post","text":"beta"}`,
			DeferReason:     "missing parent",
			PublishAttempts: 0,
			DeferAttempts:   1,
		},
	}
	for _, message := range seedMessages {
		if err := database.AddMessage(ctx, message); err != nil {
			t.Fatalf("seed message %s: %v", message.ATURI, err)
		}
	}

	body := fetchUI(t, database, "/messages?q=did:plc:alice&type=app.bsky.feed.post&state=deferred&sort=attempts_desc&limit=50")
	for _, expected := range []string{
		"Filter and paginate bridged records",
		"Page Size",
		"value=\"did:plc:alice\"",
		"value=\"50\" selected",
		"value=\"app.bsky.feed.post\" selected",
		"value=\"deferred\" selected",
		"value=\"attempts_desc\" selected",
		"at://did:plc:alice/app.bsky.feed.post/alpha",
		"at://did:plc:alice/app.bsky.feed.post/beta",
		"missing reply root",
		"missing parent",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("messages page missing %q: %s", expected, body)
		}
	}
	if strings.Contains(body, "at://did:plc:bob/app.bsky.graph.follow/gamma") {
		t.Fatalf("messages page should filter out non-matching records: %s", body)
	}

	if strings.Index(body, "at://did:plc:alice/app.bsky.feed.post/alpha") > strings.Index(body, "at://did:plc:alice/app.bsky.feed.post/beta") {
		t.Fatalf("messages page should sort higher-attempt rows first: %s", body)
	}
}

func TestMessagesPageSummarizesLongDeferredIssues(t *testing.T) {
	database := openTestDB(t)
	defer database.Close()

	ctx := context.Background()
	if err := database.AddMessage(ctx, db.Message{
		ATURI:        "at://did:plc:alice/app.bsky.feed.post/reply-wait",
		ATCID:        "bafy-reply-wait",
		ATDID:        "did:plc:alice",
		Type:         mapper.RecordTypePost,
		MessageState: db.MessageStateDeferred,
		RawATJson:    `{"text":"reply wait"}`,
		RawSSBJson:   `{"type":"post","text":"reply wait"}`,
		DeferReason:  "_atproto_reply_root=at://did:plc:bob/app.bsky.feed.post/root;_atproto_reply_parent=at://did:plc:bob/app.bsky.feed.post/parent",
	}); err != nil {
		t.Fatalf("seed deferred reply message: %v", err)
	}

	body := fetchUI(t, database, "/messages")
	if !strings.Contains(body, "Waiting on reply target bridge") {
		t.Fatalf("messages page should summarize long deferred reply issues: %s", body)
	}
	if !strings.Contains(body, "Show full issue") {
		t.Fatalf("messages page should provide expand/collapse issue details: %s", body)
	}
	if !strings.Contains(body, "_atproto_reply_root=at://did:plc:bob/app.bsky.feed.post/root;_atproto_reply_parent=") {
		t.Fatalf("messages page should keep full deferred reason available in expanded details: %s", body)
	}
}

func TestMessageDetailRendersStructuredAndRawPayloads(t *testing.T) {
	database := openTestDB(t)
	defer database.Close()

	ctx := context.Background()
	messageURI := "at://did:plc:alice/app.bsky.feed.post/detail"
	if err := database.AddMessage(ctx, db.Message{
		ATURI:           messageURI,
		ATCID:           "bafy-detail",
		ATDID:           "did:plc:alice",
		Type:            mapper.RecordTypePost,
		MessageState:    db.MessageStateDeferred,
		RawATJson:       `{"text":"original hello","reply":{"root":{"uri":"at://did:plc:bob/app.bsky.feed.post/root"},"parent":{"uri":"at://did:plc:bob/app.bsky.feed.post/parent"}}}`,
		RawSSBJson:      `{"type":"post","text":"bridged hello","_atproto_reply_parent":"at://did:plc:bob/app.bsky.feed.post/parent"}`,
		DeferReason:     "_atproto_reply_parent=at://did:plc:bob/app.bsky.feed.post/parent",
		PublishAttempts: 2,
	}); err != nil {
		t.Fatalf("seed detail message: %v", err)
	}

	path := "/messages/detail?at_uri=" + url.QueryEscape(messageURI)
	body := fetchUI(t, database, path)
	for _, expected := range []string{
		"Message Detail",
		"Original ATProto Message",
		"Bridged SSB Message",
		"Raw ATProto JSON",
		"Raw SSB JSON",
		"original hello",
		"bridged hello",
		"&#34;text&#34;: &#34;original hello&#34;",
		"&#34;text&#34;: &#34;bridged hello&#34;",
		"_atproto_reply_parent=at://did:plc:bob/app.bsky.feed.post/parent",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("message detail missing %q: %s", expected, body)
		}
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
