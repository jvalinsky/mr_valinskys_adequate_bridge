package bridge

import (
	"context"
	"errors"
	"io"
	"log"
	"strings"
	"testing"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/db"
)

func TestApplyMappedMessageLifecycleUnresolvedDefersWithBlobFallback(t *testing.T) {
	p := NewProcessor(&mockProcessorDatabase{}, log.New(io.Discard, "", 0))

	msg := db.Message{
		ATDID:        "did:plc:alice",
		MessageState: db.MessageStatePending,
	}
	outcome := p.applyMappedMessageLifecycle(
		context.Background(),
		&msg,
		map[string]interface{}{"_atproto_subject": "at://did:plc:bob/app.bsky.feed.post/missing"},
		errors.New("blob fail"),
		true,
	)

	if outcome.State != db.MessageStateDeferred {
		t.Fatalf("expected deferred state, got %q", outcome.State)
	}
	if !strings.Contains(msg.DeferReason, "at://did:plc:bob/app.bsky.feed.post/missing") {
		t.Fatalf("expected defer reason to include missing subject, got %q", msg.DeferReason)
	}
	if !strings.Contains(msg.PublishError, "blob_fallback") {
		t.Fatalf("expected blob fallback annotation, got %q", msg.PublishError)
	}
}

func TestApplyMappedMessageLifecycleSanitizeIncompleteDefers(t *testing.T) {
	p := NewProcessor(
		&mockProcessorDatabase{},
		log.New(io.Discard, "", 0),
		WithPublisher(&mockPublisher{ref: "%unused.sha256"}),
	)

	msg := db.Message{
		ATDID:        "did:plc:alice",
		MessageState: db.MessageStatePending,
	}
	outcome := p.applyMappedMessageLifecycle(
		context.Background(),
		&msg,
		map[string]interface{}{"type": "contact"},
		nil,
		true,
	)

	if outcome.State != db.MessageStateDeferred {
		t.Fatalf("expected deferred state, got %q", outcome.State)
	}
	if msg.DeferReason != deferReasonMissingRequiredFieldsAfterSanitize {
		t.Fatalf("expected defer reason %q, got %q", deferReasonMissingRequiredFieldsAfterSanitize, msg.DeferReason)
	}
}

func TestApplyMappedMessageLifecyclePublishOutcomes(t *testing.T) {
	successProc := NewProcessor(
		&mockProcessorDatabase{},
		log.New(io.Discard, "", 0),
		WithPublisher(&mockPublisher{ref: "%ok.sha256"}),
	)
	successMsg := db.Message{ATDID: "did:plc:alice", MessageState: db.MessageStatePending}
	successOutcome := successProc.applyMappedMessageLifecycle(
		context.Background(),
		&successMsg,
		map[string]interface{}{"type": "post", "text": "hello"},
		errors.New("blob fail"),
		true,
	)
	if successOutcome.State != db.MessageStatePublished {
		t.Fatalf("expected published state, got %q", successOutcome.State)
	}
	if successOutcome.PublishedRef != "%ok.sha256" {
		t.Fatalf("expected published ref %%ok.sha256, got %q", successOutcome.PublishedRef)
	}
	if !strings.Contains(successMsg.PublishError, "blob_fallback") {
		t.Fatalf("expected blob fallback annotation, got %q", successMsg.PublishError)
	}

	publishErr := errors.New("publish fail")
	failProc := NewProcessor(
		&mockProcessorDatabase{},
		log.New(io.Discard, "", 0),
		WithPublisher(&mockPublisher{err: publishErr}),
	)
	failMsg := db.Message{ATDID: "did:plc:alice", MessageState: db.MessageStatePending}
	failOutcome := failProc.applyMappedMessageLifecycle(
		context.Background(),
		&failMsg,
		map[string]interface{}{"type": "post", "text": "hello"},
		errors.New("blob fail"),
		true,
	)
	if failOutcome.State != db.MessageStateFailed {
		t.Fatalf("expected failed state, got %q", failOutcome.State)
	}
	if !errors.Is(failOutcome.PublishErr, publishErr) {
		t.Fatalf("expected publish error, got %v", failOutcome.PublishErr)
	}
	if !strings.Contains(failMsg.PublishError, "publish fail") || !strings.Contains(failMsg.PublishError, "blob_fallback") {
		t.Fatalf("expected publish and blob fallback errors, got %q", failMsg.PublishError)
	}
}
