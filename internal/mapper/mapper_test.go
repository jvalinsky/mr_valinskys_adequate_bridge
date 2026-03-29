package mapper

import (
	"testing"

	appbsky "github.com/bluesky-social/indigo/api/bsky"
)

func TestMapLike(t *testing.T) {
	rawJSON := []byte(`{"subject":{"uri":"at://did:plc:bob/app.bsky.feed.post/123","cid":"bafy-abc"},"createdAt":"2023-01-01T00:00:00Z"}`)

	res, err := MapRecord(RecordTypeLike, "did:plc:alice", rawJSON)
	if err != nil {
		t.Fatalf("MapRecord failed: %v", err)
	}

	if res["type"] != "vote" {
		t.Fatalf("expected type vote, got %v", res["type"])
	}
	vote, ok := res["vote"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected vote map, got %T", res["vote"])
	}
	if vote["value"] != 1 {
		t.Fatalf("expected vote value 1, got %v", vote["value"])
	}
	if vote["expression"] != "Like" {
		t.Fatalf("expected expression Like, got %v", vote["expression"])
	}
	if res["_atproto_subject"] != "at://did:plc:bob/app.bsky.feed.post/123" {
		t.Fatalf("expected subject placeholder, got %v", res["_atproto_subject"])
	}
}

func TestMapRepost(t *testing.T) {
	rawJSON := []byte(`{"subject":{"uri":"at://did:plc:bob/app.bsky.feed.post/456","cid":"bafy-def"},"createdAt":"2023-01-01T00:00:00Z"}`)

	res, err := MapRecord(RecordTypeRepost, "did:plc:alice", rawJSON)
	if err != nil {
		t.Fatalf("MapRecord failed: %v", err)
	}

	if res["type"] != "post" {
		t.Fatalf("expected type post, got %v", res["type"])
	}
	if res["text"] != "" {
		t.Fatalf("expected empty text, got %v", res["text"])
	}
	if res["_atproto_repost_subject"] != "at://did:plc:bob/app.bsky.feed.post/456" {
		t.Fatalf("expected repost subject placeholder, got %v", res["_atproto_repost_subject"])
	}
}

func TestMapLikeInvalidJSON(t *testing.T) {
	_, err := MapRecord(RecordTypeLike, "did:plc:alice", []byte(`{invalid`))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestMapRepostInvalidJSON(t *testing.T) {
	_, err := MapRecord(RecordTypeRepost, "did:plc:alice", []byte(`{invalid`))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestMapFollowWithEmptySubject(t *testing.T) {
	rawJSON := []byte(`{"subject":"","createdAt":"2023-01-01T00:00:00Z"}`)
	res, err := MapRecord(RecordTypeFollow, "did:plc:alice", rawJSON)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res["_atproto_contact"] != "" {
		t.Fatalf("expected empty contact for empty subject, got %v", res["_atproto_contact"])
	}
}

func TestMapBlockWithEmptySubject(t *testing.T) {
	rawJSON := []byte(`{"subject":"","createdAt":"2023-01-01T00:00:00Z"}`)
	res, err := MapRecord(RecordTypeBlock, "did:plc:alice", rawJSON)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res["_atproto_contact"] != "" {
		t.Fatalf("expected empty contact for empty subject, got %v", res["_atproto_contact"])
	}
}

func TestMapFollowInvalidJSON(t *testing.T) {
	_, err := MapRecord(RecordTypeFollow, "did:plc:alice", []byte(`{invalid`))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestMapBlockInvalidJSON(t *testing.T) {
	_, err := MapRecord(RecordTypeBlock, "did:plc:alice", []byte(`{invalid`))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestMapProfileInvalidJSON(t *testing.T) {
	_, err := MapRecord(RecordTypeProfile, "did:plc:alice", []byte(`{invalid`))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestMapProfileWithNilFields(t *testing.T) {
	rawJSON := []byte(`{"createdAt":"2023-01-01T00:00:00Z"}`)

	res, err := MapRecord(RecordTypeProfile, "did:plc:alice", rawJSON)
	if err != nil {
		t.Fatalf("MapRecord failed: %v", err)
	}

	if res["type"] != "about" {
		t.Fatalf("expected type about, got %v", res["type"])
	}
	if res["_atproto_about_did"] != "did:plc:alice" {
		t.Fatalf("expected did placeholder, got %v", res["_atproto_about_did"])
	}
	if _, hasName := res["name"]; hasName {
		t.Fatalf("expected no name for nil displayName")
	}
	if _, hasDesc := res["description"]; hasDesc {
		t.Fatalf("expected no description for nil description")
	}
}

func TestQuoteSubjectURIWithMedia(t *testing.T) {
	rawJSON := []byte(`{
		"text": "check this out",
		"embed": {
			"$type": "app.bsky.embed.recordWithMedia",
			"media": {"$type": "app.bsky.embed.images"},
			"record": {
				"record": {"uri":"at://did:plc:bob/app.bsky.feed.post/embed","cid":"bafy-embed"}
			}
		},
		"createdAt": "2023-01-01T00:00:00Z"
	}`)

	res, err := MapRecord(RecordTypePost, "did:plc:alice", rawJSON)
	if err != nil {
		t.Fatalf("MapRecord failed: %v", err)
	}

	if res["_atproto_quote_subject"] != "at://did:plc:bob/app.bsky.feed.post/embed" {
		t.Fatalf("expected quote subject from recordWithMedia, got %v", res["_atproto_quote_subject"])
	}
}

func TestSanitizeForPublish(t *testing.T) {
	msg := map[string]interface{}{
		"type":                "post",
		"text":                "hello",
		"_atproto_reply_root": "some-value",
		"_atproto_subject":    "another-value",
		"_atproto_contact":    "contact-value",
		"_atproto_about_did":  "did-value",
		"regular_field":       "keep-me",
	}

	SanitizeForPublish(msg)

	if _, ok := msg["_atproto_reply_root"]; ok {
		t.Fatal("expected _atproto_reply_root to be removed")
	}
	if _, ok := msg["_atproto_subject"]; ok {
		t.Fatal("expected _atproto_subject to be removed")
	}
	if _, ok := msg["_atproto_contact"]; ok {
		t.Fatal("expected _atproto_contact to be removed")
	}
	if msg["type"] != "post" {
		t.Fatal("expected type to be preserved")
	}
	if msg["regular_field"] != "keep-me" {
		t.Fatal("expected regular_field to be preserved")
	}
}

func TestReadyForPublishContact(t *testing.T) {
	if !ReadyForPublish(map[string]interface{}{"type": "post", "text": "hello"}) {
		t.Fatal("post should be ready")
	}
	if ReadyForPublish(map[string]interface{}{"type": "contact"}) {
		t.Fatal("contact without contact field should not be ready")
	}
	if !ReadyForPublish(map[string]interface{}{"type": "contact", "contact": " @bob.ed25519"}) {
		t.Fatal("contact with non-empty contact field should be ready")
	}
	if ReadyForPublish(map[string]interface{}{"type": "vote"}) {
		t.Fatal("vote without vote.link should not be ready")
	}
	if !ReadyForPublish(map[string]interface{}{"type": "vote", "vote": map[string]interface{}{"link": " %msg.sha256"}}) {
		t.Fatal("vote with non-empty vote.link should be ready")
	}
}

func TestNormalizedMentionsWithInterfaceSlice(t *testing.T) {
	raw := []interface{}{
		map[string]interface{}{"link": "did:plc:bob", "name": "@bob"},
		map[string]interface{}{"link": "#bridge", "name": "#bridge"},
		"not a map",
		nil,
	}

	res := normalizedMentions(raw)
	if len(res) != 2 {
		t.Fatalf("expected 2 valid mentions, got %d", len(res))
	}
	if res[0]["link"] != "did:plc:bob" {
		t.Fatalf("expected first mention to be bob, got %v", res[0])
	}
}

func TestNormalizedMentionsWithNil(t *testing.T) {
	res := normalizedMentions(nil)
	if res != nil {
		t.Fatalf("expected nil for nil input, got %v", res)
	}
}

func TestNormalizedMentionsWithInvalidType(t *testing.T) {
	res := normalizedMentions("invalid")
	if res != nil {
		t.Fatalf("expected nil for invalid type, got %v", res)
	}
}

func TestAsString(t *testing.T) {
	if asString(nil) != "" {
		t.Fatal("expected empty string for nil")
	}
	if asString("hello") != "hello" {
		t.Fatalf("expected hello, got %s", asString("hello"))
	}
	if asString(123) != "123" {
		t.Fatalf("expected 123, got %s", asString(123))
	}
	if asString("") != "" {
		t.Fatal("expected empty string for empty string")
	}
}

func TestAppendMarkdownBlock(t *testing.T) {
	if appendMarkdownBlock("", "block") != "block" {
		t.Fatal("expected block for empty text")
	}
	if appendMarkdownBlock("text", "") != "text" {
		t.Fatal("expected text for empty block")
	}
	if appendMarkdownBlock("text\n\n", "block") != "text\n\nblock" {
		t.Fatal("expected text with block appended")
	}
	if appendMarkdownBlock("text", "block") != "text\n\nblock" {
		t.Fatal("expected text with block separated by newlines")
	}
}

func TestUnresolvedATProtoRefs(t *testing.T) {
	msg := map[string]interface{}{
		"_atproto_reply_root":   "at://did:plc:a/app.bsky.feed.post/1",
		"_atproto_reply_parent": "at://did:plc:b/app.bsky.feed.post/2",
		"text":                  "hello",
	}

	unresolved := UnresolvedATProtoRefs(msg)
	if len(unresolved) != 2 {
		t.Fatalf("expected 2 unresolved refs, got %d: %v", len(unresolved), unresolved)
	}
}

func TestUnresolvedATProtoRefsWithEmptyValues(t *testing.T) {
	msg := map[string]interface{}{
		"_atproto_reply_root":   "",
		"_atproto_reply_parent": "   ",
		"text":                  "hello",
	}

	unresolved := UnresolvedATProtoRefs(msg)
	if len(unresolved) != 0 {
		t.Fatalf("expected 0 unresolved refs for empty values, got %d", len(unresolved))
	}
}

func TestAppendUniqueMentionDeduplicates(t *testing.T) {
	mentions := []map[string]interface{}{}
	seen := make(map[string]struct{})

	mentions = appendUniqueMention(mentions, seen, map[string]interface{}{"link": "@bob.ed25519", "name": "@bob"})
	mentions = appendUniqueMention(mentions, seen, map[string]interface{}{"link": "@bob.ed25519", "name": "@bob"})
	mentions = appendUniqueMention(mentions, seen, map[string]interface{}{"link": "@alice.ed25519", "name": "@alice"})

	if len(mentions) != 2 {
		t.Fatalf("expected 2 mentions after dedup, got %d", len(mentions))
	}
}

func TestAppendUniqueMentionSkipsEmptyLink(t *testing.T) {
	mentions := []map[string]interface{}{}
	seen := make(map[string]struct{})

	mentions = appendUniqueMention(mentions, seen, map[string]interface{}{"link": "  ", "name": "empty"})
	if len(mentions) != 0 {
		t.Fatalf("expected 0 mentions for empty link, got %d", len(mentions))
	}
}

func TestRewriteFacetSegmentWithNilFacet(t *testing.T) {
	if rewriteFacetSegment("text", nil) != "text" {
		t.Fatal("expected text unchanged for nil facet")
	}
}

func TestRewriteFacetSegmentWithLink(t *testing.T) {
	segment := "click here"
	facet := &appbsky.RichtextFacet{
		Features: []*appbsky.RichtextFacet_Features_Elem{
			{RichtextFacet_Link: &appbsky.RichtextFacet_Link{Uri: "https://example.com"}},
		},
	}

	result := rewriteFacetSegment(segment, facet)
	if result != "[click here](https://example.com)" {
		t.Fatalf("expected markdown link, got %s", result)
	}
}

func TestRewriteFacetSegmentWithEmptyUri(t *testing.T) {
	segment := "link"
	facet := &appbsky.RichtextFacet{
		Features: []*appbsky.RichtextFacet_Features_Elem{
			{RichtextFacet_Link: &appbsky.RichtextFacet_Link{Uri: "  "}},
		},
	}

	result := rewriteFacetSegment(segment, facet)
	if result != "link" {
		t.Fatalf("expected text unchanged for empty URI, got %s", result)
	}
}

func TestMapRecordUnsupportedType(t *testing.T) {
	_, err := MapRecord("app.bsky.feed.notsupported", "did:plc:alice", []byte(`{}`))
	if err == nil {
		t.Fatal("expected error for unsupported type")
	}
}

func TestRewriteRichTextWithInvalidFacets(t *testing.T) {
	text := "hello world"
	facets := []*appbsky.RichtextFacet{
		nil,
		{Index: nil},
		{Index: &appbsky.RichtextFacet_ByteSlice{ByteStart: -1, ByteEnd: 5}},
		{Index: &appbsky.RichtextFacet_ByteSlice{ByteStart: 0, ByteEnd: 20}},
		{Index: &appbsky.RichtextFacet_ByteSlice{ByteStart: 3, ByteEnd: 2}},
	}

	result, mentions := rewriteRichText(text, facets, nil)
	if result != "hello world" {
		t.Fatalf("expected unchanged text for invalid facets, got %s", result)
	}
	if len(mentions) != 0 {
		t.Fatalf("expected no mentions for invalid facets, got %d", len(mentions))
	}
}

func TestRewriteRichTextWithTags(t *testing.T) {
	text := "post"
	tags := []string{"ssb", "#bridge", "  ", "#", "bridge"}

	_, mentions := rewriteRichText(text, nil, tags)
	if len(mentions) != 2 {
		t.Fatalf("expected 2 tag mentions, got %d: %v", len(mentions), mentions)
	}
}

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
