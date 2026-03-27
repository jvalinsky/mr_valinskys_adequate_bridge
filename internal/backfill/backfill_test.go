package backfill

import "testing"

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
