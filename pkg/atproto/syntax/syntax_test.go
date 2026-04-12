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

func TestParseATURI_EdgeCases(t *testing.T) {
	tests := []struct {
		name  string
		input string
		valid bool
	}{
		{"empty_string", "", false},
		{"missing_at_prefix", "did:plc:test/app.bsky.feed.post/abc", false},
		{"invalid_prefix", "http://did:plc:test/app.bsky.feed.post/abc", false},
		{"just_at_prefix", "at://", false},
		{"single_segment", "at://did:plc:test", true}, // Valid: authority only, no collection/rkey
		{"empty_collection", "at://did:plc:test/", false},                                  // INVALID: trailing slash without collection
		{"empty_recordkey", "at://did:plc:test/app.bsky.feed.post/", false},                // INVALID: trailing slash without rkey
		{"special_chars_in_rkey", "at://did:plc:test/app.bsky.feed.post/abc-def_123", true}, // Valid special chars
		{"underscore_in_rkey", "at://did:plc:test/app.bsky.feed.post/abc_def", true},
		{"numbers_in_rkey", "at://did:plc:test/app.bsky.feed.post/12345", true},
		{"double_slash_in_authority", "at://did:plc:test//app.bsky.feed.post/abc", false}, // INVALID: empty path segment
		{"four_segments", "at://did:plc:test/a/b/c", false},
		{"trailing_slash_rkey", "at://did:plc:test/app.bsky.feed.post/abc/", false}, // INVALID: trailing slash creates empty segment
		{"valid_full", "at://did:plc:test/app.bsky.feed.post/abc", true},
		{"valid_no_rkey", "at://did:plc:test/app.bsky.feed.post", true},
		{"handle_authority", "at://alice.test/app.bsky.feed.post/abc", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseATURI(tt.input)
			if tt.valid && err != nil {
				t.Errorf("ParseATURI(%q) unexpected error: %v", tt.input, err)
			}
			if !tt.valid && err == nil {
				t.Errorf("ParseATURI(%q) expected error, got nil", tt.input)
			}
		})
	}
}

func FuzzParseATURI(f *testing.F) {
	f.Add("at://did:plc:test/app.bsky.feed.post/abc")
	f.Add("at://alice.test/app.bsky.graph.follow/xyz")
	f.Add("at://did:plc:z72i7hdynmk6r22w27h6cj7f/app.bsky.actor.profile/self")
	f.Add("")
	f.Add("not-a-aturi")
	f.Add("at://")
	f.Add("at://did:plc:test/")
	f.Add("at://did:plc:test/app.bsky.feed.post")

	f.Fuzz(func(t *testing.T, s string) {
		_, err := ParseATURI(s)
		if err != nil {
			return
		}
		if s != "" && !hasATURIPrefix(s) {
			t.Errorf("ParseATURI(%q) should have failed for non-AT-URI input", s)
		}
	})
}

func hasATURIPrefix(s string) bool {
	return len(s) >= 3 && s[0] == 'a' && s[1] == 't' && s[2] == ':'
}
