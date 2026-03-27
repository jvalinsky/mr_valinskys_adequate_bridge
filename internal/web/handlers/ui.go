package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/mr_valinskys_adequate_bridge/internal/db"
	"github.com/mr_valinskys_adequate_bridge/internal/web/templates"
)

type UIHandler struct {
	db     *db.DB
	active bool
}

func NewUIHandler(database *db.DB) *UIHandler {
	return &UIHandler{
		db:     database,
		active: false,
	}
}

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

	data := templates.DashboardData{
		AccountCount:        accountCount,
		MessageCount:        messageCount,
		PublishedCount:      publishedCount,
		PublishFailureCount: publishFailureCount,
		BlobCount:           blobCount,
		FirehoseCursor:      cursorValue,
		Active:              h.active,
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
			SSBMsgRef:       message.SSBMsgRef,
			PublishError:    message.PublishError,
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
			PublishError:    message.PublishError,
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

	w.Header().Set("Content-Type", "text/html")
	if err := templates.RenderState(w, templates.StateData{State: rows}); err != nil {
		http.Error(w, "Template error", http.StatusInternalServerError)
	}
}
