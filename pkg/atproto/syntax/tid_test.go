package syntax

import (
	"testing"
	"time"
)

func TestParseTID(t *testing.T) {
	tests := []struct {
		name  string
		input string
		valid bool
	}{
		// Valid cases
		{"valid_13_chars", "3kxb3qd4jf26p", true},
		{"valid_all_chars", "3zzzzzzzzzzzz", true},
		{"valid_min_timestamp", "2222222222222", true},

		// Invalid cases
		{"empty", "", false},
		{"too_short_12", "3kxb3qd4jf26", false},
		{"too_long_14", "3kxb3qd4jf26pp", false},
		{"invalid_char_0", "3kxb3qd4jf060", false}, // 0 not in alphabet
		{"invalid_char_1", "3kxb3qd4jf161", false}, // 1 not in alphabet
		{"invalid_char_8", "3kxb3qd4jf868", false}, // 8 not in alphabet
		{"invalid_char_9", "3kxb3qd4jf969", false}, // 9 not in alphabet
		{"invalid_first_char_k", "kkxb3qd4jf26p", false}, // k not allowed as first char
		{"invalid_first_char_z", "zkxb3qd4jf26p", false}, // z not allowed as first char (too high timestamp)
		{"uppercase", "3KXB3QD4JF26P", false}, // uppercase not valid
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseTID(tt.input)
			if tt.valid && err != nil {
				t.Errorf("ParseTID(%q) unexpected error: %v", tt.input, err)
			}
			if !tt.valid && err == nil {
				t.Errorf("ParseTID(%q) expected error, got nil", tt.input)
			}
		})
	}
}

func TestTID_Integer_RoundTrip(t *testing.T) {
	// Test round-trip: integer -> TID -> integer
	testCases := []uint64{
		0,
		1,
		1000000,
		0x7FFFFFFFFFFFF, // max 63-bit value
	}

	for _, v := range testCases {
		tid := NewTIDFromInteger(v)
		got := tid.Integer()
		if got != v {
			t.Errorf("NewTIDFromInteger(%d).Integer() = %d, want %d", v, got, v)
		}
	}
}

func TestTID_Time_RoundTrip(t *testing.T) {
	// Test round-trip: time -> TID -> time
	now := time.Now().UTC().Truncate(time.Microsecond)
	clockID := uint(0)

	tid := NewTIDFromTime(now, clockID)
	got := tid.Time()

	// Should be close (within 1 microsecond due to truncation)
	diff := got.Sub(now)
	if diff < 0 {
		diff = -diff
	}
	if diff > time.Microsecond {
		t.Errorf("Time round-trip diff = %v, want <= 1µs", diff)
	}
}

func TestTID_ClockID(t *testing.T) {
	tests := []struct {
		clockID uint
	}{
		{0},
		{1},
		{100},
		{0x3FF}, // max 10-bit value
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			tid := NewTID(time.Now().UnixMicro(), tt.clockID)
			got := tid.ClockID()
			if got != tt.clockID {
				t.Errorf("ClockID() = %d, want %d", got, tt.clockID)
			}
		})
	}
}

func TestTIDClock_Monotonicity(t *testing.T) {
	clock := NewTIDClock(0)

	// Generate multiple TIDs and verify monotonicity
	var last TID
	for i := 0; i < 100; i++ {
		next := clock.Next()
		if i > 0 {
			if next.Integer() <= last.Integer() {
				t.Errorf("TID %d (int=%d) <= previous (int=%d), not monotonic", i, next.Integer(), last.Integer())
			}
		}
		last = next
	}
}

func TestTIDClock_FromExistingTID(t *testing.T) {
	// Create a TID
	existing := NewTIDNow(42)

	// Create clock from it
	clock := ClockFromTID(existing)

	// Next should be greater
	next := clock.Next()
	if next.Integer() <= existing.Integer() {
		t.Errorf("ClockFromTID().Next() = %d, want > %d", next.Integer(), existing.Integer())
	}
}

func TestTID_String(t *testing.T) {
	tid := TID("3kxb3qd4jf26p")
	if tid.String() != "3kxb3qd4jf26p" {
		t.Errorf("String() = %q, want %q", tid.String(), "3kxb3qd4jf26p")
	}
}

func TestNewTIDNow(t *testing.T) {
	tid := NewTIDNow(123)
	if len(tid.String()) != 13 {
		t.Errorf("NewTIDNow() length = %d, want 13", len(tid.String()))
	}
	_, err := ParseTID(tid.String())
	if err != nil {
		t.Errorf("NewTIDNow() produced invalid TID: %v", err)
	}
}
