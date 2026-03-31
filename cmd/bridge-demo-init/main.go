package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/db"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/mapper"
)

func main() {
	dbPath := "demo.sqlite"
	if len(os.Args) > 1 {
		dbPath = os.Args[1]
	}

	_ = os.Remove(dbPath)

	database, err := db.Open(dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer database.Close()

	ctx := context.Background()

	// 1. Add some bridged accounts
	accounts := []db.BridgedAccount{
		{ATDID: "did:plc:alice", SSBFeedID: "@alice.ed25519", Active: true},
		{ATDID: "did:plc:bob", SSBFeedID: "@bob.ed25519", Active: true},
		{ATDID: "did:plc:carol", SSBFeedID: "@carol.ed25519", Active: false},
	}

	for _, acc := range accounts {
		if err := database.AddBridgedAccount(ctx, acc); err != nil {
			log.Fatal(err)
		}
	}

	// 2. Add some messages
	now := time.Now().UTC()

	// Image post example
	imageCID := "bafkreidv6ia7sq36dk66uvtyvtw6mcwq7ny7axvshvfuioyfgnxh7khawy"
	imageBlobJSON := `{
		"$type": "app.bsky.feed.post",
		"text": "Check out this sunset! 🌅",
		"createdAt": "2026-03-31T11:00:00Z",
		"embed": {
			"$type": "app.bsky.embed.images",
			"images": [
				{
					"alt": "A beautiful orange sunset over the ocean",
					"image": {
						"$type": "blob",
						"ref": { "$link": "` + imageCID + `" },
						"mimeType": "image/jpeg",
						"size": 102456
					}
				}
			]
		}
	}`

	// Video post example
	videoCID := "bafybeidv6ia7sq36dk66uvtyvtw6mcwq7ny7axvshvfuioyfgnxh7khawy"
	videoThumbCID := "bafkreidv6ia7sq36dk66uvtyvtw6mcwq7ny7axvshvfuioyfgnxh7khawy"
	videoBlobJSON := `{
		"$type": "app.bsky.feed.post",
		"text": "My new vlog is up!",
		"createdAt": "2026-03-31T11:15:00Z",
		"embed": {
			"$type": "app.bsky.embed.video",
			"video": {
				"$type": "blob",
				"ref": { "$link": "` + videoCID + `" },
				"mimeType": "video/mp4",
				"size": 5024560
			},
			"alt": "Vlog episode 1"
		}
	}`

	messages := []db.Message{
		{
			ATURI:        "at://did:plc:alice/app.bsky.feed.post/1",
			ATCID:        "bafy1",
			ATDID:        "did:plc:alice",
			Type:         mapper.RecordTypePost,
			MessageState: db.MessageStatePublished,
			RawATJson:    `{"$type": "app.bsky.feed.post", "text": "Hello SSB! #demo", "createdAt": "2026-03-31T10:00:00Z"}`,
			RawSSBJson:   `{"type": "post", "text": "Hello SSB! #demo"}`,
			SSBMsgRef:    "%msg1.sha256",
			PublishedAt:  &now,
			CreatedAt:    now.Add(-1 * time.Hour),
		},
		{
			ATURI:        "at://did:plc:bob/app.bsky.feed.post/2",
			ATCID:        "bafy2",
			ATDID:        "did:plc:bob",
			Type:         mapper.RecordTypePost,
			MessageState: db.MessageStatePublished,
			RawATJson:    `{"$type": "app.bsky.feed.post", "text": "Replicating from Bluesky to Scuttlebutt", "createdAt": "2026-03-31T10:05:00Z"}`,
			RawSSBJson:   `{"type": "post", "text": "Replicating from Bluesky to Scuttlebutt"}`,
			SSBMsgRef:    "%msg2.sha256",
			PublishedAt:  &now,
			CreatedAt:    now.Add(-55 * time.Minute),
		},
		{
			ATURI:        "at://did:plc:alice/app.bsky.feed.post/img1",
			ATCID:        "bafy-img",
			ATDID:        "did:plc:alice",
			Type:         mapper.RecordTypePost,
			MessageState: db.MessageStatePublished,
			RawATJson:    imageBlobJSON,
			RawSSBJson:   `{"type": "post", "text": "Check out this sunset! 🌅 ![A beautiful orange sunset over the ocean](&` + imageCID + `.sha256)", "mentions": [{"link": "&` + imageCID + `.sha256", "name": "A beautiful orange sunset over the ocean", "type": "image/jpeg"}]}`,
			SSBMsgRef:    "%msg-img.sha256",
			PublishedAt:  &now,
			CreatedAt:    now.Add(-30 * time.Minute),
		},
		{
			ATURI:        "at://did:plc:bob/app.bsky.feed.post/vid1",
			ATCID:        "bafy-vid",
			ATDID:        "did:plc:bob",
			Type:         mapper.RecordTypePost,
			MessageState: db.MessageStatePublished,
			RawATJson:    videoBlobJSON,
			RawSSBJson:   `{"type": "post", "text": "My new vlog is up! [Vlog episode 1](&` + videoCID + `.sha256)", "mentions": [{"link": "&` + videoCID + `.sha256", "name": "Vlog episode 1", "type": "video/mp4"}]}`,
			SSBMsgRef:    "%msg-vid.sha256",
			PublishedAt:  &now,
			CreatedAt:    now.Add(-20 * time.Minute),
		},
		{
			ATURI:        "at://did:plc:alice/app.bsky.feed.post/3",
			ATCID:        "bafy3",
			ATDID:        "did:plc:alice",
			Type:         mapper.RecordTypePost,
			MessageState: db.MessageStateFailed,
			RawATJson:    `{"$type": "app.bsky.feed.post", "text": "This one failed to publish", "createdAt": "2026-03-31T10:10:00Z"}`,
			PublishError: "connection refused: sbot not responding",
			CreatedAt:    now.Add(-50 * time.Minute),
		},
		{
			ATURI:        "at://did:plc:bob/app.bsky.feed.post/4",
			ATCID:        "bafy4",
			ATDID:        "did:plc:bob",
			Type:         mapper.RecordTypePost,
			MessageState: db.MessageStateDeferred,
			RawATJson:    `{"$type": "app.bsky.feed.post", "text": "Waiting for parent...", "reply": {"root": {"uri": "at://did:plc:unknown/app.bsky.feed.post/x", "cid": "bafy-missing"}}}`,
			DeferReason:  "_atproto_reply_root=at://did:plc:unknown/app.bsky.feed.post/x",
			CreatedAt:    now.Add(-45 * time.Minute),
		},
	}

	for _, msg := range messages {
		if err := database.AddMessage(ctx, msg); err != nil {
			log.Fatal(err)
		}
	}

	// 3. Add blobs
	err = database.AddBlob(ctx, db.Blob{
		ATCID:      imageCID,
		SSBBlobRef: "&" + imageCID + ".sha256",
		Size:       102456,
		MimeType:   "image/jpeg",
	})
	if err != nil {
		log.Fatal(err)
	}

	database.AddBlob(ctx, db.Blob{
		ATCID:      videoCID,
		SSBBlobRef: "&" + videoCID + ".sha256",
		Size:       5024560,
		MimeType:   "video/mp4",
	})
	database.AddBlob(ctx, db.Blob{
		ATCID:      videoThumbCID,
		SSBBlobRef: "&" + videoThumbCID + ".sha256",
		Size:       54321,
		MimeType:   "image/jpeg",
	})

	// 4. Add some bridge state
	database.SetBridgeState(ctx, "firehose_seq", "123456789")
	database.SetBridgeState(ctx, "bridge_runtime_status", "live")
	database.SetBridgeState(ctx, "bridge_runtime_last_heartbeat_at", now.Format(time.RFC3339))

	// 5. Add some known peers
	peers := []db.KnownPeer{
		{Addr: "ssb.nz:8008", PubKey: make([]byte, 32), CreatedAt: now},
		{Addr: "127.0.0.1:8008", PubKey: make([]byte, 32), CreatedAt: now},
	}
	for _, p := range peers {
		database.AddKnownPeer(ctx, p)
	}

	fmt.Printf("Demo database initialized at %s\n", dbPath)
}
