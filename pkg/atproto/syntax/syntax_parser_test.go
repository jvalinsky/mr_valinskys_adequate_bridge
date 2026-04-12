package syntax

import "testing"

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
