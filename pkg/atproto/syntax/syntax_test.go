package syntax

import "testing"

func TestParseATURIDIDAuthority(t *testing.T) {
	parsed, err := ParseATURI("at://did:plc:test123/app.bsky.feed.post/abc")
	if err != nil {
		t.Fatalf("ParseATURI returned error: %v", err)
	}
	if got := parsed.Authority().String(); got != "did:plc:test123" {
		t.Fatalf("authority=%q", got)
	}
	if got := parsed.Collection().String(); got != "app.bsky.feed.post" {
		t.Fatalf("collection=%q", got)
	}
	if got := parsed.RecordKey().String(); got != "abc" {
		t.Fatalf("recordKey=%q", got)
	}
}

func TestParseATURIHandleAuthority(t *testing.T) {
	parsed, err := ParseATURI("at://alice.test/app.bsky.feed.post/abc")
	if err != nil {
		t.Fatalf("ParseATURI returned error: %v", err)
	}
	if got := parsed.Authority().String(); got != "alice.test" {
		t.Fatalf("authority=%q", got)
	}
}

func TestParseATURIRejectsExtraSegments(t *testing.T) {
	if _, err := ParseATURI("at://did:plc:test123/app.bsky.feed.post/abc/extra"); err == nil {
		t.Fatal("expected error for extra path segments")
	}
}
