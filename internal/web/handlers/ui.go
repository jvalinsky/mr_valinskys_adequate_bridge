// Package handlers wires HTTP routes for the bridge admin UI.
package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/mr_valinskys_adequate_bridge/internal/db"
	"github.com/mr_valinskys_adequate_bridge/internal/web/templates"
)

// UIHandler serves admin pages backed by bridge database state.
type UIHandler struct {
	db *db.DB
}

// NewUIHandler creates a UIHandler bound to database.
func NewUIHandler(database *db.DB) *UIHandler {
	return &UIHandler{
		db: database,
	}
}

// Mount registers admin UI routes on r.
func (h *UIHandler) Mount(r chi.Router) {
	r.Get("/", h.handleDashboard)
	r.Get("/accounts", h.handleAccounts)
	r.Get("/messages", h.handleMessages)
	r.Get("/failures", h.handleFailures)
	r.Get("/blobs", h.handleBlobs)
	r.Get("/state", h.handleState)
}

func (h *UIHandler) handleDashboard(w http.ResponseWriter, r *http.Request) {
	accountCount, err := h.db.CountBridgedAccounts(r.Context())
	if err != nil {
		http.Error(w, "Failed to get account count", http.StatusInternalServerError)
		return
	}

	messageCount, err := h.db.CountMessages(r.Context())
	if err != nil {
		http.Error(w, "Failed to get message count", http.StatusInternalServerError)
		return
	}

	publishedCount, err := h.db.CountPublishedMessages(r.Context())
	if err != nil {
		http.Error(w, "Failed to get published count", http.StatusInternalServerError)
		return
	}

	publishFailureCount, err := h.db.CountPublishFailures(r.Context())
	if err != nil {
		http.Error(w, "Failed to get failure count", http.StatusInternalServerError)
		return
	}
	deferredCount, err := h.db.CountDeferredMessages(r.Context())
	if err != nil {
		http.Error(w, "Failed to get deferred count", http.StatusInternalServerError)
		return
	}
	deletedCount, err := h.db.CountDeletedMessages(r.Context())
	if err != nil {
		http.Error(w, "Failed to get deleted count", http.StatusInternalServerError)
		return
	}

	blobCount, err := h.db.CountBlobs(r.Context())
	if err != nil {
		http.Error(w, "Failed to get blob count", http.StatusInternalServerError)
		return
	}

	cursorValue, _, err := h.db.GetBridgeState(r.Context(), "firehose_seq")
	if err != nil {
		http.Error(w, "Failed to get cursor state", http.StatusInternalServerError)
		return
	}
	bridgeStatus, ok, err := h.db.GetBridgeState(r.Context(), "bridge_runtime_status")
	if err != nil {
		http.Error(w, "Failed to get bridge status", http.StatusInternalServerError)
		return
	}
	if !ok || bridgeStatus == "" {
		bridgeStatus = "unknown"
	}
	lastHeartbeat, _, err := h.db.GetBridgeState(r.Context(), "bridge_runtime_last_heartbeat_at")
	if err != nil {
		http.Error(w, "Failed to get bridge heartbeat", http.StatusInternalServerError)
		return
	}

	data := templates.DashboardData{
		AccountCount:        accountCount,
		MessageCount:        messageCount,
		PublishedCount:      publishedCount,
		PublishFailureCount: publishFailureCount,
		DeferredCount:       deferredCount,
		DeletedCount:        deletedCount,
		BlobCount:           blobCount,
		FirehoseCursor:      cursorValue,
		BridgeStatus:        bridgeStatus,
		LastHeartbeat:       lastHeartbeat,
	}

	w.Header().Set("Content-Type", "text/html")
	if err := templates.RenderDashboard(w, data); err != nil {
		http.Error(w, "Template error", http.StatusInternalServerError)
	}
}

func (h *UIHandler) handleAccounts(w http.ResponseWriter, r *http.Request) {
	accounts, err := h.db.GetAllBridgedAccounts(r.Context())
	if err != nil {
		http.Error(w, "Failed to get accounts", http.StatusInternalServerError)
		return
	}

	rows := make([]templates.AccountRow, 0, len(accounts))
	for _, account := range accounts {
		rows = append(rows, templates.AccountRow{
			ATDID:     account.ATDID,
			SSBFeedID: account.SSBFeedID,
			Active:    account.Active,
			CreatedAt: account.CreatedAt,
		})
	}

	w.Header().Set("Content-Type", "text/html")
	if err := templates.RenderAccounts(w, templates.AccountsData{Accounts: rows}); err != nil {
		http.Error(w, "Template error", http.StatusInternalServerError)
	}
}

func (h *UIHandler) handleMessages(w http.ResponseWriter, r *http.Request) {
	messages, err := h.db.GetRecentMessages(r.Context(), 200)
	if err != nil {
		http.Error(w, "Failed to get messages", http.StatusInternalServerError)
		return
	}

	rows := make([]templates.MessageRow, 0, len(messages))
	for _, message := range messages {
		rows = append(rows, templates.MessageRow{
			ATURI:           message.ATURI,
			ATDID:           message.ATDID,
			Type:            message.Type,
			State:           message.MessageState,
			SSBMsgRef:       message.SSBMsgRef,
			PublishError:    message.PublishError,
			DeferReason:     message.DeferReason,
			PublishAttempts: message.PublishAttempts,
			CreatedAt:       message.CreatedAt,
		})
	}

	w.Header().Set("Content-Type", "text/html")
	if err := templates.RenderMessages(w, templates.MessagesData{Messages: rows}); err != nil {
		http.Error(w, "Template error", http.StatusInternalServerError)
	}
}

func (h *UIHandler) handleFailures(w http.ResponseWriter, r *http.Request) {
	messages, err := h.db.GetPublishFailures(r.Context(), 200)
	if err != nil {
		http.Error(w, "Failed to get publish failures", http.StatusInternalServerError)
		return
	}

	rows := make([]templates.FailureRow, 0, len(messages))
	for _, message := range messages {
		rows = append(rows, templates.FailureRow{
			ATURI:           message.ATURI,
			ATDID:           message.ATDID,
			Type:            message.Type,
			State:           message.MessageState,
			Reason:          issueReason(message),
			PublishAttempts: message.PublishAttempts,
			CreatedAt:       message.CreatedAt,
		})
	}

	w.Header().Set("Content-Type", "text/html")
	if err := templates.RenderFailures(w, templates.FailuresData{Failures: rows}); err != nil {
		http.Error(w, "Template error", http.StatusInternalServerError)
	}
}

func (h *UIHandler) handleBlobs(w http.ResponseWriter, r *http.Request) {
	blobs, err := h.db.GetRecentBlobs(r.Context(), 200)
	if err != nil {
		http.Error(w, "Failed to get blobs", http.StatusInternalServerError)
		return
	}

	rows := make([]templates.BlobRow, 0, len(blobs))
	for _, blob := range blobs {
		rows = append(rows, templates.BlobRow{
			ATCID:        blob.ATCID,
			SSBBlobRef:   blob.SSBBlobRef,
			Size:         blob.Size,
			MimeType:     blob.MimeType,
			DownloadedAt: blob.DownloadedAt,
		})
	}

	w.Header().Set("Content-Type", "text/html")
	if err := templates.RenderBlobs(w, templates.BlobsData{Blobs: rows}); err != nil {
		http.Error(w, "Template error", http.StatusInternalServerError)
	}
}

func (h *UIHandler) handleState(w http.ResponseWriter, r *http.Request) {
	state, err := h.db.GetAllBridgeState(r.Context())
	if err != nil {
		http.Error(w, "Failed to get bridge state", http.StatusInternalServerError)
		return
	}

	rows := make([]templates.StateRow, 0, len(state))
	for _, s := range state {
		rows = append(rows, templates.StateRow{
			Key:       s.Key,
			Value:     s.Value,
			UpdatedAt: s.UpdatedAt,
		})
	}

	deferredCount, err := h.db.CountDeferredMessages(r.Context())
	if err != nil {
		http.Error(w, "Failed to get deferred count", http.StatusInternalServerError)
		return
	}
	deletedCount, err := h.db.CountDeletedMessages(r.Context())
	if err != nil {
		http.Error(w, "Failed to get deleted count", http.StatusInternalServerError)
		return
	}
	latestReason, _, err := h.db.GetLatestDeferredReason(r.Context())
	if err != nil {
		http.Error(w, "Failed to get latest deferred reason", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	if err := templates.RenderState(w, templates.StateData{
		State:             rows,
		DeferredCount:     deferredCount,
		DeletedCount:      deletedCount,
		LatestDeferReason: latestReason,
	}); err != nil {
		http.Error(w, "Template error", http.StatusInternalServerError)
	}
}

func issueReason(message db.Message) string {
	if message.MessageState == db.MessageStateDeferred && message.DeferReason != "" {
		return message.DeferReason
	}
	return message.PublishError
}
