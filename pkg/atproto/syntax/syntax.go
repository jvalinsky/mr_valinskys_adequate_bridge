package syntax

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// Spec-compliant regex patterns per atproto.com/specs
var (
	// NSID: https://atproto.com/specs/nsid
	// Max 317 chars, segments max 63 chars, at least 2 segments
	nsidRegex = regexp.MustCompile(`^[a-zA-Z]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)+(\.[a-zA-Z]([a-zA-Z0-9]{0,62})?)$`)

	// RecordKey: https://atproto.com/specs/record-key
	// Max 512 chars, allowed chars: a-zA-Z0-9_~.:-, forbidden: "." and ".."
	recordKeyRegex = regexp.MustCompile(`^[a-zA-Z0-9_~.:-]{1,512}$`)

	// AT-URI: https://atproto.com/specs/at-uri-scheme
	// Max 8192 chars, structure: at://authority[/collection[/rkey]]
	aturiRegex = regexp.MustCompile(`^at://([a-zA-Z0-9._:%-]+)(/[a-zA-Z0-9-.]+(/[a-zA-Z0-9_~.:-]+)?)?$`)
)

type DID string
type Handle string
type NSID string
type RecordKey string
type Identifier string

type ATURI struct {
	authority  Identifier
	collection NSID
	recordKey  RecordKey
}

func ParseDID(raw string) (DID, error) {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(raw, "did:") {
		return "", fmt.Errorf("invalid did %q", raw)
	}
	parts := strings.Split(raw, ":")
	if len(parts) < 3 || strings.TrimSpace(parts[1]) == "" || strings.TrimSpace(parts[2]) == "" {
		return "", fmt.Errorf("invalid did %q", raw)
	}
	return DID(raw), nil
}

func (d DID) String() string {
	return string(d)
}

func (d DID) Method() string {
	parts := strings.SplitN(string(d), ":", 3)
	if len(parts) < 3 {
		return ""
	}
	return parts[1]
}

func ParseHandle(raw string) (Handle, error) {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" || !strings.Contains(raw, ".") {
		return "", fmt.Errorf("invalid handle %q", raw)
	}
	for _, r := range raw {
		switch {
		case unicode.IsLower(r), unicode.IsDigit(r), r == '-', r == '.':
		default:
			return "", fmt.Errorf("invalid handle %q", raw)
		}
	}
	return Handle(raw), nil
}

func (h Handle) String() string {
	return string(h)
}

func ParseNSID(raw string) (NSID, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("invalid nsid: empty string")
	}
	if len(raw) > 317 {
		return "", fmt.Errorf("invalid nsid: too long (max 317 chars)")
	}
	if !nsidRegex.MatchString(raw) {
		return "", fmt.Errorf("invalid nsid %q: doesn't match required format", raw)
	}
	return NSID(raw), nil
}

func (n NSID) String() string {
	return string(n)
}

func ParseRecordKey(raw string) (RecordKey, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("invalid record key: empty string")
	}
	if raw == "." || raw == ".." {
		return "", fmt.Errorf("invalid record key: %q is reserved", raw)
	}
	if len(raw) > 512 {
		return "", fmt.Errorf("invalid record key: too long (max 512 chars)")
	}
	if !recordKeyRegex.MatchString(raw) {
		return "", fmt.Errorf("invalid record key %q: doesn't match required format", raw)
	}
	return RecordKey(raw), nil
}

func (rk RecordKey) String() string {
	return string(rk)
}

func (id Identifier) String() string {
	return string(id)
}

func ParseATURI(raw string) (ATURI, error) {
	raw = strings.TrimSpace(raw)
	
	// Length limit per spec
	if len(raw) > 8192 {
		return ATURI{}, fmt.Errorf("invalid at-uri: too long (max 8192 chars)")
	}
	
	if !strings.HasPrefix(raw, "at://") {
		return ATURI{}, fmt.Errorf("invalid at-uri %q: missing 'at://' prefix", raw)
	}

	rest := strings.TrimPrefix(raw, "at://")
	// Strip query/fragment for parsing
	if idx := strings.IndexAny(rest, "?#"); idx >= 0 {
		rest = rest[:idx]
	}
	if rest == "" {
		return ATURI{}, fmt.Errorf("invalid at-uri %q: missing authority", raw)
	}

	// Split path - check for double-slash (empty segments)
	parts := strings.Split(rest, "/")
	
	// Validate authority
	authorityRaw := parts[0]
	if authorityRaw == "" {
		return ATURI{}, fmt.Errorf("invalid at-uri %q: missing authority", raw)
	}
	switch {
	case strings.HasPrefix(authorityRaw, "did:"):
		if _, err := ParseDID(authorityRaw); err != nil {
			return ATURI{}, err
		}
	default:
		if _, err := ParseHandle(authorityRaw); err != nil {
			return ATURI{}, err
		}
	}

	// Process path segments - detect empty segments (double-slash)
	var collection NSID
	var rkey RecordKey
	
	if len(parts) > 1 {
		// Check for empty segments indicating double-slash
		for i, part := range parts[1:] {
			if part == "" {
				// Empty segment means double-slash - invalid
				return ATURI{}, fmt.Errorf("invalid at-uri %q: empty path segment", raw)
			}
			if i == 0 {
				// First path segment is collection (NSID)
				var err error
				collection, err = ParseNSID(part)
				if err != nil {
					return ATURI{}, fmt.Errorf("invalid at-uri %q: invalid collection: %w", raw, err)
				}
			} else if i == 1 {
				// Second path segment is record key
				var err error
				rkey, err = ParseRecordKey(part)
				if err != nil {
					return ATURI{}, fmt.Errorf("invalid at-uri %q: invalid record key: %w", raw, err)
				}
			} else {
				// More than 2 path segments - invalid
				return ATURI{}, fmt.Errorf("invalid at-uri %q: too many path segments", raw)
			}
		}
	}

	return ATURI{
		authority:  Identifier(authorityRaw),
		collection: collection,
		recordKey:  rkey,
	}, nil
}

func (u ATURI) String() string {
	base := "at://" + u.authority.String()
	if u.collection != "" {
		base += "/" + u.collection.String()
	}
	if u.recordKey != "" {
		base += "/" + u.recordKey.String()
	}
	return base
}

func (u ATURI) Normalize() ATURI {
	return u
}

func (u ATURI) Authority() Identifier {
	return u.authority
}

func (u ATURI) Collection() NSID {
	return u.collection
}

func (u ATURI) RecordKey() RecordKey {
	return u.recordKey
}
