package replication

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/feedlog"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
)

const EBTVersion = 3

type Note struct {
	Seq           int64
	Replicate     bool
	Receive       bool
	TangleRootSeq int64
}

type TangleFrontier map[string]int64

type NetworkFrontier map[string]Note

func (n Note) MarshalJSON() ([]byte, error) {
	if !n.Replicate || n.Seq == -1 {
		return []byte("-1"), nil
	}
	i := n.Seq
	i = i << 1
	if !n.Receive {
		i |= 1
	}
	return []byte(fmt.Sprintf("%d", i)), nil
}

func (nf *NetworkFrontier) UnmarshalJSON(data []byte) error {
	var dummy map[string]int64
	if err := json.Unmarshal(data, &dummy); err != nil {
		return err
	}

	result := make(NetworkFrontier)
	for fstr, i := range dummy {
		_, err := refs.ParseFeedRef(fstr)
		if err != nil {
			continue
		}

		n := Note{
			Replicate: i != -1,
			Receive:   i != -1 && !(i&1 == 1),
			Seq:       i >> 1,
		}
		result[fstr] = n
	}
	*nf = result
	return nil
}

type StateMatrix struct {
	basePath        string
	self            string
	mu              sync.Mutex
	frontiers       map[string]NetworkFrontier
	tangleFrontiers map[string]TangleFrontier
	store           feedlog.FeedStore
	updateCh        chan struct{}
}

func NewStateMatrix(basePath string, self *refs.FeedRef, store feedlog.FeedStore) (*StateMatrix, error) {
	sm := &StateMatrix{
		basePath:        basePath,
		frontiers:       make(map[string]NetworkFrontier),
		tangleFrontiers: make(map[string]TangleFrontier),
		store:           store,
		updateCh:        make(chan struct{}, 1),
	}
	if self != nil {
		sm.self = self.String()
	}
	return sm, nil
}

func (sm *StateMatrix) GetTangleFrontier(peer string) TangleFrontier {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	return sm.tangleFrontiers[peer]
}

func (sm *StateMatrix) SetTangleFrontier(peer string, frontier TangleFrontier) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.tangleFrontiers[peer] = frontier
}

func (sm *StateMatrix) notify() {
	select {
	case sm.updateCh <- struct{}{}:
	default:
	}
}

func (sm *StateMatrix) WaitForUpdate(ctx context.Context) <-chan struct{} {
	return sm.updateCh
}

func (sm *StateMatrix) initializeFeed(feed *refs.FeedRef, seq int64) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.self == "" {
		return
	}

	selfFrontier := sm.frontiers[sm.self]
	if selfFrontier == nil {
		selfFrontier = make(NetworkFrontier)
	}

	selfFrontier[feed.String()] = Note{
		Seq:       seq,
		Replicate: true,
		Receive:   true,
	}

	sm.frontiers[sm.self] = selfFrontier
	sm.notify()
	slog.Debug("ebt initialize feed", "feed", feed.String(), "seq", seq, "receive", true)
}

func (sm *StateMatrix) InitializeFromFeedlog() error {
	if sm.store == nil {
		return nil
	}

	feeds, err := sm.store.Logs().List()
	if err != nil {
		return fmt.Errorf("failed to list feeds: %w", err)
	}

	for _, feedID := range feeds {
		log, err := sm.store.Logs().Get(feedID)
		if err != nil {
			continue
		}

		seq, err := log.Seq()
		if err != nil {
			continue
		}

		feedRef, err := refs.ParseFeedRef(feedID)
		if err != nil {
			continue
		}

		sm.initializeFeed(feedRef, seq)
	}

	return nil
}

func (sm *StateMatrix) Inspect(peer *refs.FeedRef) (NetworkFrontier, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	return sm.loadFrontier(peer.String())
}

func (sm *StateMatrix) loadFrontier(peer string) (NetworkFrontier, error) {
	if frontier, ok := sm.frontiers[peer]; ok {
		return frontier, nil
	}
	return make(NetworkFrontier), nil
}

func (sm *StateMatrix) Update(who *refs.FeedRef, update NetworkFrontier) (NetworkFrontier, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	current := sm.frontiers[who.String()]
	if current == nil {
		current = make(NetworkFrontier)
	}

	for feed, note := range update {
		current[feed] = note
	}

	sm.frontiers[who.String()] = current
	return current, nil
}

func (sm *StateMatrix) SetFeedSeq(feed *refs.FeedRef, seq int64) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.self == "" {
		return
	}

	selfFrontier := sm.frontiers[sm.self]
	if selfFrontier == nil {
		selfFrontier = make(NetworkFrontier)
	}

	selfFrontier[feed.String()] = Note{
		Seq:       seq,
		Replicate: true,
		Receive:   true,
	}

	sm.frontiers[sm.self] = selfFrontier
	sm.notify()
	slog.Debug("ebt set feed seq", "feed", feed.String(), "seq", seq, "frontier_count", len(selfFrontier))
}

func (sm *StateMatrix) Changed(self, peer *refs.FeedRef) (NetworkFrontier, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	selfNf, err := sm.loadFrontier(self.String())
	if err != nil {
		return nil, err
	}

	if peer == nil {
		relevant := make(NetworkFrontier)
		for feed, note := range selfNf {
			if note.Replicate {
				relevant[feed] = note
			}
		}
		return relevant, nil
	}

	peerNf, err := sm.loadFrontier(peer.String())
	if err != nil {
		return nil, err
	}

	relevant := make(NetworkFrontier)
	for feed, myNote := range selfNf {
		theirNote, has := peerNf[feed]
		if !has {
			relevant[feed] = myNote
			continue
		}

		if myNote.Seq != theirNote.Seq || myNote.Receive != theirNote.Receive || myNote.Replicate != theirNote.Replicate {
			relevant[feed] = myNote
		}
	}
	return relevant, nil
}

func (sm *StateMatrix) Export() map[string]map[string]int64 {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	res := make(map[string]map[string]int64)
	for peer, nf := range sm.frontiers {
		res[peer] = make(map[string]int64)
		for feed, note := range nf {
			res[peer][feed] = note.Seq
		}
	}
	return res
}

func (sm *StateMatrix) Close() error {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.frontiers = nil
	return nil
}

type Sessions struct {
	mu      sync.Mutex
	open    map[string]*Session
	waiting map[string]chan<- struct{}
}

type Session struct {
	remote     string
	mu         sync.Mutex
	subscribed map[string]context.CancelFunc
}

func NewSessions() *Sessions {
	return &Sessions{
		open:    make(map[string]*Session),
		waiting: make(map[string]chan<- struct{}),
	}
}

func (s *Sessions) Started(addr string) *Session {
	s.mu.Lock()
	defer s.mu.Unlock()

	session := &Session{
		remote:     addr,
		subscribed: make(map[string]context.CancelFunc),
	}

	s.open[addr] = session

	if ch, ok := s.waiting[addr]; ok {
		close(ch)
		delete(s.waiting, addr)
	}

	return session
}

func (s *Sessions) Ended(addr string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if session, ok := s.open[addr]; ok {
		session.mu.Lock()
		for _, cancel := range session.subscribed {
			cancel()
		}
		session.mu.Unlock()
		delete(s.open, addr)
	}
}

func (s *Sessions) WaitFor(ctx context.Context, addr string, dur time.Duration) bool {
	s.mu.Lock()

	if _, has := s.open[addr]; has {
		s.mu.Unlock()
		return true
	}

	c := make(chan struct{})
	s.waiting[addr] = c
	s.mu.Unlock()

	select {
	case <-c:
		return true
	case <-ctx.Done():
		return false
	case <-time.After(dur):
		return false
	}
}

func (s *Session) Subscribed(feed string, cancel context.CancelFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if fn, ok := s.subscribed[feed]; ok {
		fn()
		delete(s.subscribed, feed)
	}
	s.subscribed[feed] = cancel
}

func (s *Session) Unsubscribe(feed string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if fn, ok := s.subscribed[feed]; ok {
		fn()
		delete(s.subscribed, feed)
	}
}

type ReplicationLister interface {
	ListFeeds() ([]refs.FeedRef, error)
}

type FeedManager interface {
	GetFeedSeq(author *refs.FeedRef) (int64, error)
	GetMessage(author *refs.FeedRef, seq int64) ([]byte, error)
	AppendSignedMessage(raw []byte) (*refs.FeedRef, int64, error)
}

var ErrNotFound = fmt.Errorf("message not found")

type ByteSourceReader interface {
	Next(ctx context.Context) bool
	Bytes() ([]byte, error)
	Err() error
}

type Writer interface {
	Write(ctx context.Context, data []byte) error
}

type EBTHandler struct {
	self        *refs.FeedRef
	stateMatrix *StateMatrix
	store       FeedManager
	sessions    *Sessions
	replicate   ReplicationLister
}

func NewEBTHandler(self *refs.FeedRef, store FeedManager, matrix *StateMatrix, repl ReplicationLister) *EBTHandler {
	return &EBTHandler{
		self:        self,
		stateMatrix: matrix,
		store:       store,
		sessions:    NewSessions(),
		replicate:   repl,
	}
}

func (h *EBTHandler) HandleDuplex(ctx context.Context, tx Writer, rx ByteSourceReader, remoteAddr string, remoteFeed *refs.FeedRef) error {
	slog.Debug("ebt handle duplex start", "remote", remoteAddr, "remote_feed", remoteFeed)

	session := h.sessions.Started(remoteAddr)
	defer h.sessions.Ended(remoteAddr)

	slog.Debug("ebt handle duplex sending initial state", "remote", remoteAddr)
	if err := h.sendState(ctx, tx, remoteAddr); err != nil {
		slog.Debug("ebt handle duplex send state failed", "err", err)
		return err
	}
	slog.Debug("ebt handle duplex initial state sent, waiting for peer frontier")

	// Launch background loop to monitor for local state changes and notify peer
	go func() {
		updateCh := h.stateMatrix.WaitForUpdate(ctx)
		for {
			select {
			case <-ctx.Done():
				return
			case <-updateCh:
				// Something changed locally, check if we need to send new notes to peer
				wants, err := h.stateMatrix.Changed(h.self, remoteFeed)
				if err == nil && len(wants) > 0 {
					data, err := json.Marshal(wants)
					if err == nil {
						_ = tx.Write(ctx, data)
					}
				}
			}
		}
	}()

	for {
		slog.Debug("ebt handle duplex calling rx.Next")
		ok := rx.Next(ctx)
		if !ok {
			err := rx.Err()
			slog.Debug("ebt handle duplex rx.Next returned false", "err", err)
			return err
		}

		data, err := rx.Bytes()
		if err != nil {
			slog.Debug("ebt handle duplex rx.Bytes error", "err", err)
			return err
		}
		slog.Debug("ebt handle duplex received bytes", "bytes", len(data), "data", string(data))

		var frontierUpdate NetworkFrontier
		if err := json.Unmarshal(data, &frontierUpdate); err != nil {
			author, seq, appendErr := h.store.AppendSignedMessage(data)
			if appendErr != nil {
				slog.Debug("ebt handle duplex failed to decode message", "err", err, "append_err", appendErr)
				continue
			}
			if author != nil {
				h.stateMatrix.SetFeedSeq(author, seq)
			}
			continue
		}

		slog.Debug("ebt handle duplex received frontier", "remote", remoteAddr, "update", fmt.Sprintf("%+v", frontierUpdate))

		// Store remote peer's frontier under their identity, not ours
		_, err = h.stateMatrix.Update(remoteFeed, frontierUpdate)
		if err != nil {
			return err
		}

		// Decide what to stream based on current frontiers
		sm := h.stateMatrix
		sm.mu.Lock()
		selfNf, _ := sm.loadFrontier(h.self.String())
		sm.mu.Unlock()

		for feedStr, theirNote := range frontierUpdate {
			myNote, has := selfNf[feedStr]
			if !has {
				continue
			}

			// If they said they want to receive AND we have more than they do, start streaming
			if theirNote.Receive && myNote.Seq > theirNote.Seq {
				feed, err := refs.ParseFeedRef(feedStr)
				if err != nil {
					continue
				}

				slog.Debug("ebt handle duplex streaming history", "feed", feedStr, "from_seq", theirNote.Seq+1)

				arg := CreateHistArgs{
					ID:    feed,
					Seq:   theirNote.Seq + 1,
					Limit: -1,
					Live:  true,
				}

				subCtx, cancel := context.WithCancel(ctx)
				session.Subscribed(feedStr, cancel)

				go func(fStr string, cxt context.Context, a CreateHistArgs) {
					if err := h.createStreamHistory(cxt, tx, a); err != nil {
						if !errors.Is(err, context.Canceled) {
							slog.Debug("ebt create stream history error", "feed", fStr, "err", err)
						}
					}
				}(feedStr, subCtx, arg)
			}
		}

		// Also check if WE need to send notes back (e.g. if we are following feeds they just mentioned)
		wants, err := h.stateMatrix.Changed(h.self, remoteFeed)
		if err == nil && len(wants) > 0 {
			data, err := json.Marshal(wants)
			if err == nil {
				_ = tx.Write(ctx, data)
			}
		}
	}
}

func (h *EBTHandler) sendState(ctx context.Context, tx Writer, remote string) error {
	currState, err := h.stateMatrix.Changed(h.self, nil)
	if err != nil {
		return err
	}

	data, err := json.Marshal(currState)
	if err != nil {
		return err
	}

	slog.Debug("ebt send state", "remote", remote, "bytes", len(data), "state", string(data))

	return tx.Write(ctx, data)
}

type CreateHistArgs struct {
	ID    *refs.FeedRef
	Seq   int64
	Limit int
	Live  bool
}

func (h *EBTHandler) createStreamHistory(ctx context.Context, tx Writer, arg CreateHistArgs) error {
	feed := arg.ID
	slog.Debug("ebt create stream history starting", "feed", feed.String(), "seq", arg.Seq, "live", arg.Live)

	retryDelay := 100 * time.Millisecond
	maxRetries := 50
	maxWaitTime := 60 * time.Second
	retries := 0
	startTime := time.Now()

	for seq := arg.Seq; ; {
		msg, err := h.store.GetMessage(feed, seq)
		if err != nil {
			if errors.Is(err, feedlog.ErrNotFound) || errors.Is(err, ErrNotFound) {
				if !arg.Live {
					return nil
				}
				if time.Since(startTime) > maxWaitTime {
					slog.Debug("ebt create stream history not found after timeout", "feed", feed.String(), "seq", seq, "elapsed", time.Since(startTime))
					return nil
				}
				retries++
				if retries > maxRetries {
					slog.Debug("ebt create stream history not found after retries", "feed", feed.String(), "seq", seq, "max_retries", maxRetries)
					currentSeq, seqErr := h.store.GetFeedSeq(feed)
					if seqErr == nil && currentSeq >= seq {
						slog.Debug("ebt create stream history caught up", "feed", feed.String(), "seq", currentSeq)
						return nil
					}
					retries = 0
					retryDelay = 100 * time.Millisecond
				}
				slog.Debug("ebt create stream history not found, retrying", "feed", feed.String(), "seq", seq, "retry", retries, "max_retries", maxRetries, "delay", retryDelay)
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(retryDelay):
					if retryDelay < 2*time.Second {
						retryDelay *= 2
					}
					continue
				}
			}
			return err
		}

		retries = 0
		retryDelay = 100 * time.Millisecond
		startTime = time.Now()

		slog.Debug("ebt create stream history sending msg", "feed", feed.String(), "seq", seq, "bytes", len(msg))
		if err := tx.Write(ctx, msg); err != nil {
			return err
		}
		seq++
	}
}

func FeedRefToPtr(f refs.FeedRef) *refs.FeedRef {
	return &f
}
