package handlers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/db"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/mapper"
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

func TestDashboardWithData(t *testing.T) {
	database := openTestDB(t)
	defer database.Close()

	ctx := context.Background()
	// Seed deferred reasons.
	if err := database.AddMessage(ctx, db.Message{
		ATURI: "at://did:plc:x/app.bsky.feed.post/1", ATCID: "c1", ATDID: "did:plc:x",
		Type: mapper.RecordTypePost, MessageState: db.MessageStateDeferred, DeferReason: "reason1",
	}); err != nil {
		t.Fatal(err)
	}
	// Seed issue accounts.
	if err := database.AddBridgedAccount(ctx, db.BridgedAccount{
		ATDID: "did:plc:y", SSBFeedID: "@y.ed25519", Active: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := database.AddMessage(ctx, db.Message{
		ATURI: "at://did:plc:y/app.bsky.feed.post/2", ATCID: "c2", ATDID: "did:plc:y",
		Type: mapper.RecordTypePost, MessageState: db.MessageStateFailed, PublishError: "boom",
	}); err != nil {
		t.Fatal(err)
	}

	body := fetchUI(t, database, "/")
	if !strings.Contains(body, "reason1") {
		t.Fatalf("dashboard missing deferred reason: %s", body)
	}
	if !strings.Contains(body, "did:plc:y") {
		t.Fatalf("dashboard missing issue account: %s", body)
	}
}

func TestAccountsPageWithData(t *testing.T) {
	database := openTestDB(t)
	defer database.Close()

	ctx := context.Background()
	if err := database.AddBridgedAccount(ctx, db.BridgedAccount{
		ATDID: "did:plc:z", SSBFeedID: "@z.ed25519", Active: true,
	}); err != nil {
		t.Fatal(err)
	}

	body := fetchUI(t, database, "/accounts")
	if !strings.Contains(body, "did:plc:z") {
		t.Fatalf("accounts page missing account: %s", body)
	}
}

func TestBlobsPageWithData(t *testing.T) {
	database := openTestDB(t)
	defer database.Close()

	ctx := context.Background()
	if err := database.AddBlob(ctx, db.Blob{
		ATCID: "cid1", SSBBlobRef: "&blob1", Size: 100, MimeType: "image/png",
	}); err != nil {
		t.Fatal(err)
	}

	body := fetchUI(t, database, "/blobs")
	if !strings.Contains(body, "cid1") || !strings.Contains(body, "&amp;blob1") {
		t.Fatalf("blobs page missing blob: %s", body)
	}
}

func TestMessagesPageWithData(t *testing.T) {
	database := openTestDB(t)
	defer database.Close()

	ctx := context.Background()
	if err := database.AddMessage(ctx, db.Message{
		ATURI: "at://did:plc:x/app.bsky.feed.post/1", ATCID: "c1", ATDID: "did:plc:x",
		Type: mapper.RecordTypePost, MessageState: db.MessageStatePublished,
	}); err != nil {
		t.Fatal(err)
	}

	body := fetchUI(t, database, "/messages")
	if !strings.Contains(body, "at://did:plc:x/app.bsky.feed.post/1") {
		t.Fatalf("messages page missing message: %s", body)
	}
}

func TestStatePageWithData(t *testing.T) {
	database := openTestDB(t)
	defer database.Close()

	ctx := context.Background()
	if err := database.SetBridgeState(ctx, "k1", "v1"); err != nil {
		t.Fatal(err)
	}

	body := fetchUI(t, database, "/state")
	if !strings.Contains(body, "k1") || !strings.Contains(body, "v1") {
		t.Fatalf("state page missing key/value: %s", body)
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

func TestHealthzReturns200WhenLive(t *testing.T) {
	database := openTestDB(t)
	defer database.Close()

	ctx := context.Background()
	if err := database.SetBridgeState(ctx, "bridge_runtime_status", "live"); err != nil {
		t.Fatal(err)
	}
	if err := database.SetBridgeState(ctx, "bridge_runtime_last_heartbeat_at", time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}

	router := chi.NewRouter()
	NewUIHandler(database, nil).Mount(router)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if body := rr.Body.String(); body != "ok" {
		t.Fatalf("expected body 'ok', got %q", body)
	}
}

func TestHealthzReturns503WhenNotLive(t *testing.T) {
	database := openTestDB(t)
	defer database.Close()

	// No bridge state set — status is empty.
	router := chi.NewRouter()
	NewUIHandler(database, nil).Mount(router)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestHealthzReturns503WhenHeartbeatStale(t *testing.T) {
	database := openTestDB(t)
	defer database.Close()

	ctx := context.Background()
	if err := database.SetBridgeState(ctx, "bridge_runtime_status", "live"); err != nil {
		t.Fatal(err)
	}
	staleTime := time.Now().Add(-5 * time.Minute).UTC().Format(time.RFC3339)
	if err := database.SetBridgeState(ctx, "bridge_runtime_last_heartbeat_at", staleTime); err != nil {
		t.Fatal(err)
	}

	router := chi.NewRouter()
	NewUIHandler(database, nil).Mount(router)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 for stale heartbeat, got %d: %s", rr.Code, rr.Body.String())
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
	NewUIHandler(database, nil).Mount(router)

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

func TestAccountsPageRenders(t *testing.T) {
	database := openTestDB(t)
	defer database.Close()

	body := fetchUI(t, database, "/accounts")
	if !strings.Contains(body, "Accounts") {
		t.Fatalf("accounts page should render: %s", body)
	}
}

func TestBlobsPageRenders(t *testing.T) {
	database := openTestDB(t)
	defer database.Close()

	body := fetchUI(t, database, "/blobs")
	if !strings.Contains(body, "Blobs") {
		t.Fatalf("blobs page should render: %s", body)
	}
}

func TestMessagesPageHandlesAllStates(t *testing.T) {
	database := openTestDB(t)
	defer database.Close()

	body := fetchUI(t, database, "/messages?state=pending")
	if !strings.Contains(body, "Filter and paginate") {
		t.Fatalf("messages page should handle pending state: %s", body)
	}

	body = fetchUI(t, database, "/messages?state=published")
	if !strings.Contains(body, "Filter and paginate") {
		t.Fatalf("messages page should handle published state: %s", body)
	}
}

func TestMessagesPageHandlesAllSorts(t *testing.T) {
	database := openTestDB(t)
	defer database.Close()

	sorts := []string{"created_at_asc", "created_at_desc", "type_asc", "type_desc"}
	for _, sort := range sorts {
		body := fetchUI(t, database, "/messages?sort="+sort)
		if !strings.Contains(body, "Filter and paginate") {
			t.Fatalf("messages page should handle sort %s: %s", sort, body)
		}
	}
}

func TestMessagesPageHandlesInvalidParams(t *testing.T) {
	database := openTestDB(t)
	defer database.Close()

	body := fetchUI(t, database, "/messages?limit=999")
	if !strings.Contains(body, "Filter and paginate") {
		t.Fatalf("messages page should handle large limit: %s", body)
	}

	body = fetchUI(t, database, "/messages?limit=0")
	if !strings.Contains(body, "Filter and paginate") {
		t.Fatalf("messages page should handle zero limit: %s", body)
	}
}

func TestMessagesPageHandlesBadLimit(t *testing.T) {
	database := openTestDB(t)
	defer database.Close()

	router := chi.NewRouter()
	NewUIHandler(database, nil).Mount(router)
	req := httptest.NewRequest(http.MethodGet, "/messages?limit=abc", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for bad limit (uses default), got %d", rr.Code)
	}
}

func TestMessageDetailHandlesUnknownURI(t *testing.T) {
	database := openTestDB(t)
	defer database.Close()

	router := chi.NewRouter()
	NewUIHandler(database, nil).Mount(router)
	req := httptest.NewRequest(http.MethodGet, "/messages/detail?at_uri=at://unknown", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown URI, got %d", rr.Code)
	}
}

func TestMessagesPageHandlesEmptyResults(t *testing.T) {
	database := openTestDB(t)
	defer database.Close()

	body := fetchUI(t, database, "/messages?q=nonexistent")
	if !strings.Contains(body, "Filter and paginate") {
		t.Fatalf("messages page should render with no results: %s", body)
	}
}

func TestTruncateMiddle(t *testing.T) {
	tests := []struct {
		input string
		max   int
		want  string
	}{
		{"hello", 10, "hello"},
		{"hello", 5, "hello"},
		{"hello", 3, "hel"},
		{"hello", 2, "he"},
		{"hello", 1, "h"},
		{"hello", 0, "hello"},
		{"hello", -1, "hello"},
		{"hello world", 8, "hel…orld"},
		{"ab", 8, "ab"},
		{"abcdefgh", 7, "abcdefg"},
		{"", 5, ""},
		{"   ", 5, ""},
	}
	for _, tt := range tests {
		got := truncateMiddle(tt.input, tt.max)
		if got != tt.want {
			t.Errorf("truncateMiddle(%q, %d) = %q, want %q", tt.input, tt.max, got, tt.want)
		}
	}
}

func TestHumanizeDuration(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{0, "0s"},
		{500 * time.Millisecond, "0s"},
		{30 * time.Second, "30s"},
		{59 * time.Second, "59s"},
		{1 * time.Minute, "1m"},
		{90 * time.Second, "1m"},
		{5 * time.Minute, "5m"},
		{59 * time.Minute, "59m"},
		{1 * time.Hour, "1h"},
		{2 * time.Hour, "2h"},
		{25 * time.Hour, "25h"},
		{-5 * time.Second, "0s"},
	}
	for _, tt := range tests {
		got := humanizeDuration(tt.d)
		if got != tt.want {
			t.Errorf("humanizeDuration(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}

func TestMessageStateClass(t *testing.T) {
	tests := []struct {
		state string
		want  string
	}{
		{"published", "state-published"},
		{"failed", "state-failed"},
		{"deferred", "state-deferred"},
		{"deleted", "state-deleted"},
		{"pending", "state-pending"},
		{"", "state-pending"},
		{"unknown", "state-pending"},
		{"  published  ", "state-published"},
	}
	for _, tt := range tests {
		got := messageStateClass(tt.state)
		if got != tt.want {
			t.Errorf("messageStateClass(%q) = %q, want %q", tt.state, got, tt.want)
		}
	}
}

func TestMessageIssueSummary(t *testing.T) {
	tests := []struct {
		name        string
		message     db.Message
		wantSummary string
		wantClass   string
	}{
		{
			name:        "failed with error",
			message:     db.Message{MessageState: db.MessageStateFailed, PublishError: "connection refused"},
			wantSummary: "connection refused",
			wantClass:   "",
		},
		{
			name:        "failed with empty error",
			message:     db.Message{MessageState: db.MessageStateFailed},
			wantSummary: "No issue",
			wantClass:   "muted",
		},
		{
			name:        "deferred with reason",
			message:     db.Message{MessageState: db.MessageStateDeferred, DeferReason: "_atproto_reply_root=at://..."},
			wantSummary: "Waiting on reply target bridge",
			wantClass:   "warning",
		},
		{
			name:        "deferred with empty reason",
			message:     db.Message{MessageState: db.MessageStateDeferred},
			wantSummary: "No issue",
			wantClass:   "muted",
		},
		{
			name:        "deleted with reason",
			message:     db.Message{MessageState: db.MessageStateDeleted, DeletedReason: "atproto_delete"},
			wantSummary: "atproto_delete",
			wantClass:   "muted",
		},
		{
			name:        "deleted with empty reason",
			message:     db.Message{MessageState: db.MessageStateDeleted},
			wantSummary: "No issue",
			wantClass:   "muted",
		},
		{
			name:        "fallback to publish error",
			message:     db.Message{MessageState: db.MessageStatePublished, PublishError: "some error"},
			wantSummary: "some error",
			wantClass:   "",
		},
		{
			name:        "fallback to defer reason",
			message:     db.Message{MessageState: db.MessageStatePublished, DeferReason: "_atproto_contact=did:plc:bob"},
			wantSummary: "Waiting on contact bridge",
			wantClass:   "warning",
		},
		{
			name:        "no issues",
			message:     db.Message{MessageState: db.MessageStatePublished},
			wantSummary: "No issue",
			wantClass:   "muted",
		},
		{
			name:        "pending_with_delete",
			message:     db.Message{MessageState: db.MessageStatePending, DeletedReason: "del"},
			wantSummary: "del",
			wantClass:   "muted",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			summary, cls := messageIssueSummary(tt.message)
			if summary != tt.wantSummary {
				t.Errorf("summary = %q, want %q", summary, tt.wantSummary)
			}
			if cls != tt.wantClass {
				t.Errorf("class = %q, want %q", cls, tt.wantClass)
			}
		})
	}
}

func TestSummarizeDeferredIssue(t *testing.T) {
	tests := []struct {
		reason string
		want   string
	}{
		{"", "Deferred"},
		{"  ", "Deferred"},
		{"_atproto_reply_root=at://did:plc:a/app.bsky.feed.post/1", "Waiting on reply target bridge"},
		{"_atproto_reply_parent=at://did:plc:a/app.bsky.feed.post/1", "Waiting on reply target bridge"},
		{"_atproto_contact=did:plc:bob", "Waiting on contact bridge"},
		{"_atproto_subject=at://did:plc:a/app.bsky.feed.post/1", "Waiting on subject bridge"},
		{"_atproto_quote_subject=at://did:plc:a/app.bsky.feed.post/1", "Waiting on quoted post bridge"},
		{"_atproto_about_did=did:plc:bob", "Waiting on author feed bridge"},
		{"random reason", "random reason"},
		{"_atproto_unknown=value", "_atproto_unknown=value"},
	}
	for _, tt := range tests {
		got := summarizeDeferredIssue(tt.reason)
		if got != tt.want {
			t.Errorf("summarizeDeferredIssue(%q) = %q, want %q", tt.reason, got, tt.want)
		}
	}
}

func TestCompactIssueText(t *testing.T) {
	tests := []struct {
		text string
		want string
	}{
		{"", "No issue"},
		{"  ", "No issue"},
		{"short", "short"},
		{strings.Repeat("a", 100), strings.Repeat("a", 85) + "..."},
		{"  multiple   spaces  here  ", "multiple spaces here"},
	}
	for _, tt := range tests {
		got := compactIssueText(tt.text)
		if got != tt.want {
			t.Errorf("compactIssueText(%q) = %q, want %q", tt.text, got, tt.want)
		}
	}
}

func TestRuntimeHealth(t *testing.T) {
	tests := []struct {
		name          string
		lastHeartbeat string
		wantLabel     string
	}{
		{"unknown when empty", "", "unknown"},
		{"unknown when invalid", "not-a-time", "unknown"},
		{"healthy recent", time.Now().Add(-30 * time.Second).Format(time.RFC3339), "healthy"},
		{"stale old", time.Now().Add(-2 * time.Minute).Format(time.RFC3339), "stale"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			label, _, _ := runtimeHealth(tt.lastHeartbeat)
			if label != tt.wantLabel {
				t.Errorf("label = %q, want %q", label, tt.wantLabel)
			}
		})
	}
}

func TestHeartbeatFreshness(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name          string
		lastHeartbeat string
		wantStale     bool
		wantLabel     string
	}{
		{"unknown when empty", "", true, "unknown"},
		{"unknown when invalid", "not-a-time", true, "unknown"},
		{"fresh", now.Add(-30 * time.Second).Format(time.RFC3339), false, "30s ago"},
		{"stale", now.Add(-2 * time.Minute).Format(time.RFC3339), true, "2m ago"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stale, label := heartbeatFreshness(tt.lastHeartbeat)
			if stale != tt.wantStale {
				t.Errorf("stale = %v, want %v", stale, tt.wantStale)
			}
			if label != tt.wantLabel {
				t.Errorf("label = %q, want %q", label, tt.wantLabel)
			}
		})
	}
}

func TestParseTimestampString(t *testing.T) {
	validTime := "2026-03-28T10:00:00Z"
	_, ok := parseTimestampString(validTime)
	if !ok {
		t.Error("expected valid timestamp to parse")
	}

	_, ok = parseTimestampString("")
	if ok {
		t.Error("expected empty string to fail")
	}

	_, ok = parseTimestampString("invalid")
	if ok {
		t.Error("expected invalid string to fail")
	}
}

func TestFormatTime(t *testing.T) {
	if formatTime(time.Time{}) != "" {
		t.Error("expected empty string for zero time")
	}

	t1 := time.Date(2026, 3, 28, 10, 0, 0, 0, time.UTC)
	got := formatTime(t1)
	if !strings.Contains(got, "2026-03-28") {
		t.Errorf("expected date in output, got %s", got)
	}
}

func TestFormatOptionalTime(t *testing.T) {
	if formatOptionalTime(nil) != "" {
		t.Error("expected empty string for nil")
	}

	if formatOptionalTime(new(time.Time)) != "" {
		t.Error("expected empty string for zero time")
	}

	t1 := time.Date(2026, 3, 28, 10, 0, 0, 0, time.UTC)
	got := formatOptionalTime(&t1)
	if !strings.Contains(got, "2026-03-28") {
		t.Errorf("expected date in output, got %s", got)
	}
}

func TestFormatOptionalSeq(t *testing.T) {
	if formatOptionalSeq(nil) != "" {
		t.Error("expected empty string for nil")
	}

	v := int64(42)
	if formatOptionalSeq(&v) != "42" {
		t.Error("expected 42")
	}
}

func TestBuildTypeOptionsEdgeCases(t *testing.T) {
	// Selected type not in list
	opts := buildTypeOptions([]string{"a.b.c"}, "x.y.z")
	found := false
	for _, o := range opts {
		if o.Value == "x.y.z" && o.Selected {
			found = true
		}
	}
	if !found {
		t.Error("expected selected type to be added to options")
	}

	// Empty record type in list
	opts = buildTypeOptions([]string{"", "  "}, "")
	if len(opts) != 1 {
		t.Errorf("expected only 'All types' option, got %d", len(opts))
	}
}

func TestMessageStateLabel(t *testing.T) {
	tests := []struct {
		state string
		want  string
	}{
		{"published", "Published"},
		{"failed", "Failed"},
		{"deferred", "Deferred"},
		{"deleted", "Deleted"},
		{"pending", "Pending"},
		{"unknown", "Unknown"},
	}
	for _, tt := range tests {
		got := messageStateLabel(tt.state)
		if got != tt.want {
			t.Errorf("messageStateLabel(%q) = %q, want %q", tt.state, got, tt.want)
		}
	}
}

func TestSanitizeMessageState(t *testing.T) {
	if sanitizeMessageState("") != "" {
		t.Error("expected empty for empty input")
	}
	if sanitizeMessageState("published") != "published" {
		t.Error("expected published")
	}
	if sanitizeMessageState("invalid") != "" {
		t.Error("expected empty for invalid")
	}
}

func TestSanitizeMessageDirection(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"prev", "prev"},
		{"asc", "next"},
		{"desc", "next"},
		{"", "next"},
		{"invalid", "next"},
	}
	for _, tt := range tests {
		got := sanitizeMessageDirection(tt.input)
		if got != tt.want {
			t.Errorf("sanitizeMessageDirection(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestParseBoolFlag(t *testing.T) {
	tests := []struct {
		val  string
		want bool
	}{
		{"true", true},
		{"1", true},
		{"yes", true},
		{"on", true},
		{"false", false},
		{"0", false},
		{"no", false},
		{"off", false},
		{"", false},
		{"other", false},
	}
	for _, tt := range tests {
		got := parseBoolFlag(tt.val)
		if got != tt.want {
			t.Errorf("parseBoolFlag(%q) = %v, want %v", tt.val, got, tt.want)
		}
	}
}

func TestBuildTypeOptions(t *testing.T) {
	types := []string{"app.bsky.feed.post", "app.bsky.graph.follow"}
	selected := "app.bsky.feed.post"
	got := buildTypeOptions(types, selected)

	if len(got) < 2 {
		t.Fatalf("expected at least 2 options, got %d", len(got))
	}
	foundSelected := false
	for _, opt := range got {
		if opt.Value == selected && opt.Selected {
			foundSelected = true
		}
	}
	if !foundSelected {
		t.Error("expected selected attribute for app.bsky.feed.post")
	}
}

func TestBuildActiveMessageFilters(t *testing.T) {
	tests := []struct {
		query db.MessageListQuery
		want  string
	}{
		{db.MessageListQuery{}, ""},
		{db.MessageListQuery{ATDID: "did:plc:alice"}, "did:plc:alice"},
		{db.MessageListQuery{State: "failed"}, "Failed"},
	}
	for _, tt := range tests {
		got := buildActiveMessageFilters(tt.query)
		if tt.want == "" {
			if len(got) != 0 {
				t.Errorf("expected empty for %+v, got %v", tt.query, got)
			}
		} else {
			found := false
			for _, f := range got {
				if strings.Contains(f.Label, tt.want) || strings.Contains(f.Value, tt.want) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("buildActiveMessageFilters(%+v) missing filter for %q, got %v", tt.query, tt.want, got)
			}
		}
	}
}

func TestBuildMessagePageURL(t *testing.T) {
	tests := []struct {
		name         string
		query        db.MessageListQuery
		cursor       string
		direction    string
		wantContains string
	}{
		{
			name:         "basic",
			query:        db.MessageListQuery{Limit: 25},
			cursor:       "",
			direction:    "next",
			wantContains: "limit=25",
		},
		{
			name:         "with DID",
			query:        db.MessageListQuery{ATDID: "did:plc:alice", Limit: 10},
			cursor:       "",
			direction:    "next",
			wantContains: "did=did%3Aplc%3Aalice",
		},
		{
			name:         "with state",
			query:        db.MessageListQuery{State: "failed", Limit: 10},
			cursor:       "",
			direction:    "next",
			wantContains: "state=failed",
		},
		{
			name:         "with cursor",
			query:        db.MessageListQuery{Limit: 10},
			cursor:       "abc123",
			direction:    "prev",
			wantContains: "cursor=abc123",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildMessagePageURL(tt.query, tt.cursor, tt.direction)
			if !strings.Contains(got, tt.wantContains) {
				t.Errorf("buildMessagePageURL missing %q, got %s", tt.wantContains, got)
			}
		})
	}
}

func TestBuildMessagePageURLWithSearch(t *testing.T) {
	query := db.MessageListQuery{
		Search: "hello",
		Limit:  20,
	}
	got := buildMessagePageURL(query, "", "next")
	if !strings.Contains(got, "q=hello") {
		t.Errorf("expected q=hello in URL, got %s", got)
	}
}

func TestBuildMessagePageURLWithSort(t *testing.T) {
	query := db.MessageListQuery{
		Sort:  "attempts_desc",
		Limit: 20,
	}
	got := buildMessagePageURL(query, "", "next")
	if !strings.Contains(got, "sort=attempts_desc") {
		t.Errorf("expected sort in URL, got %s", got)
	}
}

func TestBuildMessagePageURLWithHasIssue(t *testing.T) {
	query := db.MessageListQuery{
		HasIssue: true,
		Limit:    20,
	}
	got := buildMessagePageURL(query, "", "next")
	if !strings.Contains(got, "has_issue=1") {
		t.Errorf("expected has_issue=1 in URL, got %s", got)
	}
}

type mockDatabase struct {
	err error
}

func (m *mockDatabase) CheckBridgeHealth(ctx context.Context, maxStale time.Duration) (*db.BridgeHealthStatus, error) {
	return nil, m.err
}
func (m *mockDatabase) CountBridgedAccounts(ctx context.Context) (int, error)   { return 0, m.err }
func (m *mockDatabase) CountMessages(ctx context.Context) (int, error)          { return 0, m.err }
func (m *mockDatabase) CountPublishedMessages(ctx context.Context) (int, error) { return 0, m.err }
func (m *mockDatabase) CountPublishFailures(ctx context.Context) (int, error)   { return 0, m.err }
func (m *mockDatabase) CountDeferredMessages(ctx context.Context) (int, error)  { return 0, m.err }
func (m *mockDatabase) CountDeletedMessages(ctx context.Context) (int, error)   { return 0, m.err }
func (m *mockDatabase) CountBlobs(ctx context.Context) (int, error)             { return 0, m.err }
func (m *mockDatabase) GetBridgeState(ctx context.Context, key string) (string, bool, error) {
	return "", false, m.err
}
func (m *mockDatabase) ListTopDeferredReasons(ctx context.Context, limit int) ([]db.DeferredReasonCount, error) {
	return nil, m.err
}
func (m *mockDatabase) ListTopIssueAccounts(ctx context.Context, limit int) ([]db.AccountIssueSummary, error) {
	return nil, m.err
}
func (m *mockDatabase) ListBridgedAccountsWithStats(ctx context.Context) ([]db.BridgedAccountStats, error) {
	return nil, m.err
}
func (m *mockDatabase) ListMessagesPage(ctx context.Context, query db.MessageListQuery) (db.MessagePage, error) {
	return db.MessagePage{}, m.err
}
func (m *mockDatabase) ListMessageTypes(ctx context.Context) ([]string, error) { return nil, m.err }
func (m *mockDatabase) GetMessage(ctx context.Context, atURI string) (*db.Message, error) {
	return nil, m.err
}
func (m *mockDatabase) GetPublishFailures(ctx context.Context, limit int) ([]db.Message, error) {
	return nil, m.err
}
func (m *mockDatabase) GetRecentBlobs(ctx context.Context, limit int) ([]db.Blob, error) {
	return nil, m.err
}
func (m *mockDatabase) GetAllBridgeState(ctx context.Context) ([]db.BridgeState, error) {
	return nil, m.err
}
func (m *mockDatabase) GetLatestDeferredReason(ctx context.Context) (string, bool, error) {
	return "", false, m.err
}

func TestUIHandlerInternalErrors(t *testing.T) {
	m := &mockDatabase{err: fmt.Errorf("db boom")}
	h := NewUIHandler(m, nil)
	router := chi.NewRouter()
	h.Mount(router)

	paths := []string{
		"/",
		"/accounts",
		"/messages",
		"/messages/detail?at_uri=at://x",
		"/failures",
		"/blobs",
		"/state",
	}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, req)
			if rr.Code != http.StatusInternalServerError {
				t.Errorf("path %s: expected 500, got %d", path, rr.Code)
			}
		})
	}
}

func TestHealthzInternalError(t *testing.T) {
	m := &mockDatabase{err: fmt.Errorf("db boom")}
	h := NewUIHandler(m, nil)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rr := httptest.NewRecorder()
	h.handleHealthz(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rr.Code)
	}
}

func TestMessageDetailBadRequest(t *testing.T) {
	h := NewUIHandler(&mockDatabase{}, nil)
	req := httptest.NewRequest(http.MethodGet, "/messages/detail", nil) // missing at_uri
	rr := httptest.NewRecorder()
	h.handleMessageDetail(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestUIHandlerHandleMessageDetailNotFound(t *testing.T) {
	m := &mockDatabase{} // returns nil, nil for GetMessage
	h := NewUIHandler(m, nil)
	req := httptest.NewRequest(http.MethodGet, "/messages/detail?at_uri=at://none", nil)
	rr := httptest.NewRecorder()
	h.handleMessageDetail(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rr.Code)
	}
}

func TestUIHandlerHandleMessagesInvalidLimit(t *testing.T) {
	h := NewUIHandler(&mockDatabase{}, nil)
	req := httptest.NewRequest(http.MethodGet, "/messages?limit=abc", nil)
	rr := httptest.NewRecorder()
	h.handleMessages(rr, req)
	if rr.Code != http.StatusOK { // it defaults to defaultLimit
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

type granularMockDatabase struct {
	err error

	// Per-call overrides
	countBridgedAccountsErr         error
	countMessagesErr                error
	countPublishedMessagesErr       error
	countPublishFailuresErr         error
	countDeferredMessagesErr        error
	countDeletedMessagesErr         error
	countBlobsErr                   error
	getBridgeStateErrs              map[string]error
	listTopDeferredReasonsErr       error
	listTopIssueAccountsErr         error
	listBridgedAccountsWithStatsErr error
	listMessagesPageErr             error
	listMessageTypesErr             error
	getMessageErr                   error
	getPublishFailuresErr           error
	getRecentBlobsErr               error
	getAllBridgeStateErr            error
	getLatestDeferredReasonErr      error
}

func (m *granularMockDatabase) CheckBridgeHealth(ctx context.Context, maxStale time.Duration) (*db.BridgeHealthStatus, error) {
	return nil, m.err
}
func (m *granularMockDatabase) CountBridgedAccounts(ctx context.Context) (int, error) {
	if m.countBridgedAccountsErr != nil {
		return 0, m.countBridgedAccountsErr
	}
	return 0, m.err
}
func (m *granularMockDatabase) CountMessages(ctx context.Context) (int, error) {
	if m.countMessagesErr != nil {
		return 0, m.countMessagesErr
	}
	return 0, m.err
}
func (m *granularMockDatabase) CountPublishedMessages(ctx context.Context) (int, error) {
	if m.countPublishedMessagesErr != nil {
		return 0, m.countPublishedMessagesErr
	}
	return 0, m.err
}
func (m *granularMockDatabase) CountPublishFailures(ctx context.Context) (int, error) {
	if m.countPublishFailuresErr != nil {
		return 0, m.countPublishFailuresErr
	}
	return 0, m.err
}
func (m *granularMockDatabase) CountDeferredMessages(ctx context.Context) (int, error) {
	if m.countDeferredMessagesErr != nil {
		return 0, m.countDeferredMessagesErr
	}
	return 0, m.err
}
func (m *granularMockDatabase) CountDeletedMessages(ctx context.Context) (int, error) {
	if m.countDeletedMessagesErr != nil {
		return 0, m.countDeletedMessagesErr
	}
	return 0, m.err
}
func (m *granularMockDatabase) CountBlobs(ctx context.Context) (int, error) {
	if m.countBlobsErr != nil {
		return 0, m.countBlobsErr
	}
	return 0, m.err
}
func (m *granularMockDatabase) GetBridgeState(ctx context.Context, key string) (string, bool, error) {
	if m.getBridgeStateErrs != nil {
		if err, ok := m.getBridgeStateErrs[key]; ok {
			return "", false, err
		}
	}
	return "", false, m.err
}
func (m *granularMockDatabase) ListTopDeferredReasons(ctx context.Context, limit int) ([]db.DeferredReasonCount, error) {
	if m.listTopDeferredReasonsErr != nil {
		return nil, m.listTopDeferredReasonsErr
	}
	return nil, m.err
}
func (m *granularMockDatabase) ListTopIssueAccounts(ctx context.Context, limit int) ([]db.AccountIssueSummary, error) {
	if m.listTopIssueAccountsErr != nil {
		return nil, m.listTopIssueAccountsErr
	}
	return nil, m.err
}
func (m *granularMockDatabase) ListBridgedAccountsWithStats(ctx context.Context) ([]db.BridgedAccountStats, error) {
	if m.listBridgedAccountsWithStatsErr != nil {
		return nil, m.listBridgedAccountsWithStatsErr
	}
	return nil, m.err
}
func (m *granularMockDatabase) ListMessagesPage(ctx context.Context, query db.MessageListQuery) (db.MessagePage, error) {
	if m.listMessagesPageErr != nil {
		return db.MessagePage{}, m.listMessagesPageErr
	}
	return db.MessagePage{}, m.err
}
func (m *granularMockDatabase) ListMessageTypes(ctx context.Context) ([]string, error) {
	if m.listMessageTypesErr != nil {
		return nil, m.listMessageTypesErr
	}
	return nil, m.err
}
func (m *granularMockDatabase) GetMessage(ctx context.Context, atURI string) (*db.Message, error) {
	if m.getMessageErr != nil {
		return nil, m.getMessageErr
	}
	return nil, m.err
}
func (m *granularMockDatabase) GetPublishFailures(ctx context.Context, limit int) ([]db.Message, error) {
	if m.getPublishFailuresErr != nil {
		return nil, m.getPublishFailuresErr
	}
	return nil, m.err
}
func (m *granularMockDatabase) GetRecentBlobs(ctx context.Context, limit int) ([]db.Blob, error) {
	if m.getRecentBlobsErr != nil {
		return nil, m.getRecentBlobsErr
	}
	return nil, m.err
}
func (m *granularMockDatabase) GetAllBridgeState(ctx context.Context) ([]db.BridgeState, error) {
	if m.getAllBridgeStateErr != nil {
		return nil, m.getAllBridgeStateErr
	}
	return nil, m.err
}
func (m *granularMockDatabase) GetLatestDeferredReason(ctx context.Context) (string, bool, error) {
	if m.getLatestDeferredReasonErr != nil {
		return "", false, m.getLatestDeferredReasonErr
	}
	return "", false, m.err
}

func TestUIHandlerDashboardAllErrors(t *testing.T) {
	errBoom := fmt.Errorf("boom")
	// The dashboard handler calls many methods. We need to fail each one while others succeed.
	methods := []string{
		"CountBridgedAccounts",
		"CountMessages",
		"CountPublishedMessages",
		"CountPublishFailures",
		"CountDeferredMessages",
		"CountDeletedMessages",
		"CountBlobs",
		"GetBridgeState_firehose",
		"GetBridgeState_status",
		"GetBridgeState_heartbeat",
		"ListTopDeferredReasons",
		"ListTopIssueAccounts",
	}

	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			m := &granularMockDatabase{}
			switch method {
			case "CountBridgedAccounts":
				m.countBridgedAccountsErr = errBoom
			case "CountMessages":
				m.countMessagesErr = errBoom
			case "CountPublishedMessages":
				m.countPublishedMessagesErr = errBoom
			case "CountPublishFailures":
				m.countPublishFailuresErr = errBoom
			case "CountDeferredMessages":
				m.countDeferredMessagesErr = errBoom
			case "CountDeletedMessages":
				m.countDeletedMessagesErr = errBoom
			case "CountBlobs":
				m.countBlobsErr = errBoom
			case "GetBridgeState_firehose":
				m.getBridgeStateErrs = map[string]error{"firehose_seq": errBoom}
			case "GetBridgeState_status":
				m.getBridgeStateErrs = map[string]error{"bridge_runtime_status": errBoom}
			case "GetBridgeState_heartbeat":
				m.getBridgeStateErrs = map[string]error{"bridge_runtime_last_heartbeat_at": errBoom}
			case "ListTopDeferredReasons":
				m.listTopDeferredReasonsErr = errBoom
			case "ListTopIssueAccounts":
				m.listTopIssueAccountsErr = errBoom
			}
			h := NewUIHandler(m, nil)
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			rr := httptest.NewRecorder()
			h.handleDashboard(rr, req)
			if rr.Code != http.StatusInternalServerError {
				t.Errorf("method %s: expected 500, got %d", method, rr.Code)
			}
		})
	}
}

func TestUIHandlerMoreErrors(t *testing.T) {
	errBoom := fmt.Errorf("boom")

	t.Run("handleAccounts_TemplateError", func(t *testing.T) {
		// We can't easily trigger a template error without mocking the templates package
		// which is a global. Let's skip for now and focus on logic.
	})

	t.Run("handleMessages_ListMessagesPageError", func(t *testing.T) {
		m := &granularMockDatabase{listMessagesPageErr: errBoom}
		h := NewUIHandler(m, nil)
		req := httptest.NewRequest(http.MethodGet, "/messages", nil)
		rr := httptest.NewRecorder()
		h.handleMessages(rr, req)
		if rr.Code != http.StatusInternalServerError {
			t.Error()
		}
	})

	t.Run("handleMessages_ListMessageTypesError", func(t *testing.T) {
		m := &granularMockDatabase{listMessageTypesErr: errBoom}
		h := NewUIHandler(m, nil)
		req := httptest.NewRequest(http.MethodGet, "/messages", nil)
		rr := httptest.NewRecorder()
		h.handleMessages(rr, req)
		if rr.Code != http.StatusInternalServerError {
			t.Error()
		}
	})

	t.Run("handleAccounts_Error", func(t *testing.T) {
		m := &granularMockDatabase{listBridgedAccountsWithStatsErr: errBoom}
		h := NewUIHandler(m, nil)
		req := httptest.NewRequest(http.MethodGet, "/accounts", nil)
		rr := httptest.NewRecorder()
		h.handleAccounts(rr, req)
		if rr.Code != http.StatusInternalServerError {
			t.Error()
		}
	})

	t.Run("handleFailures_Error", func(t *testing.T) {
		m := &granularMockDatabase{getPublishFailuresErr: errBoom}
		h := NewUIHandler(m, nil)
		req := httptest.NewRequest(http.MethodGet, "/failures", nil)
		rr := httptest.NewRecorder()
		h.handleFailures(rr, req)
		if rr.Code != http.StatusInternalServerError {
			t.Error()
		}
	})

	t.Run("handleBlobs_Error", func(t *testing.T) {
		m := &granularMockDatabase{getRecentBlobsErr: errBoom}
		h := NewUIHandler(m, nil)
		req := httptest.NewRequest(http.MethodGet, "/blobs", nil)
		rr := httptest.NewRecorder()
		h.handleBlobs(rr, req)
		if rr.Code != http.StatusInternalServerError {
			t.Error()
		}
	})

	t.Run("handleState_Error", func(t *testing.T) {
		m := &granularMockDatabase{getAllBridgeStateErr: errBoom}
		h := NewUIHandler(m, nil)
		req := httptest.NewRequest(http.MethodGet, "/state", nil)
		rr := httptest.NewRecorder()
		h.handleState(rr, req)
		if rr.Code != http.StatusInternalServerError {
			t.Error()
		}
	})
}

func TestIssueReason(t *testing.T) {
	tests := []struct {
		name string
		msg  db.Message
		want string
	}{
		{"deferred_with_reason", db.Message{MessageState: db.MessageStateDeferred, DeferReason: "r1"}, "r1"},
		{"deleted_with_reason", db.Message{MessageState: db.MessageStateDeleted, DeletedReason: "r2"}, "r2"},
		{"failed_with_publish_error", db.Message{MessageState: db.MessageStateFailed, PublishError: "e1"}, "e1"},
		{"any_with_defer_reason", db.Message{MessageState: db.MessageStatePublished, DeferReason: "r3"}, "r3"},
		{"any_with_delete_reason", db.Message{MessageState: db.MessageStatePublished, DeletedReason: "r4"}, "r4"},
		{"none", db.Message{MessageState: db.MessageStatePublished}, "(none)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := issueReason(tt.msg)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
			if strings.TrimSpace(got) != fullIssueText(tt.msg) {
				t.Errorf("fullIssueText mismatch: %q", fullIssueText(tt.msg))
			}
		})
	}
}

func TestSanitizeMessageSort(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"oldest", "oldest"},
		{"newest", "newest"},
		{"invalid", "newest"},
		{"", "newest"},
		{"  attempts_desc  ", "attempts_desc"},
	}
	for _, tt := range tests {
		got := sanitizeMessageSort(tt.input)
		if got != tt.want {
			t.Errorf("sanitizeMessageSort(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestParseMessageLimit(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"50", 50},
		{"100", 100},
		{"200", 200},
		{"500", 500},
		{"999", 100},
		{"abc", 100},
		{"", 100},
	}
	for _, tt := range tests {
		got := parseMessageLimit(tt.input)
		if got != tt.want {
			t.Errorf("parseMessageLimit(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestMessageTypeLabelMore(t *testing.T) {
	tests := []struct {
		typ  string
		want string
	}{
		{"app.bsky.feed.post", "post"},
		{"app.bsky.feed.like", "like"},
		{"app.bsky.graph.follow", "follow"},
		{"app.bsky.graph.block", "block"},
		{"app.bsky.actor.profile", "profile"},
		{"app.bsky.feed.repost", "repost"},
		{"unknown", "unknown"},
		{"", "unknown"},
		{"  ", "unknown"},
	}
	for _, tt := range tests {
		got := messageTypeLabel(tt.typ)
		if got != tt.want {
			t.Errorf("messageTypeLabel(%q) = %q, want %q", tt.typ, got, tt.want)
		}
	}
}
