package muxrpc

import (
	"context"
	"io"
	"sync"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/muxrpc/codec"
)

type Packer struct {
	rl sync.Mutex
	wl sync.Mutex

	r *codec.Reader
	w *codec.Writer
	c io.Closer

	cl        sync.Mutex
	closeErr  error
	closeOnce sync.Once
	closing   chan struct{}
}

func NewPacker(rwc io.ReadWriteCloser) *Packer {
	return &Packer{
		r:       codec.NewReader(rwc),
		w:       codec.NewWriter(rwc),
		c:       rwc,
		closing: make(chan struct{}),
	}
}

func (pkr *Packer) NextHeader(ctx context.Context, hdr *codec.Header) error {
	pkr.rl.Lock()
	defer pkr.rl.Unlock()

	err := pkr.r.ReadHeader(hdr)
	if err != nil {
		return err
	}

	hdr.Req = -hdr.Req

	return nil
}

func (pkr *Packer) NextPacket(ctx context.Context) (*codec.Packet, error) {
	pkr.rl.Lock()
	defer pkr.rl.Unlock()

	return pkr.r.ReadPacket()
}

func (pkr *Packer) WritePacket(p codec.Packet) error {
	pkr.wl.Lock()
	defer pkr.wl.Unlock()

	return pkr.w.WritePacket(p)
}

func (pkr *Packer) NewPacketStream(req int32, flag codec.Flag) *PacketStream {
	return &PacketStream{
		packer: pkr,
		req:    req,
		flag:   flag,
	}
}

func (pkr *Packer) Close() error {
	pkr.cl.Lock()
	defer pkr.cl.Unlock()

	select {
	case <-pkr.closing:
		return pkr.closeErr
	default:
	}

	var err error
	pkr.closeOnce.Do(func() {
		err = pkr.c.Close()
		close(pkr.closing)
	})
	pkr.closeErr = err
	return err
}
