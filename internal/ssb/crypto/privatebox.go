package crypto

import (
	"crypto/rand"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/curve25519"
	"golang.org/x/crypto/nacl/secretbox"
)

const (
	PrivateBoxNonceSize  = 24
	PrivateBoxHeaderSize = 32
	PrivateBoxKeySize    = 32
	PrivateBoxKeyBoxSize = 49
	MaxRecipients        = 7
)

var (
	ErrTooManyRecipients = errors.New("private-box: too many recipients (max 7)")
	ErrInvalidBox        = errors.New("private-box: invalid ciphertext")
	ErrPrivateBoxDecrypt = errors.New("private-box: decryption failed")
)

type PrivateBoxMessage struct {
	Format string `json:"format"`
	Nonce  string `json:"nonce"`
	Keys   string `json:"keys"`
	Header string `json:"header"`
	Body   string `json:"body"`
}

func EncryptPrivateBox(plaintext []byte, recipients [][]byte) ([]byte, error) {
	if len(recipients) == 0 {
		return nil, errors.New("private-box: no recipients")
	}
	if len(recipients) > MaxRecipients {
		return nil, ErrTooManyRecipients
	}

	var nonce [PrivateBoxNonceSize]byte
	if _, err := io.ReadFull(rand.Reader, nonce[:]); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}

	var msgKey [PrivateBoxKeySize]byte
	if _, err := io.ReadFull(rand.Reader, msgKey[:]); err != nil {
		return nil, fmt.Errorf("generate message key: %w", err)
	}

	body, err := encryptBody(plaintext, &nonce, &msgKey)
	if err != nil {
		return nil, err
	}

	header, keys, err := encryptKeysForRecipients(&nonce, &msgKey, recipients)
	if err != nil {
		return nil, err
	}

	msg := PrivateBoxMessage{
		Format: "private-box",
		Nonce:  base64.StdEncoding.EncodeToString(nonce[:]),
		Keys:   base64.StdEncoding.EncodeToString(keys),
		Header: base64.StdEncoding.EncodeToString(header),
		Body:   base64.StdEncoding.EncodeToString(body),
	}

	return json.Marshal(msg)
}

func encryptBody(plaintext []byte, nonce *[PrivateBoxNonceSize]byte, key *[PrivateBoxKeySize]byte) ([]byte, error) {
	return secretbox.Seal(nil, plaintext, nonce, key), nil
}

func encryptKeysForRecipients(nonce *[PrivateBoxNonceSize]byte, msgKey *[PrivateBoxKeySize]byte, recipients [][]byte) ([]byte, []byte, error) {
	var headerKey [32]byte
	if _, err := io.ReadFull(rand.Reader, headerKey[:]); err != nil {
		return nil, nil, fmt.Errorf("generate header key: %w", err)
	}

	var headerPriv [32]byte
	if _, err := io.ReadFull(rand.Reader, headerPriv[:]); err != nil {
		return nil, nil, fmt.Errorf("generate header private: %w", err)
	}
	headerPriv[0] &= 248
	headerPriv[31] &= 127
	headerPriv[31] |= 64

	var headerPub [32]byte
	curve25519.ScalarBaseMult(&headerPub, &headerPriv)

	keys := make([]byte, 0, len(recipients)*PrivateBoxKeyBoxSize)
	for i, recipientPub := range recipients {
		if len(recipientPub) != 32 {
			return nil, nil, errors.New("invalid recipient key length")
		}

		var recipient [32]byte
		copy(recipient[:], recipientPub)

		shared, err := curve25519.X25519(headerPriv[:], recipient[:])
		if err != nil {
			return nil, nil, fmt.Errorf("compute shared secret: %w", err)
		}

		var keyNonce [24]byte
		copy(keyNonce[:], nonce[:])
		keyNonce[0] ^= byte(i)

		var sharedKey [32]byte
		copy(sharedKey[:], shared)

		keyBox := secretbox.Seal(nil, msgKey[:], &keyNonce, &sharedKey)
		keys = append(keys, keyBox...)
	}

	return headerPub[:], keys, nil
}

func DecryptPrivateBox(ciphertext []byte, recipientPriv []byte) ([]byte, error) {
	if len(recipientPriv) != 32 {
		return nil, errors.New("invalid private key length")
	}

	curvePriv := ed25519ToCurve25519(recipientPriv)

	var msg PrivateBoxMessage
	if err := json.Unmarshal(ciphertext, &msg); err != nil {
		return nil, fmt.Errorf("parse message: %w", err)
	}

	if msg.Format != "private-box" {
		return nil, fmt.Errorf("unsupported format: %s", msg.Format)
	}

	nonceBytes, err := base64.StdEncoding.DecodeString(msg.Nonce)
	if err != nil || len(nonceBytes) != PrivateBoxNonceSize {
		return nil, ErrInvalidBox
	}

	keysBytes, err := base64.StdEncoding.DecodeString(msg.Keys)
	if err != nil {
		return nil, ErrInvalidBox
	}

	headerBytes, err := base64.StdEncoding.DecodeString(msg.Header)
	if err != nil || len(headerBytes) != PrivateBoxHeaderSize {
		return nil, ErrInvalidBox
	}

	bodyBytes, err := base64.StdEncoding.DecodeString(msg.Body)
	if err != nil {
		return nil, ErrInvalidBox
	}

	var nonce [PrivateBoxNonceSize]byte
	copy(nonce[:], nonceBytes)

	var headerPub [32]byte
	copy(headerPub[:], headerBytes)

	curve25519.ScalarBaseMult(&headerPub, &curvePriv)

	numRecipients := len(keysBytes) / PrivateBoxKeyBoxSize

	var msgKey [PrivateBoxKeySize]byte
	var found bool

	for i := 0; i < numRecipients; i++ {
		keyBox := keysBytes[i*PrivateBoxKeyBoxSize : (i+1)*PrivateBoxKeyBoxSize]

		var keyNonce [24]byte
		copy(keyNonce[:], nonce[:])
		keyNonce[0] ^= byte(i)

		var sharedWithRecipient [32]byte
		curve25519.ScalarMult(&sharedWithRecipient, &curvePriv, &headerPub)

		var sharedKey [32]byte
		copy(sharedKey[:], sharedWithRecipient[:])

		decryptedKey, ok := secretbox.Open(nil, keyBox, &keyNonce, &sharedKey)
		if ok && len(decryptedKey) == PrivateBoxKeySize {
			copy(msgKey[:], decryptedKey)
			found = true
			break
		}
	}

	if !found {
		return nil, ErrPrivateBoxDecrypt
	}

	plaintext, ok := secretbox.Open(nil, bodyBytes, &nonce, &msgKey)
	if !ok {
		return nil, ErrPrivateBoxDecrypt
	}

	return plaintext, nil
}

func ed25519ToCurve25519(edPriv []byte) [32]byte {
	h := sha512.Sum512(edPriv)
	s := h[:32]
	s[0] &= 248
	s[31] &= 127
	s[31] |= 64
	var out [32]byte
	copy(out[:], s)
	return out
}

func PrivateBoxDecryptWithFeedKey(ciphertext []byte, feedKeyPair *KeyPair) ([]byte, error) {
	return DecryptPrivateBox(ciphertext, feedKeyPair.Secret())
}

type KeyPair struct {
	public [32]byte
	secret [32]byte
}

func NewKeyPairFromSecret(secret []byte) (*KeyPair, error) {
	if len(secret) != 32 {
		return nil, errors.New("invalid secret key length")
	}

	var kp KeyPair
	copy(kp.secret[:], secret)

	var pubKey [32]byte
	curve25519.ScalarBaseMult(&pubKey, &kp.secret)
	copy(kp.public[:], pubKey[:])

	return &kp, nil
}

func (kp *KeyPair) Public() []byte {
	return kp.public[:]
}

func (kp *KeyPair) Secret() []byte {
	return kp.secret[:]
}
