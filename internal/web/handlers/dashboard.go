// Package handlers wires HTTP routes for the bridge admin UI.
package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/web/templates"
)

func (h *UIHandler) handleDashboard(w http.ResponseWriter, r *http.Request) {
	accountCount, err := h.db.CountBridgedAccounts(r.Context())
	if err != nil {
		h.writeInternalError(w, "dashboard", "Failed to get account count", err)
		return
	}

	messageCount, err := h.db.CountMessages(r.Context())
	if err != nil {
		h.writeInternalError(w, "dashboard", "Failed to get message count", err)
		return
	}

	publishedCount, err := h.db.CountPublishedMessages(r.Context())
	if err != nil {
		h.writeInternalError(w, "dashboard", "Failed to get published count", err)
		return
	}

	publishFailureCount, err := h.db.CountPublishFailures(r.Context())
	if err != nil {
		h.writeInternalError(w, "dashboard", "Failed to get failure count", err)
		return
	}

	deferredCount, err := h.db.CountDeferredMessages(r.Context())
	if err != nil {
		h.writeInternalError(w, "dashboard", "Failed to get deferred count", err)
		return
	}

	deletedCount, err := h.db.CountDeletedMessages(r.Context())
	if err != nil {
		h.writeInternalError(w, "dashboard", "Failed to get deleted count", err)
		return
	}

	blobCount, err := h.db.CountBlobs(r.Context())
	if err != nil {
		h.writeInternalError(w, "dashboard", "Failed to get blob count", err)
		return
	}

	replayCursor, err := h.bridgeReplayCursor(r.Context())
	if err != nil {
		h.writeInternalError(w, "dashboard", "Failed to get cursor state", err)
		return
	}
	eventLogHeadCursor, err := h.eventLogHeadCursor(r.Context())
	if err != nil {
		h.writeInternalError(w, "dashboard", "Failed to get event-log cursor state", err)
		return
	}
	relaySourceCursor := ""
	source, err := h.db.GetATProtoSource(r.Context(), defaultATProtoSourceKey)
	if err != nil {
		h.writeInternalError(w, "dashboard", "Failed to get relay source cursor", err)
		return
	}
	if source != nil {
		relaySourceCursor = strconv.FormatInt(source.LastSeq, 10)
	}

	bridgeStatus, ok, err := h.db.GetBridgeState(r.Context(), "bridge_runtime_status")
	if err != nil {
		h.writeInternalError(w, "dashboard", "Failed to get bridge status", err)
		return
	}
	if !ok || strings.TrimSpace(bridgeStatus) == "" {
		bridgeStatus = "unknown"
	}

	lastHeartbeat, _, err := h.db.GetBridgeState(r.Context(), "bridge_runtime_last_heartbeat_at")
	if err != nil {
		h.writeInternalError(w, "dashboard", "Failed to get bridge heartbeat", err)
		return
	}

	healthLabel, healthDesc, healthTone := runtimeHealth(lastHeartbeat)

	reasonStats, err := h.db.ListTopDeferredReasons(r.Context(), 5)
	if err != nil {
		h.writeInternalError(w, "dashboard", "Failed to get deferred reason summary", err)
		return
	}
	topReasons := make([]templates.DeferredReasonView, 0, len(reasonStats))
	for _, stat := range reasonStats {
		topReasons = append(topReasons, templates.DeferredReasonView{
			Reason:      stat.Reason,
			Count:       stat.Count,
			MessagesURL: "/messages?state=deferred&q=" + url.QueryEscape(stat.Reason),
		})
	}

	issueAccounts, err := h.db.ListTopIssueAccounts(r.Context(), 5)
	if err != nil {
		h.writeInternalError(w, "dashboard", "Failed to get account issue summary", err)
		return
	}
	topAccounts := make([]templates.IssueAccountView, 0, len(issueAccounts))
	for _, acc := range issueAccounts {
		topAccounts = append(topAccounts, templates.IssueAccountView{
			ATDID:          acc.ATDID,
			Active:         acc.Active,
			TotalMessages:  acc.TotalMessages,
			IssueMessages:  acc.IssueMessages,
			FailedMessages: acc.FailedMessages,
			DeferredCount:  acc.DeferredCount,
			DeletedCount:   acc.DeletedCount,
			MessagesURL:    "/messages?did=" + url.QueryEscape(acc.ATDID) + "&has_issue=1",
		})
	}

	metrics := []templates.DashboardMetric{
		{ID: "accountCount", Label: "Bridged Accounts", Value: accountCount, Tone: "neutral", Href: "/accounts", Note: "Open account roster"},
		{ID: "messageCount", Label: "Messages Bridged", Value: messageCount, Tone: "neutral", Href: "/messages", Note: "Browse stream"},
		{ID: "publishedCount", Label: "Messages Published", Value: publishedCount, Tone: "success", Href: "/messages?state=published", Note: "Published state"},
		{ID: "publishFailureCount", Label: "Publish Failures", Value: publishFailureCount, Tone: "danger", Href: "/failures", Note: "Failed rows"},
		{ID: "deferredCount", Label: "Messages Deferred", Value: deferredCount, Tone: "warning", Href: "/messages?state=deferred", Note: "Deferred rows"},
		{ID: "deletedCount", Label: "Messages Deleted", Value: deletedCount, Tone: "neutral", Href: "/messages?state=deleted", Note: "Deleted tombstones"},
		{ID: "blobCount", Label: "Blobs Bridged", Value: blobCount, Tone: "neutral", Href: "/blobs", Note: "Blob mappings"},
	}

	data := templates.DashboardData{
		Chrome: templates.PageChrome{
			ActiveNav: "dashboard",
			Breadcrumbs: []templates.Breadcrumb{
				{Label: "Dashboard", Href: ""},
			},
			Status: templates.PageStatus{
				Visible: true,
				Tone:    healthTone,
				Title:   "Runtime health: " + healthLabel,
				Body:    healthDesc,
			},
		},
		Metrics:                  metrics,
		BridgeStatus:             bridgeStatus,
		LastHeartbeat:            lastHeartbeat,
		BridgeReplayCursor:       replayCursor,
		RelaySourceCursor:        relaySourceCursor,
		EventLogHeadCursor:       eventLogHeadCursor,
		RuntimeHealth:            healthLabel,
		RuntimeHealthDescription: healthDesc,
		TopDeferredReasons:       topReasons,
		TopIssueAccounts:         topAccounts,
	}

	w.Header().Set("Content-Type", "text/html")
	if err := templates.RenderDashboard(w, data); err != nil {
		h.writeInternalError(w, "dashboard", "Template error", err)
	}
}

func (h *UIHandler) handleEvents(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	// Send an initial payload immediately
	h.sendDashboardStats(w, r, flusher)

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			h.sendDashboardStats(w, r, flusher)
		}
	}
}

func (h *UIHandler) sendDashboardStats(w http.ResponseWriter, r *http.Request, flusher http.Flusher) {
	ctx := r.Context()
	
	accountCount, _ := h.db.CountBridgedAccounts(ctx)
	messageCount, _ := h.db.CountMessages(ctx)
	publishedCount, _ := h.db.CountPublishedMessages(ctx)
	publishFailureCount, _ := h.db.CountPublishFailures(ctx)
	deferredCount, _ := h.db.CountDeferredMessages(ctx)
	deletedCount, _ := h.db.CountDeletedMessages(ctx)
	blobCount, _ := h.db.CountBlobs(ctx)

	// Since SSE data needs to be lightweight, we can just render it as JSON payload
	// or simple strings. Here we'll send a JSON object with these counts.
	// You could use full views, but simple fields is usually enough.

	stats := map[string]interface{}{
		"accountCount": accountCount,
		"messageCount": messageCount,
		"publishedCount": publishedCount,
		"publishFailureCount": publishFailureCount,
		"deferredCount": deferredCount,
		"deletedCount": deletedCount,
		"blobCount": blobCount,
	}
	
	// Print as an SSE event containing JSON
	jsonBytes, err := json.Marshal(stats)
	if err == nil {
		fmt.Fprintf(w, "data: %s\n\n", string(jsonBytes))
		flusher.Flush()
	}
}
