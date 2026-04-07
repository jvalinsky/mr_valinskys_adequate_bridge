package keys

import (
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"io"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/secretstream"
)

var (
	ErrInvalidSeed    = errors.New("keys: invalid seed length")
	ErrInvalidKeyPair = errors.New("keys: invalid key pair")
	ErrSignFailed     = errors.New("keys: signing failed")
)

type KeyPair struct {
	private [64]byte
}

func (k *KeyPair) Private() ed25519.PrivateKey {
	return ed25519.PrivateKey(k.private[:])
}

func (k *KeyPair) Public() [32]byte {
	return *((*[32]byte)(k.private[32:]))
}

func (k *KeyPair) Seed() [32]byte {
	return *((*[32]byte)(k.private[:32]))
}

func (k *KeyPair) FeedRef() refs.FeedRef {
	pub := k.Public()
	ref, _ := refs.NewFeedRef(pub[:], refs.RefAlgoFeedSSB1)
	return *ref
}

type ID struct {
	ref refs.FeedRef
}

func (k *KeyPair) ID() ID {
	return ID{ref: k.FeedRef()}
}

func (id ID) Ref() string {
	return id.ref.String()
}

func (k *KeyPair) Sign(msg []byte) ([]byte, error) {
	if len(msg) == 0 {
		return nil, ErrSignFailed
	}
	sig := ed25519.Sign(ed25519.PrivateKey(k.private[:]), msg)
	return sig, nil
}

func (k *KeyPair) Verify(msg, sig []byte) bool {
	if len(sig) != ed25519.SignatureSize {
		return false
	}
	pub := k.Public()
	return ed25519.Verify(pub[:], msg, sig)
}

func Generate() (*KeyPair, error) {
	var seed [32]byte
	_, err := io.ReadFull(rand.Reader, seed[:])
	if err != nil {
		return nil, err
	}
	return FromSeed(seed), nil
}

func FromSeed(seed [32]byte) *KeyPair {
	private := ed25519.NewKeyFromSeed(seed[:])
	var kp KeyPair
	copy(kp.private[:], private)
	return &kp
}

func FromSeedString(seedStr string) (*KeyPair, error) {
	data, err := base64.StdEncoding.DecodeString(seedStr)
	if err != nil {
		return nil, ErrInvalidSeed
	}
	if len(data) != 32 {
		return nil, ErrInvalidSeed
	}
	var seed [32]byte
	copy(seed[:], data)
	return FromSeed(seed), nil
}

func SignWithHMAC(kp *KeyPair, msg, hmacKey []byte) ([]byte, error) {
	h := hmac.New(sha256.New, hmacKey)
	h.Write(msg)
	hashed := h.Sum(nil)

	sig, err := kp.Sign(hashed)
	if err != nil {
		return nil, err
	}
	return sig, nil
}

func VerifyWithHMAC(pubKey []byte, msg, sig, hmacKey []byte) bool {
	h := hmac.New(sha256.New, hmacKey)
	h.Write(msg)
	hashed := h.Sum(nil)

	return ed25519.Verify(pubKey, hashed, sig)
}

func Curve25519Public(ed25519Pub [32]byte) [32]byte {
	return secretstream.Ed25519PublicToCurve25519(ed25519Pub[:])
}

func Curve25519Private(ed25519Priv [64]byte) [32]byte {
	var curve25519Priv [32]byte
	subtle.ConstantTimeCopy(1, curve25519Priv[:], ed25519Priv[32:])
	return curve25519Priv
}

func (k *KeyPair) ToCurve25519() (pub [32]byte, priv [32]byte) {
	pub = Curve25519Public(k.Public())
	priv = Curve25519Private(k.private)
	return
}

func SecureCompare(a, b []byte) bool {
	return subtle.ConstantTimeCompare(a, b) == 1
}
