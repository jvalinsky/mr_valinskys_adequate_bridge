package handlers

import (
	"encoding/base64"
	"net/http"
	"strings"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/db"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/web/templates"
)

func (h *UIHandler) handleConnections(w http.ResponseWriter, r *http.Request) {
	var peers []PeerStatus
	var ebtState map[string]map[string]int64
	if h.ssbStatus != nil {
		peers = h.ssbStatus.GetPeers()
		ebtState = h.ssbStatus.GetEBTState()
	}

	knownPeers, err := h.db.GetKnownPeers(r.Context())
	if err != nil {
		h.writeInternalError(w, "handleConnections", "failed to load known peers", err)
		return
	}

	tplPeers := make([]templates.PeerStatus, 0, len(peers))
	for _, p := range peers {
		tplPeers = append(tplPeers, templates.PeerStatus{
			Addr:       p.Addr,
			Feed:       p.Feed,
			ReadBytes:  p.ReadBytes,
			WriteBytes: p.WriteBytes,
			Latency:    p.Latency,
		})
	}

	tplKnown := make([]templates.KnownPeer, 0, len(knownPeers))
	for _, p := range knownPeers {
		tplKnown = append(tplKnown, templates.KnownPeer{
			Addr:      p.Addr,
			PubKey:    base64.StdEncoding.EncodeToString(p.PubKey),
			CreatedAt: p.CreatedAt,
		})
	}

	data := templates.ConnectionsData{
		Chrome: templates.PageChrome{
			ActiveNav: "connections",
			Breadcrumbs: []templates.Breadcrumb{
				{Label: "Admin", Href: "/"},
				{Label: "Connections"},
			},
		},
		Peers:      tplPeers,
		KnownPeers: tplKnown,
		EBTState:   ebtState,
	}

	if err := templates.RenderConnections(w, data); err != nil {
		h.writeInternalError(w, "handleConnections", "failed to render connections page", err)
	}
}

func (h *UIHandler) handleConnectionAdd(w http.ResponseWriter, r *http.Request) {
	addr := strings.TrimSpace(r.FormValue("addr"))
	pubkeyB64 := strings.TrimSpace(r.FormValue("pubkey"))

	if addr == "" || pubkeyB64 == "" {
		http.Error(w, "missing addr or pubkey", http.StatusBadRequest)
		return
	}

	// Pubkey should be base64
	pubkey, err := base64.StdEncoding.DecodeString(pubkeyB64)
	if err != nil {
		http.Error(w, "invalid pubkey base64", http.StatusBadRequest)
		return
	}

	if len(pubkey) != 32 {
		http.Error(w, "pubkey must be 32 bytes", http.StatusBadRequest)
		return
	}

	p := db.KnownPeer{
		Addr:   addr,
		PubKey: pubkey,
	}

	if err := h.db.AddKnownPeer(r.Context(), p); err != nil {
		h.writeInternalError(w, "handleConnectionAdd", "failed to save peer", err)
		return
	}

	http.Redirect(w, r, "/connections", http.StatusSeeOther)
}

func (h *UIHandler) handleConnectionConnect(w http.ResponseWriter, r *http.Request) {
	addr := strings.TrimSpace(r.FormValue("addr"))
	pubkeyB64 := strings.TrimSpace(r.FormValue("pubkey"))

	if addr == "" || pubkeyB64 == "" {
		http.Error(w, "missing addr or pubkey", http.StatusBadRequest)
		return
	}

	pubkey, err := base64.StdEncoding.DecodeString(pubkeyB64)
	if err != nil {
		http.Error(w, "invalid pubkey base64", http.StatusBadRequest)
		return
	}

	if h.ssbStatus == nil {
		http.Error(w, "ssb status provider not available", http.StatusServiceUnavailable)
		return
	}

	if err := h.ssbStatus.ConnectPeer(r.Context(), addr, pubkey); err != nil {
		h.writeInternalError(w, "handleConnectionConnect", "failed to connect to peer", err)
		return
	}

	http.Redirect(w, r, "/connections", http.StatusSeeOther)
}
