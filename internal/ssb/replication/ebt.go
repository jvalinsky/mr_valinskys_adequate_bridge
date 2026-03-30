package replication

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/feedlog"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
)

const EBTVersion = 3

type Note struct {
	Seq       int64
	Replicate bool
	Receive   bool
}

type NetworkFrontier map[string]Note

func (n Note) MarshalJSON() ([]byte, error) {
	if !n.Replicate {
		return []byte("-1"), nil
	}
	i := n.Seq
	if i == -1 {
		i = 0
	}
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
			Receive:   !(i&1 == 1),
			Seq:       i >> 1,
		}
		result[fstr] = n
	}
	*nf = result
	return nil
}

type StateMatrix struct {
	basePath  string
	self      string
	mu        sync.Mutex
	frontiers map[string]NetworkFrontier
	store     feedlog.FeedStore
}

func NewStateMatrix(basePath string, self *refs.FeedRef, store feedlog.FeedStore) (*StateMatrix, error) {
	sm := &StateMatrix{
		basePath:  basePath,
		frontiers: make(map[string]NetworkFrontier),
		store:     store,
	}
	if self != nil {
		sm.self = self.String()
	}
	return sm, nil
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
	log.Printf("[EBT DEBUG] initializeFeed: feed=%s seq=%d, Receive=true", feed.String(), seq)
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
	log.Printf("[EBT DEBUG] SetFeedSeq: feed=%s seq=%d, self_frontier now has %d feeds", feed.String(), seq, len(selfFrontier))
}

func (sm *StateMatrix) Changed(self, peer *refs.FeedRef) (NetworkFrontier, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	selfNf, err := sm.loadFrontier(self.String())
	if err != nil {
		return nil, err
	}
	if selfNf == nil {
		selfNf = make(NetworkFrontier)
	}

	log.Printf("[EBT DEBUG] Changed: self=%s, selfNf_count=%d, selfNf=%+v", self.String(), len(selfNf), selfNf)

	var peerNf NetworkFrontier
	if peer != nil {
		peerNf, err = sm.loadFrontier(peer.String())
		if err != nil {
			return nil, err
		}
		if peerNf == nil {
			peerNf = make(NetworkFrontier)
		}
		log.Printf("[EBT DEBUG] Changed: peer=%s, peerNf_count=%d", peer.String(), len(peerNf))
	} else {
		peerNf = make(NetworkFrontier)
		log.Printf("[EBT DEBUG] Changed: peer=nil (initial state request)")
	}

	relevant := make(NetworkFrontier)

	if peer == nil {
		for feed, note := range selfNf {
			if note.Replicate {
				relevant[feed] = note
			}
		}
		log.Printf("[EBT DEBUG] Changed: initial state, advertising %d feeds", len(relevant))
		return relevant, nil
	}

	for wantedFeed, myNote := range selfNf {
		theirNote, has := peerNf[wantedFeed]
		if !has && myNote.Receive {
			relevant[wantedFeed] = myNote
			continue
		}

		if !theirNote.Replicate {
			continue
		}

		if !theirNote.Receive && wantedFeed != peer.String() {
			continue
		}

		relevant[wantedFeed] = theirNote
	}

	return relevant, nil
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
	delete(s.open, addr)
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
	log.Printf("[EBT DEBUG] HandleDuplex: START remote=%s", remoteAddr)

	session := h.sessions.Started(remoteAddr)
	defer h.sessions.Ended(remoteAddr)

	log.Printf("[EBT DEBUG] HandleDuplex: sending initial state to %s", remoteAddr)
	if err := h.sendState(ctx, tx, remoteAddr); err != nil {
		log.Printf("[EBT DEBUG] HandleDuplex: sendState failed: %v", err)
		return err
	}
	log.Printf("[EBT DEBUG] HandleDuplex: initial state sent, waiting for peer frontier...")

	for {
		log.Printf("[EBT DEBUG] HandleDuplex: about to call rx.Next(ctx)...")
		ok := rx.Next(ctx)
		if !ok {
			err := rx.Err()
			log.Printf("[EBT DEBUG] HandleDuplex: rx.Next returned false, err=%v", err)
			return err
		}

		data, err := rx.Bytes()
		if err != nil {
			log.Printf("[EBT DEBUG] HandleDuplex: rx.Bytes error: %v", err)
			return err
		}
		log.Printf("[EBT DEBUG] HandleDuplex: received %d bytes from peer: %s", len(data), string(data))

		var frontierUpdate NetworkFrontier
		if err := json.Unmarshal(data, &frontierUpdate); err != nil {
			log.Printf("[EBT DEBUG] HandleDuplex: failed to unmarshal frontier: %v", err)
			continue
		}

		log.Printf("[EBT DEBUG] HandleDuplex: received frontier from %s: %+v", remoteAddr, frontierUpdate)

		// Store remote peer's frontier under their identity, not ours
		_, err = h.stateMatrix.Update(remoteFeed, frontierUpdate)
		if err != nil {
			return err
		}

		// Compute what's changed between our frontier and the remote peer's
		wants, err := h.stateMatrix.Changed(h.self, remoteFeed)
		if err != nil {
			return err
		}

		log.Printf("[EBT DEBUG] HandleDuplex: feeds_wanted=%d: %+v", len(wants), wants)

		for feedStr, note := range wants {
			if !note.Replicate {
				session.Unsubscribe(feedStr)
				continue
			}

			if !note.Receive {
				session.Unsubscribe(feedStr)
				continue
			}

			feed, err := refs.ParseFeedRef(feedStr)
			if err != nil {
				continue
			}

			log.Printf("[EBT DEBUG] HandleDuplex: streaming history for feed=%s seq=%d", feedStr, note.Seq+1)

			arg := CreateHistArgs{
				ID:    feed,
				Seq:   note.Seq + 1,
				Limit: -1,
				Live:  true,
			}

			subCtx, cancel := context.WithCancel(ctx)
			err = h.createStreamHistory(subCtx, tx, arg)
			if err != nil {
				log.Printf("[EBT DEBUG] HandleDuplex: createStreamHistory error for %s: %v", feedStr, err)
				cancel()
				continue
			}
			session.Subscribed(feedStr, cancel)
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

	log.Printf("[EBT DEBUG] sendState: remote=%s, state_bytes=%d, state=%s", remote, len(data), string(data))

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
	log.Printf("[EBT DEBUG] createStreamHistory: starting for feed=%s seq=%d live=%v", feed.String(), arg.Seq, arg.Live)

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
					log.Printf("[EBT DEBUG] createStreamHistory: feed=%s seq=%d not found after %v, returning current state", feed.String(), seq, time.Since(startTime))
					return nil
				}
				retries++
				if retries > maxRetries {
					log.Printf("[EBT DEBUG] createStreamHistory: feed=%s seq=%d not found after %d retries, checking GetFeedSeq", feed.String(), seq, maxRetries)
					currentSeq, seqErr := h.store.GetFeedSeq(feed)
					if seqErr == nil && currentSeq >= seq {
						log.Printf("[EBT DEBUG] createStreamHistory: feed=%s caught up to seq=%d", feed.String(), currentSeq)
						return nil
					}
					retries = 0
					retryDelay = 100 * time.Millisecond
				}
				log.Printf("[EBT DEBUG] createStreamHistory: feed=%s seq=%d not found, retry %d/%d in %v", feed.String(), seq, retries, maxRetries, retryDelay)
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

		log.Printf("[EBT DEBUG] createStreamHistory: sending msg for feed=%s seq=%d bytes=%d", feed.String(), seq, len(msg))
		if err := tx.Write(ctx, msg); err != nil {
			return err
		}
		seq++
	}
}

func FeedRefToPtr(f refs.FeedRef) *refs.FeedRef {
	return &f
}
