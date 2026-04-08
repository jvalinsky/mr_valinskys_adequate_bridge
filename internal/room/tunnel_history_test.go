package room

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/feedlog"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/keys"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/muxrpc"
	legacyhandlers "github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/muxrpc/handlers"
	roomhandlers "github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/muxrpc/handlers/room"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/roomdb"
)

func TestRuntimeTunnelConnectSupportsCreateHistoryStream(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	rt := startTestRuntime(t, "open", nil)

	memberKey, err := keys.Generate()
	if err != nil {
		t.Fatalf("generate member key: %v", err)
	}
	if _, err := rt.roomDB.Members().Add(ctx, memberKey.FeedRef(), roomdb.RoleMember); err != nil {
		t.Fatalf("add member: %v", err)
	}

	store, err := feedlog.NewStore(feedlog.Config{
		DBPath:     filepath.Join(t.TempDir(), "history.sqlite"),
		RepoPath:   t.TempDir(),
		BlobSubdir: "blobs",
	})
	if err != nil {
		t.Fatalf("new feedlog store: %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})

	log, err := store.Logs().Create(memberKey.FeedRef().String())
	if err != nil {
		t.Fatalf("create feed log: %v", err)
	}

	msgRef := refs.MustNewMessageRef(bytes.Repeat([]byte{0x42}, 32), refs.RefAlgoMessageSSB1)
	if _, err := log.Append([]byte(`{"type":"post","text":"hello from tunnel"}`), &feedlog.Metadata{
		Author:    memberKey.FeedRef().String(),
		Sequence:  1,
		Timestamp: 12345,
		Hash:      msgRef.String(),
		Sig:       []byte("signature-1"),
	}); err != nil {
		t.Fatalf("append feed log message: %v", err)
	}

	tunnelCalled := make(chan struct{}, 1)
	historyCalled := make(chan struct{}, 1)

	handlerMux := &muxrpc.HandlerMux{}
	handlerMux.Register(muxrpc.Method{"whoami"}, legacyhandlers.NewWhoamiHandler(memberKey))
	handlerMux.Register(muxrpc.Method{"gossip", "ping"}, legacyhandlers.NewPingHandler())
	handlerMux.Register(muxrpc.Method{"createHistoryStream"}, &recordingHandler{
		method:   muxrpc.Method{"createHistoryStream"},
		inner:    legacyhandlers.NewHistoryStreamHandler(store),
		calledCh: historyCalled,
	})
	handlerMux.Register(muxrpc.Method{"tunnel", "connect"}, &recordingHandler{
		method:   muxrpc.Method{"tunnel", "connect"},
		inner:    roomhandlers.NewClientTunnelConnectHandler(memberKey.FeedRef(), handlerMux),
		calledCh: tunnelCalled,
	})

	memberClient := connectRuntimeRoomClient(t, ctx, rt, memberKey, handlerMux)
	var meta struct {
		Membership bool `json:"membership"`
	}
	if err := memberClient.endpoint.Async(ctx, &meta, muxrpc.TypeJSON, muxrpc.Method{"room", "metadata"}); err != nil {
		t.Fatalf("room.metadata: %v", err)
	}
	if !meta.Membership {
		t.Fatalf("expected room member metadata, got %+v", meta)
	}

	probeKey, err := keys.Generate()
	if err != nil {
		t.Fatalf("generate probe key: %v", err)
	}
	probeClient := connectRuntimeRoomClient(t, ctx, rt, probeKey, nil)

	tunnelSource, tunnelSink, err := probeClient.endpoint.Duplex(ctx, muxrpc.TypeBinary, muxrpc.Method{"tunnel", "connect"}, map[string]interface{}{
		"portal": rt.RoomFeed(),
		"target": memberKey.FeedRef(),
	})
	if err != nil {
		t.Fatalf("tunnel.connect: %v", err)
	}

	streamCtx, streamCancel := context.WithCancel(ctx)
	defer streamCancel()
	streamConn := muxrpc.NewByteStreamConn(streamCtx, tunnelSource, tunnelSink, probeClient.conn.RemoteAddr())
	endpoint := muxrpc.NewServer(streamCtx, streamConn, nil, nil)
	defer endpoint.Terminate()
	defer func() {
		_ = streamConn.Close()
		tunnelSource.Cancel(nil)
		_ = tunnelSink.Close()
	}()

	src, err := endpoint.Source(ctx, muxrpc.TypeJSON, muxrpc.Method{"createHistoryStream"}, map[string]interface{}{
		"id":    memberKey.FeedRef().String(),
		"limit": 1,
	})
	if err != nil {
		t.Fatalf("createHistoryStream over tunnel: %v", err)
	}

	select {
	case <-tunnelCalled:
	case <-time.After(2 * time.Second):
		t.Fatal("tunnel.connect request never reached target peer handler")
	}

	select {
	case <-historyCalled:
	case <-time.After(2 * time.Second):
		t.Fatal("createHistoryStream request never reached target peer inner handler")
	}

	var frame map[string]interface{}
	readSourceJSON(t, ctx, src, &frame)
	if got := frame["key"]; got != msgRef.String() {
		t.Fatalf("expected tunneled history key %q, got %#v", msgRef.String(), got)
	}
}

type recordingHandler struct {
	method   muxrpc.Method
	inner    muxrpc.Handler
	calledCh chan struct{}
}

func (h *recordingHandler) Handled(m muxrpc.Method) bool {
	return h.inner != nil && h.inner.Handled(m)
}

func (h *recordingHandler) HandleCall(ctx context.Context, req *muxrpc.Request) {
	select {
	case h.calledCh <- struct{}{}:
	default:
	}
	if h.inner == nil {
		req.CloseWithError(fmt.Errorf("%s inner handler missing", h.method.String()))
		return
	}
	h.inner.HandleCall(ctx, req)
}

func (h *recordingHandler) HandleConnect(ctx context.Context, edp muxrpc.Endpoint) {
	if h.inner != nil {
		h.inner.HandleConnect(ctx, edp)
	}
}
