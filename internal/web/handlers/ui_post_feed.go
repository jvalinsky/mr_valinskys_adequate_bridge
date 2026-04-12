package handlers

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/web/templates"
	appbsky "github.com/jvalinsky/mr_valinskys_adequate_bridge/pkg/atproto/appbsky"
)

func (h *UIHandler) handlePost(w http.ResponseWriter, r *http.Request) {
	accounts, err := h.db.ListBridgedAccountsWithStats(r.Context())
	if err != nil {
		h.writeInternalError(w, "handlePost", "Failed to get accounts", err)
		return
	}
	h.logger.Printf("event=handle_post accounts=%d", len(accounts))

	data := templates.PostData{
		Chrome: templates.PageChrome{
			ActiveNav: "post",
			CSRFToken: csrfToken(r),
			Breadcrumbs: []templates.Breadcrumb{
				{Label: "Dashboard", Href: "/"},
				{Label: "Compose Post", Href: ""},
			},
		},
		Accounts:       mapAccountRows(accounts),
		PostingEnabled: h.atpClient != nil,
	}

	w.Header().Set("Content-Type", "text/html")
	if err := templates.RenderPost(w, data); err != nil {
		h.writeInternalError(w, "handlePost", "Template error", err)
	}
}

func (h *UIHandler) handlePostAction(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(10 << 20); err != nil { // 10MB limit
		http.Error(w, "Unable to parse form", http.StatusBadRequest)
		return
	}

	atDID := strings.TrimSpace(r.FormValue("at_did"))
	text := strings.TrimSpace(r.FormValue("text"))

	if atDID == "" {
		http.Error(w, "Author DID is required", http.StatusBadRequest)
		return
	}

	if text == "" {
		http.Error(w, "Message text is required", http.StatusBadRequest)
		return
	}

	account, err := h.db.GetBridgedAccount(r.Context(), atDID)
	if err != nil {
		h.writeInternalError(w, "handlePostAction", "Failed to get bridged account", err)
		return
	}
	if account == nil || !account.Active {
		http.Error(w, "Invalid or inactive account", http.StatusBadRequest)
		return
	}

	if h.atpClient == nil {
		http.Error(w, "ATProto posting is not configured on this bridge instance; please restart with --pds-host and --pds-password", http.StatusServiceUnavailable)
		return
	}

	var imageBlob *appbsky.LexBlob

	if len(r.MultipartForm.File["image"]) > 0 {
		fh := r.MultipartForm.File["image"][0]
		file, err := fh.Open()
		if err != nil {
			h.writeInternalError(w, "handlePostAction", "Failed to open uploaded file", err)
			return
		}
		defer file.Close()

		buffer := make([]byte, 512)
		n, err := io.ReadFull(file, buffer)
		if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
			h.writeInternalError(w, "handlePostAction", "Failed to read uploaded file", err)
			return
		}

		if _, err := file.Seek(0, io.SeekStart); err != nil {
			h.writeInternalError(w, "handlePostAction", "Failed to rewind uploaded file", err)
			return
		}
		mimeType := http.DetectContentType(buffer[:n])

		if !strings.HasPrefix(mimeType, "image/") {
			http.Error(w, "Uploaded file must be an image", http.StatusBadRequest)
			return
		}

		blob, err := h.atpClient.UploadBlob(r.Context(), atDID, file, mimeType)
		if err != nil {
			h.writeInternalError(w, "handlePostAction", "Failed to upload blob", err)
			return
		}

		imageBlob = blob
	}

	postURI, err := h.atpClient.CreatePost(r.Context(), atDID, text, imageBlob)
	if err != nil {
		h.writeInternalError(w, "handlePostAction", "Failed to create post", err)
		return
	}

	http.Redirect(w, r, fmt.Sprintf("/messages/detail?at_uri=%s", url.QueryEscape(postURI)), http.StatusSeeOther)
}

func (h *UIHandler) handleFeed(w http.ResponseWriter, r *http.Request) {
	limitStr := r.URL.Query().Get("limit")
	limit := 50
	if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
		limit = l
	}

	messages, err := h.db.ListPublishedMessagesGlobal(r.Context(), limit)
	if err != nil {
		h.writeInternalError(w, "handleFeed", "Failed to get global feed", err)
		return
	}

	rows := make([]templates.FeedRow, 0, len(messages))
	for _, msg := range messages {
		row := templates.FeedRow{
			ATURI:     msg.ATURI,
			ATDID:     msg.ATDID,
			Type:      msg.Type,
			CreatedAt: msg.CreatedAt,
			Text:      extractSSBText(msg.RawSSBJson),
			HasImage:  hasSSBImage(msg.RawSSBJson),
			ImageRef:  getSSBImageRef(msg.RawSSBJson),
		}
		if msg.RootATURI != "" {
			row.ThreadURL = fmt.Sprintf("/messages/thread?root_at_uri=%s", url.QueryEscape(msg.RootATURI))
		}
		rows = append(rows, row)
	}

	data := templates.FeedData{
		Chrome: templates.PageChrome{
			ActiveNav: "feed",
			CSRFToken: csrfToken(r),
			Breadcrumbs: []templates.Breadcrumb{
				{Label: "Dashboard", Href: "/"},
				{Label: "Global Feed", Href: ""},
			},
		},
		Feed: rows,
	}

	w.Header().Set("Content-Type", "text/html")
	if err := templates.RenderFeed(w, data); err != nil {
		h.writeInternalError(w, "handleFeed", "Template error", err)
	}
}
