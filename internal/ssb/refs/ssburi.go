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
	format, ok := feedURIFormat(f.Ref.Algo())
	if !ok {
		return ""
	}
	return "ssb:feed/" + format + "/" + base64.URLEncoding.EncodeToString(f.Ref.PubKey())
}

func (m *MessageURI) String() string {
	if m.Ref == nil {
		return ""
	}
	format, ok := messageURIFormat(m.Ref.Algo())
	if !ok {
		return ""
	}
	return "ssb:message/" + format + "/" + base64.URLEncoding.EncodeToString(m.Ref.Hash())
}

func (b *BlobURI) String() string {
	if b.Ref == nil {
		return ""
	}
	return "ssb:blob/classic/" + base64.URLEncoding.EncodeToString(b.Ref.Hash())
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

	path := u.Opaque
	if path == "" {
		path = u.Path
	}
	path = strings.TrimPrefix(path, "/")
	if idx := strings.IndexByte(path, '?'); idx >= 0 {
		path = path[:idx]
	}
	parts := strings.FieldsFunc(path, func(r rune) bool {
		return r == '/' || r == ':'
	})
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
	if len(parts) < 3 {
		return nil, ErrInvalidResource
	}

	var algo RefAlgo
	switch parts[1] {
	case "classic", "ed25519":
		algo = RefAlgoFeedSSB1
	case "bendybutt-v1":
		algo = RefAlgoFeedBendyButt
	case "gabbygrove-v1":
		algo = RefAlgoFeedGabby
	case "bamboo":
		algo = RefAlgoFeedBamboo
	case "indexed-v1":
		algo = RefAlgoFeedIndexed
	default:
		return nil, ErrInvalidResource
	}

	data, err := base64.URLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, ErrInvalidEncoding
	}

	ref, err := NewFeedRef(data, algo)
	if err != nil {
		return nil, err
	}

	return &FeedURI{Ref: ref}, nil
}

func parseMessageURI(parts []string) (*MessageURI, error) {
	if len(parts) < 3 {
		return nil, ErrInvalidResource
	}

	var algo RefAlgoMessage
	switch parts[1] {
	case "classic", "sha256":
		algo = RefAlgoMessageSSB1
	case "bendybutt-v1":
		algo = RefAlgoMessageBendyButt
	case "gabbygrove-v1":
		algo = RefAlgoMessageGabby
	case "bamboo":
		algo = RefAlgoMessageBamboo
	case "indexed-v1":
		algo = RefAlgoMessageIndexed
	default:
		return nil, ErrInvalidResource
	}

	data, err := base64.URLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, ErrInvalidEncoding
	}

	ref, err := NewMessageRef(data, algo)
	if err != nil {
		return nil, err
	}

	return &MessageURI{Ref: ref}, nil
}

func parseBlobURI(parts []string) (*BlobURI, error) {
	if len(parts) < 2 {
		return nil, ErrInvalidResource
	}

	if parts[1] != "classic" && parts[1] != "sha256" {
		return nil, ErrInvalidResource
	}

	if len(parts) < 3 {
		return nil, ErrInvalidEncoding
	}

	data, err := base64.URLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, ErrInvalidEncoding
	}

	ref, err := NewBlobRef(data)
	if err != nil {
		return nil, err
	}

	return &BlobURI{Ref: ref}, nil
}

func feedURIFormat(algo RefAlgo) (string, bool) {
	switch algo {
	case RefAlgoFeedSSB1:
		return "classic", true
	case RefAlgoFeedBendyButt:
		return "bendybutt-v1", true
	case RefAlgoFeedGabby:
		return "gabbygrove-v1", true
	case RefAlgoFeedBamboo:
		return "bamboo", true
	case RefAlgoFeedIndexed:
		return "indexed-v1", true
	default:
		return "", false
	}
}

func messageURIFormat(algo RefAlgoMessage) (string, bool) {
	switch algo {
	case RefAlgoMessageSSB1:
		return "classic", true
	case RefAlgoMessageBendyButt:
		return "bendybutt-v1", true
	case RefAlgoMessageGabby:
		return "gabbygrove-v1", true
	case RefAlgoMessageBamboo:
		return "bamboo", true
	case RefAlgoMessageIndexed:
		return "indexed-v1", true
	default:
		return "", false
	}
}
