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
	// Additional routes for /accounts and /messages would go here
}

func (h *UIHandler) handleDashboard(w http.ResponseWriter, r *http.Request) {
	accounts, err := h.db.GetAllBridgedAccounts(r.Context())
	if err != nil {
		http.Error(w, "Failed to get accounts", http.StatusInternalServerError)
		return
	}

	data := templates.DashboardData{
		AccountCount: len(accounts),
		MessageCount: 0, // Would query from DB in real implementation
		Active:       h.active,
	}

	w.Header().Set("Content-Type", "text/html")
	if err := templates.RenderDashboard(w, data); err != nil {
		http.Error(w, "Template error", http.StatusInternalServerError)
	}
}
