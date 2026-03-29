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

	if len(m.Author) != 34 || m.Author[0] != bfe.TypeFeed {
		return ErrInvalidAuthor
	}

	if m.Previous == nil {
	} else if len(m.Previous) != 34 || m.Previous[0] != bfe.TypeMessage {
		return ErrInvalidPrevious
	}

	if m.Timestamp < 0 {
		return ErrInvalidTimestamp
	}

	if len(m.ContentSection) < 2 {
		return ErrInvalidContent
	}

	return nil
}

func (m *Message) Key() ([]byte, error) {
	encoded, err := bencode.Encode(m)
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
	contentBytes, err := bencode.Encode(m.ContentSection)
	if err != nil {
		return nil, err
	}

	prefix := []byte("bendybutt")
	msgToSign := make([]byte, 0, len(prefix)+len(contentBytes))
	msgToSign = append(msgToSign, prefix...)
	msgToSign = append(msgToSign, contentBytes...)

	sig := ed25519.Sign(kp.Private(), msgToSign)
	return sig, nil
}

func (m *Message) Verify() error {
	if err := m.Validate(); err != nil {
		return err
	}

	if len(m.Signature) != ed25519.SignatureSize {
		return ErrInvalidSignature
	}

	if len(m.Author) < 3 {
		return ErrInvalidAuthor
	}

	authorPubKey := m.Author[2:34]

	contentBytes, err := bencode.Encode(m.ContentSection)
	if err != nil {
		return err
	}

	prefix := []byte("bendybutt")
	msgToVerify := make([]byte, 0, len(prefix)+len(contentBytes))
	msgToVerify = append(msgToVerify, prefix...)
	msgToVerify = append(msgToVerify, contentBytes...)

	if !ed25519.Verify(authorPubKey, msgToVerify, m.Signature) {
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

	sig, ok := list[1].([]byte)
	if !ok {
		return nil, ErrInvalidSignature
	}

	if len(payload) != 5 {
		return nil, ErrInvalidMessage
	}

	msg := &Message{
		Signature: sig,
	}

	author, ok := payload[0].([]byte)
	if !ok {
		return nil, ErrInvalidAuthor
	}
	msg.Author = author

	seq, ok := payload[1].(int64)
	if !ok {
		return nil, ErrInvalidSequence
	}
	msg.Sequence = seq

	prev, ok := payload[2].([]byte)
	if !ok {
		if payload[2] != nil {
			return nil, ErrInvalidPrevious
		}
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
	msg.ContentSection = cs

	return msg, nil
}

func (m *Message) Encode() ([]byte, error) {
	encoded, err := bencode.Encode(m)
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

	contentSection := []interface{}{contentBFE, nil}

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

	sig, err := msg.Sign(kp)
	if err != nil {
		return nil, err
	}

	msg.ContentSection[1] = sig

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
			return []byte{bfe.TypeGeneric, 0x01}
		}
		return []byte{bfe.TypeGeneric, 0x01}
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
