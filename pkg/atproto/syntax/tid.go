package syntax

import (
	"encoding/base32"
	"errors"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Base32SortAlphabet is the base32 alphabet used for TIDs.
// Excludes 0, 1, 8, 9 to avoid confusion with letters.
const Base32SortAlphabet = "234567abcdefghijklmnopqrstuvwxyz"

// base32SortEncoding is the base32 encoding using the sort alphabet.
var base32SortEncoding = base32.NewEncoding(Base32SortAlphabet).WithPadding(base32.NoPadding)

// TID represents a Timestamp ID (13-character base32sort identifier).
// Used for record keys and commit ordering in ATProto repos.
//
// Spec: https://atproto.com/specs/record-key#tid-format
type TID string

// tidRegex validates TID format:
// - First char: must be in first 1/3 of alphabet (2-9, a-j) to constrain timestamp
// - Remaining 12 chars: any base32sort character
var tidRegex = regexp.MustCompile(`^[234567abcdefghij][234567abcdefghijklmnopqrstuvwxyz]{12}$`)

// ParseTID validates and parses a TID string.
func ParseTID(raw string) (TID, error) {
	if raw == "" {
		return "", errors.New("expected TID, got empty string")
	}
	if len(raw) != 13 {
		return "", errors.New("TID is wrong length (expected 13 chars)")
	}
	if !tidRegex.MatchString(raw) {
		return "", errors.New("TID syntax didn't validate via regex")
	}
	return TID(raw), nil
}

// String returns the string representation of the TID.
func (t TID) String() string {
	return string(t)
}

// NewTIDNow generates a TID from the current time.
// Note: For monotonic generation, use TIDClock instead.
func NewTIDNow(clockID uint) TID {
	return NewTID(time.Now().UTC().UnixMicro(), clockID)
}

// NewTID generates a TID from a Unix microsecond timestamp and clock ID.
// The timestamp provides 53 bits, the clock ID provides 10 bits.
func NewTID(unixMicros int64, clockID uint) TID {
	// Combine timestamp (upper 53 bits) and clock ID (lower 10 bits)
	v := (uint64(unixMicros&0x1F_FFFF_FFFF_FFFF) << 10) | uint64(clockID&0x3FF)
	return NewTIDFromInteger(v)
}

// NewTIDFromTime generates a TID from a time.Time and clock ID.
func NewTIDFromTime(ts time.Time, clockID uint) TID {
	return NewTID(ts.UTC().UnixMicro(), clockID)
}

// NewTIDFromInteger creates a TID from its 64-bit integer representation.
func NewTIDFromInteger(v uint64) TID {
	// Mask to 63 bits (valid TID range)
	v = (0x7FFF_FFFF_FFFF_FFFF & v)
	var buf [13]byte
	// Convert to base32sort, most significant bits first
	for i := 12; i >= 0; i-- {
		buf[i] = Base32SortAlphabet[v&0x1F]
		v = v >> 5
	}
	return TID(buf[:])
}

// Integer returns the full 64-bit integer representation of the TID.
func (t TID) Integer() uint64 {
	s := t.String()
	if len(s) != 13 {
		return 0
	}
	var v uint64
	for i := 0; i < 13; i++ {
		c := strings.IndexByte(Base32SortAlphabet, s[i])
		if c < 0 {
			return 0
		}
		v = (v << 5) | uint64(c&0x1F)
	}
	return v
}

// Time returns the timestamp portion of the TID as a time.Time.
func (t TID) Time() time.Time {
	i := t.Integer()
	// Extract upper 53 bits (timestamp)
	i = (i >> 10) & 0x1FFF_FFFF_FFFF_FFFF
	return time.UnixMicro(int64(i)).UTC()
}

// ClockID returns the clock ID portion of the TID (lower 10 bits).
func (t TID) ClockID() uint {
	i := t.Integer()
	return uint(i & 0x3FF)
}

// TIDClock provides monotonic TID generation to ensure TIDs are always increasing.
type TIDClock struct {
	ClockID       uint
	mtx           sync.Mutex
	lastUnixMicro int64
}

// NewTIDClock creates a new TIDClock with the given clock ID.
func NewTIDClock(clockID uint) *TIDClock {
	return &TIDClock{
		ClockID: clockID,
	}
}

// ClockFromTID creates a TIDClock initialized from an existing TID.
// This ensures the next generated TID will be greater than the given TID.
func ClockFromTID(t TID) *TIDClock {
	um := t.Integer()
	um = (um >> 10) & 0x1FFF_FFFF_FFFF_FFFF
	return &TIDClock{
		ClockID:       t.ClockID(),
		lastUnixMicro: int64(um),
	}
}

// Next generates the next TID, ensuring monotonicity.
// If the current time is before the last TID's timestamp, uses last + 1 microsecond.
func (c *TIDClock) Next() TID {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	now := time.Now().UTC().UnixMicro()
	if now <= c.lastUnixMicro {
		now = c.lastUnixMicro + 1
	}
	c.lastUnixMicro = now

	return NewTID(now, c.ClockID)
}

// Last returns the timestamp of the last generated TID.
func (c *TIDClock) Last() time.Time {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	return time.UnixMicro(c.lastUnixMicro).UTC()
}
