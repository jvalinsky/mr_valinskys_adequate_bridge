package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/db"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/mapper"
)

func main() {
	dbPath := "demo.sqlite"
	repoPath := "demo-repo"
	if len(os.Args) > 1 {
		dbPath = os.Args[1]
	}
	if len(os.Args) > 2 {
		repoPath = os.Args[2]
	}

	_ = os.Remove(dbPath)
	_ = os.RemoveAll(repoPath)

	database, err := db.Open(dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer database.Close()

	ctx := context.Background()

	// 1. Setup blob storage
	blobDir := filepath.Join(repoPath, "blobs")
	if err := os.MkdirAll(blobDir, 0755); err != nil {
		log.Fatal(err)
	}

	writeBlob := func(data []byte) (string, string) {
		h := sha256.Sum256(data)
		hexHash := fmt.Sprintf("%x", h)
		b64Hash := base64.StdEncoding.EncodeToString(h[:])
		ref := "&" + b64Hash + ".sha256"

		if err := os.WriteFile(filepath.Join(blobDir, hexHash), data, 0644); err != nil {
			log.Fatal(err)
		}
		return hexHash, ref
	}

	// Create a small fake JPEG (red square)
	redJPEG := []byte("\xff\xd8\xff\xe0\x00\x10JFIF\x00\x01\x01\x01\x00H\x00H\x00\x00\xff\xdb\x00C\x00\x08\x06\x06\x07\x06\x05\x08\x07\x07\x07\t\t\x08\n\x0c\x14\x08\x08\x0b\x0b\x0b\x19\x12\x13\x0f\x14\x1d\x1a\x1f\x1e\x1d\x1a\x1c\x1c $.' \",#\x1c\x1c(7),01444\x1f'9=82<.342\xff\xc0\x00\x11\x08\x00\x01\x00\x01\x03\x01\x22\x00\x02\x11\x01\x03\x11\x01\xff\xc4\x00\x1f\x00\x00\x01\x05\x01\x01\x01\x01\x01\x01\x00\x00\x00\x00\x00\x00\x00\x00\x01\x02\x03\x04\x05\x06\x07\x08\t\n\x0b\xff\xc4\x00\xb5\x10\x00\x02\x01\x03\x03\x02\x04\x03\x05\x05\x04\x04\x00\x00\x01\x7d\x01\x02\x03\x00\x04\x11\x05\x12!1A\x06\x13Qa\x07\"q\x142\x81\x91\xa1\x08#B\xb1\xc1\x15R\xd1\xf0$3br\x82\x16\x17\x18\x19\x1a%&'()*456789:CDEFGHIJSTUVWXYZcdefghijstuvwxyz\x83\x84\x85\x86\x87\x88\x89\x8a\x92\x93\x94\x95\x96\x97\x98\x99\x9a\xa2\xa3\xa4\xa5\xa6\xa7\xa8\xa9\xaa\xb2\xb3\xb4\xb5\xb6\xb7\xb8\xb9\xba\xc2\xc3\xc4\xc5\xc6\xc7\xc8\xc9\xca\xd2\xd3\xd4\xd5\xd6\xd7\xd8\xd9\xda\xe1\xe2\xe3\xe4\xe5\xe6\xe7\xe8\xe9\xea\xf1\xf2\xf3\xf4\xf5\xf6\xf7\xf8\xf9\xfa\xff\xda\x00\x0c\x03\x01\x00\x02\x11\x03\x11\x00?\x00\xf7\xfa\x28\xa2\x8a\x28\xa2\x8a\x28\xa2\x8a\x28\xa2\x8a\x28\xa2\x8a\x28\xa2\x8a\x28\xa2\x8a\x28\xa2\x8a\x28\xa2\x8a\x28\xa2\x8a\x28\xa2\x8a\x28\xa2\x8a\x28\xa2\x8a\x28\xa2\x8a\x28\xa2\x8a\x28\xa2\x8a\x28\xa2\x8a\x28\xa2\x8a\x28\xa2\x8a\x28\xa2\x8a\x28\xa2\x8a\x28\xa2\x8a\x28\xa2\x8a\x28\xa2\x8a\x28\xa2\x8a\x28\xa2\x8a\x28\xa2\x8a\x28\xa2\x8a\x28\xa2\x8a\x28\xa2\x8a\x28\xa2\x8a\x28\xa2\x8a\x28\xa2\x8a\x28\xa2\x8a\x28\xa2\x8a\x28\xa2\x8a\x28\xa2\x8a\x28\xa2\x8a\x28\xa2\x8a\x28\xa2\x8a\xff\xd9")
	_, imageRef := writeBlob(redJPEG)
	imageCID := "bafkreidv6ia7sq36dk66uvtyvtw6mcwq7ny7axvshvfuioyfgnxh7khawy"

	// Create a fake video file
	videoData := []byte("FAKE VIDEO DATA - MP4 HEADER")
	_, videoRef := writeBlob(videoData)
	videoCID := "bafybeidv6ia7sq36dk66uvtyvtw6mcwq7ny7axvshvfuioyfgnxh7khawy"

	// 2. Add some bridged accounts
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

	// 3. Add some messages
	now := time.Now().UTC()

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
						"size": ` + fmt.Sprintf("%d", len(redJPEG)) + `
					}
				}
			]
		}
	}`

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
				"size": ` + fmt.Sprintf("%d", len(videoData)) + `
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
			RawSSBJson:   `{"type": "post", "text": "Check out this sunset! 🌅 ![` + "Preview" + `](` + imageRef + `)", "mentions": [{"link": "` + imageRef + `", "name": "A beautiful orange sunset over the ocean", "type": "image/jpeg"}]}`,
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
			RawSSBJson:   `{"type": "post", "text": "My new vlog is up! [Vlog episode 1](` + videoRef + `)", "mentions": [{"link": "` + videoRef + `", "name": "Vlog episode 1", "type": "video/mp4"}]}`,
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

	// 4. Add blobs to DB
	database.AddBlob(ctx, db.Blob{
		ATCID:      imageCID,
		SSBBlobRef: imageRef,
		Size:       int64(len(redJPEG)),
		MimeType:   "image/jpeg",
	})
	database.AddBlob(ctx, db.Blob{
		ATCID:      videoCID,
		SSBBlobRef: videoRef,
		Size:       int64(len(videoData)),
		MimeType:   "video/mp4",
	})

	// 5. Add some bridge state
	database.SetBridgeState(ctx, "firehose_seq", "123456789")
	database.SetBridgeState(ctx, "bridge_runtime_status", "live")
	database.SetBridgeState(ctx, "bridge_runtime_last_heartbeat_at", now.Format(time.RFC3339))

	// 6. Add some known peers
	peers := []db.KnownPeer{
		{Addr: "ssb.nz:8008", PubKey: make([]byte, 32), CreatedAt: now},
		{Addr: "127.0.0.1:8008", PubKey: make([]byte, 32), CreatedAt: now},
	}
	for _, p := range peers {
		database.AddKnownPeer(ctx, p)
	}

	fmt.Printf("Demo database initialized at %s\n", dbPath)
	fmt.Printf("Demo repo initialized at %s\n", repoPath)
}
