package refs

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

var (
	ErrInvalidFeedRef    = errors.New("refs: invalid feed ref")
	ErrInvalidMessageRef = errors.New("refs: invalid message ref")
	ErrInvalidBlobRef    = errors.New("refs: invalid blob ref")
	ErrInvalidSSBURI     = errors.New("refs: invalid SSB URI")
)

const (
	SigilFeed    = '@'
	SigilMessage = '%'
	SigilBlob    = '&'
)

type RefAlgo string

const (
	RefAlgoFeedSSB1      RefAlgo = "ed25519"
	RefAlgoFeedBendyButt RefAlgo = "bendybutt-v1"
	RefAlgoFeedGabby     RefAlgo = "gabbygrove-v1"
)

type RefAlgoMessage string

const (
	RefAlgoMessageSSB1      RefAlgoMessage = "sha256"
	RefAlgoMessageBendyButt RefAlgoMessage = "bendybutt-v1"
)

type FeedRef struct {
	algo RefAlgo
	id   [32]byte
}

func (f FeedRef) PubKey() []byte {
	return f.id[:]
}

func (f FeedRef) Algo() RefAlgo {
	return f.algo
}

func (f FeedRef) String() string {
	return "@" + base64.StdEncoding.EncodeToString(f.id[:]) + "." + string(f.algo)
}

func (f FeedRef) Ref() string {
	return f.String()
}

func (f FeedRef) Equal(other FeedRef) bool {
	return f.algo == other.algo && f.id == other.id
}

func (f FeedRef) MarshalJSON() ([]byte, error) {
	return json.Marshal(f.String())
}

func (f *FeedRef) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	parsed, err := ParseFeedRef(s)
	if err != nil {
		return fmt.Errorf("refs: unmarshal feed ref: %w", err)
	}
	*f = *parsed
	return nil
}

func NewFeedRef(id []byte, algo RefAlgo) (*FeedRef, error) {
	if len(id) != 32 {
		return nil, ErrInvalidFeedRef
	}
	var ref FeedRef
	ref.algo = algo
	copy(ref.id[:], id)
	return &ref, nil
}

func MustNewFeedRef(id []byte, algo RefAlgo) *FeedRef {
	ref, err := NewFeedRef(id, algo)
	if err != nil {
		panic(err)
	}
	return ref
}

func ParseFeedRef(s string) (*FeedRef, error) {
	s = strings.TrimSpace(s)
	if len(s) < 2 || s[0] != SigilFeed {
		return nil, ErrInvalidFeedRef
	}

	s = s[1:]

	algo := RefAlgoFeedSSB1
	if idx := strings.LastIndex(s, "."); idx > 0 {
		algoStr := s[idx+1:]
		s = s[:idx]
		switch RefAlgo(algoStr) {
		case RefAlgoFeedSSB1, RefAlgoFeedBendyButt, RefAlgoFeedGabby:
			algo = RefAlgo(algoStr)
		default:
			return nil, ErrInvalidFeedRef
		}
	}

	data, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, ErrInvalidFeedRef
	}

	return NewFeedRef(data, algo)
}

func ParseFeedRefB64(s string) (*FeedRef, error) {
	data, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, ErrInvalidFeedRef
	}
	return NewFeedRef(data, RefAlgoFeedSSB1)
}

type MessageRef struct {
	algo RefAlgoMessage
	hash [32]byte
}

func (m MessageRef) Hash() []byte {
	return m.hash[:]
}

func (m MessageRef) Algo() RefAlgoMessage {
	return m.algo
}

func (m MessageRef) String() string {
	return "%" + base64.StdEncoding.EncodeToString(m.hash[:]) + "." + string(m.algo)
}

func (m MessageRef) Ref() string {
	return m.String()
}

func (m MessageRef) MarshalJSON() ([]byte, error) {
	return json.Marshal(m.String())
}

func (m *MessageRef) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	parsed, err := ParseMessageRef(s)
	if err != nil {
		return fmt.Errorf("refs: unmarshal message ref: %w", err)
	}
	*m = *parsed
	return nil
}

func (m MessageRef) Equal(other MessageRef) bool {
	return m.algo == other.algo && m.hash == other.hash
}

func NewMessageRef(hash []byte, algo RefAlgoMessage) (*MessageRef, error) {
	if len(hash) != 32 {
		return nil, ErrInvalidMessageRef
	}
	var ref MessageRef
	ref.algo = algo
	copy(ref.hash[:], hash)
	return &ref, nil
}

func MustNewMessageRef(hash []byte, algo RefAlgoMessage) *MessageRef {
	ref, err := NewMessageRef(hash, algo)
	if err != nil {
		panic(err)
	}
	return ref
}

func ParseMessageRef(s string) (*MessageRef, error) {
	s = strings.TrimSpace(s)
	if len(s) < 2 || s[0] != SigilMessage {
		return nil, ErrInvalidMessageRef
	}

	s = s[1:]

	algo := RefAlgoMessageSSB1
	if idx := strings.LastIndex(s, "."); idx > 0 {
		algoStr := s[idx+1:]
		s = s[:idx]
		switch RefAlgoMessage(algoStr) {
		case RefAlgoMessageSSB1, RefAlgoMessageBendyButt:
			algo = RefAlgoMessage(algoStr)
		default:
			return nil, ErrInvalidMessageRef
		}
	}

	data, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, ErrInvalidMessageRef
	}

	return NewMessageRef(data, algo)
}

type BlobRef struct {
	hash [32]byte
}

func (b BlobRef) Hash() []byte {
	return b.hash[:]
}

func (b BlobRef) String() string {
	return "&" + base64.StdEncoding.EncodeToString(b.hash[:]) + ".sha256"
}

func (b BlobRef) Ref() string {
	return b.String()
}

func (b BlobRef) Equal(other BlobRef) bool {
	return b.hash == other.hash
}

func NewBlobRef(hash []byte) (*BlobRef, error) {
	if len(hash) != 32 {
		return nil, ErrInvalidBlobRef
	}
	var ref BlobRef
	copy(ref.hash[:], hash)
	return &ref, nil
}

func MustNewBlobRef(hash []byte) *BlobRef {
	ref, err := NewBlobRef(hash)
	if err != nil {
		panic(err)
	}
	return ref
}

func ParseBlobRef(s string) (*BlobRef, error) {
	s = strings.TrimSpace(s)
	if len(s) < 2 || s[0] != SigilBlob {
		return nil, ErrInvalidBlobRef
	}

	s = s[1:]

	if idx := strings.LastIndex(s, "."); idx > 0 {
		s = s[:idx]
	}

	data, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		// Fallback to raw (unpadded) for robustness
		data, err = base64.RawStdEncoding.DecodeString(s)
		if err != nil {
			return nil, ErrInvalidBlobRef
		}
	}

	return NewBlobRef(data)
}
