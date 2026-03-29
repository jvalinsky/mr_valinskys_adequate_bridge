package room

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/roomdb"
)

type inviteHandler struct {
	roomDB  roomdb.InvitesService
	config  roomdb.RoomConfig
	keyPair inviteKeyPair
	domain  string
}

type inviteKeyPair interface {
	FeedRef() refs.FeedRef
}

func newInviteHandler(roomDB roomdb.InvitesService, config roomdb.RoomConfig, keyPair inviteKeyPair, domain string) *inviteHandler {
	return &inviteHandler{
		roomDB:  roomDB,
		config:  config,
		keyPair: keyPair,
		domain:  domain,
	}
}

func (h *inviteHandler) handleCreateInvite(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	mode, err := h.config.GetPrivacyMode(r.Context())
	if err != nil {
		http.Error(w, "Failed to check room mode", http.StatusInternalServerError)
		return
	}

	if mode != roomdb.ModeOpen {
		http.Error(w, "room mode is not open", http.StatusForbidden)
		return
	}

	token, err := h.roomDB.Create(r.Context(), 0)
	if err != nil {
		http.Error(w, "Failed to create invite", http.StatusInternalServerError)
		return
	}

	feedRef := h.keyPair.FeedRef()
	domain := h.domain
	if domain == "" {
		domain = "localhost"
	}

	inviteURL := fmt.Sprintf("https://%s/%s/join?token=%s", domain, feedRef.String(), token)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"url": inviteURL,
	})
}

func (h *inviteHandler) handleJoin(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		http.NotFound(w, r)
		return
	}

	if r.Method == http.MethodGet {
		h.serveJoinPage(w, r, token)
		return
	}

	if r.Method == http.MethodPost {
		h.handleJoinSubmit(w, r, token)
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

func (h *inviteHandler) serveJoinPage(w http.ResponseWriter, r *http.Request, token string) {
	_, err := h.roomDB.GetByToken(r.Context(), token)
	if err != nil {
		http.Error(w, "Invalid or expired invite", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(joinPageHTML))
}

func (h *inviteHandler) handleJoinSubmit(w http.ResponseWriter, r *http.Request, token string) {
	http.Error(w, "Use SSB client to accept invite", http.StatusNotImplemented)
}

const joinPageHTML = `
<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <title>Join Room</title>
</head>
<body>
  <h1>Join Room</h1>
  <p>To join this room, use your SSB client with the invite link.</p>
</body>
</html>
`

func withContext(f func(context.Context) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := f(r.Context()); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}
