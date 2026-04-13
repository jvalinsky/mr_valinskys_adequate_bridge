// Package handlers wires HTTP routes for the bridge admin UI.
package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/web/templates"
)

func (h *UIHandler) fetchDashboardData(ctx context.Context, r *http.Request) (templates.DashboardData, error) {
	var data templates.DashboardData

	accountCount, err := h.db.CountBridgedAccounts(ctx)
	if err != nil {
		return data, fmt.Errorf("Failed to get account count: %w", err)
	}

	messageCount, err := h.db.CountMessages(ctx)
	if err != nil {
		return data, fmt.Errorf("Failed to get message count: %w", err)
	}

	publishedCount, err := h.db.CountPublishedMessages(ctx)
	if err != nil {
		return data, fmt.Errorf("Failed to get published count: %w", err)
	}

	publishFailureCount, err := h.db.CountPublishFailures(ctx)
	if err != nil {
		return data, fmt.Errorf("Failed to get failure count: %w", err)
	}

	deferredCount, err := h.db.CountDeferredMessages(ctx)
	if err != nil {
		return data, fmt.Errorf("Failed to get deferred count: %w", err)
	}

	deletedCount, err := h.db.CountDeletedMessages(ctx)
	if err != nil {
		return data, fmt.Errorf("Failed to get deleted count: %w", err)
	}

	blobCount, err := h.db.CountBlobs(ctx)
	if err != nil {
		return data, fmt.Errorf("Failed to get blob count: %w", err)
	}

	replayCursor, err := h.bridgeReplayCursor(ctx)
	if err != nil {
		return data, fmt.Errorf("Failed to get cursor state: %w", err)
	}
	eventLogHeadCursor, err := h.eventLogHeadCursor(ctx)
	if err != nil {
		return data, fmt.Errorf("Failed to get event-log cursor state: %w", err)
	}
	relaySourceCursor := ""
	source, err := h.db.GetATProtoSource(ctx, defaultATProtoSourceKey)
	if err != nil {
		return data, fmt.Errorf("Failed to get relay source cursor: %w", err)
	}
	if source != nil {
		relaySourceCursor = strconv.FormatInt(source.LastSeq, 10)
	}

	bridgeStatus, ok, err := h.db.GetBridgeState(ctx, "bridge_runtime_status")
	if err != nil {
		return data, fmt.Errorf("Failed to get bridge status: %w", err)
	}
	if !ok || strings.TrimSpace(bridgeStatus) == "" {
		bridgeStatus = "unknown"
	}

	lastHeartbeat, _, err := h.db.GetBridgeState(ctx, "bridge_runtime_last_heartbeat_at")
	if err != nil {
		return data, fmt.Errorf("Failed to get bridge heartbeat: %w", err)
	}

	healthLabel, healthDesc, healthTone := runtimeHealth(lastHeartbeat)

	reasonStats, err := h.db.ListTopDeferredReasons(ctx, 5)
	if err != nil {
		return data, fmt.Errorf("Failed to get deferred reason summary: %w", err)
	}
	topReasons := make([]templates.DeferredReasonView, 0, len(reasonStats))
	for _, stat := range reasonStats {
		topReasons = append(topReasons, templates.DeferredReasonView{
			Reason:      stat.Reason,
			Count:       stat.Count,
			MessagesURL: "/messages?state=deferred&q=" + url.QueryEscape(stat.Reason),
		})
	}

	issueAccounts, err := h.db.ListTopIssueAccounts(ctx, 5)
	if err != nil {
		return data, fmt.Errorf("Failed to get account issue summary: %w", err)
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

	data = templates.DashboardData{
		Chrome: templates.PageChrome{
			ActiveNav: "dashboard",
			CSRFToken: "",
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

	// Always grab the real CSRF token if this is called in context of a real request
	if r != nil {
		data.Chrome.CSRFToken = csrfToken(r)
	}

	return data, nil
}

func (h *UIHandler) handleDashboard(w http.ResponseWriter, r *http.Request) {
	data, err := h.fetchDashboardData(r.Context(), r)
	if err != nil {
		h.writeInternalError(w, "dashboard", "Failed to compile dashboard data", err)
		return
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
	// We no longer need the request to pass to fetchDashboardData since we don't care
	// about CSRF token in the SSE stream itself.
	data, err := h.fetchDashboardData(r.Context(), nil)
	if err != nil {
		h.logger.Printf("SSE dashboard data error: %v", err)
		return
	}
	
	// Convert data directly (no longer limited to just metrics)
	jsonBytes, err := json.Marshal(data)
	if err == nil {
		fmt.Fprintf(w, "data: %s\n\n", string(jsonBytes))
		flusher.Flush()
	}
}
