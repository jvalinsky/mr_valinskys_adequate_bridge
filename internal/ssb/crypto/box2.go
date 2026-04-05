package crypto

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"golang.org/x/crypto/curve25519"
	"golang.org/x/crypto/nacl/box"
)

const (
	NonceSize = 24
)

var (
	ErrInvalidCiphertext = errors.New("dm: invalid ciphertext")
	ErrDecryptionFailed  = errors.New("dm: decryption failed")
	ErrMissingRecipient  = errors.New("dm: missing recipient")
	ErrInvalidRecipient  = errors.New("dm: invalid recipient format")
)

type DMContent struct {
	Content   interface{} `json:"content"`
	Recipient string      `json:"recipient"`
}

type EncryptedMessage struct {
	Format     string `json:"format"`
	Ciphertext string `json:"ciphertext"`
	Nonce      string `json:"nonce"`
}

func EncryptDM(plaintext []byte, senderPub [32]byte, senderPriv [32]byte, recipientPub [32]byte) ([]byte, error) {
	sharedPub, err := curve25519.X25519(senderPriv[:], recipientPub[:])
	if err != nil {
		return nil, fmt.Errorf("compute shared secret: %w", err)
	}

	var nonce [NonceSize]byte
	if _, err := io.ReadFull(rand.Reader, nonce[:]); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}

	var shared [32]byte
	copy(shared[:], sharedPub)

	var ciphertext []byte
	ciphertext = box.SealAfterPrecomputation(ciphertext, plaintext, &nonce, &shared)

	em := EncryptedMessage{
		Format:     "box2",
		Ciphertext: base64.StdEncoding.EncodeToString(ciphertext),
		Nonce:      base64.StdEncoding.EncodeToString(nonce[:]),
	}

	return json.Marshal(em)
}

func DecryptDM(ciphertext []byte, recipientPub [32]byte, recipientPriv [32]byte, senderPub [32]byte) ([]byte, error) {
	sharedPub, err := curve25519.X25519(recipientPriv[:], senderPub[:])
	if err != nil {
		return nil, fmt.Errorf("compute shared secret: %w", err)
	}

	var em EncryptedMessage
	if err := json.Unmarshal(ciphertext, &em); err != nil {
		return nil, fmt.Errorf("parse encrypted message: %w", err)
	}

	if em.Format != "box2" {
		return nil, fmt.Errorf("unsupported format: %s", em.Format)
	}

	ciphertextBytes, err := base64.StdEncoding.DecodeString(em.Ciphertext)
	if err != nil {
		return nil, fmt.Errorf("decode ciphertext: %w", err)
	}

	nonceBytes, err := base64.StdEncoding.DecodeString(em.Nonce)
	if err != nil {
		return nil, fmt.Errorf("decode nonce: %w", err)
	}

	if len(nonceBytes) != NonceSize {
		return nil, fmt.Errorf("invalid nonce length: %d", len(nonceBytes))
	}

	var nonce [NonceSize]byte
	copy(nonce[:], nonceBytes)

	var shared [32]byte
	copy(shared[:], sharedPub)

	plaintext := make([]byte, 0, len(ciphertextBytes)-box.Overhead)
	var ok bool
	if plaintext, ok = box.OpenAfterPrecomputation(plaintext, ciphertextBytes, &nonce, &shared); !ok {
		return nil, ErrDecryptionFailed
	}

	return plaintext, nil
}

func WrapContentForDM(content interface{}, recipient string) ([]byte, error) {
	dmContent := DMContent{
		Content:   content,
		Recipient: recipient,
	}
	return json.Marshal(dmContent)
}

func ParseRecipient(feedID string) ([]byte, error) {
	feedID = strings.TrimSpace(feedID)

	if !strings.HasPrefix(feedID, "@") {
		return nil, ErrInvalidRecipient
	}

	feedID = feedID[1:]

	if idx := strings.LastIndex(feedID, "."); idx > 0 {
		feedID = feedID[:idx]
	}

	data, err := base64.StdEncoding.DecodeString(feedID)
	if err != nil {
		return nil, fmt.Errorf("decode feed ID: %w", err)
	}

	if len(data) != 32 {
		return nil, fmt.Errorf("invalid pubkey length: %d", len(data))
	}

	return data, nil
}

func UnwrapDMContent(plaintext []byte) (*DMContent, error) {
	var content DMContent
	if err := json.Unmarshal(plaintext, &content); err != nil {
		return nil, fmt.Errorf("parse DM content: %w", err)
	}

	if content.Recipient == "" {
		return nil, ErrMissingRecipient
	}

	return &content, nil
}
