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
	ssbFeedID := strings.TrimSpace(r.FormValue("ssb_feed_id"))

	if atDID == "" || ssbFeedID == "" {
		http.Error(w, "Missing at_did or ssb_feed_id", http.StatusBadRequest)
		return
	}

	acc := db.BridgedAccount{
		ATDID:     atDID,
		SSBFeedID: ssbFeedID,
		Active:    true,
	}

	if err := h.db.AddBridgedAccount(r.Context(), acc); err != nil {
		h.writeInternalError(w, "accounts_add", "Failed to add account", err)
		return
	}

	http.Redirect(w, r, "/accounts", http.StatusSeeOther)
}
