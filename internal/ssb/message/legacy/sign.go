package legacy

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/keys"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
)

var (
	ErrInvalidSignature       = errors.New("legacy: invalid signature")
	ErrSignatureTooShort      = errors.New("legacy: signature too short")
	ErrSignatureTooLong       = errors.New("legacy: signature too long")
	ErrInvalidBase64          = errors.New("legacy: invalid base64")
	ErrInvalidSignatureLength = errors.New("legacy: signature wrong length")
	ErrInvalidFeedAlgorithm   = errors.New("legacy: invalid feed algorithm")
	ErrSignatureNotFound      = errors.New("legacy: signature not found")
)

type Signature []byte

var signatureSuffix = []byte(".sig.ed25519")

func NewSignatureFromBase64(input []byte) (Signature, error) {
	if !bytes.HasSuffix(input, signatureSuffix) {
		return nil, ErrInvalidSignature
	}
	b64 := bytes.TrimSuffix(input, signatureSuffix)

	gotLen := base64.StdEncoding.DecodedLen(len(b64))
	if gotLen < ed25519.SignatureSize {
		return nil, ErrSignatureTooShort
	}
	if gotLen > ed25519.SignatureSize+2 {
		return nil, ErrSignatureTooLong
	}

	decoded := make([]byte, gotLen)
	n, err := base64.StdEncoding.Decode(decoded, b64)
	if err != nil {
		return nil, ErrInvalidBase64
	}

	if n != ed25519.SignatureSize {
		return nil, ErrInvalidSignatureLength
	}

	return decoded[:ed25519.SignatureSize], nil
}

func (s Signature) Verify(content []byte, r refs.FeedRef) error {
	algo := r.Algo()
	if algo != refs.RefAlgoFeedSSB1 && algo != refs.RefAlgoFeedBendyButt {
		return ErrInvalidFeedAlgorithm
	}

	if !ed25519.Verify(r.PubKey(), content, s) {
		return ErrInvalidSignature
	}

	return nil
}

func (m *Message) Sign(kp *keys.KeyPair, hmacKey []byte) (*refs.MessageRef, []byte, error) {
	m.Hash = HashAlgorithm

	contentToSign, err := m.marshalForSigning()
	if err != nil {
		return nil, nil, err
	}

	if hmacKey != nil {
		h := sha256.New()
		h.Write(hmacKey)
		h.Write(contentToSign)
		contentToSign = h.Sum(nil)
	}

	sig := ed25519.Sign(kp.Private(), contentToSign)

	msgToHash, err := m.marshalWithSignature(sig)
	if err != nil {
		return nil, nil, err
	}

	h := sha256.Sum256(msgToHash)
	msgRef, err := refs.NewMessageRef(h[:], refs.RefAlgoMessageSSB1)
	if err != nil {
		return nil, nil, err
	}

	return msgRef, sig, nil
}

func (m *Message) MarshalForSigning() ([]byte, error) {
	return m.marshalForSigning()
}

func (m *Message) marshalForSigning() ([]byte, error) {
	msg := struct {
		Previous  *refs.MessageRef `json:"previous,omitempty"`
		Author    string           `json:"author"`
		Sequence  int64            `json:"sequence"`
		Timestamp int64            `json:"timestamp"`
		Hash      string           `json:"hash"`
		Content   interface{}      `json:"content"`
	}{
		Previous:  m.Previous,
		Author:    m.Author.String(),
		Sequence:  m.Sequence,
		Timestamp: m.Timestamp,
		Hash:      m.Hash,
		Content:   m.Content,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return nil, err
	}

	return PrettyPrint(data)
}

func (m *Message) marshalWithSignature(sig []byte) ([]byte, error) {
	signed := struct {
		Previous  *refs.MessageRef `json:"previous,omitempty"`
		Author    string           `json:"author"`
		Sequence  int64            `json:"sequence"`
		Timestamp int64            `json:"timestamp"`
		Hash      string           `json:"hash"`
		Content   interface{}      `json:"content"`
		Signature string           `json:"signature"`
	}{
		Previous:  m.Previous,
		Author:    m.Author.String(),
		Sequence:  m.Sequence,
		Timestamp: m.Timestamp,
		Hash:      m.Hash,
		Content:   m.Content,
		Signature: base64.StdEncoding.EncodeToString(sig) + ".sig.ed25519",
	}

	data, err := json.Marshal(signed)
	if err != nil {
		return nil, err
	}

	return PrettyPrint(data)
}

func ExtractSignature(b []byte) ([]byte, Signature, error) {
	endIdx := bytes.LastIndex(b, []byte(`"`))
	if endIdx < 0 {
		return nil, nil, ErrSignatureNotFound
	}

	startIdx := bytes.LastIndex(b[:endIdx], []byte(`"`))
	if startIdx < 0 {
		return nil, nil, ErrSignatureNotFound
	}

	commaIdx := bytes.LastIndex(b[:startIdx], []byte(","))
	if commaIdx < 0 {
		return nil, nil, ErrSignatureNotFound
	}

	sigData, err := NewSignatureFromBase64(b[startIdx+1 : endIdx])
	if err != nil {
		return nil, nil, err
	}

	beforeSig := b[:commaIdx]
	afterSig := b[endIdx+1:]

	msg := make([]byte, len(beforeSig)+len(afterSig))
	copy(msg, beforeSig)
	copy(msg[len(beforeSig):], afterSig)

	return msg, sigData, nil
}
