package bendy

import (
	"crypto/ed25519"
	"crypto/sha256"
	"errors"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/bfe"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/keys"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/message/bencode"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
)

const (
	MaxMessageSize = 8192
)

var (
	ErrInvalidMessage   = errors.New("bendy: invalid message")
	ErrInvalidSequence  = errors.New("bendy: invalid sequence number")
	ErrInvalidAuthor    = errors.New("bendy: invalid author format")
	ErrInvalidPrevious  = errors.New("bendy: invalid previous format")
	ErrInvalidTimestamp = errors.New("bendy: invalid timestamp")
	ErrMessageTooLarge  = errors.New("bendy: message too large")
	ErrInvalidContent   = errors.New("bendy: invalid content section")
	ErrInvalidSignature = errors.New("bendy: invalid signature")

	bendySignPrefix        = []byte("bendybutt")
	bendyFeedFormatCode    = bfe.FeedFormatCodes["bendybutt-v1"]
	bendyMessageFormatCode = bfe.MessageFormatCodes["bendybutt-v1"]
)

type Message struct {
	Author         []byte
	Sequence       int64
	Previous       []byte
	Timestamp      int64
	ContentSection []interface{}
	Signature      []byte
}

func (m *Message) Validate() error {
	if m.Sequence < 1 {
		return ErrInvalidSequence
	}

	if len(m.Author) != 34 || m.Author[0] != bfe.TypeFeed || m.Author[1] != bendyFeedFormatCode {
		return ErrInvalidAuthor
	}

	if m.Sequence == 1 {
		if !isBFENil(m.Previous) {
			return ErrInvalidPrevious
		}
	} else {
		if len(m.Previous) != 34 || m.Previous[0] != bfe.TypeMessage || m.Previous[1] != bendyMessageFormatCode {
			return ErrInvalidPrevious
		}
	}

	if m.Timestamp < 0 {
		return ErrInvalidTimestamp
	}

	if len(m.ContentSection) != 2 {
		return ErrInvalidContent
	}

	return nil
}

func isBFENil(v []byte) bool {
	nilBFE := bfe.EncodeNil()
	return len(v) == len(nilBFE) && v[0] == nilBFE[0] && v[1] == nilBFE[1]
}

func (m *Message) ToBencode() interface{} {
	header := []interface{}{
		m.Author,
		m.Sequence,
		m.Previous,
		m.Timestamp,
		m.ContentSection,
	}
	return []interface{}{
		header,
		m.Signature,
	}
}

func (m *Message) Key() ([]byte, error) {
	encoded, err := bencode.Encode(m.ToBencode())
	if err != nil {
		return nil, err
	}

	if len(encoded) > MaxMessageSize {
		return nil, ErrMessageTooLarge
	}

	h := sha256.Sum256(encoded)
	return h[:], nil
}

func (m *Message) Sign(kp *keys.KeyPair) ([]byte, error) {
	if len(m.ContentSection) != 2 {
		return nil, ErrInvalidContent
	}

	contentBytes, err := bencode.Encode(m.ContentSection[0])
	if err != nil {
		return nil, err
	}

	msgToSign := make([]byte, 0, len(bendySignPrefix)+len(contentBytes))
	msgToSign = append(msgToSign, bendySignPrefix...)
	msgToSign = append(msgToSign, contentBytes...)
	contentSig := ed25519.Sign(kp.Private(), msgToSign)
	m.ContentSection[1] = bfe.EncodeSignature(contentSig)

	payloadBytes, err := bencode.Encode([]interface{}{
		m.Author,
		m.Sequence,
		m.Previous,
		m.Timestamp,
		m.ContentSection,
	})
	if err != nil {
		return nil, err
	}

	m.Signature = bfe.EncodeSignature(ed25519.Sign(kp.Private(), payloadBytes))
	return m.Signature, nil
}

func (m *Message) Verify() error {
	if err := m.Validate(); err != nil {
		return err
	}

	if len(m.Author) < 3 {
		return ErrInvalidAuthor
	}

	authorPubKey := m.Author[2:34]
	contentSigField := asBytes(m.ContentSection[1])
	if contentSigField == nil {
		return ErrInvalidSignature
	}
	contentSig, err := bfe.DecodeSignature(contentSigField)
	if err != nil {
		return ErrInvalidSignature
	}

	contentBytes, err := bencode.Encode(m.ContentSection[0])
	if err != nil {
		return err
	}

	msgToVerify := make([]byte, 0, len(bendySignPrefix)+len(contentBytes))
	msgToVerify = append(msgToVerify, bendySignPrefix...)
	msgToVerify = append(msgToVerify, contentBytes...)

	if !ed25519.Verify(authorPubKey, msgToVerify, contentSig) {
		return ErrInvalidSignature
	}

	payloadSig, err := bfe.DecodeSignature(m.Signature)
	if err != nil {
		return ErrInvalidSignature
	}
	payloadBytes, err := bencode.Encode([]interface{}{
		m.Author,
		m.Sequence,
		m.Previous,
		m.Timestamp,
		m.ContentSection,
	})
	if err != nil {
		return err
	}

	if !ed25519.Verify(authorPubKey, payloadBytes, payloadSig) {
		return ErrInvalidSignature
	}

	return nil
}

func (m *Message) ToRefsMessage() (*refs.MessageRef, error) {
	key, err := m.Key()
	if err != nil {
		return nil, err
	}

	return refs.NewMessageRef(key, refs.RefAlgoMessageBendyButt)
}

func asBytes(v interface{}) []byte {
	switch val := v.(type) {
	case []byte:
		return val
	case string:
		return []byte(val)
	default:
		return nil
	}
}

func FromStoredMessage(encoded []byte) (*Message, error) {
	decoded, err := bencode.DecodeBytes(encoded)
	if err != nil {
		return nil, err
	}

	list, ok := decoded.([]interface{})
	if !ok {
		return nil, ErrInvalidMessage
	}

	if len(list) != 2 {
		return nil, ErrInvalidMessage
	}

	payload, ok := list[0].([]interface{})
	if !ok {
		return nil, ErrInvalidMessage
	}

	sig := asBytes(list[1])
	if sig == nil {
		return nil, ErrInvalidSignature
	}

	if len(payload) != 5 {
		return nil, ErrInvalidMessage
	}

	msg := &Message{
		Signature: sig,
	}

	author := asBytes(payload[0])
	if author == nil {
		return nil, ErrInvalidAuthor
	}
	msg.Author = author

	seq, ok := payload[1].(int64)
	if !ok {
		return nil, ErrInvalidSequence
	}
	msg.Sequence = seq

	prev := asBytes(payload[2])
	if prev == nil && payload[2] != nil {
		return nil, ErrInvalidPrevious
	}
	msg.Previous = prev

	ts, ok := payload[3].(int64)
	if !ok {
		return nil, ErrInvalidTimestamp
	}
	msg.Timestamp = ts

	cs, ok := payload[4].([]interface{})
	if !ok {
		return nil, ErrInvalidContent
	}
	msg.ContentSection = convertStringsToBytes(cs).([]interface{})

	return msg, nil
}

func convertStringsToBytes(v interface{}) interface{} {
	switch val := v.(type) {
	case string:
		return []byte(val)
	case []interface{}:
		result := make([]interface{}, len(val))
		for i, v := range val {
			result[i] = convertStringsToBytes(v)
		}
		return result
	case map[string]interface{}:
		result := make(map[string]interface{})
		for k, v := range val {
			result[k] = convertStringsToBytes(v)
		}
		return result
	default:
		return v
	}
}

func (m *Message) Encode() ([]byte, error) {
	encoded, err := bencode.Encode(m.ToBencode())
	if err != nil {
		return nil, err
	}

	if len(encoded) > MaxMessageSize {
		return nil, ErrMessageTooLarge
	}

	return encoded, nil
}

func CreateMessage(author []byte, sequence int64, previous []byte, timestamp int64, content map[string]interface{}, kp *keys.KeyPair) (*Message, error) {
	contentBFE := encodeContentToBFE(content)

	if sequence == 1 && len(previous) == 0 {
		previous = bfe.EncodeNil()
	}

	contentSection := []interface{}{contentBFE, bfe.EncodeSignature(make([]byte, ed25519.SignatureSize))}

	msg := &Message{
		Author:         author,
		Sequence:       sequence,
		Previous:       previous,
		Timestamp:      timestamp,
		ContentSection: contentSection,
	}

	if err := msg.Validate(); err != nil {
		return nil, err
	}

	if _, err := msg.Sign(kp); err != nil {
		return nil, err
	}

	return msg, nil
}

func encodeContentToBFE(content map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})
	for k, v := range content {
		result[string(bfe.EncodeString(k)[2:])] = encodeValueToBFE(v)
	}
	return result
}

func encodeValueToBFE(v interface{}) interface{} {
	switch val := v.(type) {
	case string:
		return bfe.EncodeString(val)[2:]
	case bool:
		if val {
			return []byte{bfe.TypeGeneric, 0x01, 0x01}
		}
		return []byte{bfe.TypeGeneric, 0x01, 0x00}
	case nil:
		return bfe.EncodeNil()
	case int, int64, int32:
		return val
	case map[string]interface{}:
		return encodeContentToBFE(val)
	case []interface{}:
		result := make([]interface{}, len(val))
		for i, elem := range val {
			result[i] = encodeValueToBFE(elem)
		}
		return result
	default:
		return val
	}
}
