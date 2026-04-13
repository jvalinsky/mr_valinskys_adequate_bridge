package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"log"
	"strings"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/room"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/muxrpc"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/secretstream"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssbruntime"
	"golang.org/x/crypto/ed25519"
)

type roomAwareReverseBlobFetcher struct {
	runtime *ssbruntime.Runtime
	room    *room.Runtime
	logger  *log.Logger
}

func newRoomAwareReverseBlobFetcher(runtime *ssbruntime.Runtime, roomRuntime *room.Runtime, logger *log.Logger) *roomAwareReverseBlobFetcher {
	if runtime == nil {
		return nil
	}
	return &roomAwareReverseBlobFetcher{
		runtime: runtime,
		room:    roomRuntime,
		logger:  logger,
	}
}

func (f *roomAwareReverseBlobFetcher) EnsureBlob(ctx context.Context, sourceFeedID string, ref *refs.BlobRef) error {
	if f == nil || f.runtime == nil {
		return fmt.Errorf("reverse blob fetcher unavailable")
	}
	directErr := f.runtime.EnsureBlob(ctx, sourceFeedID, ref)
	if directErr == nil {
		f.logf("event=reverse_blob_fetch mode=direct source_feed=%s blob_ref=%s result=success", sourceFeedID, ref.Ref())
		return nil
	}
	f.logf("event=reverse_blob_fetch mode=direct source_feed=%s blob_ref=%s result=failed err=%v", sourceFeedID, ref.Ref(), directErr)
	if f.room == nil {
		return directErr
	}

	sourceFeedID = strings.TrimSpace(sourceFeedID)
	if sourceFeedID == "" {
		return directErr
	}
	targetFeed, err := refs.ParseFeedRef(sourceFeedID)
	if err != nil {
		return fmt.Errorf("%v; parse reverse blob source feed %s: %w", directErr, sourceFeedID, err)
	}
	if err := f.fetchViaRoomTunnel(ctx, *targetFeed, ref); err != nil {
		f.logf("event=reverse_blob_fetch mode=room_tunnel source_feed=%s blob_ref=%s result=failed err=%v", sourceFeedID, ref.Ref(), err)
		return fmt.Errorf("%v; room tunnel blob fetch: %w", directErr, err)
	}
	f.logf("event=reverse_blob_fetch mode=room_tunnel source_feed=%s blob_ref=%s result=success", sourceFeedID, ref.Ref())
	return nil
}

func (f *roomAwareReverseBlobFetcher) fetchViaRoomTunnel(ctx context.Context, target refs.FeedRef, ref *refs.BlobRef) error {
	if f == nil || f.runtime == nil || f.room == nil {
		return fmt.Errorf("room-aware reverse blob fetcher unavailable")
	}

	fetchCtx := ctx
	cancel := func() {}
	if _, hasDeadline := fetchCtx.Deadline(); !hasDeadline {
		fetchCtx, cancel = context.WithTimeout(fetchCtx, 15*time.Second)
	}
	defer cancel()

	roomPeer, err := f.runtime.Node().Connect(fetchCtx, f.room.Addr(), f.room.RoomFeed().PubKey())
	if err != nil {
		return fmt.Errorf("connect room for blob fetch: %w", err)
	}
	defer roomPeer.Conn.Close()

	roomRPC := roomPeer.RPC()
	if roomRPC == nil {
		return fmt.Errorf("room rpc unavailable for blob fetch")
	}

	tunnelSource, tunnelSink, err := roomRPC.Duplex(fetchCtx, muxrpc.TypeBinary, muxrpc.Method{"tunnel", "connect"}, map[string]any{
		"portal": f.room.RoomFeed(),
		"target": target,
	})
	if err != nil {
		return fmt.Errorf("open room tunnel for %s: %w", target.String(), err)
	}

	streamCtx, streamCancel := context.WithCancel(fetchCtx)
	defer streamCancel()

	streamConn := muxrpc.NewByteStreamConn(streamCtx, tunnelSource, tunnelSink, roomPeer.Conn.RemoteAddr())
	shsClient, err := secretstream.NewClient(streamConn, f.runtime.Node().AppKey(), f.runtime.Node().KeyPair.Private(), ed25519.PublicKey(target.PubKey()))
	if err != nil {
		_ = streamConn.Close()
		return fmt.Errorf("open room tunnel for %s: inner SHS init: %w", target.String(), err)
	}
	if err := shsClient.Handshake(); err != nil {
		_ = streamConn.Close()
		return fmt.Errorf("open room tunnel for %s: inner SHS handshake: %w", target.String(), err)
	}

	endpoint := muxrpc.NewServer(streamCtx, shsClient, nil, nil)
	defer endpoint.Terminate()
	defer func() {
		_ = shsClient.Close()
		tunnelSource.Cancel(nil)
		_ = tunnelSink.Close()
	}()

	src, err := endpoint.Source(streamCtx, muxrpc.TypeBinary, muxrpc.Method{"blobs", "get"}, map[string]string{
		"hash": ref.Ref(),
	})
	if err != nil {
		return fmt.Errorf("fetch blob %s via room tunnel: %w", ref.Ref(), err)
	}
	return f.storeBlobFromSource(streamCtx, ref, src)
}

func (f *roomAwareReverseBlobFetcher) storeBlobFromSource(ctx context.Context, ref *refs.BlobRef, src *muxrpc.ByteSource) error {
	if f == nil || f.runtime == nil {
		return fmt.Errorf("reverse blob fetcher unavailable")
	}
	if ref == nil {
		return fmt.Errorf("blob ref is nil")
	}
	if src == nil {
		return fmt.Errorf("blob source is nil")
	}

	var payload bytes.Buffer
	for src.Next(ctx) {
		chunk, err := src.Bytes()
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		if len(chunk) > 0 {
			if _, err := payload.Write(chunk); err != nil {
				return err
			}
		}
	}
	if err := src.Err(); err != nil {
		return err
	}
	if payload.Len() == 0 {
		return fmt.Errorf("empty blob response")
	}

	sum := sha256.Sum256(payload.Bytes())
	if !bytes.Equal(sum[:], ref.Hash()) {
		return fmt.Errorf("blob hash mismatch")
	}
	storedHash, err := f.runtime.BlobStore().Put(bytes.NewReader(payload.Bytes()))
	if err != nil {
		return err
	}
	if !bytes.Equal(storedHash, ref.Hash()) {
		return fmt.Errorf("stored blob hash mismatch")
	}
	return nil
}

func (f *roomAwareReverseBlobFetcher) logf(format string, args ...any) {
	if f == nil || f.logger == nil {
		return
	}
	f.logger.Printf(format, args...)
}
