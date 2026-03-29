package bfe

import (
	"encoding/base64"
	"errors"
)

var (
	ErrInvalidBFE    = errors.New("bfe: invalid BFE encoding")
	ErrInvalidFormat = errors.New("bfe: unknown format")
)

const (
	TypeFeed      = 0x00
	TypeMessage   = 0x01
	TypeBlob      = 0x02
	TypeEncKey    = 0x03
	TypeSignature = 0x04
	TypeEncrypted = 0x05
	TypeGeneric   = 0x06
	TypeIdentity  = 0x07
)

var FeedFormatCodes = map[string]uint8{
	"ed25519":       0x00,
	"gabbygrove-v1": 0x01,
	"bamboo":        0x02,
	"bendybutt-v1":  0x03,
	"buttwoo-v1":    0x04,
	"indexed-v1":    0x05,
}

var FeedFormatCodesReverse = map[uint8]string{
	0x00: "ed25519",
	0x01: "gabbygrove-v1",
	0x02: "bamboo",
	0x03: "bendybutt-v1",
	0x04: "buttwoo-v1",
	0x05: "indexed-v1",
}

var MessageFormatCodes = map[string]uint8{
	"sha256":        0x00,
	"gabbygrove-v1": 0x01,
	"cloaked":       0x02,
	"bamboo":        0x03,
	"bendybutt-v1":  0x04,
	"buttwoo-v1":    0x05,
	"indexed-v1":    0x06,
}

var MessageFormatCodesReverse = map[uint8]string{
	0x00: "sha256",
	0x01: "gabbygrove-v1",
	0x02: "cloaked",
	0x03: "bamboo",
	0x04: "bendybutt-v1",
	0x05: "buttwoo-v1",
	0x06: "indexed-v1",
}

var GenericFormatCodes = map[string]uint8{
	"string-UTF8": 0x00,
	"boolean":     0x01,
	"nil":         0x02,
	"any-bytes":   0x03,
}

var GenericFormatCodesReverse = map[uint8]string{
	0x00: "string-UTF8",
	0x01: "boolean",
	0x02: "nil",
	0x03: "any-bytes",
}

func EncodeFeed(algo string, pubKey []byte) []byte {
	formatCode := FeedFormatCodes[algo]
	if formatCode == 0 && algo != "ed25519" {
		formatCode = 0x00
	}

	result := make([]byte, 0, 34)
	result = append(result, TypeFeed, formatCode)
	result = append(result, pubKey...)
	return result
}

func DecodeFeed(b []byte) (algo string, pubKey []byte, err error) {
	if len(b) < 34 {
		return "", nil, ErrInvalidBFE
	}
	if b[0] != TypeFeed {
		return "", nil, ErrInvalidBFE
	}

	formatCode := b[1]
	algo, ok := FeedFormatCodesReverse[formatCode]
	if !ok {
		return "", nil, ErrInvalidFormat
	}

	return algo, b[2:34], nil
}

func EncodeMessage(algo string, hash []byte) []byte {
	formatCode := MessageFormatCodes[algo]
	if formatCode == 0 && algo != "sha256" {
		formatCode = 0x00
	}

	result := make([]byte, 0, 34)
	result = append(result, TypeMessage, formatCode)
	result = append(result, hash...)
	return result
}

func DecodeMessage(b []byte) (algo string, hash []byte, err error) {
	if len(b) < 34 {
		return "", nil, ErrInvalidBFE
	}
	if b[0] != TypeMessage {
		return "", nil, ErrInvalidBFE
	}

	formatCode := b[1]
	algo, ok := MessageFormatCodesReverse[formatCode]
	if !ok {
		return "", nil, ErrInvalidFormat
	}

	return algo, b[2:34], nil
}

func EncodeBlob(hash []byte) []byte {
	result := make([]byte, 0, 34)
	result = append(result, TypeBlob, 0x00)
	result = append(result, hash...)
	return result
}

func DecodeBlob(b []byte) ([]byte, error) {
	if len(b) < 34 {
		return nil, ErrInvalidBFE
	}
	if b[0] != TypeBlob {
		return nil, ErrInvalidBFE
	}
	if b[1] != 0x00 {
		return nil, ErrInvalidFormat
	}
	return b[2:34], nil
}

func EncodeSignature(sig []byte) []byte {
	result := make([]byte, 0, 2+len(sig))
	result = append(result, TypeSignature, 0x00)
	result = append(result, sig...)
	return result
}

func DecodeSignature(b []byte) ([]byte, error) {
	if len(b) < 66 {
		return nil, ErrInvalidBFE
	}
	if b[0] != TypeSignature {
		return nil, ErrInvalidBFE
	}
	if b[1] != 0x00 {
		return nil, ErrInvalidFormat
	}
	return b[2:66], nil
}

func EncodeNil() []byte {
	return []byte{TypeGeneric, 0x02}
}

func EncodeString(s string) []byte {
	encoded := base64.StdEncoding.EncodeToString([]byte(s))
	result := make([]byte, 0, 2+len(encoded))
	result = append(result, TypeGeneric, GenericFormatCodes["string-UTF8"])
	result = append(result, encoded...)
	return result
}

func DecodeString(b []byte) (string, error) {
	if len(b) < 3 {
		return "", ErrInvalidBFE
	}
	if b[0] != TypeGeneric || b[1] != GenericFormatCodes["string-UTF8"] {
		return "", ErrInvalidFormat
	}
	decoded, err := base64.StdEncoding.DecodeString(string(b[2:]))
	if err != nil {
		return "", err
	}
	return string(decoded), nil
}
