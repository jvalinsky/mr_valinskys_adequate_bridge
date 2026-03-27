package mapper

import "testing"

func TestMapPostRichTextReplyAndQuote(t *testing.T) {
	rawJSON := []byte(`{
		"text": "Hi @bob visit bsky.app #bridge",
		"facets": [
			{
				"index": {"byteStart": 3, "byteEnd": 7},
				"features": [{"$type":"app.bsky.richtext.facet#mention","did":"did:plc:bob"}]
			},
			{
				"index": {"byteStart": 14, "byteEnd": 22},
				"features": [{"$type":"app.bsky.richtext.facet#link","uri":"https://bsky.app/profile/did:plc:bob"}]
			},
			{
				"index": {"byteStart": 23, "byteEnd": 30},
				"features": [{"$type":"app.bsky.richtext.facet#tag","tag":"bridge"}]
			}
		],
		"tags": ["ssb"],
		"reply": {
			"root": {"uri":"at://did:plc:carol/app.bsky.feed.post/root","cid":"bafy-root"},
			"parent": {"uri":"at://did:plc:dan/app.bsky.feed.post/parent","cid":"bafy-parent"}
		},
		"embed": {
			"$type": "app.bsky.embed.record",
			"record": {"uri":"at://did:plc:erin/app.bsky.feed.post/quote","cid":"bafy-quote"}
		},
		"createdAt": "2023-01-01T00:00:00Z"
	}`)

	res, err := MapRecord(RecordTypePost, "did:plc:alice", rawJSON)
	if err != nil {
		t.Fatalf("MapRecord failed: %v", err)
	}

	if got := res["text"]; got != "Hi @bob visit [bsky.app](https://bsky.app/profile/did:plc:bob) #bridge" {
		t.Fatalf("unexpected rewritten text: %v", got)
	}
	if got := res["_atproto_reply_root"]; got != "at://did:plc:carol/app.bsky.feed.post/root" {
		t.Fatalf("unexpected root placeholder: %v", got)
	}
	if got := res["_atproto_reply_parent"]; got != "at://did:plc:dan/app.bsky.feed.post/parent" {
		t.Fatalf("unexpected parent placeholder: %v", got)
	}
	if got := res["_atproto_quote_subject"]; got != "at://did:plc:erin/app.bsky.feed.post/quote" {
		t.Fatalf("unexpected quote placeholder: %v", got)
	}

	mentions, ok := res["mentions"].([]map[string]interface{})
	if !ok {
		t.Fatalf("expected []map[string]interface{} mentions, got %T", res["mentions"])
	}
	if len(mentions) != 3 {
		t.Fatalf("expected 3 mentions, got %+v", mentions)
	}
	if mentions[0]["link"] != "did:plc:bob" || mentions[0]["name"] != "@bob" {
		t.Fatalf("unexpected mention facet mapping: %+v", mentions[0])
	}
	if mentions[1]["link"] != "#bridge" || mentions[1]["name"] != "#bridge" {
		t.Fatalf("unexpected hashtag facet mapping: %+v", mentions[1])
	}
	if mentions[2]["link"] != "#ssb" || mentions[2]["name"] != "#ssb" {
		t.Fatalf("unexpected additional tag mapping: %+v", mentions[2])
	}
}

func TestMapFollowBlockAndProfile(t *testing.T) {
	follow, err := MapRecord(RecordTypeFollow, "did:plc:alice", []byte(`{"subject":"did:plc:bob","createdAt":"2023-01-01T00:00:00Z"}`))
	if err != nil {
		t.Fatalf("map follow: %v", err)
	}
	if follow["following"] != true || follow["blocking"] != false {
		t.Fatalf("unexpected follow mapping: %+v", follow)
	}

	block, err := MapRecord(RecordTypeBlock, "did:plc:alice", []byte(`{"subject":"did:plc:bob","createdAt":"2023-01-01T00:00:00Z"}`))
	if err != nil {
		t.Fatalf("map block: %v", err)
	}
	if block["following"] != false || block["blocking"] != true {
		t.Fatalf("unexpected block mapping: %+v", block)
	}

	profile, err := MapRecord(RecordTypeProfile, "did:plc:alice", []byte(`{
		"displayName":"Alice",
		"description":"Bridge bio",
		"createdAt":"2023-01-01T00:00:00Z"
	}`))
	if err != nil {
		t.Fatalf("map profile: %v", err)
	}
	if profile["type"] != "about" {
		t.Fatalf("unexpected profile type: %+v", profile)
	}
	if profile["_atproto_about_did"] != "did:plc:alice" {
		t.Fatalf("unexpected about placeholder: %+v", profile)
	}
	if profile["name"] != "Alice" || profile["description"] != "Bridge bio" {
		t.Fatalf("unexpected profile text mapping: %+v", profile)
	}
}

func TestReplaceATProtoRefsResolvesQuoteAndDropsUnresolvedMentionDIDs(t *testing.T) {
	msg := map[string]interface{}{
		"type":                   "about",
		"_atproto_about_did":     "did:plc:alice",
		"_atproto_quote_subject": "at://did:plc:quote/app.bsky.feed.post/1",
		"text":                   "hello",
		"mentions": []map[string]interface{}{
			{"link": "did:plc:resolved", "name": "@resolved"},
			{"link": "did:plc:missing", "name": "@missing"},
		},
	}

	ReplaceATProtoRefs(
		msg,
		func(uri string) string {
			if uri == "at://did:plc:quote/app.bsky.feed.post/1" {
				return "%quote.sha256"
			}
			return ""
		},
		func(did string) string {
			switch did {
			case "did:plc:alice":
				return "@alice.ed25519"
			case "did:plc:resolved":
				return "@resolved.ed25519"
			default:
				return ""
			}
		},
	)

	if msg["about"] != "@alice.ed25519" {
		t.Fatalf("expected resolved about field, got %+v", msg)
	}
	if got := msg["text"]; got != "hello\n\n[quoted post](%quote.sha256)" {
		t.Fatalf("unexpected quote text: %v", got)
	}
	mentions, ok := msg["mentions"].([]map[string]interface{})
	if !ok {
		t.Fatalf("expected normalized mentions, got %T", msg["mentions"])
	}
	if len(mentions) != 2 {
		t.Fatalf("expected resolved mention plus quote mention, got %+v", mentions)
	}
	if mentions[0]["link"] != "@resolved.ed25519" {
		t.Fatalf("expected resolved DID mention, got %+v", mentions[0])
	}
	if mentions[1]["link"] != "%quote.sha256" || mentions[1]["name"] != "quoted post" {
		t.Fatalf("expected quote mention, got %+v", mentions[1])
	}
	if unresolved := UnresolvedATProtoRefs(msg); len(unresolved) != 0 {
		t.Fatalf("expected no unresolved placeholders, got %v", unresolved)
	}
}
