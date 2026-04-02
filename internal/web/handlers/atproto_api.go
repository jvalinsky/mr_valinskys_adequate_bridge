package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

const defaultATProtoSourceKey = "default-relay"

type atprotoRepoActionRequest struct {
	DID    string `json:"did"`
	Reason string `json:"reason"`
}

func (h *UIHandler) bridgeReplayCursor(ctx context.Context) (string, error) {
	value, _, err := h.db.GetBridgeState(ctx, "atproto_event_cursor")
	if err != nil {
		return "", err
	}
	return value, nil
}

func (h *UIHandler) legacyFirehoseCursor(ctx context.Context) (string, error) {
	value, _, err := h.db.GetBridgeState(ctx, "firehose_seq")
	if err != nil {
		return "", err
	}
	return value, nil
}

func (h *UIHandler) eventLogHeadCursor(ctx context.Context) (string, error) {
	cursor, ok, err := h.db.GetLatestATProtoEventCursor(ctx)
	if err != nil || !ok {
		return "", err
	}
	return strconv.FormatInt(cursor, 10), nil
}

func (h *UIHandler) handleATProtoHealth(w http.ResponseWriter, r *http.Request) {
	store := h.requireATProtoStore(w)
	if store == nil {
		return
	}

	source, err := store.GetATProtoSource(r.Context(), defaultATProtoSourceKey)
	if err != nil {
		h.writeInternalError(w, "handleATProtoHealth", "Failed to load ATProto source state", err)
		return
	}
	repos, err := store.ListTrackedATProtoRepos(r.Context(), "")
	if err != nil {
		h.writeInternalError(w, "handleATProtoHealth", "Failed to load tracked repos", err)
		return
	}
	replayCursor, err := h.bridgeReplayCursor(r.Context())
	if err != nil {
		h.writeInternalError(w, "handleATProtoHealth", "Failed to load bridge replay cursor", err)
		return
	}
	eventHeadCursor, err := h.eventLogHeadCursor(r.Context())
	if err != nil {
		h.writeInternalError(w, "handleATProtoHealth", "Failed to load event-log head cursor", err)
		return
	}
	legacyCursor, err := h.legacyFirehoseCursor(r.Context())
	if err != nil {
		h.writeInternalError(w, "handleATProtoHealth", "Failed to load legacy cursor state", err)
		return
	}

	repoCounts := make(map[string]int)
	for _, repo := range repos {
		repoCounts[repo.SyncState]++
	}
	var relaySourceCursor any
	if source != nil {
		relaySourceCursor = source.LastSeq
	}

	h.writeJSON(w, http.StatusOK, map[string]any{
		"status":                 "ok",
		"source":                 source,
		"tracked_repo_count":     len(repos),
		"repo_counts":            repoCounts,
		"bridge_replay_cursor":   replayCursor,
		"atproto_event_cursor":   replayCursor,
		"event_log_head_cursor":  eventHeadCursor,
		"legacy_firehose_cursor": legacyCursor,
		"relay_source_cursor":    relaySourceCursor,
	})
}

func (h *UIHandler) handleATProtoSource(w http.ResponseWriter, r *http.Request) {
	store := h.requireATProtoStore(w)
	if store == nil {
		return
	}

	source, err := store.GetATProtoSource(r.Context(), defaultATProtoSourceKey)
	if err != nil {
		h.writeInternalError(w, "handleATProtoSource", "Failed to load ATProto source state", err)
		return
	}
	if source == nil {
		http.Error(w, "ATProto source not found", http.StatusNotFound)
		return
	}
	h.writeJSON(w, http.StatusOK, source)
}

func (h *UIHandler) handleATProtoRepos(w http.ResponseWriter, r *http.Request) {
	store := h.requireATProtoStore(w)
	if store == nil {
		return
	}

	state := strings.TrimSpace(r.URL.Query().Get("state"))
	repos, err := store.ListTrackedATProtoRepos(r.Context(), state)
	if err != nil {
		h.writeInternalError(w, "handleATProtoRepos", "Failed to list tracked repos", err)
		return
	}

	h.writeJSON(w, http.StatusOK, map[string]any{
		"state": state,
		"repos": repos,
		"count": len(repos),
	})
}

func (h *UIHandler) handleATProtoRepo(w http.ResponseWriter, r *http.Request) {
	service := h.requireATProtoService(w)
	if service == nil {
		return
	}

	did := strings.TrimSpace(r.URL.Query().Get("did"))
	if did == "" {
		http.Error(w, "missing did", http.StatusBadRequest)
		return
	}

	repo, err := service.GetRepoInfo(r.Context(), did)
	if err != nil {
		h.writeInternalError(w, "handleATProtoRepo", "Failed to get repo info", err)
		return
	}
	if repo == nil {
		http.Error(w, "repo not found", http.StatusNotFound)
		return
	}
	h.writeJSON(w, http.StatusOK, repo)
}

func (h *UIHandler) handleATProtoTrackRepo(w http.ResponseWriter, r *http.Request) {
	service := h.requireATProtoService(w)
	if service == nil {
		return
	}

	req, err := parseATProtoRepoActionRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := service.TrackRepo(r.Context(), req.DID, req.Reason); err != nil {
		h.writeInternalError(w, "handleATProtoTrackRepo", "Failed to track repo", err)
		return
	}

	repo, err := service.GetRepoInfo(r.Context(), req.DID)
	if err != nil {
		h.writeInternalError(w, "handleATProtoTrackRepo", "Failed to reload repo after track", err)
		return
	}
	h.writeJSON(w, http.StatusAccepted, map[string]any{
		"tracked": true,
		"repo":    repo,
	})
}

func (h *UIHandler) handleATProtoUntrackRepo(w http.ResponseWriter, r *http.Request) {
	service := h.requireATProtoService(w)
	if service == nil {
		return
	}

	req, err := parseATProtoRepoActionRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := service.UntrackRepo(r.Context(), req.DID); err != nil {
		h.writeInternalError(w, "handleATProtoUntrackRepo", "Failed to untrack repo", err)
		return
	}

	repo, err := service.GetRepoInfo(r.Context(), req.DID)
	if err != nil {
		h.writeInternalError(w, "handleATProtoUntrackRepo", "Failed to reload repo after untrack", err)
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{
		"tracked": false,
		"repo":    repo,
	})
}

func (h *UIHandler) handleATProtoRecord(w http.ResponseWriter, r *http.Request) {
	service := h.requireATProtoService(w)
	if service == nil {
		return
	}

	atURI := strings.TrimSpace(r.URL.Query().Get("at_uri"))
	if atURI == "" {
		atURI = strings.TrimSpace(r.URL.Query().Get("atURI"))
	}
	if atURI == "" {
		http.Error(w, "missing at_uri", http.StatusBadRequest)
		return
	}

	record, err := service.GetRecord(r.Context(), atURI)
	if err != nil {
		h.writeInternalError(w, "handleATProtoRecord", "Failed to get record", err)
		return
	}
	if record == nil {
		http.Error(w, "record not found", http.StatusNotFound)
		return
	}
	h.writeJSON(w, http.StatusOK, record)
}

func (h *UIHandler) handleATProtoRecords(w http.ResponseWriter, r *http.Request) {
	service := h.requireATProtoService(w)
	if service == nil {
		return
	}

	did := strings.TrimSpace(r.URL.Query().Get("did"))
	collection := strings.TrimSpace(r.URL.Query().Get("collection"))
	if did == "" || collection == "" {
		http.Error(w, "missing did or collection", http.StatusBadRequest)
		return
	}

	cursor := strings.TrimSpace(r.URL.Query().Get("cursor"))
	limit, err := parsePositiveIntQuery(r, "limit", 100, 1000)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	records, err := service.ListRecords(r.Context(), did, collection, cursor, limit)
	if err != nil {
		h.writeInternalError(w, "handleATProtoRecords", "Failed to list records", err)
		return
	}

	nextCursor := cursor
	if len(records) > 0 {
		nextCursor = records[len(records)-1].ATURI
	}

	h.writeJSON(w, http.StatusOK, map[string]any{
		"did":         did,
		"collection":  collection,
		"cursor":      cursor,
		"next_cursor": nextCursor,
		"records":     records,
		"count":       len(records),
	})
}

func (h *UIHandler) handleATProtoEvents(w http.ResponseWriter, r *http.Request) {
	store := h.requireATProtoStore(w)
	if store == nil {
		return
	}

	cursor, err := parseInt64Query(r, "cursor", 0)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	limit, err := parsePositiveIntQuery(r, "limit", 100, 1000)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	events, err := store.ListATProtoEventsAfter(r.Context(), cursor, limit)
	if err != nil {
		h.writeInternalError(w, "handleATProtoEvents", "Failed to list ATProto events", err)
		return
	}

	nextCursor := cursor
	if len(events) > 0 {
		nextCursor = events[len(events)-1].Cursor
	}

	h.writeJSON(w, http.StatusOK, map[string]any{
		"cursor":      cursor,
		"next_cursor": nextCursor,
		"events":      events,
		"count":       len(events),
	})
}

func (h *UIHandler) requireATProtoStore(w http.ResponseWriter) ATProtoDebugStore {
	if h.atprotoStore == nil {
		http.Error(w, "ATProto debug store not configured", http.StatusServiceUnavailable)
		return nil
	}
	return h.atprotoStore
}

func (h *UIHandler) requireATProtoService(w http.ResponseWriter) ATProtoService {
	if h.atprotoSvc == nil {
		http.Error(w, "ATProto service not configured", http.StatusServiceUnavailable)
		return nil
	}
	return h.atprotoSvc
}

func (h *UIHandler) writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		h.logger.Printf("event=handler_json_write_error err=%v", err)
	}
}

func parseATProtoRepoActionRequest(r *http.Request) (atprotoRepoActionRequest, error) {
	var req atprotoRepoActionRequest
	if strings.Contains(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			return req, fmt.Errorf("invalid json body: %w", err)
		}
	}
	if err := r.ParseForm(); err == nil {
		if strings.TrimSpace(req.DID) == "" {
			req.DID = strings.TrimSpace(r.FormValue("did"))
		}
		if strings.TrimSpace(req.Reason) == "" {
			req.Reason = strings.TrimSpace(r.FormValue("reason"))
		}
	}
	req.DID = strings.TrimSpace(req.DID)
	req.Reason = strings.TrimSpace(req.Reason)
	if req.DID == "" {
		return req, fmt.Errorf("missing did")
	}
	return req, nil
}

func parsePositiveIntQuery(r *http.Request, key string, defaultValue, maxValue int) (int, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return defaultValue, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid %s", key)
	}
	if value <= 0 {
		return 0, fmt.Errorf("%s must be positive", key)
	}
	if maxValue > 0 && value > maxValue {
		return maxValue, nil
	}
	return value, nil
}

func parseInt64Query(r *http.Request, key string, defaultValue int64) (int64, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return defaultValue, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid %s", key)
	}
	if value < 0 {
		return 0, fmt.Errorf("%s must be non-negative", key)
	}
	return value, nil
}
