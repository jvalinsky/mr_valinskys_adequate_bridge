package backfill

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestParseSince(t *testing.T) {
	seqFilter, err := ParseSince("42")
	if err != nil {
		t.Fatalf("parse seq since: %v", err)
	}
	if seqFilter.Sequence == nil || *seqFilter.Sequence != 42 {
		t.Fatalf("expected sequence filter")
	}

	timeFilter, err := ParseSince("2026-01-01T00:00:00Z")
	if err != nil {
		t.Fatalf("parse time since: %v", err)
	}
	if timeFilter.Timestamp == nil {
		t.Fatalf("expected timestamp filter")
	}

	if _, err := ParseSince("not-a-date"); err == nil {
		t.Fatalf("expected parse error for invalid value")
	}
}

func TestSinceFilterInclude(t *testing.T) {
	filter, err := ParseSince("2026-01-01T00:00:00Z")
	if err != nil {
		t.Fatalf("parse since: %v", err)
	}

	if !filter.Include([]byte(`{"createdAt":"2026-01-01T00:00:00Z"}`)) {
		t.Fatalf("expected same timestamp to be included")
	}
	if filter.Include([]byte(`{"createdAt":"2025-12-31T23:59:59Z"}`)) {
		t.Fatalf("expected older record to be excluded")
	}
	if !filter.Include([]byte(`{"text":"missing createdAt"}`)) {
		t.Fatalf("expected record without createdAt to be included")
	}
}

func TestIsSupportedCollectionIncludesFollow(t *testing.T) {
	if !isSupportedCollection("app.bsky.graph.follow") {
		t.Fatalf("expected app.bsky.graph.follow to be supported")
	}
}

func TestCollectionFromPath(t *testing.T) {
	tests := []struct {
		path   string
		want   string
		wantOK bool
	}{
		{"app.bsky.feed.post/abc123", "app.bsky.feed.post", true},
		{"app.bsky.feed.like/xyz789", "app.bsky.feed.like", true},
		{"app.bsky.graph.follow/def456", "app.bsky.graph.follow", true},
		{"app.bsky.graph.block/ghi789", "app.bsky.graph.block", true},
		{"app.bsky.actor.profile/jkl012", "app.bsky.actor.profile", true},
		{"", "", false},
		{"/onlycollection", "", false},
		{"nocollectionpath", "", false},
		{"single/extra/slash", "single", true},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got, ok := collectionFromPath(tt.path)
			if ok != tt.wantOK {
				t.Errorf("collectionFromPath(%q) ok = %v, want %v", tt.path, ok, tt.wantOK)
			}
			if ok && got != tt.want {
				t.Errorf("collectionFromPath(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestIsSupportedCollection(t *testing.T) {
	supported := []string{
		"app.bsky.feed.post",
		"app.bsky.feed.like",
		"app.bsky.graph.follow",
		"app.bsky.graph.block",
		"app.bsky.actor.profile",
	}
	unsupported := []string{
		"app.bsky.feed.repost",
		"app.bsky.feed.generator",
		"app.bsky.actor.defs#profileViewBasic",
		"chat.bsky.convo",
		"",
	}

	for _, c := range supported {
		if !isSupportedCollection(c) {
			t.Errorf("isSupportedCollection(%q) = false, want true", c)
		}
	}
	for _, c := range unsupported {
		if isSupportedCollection(c) {
			t.Errorf("isSupportedCollection(%q) = true, want false", c)
		}
	}
}

func TestCborToJSON(t *testing.T) {
	_, err := cborToJSON([]byte("not valid cbor"))
	if err == nil {
		t.Fatalf("expected error for invalid CBOR")
	}
}

func TestSinceFilterIncludeInvalidJSON(t *testing.T) {
	filter, err := ParseSince("2026-01-01T00:00:00Z")
	if err != nil {
		t.Fatalf("parse since: %v", err)
	}
	if !filter.Include([]byte("not json")) {
		t.Fatalf("expected invalid JSON to be included (pass through)")
	}
}

func TestSinceFilterIncludeEmptyCreatedAt(t *testing.T) {
	filter, err := ParseSince("2026-01-01T00:00:00Z")
	if err != nil {
		t.Fatalf("parse since: %v", err)
	}
	if !filter.Include([]byte(`{"createdAt":""}`)) {
		t.Fatalf("expected empty createdAt to be included")
	}
}

func TestSinceFilterIncludeInvalidTimestamp(t *testing.T) {
	filter, err := ParseSince("2026-01-01T00:00:00Z")
	if err != nil {
		t.Fatalf("parse since: %v", err)
	}
	if !filter.Include([]byte(`{"createdAt":"not-a-timestamp"}`)) {
		t.Fatalf("expected invalid timestamp to be included (fail open)")
	}
}

func TestParseSinceEmpty(t *testing.T) {
	filter, err := ParseSince("")
	if err != nil {
		t.Fatalf("parse empty since: %v", err)
	}
	if filter.Raw != "" {
		t.Errorf("expected empty Raw, got %q", filter.Raw)
	}
	if filter.Timestamp != nil {
		t.Errorf("expected nil Timestamp")
	}
	if filter.Sequence != nil {
		t.Errorf("expected nil Sequence")
	}
}

func TestParseSinceDateOnly(t *testing.T) {
	filter, err := ParseSince("2026-03-15")
	if err != nil {
		t.Fatalf("parse date only: %v", err)
	}
	if filter.Timestamp == nil {
		t.Fatalf("expected Timestamp to be set")
	}
}

func TestParseSinceDateTime(t *testing.T) {
	filter, err := ParseSince("2026-03-15 14:30:00")
	if err != nil {
		t.Fatalf("parse datetime: %v", err)
	}
	if filter.Timestamp == nil {
		t.Fatalf("expected Timestamp to be set")
	}
}

func TestParseSinceSequenceNotice(t *testing.T) {
	filter, err := ParseSince("42")
	if err != nil {
		t.Fatalf("parse seq since: %v", err)
	}
	if filter.SequenceNotice == "" {
		t.Errorf("expected SequenceNotice to be set for sequence-based filtering")
	}
}

func TestProcessRepoCARReadError(t *testing.T) {
	// Invalid CAR data should fail ReadRepoFromCar
	_, err := processRepoCAR(context.Background(), []byte("garbage"), "did:plc:x", SinceFilter{}, nil, nil)
	if err == nil {
		t.Fatal("expected error for invalid CAR data")
	}
	if !strings.Contains(err.Error(), "read repo car") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestIsSupportedCollectionAll(t *testing.T) {
	if !isSupportedCollection("app.bsky.feed.post") {
		t.Error()
	}
	if !isSupportedCollection("app.bsky.feed.like") {
		t.Error()
	}
	if !isSupportedCollection("app.bsky.graph.follow") {
		t.Error()
	}
	if !isSupportedCollection("app.bsky.graph.block") {
		t.Error()
	}
	if !isSupportedCollection("app.bsky.actor.profile") {
		t.Error()
	}
	if isSupportedCollection("other") {
		t.Error()
	}
}

func TestRunForDID_DialError(t *testing.T) {
	ctx := context.Background()
	res := RunForDID(ctx, "did:plc:err", SinceFilter{}, nil, nil,
		&stubHostResolver{errs: map[string]error{"did:plc:err": fmt.Errorf("dial fail")}}, nil)
	if res.Status != StatusTransportError {
		t.Errorf("expected StatusTransportError, got %s", res.Status)
	}
}

func TestProcessRepoCAR_IterationError(t *testing.T) {
	// A repo that has a valid header but fails during iteration.
	// mustCreateRepoCAR is available in pds_test.go (same package).
	carData := mustCreateRepoCAR(t, "did:plc:test")
	// Truncate it to corrupt the block stream
	carData = carData[:len(carData)-10]

	_, err := processRepoCAR(context.Background(), carData, "did:plc:test", SinceFilter{}, nil, nil)
	if err == nil {
		t.Fatal("expected iteration error for truncated CAR")
	}
}
