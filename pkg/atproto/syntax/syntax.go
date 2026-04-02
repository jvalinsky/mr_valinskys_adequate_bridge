package syntax

import (
	"fmt"
	"strings"
	"unicode"
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
	if raw == "" || !strings.Contains(raw, ".") {
		return "", fmt.Errorf("invalid nsid %q", raw)
	}
	return NSID(raw), nil
}

func (n NSID) String() string {
	return string(n)
}

func ParseRecordKey(raw string) (RecordKey, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.Contains(raw, "/") {
		return "", fmt.Errorf("invalid record key %q", raw)
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
	if !strings.HasPrefix(raw, "at://") {
		return ATURI{}, fmt.Errorf("invalid at-uri %q", raw)
	}

	rest := strings.TrimPrefix(raw, "at://")
	if idx := strings.IndexAny(rest, "?#"); idx >= 0 {
		rest = rest[:idx]
	}
	if strings.TrimSpace(rest) == "" {
		return ATURI{}, fmt.Errorf("invalid at-uri %q: missing authority", raw)
	}

	parts := strings.Split(rest, "/")
	authorityRaw := strings.TrimSpace(parts[0])
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

	pathParts := make([]string, 0, 2)
	for _, part := range parts[1:] {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		pathParts = append(pathParts, part)
	}
	if len(pathParts) > 2 {
		return ATURI{}, fmt.Errorf("invalid at-uri %q: too many path segments", raw)
	}

	var collection NSID
	var rkey RecordKey
	if len(pathParts) > 0 {
		var err error
		collection, err = ParseNSID(pathParts[0])
		if err != nil {
			return ATURI{}, err
		}
	}
	if len(pathParts) > 1 {
		var err error
		rkey, err = ParseRecordKey(pathParts[1])
		if err != nil {
			return ATURI{}, err
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
