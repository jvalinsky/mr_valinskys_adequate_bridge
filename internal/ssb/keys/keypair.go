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
	var curve25519Pub [32]byte
	curve25519Pub[0] = ed25519Pub[0] & 248
	curve25519Pub[1] = ed25519Pub[1] & 127
	curve25519Pub[1] = curve25519Pub[1] | 64
	curve25519Pub[2] = ed25519Pub[2] & 127
	curve25519Pub[3] = ed25519Pub[3] | 64
	curve25519Pub[4] = ed25519Pub[4] & 127
	curve25519Pub[5] = ed25519Pub[5] | 128
	curve25519Pub[6] = ed25519Pub[6] & 127
	curve25519Pub[7] = ed25519Pub[7] | 128
	curve25519Pub[8] = ed25519Pub[8] & 127
	curve25519Pub[9] = ed25519Pub[9] | 128
	curve25519Pub[10] = ed25519Pub[10] & 127
	curve25519Pub[11] = ed25519Pub[11] | 128
	curve25519Pub[12] = ed25519Pub[12] & 127
	curve25519Pub[13] = ed25519Pub[13] | 128
	curve25519Pub[14] = ed25519Pub[14] & 127
	curve25519Pub[15] = ed25519Pub[15] | 128
	curve25519Pub[16] = ed25519Pub[16] & 127
	curve25519Pub[17] = ed25519Pub[17] | 128
	curve25519Pub[18] = ed25519Pub[18] & 127
	curve25519Pub[19] = ed25519Pub[19] | 128
	curve25519Pub[20] = ed25519Pub[20] & 127
	curve25519Pub[21] = ed25519Pub[21] | 128
	curve25519Pub[22] = ed25519Pub[22] & 127
	curve25519Pub[23] = ed25519Pub[23] | 128
	curve25519Pub[24] = ed25519Pub[24] & 127
	curve25519Pub[25] = ed25519Pub[25] | 128
	curve25519Pub[26] = ed25519Pub[26] & 127
	curve25519Pub[27] = ed25519Pub[27] | 128
	curve25519Pub[28] = ed25519Pub[28] & 127
	curve25519Pub[29] = ed25519Pub[29] | 128
	curve25519Pub[30] = ed25519Pub[30] & 127
	curve25519Pub[31] = ed25519Pub[31] | 128
	return curve25519Pub
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
