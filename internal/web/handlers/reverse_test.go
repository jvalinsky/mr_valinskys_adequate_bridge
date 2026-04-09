package handlers

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/bridge"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/db"
)

type stubReverseSyncProvider struct {
	enabled  bool
	statuses map[string]bridge.ReverseCredentialStatus
	retried  []string
}

func (s *stubReverseSyncProvider) Enabled() bool {
	return s.enabled
}

func (s *stubReverseSyncProvider) CredentialStatus(did string) bridge.ReverseCredentialStatus {
	if s.statuses == nil {
		return bridge.ReverseCredentialStatus{}
	}
	return s.statuses[did]
}

func (s *stubReverseSyncProvider) RetryEvent(_ context.Context, sourceSSBMsgRef string) error {
	s.retried = append(s.retried, sourceSSBMsgRef)
	return nil
}

func TestHandleReverseRendersMappingsAndEvents(t *testing.T) {
	database := openTestDB(t)
	defer database.Close()

	ctx := context.Background()
	if err := database.AddReverseIdentityMapping(ctx, db.ReverseIdentityMapping{
		SSBFeedID:    "@alice.ed25519",
		ATDID:        "did:plc:alice",
		Active:       true,
		AllowPosts:   true,
		AllowReplies: true,
		AllowFollows: true,
	}); err != nil {
		t.Fatalf("add reverse mapping: %v", err)
	}
	if err := database.AddReverseEvent(ctx, db.ReverseEvent{
		SourceSSBMsgRef: "%reverse-post.sha256",
		SourceSSBAuthor: "@alice.ed25519",
		ReceiveLogSeq:   9,
		ATDID:           "did:plc:alice",
		Action:          db.ReverseActionPost,
		EventState:      db.ReverseEventStateDeferred,
		DeferReason:     "credentials_missing",
		RawSSBJSON:      `{"type":"post","text":"hello"}`,
	}); err != nil {
		t.Fatalf("add reverse event: %v", err)
	}

	router := chi.NewRouter()
	handler := NewUIHandler(database, log.New(io.Discard, "", 0), nil, nil, &mockSSBStatus{}, &mockFeedIDProvider{}).
		WithReverseSync(&stubReverseSyncProvider{
			enabled: true,
			statuses: map[string]bridge.ReverseCredentialStatus{
				"did:plc:alice": {Configured: true},
			},
		})
	handler.Mount(router)

	req := httptest.NewRequest(http.MethodGet, "/reverse", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	body := rr.Body.String()
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, body)
	}
	if !strings.Contains(body, "@alice.ed25519") || !strings.Contains(body, "%reverse-post.sha256") || !strings.Contains(body, "configured") {
		t.Fatalf("reverse page missing expected content: %s", body)
	}
}

func TestHandleReverseEventRetry(t *testing.T) {
	database := openTestDB(t)
	defer database.Close()

	provider := &stubReverseSyncProvider{enabled: true}
	router := chi.NewRouter()
	handler := NewUIHandler(database, log.New(io.Discard, "", 0), nil, nil, &mockSSBStatus{}, &mockFeedIDProvider{}).WithReverseSync(provider)
	handler.Mount(router)

	form := url.Values{}
	form.Set("source_ssb_msg_ref", "%retry.sha256")
	req := httptest.NewRequest(http.MethodPost, "/reverse/events/retry", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect, got %d", rr.Code)
	}
	if len(provider.retried) != 1 || provider.retried[0] != "%retry.sha256" {
		t.Fatalf("unexpected retry calls: %#v", provider.retried)
	}
}

func TestHandleReverseShowsMediaDeferReason(t *testing.T) {
	database := openTestDB(t)
	defer database.Close()

	ctx := context.Background()
	if err := database.AddReverseIdentityMapping(ctx, db.ReverseIdentityMapping{
		SSBFeedID:    "@alice.ed25519",
		ATDID:        "did:plc:alice",
		Active:       true,
		AllowPosts:   true,
		AllowReplies: true,
		AllowFollows: true,
	}); err != nil {
		t.Fatalf("add reverse mapping: %v", err)
	}
	if err := database.AddReverseEvent(ctx, db.ReverseEvent{
		SourceSSBMsgRef: "%reverse-media.sha256",
		SourceSSBAuthor: "@alice.ed25519",
		ReceiveLogSeq:   10,
		ATDID:           "did:plc:alice",
		Action:          db.ReverseActionPost,
		EventState:      db.ReverseEventStateDeferred,
		DeferReason:     "blob_upload_failed=&media.sha256",
		RawSSBJSON:      `{"type":"post","text":"hello"}`,
	}); err != nil {
		t.Fatalf("add reverse event: %v", err)
	}

	router := chi.NewRouter()
	handler := NewUIHandler(database, log.New(io.Discard, "", 0), nil, nil, &mockSSBStatus{}, &mockFeedIDProvider{}).
		WithReverseSync(&stubReverseSyncProvider{enabled: true})
	handler.Mount(router)

	req := httptest.NewRequest(http.MethodGet, "/reverse", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "blob_upload_failed=&amp;media.sha256") {
		t.Fatalf("expected media defer reason in body: %s", rr.Body.String())
	}
}
