// Package handlers wires HTTP routes for the bridge admin UI.
package handlers

import (
	"net/http"
	"strings"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/db"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/web/templates"
)

func (h *UIHandler) handleAccounts(w http.ResponseWriter, r *http.Request) {
	accounts, err := h.db.ListBridgedAccountsWithStats(r.Context())
	if err != nil {
		h.writeInternalError(w, "accounts", "Failed to get accounts", err)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	if err := templates.RenderAccounts(w, templates.AccountsData{
		Chrome: templates.PageChrome{
			ActiveNav: "accounts",
			Breadcrumbs: []templates.Breadcrumb{
				{Label: "Dashboard", Href: "/"},
				{Label: "Accounts", Href: ""},
			},
		},
		Accounts: mapAccountRows(accounts),
	}); err != nil {
		h.writeInternalError(w, "accounts", "Template error", err)
	}
}

func (h *UIHandler) handleAccountsAdd(w http.ResponseWriter, r *http.Request) {
	atDID := strings.TrimSpace(r.FormValue("at_did"))

	if atDID == "" {
		http.Error(w, "Missing at_did", http.StatusBadRequest)
		return
	}

	if h.feedIDProvider == nil {
		h.writeInternalError(w, "accounts_add", "Feed ID provider not configured", nil)
		return
	}

	feedRef, err := h.feedIDProvider.GetFeedID(atDID)
	if err != nil {
		h.writeInternalError(w, "accounts_add", "Failed to derive feed ID", err)
		return
	}

	acc := db.BridgedAccount{
		ATDID:     atDID,
		SSBFeedID: feedRef.Ref(),
		Active:    true,
	}

	if err := h.db.AddBridgedAccount(r.Context(), acc); err != nil {
		h.writeInternalError(w, "accounts_add", "Failed to add account", err)
		return
	}

	// Proactively trigger tracking if service is available
	if h.atprotoSvc != nil {
		if err := h.atprotoSvc.TrackRepo(r.Context(), atDID, "admin_ui_add"); err != nil {
			h.logger.Printf("event=accounts_add_track_failed did=%s err=%v", atDID, err)
		}
	}

	http.Redirect(w, r, "/accounts", http.StatusSeeOther)
}

func (h *UIHandler) handleAccountsBackfill(w http.ResponseWriter, r *http.Request) {
	atDID := strings.TrimSpace(r.URL.Query().Get("at_did"))
	if atDID == "" {
		http.Error(w, "Missing at_did", http.StatusBadRequest)
		return
	}

	if h.atprotoSvc == nil {
		http.Error(w, "Indexing service not available", http.StatusServiceUnavailable)
		return
	}

	if err := h.atprotoSvc.TrackRepo(r.Context(), atDID, "admin_ui_manual"); err != nil {
		h.writeInternalError(w, "accounts_backfill", "Failed to track repo", err)
		return
	}

	if err := h.atprotoSvc.RequestResync(r.Context(), atDID, "admin_ui_manual"); err != nil {
		h.writeInternalError(w, "accounts_backfill", "Failed to request resync", err)
		return
	}

	http.Redirect(w, r, "/accounts", http.StatusSeeOther)
}

func (h *UIHandler) handleAccountsRemove(w http.ResponseWriter, r *http.Request) {
	atDID := strings.TrimSpace(r.FormValue("at_did"))
	if atDID == "" {
		atDID = strings.TrimSpace(r.URL.Query().Get("at_did"))
	}
	if atDID == "" {
		http.Error(w, "Missing at_did", http.StatusBadRequest)
		return
	}

	if err := h.db.RemoveBridgedAccount(r.Context(), atDID); err != nil {
		h.writeInternalError(w, "accounts_remove", "Failed to remove account", err)
		return
	}

	http.Redirect(w, r, "/accounts", http.StatusSeeOther)
}
