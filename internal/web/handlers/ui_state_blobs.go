package handlers

import (
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/web/templates"
)

func (h *UIHandler) handleBlobs(w http.ResponseWriter, r *http.Request) {
	blobs, err := h.db.GetRecentBlobs(r.Context(), 200)
	if err != nil {
		h.writeInternalError(w, "blobs", "Failed to get blobs", err)
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
	if err := templates.RenderBlobs(w, templates.BlobsData{
		Chrome: templates.PageChrome{
			ActiveNav: "blobs",
			CSRFToken: csrfToken(r),
			Breadcrumbs: []templates.Breadcrumb{
				{Label: "Dashboard", Href: "/"},
				{Label: "Blobs", Href: ""},
			},
		},
		Blobs: rows,
	}); err != nil {
		h.writeInternalError(w, "blobs", "Template error", err)
	}
}

func (h *UIHandler) handleBlobView(w http.ResponseWriter, r *http.Request) {
	refStr := r.URL.Query().Get("ref")
	if refStr == "" {
		http.Error(w, "Missing ref parameter", http.StatusBadRequest)
		return
	}

	blobMeta, err := h.db.GetBlobBySSBRef(r.Context(), refStr)
	if err != nil {
		h.writeInternalError(w, "blob_view", "Failed to lookup blob metadata", err)
		return
	}
	if blobMeta == nil {
		http.Error(w, "Blob metadata not found", http.StatusNotFound)
		return
	}

	if h.blobStore == nil {
		http.Error(w, "Blob store not configured", http.StatusServiceUnavailable)
		return
	}

	ref, err := refs.ParseBlobRef(refStr)
	if err != nil {
		http.Error(w, "Invalid SSB blob reference", http.StatusBadRequest)
		return
	}

	rc, err := h.blobStore.Get(ref.Hash())
	if err != nil {
		http.Error(w, "Blob data not found in store", http.StatusNotFound)
		return
	}
	defer rc.Close()

	if blobMeta.MimeType != "" {
		w.Header().Set("Content-Type", blobMeta.MimeType)
	} else {
		w.Header().Set("Content-Type", "application/octet-stream")
	}
	w.Header().Set("Content-Length", strconv.FormatInt(blobMeta.Size, 10))
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	w.Header().Set("Vary", "Authorization, Cookie")

	if _, err := io.Copy(w, rc); err != nil {
		h.logger.Printf("event=blob_serve_error ref=%s error=%v", refStr, err)
	}
}

func (h *UIHandler) handleState(w http.ResponseWriter, r *http.Request) {
	state, err := h.db.GetAllBridgeState(r.Context())
	if err != nil {
		h.writeInternalError(w, "state", "Failed to get bridge state", err)
		return
	}

	runtimeRows := make([]templates.StateRow, 0)
	firehoseRows := make([]templates.StateRow, 0)
	otherRows := make([]templates.StateRow, 0)
	heartbeatValue := ""

	for _, s := range state {
		row := templates.StateRow{Key: s.Key, Value: s.Value, UpdatedAt: s.UpdatedAt}
		switch {
		case strings.HasPrefix(s.Key, "bridge_runtime_"):
			runtimeRows = append(runtimeRows, row)
			if s.Key == "bridge_runtime_last_heartbeat_at" {
				heartbeatValue = s.Value
			}
		case strings.Contains(s.Key, "firehose") || strings.HasPrefix(s.Key, "atproto_"):
			firehoseRows = append(firehoseRows, row)
		default:
			otherRows = append(otherRows, row)
		}
	}

	deferredCount, err := h.db.CountDeferredMessages(r.Context())
	if err != nil {
		h.writeInternalError(w, "state", "Failed to get deferred count", err)
		return
	}

	deletedCount, err := h.db.CountDeletedMessages(r.Context())
	if err != nil {
		h.writeInternalError(w, "state", "Failed to get deleted count", err)
		return
	}

	latestReason, _, err := h.db.GetLatestDeferredReason(r.Context())
	if err != nil {
		h.writeInternalError(w, "state", "Failed to get latest deferred reason", err)
		return
	}

	heartbeatStale, heartbeatAge := heartbeatFreshness(heartbeatValue)
	statusTone := "success"
	statusTitle := "State health: runtime heartbeat fresh"
	statusBody := "Runtime and ATProto/firehose keys are grouped for faster incident inspection."
	if heartbeatStale {
		statusTone = "warning"
		statusTitle = "State health: heartbeat stale"
		statusBody = "Runtime heartbeat appears stale; verify bridge runtime and ATProto/firehose connectivity."
	}

	w.Header().Set("Content-Type", "text/html")
	if err := templates.RenderState(w, templates.StateData{
		Chrome: templates.PageChrome{
			ActiveNav: "state",
			CSRFToken: csrfToken(r),
			Breadcrumbs: []templates.Breadcrumb{
				{Label: "Dashboard", Href: "/"},
				{Label: "State", Href: ""},
			},
			Status: templates.PageStatus{
				Visible: true,
				Tone:    statusTone,
				Title:   statusTitle,
				Body:    statusBody,
			},
		},
		RuntimeState:      runtimeRows,
		FirehoseState:     firehoseRows,
		OtherState:        otherRows,
		DeferredCount:     deferredCount,
		DeletedCount:      deletedCount,
		LatestDeferReason: latestReason,
		HeartbeatStale:    heartbeatStale,
		HeartbeatAge:      heartbeatAge,
	}); err != nil {
		h.writeInternalError(w, "state", "Template error", err)
	}
}
