package codec

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
)

const HeaderSize = 9

const maxBufferSize = 1 << 18

type Body []byte

func (b Body) String() string {
	return string(b)
}

type Packet struct {
	Flag Flag
	Req  int32
	Body Body
}

type Flag byte

func (f Flag) Set(g Flag) Flag {
	return f | g
}

func (f Flag) Clear(g Flag) Flag {
	return f &^ g
}

func (f Flag) Get(g Flag) bool {
	return f&g == g
}

type Header struct {
	Flag Flag
	Len  uint32
	Req  int32
}

const (
	FlagString Flag = 1 << iota
	FlagJSON
	FlagEndErr
	FlagStream
)

type Reader struct{ r io.Reader }

func NewReader(r io.Reader) *Reader {
	return &Reader{r: r}
}

func (r Reader) ReadPacket() (*Packet, error) {
	var hdr Header
	err := r.ReadHeader(&hdr)
	if err != nil {
		return nil, err
	}

	p := &Packet{
		Flag: hdr.Flag,
		Req:  hdr.Req,
		Body: make([]byte, hdr.Len),
	}

	_, err = io.ReadFull(r.r, p.Body)
	if err != nil {
		if errors.Is(err, os.ErrClosed) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrClosedPipe) {
			return nil, err
		}
		return nil, fmt.Errorf("pkt-codec: read body failed: %w", err)
	}

	return p, nil
}

func (r Reader) ReadHeader(hdr *Header) error {
	var buf [HeaderSize]byte
	_, err := io.ReadFull(r.r, buf[:])
	if err != nil {
		if errors.Is(err, os.ErrClosed) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrClosedPipe) {
			return io.EOF
		}
		return fmt.Errorf("pkt-codec: header read failed: %w", err)
	}

	hdr.Flag = Flag(buf[0])
	hdr.Len = binary.BigEndian.Uint32(buf[1:5])
	hdr.Req = int32(binary.BigEndian.Uint32(buf[5:9]))

	if hdr.Flag == 0 && hdr.Len == 0 && hdr.Req == 0 {
		return io.EOF
	}
	return nil
}

type Writer struct {
	mu sync.Mutex
	w  io.Writer
}

func NewWriter(w io.Writer) *Writer {
	return &Writer{w: w}
}

func (w *Writer) WritePacket(p Packet) error {
	bodyLen := len(p.Body)
	if bodyLen > maxBufferSize {
		return fmt.Errorf("pkt-codec: body too large (%d)", bodyLen)
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	var hdr [HeaderSize]byte
	hdr[0] = byte(p.Flag)
	binary.BigEndian.PutUint32(hdr[1:5], uint32(bodyLen))
	binary.BigEndian.PutUint32(hdr[5:9], uint32(p.Req))

	if _, err := w.w.Write(hdr[:]); err != nil {
		return fmt.Errorf("pkt-codec: header write failed: %w", err)
	}

	if _, err := w.w.Write(p.Body); err != nil {
		return fmt.Errorf("pkt-codec: body write failed: %w", err)
	}

	return nil
}

func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	_, err := w.w.Write([]byte{0, 0, 0, 0, 0, 0, 0, 0, 0})
	if err != nil {
		return fmt.Errorf("pkt-codec: failed to write Close() packet: %w", err)
	}

	if c, ok := w.w.(io.Closer); ok {
		if err := c.Close(); err != nil {
			return fmt.Errorf("pkt-codec: failed to close underlying writer: %w", err)
		}
	}

	return nil
}
