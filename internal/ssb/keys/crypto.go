package keys

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"

	"golang.org/x/crypto/nacl/secretbox"
	"golang.org/x/crypto/scrypt"
)

const (
	SaltSize  = 32
	NonceSize = 24
	SeedSize  = 32
)

var (
	ErrInvalidCiphertext = errors.New("keys: invalid ciphertext")
	ErrDecryptionFailed  = errors.New("keys: decryption failed")
	ErrInvalidPassphrase = errors.New("keys: invalid passphrase")
)

func EncryptKeyWithPassword(kp *KeyPair, passphrase string) ([]byte, error) {
	if passphrase == "" {
		return nil, ErrInvalidPassphrase
	}

	salt := make([]byte, SaltSize)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, fmt.Errorf("generate salt: %w", err)
	}

	key, err := deriveKey(passphrase, salt)
	if err != nil {
		return nil, fmt.Errorf("derive key: %w", err)
	}

	seed := kp.Seed()

	var nonce [NonceSize]byte
	if _, err := io.ReadFull(rand.Reader, nonce[:]); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}

	var keyArray [32]byte
	copy(keyArray[:], key)

	var ciphertext []byte
	ciphertext = secretbox.Seal(ciphertext, seed[:], &nonce, &keyArray)

	ciphertextBytes := make([]byte, 0, SaltSize+NonceSize+len(ciphertext))
	ciphertextBytes = append(ciphertextBytes, salt...)
	ciphertextBytes = append(ciphertextBytes, nonce[:]...)
	ciphertextBytes = append(ciphertextBytes, ciphertext...)

	return ciphertextBytes, nil
}

func DecryptKeyWithPassword(data []byte, passphrase string) (*KeyPair, error) {
	if passphrase == "" {
		return nil, ErrInvalidPassphrase
	}

	if len(data) < SaltSize+NonceSize+SeedSize {
		return nil, ErrInvalidCiphertext
	}

	salt := data[:SaltSize]
	nonce := data[SaltSize : SaltSize+NonceSize]
	ciphertext := data[SaltSize+NonceSize:]

	key, err := deriveKey(passphrase, salt)
	if err != nil {
		return nil, fmt.Errorf("derive key: %w", err)
	}

	var nonceArray [NonceSize]byte
	copy(nonceArray[:], nonce)

	var keyArray [32]byte
	copy(keyArray[:], key)

	var seedArray [SeedSize]byte
	_, ok := secretbox.Open(seedArray[:0], ciphertext, &nonceArray, &keyArray)
	if !ok {
		return nil, ErrDecryptionFailed
	}

	return FromSeed(seedArray), nil
}

func deriveKey(passphrase string, salt []byte) ([]byte, error) {
	return scrypt.Key([]byte(passphrase), salt, 1<<15, 8, 1, 32)
}

func (kp *KeyPair) SeedString() string {
	seed := kp.Seed()
	return base64.StdEncoding.EncodeToString(seed[:])
}

func (kp *KeyPair) SeedStringWithSuffix() string {
	return kp.SeedString() + ".ed25519"
}

func ParseSeedOrSecret(input string) (*KeyPair, error) {
	input = strings.TrimSpace(input)

	suffix := ".ed25519"
	if len(input) > len(suffix) && strings.HasSuffix(input, suffix) {
		input = strings.TrimSuffix(input, suffix)
	}

	if strings.Contains(input, `"`) || strings.Contains(input, ":") {
		return ParseSecret(strings.NewReader(input))
	}

	data, err := base64.StdEncoding.DecodeString(input)
	if err != nil {
		return nil, fmt.Errorf("decode input: %w", err)
	}

	if len(data) == 64 {
		var private [64]byte
		copy(private[:], data)
		kp := &KeyPair{}
		copy(kp.private[:], private[:])
		return kp, nil
	}

	if len(data) == 32 {
		var seed [32]byte
		copy(seed[:], data)
		return FromSeed(seed), nil
	}

	return nil, fmt.Errorf("invalid key length: %d bytes", len(data))
}

func (kp *KeyPair) ToBase64JSON() string {
	return fmt.Sprintf(`{
  "curve": "ed25519",
  "id": "%s",
  "private": "%s.ed25519",
  "public": "%s.ed25519"
}`,
		kp.FeedRef().String(),
		EncodePrivateKey(kp),
		EncodePublicKey(kp),
	)
}

func (kp *KeyPair) ToSeedOnly() string {
	return kp.SeedStringWithSuffix()
}
