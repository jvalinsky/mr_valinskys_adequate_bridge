package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/feedlog"
)

func (h *clientUIHandler) handleAPIWhoami(w http.ResponseWriter, r *http.Request) {
	whoami, err := h.sbot.Whoami()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSONResponse(w, map[string]string{"feedId": whoami})
}

func (h *clientUIHandler) handleAPIState(w http.ResponseWriter, r *http.Request) {
	store := h.sbot.Store()
	whoami, _ := h.sbot.Whoami()
	peers := h.sbot.Peers()

	rxLog, _ := store.ReceiveLog()
	rxSeq := int64(0)
	if rxLog != nil {
		rxSeq, _ = rxLog.Seq()
	}

	userSeq := int64(-1)
	if userLog, err := store.Logs().Get(whoami); err == nil {
		userSeq, _ = userLog.Seq()
	}

	feeds, _ := store.Logs().List()

	peerIDs := make([]string, 0, len(peers))
	for _, p := range peers {
		peerIDs = append(peerIDs, p.ID.String())
	}

	ebtPeers := 0
	if sm := h.sbot.StateMatrix(); sm != nil {
		ebtPeers = len(sm.Export())
	}

	state := map[string]interface{}{
		"identity":       whoami,
		"peers":          len(peers),
		"feedsCount":     len(feeds),
		"receiveLogSeq":  rxSeq,
		"userFeedSeq":    userSeq,
		"connectedPeers": peerIDs,
		"ebtPeers":       ebtPeers,
		"uptimeSeconds":  int64(time.Since(h.startTime).Seconds()),
	}

	writeJSONResponse(w, state)
}

func (h *clientUIHandler) handleAPIFeeds(w http.ResponseWriter, r *http.Request) {
	store := h.sbot.Store()
	feedIDs, _ := store.Logs().List()

	type FeedInfo struct {
		FeedID   string `json:"feedId"`
		Sequence int64  `json:"sequence"`
	}

	feedList := make([]FeedInfo, 0, len(feedIDs))
	for _, id := range feedIDs {
		seq := int64(0)
		if feedLog, err := store.Logs().Get(id); err == nil {
			seq, _ = feedLog.Seq()
		}
		feedList = append(feedList, FeedInfo{FeedID: id, Sequence: seq})
	}

	writeJSONResponse(w, map[string]interface{}{
		"feeds": feedList,
		"count": len(feedList),
	})
}

func (h *clientUIHandler) handleAPIFeed(w http.ResponseWriter, r *http.Request) {
	store := h.sbot.Store()
	whoami, _ := h.sbot.Whoami()
	limit := parseLimit(r, 50)
	filterType := r.URL.Query().Get("type")

	var messages []map[string]interface{}

	collectMessages := func(feedLog feedlog.Log, skipAuthor string) {
		for _, msg := range readFeedMessages(feedLog, limit) {
			if skipAuthor != "" && msg.Metadata.Author == skipAuthor {
				continue
			}
			item := messageDTO(msg)
			if filterType != "" {
				var c map[string]interface{}
				json.Unmarshal(msg.Value, &c)
				if t, _ := c["type"].(string); t != filterType {
					continue
				}
			}
			messages = append(messages, item)
		}
	}

	if userLog, err := store.Logs().Get(whoami); err == nil {
		collectMessages(userLog, "")
	}
	if rxLog, err := store.ReceiveLog(); err == nil {
		collectMessages(rxLog, whoami)
	}

	writeJSONResponse(w, map[string]interface{}{
		"messages": messages,
		"count":    len(messages),
	})
}

func (h *clientUIHandler) handleAPIFeedByID(w http.ResponseWriter, r *http.Request) {
	feedId := chi.URLParam(r, "feedId")
	limit := parseLimit(r, 50)
	filterType := r.URL.Query().Get("type")

	store := h.sbot.Store()
	feedLog, err := store.Logs().Get(feedId)
	if err != nil {
		http.Error(w, `{"error":"feed not found"}`, http.StatusNotFound)
		return
	}

	var messages []map[string]interface{}
	for _, msg := range readFeedMessages(feedLog, limit) {
		if filterType != "" {
			var c map[string]interface{}
			json.Unmarshal(msg.Value, &c)
			if t, _ := c["type"].(string); t != filterType {
				continue
			}
		}
		messages = append(messages, messageDTO(msg))
	}

	writeJSONResponse(w, map[string]interface{}{
		"messages": messages,
		"count":    len(messages),
	})
}

func (h *clientUIHandler) handleAPIMessage(w http.ResponseWriter, r *http.Request) {
	feedId := chi.URLParam(r, "feedId")
	seqStr := chi.URLParam(r, "seq")

	seq, err := strconv.ParseInt(seqStr, 10, 64)
	if err != nil {
		http.Error(w, `{"error":"invalid sequence"}`, http.StatusBadRequest)
		return
	}

	store := h.sbot.Store()
	feedLog, err := store.Logs().Get(feedId)
	if err != nil {
		http.Error(w, `{"error":"feed not found"}`, http.StatusNotFound)
		return
	}

	msg, err := feedLog.Get(seq)
	if err != nil {
		http.Error(w, `{"error":"message not found"}`, http.StatusNotFound)
		return
	}

	writeJSONResponse(w, messageDetailDTO(msg))
}

func (h *clientUIHandler) handleAPIPeers(w http.ResponseWriter, r *http.Request) {
	peers := h.sbot.Peers()

	type PeerInfo struct {
		ID         string `json:"id"`
		Address    string `json:"address"`
		Connected  bool   `json:"connected"`
		ReadBytes  int64  `json:"readBytes"`
		WriteBytes int64  `json:"writeBytes"`
		LatencyMs  int64  `json:"latencyMs"`
	}

	peerList := make([]PeerInfo, 0, len(peers))
	for _, p := range peers {
		peerList = append(peerList, PeerInfo{
			ID:         p.ID.String(),
			Address:    p.Conn.RemoteAddr().String(),
			Connected:  true,
			ReadBytes:  p.ReadBytes(),
			WriteBytes: p.WriteBytes(),
			LatencyMs:  p.Latency().Milliseconds(),
		})
	}

	writeJSONResponse(w, map[string]interface{}{
		"peers": peerList,
		"count": len(peerList),
	})
}

func (h *clientUIHandler) handleAPIMessages(w http.ResponseWriter, r *http.Request) {
	store := h.sbot.Store()
	whoami, _ := h.sbot.Whoami()
	limit := parseLimit(r, 50)
	msgType := r.URL.Query().Get("type")
	author := r.URL.Query().Get("author")

	var messages []map[string]interface{}

	// If author is specified, query that specific feed
	targetFeed := whoami
	if author != "" {
		targetFeed = author
	}

	feedLog, err := store.Logs().Get(targetFeed)
	if err == nil {
		for _, msg := range readFeedMessages(feedLog, limit) {
			if msgType != "" {
				var c map[string]interface{}
				json.Unmarshal(msg.Value, &c)
				if t, _ := c["type"].(string); t != msgType {
					continue
				}
			}
			messages = append(messages, messageDTO(msg))
		}
	}

	writeJSONResponse(w, map[string]interface{}{
		"messages": messages,
		"count":    len(messages),
	})
}

func (h *clientUIHandler) handleAPIPublish(w http.ResponseWriter, r *http.Request) {
	var content map[string]interface{}

	// Accept both JSON body and form-encoded data
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		if err := json.NewDecoder(r.Body).Decode(&content); err != nil {
			http.Error(w, `{"error":"invalid JSON body"}`, http.StatusBadRequest)
			return
		}
	} else {
		r.ParseForm()
		contentType := strings.TrimSpace(r.Form.Get("type"))
		if contentType == "" {
			contentType = "post"
		}
		content = map[string]interface{}{"type": contentType}
		if text := strings.TrimSpace(r.Form.Get("text")); text != "" {
			content["text"] = text
		}
		for k, v := range r.Form {
			if k != "type" && k != "text" {
				content[k] = v[0]
			}
		}
	}

	if content["type"] == nil || content["type"] == "" {
		content["type"] = "post"
	}

	pub, err := h.sbot.Publisher()
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	msgRef, err := pub.PublishJSON(content)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	writeJSONResponse(w, map[string]interface{}{
		"success": true,
		"key":     msgRef.String(),
	})
}

func (h *clientUIHandler) handleAPIConnect(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Address string `json:"address"`
		PubKey  string `json:"pubkey"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid JSON body"}`, http.StatusBadRequest)
		return
	}

	if req.Address == "" || req.PubKey == "" {
		http.Error(w, `{"error":"address and pubkey required"}`, http.StatusBadRequest)
		return
	}

	pubkeyBytes, err := base64.StdEncoding.DecodeString(strings.TrimSuffix(req.PubKey, ".ed25519"))
	if err != nil || len(pubkeyBytes) != 32 {
		http.Error(w, `{"error":"invalid pubkey format"}`, http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	peer, err := h.sbot.Connect(ctx, req.Address, pubkeyBytes)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"connect failed: %s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	writeJSONResponse(w, map[string]interface{}{
		"success": true,
		"peer":    peer.ID.String(),
		"address": peer.Conn.RemoteAddr().String(),
	})
}

func (h *clientUIHandler) handleAPIFollow(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Feed      string `json:"feed"`
		Following *bool  `json:"following"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid JSON body"}`, http.StatusBadRequest)
		return
	}

	if req.Feed == "" {
		http.Error(w, `{"error":"feed required"}`, http.StatusBadRequest)
		return
	}
	if !strings.HasPrefix(req.Feed, "@") {
		req.Feed = "@" + req.Feed
	}

	following := true
	if req.Following != nil {
		following = *req.Following
	}

	pub, err := h.sbot.Publisher()
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	content := map[string]interface{}{
		"type":      "contact",
		"contact":   req.Feed,
		"following": following,
		"blocking":  false,
	}

	msgRef, err := pub.PublishJSON(content)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	writeJSONResponse(w, map[string]interface{}{
		"success":   true,
		"key":       msgRef.String(),
		"feed":      req.Feed,
		"following": following,
	})
}

func (h *clientUIHandler) handleAPIReplication(w http.ResponseWriter, r *http.Request) {
	sm := h.sbot.StateMatrix()
	if sm == nil {
		writeJSONResponse(w, map[string]interface{}{"enabled": false})
		return
	}

	matrix := sm.Export()
	writeJSONResponse(w, map[string]interface{}{
		"enabled": true,
		"peers":   len(matrix),
		"matrix":  matrix,
	})
}

func (h *clientUIHandler) handleAPIConversations(w http.ResponseWriter, r *http.Request) {
	whoami, _ := h.sbot.Whoami()

	writeJSONResponse(w, map[string]interface{}{
		"feed":          whoami,
		"conversations": []string{},
		"note":          "DM storage requires database integration",
	})
}

func (h *clientUIHandler) handleAPIConversation(w http.ResponseWriter, r *http.Request) {
	feedID := chi.URLParam(r, "feed")
	if feedID == "" {
		http.Error(w, "feed required", http.StatusBadRequest)
		return
	}

	writeJSONResponse(w, map[string]interface{}{
		"with":     feedID,
		"messages": []interface{}{},
		"note":     "DM storage requires database integration",
	})
}

func (h *clientUIHandler) handleAPISendDM(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Recipient string          `json:"recipient"`
		Content   json.RawMessage `json:"content"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	if req.Recipient == "" || len(req.Content) == 0 {
		http.Error(w, "recipient and content required", http.StatusBadRequest)
		return
	}

	writeJSONResponse(w, map[string]interface{}{
		"status":    "placeholder",
		"message":   "DM sending requires ssb-client with DM support compiled in",
		"recipient": req.Recipient,
	})
}

// ---------------------------------------------------------------------------
// CLI identity commands (offline — no server needed)
// ---------------------------------------------------------------------------
