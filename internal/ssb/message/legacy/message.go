package legacy

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
)

type Message struct {
	Previous  *refs.MessageRef `json:"previous,omitempty"`
	Author    refs.FeedRef     `json:"author"`
	Sequence  int64            `json:"sequence"`
	Timestamp int64            `json:"timestamp"`
	Hash      string           `json:"hash"`
	Content   interface{}      `json:"content"`
}

type SignedMessage struct {
	Previous  *refs.MessageRef `json:"previous,omitempty"`
	Author    refs.FeedRef     `json:"author"`
	Sequence  int64            `json:"sequence"`
	Timestamp int64            `json:"timestamp"`
	Hash      string           `json:"hash"`
	Content   interface{}      `json:"content"`
	Signature []byte           `json:"signature"`
}

type StoredMessage struct {
	Key       refs.MessageRef `json:"key"`
	Value     SignedMessage   `json:"value"`
	Timestamp float64         `json:"timestamp"`
}

const HashAlgorithm = "sha256"

func (m *Message) Copy() *Message {
	return &Message{
		Previous:  m.Previous,
		Author:    m.Author,
		Sequence:  m.Sequence,
		Timestamp: m.Timestamp,
		Hash:      m.Hash,
		Content:   m.Content,
	}
}

func (m *SignedMessage) Verify() error {
	msg := &Message{
		Previous:  m.Previous,
		Author:    m.Author,
		Sequence:  m.Sequence,
		Timestamp: m.Timestamp,
		Hash:      m.Hash,
		Content:   m.Content,
	}
	contentToSign, err := msg.marshalForSigning()
	if err != nil {
		return err
	}

	algo := m.Author.Algo()
	if algo != refs.RefAlgoFeedSSB1 && algo != refs.RefAlgoFeedBendyButt {
		return fmt.Errorf("legacy: unsupported feed algorithm: %s", algo)
	}

	if !verifySignature(m.Author.PubKey(), contentToSign, m.Signature) {
		return fmt.Errorf("legacy: invalid signature")
	}

	return nil
}

func (m *SignedMessage) SignedMessageWithoutSignature() []byte {
	msg := struct {
		Previous  *refs.MessageRef `json:"previous,omitempty"`
		Author    refs.FeedRef     `json:"author"`
		Sequence  int64            `json:"sequence"`
		Timestamp int64            `json:"timestamp"`
		Hash      string           `json:"hash"`
		Content   interface{}      `json:"content"`
	}{
		Previous:  m.Previous,
		Author:    m.Author,
		Sequence:  m.Sequence,
		Timestamp: m.Timestamp,
		Hash:      m.Hash,
		Content:   m.Content,
	}

	data, _ := json.Marshal(msg)
	return CanonicalJSON(data)
}

func (m *SignedMessage) ContentHash() []byte {
	data := m.SignedMessageWithoutSignature()
	h := sha256.Sum256(data)
	return h[:]
}

func verifySignature(pubKey []byte, content, sig []byte) bool {
	if len(sig) != ed25519.SignatureSize || len(pubKey) != ed25519.PublicKeySize {
		return false
	}
	return ed25519.Verify(pubKey, content, sig)
}

func PrettyPrint(input []byte) ([]byte, error) {
	var buf bytes.Buffer
	if err := formatJSON(&buf, input, 0); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func formatJSON(buf *bytes.Buffer, data []byte, depth int) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil
	}

	indent := strings.Repeat("  ", depth)

	switch data[0] {
	case '{':
		return formatObject(buf, data, depth, indent)
	case '[':
		return formatArray(buf, data, depth, indent)
	default:
		buf.Write(data)
		return nil
	}
}

func formatObject(buf *bytes.Buffer, data []byte, depth int, indent string) error {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil {
		buf.Write(data)
		return nil
	}

	buf.WriteString("{\n")

	// Preserve key order using a manual scan of the raw data.
	// Go's json.Unmarshal sorts map keys, which breaks SSB verification.
	keys := getKeysInOrder(data)
	if len(keys) == 0 {
		// Fallback to sorted keys if order extraction fails
		keys = make([]string, 0, len(obj))
		for k := range obj {
			keys = append(keys, k)
		}
		sortStrings(keys)
	}

	for i, k := range keys {
		val, ok := obj[k]
		if !ok {
			continue
		}
		if i > 0 {
			buf.WriteString(",\n")
		}
		buf.WriteString(indent)
		buf.WriteString("  \"")
		buf.WriteString(k)
		buf.WriteString("\": ")

		if err := formatJSON(buf, val, depth+1); err != nil {
			return err
		}
	}

	buf.WriteString("\n")
	buf.WriteString(indent)
	buf.WriteString("}")
	return nil
}

func getKeysInOrder(data []byte) []string {
	var keys []string
	dec := json.NewDecoder(bytes.NewReader(data))
	t, err := dec.Token()
	if err != nil || t != json.Delim('{') {
		return nil
	}
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			break
		}
		if s, ok := t.(string); ok {
			keys = append(keys, s)
		}
		// Skip the value
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			break
		}
	}
	return keys
}

func formatArray(buf *bytes.Buffer, data []byte, depth int, indent string) error {
	var arr []json.RawMessage
	if err := json.Unmarshal(data, &arr); err != nil {
		buf.Write(data)
		return nil
	}

	buf.WriteString("[")
	hasContent := len(arr) > 0

	for i, elem := range arr {
		if i > 0 {
			buf.WriteString(", ")
		}
		if hasContent {
			buf.WriteString("\n")
			buf.WriteString(indent)
			buf.WriteString("  ")
		}
		if err := formatJSON(buf, elem, depth+1); err != nil {
			return err
		}
	}

	if hasContent {
		buf.WriteString("\n")
		buf.WriteString(indent)
	}
	buf.WriteString("]")
	return nil
}

func sortStrings(s []string) {
	for i := 0; i < len(s)-1; i++ {
		for j := i + 1; j < len(s); j++ {
			if s[i] > s[j] {
				s[i], s[j] = s[j], s[i]
			}
		}
	}
}

func CanonicalJSON(data []byte) []byte {
	canonical, err := PrettyPrint(data)
	if err != nil {
		return data
	}
	return canonical
}

func HashMessage(content []byte) []byte {
	v8Encoded := V8Binary(content)
	h := sha256.Sum256(v8Encoded)
	return h[:]
}

// V8Binary implements the specific quirky encoding used in SSB message hashing.
// It mimics how legacy Node.js/V8 implementations hashed strings by converting
// to UTF-16 and taking only the low 8 bits of each code unit.
func V8Binary(data []byte) []byte {
	runes := []rune(string(data))
	var quirky []byte
	for _, r := range runes {
		if r < 0x10000 {
			quirky = append(quirky, byte(r&0xff))
		} else {
			// Surrogate pair for non-BMP characters
			r -= 0x10000
			h := 0xd800 + (r >> 10)
			l := 0xdc00 + (r & 0x3ff)
			quirky = append(quirky, byte(h&0xff), byte(l&0xff))
		}
	}
	return quirky
}

func escapeString(s string) string {
	var result []byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '"', '\\':
			result = append(result, '\\', c)
		case '\b':
			result = append(result, '\\', 'b')
		case '\f':
			result = append(result, '\\', 'f')
		case '\n':
			result = append(result, '\\', 'n')
		case '\r':
			result = append(result, '\\', 'r')
		case '\t':
			result = append(result, '\\', 't')
		default:
			if c < 0x20 {
				// Escape control characters as \u00xx
				result = append(result, []byte(fmt.Sprintf("\\u%04x", c))...)
			} else {
				result = append(result, c)
			}
		}
	}
	return string(result)
}

func NewMessageRef(hash []byte) (*refs.MessageRef, error) {
	return refs.NewMessageRef(hash, refs.RefAlgoMessageSSB1)
}

func (m *SignedMessage) ToStoredMessage() (*StoredMessage, error) {
	contentHash := m.ContentHash()
	msgRef, err := NewMessageRef(contentHash)
	if err != nil {
		return nil, err
	}

	return &StoredMessage{
		Key:       *msgRef,
		Value:     *m,
		Timestamp: float64(m.Timestamp),
	}, nil
}

func (m *SignedMessage) HashContent() ([]byte, error) {
	data, err := json.Marshal(m.Content)
	if err != nil {
		return nil, err
	}
	return CanonicalJSON(data), nil
}
