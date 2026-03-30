package legacy

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strconv"

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
	buf := &bytes.Buffer{}
	buf.WriteString("{\n")

	buf.WriteString(`  "previous": `)
	if m.Previous != nil {
		buf.WriteString(`"` + m.Previous.String() + `"`)
	} else {
		buf.WriteString("null")
	}
	buf.WriteString(",\n")

	buf.WriteString(`  "author": "`)
	buf.WriteString(m.Author.String())
	buf.WriteString(`",` + "\n")

	buf.WriteString(`  "sequence": `)
	buf.WriteString(strconv.FormatInt(m.Sequence, 10))
	buf.WriteString(",\n")

	buf.WriteString(`  "timestamp": `)
	buf.WriteString(strconv.FormatInt(m.Timestamp, 10))
	buf.WriteString(",\n")

	buf.WriteString(`  "hash": "`)
	buf.WriteString(m.Hash)
	buf.WriteString(`",` + "\n")

	buf.WriteString(`  "content": `)
	// Content must be indented with JSON.stringify(msg, null, 2) semantics.
	// At depth 1, nested keys use prefix "  " (parent depth) + indent "  " per level.
	contentBytes, err := json.MarshalIndent(m.Content, "  ", "  ")
	if err != nil {
		return nil, err
	}
	buf.Write(contentBytes)
	buf.WriteString("\n")

	buf.WriteString("}")
	return buf.Bytes(), nil
}

func (m *Message) marshalWithSignature(sig []byte) ([]byte, error) {
	buf := &bytes.Buffer{}
	buf.WriteString("{\n")

	buf.WriteString(`  "previous": `)
	if m.Previous != nil {
		buf.WriteString(`"` + m.Previous.String() + `"`)
	} else {
		buf.WriteString("null")
	}
	buf.WriteString(",\n")

	buf.WriteString(`  "author": "`)
	buf.WriteString(m.Author.String())
	buf.WriteString(`",` + "\n")

	buf.WriteString(`  "sequence": `)
	buf.WriteString(strconv.FormatInt(m.Sequence, 10))
	buf.WriteString(",\n")

	buf.WriteString(`  "timestamp": `)
	buf.WriteString(strconv.FormatInt(m.Timestamp, 10))
	buf.WriteString(",\n")

	buf.WriteString(`  "hash": "`)
	buf.WriteString(m.Hash)
	buf.WriteString(`",` + "\n")

	buf.WriteString(`  "content": `)
	contentBytes, err := json.MarshalIndent(m.Content, "  ", "  ")
	if err != nil {
		return nil, err
	}
	buf.Write(contentBytes)
	buf.WriteString(",\n")

	buf.WriteString(`  "signature": "`)
	buf.WriteString(base64.StdEncoding.EncodeToString(sig))
	buf.WriteString(`.sig.ed25519"` + "\n")

	buf.WriteString("}")
	return buf.Bytes(), nil
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
