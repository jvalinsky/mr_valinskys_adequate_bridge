package room

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/message/legacy"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/muxrpc"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/roomdb"
)

var (
	loginLimiter = &rateLimiter{
		limits: make(map[string]time.Time),
	}
)

type rateLimiter struct {
	mu     sync.Mutex
	limits map[string]time.Time
}

func (l *rateLimiter) Allow(remoteAddr string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	host, _, _ := net.SplitHostPort(remoteAddr)
	last, ok := l.limits[host]
	if ok && time.Since(last) < 1*time.Second {
		return false
	}
	l.limits[host] = time.Now()
	return true
}

type challengeStore struct {
	mu     sync.Mutex
	stored map[string]pendingAuth
}

type pendingAuth struct {
	sc      []byte
	cc      []byte
	cid     refs.FeedRef
	expires time.Time
}

func newChallengeStore(ctx context.Context) *challengeStore {
	cs := &challengeStore{
		stored: make(map[string]pendingAuth),
	}
	go cs.cleanupLoop(ctx)
	return cs
}

func (s *challengeStore) cleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.mu.Lock()
			now := time.Now()
			for k, v := range s.stored {
				if now.After(v.expires) {
					delete(s.stored, k)
				}
			}
			s.mu.Unlock()
		}
	}
}

func (s *challengeStore) Add(sc, cc []byte, cid refs.FeedRef) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := fmt.Sprintf("%s:%s", base64.StdEncoding.EncodeToString(sc), base64.StdEncoding.EncodeToString(cc))
	s.stored[key] = pendingAuth{
		sc:      sc,
		cc:      cc,
		cid:     cid,
		expires: time.Now().Add(5 * time.Minute),
	}
}

func (s *challengeStore) Get(sc, cc []byte) (pendingAuth, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := fmt.Sprintf("%s:%s", base64.StdEncoding.EncodeToString(sc), base64.StdEncoding.EncodeToString(cc))
	p, ok := s.stored[key]
	if !ok {
		return pendingAuth{}, false
	}
	if time.Now().After(p.expires) {
		delete(s.stored, key)
		return pendingAuth{}, false
	}
	return p, true
}

func (s *challengeStore) Remove(sc, cc []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := fmt.Sprintf("%s:%s", base64.StdEncoding.EncodeToString(sc), base64.StdEncoding.EncodeToString(cc))
	delete(s.stored, key)
}

type ssbAuthHandler struct {
	store      *challengeStore
	members    roomdb.MembersService
	authTokens roomdb.AuthWithSSBService
	roomID     refs.FeedRef
	getPeer    func(refs.FeedRef) *muxrpc.Server
}

func newSSBAuthHandler(store *challengeStore, members roomdb.MembersService, authTokens roomdb.AuthWithSSBService, roomID refs.FeedRef, getPeer func(refs.FeedRef) *muxrpc.Server) *ssbAuthHandler {
	return &ssbAuthHandler{
		store:      store,
		members:    members,
		authTokens: authTokens,
		roomID:     roomID,
		getPeer:    getPeer,
	}
}

func (h *ssbAuthHandler) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("ssb-http-auth") != "1" {
		http.Error(w, "Not a SIP 6 request", http.StatusBadRequest)
		return
	}

	cidStr := r.URL.Query().Get("cid")
	ccB64 := r.URL.Query().Get("cc")

	if cidStr == "" || ccB64 == "" {
		http.Error(w, "Missing cid or cc", http.StatusBadRequest)
		return
	}

	cid, err := refs.ParseFeedRef(cidStr)
	if err != nil {
		http.Error(w, "Invalid cid", http.StatusBadRequest)
		return
	}

	cc, err := base64.StdEncoding.DecodeString(ccB64)
	if err != nil || len(cc) != 32 {
		http.Error(w, "Invalid cc (must be 32 bytes base64)", http.StatusBadRequest)
		return
	}

	// Rate limit check
	if !loginLimiter.Allow(r.RemoteAddr) {
		http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
		return
	}

	// 1. Generate sc
	sc := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, sc); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// 2. Find peer connection
	peer := h.getPeer(*cid)
	if peer == nil {
		http.Error(w, "Access Forbidden", http.StatusForbidden)
		return
	}

	// 3. Call requestSolution async
	scB64 := base64.StdEncoding.EncodeToString(sc)

	// Add to store to prevent replay
	h.store.Add(sc, cc, *cid)
	defer h.store.Remove(sc, cc)

	var sol []byte
	err = peer.Async(r.Context(), &sol, muxrpc.TypeBinary, muxrpc.Method{"httpAuth", "requestSolution"}, scB64, ccB64)
	if err != nil {
		// Do not leak internal muxrpc errors
		http.Error(w, "Authentication flow failed", http.StatusForbidden)
		return
	}

	// 4. Reconstruct signed message
	// =http-auth-sign-in:${sid}:${cid}:${sc}:${cc}
	msg := fmt.Sprintf("=http-auth-sign-in:%s:%s:%s:%s", h.roomID.String(), cid.String(), scB64, ccB64)

	// 5. Verify signature
	if err := legacy.Signature(sol).Verify([]byte(msg), *cid); err != nil {
		http.Error(w, "Invalid signature", http.StatusForbidden)
		return
	}

	// 6. Issue token
	member, err := h.members.GetByFeed(r.Context(), *cid)
	if err != nil {
		http.Error(w, "Not a member", http.StatusForbidden)
		return
	}

	token, err := h.authTokens.CreateToken(r.Context(), member.ID)
	if err != nil {
		http.Error(w, "Token creation failed", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     authTokenCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   r.TLS != nil,
	})

	// Success
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "OK")
}
