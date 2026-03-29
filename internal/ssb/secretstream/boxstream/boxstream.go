package boxstream

import (
	"bytes"
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

type BoxerEngine struct {
	secret [32]byte
	nonce  [24]byte
}

func NewBoxerEngine(nonce *[24]byte, secret *[32]byte) *BoxerEngine {
	return &BoxerEngine{
		secret: *secret,
		nonce:  *nonce,
	}
}

func (b *BoxerEngine) EncryptMessage(msg []byte) []byte {
	if len(msg) > MaxSegmentSize {
		panic("message exceeds maximum segment size")
	}

	headerNonce := b.nonce
	incrementNonce(&b.nonce)
	bodyNonce := b.nonce
	incrementNonce(&b.nonce)

	bodyBox := secretbox.Seal(nil, msg, &bodyNonce, &b.secret)
	bodyMAC, body := bodyBox[:secretbox.Overhead], bodyBox[secretbox.Overhead:]

	header := make([]byte, 2+secretbox.Overhead)
	binary.BigEndian.PutUint16(header[:2], uint16(len(msg)))
	copy(header[2:], bodyMAC)
	headerBox := secretbox.Seal(nil, header, &headerNonce, &b.secret)

	out := make([]byte, 0, len(headerBox)+len(body))
	out = append(out, headerBox...)
	out = append(out, body...)
	return out
}

func (b *BoxerEngine) EncryptGoodbye() []byte {
	return secretbox.Seal(nil, goodbye[:], &b.nonce, &b.secret)
}

type Boxer struct {
	l      sync.Mutex
	w      io.Writer
	engine *BoxerEngine
}

func NewBoxer(w io.Writer, nonce *[24]byte, secret *[32]byte) *Boxer {
	return &Boxer{
		w:      w,
		engine: NewBoxerEngine(nonce, secret),
	}
}

func (b *Boxer) Write(p []byte) (int, error) {
	if len(p) > MaxSegmentSize {
		written := 0
		for written < len(p) {
			chunkSize := len(p) - written
			if chunkSize > MaxSegmentSize {
				chunkSize = MaxSegmentSize
			}
			if err := b.WriteMessage(p[written : written+chunkSize]); err != nil {
				return written, err
			}
			written += chunkSize
		}
		return written, nil
	}

	if err := b.WriteMessage(p); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (b *Boxer) WriteMessage(msg []byte) error {
	b.l.Lock()
	defer b.l.Unlock()

	out := b.engine.EncryptMessage(msg)
	_, err := b.w.Write(out)
	return err
}

func (b *Boxer) WriteGoodbye() error {
	b.l.Lock()
	defer b.l.Unlock()
	_, err := b.w.Write(b.engine.EncryptGoodbye())
	return err
}

type UnboxerEngine struct {
	secret [32]byte
	nonce  [24]byte
	buffer []byte
}

func NewUnboxerEngine(nonce *[24]byte, secret *[32]byte) *UnboxerEngine {
	return &UnboxerEngine{
		secret: *secret,
		nonce:  *nonce,
	}
}

func (u *UnboxerEngine) AddRawData(p []byte) {
	if len(p) > 0 {
		u.buffer = append(u.buffer, p...)
	}
}

func (u *UnboxerEngine) NextMessage() ([]byte, error) {
	if len(u.buffer) < HeaderLength {
		return nil, nil // Need more data
	}

	headerBox := u.buffer[:HeaderLength]
	header, ok := secretbox.Open(nil, headerBox, &u.nonce, &u.secret)
	if !ok {
		return nil, errors.New("invalid header")
	}

	if bytes.Equal(header, goodbye[:]) {
		u.buffer = u.buffer[HeaderLength:]
		incrementNonce(&u.nonce)
		return header, nil
	}
	incrementNonce(&u.nonce)

	bodyLen := int(binary.BigEndian.Uint16(header[:2]))
	if bodyLen > MaxSegmentSize {
		return nil, errors.New("message too large")
	}

	bodyMAC := header[2:18]
	totalBodyLength := bodyLen

	if len(u.buffer) < HeaderLength+totalBodyLength {
		return nil, nil
	}

	// Actually, we have enough data.
	bodyBox := make([]byte, 16+bodyLen)
	copy(bodyBox[:16], bodyMAC)
	copy(bodyBox[16:], u.buffer[HeaderLength:HeaderLength+bodyLen])

	body, ok := secretbox.Open(nil, bodyBox, &u.nonce, &u.secret)
	if !ok {
		return nil, errors.New("decryption failed")
	}
	incrementNonce(&u.nonce)

	// Consume from buffer
	u.buffer = u.buffer[HeaderLength+bodyLen:]

	return body, nil
}

type Unboxer struct {
	engine   *UnboxerEngine
	r        io.Reader
	leftover []byte
}

func NewUnboxer(r io.Reader, nonce *[24]byte, secret *[32]byte) *Unboxer {
	return &Unboxer{
		r:      r,
		engine: NewUnboxerEngine(nonce, secret),
	}
}

func (u *Unboxer) Read(p []byte) (int, error) {
	if len(u.leftover) > 0 {
		n := copy(p, u.leftover)
		u.leftover = u.leftover[n:]
		return n, nil
	}

	msg, err := u.ReadMessage()
	if err != nil {
		return 0, err
	}

	n := copy(p, msg)
	if n < len(msg) {
		u.leftover = msg[n:]
	}
	return n, nil
}

func (u *Unboxer) ReadMessage() ([]byte, error) {
	// Traditional ReadMessage will read exactly what's needed from io.Reader
	// and feed it to the engine.

	// First, check if we already have a message in buffer
	if msg, err := u.engine.NextMessage(); msg != nil || err != nil {
		return msg, err
	}

	// If not, read from reader until we have a message
	for {
		buf := make([]byte, MaxSegmentSize)
		n, err := u.r.Read(buf)
		if n > 0 {
			u.engine.AddRawData(buf[:n])
			msg, err := u.engine.NextMessage()
			if err != nil {
				return nil, err
			}
			if msg != nil {
				return msg, nil
			}
		}
		if err != nil {
			return nil, err
		}
	}
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
