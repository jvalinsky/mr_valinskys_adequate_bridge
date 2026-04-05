package db

import (
	"context"
	"testing"
)

func FuzzMessageListCursor(f *testing.F) {
	f.Add("at://did:plc:abc/app.bsky.feed.post/123")
	f.Add("at://did:plc:xyz/app.bsky.feed.post/456")
	f.Add("")

	f.Fuzz(func(t *testing.T, input string) {
		if input == "" {
			return
		}
		decoded, ok := decodeMessageListCursor(input)
		if !ok {
			return
		}
		if decoded.ATURI == "" {
			t.Error("decoded ATURI should not be empty")
		}
	})
}

func FuzzAddMessage(f *testing.F) {
	f.Fuzz(func(t *testing.T, atURI, atCID, atDID, msgType, rawJSON string) {
		if atURI == "" || atCID == "" || atDID == "" || msgType == "" {
			return
		}

		db, err := Open(":memory:?parseTime=true")
		if err != nil {
			t.Skip("skipping: ", err)
		}
		defer db.Close()

		ctx := context.Background()
		msg := Message{
			ATURI:        atURI,
			ATCID:        atCID,
			ATDID:        atDID,
			Type:         msgType,
			MessageState: MessageStatePending,
			RawATJson:    rawJSON,
		}

		if err := db.AddMessage(ctx, msg); err != nil {
			t.Fatalf("AddMessage failed: %v", err)
		}

		got, err := db.GetMessage(ctx, atURI)
		if err != nil {
			t.Fatalf("GetMessage failed: %v", err)
		}
		if got == nil {
			t.Error("expected message to be stored")
		}
	})
}

func FuzzAddBridgedAccount(f *testing.F) {
	f.Fuzz(func(t *testing.T, atDID, ssbFeedID string) {
		if atDID == "" || ssbFeedID == "" {
			return
		}

		db, err := Open(":memory:?parseTime=true")
		if err != nil {
			t.Skip("skipping: ", err)
		}
		defer db.Close()

		ctx := context.Background()
		acc := BridgedAccount{
			ATDID:     atDID,
			SSBFeedID: ssbFeedID,
			Active:    true,
		}

		if err := db.AddBridgedAccount(ctx, acc); err != nil {
			t.Fatalf("AddBridgedAccount failed: %v", err)
		}

		got, err := db.GetBridgedAccount(ctx, atDID)
		if err != nil {
			t.Fatalf("GetBridgedAccount failed: %v", err)
		}
		if got == nil {
			t.Error("expected account to be stored")
		}
	})
}

func FuzzAddBlob(f *testing.F) {
	f.Fuzz(func(t *testing.T, atCID, ssbBlobRef, mimeType string) {
		if atCID == "" || ssbBlobRef == "" {
			return
		}

		db, err := Open(":memory:?parseTime=true")
		if err != nil {
			t.Skip("skipping: ", err)
		}
		defer db.Close()

		ctx := context.Background()
		blob := Blob{
			ATCID:      atCID,
			SSBBlobRef: ssbBlobRef,
			MimeType:   mimeType,
		}

		if err := db.AddBlob(ctx, blob); err != nil {
			t.Fatalf("AddBlob failed: %v", err)
		}

		got, err := db.GetBlob(ctx, atCID)
		if err != nil {
			t.Fatalf("GetBlob failed: %v", err)
		}
		if got == nil {
			t.Error("expected blob to be stored")
		}
	})
}

func FuzzSetBridgeState(f *testing.F) {
	f.Fuzz(func(t *testing.T, key, value string) {
		if key == "" {
			return
		}

		db, err := Open(":memory:?parseTime=true")
		if err != nil {
			t.Skip("skipping: ", err)
		}
		defer db.Close()

		ctx := context.Background()
		if err := db.SetBridgeState(ctx, key, value); err != nil {
			t.Fatalf("SetBridgeState failed: %v", err)
		}

		got, ok, err := db.GetBridgeState(ctx, key)
		if err != nil {
			t.Fatalf("GetBridgeState failed: %v", err)
		}
		if !ok {
			t.Error("expected state to exist")
		}
		if got != value {
			t.Errorf("expected value %q, got %q", value, got)
		}
	})
}

func FuzzNormalizeMessageLimit(f *testing.F) {
	f.Fuzz(func(t *testing.T, limit int) {
		got := normalizeMessageLimit(limit)
		if got < 1 || got > 500 {
			t.Errorf("normalizeMessageLimit returned out of range: %d", got)
		}
	})
}

func FuzzNormalizeMessageSort(f *testing.F) {
	f.Fuzz(func(t *testing.T, sort string) {
		got := normalizeMessageSort(sort)
		validSorts := map[string]bool{
			"newest":        true,
			"oldest":        true,
			"attempts_desc": true,
			"attempts_asc":  true,
			"type_asc":      true,
			"type_desc":     true,
			"state_asc":     true,
			"state_desc":    true,
		}
		if !validSorts[got] {
			t.Errorf("normalizeMessageSort returned invalid sort: %s", got)
		}
	})
}

func FuzzMessageOrderClause(f *testing.F) {
	f.Fuzz(func(t *testing.T, sort string) {
		got := messageOrderClause(sort)
		if got == "" {
			t.Error("messageOrderClause returned empty string")
		}
	})
}

func FuzzNormalizeMessageDirection(f *testing.F) {
	f.Fuzz(func(t *testing.T, direction string) {
		got := normalizeMessageDirection(direction)
		if got != "prev" && got != "next" {
			t.Errorf("normalizeMessageDirection returned invalid direction: %s", got)
		}
	})
}

func FuzzNormalizeBotDirectorySort(f *testing.F) {
	f.Fuzz(func(t *testing.T, sort string) {
		got := normalizeBotDirectorySort(sort)
		validSorts := map[string]bool{
			"newest":        true,
			"deferred_desc": true,
			"activity_desc": true,
		}
		if !validSorts[got] {
			t.Errorf("normalizeBotDirectorySort returned invalid sort: %s", got)
		}
	})
}

func FuzzMessageListQuery(f *testing.F) {
	f.Fuzz(func(t *testing.T, search, msgType, state, sort string, limit int, atDID string) {
		query := MessageListQuery{
			Search:   search,
			Type:     msgType,
			State:    state,
			Sort:     sort,
			Limit:    limit,
			ATDID:    atDID,
			HasIssue: false,
		}

		got := normalizeMessageListQuery(query)

		if got.Search != search {
			t.Error("search should be preserved")
		}
		if got.Limit < 1 || got.Limit > 500 {
			t.Errorf("limit out of range: %d", got.Limit)
		}
	})
}
