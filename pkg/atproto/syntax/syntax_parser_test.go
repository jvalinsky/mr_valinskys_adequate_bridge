package syntax

import (
	"strings"
	"testing"
)

// TestParseNSID_Invalid tests NSID parser spec compliance
func TestParseNSID_Invalid(t *testing.T) {
	tests := []struct {
		name  string
		input string
		valid bool
	}{
		// Invalid cases
		{"empty", "", false},
		{"no_period", "abc", false},
		{"only_two_segments", "a.b", false}, // NSID requires MINIMUM 3 segments
		{"only_two_segments_domain", "com.example", false},
		{"double_dot", "com..example", false},
		{"leading_dot", ".com.example", false},
		{"trailing_dot", "com.example.", false},
		{"segment_too_long", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.com.example", false}, // 64 chars

		// Valid cases
		{"valid_simple", "app.bsky.feed.post", true},
		{"valid_short_three", "a.b.c", true}, // MINIMUM is 3 segments
		{"valid_long_segment", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.b.c", true}, // 63 chars first seg
		{"valid_numbers", "com.example123.abc", true},
		{"valid_hyphen", "com.my-example.app", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseNSID(tt.input)
			if tt.valid && err != nil {
				t.Errorf("ParseNSID(%q) unexpected error: %v", tt.input, err)
			}
			if !tt.valid && err == nil {
				t.Errorf("ParseNSID(%q) expected error, got nil", tt.input)
			}
		})
	}
}

// TestParseRecordKey_Invalid tests RecordKey parser spec compliance
func TestParseRecordKey_Invalid(t *testing.T) {
	// Generate 512 'a' chars for max length test
	maxLen := make([]byte, 512)
	for i := range maxLen {
		maxLen[i] = 'a'
	}
	// Generate 513 'a' chars for too-long test
	tooLong := make([]byte, 513)
	for i := range tooLong {
		tooLong[i] = 'a'
	}

	tests := []struct {
		name  string
		input string
		valid bool
	}{
		// Invalid cases - security critical
		{"dot", ".", false},
		{"dotdot", "..", false},

		// Invalid cases - general
		{"empty", "", false},
		{"slash", "abc/def", false},
		{"space", "abc def", false},
		{"paren", "abc(def)", false},
		{"bang", "abc!def", false}, // ! is NOT allowed per spec
		{"too_long_513", string(tooLong), false},

		// Valid cases
		{"valid_simple", "abc123", true},
		{"valid_underscore", "abc_def", true},
		{"valid_tilde", "abc~def", true},
		{"valid_colon", "abc:def", true},
		{"valid_dash", "abc-def", true},
		{"valid_dot_in_middle", "abc.def", true}, // . is allowed inside, just not as entire value
		{"max_length_512", string(maxLen), true},
	}

		for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseRecordKey(tt.input)
			if tt.valid && err != nil {
				t.Errorf("ParseRecordKey(%q) unexpected error: %v", tt.input, err)
			}
			if !tt.valid && err == nil {
				t.Errorf("ParseRecordKey(%q) expected error, got nil", tt.input)
			}
		})
	}
}

// TestParseDID_SpecCompliance tests DID parser spec compliance
func TestParseDID_SpecCompliance(t *testing.T) {
	tests := []struct {
		name  string
		input string
		valid bool
	}{
		// Valid cases
		{"valid_plc", "did:plc:z72i7hdynmk6r22w27h6cj7f", true},
		{"valid_web", "did:web:example.com", true},
		{"valid_web_subdomain", "did:web:sub.example.com", true},
		{"valid_web_with_port", "did:web:localhost%3A3000", true},

		// Invalid cases
		{"empty", "", false},
		{"no_prefix", "plc:z72i7hdynmk6r22w27h6cj7f", false},
		{"uppercase_method", "did:PLC:z72i7hdynmk6r22w27h6cj7f", false},
		{"missing_method", "did::z72i7hdynmk6r22w27h6cj7f", false},
		{"missing_id", "did:plc:", false},
		{"too_long", "did:plc:" + strings.Repeat("a", 2048), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseDID(tt.input)
			if tt.valid && err != nil {
				t.Errorf("ParseDID(%q) unexpected error: %v", tt.input, err)
			}
			if !tt.valid && err == nil {
				t.Errorf("ParseDID(%q) expected error, got nil", tt.input)
			}
		})
	}
}

// TestParseHandle_SpecCompliance tests Handle parser spec compliance
func TestParseHandle_SpecCompliance(t *testing.T) {
	tests := []struct {
		name  string
		input string
		valid bool
	}{
		// Valid cases
		{"valid_simple", "alice.test", true},
		{"valid_subdomain", "alice.sub.example.com", true},
		{"valid_numbers", "user123.example.com", true},
		{"valid_hyphen", "user-name.example.com", true},
		{"uppercase_normalized", "ALICE.TEST", true}, // Should normalize to lowercase

		// Invalid cases
		{"empty", "", false},
		{"no_dot", "alice", false},
		{"too_long", "a." + strings.Repeat("b", 253), false},
		{"segment_too_long", strings.Repeat("a", 64) + ".test", false},
		{"invalid_underscore", "user_name.test", false},
		{"invalid_space", "user name.test", false},
		{"leading_hyphen", "-alice.test", false},
		{"trailing_hyphen", "alice-.test", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseHandle(tt.input)
			if tt.valid && err != nil {
				t.Errorf("ParseHandle(%q) unexpected error: %v", tt.input, err)
			}
			if !tt.valid && err == nil {
				t.Errorf("ParseHandle(%q) expected error, got nil", tt.input)
			}
		})
	}
}

// TestHandle_TLD tests TLD validation
func TestHandle_TLD(t *testing.T) {
	tests := []struct {
		handle   string
		tld      string
		allowed  bool
	}{
		{"alice.test", "test", true},
		{"alice.local", "local", false},
		{"alice.localhost", "localhost", false},
		{"alice.invalid", "invalid", false},
		{"alice.arpa", "arpa", false},
		{"alice.internal", "internal", false},
		{"alice.example", "example", false},
		{"alice.onion", "onion", false},
		{"alice.alt", "alt", false},
	}

	for _, tt := range tests {
		t.Run(tt.handle, func(t *testing.T) {
			h, err := ParseHandle(tt.handle)
			if tt.allowed {
				if err != nil {
					t.Fatalf("ParseHandle(%q) error: %v", tt.handle, err)
				}
				if h.TLD() != tt.tld {
					t.Errorf("TLD() = %q, want %q", h.TLD(), tt.tld)
				}
				if !h.AllowedTLD() {
					t.Errorf("AllowedTLD() = false for allowed TLD %q", tt.tld)
				}
			} else {
				// Disallowed TLDs still parse successfully,
				// but AllowedTLD() returns false
				if err != nil {
					t.Fatalf("ParseHandle(%q) error: %v", tt.handle, err)
				}
				if h.AllowedTLD() {
					t.Errorf("AllowedTLD() = true for disallowed TLD %q", tt.tld)
				}
			}
		})
	}
}
