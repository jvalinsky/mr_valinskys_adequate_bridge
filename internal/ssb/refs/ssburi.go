package refs

import (
	"encoding/base64"
	"errors"
	"net/url"
	"strings"
)

var (
	ErrInvalidScheme   = errors.New("ssburi: invalid scheme")
	ErrInvalidResource = errors.New("ssburi: invalid resource type")
	ErrInvalidEncoding = errors.New("ssburi: invalid encoding")
)

type SSBURIType int

const (
	URITypeFeed SSBURIType = iota
	URITypeMessage
	URITypeBlob
	URITypeAddress
	URITypeUnknown
)

type FeedURI struct {
	Ref *FeedRef
}

type MessageURI struct {
	Ref *MessageRef
}

type BlobURI struct {
	Ref *BlobRef
}

type SSBURI interface {
	Type() SSBURIType
	String() string
}

func (f *FeedURI) Type() SSBURIType    { return URITypeFeed }
func (m *MessageURI) Type() SSBURIType { return URITypeMessage }
func (b *BlobURI) Type() SSBURIType    { return URITypeBlob }

func (f *FeedURI) String() string {
	if f.Ref == nil {
		return ""
	}
	return "ssb:feed/classic/" + base64.URLEncoding.EncodeToString([]byte(f.Ref.String()[1:]))
}

func (m *MessageURI) String() string {
	if m.Ref == nil {
		return ""
	}
	return "ssb:message/classic/" + base64.URLEncoding.EncodeToString([]byte(m.Ref.String()[1:]))
}

func (b *BlobURI) String() string {
	if b.Ref == nil {
		return ""
	}
	return "ssb:blob/classic/" + base64.URLEncoding.EncodeToString([]byte(b.Ref.String()[1:]))
}

func ParseSSBURI(uri string) (SSBURI, error) {
	uri = strings.TrimSpace(uri)

	u, err := url.Parse(uri)
	if err != nil {
		return nil, ErrInvalidSSBURI
	}

	if u.Scheme != "ssb" {
		return nil, ErrInvalidScheme
	}

	parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	if len(parts) < 2 {
		return nil, ErrInvalidResource
	}

	switch parts[0] {
	case "feed":
		return parseFeedURI(parts)
	case "message":
		return parseMessageURI(parts)
	case "blob":
		return parseBlobURI(parts)
	default:
		return nil, ErrInvalidResource
	}
}

func parseFeedURI(parts []string) (*FeedURI, error) {
	if len(parts) < 2 {
		return nil, ErrInvalidResource
	}

	var format string
	var dataStr string

	switch parts[1] {
	case "classic":
		format = "ed25519"
		if len(parts) >= 3 {
			dataStr = parts[2]
		}
	case "bendybutt-v1":
		format = "bendybutt-v1"
		if len(parts) >= 3 {
			dataStr = parts[2]
		}
	case "gabbygrove-v1":
		format = "gabbygrove-v1"
		if len(parts) >= 3 {
			dataStr = parts[2]
		}
	default:
		return nil, ErrInvalidResource
	}

	if dataStr == "" {
		return nil, ErrInvalidEncoding
	}

	data, err := base64.URLEncoding.DecodeString(dataStr)
	if err != nil {
		return nil, ErrInvalidEncoding
	}

	sigil := string(rune(SigilFeed)) + string(data)
	ref, err := ParseFeedRef(sigil)
	if err != nil {
		return nil, err
	}
	_ = format

	return &FeedURI{Ref: ref}, nil
}

func parseMessageURI(parts []string) (*MessageURI, error) {
	if len(parts) < 2 {
		return nil, ErrInvalidResource
	}

	var format string
	var dataStr string

	switch parts[1] {
	case "classic":
		format = "sha256"
		if len(parts) >= 3 {
			dataStr = parts[2]
		}
	case "bendybutt-v1":
		format = "bendybutt-v1"
		if len(parts) >= 3 {
			dataStr = parts[2]
		}
	default:
		return nil, ErrInvalidResource
	}

	if dataStr == "" {
		return nil, ErrInvalidEncoding
	}

	data, err := base64.URLEncoding.DecodeString(dataStr)
	if err != nil {
		return nil, ErrInvalidEncoding
	}

	sigil := string(rune(SigilMessage)) + string(data)
	ref, err := ParseMessageRef(sigil)
	if err != nil {
		return nil, err
	}
	_ = format

	return &MessageURI{Ref: ref}, nil
}

func parseBlobURI(parts []string) (*BlobURI, error) {
	if len(parts) < 2 {
		return nil, ErrInvalidResource
	}

	if parts[1] != "classic" {
		return nil, ErrInvalidResource
	}

	if len(parts) < 3 {
		return nil, ErrInvalidEncoding
	}

	dataStr := parts[2]
	data, err := base64.URLEncoding.DecodeString(dataStr)
	if err != nil {
		return nil, ErrInvalidEncoding
	}

	sigil := string(rune(SigilBlob)) + string(data)
	ref, err := ParseBlobRef(sigil)
	if err != nil {
		return nil, err
	}

	return &BlobURI{Ref: ref}, nil
}
