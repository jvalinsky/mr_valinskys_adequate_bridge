package mapper

import (
	"testing"

	appbsky "github.com/jvalinsky/mr_valinskys_adequate_bridge/pkg/atproto/appbsky"
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

func TestMapPostWithInvalidEmbed(t *testing.T) {
	// Need to test the code paths in mapPost and appendFacetMentions that aren't fully covered
	rawJSON := []byte(`{
		"$type": "app.bsky.feed.post",
		"text": "Hello world",
		"createdAt": "2023-01-01T00:00:00Z",
		"embed": {
			"$type": "app.bsky.embed.recordWithMedia",
			"record": {
				"record": {
					"uri": "at://did:plc:alice/app.bsky.feed.post/3"
				}
			}
		}
	}`)
	res, err := MapRecord(RecordTypePost, "did:plc:alice", rawJSON)
	if err != nil {
		t.Fatalf("MapRecord failed: %v", err)
	}
	if quoteURI := res["_atproto_quote_subject"]; quoteURI != "at://did:plc:alice/app.bsky.feed.post/3" {
		t.Errorf("Expected quote_subject to be at://did:plc:alice/app.bsky.feed.post/3, got %v", quoteURI)
	}
}

func TestAppendFacetMentionsWithInvalidDID(t *testing.T) {
	// Test the fallback paths in appendFacetMentions and rewriteRichText
	rawJSON := []byte(`{
		"$type": "app.bsky.feed.post",
		"text": "Hello @unknown",
		"createdAt": "2023-01-01T00:00:00Z",
		"facets": [
			{
				"features": [
					{
						"$type": "app.bsky.richtext.facet#mention",
						"did": ""
					}
				],
				"index": {
					"byteStart": 6,
					"byteEnd": 14
				}
			}
		]
	}`)
	res, err := MapRecord(RecordTypePost, "did:plc:alice", rawJSON)
	if err != nil {
		t.Fatalf("MapRecord failed: %v", err)
	}
	// The text should be unchanged if the feature doesn't have a valid DID
	if res["text"] != "Hello @unknown" {
		t.Errorf("Expected text to be unchanged, got %v", res["text"])
	}
}

func TestReplaceATProtoRefsEdgeCases(t *testing.T) {
	lookupURI := func(uri string) string { return "%msg.sha256" }
	lookupDID := func(did string) string { return "@alice.ed25519" }

	msg := map[string]interface{}{
		"type":                   "post",
		"_atproto_quote_subject": "at://did:plc:alice/app.bsky.feed.post/1",
		// Test vote without map structure
		"vote":             "not-a-map",
		"_atproto_subject": "at://did:plc:alice/app.bsky.feed.post/1",
	}

	ReplaceATProtoRefs(msg, lookupURI, lookupDID)

	// Vote shouldn't be touched if it's not a map
	if msg["vote"] != "not-a-map" {
		t.Errorf("Expected vote to be unchanged, got %v", msg["vote"])
	}

	// Test mentions processing
	msg2 := map[string]interface{}{
		"type":                   "post",
		"_atproto_quote_subject": "at://did:plc:alice/app.bsky.feed.post/1",
		"mentions":               "not-a-slice", // invalid mentions type
	}
	ReplaceATProtoRefs(msg2, lookupURI, lookupDID)

	mentions, ok := msg2["mentions"].([]map[string]interface{})
	if !ok || len(mentions) != 1 {
		t.Errorf("Expected mentions to be replaced with new slice containing quote link")
	}
}

func TestAppendFacetMentionsWithLink(t *testing.T) {
	rawJSON := []byte(`{
		"$type": "app.bsky.feed.post",
		"text": "Check out this link",
		"createdAt": "2023-01-01T00:00:00Z",
		"facets": [
			{
				"features": [
					{
						"$type": "app.bsky.richtext.facet#link",
						"uri": "https://example.com"
					}
				],
				"index": {
					"byteStart": 15,
					"byteEnd": 19
				}
			}
		]
	}`)
	res, err := MapRecord(RecordTypePost, "did:plc:alice", rawJSON)
	if err != nil {
		t.Fatalf("MapRecord failed: %v", err)
	}

	// Text should have markdown link injected
	if res["text"] != "Check out this [link](https://example.com)" {
		t.Errorf("Expected markdown link, got %v", res["text"])
	}
}

func TestMapPostReplyWithNilRoot(t *testing.T) {
	// Reply present but root is nil — only parent should appear
	rawJSON := []byte(`{
		"text": "reply",
		"reply": {"parent": {"uri":"at://did:plc:a/app.bsky.feed.post/1","cid":"bafy"}},
		"createdAt": "2023-01-01T00:00:00Z"
	}`)
	res, err := MapRecord(RecordTypePost, "did:plc:alice", rawJSON)
	if err != nil {
		t.Fatalf("MapRecord failed: %v", err)
	}
	if _, ok := res["_atproto_reply_root"]; ok {
		t.Fatal("expected no root when reply.root is nil")
	}
	if res["_atproto_reply_parent"] != "at://did:plc:a/app.bsky.feed.post/1" {
		t.Fatalf("expected parent URI, got %v", res["_atproto_reply_parent"])
	}
}

func TestMapPostReplyWithNilParent(t *testing.T) {
	rawJSON := []byte(`{
		"text": "reply",
		"reply": {"root": {"uri":"at://did:plc:a/app.bsky.feed.post/1","cid":"bafy"}},
		"createdAt": "2023-01-01T00:00:00Z"
	}`)
	res, err := MapRecord(RecordTypePost, "did:plc:alice", rawJSON)
	if err != nil {
		t.Fatalf("MapRecord failed: %v", err)
	}
	if _, ok := res["_atproto_reply_parent"]; ok {
		t.Fatal("expected no parent when reply.parent is nil")
	}
	if res["_atproto_reply_root"] != "at://did:plc:a/app.bsky.feed.post/1" {
		t.Fatalf("expected root URI, got %v", res["_atproto_reply_root"])
	}
}

func TestMapPostInvalidJSON(t *testing.T) {
	_, err := MapRecord(RecordTypePost, "did:plc:alice", []byte(`{invalid`))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestQuoteSubjectURINonRecordEmbed(t *testing.T) {
	// Embed exists but is not a record or recordWithMedia — should return empty
	rawJSON := []byte(`{
		"text": "image post",
		"embed": {"$type": "app.bsky.embed.images", "images": []},
		"createdAt": "2023-01-01T00:00:00Z"
	}`)
	res, err := MapRecord(RecordTypePost, "did:plc:alice", rawJSON)
	if err != nil {
		t.Fatalf("MapRecord failed: %v", err)
	}
	if _, ok := res["_atproto_quote_subject"]; ok {
		t.Fatal("expected no quote subject for image-only embed")
	}
}

func TestAppendFacetMentionsNilFeature(t *testing.T) {
	mentions := appendFacetMentions(nil, make(map[string]struct{}), "text", &appbsky.RichtextFacet{
		Features: []*appbsky.RichtextFacet_Features_Elem{nil},
	})
	if len(mentions) != 0 {
		t.Fatalf("expected 0 mentions for nil feature, got %d", len(mentions))
	}
}

func TestAppendFacetMentionsTagWithWhitespaceSegment(t *testing.T) {
	seen := make(map[string]struct{})
	mentions := appendFacetMentions(nil, seen, "   ", &appbsky.RichtextFacet{
		Features: []*appbsky.RichtextFacet_Features_Elem{
			{RichtextFacet_Tag: &appbsky.RichtextFacet_Tag{Tag: "mytag"}},
		},
	})
	if len(mentions) != 1 {
		t.Fatalf("expected 1 mention, got %d", len(mentions))
	}
	if mentions[0]["name"] != "#mytag" {
		t.Fatalf("expected fallback name #mytag, got %v", mentions[0]["name"])
	}
}

func TestAppendFacetMentionsTagEmpty(t *testing.T) {
	seen := make(map[string]struct{})
	mentions := appendFacetMentions(nil, seen, "text", &appbsky.RichtextFacet{
		Features: []*appbsky.RichtextFacet_Features_Elem{
			{RichtextFacet_Tag: &appbsky.RichtextFacet_Tag{Tag: "  "}},
		},
	})
	if len(mentions) != 0 {
		t.Fatalf("expected 0 mentions for empty tag, got %d", len(mentions))
	}
}

func TestRewriteFacetSegmentMatchingLinkText(t *testing.T) {
	// When segment text matches the URI, no markdown rewrite should occur
	segment := "https://example.com"
	facet := &appbsky.RichtextFacet{
		Features: []*appbsky.RichtextFacet_Features_Elem{
			{RichtextFacet_Link: &appbsky.RichtextFacet_Link{Uri: "https://example.com"}},
		},
	}
	result := rewriteFacetSegment(segment, facet)
	if result != segment {
		t.Fatalf("expected unchanged segment when text matches URI, got %s", result)
	}
}

func TestRewriteFacetSegmentNilFeatures(t *testing.T) {
	facet := &appbsky.RichtextFacet{
		Features: []*appbsky.RichtextFacet_Features_Elem{
			{RichtextFacet_Link: nil}, // feature exists but link is nil
		},
	}
	result := rewriteFacetSegment("text", facet)
	if result != "text" {
		t.Fatalf("expected unchanged text for nil link feature, got %s", result)
	}
}

func TestRewriteRichTextSortTiebreaker(t *testing.T) {
	// Two facets starting at the same byte offset — shorter one first
	text := "Hello world"
	facets := []*appbsky.RichtextFacet{
		{
			Index:    &appbsky.RichtextFacet_ByteSlice{ByteStart: 0, ByteEnd: 5},
			Features: []*appbsky.RichtextFacet_Features_Elem{{RichtextFacet_Tag: &appbsky.RichtextFacet_Tag{Tag: "a"}}},
		},
		{
			Index:    &appbsky.RichtextFacet_ByteSlice{ByteStart: 0, ByteEnd: 3},
			Features: []*appbsky.RichtextFacet_Features_Elem{{RichtextFacet_Tag: &appbsky.RichtextFacet_Tag{Tag: "b"}}},
		},
	}
	_, mentions := rewriteRichText(text, facets, nil)
	// The shorter facet (0-3) should sort first, the longer (0-5) is skipped since it overlaps
	if len(mentions) != 1 {
		t.Fatalf("expected 1 mention (shorter wins), got %d: %v", len(mentions), mentions)
	}
}

func TestAppendFacetMentionsNilFacet(t *testing.T) {
	mentions := appendFacetMentions(nil, make(map[string]struct{}), "text", nil)
	if len(mentions) != 0 {
		t.Fatalf("expected 0 mentions for nil facet, got %d", len(mentions))
	}
}

func TestReplaceATProtoRefsResolvesRootAndParent(t *testing.T) {
	msg := map[string]interface{}{
		"type":                  "post",
		"_atproto_reply_root":   "at://did:plc:a/app.bsky.feed.post/root",
		"_atproto_reply_parent": "at://did:plc:a/app.bsky.feed.post/parent",
	}
	ReplaceATProtoRefs(msg,
		func(uri string) string { return "%resolved.sha256" },
		func(did string) string { return "" },
	)
	if msg["root"] != "%resolved.sha256" {
		t.Fatalf("expected root resolved, got %v", msg["root"])
	}
	if msg["branch"] != "%resolved.sha256" {
		t.Fatalf("expected branch resolved, got %v", msg["branch"])
	}
	if _, ok := msg["_atproto_reply_root"]; ok {
		t.Fatal("expected _atproto_reply_root removed")
	}
	if _, ok := msg["_atproto_reply_parent"]; ok {
		t.Fatal("expected _atproto_reply_parent removed")
	}
}

func TestReplaceATProtoRefsVoteWithMap(t *testing.T) {
	msg := map[string]interface{}{
		"type":             "vote",
		"_atproto_subject": "at://did:plc:bob/app.bsky.feed.post/1",
		"vote":             map[string]interface{}{"value": 1, "expression": "Like"},
	}
	ReplaceATProtoRefs(msg,
		func(uri string) string { return "%msg.sha256" },
		func(did string) string { return "" },
	)
	vote := msg["vote"].(map[string]interface{})
	if vote["link"] != "%msg.sha256" {
		t.Fatalf("expected vote.link to be resolved, got %v", vote["link"])
	}
}

func TestReplaceATProtoRefsContactResolution(t *testing.T) {
	msg := map[string]interface{}{
		"type":             "contact",
		"_atproto_contact": "did:plc:bob",
	}
	ReplaceATProtoRefs(msg,
		func(uri string) string { return "" },
		func(did string) string { return "@bob.ed25519" },
	)
	if msg["contact"] != "@bob.ed25519" {
		t.Fatalf("expected contact resolved, got %v", msg["contact"])
	}
	if _, ok := msg["_atproto_contact"]; ok {
		t.Fatal("expected _atproto_contact removed")
	}
}

func TestReplaceATProtoRefsMentionDIDNonPrefix(t *testing.T) {
	// Mentions with non-DID links should pass through unchanged
	msg := map[string]interface{}{
		"type":     "post",
		"mentions": []map[string]interface{}{{"link": "#tag", "name": "#tag"}},
	}
	ReplaceATProtoRefs(msg,
		func(uri string) string { return "" },
		func(did string) string { return "" },
	)
	mentions := msg["mentions"].([]map[string]interface{})
	if len(mentions) != 1 || mentions[0]["link"] != "#tag" {
		t.Fatalf("expected non-DID mention to pass through, got %v", mentions)
	}
}

func TestReplaceATProtoRefsEmptyMentionsRemoved(t *testing.T) {
	// All DID mentions unresolved → mentions key deleted
	msg := map[string]interface{}{
		"type":     "post",
		"mentions": []map[string]interface{}{{"link": "did:plc:unknown", "name": "@unknown"}},
	}
	ReplaceATProtoRefs(msg,
		func(uri string) string { return "" },
		func(did string) string { return "" },
	)
	if _, ok := msg["mentions"]; ok {
		t.Fatal("expected mentions key deleted when all DID mentions unresolved")
	}
}

func TestAppendFacetMentionsWithTag(t *testing.T) {
	rawJSON := []byte(`{
		"$type": "app.bsky.feed.post",
		"text": "Hello #tag",
		"createdAt": "2023-01-01T00:00:00Z",
		"facets": [
			{
				"features": [
					{
						"$type": "app.bsky.richtext.facet#tag",
						"tag": "tag"
					}
				],
				"index": {
					"byteStart": 6,
					"byteEnd": 10
				}
			}
		]
	}`)
	res, err := MapRecord(RecordTypePost, "did:plc:alice", rawJSON)
	if err != nil {
		t.Fatalf("MapRecord failed: %v", err)
	}
	// Tags don't alter text or create mentions for SSB
	if res["text"] != "Hello #tag" {
		t.Errorf("Expected tags to leave text alone, got %v", res["text"])
	}
}

func TestReplaceATProtoRefsAdditionalBranches(t *testing.T) {
	lookupURI := func(uri string) string { return "" } // return empty to test branch
	lookupDID := func(did string) string { return "" }

	msg := map[string]interface{}{
		"type":                   "post",
		"_atproto_quote_subject": "at://did:plc:alice/app.bsky.feed.post/1",
		"_atproto_reply_root":    "at://did:plc:alice/app.bsky.feed.post/1",
		"_atproto_reply_parent":  "at://did:plc:alice/app.bsky.feed.post/1",
		"_atproto_subject":       "at://did:plc:alice/app.bsky.feed.post/1",
	}

	ReplaceATProtoRefs(msg, lookupURI, lookupDID)

	// Should remain when resolution fails
	if _, ok := msg["_atproto_reply_root"]; !ok {
		t.Errorf("Expected unresolved root to remain")
	}
	if _, ok := msg["_atproto_reply_parent"]; !ok {
		t.Errorf("Expected unresolved parent to remain")
	}
}
