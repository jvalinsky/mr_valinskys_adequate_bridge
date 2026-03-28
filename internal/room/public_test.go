package room

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mr_valinskys_adequate_bridge/internal/db"
	refs "github.com/ssbc/go-ssb-refs"
	"github.com/ssbc/go-ssb-room/v2/roomdb"
	roommockdb "github.com/ssbc/go-ssb-room/v2/roomdb/mockdb"
)

func TestBridgeRoomHandlerLandingPageOpenMode(t *testing.T) {
	roomConfig := newTestRoomConfig(roomdb.ModeOpen)
	handler := newBridgeRoomHandler(http.NotFoundHandler(), roomConfig, nil)

	req := httptest.NewRequest(http.MethodGet, "http://room.test/", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recorder.Code)
	}

	body := recorder.Body.String()
	for _, want := range []string{
		"Create room invite",
		"Browse bridged bots",
		"Open room sign-in",
		"Anyone visiting this page can create a room invite",
		"0 active bridged bots currently listed in the directory.",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("landing page missing %q\nbody:\n%s", want, body)
		}
	}
}

func TestBridgeRoomHandlerLandingPageNonOpenDisablesInvite(t *testing.T) {
	roomConfig := newTestRoomConfig(roomdb.ModeCommunity)
	handler := newBridgeRoomHandler(http.NotFoundHandler(), roomConfig, nil)

	req := httptest.NewRequest(http.MethodGet, "http://room.test/", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recorder.Code)
	}

	body := recorder.Body.String()
	for _, want := range []string{
		"Self-serve invites disabled",
		"Community",
		"Existing room members can sign in to create invites.",
		"Browse bridged bots",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("landing page missing %q\nbody:\n%s", want, body)
		}
	}
}

func TestBridgeRoomHandlerBotsPageListsActiveAccountsOnly(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "bridge.sqlite"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	activeFeed := mustTestFeedRef(t, 1)
	inactiveFeed := mustTestFeedRef(t, 2)

	if err := database.AddBridgedAccount(t.Context(), db.BridgedAccount{
		ATDID:     "did:plc:active-bridge-bot",
		SSBFeedID: activeFeed.String(),
		Active:    true,
	}); err != nil {
		t.Fatalf("add active bridged account: %v", err)
	}
	if err := database.AddBridgedAccount(t.Context(), db.BridgedAccount{
		ATDID:     "did:plc:inactive-bridge-bot",
		SSBFeedID: inactiveFeed.String(),
		Active:    false,
	}); err != nil {
		t.Fatalf("add inactive bridged account: %v", err)
	}

	// Add a published message for the active account so stats appear.
	now := time.Now()
	if err := database.AddMessage(t.Context(), db.Message{
		ATURI: "at://did:plc:active-bridge-bot/app.bsky.feed.post/test1",
		ATCID: "cid-test1", ATDID: "did:plc:active-bridge-bot",
		Type: "app.bsky.feed.post", MessageState: db.MessageStatePublished,
		PublishedAt: &now,
	}); err != nil {
		t.Fatalf("add message: %v", err)
	}

	roomConfig := newTestRoomConfig(roomdb.ModeOpen)
	handler := newBridgeRoomHandler(http.NotFoundHandler(), roomConfig, database)

	req := httptest.NewRequest(http.MethodGet, "http://room.test/bots", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recorder.Code)
	}

	body := recorder.Body.String()
	// The new card layout shows abbreviated DID and links to detail page.
	for _, want := range []string{
		"did:plc:active-br",    // abbreviated DID prefix shown on card
		"/bots/did:plc:active", // detail link
		"View details",         // card CTA
		"1 msgs",               // stats pill
		"1 published",          // published stat
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("bots page missing %q\nbody:\n%s", want, body)
		}
	}

	for _, unwanted := range []string{
		"did:plc:inactive-bridge-bot",
		inactiveFeed.String(),
	} {
		if strings.Contains(body, unwanted) {
			t.Fatalf("bots page unexpectedly included %q\nbody:\n%s", unwanted, body)
		}
	}
}

func TestBridgeRoomHandlerBotsPageSearchAndSort(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "bridge.sqlite"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	ctx := t.Context()
	if err := database.AddBridgedAccount(ctx, db.BridgedAccount{
		ATDID:     "did:plc:delta-bot",
		SSBFeedID: mustTestFeedRef(t, 10).String(),
		Active:    true,
	}); err != nil {
		t.Fatalf("add delta account: %v", err)
	}
	if err := database.AddBridgedAccount(ctx, db.BridgedAccount{
		ATDID:     "did:plc:gamma-bot",
		SSBFeedID: mustTestFeedRef(t, 11).String(),
		Active:    true,
	}); err != nil {
		t.Fatalf("add gamma account: %v", err)
	}

	seed := []db.Message{
		{ATURI: "at://did:plc:delta-bot/app.bsky.feed.post/1", ATCID: "cid-d1", ATDID: "did:plc:delta-bot", Type: "app.bsky.feed.post", MessageState: db.MessageStateDeferred, DeferReason: "missing parent"},
		{ATURI: "at://did:plc:delta-bot/app.bsky.feed.post/2", ATCID: "cid-d2", ATDID: "did:plc:delta-bot", Type: "app.bsky.feed.post", MessageState: db.MessageStateDeferred, DeferReason: "missing root"},
		{ATURI: "at://did:plc:gamma-bot/app.bsky.feed.post/1", ATCID: "cid-g1", ATDID: "did:plc:gamma-bot", Type: "app.bsky.feed.post", MessageState: db.MessageStatePublished},
	}
	for _, msg := range seed {
		if err := database.AddMessage(ctx, msg); err != nil {
			t.Fatalf("add message %s: %v", msg.ATURI, err)
		}
	}

	handler := newBridgeRoomHandler(http.NotFoundHandler(), newTestRoomConfig(roomdb.ModeOpen), database)

	// Search should narrow to delta only.
	searchReq := httptest.NewRequest(http.MethodGet, "http://room.test/bots?q=delta&sort=activity_desc", nil)
	searchRec := httptest.NewRecorder()
	handler.ServeHTTP(searchRec, searchReq)
	if searchRec.Code != http.StatusOK {
		t.Fatalf("search request status: %d", searchRec.Code)
	}
	searchBody := searchRec.Body.String()
	if !strings.Contains(searchBody, "did:plc:delta") {
		t.Fatalf("search result missing delta bot: %s", searchBody)
	}
	if strings.Contains(searchBody, "did:plc:gamma") {
		t.Fatalf("search should exclude gamma bot: %s", searchBody)
	}
	if !strings.Contains(searchBody, "Search DID/feed") || !strings.Contains(searchBody, "Most deferred") {
		t.Fatalf("search/sort controls missing from bots page: %s", searchBody)
	}

	// Deferred sort should rank delta before gamma.
	sortReq := httptest.NewRequest(http.MethodGet, "http://room.test/bots?sort=deferred_desc", nil)
	sortRec := httptest.NewRecorder()
	handler.ServeHTTP(sortRec, sortReq)
	if sortRec.Code != http.StatusOK {
		t.Fatalf("sort request status: %d", sortRec.Code)
	}
	sortBody := sortRec.Body.String()
	deltaIdx := strings.Index(sortBody, "did:plc:delta")
	gammaIdx := strings.Index(sortBody, "did:plc:gamma")
	if deltaIdx == -1 || gammaIdx == -1 {
		t.Fatalf("expected both delta and gamma cards in sorted output: %s", sortBody)
	}
	if deltaIdx > gammaIdx {
		t.Fatalf("expected delta (more deferred) before gamma in deferred_desc sort")
	}
}

func TestBridgeRoomHandlerBotDetailPage(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "bridge.sqlite"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	activeFeed := mustTestFeedRef(t, 1)

	if err := database.AddBridgedAccount(t.Context(), db.BridgedAccount{
		ATDID:     "did:plc:detail-test-bot",
		SSBFeedID: activeFeed.String(),
		Active:    true,
	}); err != nil {
		t.Fatalf("add bridged account: %v", err)
	}

	now := time.Now()
	if err := database.AddMessage(t.Context(), db.Message{
		ATURI: "at://did:plc:detail-test-bot/app.bsky.feed.post/m1",
		ATCID: "cid-m1", ATDID: "did:plc:detail-test-bot",
		Type: "app.bsky.feed.post", MessageState: db.MessageStatePublished,
		RawATJson:   `{"$type":"app.bsky.feed.post","text":"hello from atproto","createdAt":"2026-03-28T03:00:00Z"}`,
		RawSSBJson:  `{"type":"post","text":"hello from ssb bridge"}`,
		SSBMsgRef:   "%detail-test-message.sha256",
		PublishedAt: &now,
	}); err != nil {
		t.Fatalf("add message: %v", err)
	}
	if err := database.AddMessage(t.Context(), db.Message{
		ATURI: "at://did:plc:detail-test-bot/app.bsky.feed.post/m2",
		ATCID: "cid-m2", ATDID: "did:plc:detail-test-bot",
		Type: "app.bsky.feed.post", MessageState: db.MessageStateFailed,
		RawSSBJson: `{"type":"post","text":"this should stay hidden"}`,
	}); err != nil {
		t.Fatalf("add failed message: %v", err)
	}

	roomConfig := newTestRoomConfig(roomdb.ModeOpen)
	handler := newBridgeRoomHandler(http.NotFoundHandler(), roomConfig, database)

	req := httptest.NewRequest(http.MethodGet, "http://room.test/bots/did:plc:detail-test-bot", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recorder.Code)
	}

	body := recorder.Body.String()
	for _, want := range []string{
		"did:plc:detail-test-bot", // full DID on detail page
		activeFeed.String(),       // full feed ID
		activeFeed.URI(),          // canonical feed URI
		"Copy DID",                // copy button
		"Copy feed ID",            // copy button
		"Copy feed URI",           // copy button
		"Open feed URI",           // action button
		"← Back to directory",     // back nav
		"Bot detail",              // eyebrow
		"Published messages",      // published message section
		"hello from atproto",      // rendered source text
		"hello from ssb bridge",   // rendered bridged text
		"%detail-test-message.sha256",
		"Show stored payloads",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("detail page missing %q\nbody:\n%s", want, body)
		}
	}
	if strings.Contains(body, "this should stay hidden") {
		t.Fatalf("detail page should only render published messages\nbody:\n%s", body)
	}
}

func TestBridgeRoomHandlerBotDetailPage404ForUnknownDID(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "bridge.sqlite"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	roomConfig := newTestRoomConfig(roomdb.ModeOpen)
	handler := newBridgeRoomHandler(http.NotFoundHandler(), roomConfig, database)

	req := httptest.NewRequest(http.MethodGet, "http://room.test/bots/did:plc:nonexistent", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", recorder.Code)
	}
}

func TestBridgeRoomHandlerBotDetailPage404ForInactiveDID(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "bridge.sqlite"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	if err := database.AddBridgedAccount(t.Context(), db.BridgedAccount{
		ATDID:     "did:plc:inactive-detail-bot",
		SSBFeedID: mustTestFeedRef(t, 5).String(),
		Active:    false,
	}); err != nil {
		t.Fatalf("add inactive bridged account: %v", err)
	}

	roomConfig := newTestRoomConfig(roomdb.ModeOpen)
	handler := newBridgeRoomHandler(http.NotFoundHandler(), roomConfig, database)

	req := httptest.NewRequest(http.MethodGet, "http://room.test/bots/did:plc:inactive-detail-bot", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for inactive bot, got %d", recorder.Code)
	}
}

func TestBridgeRoomHandlerCreateInviteJSONFailsOutsideOpenMode(t *testing.T) {
	roomConfig := newTestRoomConfig(roomdb.ModeRestricted)
	var delegated bool
	stockHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		delegated = true
		w.WriteHeader(http.StatusOK)
	})
	handler := newBridgeRoomHandler(stockHandler, roomConfig, nil)

	req := httptest.NewRequest(http.MethodPost, "http://room.test/create-invite", nil)
	req.Header.Set("Accept", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if delegated {
		t.Fatal("expected wrapper to block stock create-invite route outside open mode")
	}
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", recorder.Code)
	}

	var response struct {
		Status string `json:"status"`
		Error  string `json:"error"`
	}
	if err := json.NewDecoder(bytes.NewReader(recorder.Body.Bytes())).Decode(&response); err != nil {
		t.Fatalf("decode json response: %v", err)
	}
	if response.Status != "failed" {
		t.Fatalf("expected failed status, got %q", response.Status)
	}
	if !strings.Contains(response.Error, "room mode is open") {
		t.Fatalf("expected explanatory error, got %q", response.Error)
	}
}

func TestBridgeRoomHandlerDelegatesStockRoutes(t *testing.T) {
	roomConfig := newTestRoomConfig(roomdb.ModeOpen)
	stockHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = io.WriteString(w, "stock auth handler")
	})
	handler := newBridgeRoomHandler(stockHandler, roomConfig, nil)

	req := httptest.NewRequest(http.MethodGet, "http://room.test/login", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusTeapot {
		t.Fatalf("expected 418 from delegated stock handler, got %d", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "stock auth handler") {
		t.Fatalf("expected delegated response body, got %q", recorder.Body.String())
	}
}

func newTestRoomConfig(mode roomdb.PrivacyMode) *roommockdb.FakeRoomConfig {
	cfg := &roommockdb.FakeRoomConfig{}
	cfg.GetPrivacyModeReturns(mode, nil)
	cfg.GetDefaultLanguageReturns("en", nil)
	return cfg
}

func mustTestFeedRef(t *testing.T, fill byte) refs.FeedRef {
	t.Helper()

	ref, err := refs.NewFeedRefFromBytes(bytes.Repeat([]byte{fill}, 32), refs.RefAlgoFeedSSB1)
	if err != nil {
		t.Fatalf("create test feed ref: %v", err)
	}
	return ref
}
