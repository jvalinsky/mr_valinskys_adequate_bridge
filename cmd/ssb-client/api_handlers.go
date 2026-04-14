package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/feedlog"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/roomdb"
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
		h.writeJSONError(w, http.StatusInternalServerError, err)
		return
	}

	msgRef, err := pub.PublishJSON(content)
	if err != nil {
		h.writeJSONError(w, http.StatusInternalServerError, err)
		return
	}
	if replicateFeed, ok := replicationTargetFromContact(content); ok {
		h.sbot.Replicate(replicateFeed)
	}

	writeJSONResponse(w, map[string]interface{}{
		"success": true,
		"key":     msgRef.String(),
	})
}

func (h *clientUIHandler) handleAPIConnect(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Address     string `json:"address"`
		PubKey      string `json:"pubkey"`
		Multiserver string `json:"multiserver"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid JSON body"}`, http.StatusBadRequest)
		return
	}

	targetAddr, pubkeyBytes, err := resolvePeerConnectTarget(req.Multiserver, req.Address, req.PubKey)
	if err != nil {
		h.writeJSONError(w, http.StatusBadRequest, err)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	peer, err := h.sbot.Connect(ctx, targetAddr, pubkeyBytes)
	if err != nil {
		h.writeJSONError(w, http.StatusInternalServerError, fmt.Errorf("connect failed: %w", err))
		return
	}

	writeJSONResponse(w, map[string]interface{}{
		"success": true,
		"peer":    peer.ID.String(),
		"address": peer.Conn.RemoteAddr().String(),
		"target":  targetAddr,
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
		h.writeJSONError(w, http.StatusInternalServerError, err)
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
		h.writeJSONError(w, http.StatusInternalServerError, err)
		return
	}
	if replicateFeed, ok := replicationTargetFromContact(content); ok {
		h.sbot.Replicate(replicateFeed)
	}

	writeJSONResponse(w, map[string]interface{}{
		"success":   true,
		"key":       msgRef.String(),
		"feed":      req.Feed,
		"following": following,
	})
}

func (h *clientUIHandler) handleAPIReplication(w http.ResponseWriter, r *http.Request) {
	snapshot, err := collectReplicationSnapshot(h.sbot)
	if err != nil {
		h.writeJSONError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSONResponse(w, snapshot)
}

func (h *clientUIHandler) handleAPICapabilities(w http.ResponseWriter, r *http.Request) {
	whoami, _ := h.sbot.Whoami()
	if err := h.ensureIndexSynced(); err != nil {
		h.slog.Warn("ui index sync failed in capabilities", "error", err)
	}

	manifestByType := map[string][]string{}
	if manifest := h.sbot.Manifest(); manifest != nil {
		manifestByType = manifest.EntriesByType()
	}
	actualMethods := flattenManifestByType(manifestByType)
	requiredMethods := flattenManifestByType(requiredManifestByType)

	gaps := []string{}
	if h.index == nil {
		gaps = append(gaps, "ui-index disabled")
	}
	missingRPC := missingMethods(requiredMethods, actualMethods)
	if len(missingRPC) > 0 {
		gaps = append(gaps, "missing required rpc methods")
	}
	gaps = append(gaps, "private-box decryption indexing is not yet implemented")

	writeJSONResponse(w, map[string]interface{}{
		"identity": whoami,
		"version":  "0.3.0-parity-wip",
		"index": map[string]interface{}{
			"enabled": h.index != nil,
			"path":    h.indexPath,
		},
		"ui": map[string]interface{}{
			"timelineModes": []string{"inbox", "network", "profile", "channel", "mentions"},
			"features": []string{
				"feed-browse", "thread-view", "channels", "votes", "followers",
				"dm-send", "room-join", "replication-status", "blob-upload",
			},
		},
		"rpc": map[string]interface{}{
			"requiredByType": requiredManifestByType,
			"manifestByType": manifestByType,
			"missingMethods": missingRPC,
		},
		"http": map[string]interface{}{
			"apiRoutes": []string{
				"/api/capabilities", "/api/timeline", "/api/thread/{msgKey}",
				"/api/channels", "/api/votes", "/api/search", "/api/followers",
				"/api/conversations", "/api/conversations/{id}",
				"/api/messages/send", "/api/room/state", "/api/room/invites",
			},
		},
		"knownGaps": gaps,
	})
}

func (h *clientUIHandler) handleAPITimeline(w http.ResponseWriter, r *http.Request) {
	if h.index == nil {
		http.Error(w, `{"error":"ui index is not enabled"}`, http.StatusServiceUnavailable)
		return
	}
	if err := h.ensureIndexSynced(); err != nil {
		h.writeJSONError(w, http.StatusInternalServerError, err)
		return
	}

	whoami, _ := h.sbot.Whoami()
	mode := timelineMode(strings.TrimSpace(r.URL.Query().Get("mode")))
	if mode == "" {
		mode = timelineModeNetwork
	}

	limit := parseLimit(r, 50)
	offset := 0
	if raw := strings.TrimSpace(r.URL.Query().Get("offset")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	items, err := h.index.queryTimeline(timelineQuery{
		Mode:     mode,
		Author:   r.URL.Query().Get("author"),
		Channel:  r.URL.Query().Get("channel"),
		Search:   strings.TrimSpace(r.URL.Query().Get("q")),
		Limit:    limit,
		Offset:   offset,
		SelfFeed: whoami,
	})
	if err != nil {
		h.writeJSONError(w, http.StatusInternalServerError, err)
		return
	}

	writeJSONResponse(w, map[string]interface{}{
		"mode":     mode,
		"count":    len(items),
		"limit":    limit,
		"offset":   offset,
		"messages": items,
	})
}

func (h *clientUIHandler) handleAPIThread(w http.ResponseWriter, r *http.Request) {
	if h.index == nil {
		http.Error(w, `{"error":"ui index is not enabled"}`, http.StatusServiceUnavailable)
		return
	}
	if err := h.ensureIndexSynced(); err != nil {
		h.writeJSONError(w, http.StatusInternalServerError, err)
		return
	}

	msgKey := strings.TrimSpace(chi.URLParam(r, "msgKey"))
	if msgKey == "" {
		http.Error(w, `{"error":"msgKey is required"}`, http.StatusBadRequest)
		return
	}

	thread, root, err := h.index.queryThread(msgKey)
	if err != nil {
		h.writeJSONError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSONResponse(w, map[string]interface{}{
		"root":     root,
		"count":    len(thread),
		"messages": thread,
	})
}

func (h *clientUIHandler) handleAPIChannels(w http.ResponseWriter, r *http.Request) {
	if h.index == nil {
		http.Error(w, `{"error":"ui index is not enabled"}`, http.StatusServiceUnavailable)
		return
	}
	if err := h.ensureIndexSynced(); err != nil {
		h.writeJSONError(w, http.StatusInternalServerError, err)
		return
	}

	limit := parseLimit(r, 50)
	channels, err := h.index.queryChannels(limit)
	if err != nil {
		h.writeJSONError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSONResponse(w, map[string]interface{}{
		"count":    len(channels),
		"channels": channels,
	})
}

func (h *clientUIHandler) handleAPIVotes(w http.ResponseWriter, r *http.Request) {
	if h.index == nil {
		http.Error(w, `{"error":"ui index is not enabled"}`, http.StatusServiceUnavailable)
		return
	}
	if err := h.ensureIndexSynced(); err != nil {
		h.writeJSONError(w, http.StatusInternalServerError, err)
		return
	}

	limit := parseLimit(r, 50)
	target := strings.TrimSpace(r.URL.Query().Get("target"))
	votes, err := h.index.queryVotes(target, limit)
	if err != nil {
		h.writeJSONError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSONResponse(w, map[string]interface{}{
		"target": target,
		"count":  len(votes),
		"votes":  votes,
	})
}

func (h *clientUIHandler) handleAPISearch(w http.ResponseWriter, r *http.Request) {
	if h.index == nil {
		http.Error(w, `{"error":"ui index is not enabled"}`, http.StatusServiceUnavailable)
		return
	}
	if err := h.ensureIndexSynced(); err != nil {
		h.writeJSONError(w, http.StatusInternalServerError, err)
		return
	}

	whoami, _ := h.sbot.Whoami()
	limit := parseLimit(r, 50)
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	items, err := h.index.queryTimeline(timelineQuery{
		Mode:     timelineModeNetwork,
		Search:   query,
		Limit:    limit,
		SelfFeed: whoami,
	})
	if err != nil {
		h.writeJSONError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSONResponse(w, map[string]interface{}{
		"query":    query,
		"count":    len(items),
		"messages": items,
	})
}

func (h *clientUIHandler) handleAPIFollowers(w http.ResponseWriter, r *http.Request) {
	if h.index == nil {
		http.Error(w, `{"error":"ui index is not enabled"}`, http.StatusServiceUnavailable)
		return
	}
	if err := h.ensureIndexSynced(); err != nil {
		h.writeJSONError(w, http.StatusInternalServerError, err)
		return
	}
	whoami, _ := h.sbot.Whoami()
	rel, err := h.index.queryFollowers(whoami)
	if err != nil {
		h.writeJSONError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSONResponse(w, map[string]interface{}{
		"feed":      whoami,
		"followers": rel.Followers,
		"following": rel.Following,
		"counts": map[string]int{
			"followers": len(rel.Followers),
			"following": len(rel.Following),
		},
	})
}

func (h *clientUIHandler) handleAPIConversationsV2(w http.ResponseWriter, r *http.Request) {
	if h.index == nil {
		http.Error(w, `{"error":"ui index is not enabled"}`, http.StatusServiceUnavailable)
		return
	}
	if err := h.ensureIndexSynced(); err != nil {
		h.writeJSONError(w, http.StatusInternalServerError, err)
		return
	}

	conversations, err := h.index.queryConversations()
	if err != nil {
		h.writeJSONError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSONResponse(w, map[string]interface{}{
		"count":         len(conversations),
		"conversations": conversations,
	})
}

func (h *clientUIHandler) handleAPIConversationV2(w http.ResponseWriter, r *http.Request) {
	if h.index == nil {
		http.Error(w, `{"error":"ui index is not enabled"}`, http.StatusServiceUnavailable)
		return
	}
	if err := h.ensureIndexSynced(); err != nil {
		h.writeJSONError(w, http.StatusInternalServerError, err)
		return
	}

	peer := normalizeFeed(chi.URLParam(r, "id"))
	if peer == "" {
		http.Error(w, `{"error":"conversation id is required"}`, http.StatusBadRequest)
		return
	}
	limit := parseLimit(r, 100)
	messages, err := h.index.queryConversationMessages(peer, limit)
	if err != nil {
		h.writeJSONError(w, http.StatusInternalServerError, err)
		return
	}

	writeJSONResponse(w, map[string]interface{}{
		"with":     peer,
		"count":    len(messages),
		"messages": messages,
	})
}

func (h *clientUIHandler) handleAPIMessagesSend(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Recipient  string         `json:"recipient"`
		Recipients []string       `json:"recipients"`
		Text       string         `json:"text"`
		Content    map[string]any `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	recipients := make([]string, 0)
	if single := normalizeFeed(req.Recipient); single != "" {
		recipients = append(recipients, single)
	}
	for _, raw := range req.Recipients {
		if normalized := normalizeFeed(raw); normalized != "" {
			recipients = append(recipients, normalized)
		}
	}
	if len(recipients) == 0 {
		http.Error(w, `{"error":"recipient or recipients is required"}`, http.StatusBadRequest)
		return
	}

	payload := map[string]interface{}{
		"type": "post",
	}
	if req.Text != "" {
		payload["text"] = req.Text
	}
	for k, v := range req.Content {
		payload[k] = v
	}
	if _, exists := payload["text"]; !exists {
		payload["text"] = req.Text
	}

	pub, err := h.sbot.Publisher()
	if err != nil {
		h.writeJSONError(w, http.StatusInternalServerError, err)
		return
	}

	keys := make([]string, 0, len(recipients))
	failures := []map[string]string{}
	for _, recipient := range recipients {
		msgRef, err := pub.PublishPrivate(payload, recipient)
		if err != nil {
			failures = append(failures, map[string]string{
				"recipient": recipient,
				"error":     err.Error(),
			})
			continue
		}
		keys = append(keys, msgRef.String())
		if h.index != nil {
			text := strings.TrimSpace(asString(payload["text"]))
			_ = h.index.recordSentPrivate(msgRef.String(), recipient, text, nowMillis())
		}
	}

	status := http.StatusOK
	if len(keys) == 0 {
		status = http.StatusBadGateway
	}
	writeJSONResponseWithStatus(w, status, map[string]interface{}{
		"sent":       len(keys),
		"keys":       keys,
		"failures":   failures,
		"recipients": recipients,
	})
}

func (h *clientUIHandler) handleAPIRoomState(w http.ResponseWriter, r *http.Request) {
	peers := h.sbot.Peers()
	peerIDs := make([]string, 0, len(peers))
	for _, p := range peers {
		peerIDs = append(peerIDs, p.ID.String())
	}

	roomEnabled := h.sbot.RoomEnabled()
	roomModeValue := strings.TrimSpace(h.sbot.RoomMode())
	roomHTTPValue := strings.TrimSpace(h.sbot.RoomHTTPAddr())

	attendants := []map[string]interface{}{}
	endpoints := []map[string]interface{}{}
	if state := h.sbot.RoomState(); state != nil {
		for _, att := range state.Attendants() {
			attendants = append(attendants, map[string]interface{}{
				"id":        att.ID.String(),
				"address":   att.Addr,
				"connected": att.Connected.UnixMilli(),
			})
		}
		for _, ep := range state.Peers() {
			endpoints = append(endpoints, map[string]interface{}{
				"id":        ep.ID.String(),
				"address":   ep.Addr,
				"connected": ep.Connected.UnixMilli(),
			})
		}
	}

	memberCount := 0
	activeInvites := 0
	totalInvites := 0
	privacyMode := "unknown"
	if roomDB := h.sbot.RoomDB(); roomDB != nil {
		ctx := r.Context()
		if members, err := roomDB.Members().List(ctx); err == nil {
			memberCount = len(members)
		}
		if count, err := roomDB.Invites().Count(ctx, true); err == nil {
			activeInvites = int(count)
		}
		if count, err := roomDB.Invites().Count(ctx, false); err == nil {
			totalInvites = int(count)
		}
		if mode, err := roomDB.RoomConfig().GetPrivacyMode(ctx); err == nil {
			switch mode {
			case roomdb.ModeOpen:
				privacyMode = "open"
			case roomdb.ModeCommunity:
				privacyMode = "community"
			case roomdb.ModeRestricted:
				privacyMode = "restricted"
			}
		}
	}

	writeJSONResponse(w, map[string]interface{}{
		"roomEnabled":      roomEnabled,
		"roomMode":         roomModeValue,
		"roomHTTPAddr":     roomHTTPValue,
		"privacyMode":      privacyMode,
		"connectedPeers":   len(peers),
		"connectedPeerIds": peerIDs,
		"attendants":       attendants,
		"endpoints":        endpoints,
		"members": map[string]interface{}{
			"count": memberCount,
		},
		"invites": map[string]interface{}{
			"active": activeInvites,
			"total":  totalInvites,
		},
		"inviteConsumePath": "/room",
	})
}

func (h *clientUIHandler) handleAPIRoomInvites(w http.ResponseWriter, r *http.Request) {
	roomDB := h.sbot.RoomDB()
	if roomDB == nil {
		http.Error(w, `{"error":"room database is unavailable"}`, http.StatusServiceUnavailable)
		return
	}

	type inviteInfo struct {
		ID        int64  `json:"id"`
		CreatedBy int64  `json:"createdBy"`
		CreatedAt int64  `json:"createdAt"`
		Active    bool   `json:"active"`
		Token     string `json:"token,omitempty"`
	}

	ctx := r.Context()
	if r.Method == http.MethodPost {
		var req struct {
			Action string `json:"action"`
			ID     int64  `json:"id"`
		}

		if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			_ = json.NewDecoder(r.Body).Decode(&req)
		} else {
			_ = r.ParseForm()
			req.Action = strings.TrimSpace(r.Form.Get("action"))
			if rawID := strings.TrimSpace(r.Form.Get("id")); rawID != "" {
				if parsed, err := strconv.ParseInt(rawID, 10, 64); err == nil {
					req.ID = parsed
				}
			}
		}

		switch strings.ToLower(strings.TrimSpace(req.Action)) {
		case "revoke":
			if req.ID <= 0 {
				http.Error(w, `{"error":"id is required for revoke action"}`, http.StatusBadRequest)
				return
			}
			if err := roomDB.Invites().Revoke(ctx, req.ID); err != nil {
				h.writeJSONError(w, http.StatusBadRequest, err)
				return
			}
			writeJSONResponse(w, map[string]interface{}{
				"ok":     true,
				"action": "revoke",
				"id":     req.ID,
			})
			return
		default:
			whoami, err := h.sbot.Whoami()
			if err != nil {
				h.writeJSONError(w, http.StatusInternalServerError, err)
				return
			}
			feedRef, err := refs.ParseFeedRef(whoami)
			if err != nil {
				h.writeJSONError(w, http.StatusInternalServerError, err)
				return
			}

			createdBy := int64(0)
			if member, err := roomDB.Members().GetByFeed(ctx, *feedRef); err == nil {
				createdBy = member.ID
			} else {
				memberID, addErr := roomDB.Members().Add(ctx, *feedRef, roomdb.RoleMember)
				if addErr != nil {
					h.writeJSONError(w, http.StatusBadRequest, addErr)
					return
				}
				createdBy = memberID
			}

			token, err := roomDB.Invites().Create(ctx, createdBy)
			if err != nil {
				h.writeJSONError(w, http.StatusBadRequest, err)
				return
			}
			inviteURL := token
			if base := strings.TrimRight(strings.TrimSpace(h.sbot.RoomHTTPAddr()), "/"); base != "" {
				inviteURL = base + "/join?token=" + token
			}

			writeJSONResponse(w, map[string]interface{}{
				"ok":        true,
				"action":    "create",
				"token":     token,
				"inviteURL": inviteURL,
			})
			return
		}
	}

	invites, err := roomDB.Invites().List(ctx)
	if err != nil {
		h.writeJSONError(w, http.StatusInternalServerError, err)
		return
	}
	activeCount, _ := roomDB.Invites().Count(ctx, true)

	items := make([]inviteInfo, 0, len(invites))
	for _, invite := range invites {
		items = append(items, inviteInfo{
			ID:        invite.ID,
			CreatedBy: invite.CreatedBy,
			CreatedAt: invite.CreatedAt,
			Active:    invite.Active,
		})
	}

	writeJSONResponse(w, map[string]interface{}{
		"count":  len(items),
		"active": int(activeCount),
		"items":  items,
		"actions": map[string]string{
			"create": "POST /api/room/invites",
			"revoke": "POST /api/room/invites {action:\"revoke\", id:<inviteId>}",
		},
	})
}

func (h *clientUIHandler) handleAPIConversations(w http.ResponseWriter, r *http.Request) {
	h.handleAPIConversationsV2(w, r)
}

func (h *clientUIHandler) handleAPIConversation(w http.ResponseWriter, r *http.Request) {
	feedID := chi.URLParam(r, "feed")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", feedID)
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
	h.handleAPIConversationV2(w, r)
}

func (h *clientUIHandler) handleAPISendDM(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Recipient string          `json:"recipient"`
		Content   json.RawMessage `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	out := map[string]interface{}{"type": "post"}
	if len(req.Content) > 0 {
		var decoded interface{}
		if err := json.Unmarshal(req.Content, &decoded); err == nil {
			switch t := decoded.(type) {
			case string:
				out["text"] = t
			case map[string]interface{}:
				for k, v := range t {
					out[k] = v
				}
			default:
				out["text"] = string(req.Content)
			}
		} else {
			out["text"] = string(req.Content)
		}
	}
	payload, _ := json.Marshal(map[string]interface{}{
		"recipient": req.Recipient,
		"content":   out,
	})
	r.Body = io.NopCloser(strings.NewReader(string(payload)))
	h.handleAPIMessagesSend(w, r)
}

// ---------------------------------------------------------------------------
// CLI identity commands (offline — no server needed)
// ---------------------------------------------------------------------------
