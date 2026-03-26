package mapper

import (
	"testing"
)

func TestMapPost(t *testing.T) {
	rawJSON := []byte(`{
		"text": "Hello ATProto",
		"createdAt": "2023-01-01T00:00:00Z"
	}`)

	res, err := MapRecord(RecordTypePost, rawJSON)
	if err != nil {
		t.Fatalf("MapRecord failed: %v", err)
	}

	if res["type"] != "post" {
		t.Errorf("expected type 'post', got %v", res["type"])
	}
	if res["text"] != "Hello ATProto" {
		t.Errorf("expected text 'Hello ATProto', got %v", res["text"])
	}
}

func TestMapLike(t *testing.T) {
	rawJSON := []byte(`{
		"subject": {
			"uri": "at://did:plc:123/app.bsky.feed.post/456",
			"cid": "bafy123"
		},
		"createdAt": "2023-01-01T00:00:00Z"
	}`)

	res, err := MapRecord(RecordTypeLike, rawJSON)
	if err != nil {
		t.Fatalf("MapRecord failed: %v", err)
	}

	if res["type"] != "vote" {
		t.Errorf("expected type 'vote', got %v", res["type"])
	}

	vote, ok := res["vote"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected vote to be map[string]interface{}, got %T", res["vote"])
	}
	if vote["value"] != 1 {
		t.Errorf("expected vote value 1, got %v", vote["value"])
	}

	if res["_atproto_subject"] != "at://did:plc:123/app.bsky.feed.post/456" {
		t.Errorf("expected _atproto_subject to be set, got %v", res["_atproto_subject"])
	}
}

func TestReplaceATProtoRefs(t *testing.T) {
	msg := map[string]interface{}{
		"type":             "vote",
		"vote":             map[string]interface{}{"value": 1, "expression": "Like"},
		"_atproto_subject": "at://did:plc:123/app.bsky.feed.post/456",
	}

	lookupURI := func(uri string) string {
		if uri == "at://did:plc:123/app.bsky.feed.post/456" {
			return "%msg123.sha256"
		}
		return ""
	}

	lookupDID := func(did string) string {
		return ""
	}

	ReplaceATProtoRefs(msg, lookupURI, lookupDID)

	if _, ok := msg["_atproto_subject"]; ok {
		t.Errorf("_atproto_subject should be deleted")
	}

	vote, ok := msg["vote"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected vote to be map")
	}
	if vote["link"] != "%msg123.sha256" {
		t.Errorf("expected vote link to be %%msg123.sha256, got %v", vote["link"])
	}
}
