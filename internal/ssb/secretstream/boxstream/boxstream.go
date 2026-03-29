package boxstream

import (
	"encoding/binary"
	"errors"
	"io"
	"sync"

	"golang.org/x/crypto/nacl/secretbox"
)

const (
	HeaderLength   = 2 + 16 + 16
	MaxSegmentSize = 4 * 1024
)

var goodbye = [18]byte{'g', 'o', 'o', 'd', 'b', 'y', 'e', '!', 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}

type Boxer struct {
	l      sync.Mutex
	w      io.Writer
	secret *[32]byte
	nonce  *[24]byte
}

func NewBoxer(w io.Writer, nonce *[24]byte, secret *[32]byte) *Boxer {
	return &Boxer{
		w:      w,
		secret: secret,
		nonce:  nonce,
	}
}

func (b *Boxer) WriteMessage(msg []byte) error {
	if len(msg) > MaxSegmentSize {
		panic("message exceeds maximum segment size")
	}
	b.l.Lock()
	defer b.l.Unlock()

	headerNonce := *b.nonce
	incrementNonce(b.nonce)
	bodyNonce := *b.nonce
	incrementNonce(b.nonce)

	bodyBox := secretbox.Seal(nil, msg, &bodyNonce, b.secret)
	bodyMAC, body := bodyBox[:secretbox.Overhead], bodyBox[secretbox.Overhead:]

	header := make([]byte, 2+secretbox.Overhead)
	binary.BigEndian.PutUint16(header[:2], uint16(len(msg)))
	copy(header[2:], bodyMAC)
	headerBox := secretbox.Seal(nil, header, &headerNonce, b.secret)

	if _, err := b.w.Write(headerBox); err != nil {
		return err
	}
	_, err := b.w.Write(body)
	return err
}

func (b *Boxer) WriteGoodbye() error {
	b.l.Lock()
	defer b.l.Unlock()
	_, err := b.w.Write(secretbox.Seal(nil, goodbye[:], b.nonce, b.secret))
	return err
}

type Unboxer struct {
	r      io.Reader
	secret *[32]byte
	nonce  *[24]byte
}

func NewUnboxer(r io.Reader, nonce *[24]byte, secret *[32]byte) *Unboxer {
	return &Unboxer{
		r:      r,
		secret: secret,
		nonce:  nonce,
	}
}

func (u *Unboxer) ReadMessage() ([]byte, error) {
	headerBox := make([]byte, HeaderLength)
	if _, err := io.ReadFull(u.r, headerBox); err != nil {
		return nil, err
	}

	headerNonce := *u.nonce
	incrementNonce(u.nonce)

	header, ok := secretbox.Open(nil, headerBox, &headerNonce, u.secret)
	if !ok {
		return nil, errors.New("invalid header")
	}

	bodyLen := int(binary.BigEndian.Uint16(header[:2]))
	if bodyLen > MaxSegmentSize {
		return nil, errors.New("message too large")
	}

	bodyMAC := header[2:18]
	padding := make([]byte, bodyLen)
	bodyBox := append(bodyMAC, padding...)

	if _, err := io.ReadFull(u.r, bodyBox); err != nil {
		return nil, err
	}

	bodyNonce := *u.nonce
	incrementNonce(u.nonce)

	body, ok := secretbox.Open(nil, bodyBox, &bodyNonce, u.secret)
	if !ok {
		return nil, errors.New("decryption failed")
	}

	return body, nil
}

func incrementNonce(b *[24]byte) {
	var i int
	for i = len(b) - 1; i >= 0; i-- {
		if b[i] != 0xff {
			b[i]++
			return
		}
		b[i] = 0
	}
}
