package handlers

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/web/templates"
)

func (h *UIHandler) handleRoomOverview(w http.ResponseWriter, r *http.Request) {
	overview := h.loadRoomOverview(r)
	data := templates.RoomOverviewData{
		Chrome: templates.PageChrome{
			ActiveNav: "room",
			CSRFToken: csrfToken(r),
			Breadcrumbs: []templates.Breadcrumb{
				{Label: "Dashboard", Href: "/"},
				{Label: "Room", Href: ""},
			},
		},
		Section:           "overview",
		Available:         overview.Available,
		DegradedReason:    overview.DegradedReason,
		ModeLabel:         overview.ModeLabel,
		ModeSummary:       overview.ModeSummary,
		PolicyHint:        overview.PolicyHint,
		OperatorRole:      roomRoleValue(overview.OperatorRole),
		MembersCount:      overview.MembersCount,
		InvitesActive:     overview.InvitesActive,
		InvitesTotal:      overview.InvitesTotal,
		AliasesCount:      overview.AliasesCount,
		DeniedCount:       overview.DeniedCount,
		AttendantsActive:  overview.AttendantsActive,
		AttendantsTotal:   overview.AttendantsTotal,
		TunnelsActive:     overview.TunnelEndpointsActive,
		TunnelsTotal:      overview.TunnelEndpointsTotal,
		HealthStatus:      overview.HealthStatus,
		HealthDetail:      overview.HealthDetail,
		StatusEndpointURL: overview.StatusSourceURL,
		ActionMessage:     strings.TrimSpace(r.URL.Query().Get("message")),
		ActionError:       strings.TrimSpace(r.URL.Query().Get("error")),
	}

	w.Header().Set("Content-Type", "text/html")
	if err := templates.RenderRoomOverview(w, data); err != nil {
		h.writeInternalError(w, "room_overview", "Template error", err)
	}
}

func (h *UIHandler) handleRoomMembersRoles(w http.ResponseWriter, r *http.Request) {
	overview := h.loadRoomOverview(r)
	rows := make([]templates.RoomMemberRow, 0)
	if overview.Available {
		listed, err := h.roomOps.MembersList(r.Context())
		if err != nil {
			overview.DegradedReason = "Failed to list members: " + err.Error()
			overview.Available = false
		} else {
			rows = listed
		}
	}

	data := templates.RoomMembersRolesData{
		Chrome: templates.PageChrome{
			ActiveNav: "room",
			CSRFToken: csrfToken(r),
			Breadcrumbs: []templates.Breadcrumb{
				{Label: "Dashboard", Href: "/"},
				{Label: "Room", Href: "/room"},
				{Label: "Members & Roles", Href: ""},
			},
		},
		Section:        "members",
		Available:      overview.Available,
		DegradedReason: overview.DegradedReason,
		ModeLabel:      overview.ModeLabel,
		ModeSummary:    overview.ModeSummary,
		PolicyHint:     overview.PolicyHint,
		OperatorRole:   roomRoleValue(overview.OperatorRole),
		Members:        rows,
		ActionMessage:  strings.TrimSpace(r.URL.Query().Get("message")),
		ActionError:    strings.TrimSpace(r.URL.Query().Get("error")),
	}

	w.Header().Set("Content-Type", "text/html")
	if err := templates.RenderRoomMembersRoles(w, data); err != nil {
		h.writeInternalError(w, "room_members", "Template error", err)
	}
}

func (h *UIHandler) handleRoomAttendantsTunnels(w http.ResponseWriter, r *http.Request) {
	overview := h.loadRoomOverview(r)
	attRows := make([]templates.RoomAttendantRow, 0)
	tunnelRows := make([]templates.RoomTunnelEndpointRow, 0)
	if overview.Available {
		attendants, err := h.roomOps.AttendantsSnapshot(r.Context())
		if err != nil {
			overview.Available = false
			overview.DegradedReason = "Failed to load attendants snapshot: " + err.Error()
		} else {
			attRows = attendants
		}
		if overview.Available {
			tunnels, err := h.roomOps.TunnelEndpointsSnapshot(r.Context())
			if err != nil {
				overview.Available = false
				overview.DegradedReason = "Failed to load tunnel endpoints snapshot: " + err.Error()
			} else {
				tunnelRows = tunnels
			}
		}
	}

	data := templates.RoomAttendantsTunnelsData{
		Chrome: templates.PageChrome{
			ActiveNav: "room",
			CSRFToken: csrfToken(r),
			Breadcrumbs: []templates.Breadcrumb{
				{Label: "Dashboard", Href: "/"},
				{Label: "Room", Href: "/room"},
				{Label: "Attendants & Tunnels", Href: ""},
			},
		},
		Section:        "attendants",
		Available:      overview.Available,
		DegradedReason: overview.DegradedReason,
		ModeLabel:      overview.ModeLabel,
		ModeSummary:    overview.ModeSummary,
		PolicyHint:     overview.PolicyHint,
		OperatorRole:   roomRoleValue(overview.OperatorRole),
		ConnectionsURL: "/connections",
		Attendants:     attRows,
		Tunnels:        tunnelRows,
		ActionMessage:  strings.TrimSpace(r.URL.Query().Get("message")),
		ActionError:    strings.TrimSpace(r.URL.Query().Get("error")),
	}

	w.Header().Set("Content-Type", "text/html")
	if err := templates.RenderRoomAttendantsTunnels(w, data); err != nil {
		h.writeInternalError(w, "room_attendants", "Template error", err)
	}
}

func (h *UIHandler) handleRoomAliasesInvites(w http.ResponseWriter, r *http.Request) {
	overview := h.loadRoomOverview(r)
	inviteRows := make([]templates.RoomInviteRow, 0)
	aliasRows := make([]templates.RoomAliasRow, 0)
	inviteJoinURL := strings.TrimSpace(r.URL.Query().Get("invite_url"))
	if overview.Available {
		invites, err := h.roomOps.InvitesList(r.Context())
		if err != nil {
			overview.Available = false
			overview.DegradedReason = "Failed to list invites: " + err.Error()
		} else {
			inviteRows = invites
		}
		if overview.Available {
			aliases, err := h.roomOps.AliasesList(r.Context())
			if err != nil {
				overview.Available = false
				overview.DegradedReason = "Failed to list aliases: " + err.Error()
			} else {
				aliasRows = aliases
			}
		}
	}

	data := templates.RoomAliasesInvitesData{
		Chrome: templates.PageChrome{
			ActiveNav: "room",
			CSRFToken: csrfToken(r),
			Breadcrumbs: []templates.Breadcrumb{
				{Label: "Dashboard", Href: "/"},
				{Label: "Room", Href: "/room"},
				{Label: "Aliases & Invites", Href: ""},
			},
		},
		Section:         "aliases",
		Available:       overview.Available,
		DegradedReason:  overview.DegradedReason,
		ModeLabel:       overview.ModeLabel,
		ModeSummary:     overview.ModeSummary,
		PolicyHint:      overview.PolicyHint,
		OperatorRole:    roomRoleValue(overview.OperatorRole),
		Invites:         inviteRows,
		Aliases:         aliasRows,
		CanCreateInvite: canCreateInvite(overview.Mode, overview.OperatorRole),
		CanRevokeInvite: canRevokeInvite(overview.Mode, overview.OperatorRole),
		CanRevokeAlias:  canRevokeAlias(overview.Mode, overview.OperatorRole),
		InviteJoinURL:   inviteJoinURL,
		ActionMessage:   strings.TrimSpace(r.URL.Query().Get("message")),
		ActionError:     strings.TrimSpace(r.URL.Query().Get("error")),
	}

	w.Header().Set("Content-Type", "text/html")
	if err := templates.RenderRoomAliasesInvites(w, data); err != nil {
		h.writeInternalError(w, "room_aliases", "Template error", err)
	}
}

func (h *UIHandler) handleRoomModeration(w http.ResponseWriter, r *http.Request) {
	overview := h.loadRoomOverview(r)
	deniedRows := make([]templates.RoomDeniedKeyRow, 0)
	if overview.Available {
		rows, err := h.roomOps.DeniedList(r.Context())
		if err != nil {
			overview.Available = false
			overview.DegradedReason = "Failed to list denied keys: " + err.Error()
		} else {
			deniedRows = rows
		}
	}

	data := templates.RoomModerationData{
		Chrome: templates.PageChrome{
			ActiveNav: "room",
			CSRFToken: csrfToken(r),
			Breadcrumbs: []templates.Breadcrumb{
				{Label: "Dashboard", Href: "/"},
				{Label: "Room", Href: "/room"},
				{Label: "Moderation", Href: ""},
			},
		},
		Section:         "moderation",
		Available:       overview.Available,
		DegradedReason:  overview.DegradedReason,
		ModeLabel:       overview.ModeLabel,
		ModeSummary:     overview.ModeSummary,
		PolicyHint:      overview.PolicyHint,
		OperatorRole:    roomRoleValue(overview.OperatorRole),
		CanMutateDenied: canMutateDenied(overview.Mode, overview.OperatorRole),
		Denied:          deniedRows,
		ActionMessage:   strings.TrimSpace(r.URL.Query().Get("message")),
		ActionError:     strings.TrimSpace(r.URL.Query().Get("error")),
	}

	w.Header().Set("Content-Type", "text/html")
	if err := templates.RenderRoomModeration(w, data); err != nil {
		h.writeInternalError(w, "room_moderation", "Template error", err)
	}
}

func (h *UIHandler) handleRoomMemberRoleSet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.roomOps == nil {
		redirectRoomStatus(w, r, "/room/members", "", "Room provider unavailable")
		return
	}

	memberID, err := parseInt64FormValue(r.FormValue("member_id"))
	if err != nil {
		redirectRoomStatus(w, r, "/room/members", "", err.Error())
		return
	}
	role, err := parseRoomMemberRole(r.FormValue("role"))
	if err != nil {
		redirectRoomStatus(w, r, "/room/members", "", err.Error())
		return
	}

	if err := h.roomOps.MemberRoleSet(r.Context(), memberID, role); err != nil {
		redirectRoomStatus(w, r, "/room/members", "", err.Error())
		return
	}
	redirectRoomStatus(w, r, "/room/members", "Member role updated", "")
}

func (h *UIHandler) handleRoomMemberRemove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.roomOps == nil {
		redirectRoomStatus(w, r, "/room/members", "", "Room provider unavailable")
		return
	}

	memberID, err := parseInt64FormValue(r.FormValue("member_id"))
	if err != nil {
		redirectRoomStatus(w, r, "/room/members", "", err.Error())
		return
	}

	if err := h.roomOps.MemberRemove(r.Context(), memberID); err != nil {
		redirectRoomStatus(w, r, "/room/members", "", err.Error())
		return
	}
	redirectRoomStatus(w, r, "/room/members", "Member removed", "")
}

func (h *UIHandler) handleRoomInviteCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.roomOps == nil {
		redirectRoomStatus(w, r, "/room/aliases", "", "Room provider unavailable")
		return
	}

	token, err := h.roomOps.InviteCreate(r.Context(), 0)
	if err != nil {
		redirectRoomStatus(w, r, "/room/aliases", "", err.Error())
		return
	}

	joinURL := h.roomOps.JoinURL(token)
	target := "/room/aliases?message=" + url.QueryEscape("Invite created") + "&invite_url=" + url.QueryEscape(joinURL)
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func (h *UIHandler) handleRoomInviteRevoke(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.roomOps == nil {
		redirectRoomStatus(w, r, "/room/aliases", "", "Room provider unavailable")
		return
	}

	inviteID, err := parseInt64FormValue(r.FormValue("invite_id"))
	if err != nil {
		inviteID, err = parseInt64FormValue(r.FormValue("id"))
		if err != nil {
			redirectRoomStatus(w, r, "/room/aliases", "", err.Error())
			return
		}
	}

	if err := h.roomOps.InviteRevoke(r.Context(), inviteID); err != nil {
		redirectRoomStatus(w, r, "/room/aliases", "", err.Error())
		return
	}
	redirectRoomStatus(w, r, "/room/aliases", "Invite revoked", "")
}

func (h *UIHandler) handleRoomAliasRevoke(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.roomOps == nil {
		redirectRoomStatus(w, r, "/room/aliases", "", "Room provider unavailable")
		return
	}

	alias := strings.TrimSpace(r.FormValue("alias"))
	if alias == "" {
		redirectRoomStatus(w, r, "/room/aliases", "", "alias is required")
		return
	}

	if err := h.roomOps.AliasRevoke(r.Context(), alias); err != nil {
		redirectRoomStatus(w, r, "/room/aliases", "", err.Error())
		return
	}
	redirectRoomStatus(w, r, "/room/aliases", fmt.Sprintf("Alias %q revoked", alias), "")
}

func (h *UIHandler) handleRoomDeniedAdd(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.roomOps == nil {
		redirectRoomStatus(w, r, "/room/moderation", "", "Room provider unavailable")
		return
	}

	feedRaw := strings.TrimSpace(r.FormValue("feed_id"))
	feed, err := refs.ParseFeedRef(feedRaw)
	if err != nil {
		redirectRoomStatus(w, r, "/room/moderation", "", "invalid feed id")
		return
	}

	comment := strings.TrimSpace(r.FormValue("comment"))
	if err := h.roomOps.DeniedAdd(r.Context(), *feed, comment); err != nil {
		redirectRoomStatus(w, r, "/room/moderation", "", err.Error())
		return
	}
	redirectRoomStatus(w, r, "/room/moderation", "Denied key added", "")
}

func (h *UIHandler) handleRoomDeniedRemove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.roomOps == nil {
		redirectRoomStatus(w, r, "/room/moderation", "", "Room provider unavailable")
		return
	}

	deniedID, err := parseInt64FormValue(r.FormValue("denied_id"))
	if err != nil {
		deniedID, err = parseInt64FormValue(r.FormValue("id"))
		if err != nil {
			redirectRoomStatus(w, r, "/room/moderation", "", err.Error())
			return
		}
	}

	if err := h.roomOps.DeniedRemove(r.Context(), deniedID); err != nil {
		redirectRoomStatus(w, r, "/room/moderation", "", err.Error())
		return
	}
	redirectRoomStatus(w, r, "/room/moderation", "Denied key removed", "")
}

func (h *UIHandler) loadRoomOverview(r *http.Request) RoomOverview {
	if h.roomOps == nil {
		return RoomOverview{
			Available:      false,
			DegradedReason: "Room data provider is not configured. Start serve-ui with --room-repo-path and optionally --room-http-base-url.",
			ModeLabel:      "Unknown",
			ModeSummary:    "Room mode could not be read.",
			OperatorRole:   0,
			PolicyHint:     "Room policy is unavailable while the room provider is not configured.",
			HealthStatus:   "degraded",
			HealthDetail:   "Room sqlite was not attached to this serve-ui process.",
		}
	}
	overview, err := h.roomOps.Overview(r.Context())
	if err != nil {
		return RoomOverview{
			Available:      false,
			DegradedReason: err.Error(),
			ModeLabel:      "Unknown",
			ModeSummary:    "Room mode could not be read.",
			OperatorRole:   0,
			PolicyHint:     "Room policy could not be evaluated.",
			HealthStatus:   "degraded",
			HealthDetail:   "Room overview query failed: " + err.Error(),
		}
	}
	return overview
}

func redirectRoomStatus(w http.ResponseWriter, r *http.Request, path, message, errMessage string) {
	vals := make(url.Values)
	if strings.TrimSpace(message) != "" {
		vals.Set("message", strings.TrimSpace(message))
	}
	if strings.TrimSpace(errMessage) != "" {
		vals.Set("error", strings.TrimSpace(errMessage))
	}
	target := path
	if len(vals) > 0 {
		target += "?" + vals.Encode()
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}
