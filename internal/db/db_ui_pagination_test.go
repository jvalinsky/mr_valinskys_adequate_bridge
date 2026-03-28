package db

import (
	"path/filepath"
	"testing"
	"time"
)

func TestListMessagesPageKeysetNewestWithPrevNext(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "bridge.sqlite"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	ctx := t.Context()
	baseTime := time.Date(2026, 3, 27, 12, 0, 0, 0, time.UTC)
	seed := []struct {
		uri    string
		did    string
		state  string
		reason string
		offset int
	}{
		{uri: "at://did:plc:alice/app.bsky.feed.post/1", did: "did:plc:alice", state: MessageStatePublished, offset: 0},
		{uri: "at://did:plc:alice/app.bsky.feed.post/2", did: "did:plc:alice", state: MessageStateFailed, reason: "publish failed", offset: 1},
		{uri: "at://did:plc:alice/app.bsky.feed.post/3", did: "did:plc:alice", state: MessageStateDeferred, reason: "_atproto_subject=at://missing", offset: 2},
		{uri: "at://did:plc:bob/app.bsky.feed.post/4", did: "did:plc:bob", state: MessageStatePublished, offset: 3},
		{uri: "at://did:plc:bob/app.bsky.feed.post/5", did: "did:plc:bob", state: MessageStateDeleted, reason: "deleted by source", offset: 4},
		{uri: "at://did:plc:bob/app.bsky.feed.post/6", did: "did:plc:bob", state: MessageStateFailed, reason: "bridge timeout", offset: 5},
	}

	for _, row := range seed {
		msg := Message{
			ATURI:        row.uri,
			ATCID:        "cid-" + row.uri[len(row.uri)-1:],
			ATDID:        row.did,
			Type:         "app.bsky.feed.post",
			MessageState: row.state,
			RawATJson:    `{"text":"hello"}`,
			RawSSBJson:   `{"type":"post","text":"hello"}`,
			PublishError: row.reason,
			DeferReason:  row.reason,
			DeletedReason: func() string {
				if row.state == MessageStateDeleted {
					return row.reason
				}
				return ""
			}(),
		}
		if err := database.AddMessage(ctx, msg); err != nil {
			t.Fatalf("add message %s: %v", row.uri, err)
		}
		if _, err := database.conn.ExecContext(ctx, `UPDATE messages SET created_at = ? WHERE at_uri = ?`, baseTime.Add(time.Duration(row.offset)*time.Minute), row.uri); err != nil {
			t.Fatalf("update created_at %s: %v", row.uri, err)
		}
	}

	page1, err := database.ListMessagesPage(ctx, MessageListQuery{Sort: "newest", Limit: 2})
	if err != nil {
		t.Fatalf("list page1: %v", err)
	}
	if len(page1.Messages) != 2 || !page1.HasNext || page1.HasPrev {
		t.Fatalf("unexpected page1: len=%d hasNext=%v hasPrev=%v", len(page1.Messages), page1.HasNext, page1.HasPrev)
	}
	if page1.Messages[0].ATURI != "at://did:plc:bob/app.bsky.feed.post/6" || page1.Messages[1].ATURI != "at://did:plc:bob/app.bsky.feed.post/5" {
		t.Fatalf("unexpected page1 order: %q %q", page1.Messages[0].ATURI, page1.Messages[1].ATURI)
	}

	page2, err := database.ListMessagesPage(ctx, MessageListQuery{
		Sort:      "newest",
		Limit:     2,
		Cursor:    page1.NextCursor,
		Direction: "next",
	})
	if err != nil {
		t.Fatalf("list page2: %v", err)
	}
	if len(page2.Messages) != 2 || !page2.HasNext || !page2.HasPrev {
		t.Fatalf("unexpected page2: len=%d hasNext=%v hasPrev=%v", len(page2.Messages), page2.HasNext, page2.HasPrev)
	}

	backToPage1, err := database.ListMessagesPage(ctx, MessageListQuery{
		Sort:      "newest",
		Limit:     2,
		Cursor:    page2.PrevCursor,
		Direction: "prev",
	})
	if err != nil {
		t.Fatalf("list backToPage1: %v", err)
	}
	if len(backToPage1.Messages) != 2 {
		t.Fatalf("unexpected backToPage1 len: %d", len(backToPage1.Messages))
	}
	if backToPage1.Messages[0].ATURI != page1.Messages[0].ATURI || backToPage1.Messages[1].ATURI != page1.Messages[1].ATURI {
		t.Fatalf("prev page mismatch: got [%s,%s], want [%s,%s]",
			backToPage1.Messages[0].ATURI,
			backToPage1.Messages[1].ATURI,
			page1.Messages[0].ATURI,
			page1.Messages[1].ATURI,
		)
	}

	filtered, err := database.ListMessagesPage(ctx, MessageListQuery{
		Sort:     "newest",
		Limit:    10,
		ATDID:    "did:plc:alice",
		HasIssue: true,
	})
	if err != nil {
		t.Fatalf("list filtered: %v", err)
	}
	if len(filtered.Messages) != 2 {
		t.Fatalf("expected 2 filtered rows, got %d", len(filtered.Messages))
	}
	for _, msg := range filtered.Messages {
		if msg.ATDID != "did:plc:alice" {
			t.Fatalf("unexpected DID in filtered result: %s", msg.ATDID)
		}
	}
}

func TestDashboardSummaryQueries(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "bridge.sqlite"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	ctx := t.Context()
	if err := database.AddBridgedAccount(ctx, BridgedAccount{ATDID: "did:plc:alpha", SSBFeedID: "@alpha.ed25519", Active: true}); err != nil {
		t.Fatalf("add alpha: %v", err)
	}
	if err := database.AddBridgedAccount(ctx, BridgedAccount{ATDID: "did:plc:beta", SSBFeedID: "@beta.ed25519", Active: true}); err != nil {
		t.Fatalf("add beta: %v", err)
	}

	messages := []Message{
		{ATURI: "at://did:plc:alpha/app.bsky.feed.post/1", ATCID: "cid1", ATDID: "did:plc:alpha", Type: "app.bsky.feed.post", MessageState: MessageStateDeferred, DeferReason: "_atproto_subject=at://x"},
		{ATURI: "at://did:plc:alpha/app.bsky.feed.post/2", ATCID: "cid2", ATDID: "did:plc:alpha", Type: "app.bsky.feed.post", MessageState: MessageStateDeferred, DeferReason: "_atproto_subject=at://x"},
		{ATURI: "at://did:plc:alpha/app.bsky.feed.post/3", ATCID: "cid3", ATDID: "did:plc:alpha", Type: "app.bsky.feed.post", MessageState: MessageStateFailed, PublishError: "publish failed"},
		{ATURI: "at://did:plc:beta/app.bsky.feed.post/4", ATCID: "cid4", ATDID: "did:plc:beta", Type: "app.bsky.feed.post", MessageState: MessageStateDeferred, DeferReason: "_atproto_contact=did:plc:zzz"},
		{ATURI: "at://did:plc:beta/app.bsky.feed.post/5", ATCID: "cid5", ATDID: "did:plc:beta", Type: "app.bsky.feed.post", MessageState: MessageStateDeleted, DeletedReason: "deleted by source"},
	}
	for _, msg := range messages {
		msg.RawATJson = `{}`
		msg.RawSSBJson = `{}`
		if err := database.AddMessage(ctx, msg); err != nil {
			t.Fatalf("add message %s: %v", msg.ATURI, err)
		}
	}

	reasons, err := database.ListTopDeferredReasons(ctx, 5)
	if err != nil {
		t.Fatalf("list reasons: %v", err)
	}
	if len(reasons) < 2 {
		t.Fatalf("expected at least 2 reasons, got %d", len(reasons))
	}
	if reasons[0].Reason != "_atproto_subject=at://x" || reasons[0].Count != 2 {
		t.Fatalf("unexpected top deferred reason: %+v", reasons[0])
	}

	issues, err := database.ListTopIssueAccounts(ctx, 5)
	if err != nil {
		t.Fatalf("list issue accounts: %v", err)
	}
	if len(issues) != 2 {
		t.Fatalf("expected 2 issue accounts, got %d", len(issues))
	}
	if issues[0].ATDID != "did:plc:alpha" || issues[0].IssueMessages != 3 {
		t.Fatalf("unexpected first issue summary: %+v", issues[0])
	}
	if issues[1].ATDID != "did:plc:beta" || issues[1].IssueMessages != 2 {
		t.Fatalf("unexpected second issue summary: %+v", issues[1])
	}
}

func TestListActiveBridgedAccountsWithStatsSorted(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "bridge.sqlite"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	ctx := t.Context()
	if err := database.AddBridgedAccount(ctx, BridgedAccount{ATDID: "did:plc:gamma", SSBFeedID: "@gamma.ed25519", Active: true}); err != nil {
		t.Fatalf("add gamma: %v", err)
	}
	if err := database.AddBridgedAccount(ctx, BridgedAccount{ATDID: "did:plc:delta", SSBFeedID: "@delta.ed25519", Active: true}); err != nil {
		t.Fatalf("add delta: %v", err)
	}
	if err := database.AddBridgedAccount(ctx, BridgedAccount{ATDID: "did:plc:inactive", SSBFeedID: "@inactive.ed25519", Active: false}); err != nil {
		t.Fatalf("add inactive: %v", err)
	}

	seed := []Message{
		{ATURI: "at://did:plc:gamma/app.bsky.feed.post/1", ATCID: "cid-g1", ATDID: "did:plc:gamma", Type: "app.bsky.feed.post", MessageState: MessageStatePublished},
		{ATURI: "at://did:plc:gamma/app.bsky.feed.post/2", ATCID: "cid-g2", ATDID: "did:plc:gamma", Type: "app.bsky.feed.post", MessageState: MessageStateDeferred, DeferReason: "missing parent"},
		{ATURI: "at://did:plc:delta/app.bsky.feed.post/1", ATCID: "cid-d1", ATDID: "did:plc:delta", Type: "app.bsky.feed.post", MessageState: MessageStateDeferred, DeferReason: "missing root"},
		{ATURI: "at://did:plc:delta/app.bsky.feed.post/2", ATCID: "cid-d2", ATDID: "did:plc:delta", Type: "app.bsky.feed.post", MessageState: MessageStateDeferred, DeferReason: "missing root"},
	}
	for _, msg := range seed {
		msg.RawATJson = `{}`
		msg.RawSSBJson = `{}`
		if err := database.AddMessage(ctx, msg); err != nil {
			t.Fatalf("add message %s: %v", msg.ATURI, err)
		}
	}

	matches, err := database.ListActiveBridgedAccountsWithStatsSorted(ctx, "delta", "activity_desc")
	if err != nil {
		t.Fatalf("search accounts: %v", err)
	}
	if len(matches) != 1 || matches[0].ATDID != "did:plc:delta" {
		t.Fatalf("unexpected search results: %+v", matches)
	}

	sorted, err := database.ListActiveBridgedAccountsWithStatsSorted(ctx, "", "deferred_desc")
	if err != nil {
		t.Fatalf("sort accounts: %v", err)
	}
	if len(sorted) != 2 {
		t.Fatalf("expected 2 active accounts, got %d", len(sorted))
	}
	if sorted[0].ATDID != "did:plc:delta" {
		t.Fatalf("expected delta first for deferred_desc, got %s", sorted[0].ATDID)
	}
}
