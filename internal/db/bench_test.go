package db

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func BenchmarkAddMessage(b *testing.B) {
	db, err := Open(":memory:?parseTime=true")
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		msg := Message{
			ATURI:        "at://did:plc:bench/app.bsky.feed.post/" + string(rune(i)),
			ATCID:        "bafy-bench-" + string(rune(i)),
			ATDID:        "did:plc:bench",
			Type:         "app.bsky.feed.post",
			MessageState: MessageStatePending,
			RawATJson:    `{"text":"benchmark test"}`,
		}
		if err := db.AddMessage(ctx, msg); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGetMessage(b *testing.B) {
	db, err := Open(":memory:?parseTime=true")
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()

	msg := Message{
		ATURI:        "at://did:plc:bench/app.bsky.feed.post/get",
		ATCID:        "bafy-get",
		ATDID:        "did:plc:bench",
		Type:         "app.bsky.feed.post",
		MessageState: MessageStatePublished,
		RawATJson:    `{"text":"test"}`,
	}
	if err := db.AddMessage(ctx, msg); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := db.GetMessage(ctx, msg.ATURI)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkListMessages(b *testing.B) {
	db, err := Open(":memory:?parseTime=true")
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()

	for i := 0; i < 100; i++ {
		msg := Message{
			ATURI:        fmt.Sprintf("at://did:plc:bench/app.bsky.feed.post/list%d", i),
			ATCID:        fmt.Sprintf("bafy-list%d", i),
			ATDID:        "did:plc:bench",
			Type:         "app.bsky.feed.post",
			MessageState: MessageStatePublished,
		}
		if err := db.AddMessage(ctx, msg); err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := db.ListMessages(ctx, MessageListQuery{Limit: 50, Sort: "newest"})
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkAddBridgedAccount(b *testing.B) {
	db, err := Open(":memory:?parseTime=true")
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		acc := BridgedAccount{
			ATDID:     "did:plc:bench-" + string(rune(i)),
			SSBFeedID: "@bench.ed25519",
			Active:    true,
		}
		if err := db.AddBridgedAccount(ctx, acc); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkAddBlob(b *testing.B) {
	db, err := Open(":memory:?parseTime=true")
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		blob := Blob{
			ATCID:      "bafy-blob-" + string(rune(i)),
			SSBBlobRef: "&blob" + string(rune(i)) + ".sha256",
			Size:       1024,
			MimeType:   "image/png",
		}
		if err := db.AddBlob(ctx, blob); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSetBridgeState(b *testing.B) {
	db, err := Open(":memory:?parseTime=true")
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := db.SetBridgeState(ctx, "bench-key", "bench-value"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGetBridgeState(b *testing.B) {
	db, err := Open(":memory:?parseTime=true")
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()

	if err := db.SetBridgeState(ctx, "bench-key", "bench-value"); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, err := db.GetBridgeState(ctx, "bench-key")
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGetRecentMessages(b *testing.B) {
	db, err := Open(":memory:?parseTime=true")
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()

	for i := 0; i < 50; i++ {
		msg := Message{
			ATURI:        "at://did:plc:bench/app.bsky.feed.post/recent" + string(rune(i)),
			ATCID:        "bafy-recent" + string(rune(i)),
			ATDID:        "did:plc:bench",
			Type:         "app.bsky.feed.post",
			MessageState: MessageStatePublished,
		}
		if err := db.AddMessage(ctx, msg); err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := db.GetRecentMessages(ctx, 20)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCountMessages(b *testing.B) {
	db, err := Open(":memory:?parseTime=true")
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()

	for i := 0; i < 50; i++ {
		msg := Message{
			ATURI:        "at://did:plc:bench/app.bsky.feed.post/count" + string(rune(i)),
			ATCID:        "bafy-count" + string(rune(i)),
			ATDID:        "did:plc:bench",
			Type:         "app.bsky.feed.post",
			MessageState: MessageStatePublished,
		}
		if err := db.AddMessage(ctx, msg); err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := db.CountMessages(ctx)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCheckBridgeHealth(b *testing.B) {
	db, err := Open(":memory:?parseTime=true")
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()

	db.SetBridgeState(ctx, "bridge_runtime_status", "live")
	db.SetBridgeState(ctx, "bridge_runtime_last_heartbeat_at", time.Now().UTC().Format(time.RFC3339))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := db.CheckBridgeHealth(ctx, 60*time.Second)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkListMessagesPage(b *testing.B) {
	db, err := Open(":memory:?parseTime=true")
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()

	for i := 0; i < 100; i++ {
		msg := Message{
			ATURI:        fmt.Sprintf("at://did:plc:bench/app.bsky.feed.post/page%d", i),
			ATCID:        fmt.Sprintf("bafy-page%d", i),
			ATDID:        "did:plc:bench",
			Type:         "app.bsky.feed.post",
			MessageState: MessageStatePublished,
		}
		if err := db.AddMessage(ctx, msg); err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := db.ListMessagesPage(ctx, MessageListQuery{Limit: 10, Sort: "newest"})
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGetRetryCandidates(b *testing.B) {
	db, err := Open(":memory:?parseTime=true")
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()

	for i := 0; i < 20; i++ {
		msg := Message{
			ATURI:           "at://did:plc:bench/app.bsky.feed.post/retry" + string(rune(i)),
			ATCID:           "bafy-retry" + string(rune(i)),
			ATDID:           "did:plc:bench",
			Type:            "app.bsky.feed.post",
			MessageState:    MessageStateFailed,
			PublishError:    "error",
			PublishAttempts: 1,
		}
		if err := db.AddMessage(ctx, msg); err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := db.GetRetryCandidates(ctx, 10, "", 8)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkListActiveBridgedAccountsWithStats(b *testing.B) {
	db, err := Open(":memory:?parseTime=true")
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()

	for i := 0; i < 20; i++ {
		acc := BridgedAccount{
			ATDID:     "did:plc:bench-" + string(rune(i)),
			SSBFeedID: "@bench" + string(rune(i)) + ".ed25519",
			Active:    true,
		}
		if err := db.AddBridgedAccount(ctx, acc); err != nil {
			b.Fatal(err)
		}

		for j := 0; j < 5; j++ {
			msg := Message{
				ATURI:        "at://did:plc:bench-" + string(rune(i)) + "/app.bsky.feed.post/m" + string(rune(j)),
				ATCID:        "bafy-m" + string(rune(i)) + string(rune(j)),
				ATDID:        "did:plc:bench-" + string(rune(i)),
				Type:         "app.bsky.feed.post",
				MessageState: MessageStatePublished,
			}
			if err := db.AddMessage(ctx, msg); err != nil {
				b.Fatal(err)
			}
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := db.ListActiveBridgedAccountsWithStats(ctx)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGetPublishFailures(b *testing.B) {
	db, err := Open(":memory:?parseTime=true")
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()

	for i := 0; i < 30; i++ {
		msg := Message{
			ATURI:           "at://did:plc:bench/app.bsky.feed.post/fail" + string(rune(i)),
			ATCID:           "bafy-fail" + string(rune(i)),
			ATDID:           "did:plc:bench",
			Type:            "app.bsky.feed.post",
			MessageState:    MessageStateFailed,
			PublishError:    "test error",
			PublishAttempts: 1,
		}
		if err := db.AddMessage(ctx, msg); err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := db.GetPublishFailures(ctx, 10)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGetRecentBlobs(b *testing.B) {
	db, err := Open(":memory:?parseTime=true")
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()

	for i := 0; i < 50; i++ {
		blob := Blob{
			ATCID:      "bafy-blob" + string(rune(i)),
			SSBBlobRef: "&blob" + string(rune(i)) + ".sha256",
			Size:       1024,
			MimeType:   "image/png",
		}
		if err := db.AddBlob(ctx, blob); err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := db.GetRecentBlobs(ctx, 20)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGetAllBridgeState(b *testing.B) {
	db, err := Open(":memory:?parseTime=true")
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()

	for i := 0; i < 50; i++ {
		if err := db.SetBridgeState(ctx, "key-"+string(rune(i)), "value-"+string(rune(i))); err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := db.GetAllBridgeState(ctx)
		if err != nil {
			b.Fatal(err)
		}
	}
}
