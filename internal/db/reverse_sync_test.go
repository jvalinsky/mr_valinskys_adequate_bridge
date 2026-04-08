package db

import (
	"context"
	"strings"
	"testing"
)

func TestReverseIdentityMappingsAndEvents(t *testing.T) {
	database := openTestDB(t)
	defer database.Close()

	ctx := context.Background()
	if err := database.AddReverseIdentityMapping(ctx, ReverseIdentityMapping{
		SSBFeedID:    "@alice.ed25519",
		ATDID:        "did:plc:alice",
		Active:       true,
		AllowPosts:   true,
		AllowReplies: false,
		AllowFollows: true,
	}); err != nil {
		t.Fatalf("add reverse mapping: %v", err)
	}

	mapping, err := database.GetReverseIdentityMapping(ctx, "@alice.ed25519")
	if err != nil {
		t.Fatalf("get reverse mapping: %v", err)
	}
	if mapping == nil || mapping.ATDID != "did:plc:alice" || mapping.AllowReplies {
		t.Fatalf("unexpected mapping: %#v", mapping)
	}

	resolved, ok, err := database.ResolveATDIDBySSBFeed(ctx, "@alice.ed25519")
	if err != nil {
		t.Fatalf("resolve at did by ssb feed: %v", err)
	}
	if !ok || resolved != "did:plc:alice" {
		t.Fatalf("unexpected resolve result: ok=%v did=%s", ok, resolved)
	}

	if err := database.AddReverseEvent(ctx, ReverseEvent{
		SourceSSBMsgRef: "%follow.sha256",
		SourceSSBAuthor: "@alice.ed25519",
		ReceiveLogSeq:   7,
		ATDID:           "did:plc:alice",
		Action:          ReverseActionFollow,
		EventState:      ReverseEventStatePublished,
		TargetATDID:     "did:plc:bob",
		ResultATURI:     "at://did:plc:alice/app.bsky.graph.follow/f1",
		ResultATCID:     "cid-follow",
		ResultCollection: "app.bsky.graph.follow",
		Attempts:        1,
	}); err != nil {
		t.Fatalf("add reverse event: %v", err)
	}

	events, err := database.ListReverseEvents(ctx, ReverseEventListQuery{Search: "did:plc:bob"})
	if err != nil {
		t.Fatalf("list reverse events: %v", err)
	}
	if len(events) != 1 || events[0].SourceSSBMsgRef != "%follow.sha256" {
		t.Fatalf("unexpected reverse events: %#v", events)
	}

	latestFollow, err := database.GetLatestPublishedReverseFollow(ctx, "did:plc:alice", "did:plc:bob")
	if err != nil {
		t.Fatalf("get latest published reverse follow: %v", err)
	}
	if latestFollow == nil || !strings.Contains(latestFollow.ResultATURI, "/f1") {
		t.Fatalf("unexpected latest follow: %#v", latestFollow)
	}
}

