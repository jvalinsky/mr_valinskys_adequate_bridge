package room

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/metrics"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/roomdb"
)

type inviteHandler struct {
	roomDB     roomdb.InvitesService
	members    roomdb.MembersService
	aliases    roomdb.AliasesService
	deniedKeys roomdb.DeniedKeysService
	authTokens roomdb.AuthWithSSBService
	config     roomdb.RoomConfig
	keyPair    inviteKeyPair
	domain     string
	muxrpcAddr string
}

type inviteKeyPair interface {
	FeedRef() refs.FeedRef
}

func newInviteHandler(
	roomDB roomdb.InvitesService,
	members roomdb.MembersService,
	aliases roomdb.AliasesService,
	deniedKeys roomdb.DeniedKeysService,
	authTokens roomdb.AuthWithSSBService,
	config roomdb.RoomConfig,
	keyPair inviteKeyPair,
	domain string,
	muxrpcAddr string,
) *inviteHandler {
	return &inviteHandler{
		roomDB:     roomDB,
		members:    members,
		aliases:    aliases,
		deniedKeys: deniedKeys,
		authTokens: authTokens,
		config:     config,
		keyPair:    keyPair,
		domain:     domain,
		muxrpcAddr: muxrpcAddr,
	}
}

type invitePageData struct {
	ShowInvitesNav bool
	InviteURL      string
	HomeURL        string
	SignInURL      string
}

type invitePolicyContext struct {
	mode          roomdb.PrivacyMode
	member        roomdb.Member
	authenticated bool
}

func (h *inviteHandler) handleCreateInvite(w http.ResponseWriter, r *http.Request) {
	authz, ok := h.authorizeInviteCreation(w, r)
	if !ok {
		return
	}

	if r.Method == http.MethodGet {
		h.serveInvitePage(w, r)
		return
	}

	if r.Method == http.MethodPost {
		h.handleCreateInviteSubmit(w, r, authz)
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

func (h *inviteHandler) serveInvitePage(w http.ResponseWriter, r *http.Request) {
	data := invitePageData{
		ShowInvitesNav: h.showInvitesNav(r),
		InviteURL:      "/create-invite",
		HomeURL:        "/",
		SignInURL:      "/login",
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if err := invitePageTemplate.Execute(w, data); err != nil {
		http.Error(w, "Template error", http.StatusInternalServerError)
	}
}

type createInviteAuth struct {
	member        roomdb.Member
	authenticated bool
}

func (h *inviteHandler) authorizeInviteCreation(w http.ResponseWriter, r *http.Request) (createInviteAuth, bool) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		return createInviteAuth{}, true
	}

	policyCtx, err := h.policyContext(r)
	if err != nil {
		h.writeInviteError(w, r, http.StatusInternalServerError, "Failed to check room mode")
		return createInviteAuth{}, false
	}

	if !h.canCreateInvite(policyCtx) {
		h.writeInviteCreateUnauthorized(w, r)
		return createInviteAuth{}, false
	}

	if policyCtx.authenticated {
		return createInviteAuth{member: policyCtx.member, authenticated: true}, true
	}
	return createInviteAuth{}, true
}

func (h *inviteHandler) writeInviteCreateUnauthorized(w http.ResponseWriter, r *http.Request) {
	h.writePolicyUnauthorized(w, r, "/create-invite", "invite creation requires an authenticated member role")
}

func (h *inviteHandler) handleCreateInviteSubmit(w http.ResponseWriter, r *http.Request, authz createInviteAuth) {
	createdBy := int64(0)
	if authz.authenticated {
		createdBy = authz.member.ID
	} else {
		creatorID, err := h.ensureRoomCreatorMemberID(r.Context())
		if err != nil {
			h.writeInviteError(w, r, http.StatusInternalServerError, "Failed to create invite")
			return
		}
		createdBy = creatorID
	}

	token, err := h.roomDB.Create(r.Context(), createdBy)
	if err != nil {
		h.writeInviteError(w, r, http.StatusInternalServerError, "Failed to create invite")
		return
	}

	inviteURL := fmt.Sprintf("%s/join?token=%s", h.externalBaseURL(r), url.QueryEscape(token))

	if wantsJSONResponse(r) {
		h.writeJSON(w, http.StatusOK, map[string]string{
			"url": inviteURL,
		})
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if err := inviteCreatedTemplate.Execute(w, map[string]string{"URL": inviteURL}); err != nil {
		http.Error(w, "Template error", http.StatusInternalServerError)
	}
}

func (h *inviteHandler) ensureRoomCreatorMemberID(ctx context.Context) (int64, error) {
	if h.members == nil {
		return 0, fmt.Errorf("members service unavailable")
	}
	if h.keyPair == nil {
		return 0, fmt.Errorf("room identity unavailable")
	}

	roomFeed := h.keyPair.FeedRef()
	member, err := h.members.GetByFeed(ctx, roomFeed)
	if err == nil {
		return member.ID, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}

	memberID, addErr := h.members.Add(ctx, roomFeed, roomdb.RoleAdmin)
	if addErr == nil {
		return memberID, nil
	}

	// Handle races on unique pub_key by re-reading after a failed insert.
	member, lookupErr := h.members.GetByFeed(ctx, roomFeed)
	if lookupErr == nil {
		return member.ID, nil
	}

	return 0, fmt.Errorf("add room identity member: %w", addErr)
}

func (h *inviteHandler) handleJoin(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		token := resolveInviteTokenQuery(r)
		if token == "" {
			http.NotFound(w, r)
			return
		}
		h.serveJoinPage(w, r, token)
		return
	}

	if r.Method == http.MethodPost {
		h.handleJoinSubmit(w, r, resolveInviteTokenQuery(r))
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

func (h *inviteHandler) handleJoinFallback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	token := resolveInviteTokenQuery(r)
	if token == "" {
		http.NotFound(w, r)
		return
	}
	if !h.tokenIsActive(r.Context(), token) {
		http.Error(w, "Invalid or expired invite", http.StatusNotFound)
		return
	}

	data := map[string]string{
		"Token":          token,
		"ManualURL":      "/join-manually?token=" + url.QueryEscape(token),
		"ClaimURL":       "/join?token=" + url.QueryEscape(token),
		"InviteHome":     "/",
		"ShowInvitesNav": boolString(h.showInvitesNav(r)),
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if err := joinFallbackTemplate.Execute(w, data); err != nil {
		http.Error(w, "Template error", http.StatusInternalServerError)
	}
}

func (h *inviteHandler) handleJoinManually(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	token := resolveInviteTokenQuery(r)
	if token == "" {
		http.NotFound(w, r)
		return
	}
	if !h.tokenIsActive(r.Context(), token) {
		http.Error(w, "Invalid or expired invite", http.StatusNotFound)
		return
	}

	data := map[string]string{
		"Token":          token,
		"ConsumeTo":      "/invite/consume",
		"ShowInvitesNav": boolString(h.showInvitesNav(r)),
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if err := joinManualTemplate.Execute(w, data); err != nil {
		http.Error(w, "Template error", http.StatusInternalServerError)
	}
}

func (h *inviteHandler) handleInviteConsumeRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	h.handleInviteConsume(w, r, resolveInviteTokenQuery(r))
}

func (h *inviteHandler) handleAliasEndpoint(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	trimmed := strings.Trim(r.URL.Path, "/")
	if trimmed == "" || strings.Contains(trimmed, "/") {
		http.NotFound(w, r)
		return
	}

	mode, err := h.config.GetPrivacyMode(r.Context())
	if err != nil {
		h.writeInviteError(w, r, http.StatusInternalServerError, "Failed to check room mode")
		return
	}
	if mode == roomdb.ModeRestricted {
		http.NotFound(w, r)
		return
	}

	aliasEntry, err := h.aliases.Resolve(r.Context(), trimmed)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	payload := h.aliasPayload(aliasEntry)
	if r.URL.Query().Get("encoding") == "json" || wantsJSONResponse(r) {
		h.writeJSON(w, http.StatusOK, payload)
		return
	}

	view := struct {
		ShowInvitesNav     bool
		Alias              string
		UserID             string
		RoomID             string
		MultiserverAddress string
		ConsumeURI         template.URL
	}{
		ShowInvitesNav:     h.showInvitesNav(r),
		Alias:              aliasEntry.Name,
		UserID:             aliasEntry.Owner.String(),
		RoomID:             h.keyPair.FeedRef().String(),
		MultiserverAddress: h.multiserverAddress(),
		ConsumeURI:         template.URL(h.consumeAliasURI(aliasEntry)),
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if err := aliasPageTemplate.Execute(w, view); err != nil {
		http.Error(w, "Template error", http.StatusInternalServerError)
	}
}

func (h *inviteHandler) handleInvites(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	policyCtx, err := h.policyContext(r)
	if err != nil {
		h.writeInviteError(w, r, http.StatusInternalServerError, "Failed to check room mode")
		return
	}

	if !h.canManageInvites(policyCtx) {
		h.writePolicyUnauthorized(w, r, "/invites", "invite management requires an authenticated member role")
		return
	}

	invites, err := h.roomDB.List(r.Context())
	if err != nil {
		h.writeInviteError(w, r, http.StatusInternalServerError, "Failed to list invites")
		return
	}

	active, inactive := splitInviteRows(invites)

	if wantsJSONResponse(r) || r.URL.Query().Get("encoding") == "json" {
		h.writeJSON(w, http.StatusOK, map[string]any{
			"status":   "successful",
			"mode":     modeLabel(policyCtx.mode),
			"active":   active,
			"inactive": inactive,
		})
		return
	}

	data := inviteManagementPageData{
		ShowInvitesNav:  true,
		ModeLabel:       modeLabel(policyCtx.mode),
		PermissionHint:  invitePermissionHint(policyCtx, h.canCreateInvite(policyCtx), h.canRevokeInvite(policyCtx)),
		CanCreateInvite: h.canCreateInvite(policyCtx),
		CanRevokeInvite: h.canRevokeInvite(policyCtx),
		ActiveInvites:   active,
		InactiveInvites: inactive,
		Message:         decodeQueryMsg(r.URL.Query().Get("message")),
		Error:           decodeQueryMsg(r.URL.Query().Get("error")),
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if err := inviteManagementTemplate.Execute(w, data); err != nil {
		http.Error(w, "Template error", http.StatusInternalServerError)
	}
}

func (h *inviteHandler) handleInviteRevoke(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	policyCtx, err := h.policyContext(r)
	if err != nil {
		h.writeInviteError(w, r, http.StatusInternalServerError, "Failed to check room mode")
		return
	}

	if !h.canRevokeInvite(policyCtx) {
		h.writePolicyUnauthorized(w, r, "/invites", "invite revoke requires an authenticated member role")
		return
	}

	inviteID, err := parseInviteIDFromRequest(r)
	if err != nil {
		if wantsJSONResponse(r) || strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
			h.writeJSON(w, http.StatusBadRequest, map[string]string{
				"status": "error",
				"error":  "invalid invite id",
			})
			return
		}
		http.Redirect(w, r, "/invites?error="+url.QueryEscape("Invalid invite id"), http.StatusSeeOther)
		return
	}

	if err := h.roomDB.Revoke(r.Context(), inviteID); err != nil {
		lower := strings.ToLower(err.Error())
		if wantsJSONResponse(r) || strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
			statusCode := http.StatusInternalServerError
			msg := "failed to revoke invite"
			if strings.Contains(lower, "no rows") {
				statusCode = http.StatusNotFound
				msg = "invite not found"
			}
			h.writeJSON(w, statusCode, map[string]string{
				"status": "error",
				"error":  msg,
			})
			return
		}

		msg := "Failed to revoke invite"
		if strings.Contains(lower, "no rows") {
			msg = "Invite not found"
		}
		http.Redirect(w, r, "/invites?error="+url.QueryEscape(msg), http.StatusSeeOther)
		return
	}

	if wantsJSONResponse(r) || strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
		h.writeJSON(w, http.StatusOK, map[string]string{
			"status": "successful",
		})
		return
	}
	http.Redirect(w, r, "/invites?message="+url.QueryEscape("Invite revoked"), http.StatusSeeOther)
}

func (h *inviteHandler) serveJoinPage(w http.ResponseWriter, r *http.Request, token string) {
	if !h.tokenIsActive(r.Context(), token) {
		if r.URL.Query().Get("encoding") == "json" {
			h.writeJSON(w, http.StatusNotFound, map[string]string{
				"status": "error",
				"error":  "invalid or expired invite",
			})
			return
		}

		http.Error(w, "Invalid or expired invite", http.StatusNotFound)
		return
	}

	postTo := h.consumeURL(r)
	if r.URL.Query().Get("encoding") == "json" {
		h.writeJSON(w, http.StatusOK, map[string]string{
			"status": "successful",
			"invite": token,
			"postTo": postTo,
		})
		return
	}

	joinURI := h.claimURI(r, token)
	fallbackURL := "/join-fallback?token=" + url.QueryEscape(token)
	manualURL := "/join-manually?token=" + url.QueryEscape(token)

	type joinPageData struct {
		ShowInvitesNav bool
		Token          string
		ClaimURI       template.URL
		FallbackURL    string
		ManualURL      string
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if err := joinPageTemplate.Execute(w, joinPageData{
		ShowInvitesNav: h.showInvitesNav(r),
		Token:          token,
		ClaimURI:       joinURI,
		FallbackURL:    fallbackURL,
		ManualURL:      manualURL,
	}); err != nil {
		http.Error(w, "Template error", http.StatusInternalServerError)
	}
}

func (h *inviteHandler) handleJoinSubmit(w http.ResponseWriter, r *http.Request, token string) {
	h.handleInviteConsume(w, r, token)
}

type inviteConsumePayload struct {
	ID     string `json:"id"`
	Invite string `json:"invite"`
	Token  string `json:"token"`
}

func (h *inviteHandler) handleInviteConsume(w http.ResponseWriter, r *http.Request, fallbackToken string) {
	contentType := strings.ToLower(strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0]))
	respondJSON := contentType == "application/json" || (contentType == "" && wantsJSONResponse(r))

	var token string
	var parsedID *refs.FeedRef

	switch contentType {
	case "application/json":
		var payload inviteConsumePayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			h.renderConsumeError(w, r, respondJSON, http.StatusBadRequest, fmt.Errorf("invalid json body: %w", err))
			return
		}
		token = strings.TrimSpace(payload.Invite)
		if token == "" {
			token = strings.TrimSpace(payload.Token)
		}
		if token == "" {
			token = fallbackToken
		}

		ref, err := refs.ParseFeedRef(strings.TrimSpace(payload.ID))
		if err != nil {
			h.renderConsumeError(w, r, respondJSON, http.StatusBadRequest, fmt.Errorf("invalid feed id"))
			return
		}
		parsedID = ref
	case "", "application/x-www-form-urlencoded":
		if err := r.ParseForm(); err != nil {
			h.renderConsumeError(w, r, respondJSON, http.StatusBadRequest, fmt.Errorf("invalid form data: %w", err))
			return
		}
		token = resolveInviteTokenForm(r)
		if token == "" {
			token = fallbackToken
		}
		idVal := strings.TrimSpace(r.FormValue("id"))
		ref, err := refs.ParseFeedRef(idVal)
		if err != nil {
			h.renderConsumeError(w, r, respondJSON, http.StatusBadRequest, fmt.Errorf("invalid feed id"))
			return
		}
		parsedID = ref
	default:
		h.renderConsumeError(w, r, true, http.StatusBadRequest, fmt.Errorf("unsupported content type"))
		return
	}

	if token == "" {
		h.renderConsumeError(w, r, respondJSON, http.StatusBadRequest, fmt.Errorf("missing invite token"))
		return
	}

	if h.deniedKeys != nil && h.deniedKeys.HasFeed(r.Context(), *parsedID) {
		h.renderConsumeError(w, r, respondJSON, http.StatusForbidden, fmt.Errorf("feed is denied"))
		return
	}

	_, err := h.roomDB.Consume(r.Context(), token, *parsedID)
	if err != nil {
		statusCode := http.StatusBadRequest
		msg := "failed to consume invite"
		lower := strings.ToLower(err.Error())
		switch {
		case strings.Contains(lower, "no rows"):
			statusCode = http.StatusNotFound
			msg = "invalid invite token"
		case strings.Contains(lower, "already used"):
			statusCode = http.StatusConflict
			msg = "invite already used"
		default:
			msg = err.Error()
		}
		metrics.RoomInvitesConsumed.WithLabelValues("failed").Inc()
		h.renderConsumeError(w, r, respondJSON, statusCode, errors.New(msg))
		return
	}

	metrics.RoomInvitesConsumed.WithLabelValues("ok").Inc()

	multiserverAddress := h.multiserverAddress()
	if respondJSON {
		h.writeJSON(w, http.StatusOK, map[string]string{
			"status":             "successful",
			"multiserverAddress": multiserverAddress,
		})
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if err := inviteConsumedTemplate.Execute(w, map[string]string{
		"ShowInvitesNav":     boolString(h.showInvitesNav(r)),
		"MultiserverAddress": multiserverAddress,
		"HomeURL":            "/",
	}); err != nil {
		http.Error(w, "Template error", http.StatusInternalServerError)
	}
}

func (h *inviteHandler) renderConsumeError(w http.ResponseWriter, r *http.Request, respondJSON bool, statusCode int, err error) {
	if respondJSON {
		h.writeJSON(w, statusCode, map[string]string{
			"status": "error",
			"error":  err.Error(),
		})
		return
	}
	http.Error(w, err.Error(), statusCode)
}

func (h *inviteHandler) tokenIsActive(ctx context.Context, token string) bool {
	invite, err := h.roomDB.GetByToken(ctx, token)
	if err != nil {
		return false
	}
	return invite.Active
}

func (h *inviteHandler) memberFromRequest(r *http.Request) (roomdb.Member, bool) {
	if h.authTokens == nil || h.members == nil {
		return roomdb.Member{}, false
	}
	cookie, err := r.Cookie(authTokenCookieName)
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		return roomdb.Member{}, false
	}

	memberID, err := h.authTokens.CheckToken(r.Context(), cookie.Value)
	if err != nil {
		return roomdb.Member{}, false
	}

	member, err := h.members.GetByID(r.Context(), memberID)
	if err != nil {
		return roomdb.Member{}, false
	}
	return member, true
}

func (h *inviteHandler) policyContext(r *http.Request) (invitePolicyContext, error) {
	mode, err := h.config.GetPrivacyMode(r.Context())
	if err != nil {
		return invitePolicyContext{}, err
	}
	member, authenticated := h.memberFromRequest(r)
	return invitePolicyContext{
		mode:          mode,
		member:        member,
		authenticated: authenticated,
	}, nil
}

func (h *inviteHandler) canCreateInvite(policy invitePolicyContext) bool {
	switch policy.mode {
	case roomdb.ModeOpen:
		return true
	case roomdb.ModeCommunity:
		return policy.authenticated && isMemberOrHigher(policy.member.Role)
	case roomdb.ModeRestricted:
		return policy.authenticated && isModeratorOrHigher(policy.member.Role)
	default:
		return false
	}
}

func (h *inviteHandler) canManageInvites(policy invitePolicyContext) bool {
	return h.canCreateInvite(policy)
}

func (h *inviteHandler) canRevokeInvite(policy invitePolicyContext) bool {
	switch policy.mode {
	case roomdb.ModeOpen, roomdb.ModeCommunity:
		return policy.authenticated && isMemberOrHigher(policy.member.Role)
	case roomdb.ModeRestricted:
		return policy.authenticated && isModeratorOrHigher(policy.member.Role)
	default:
		return false
	}
}

func (h *inviteHandler) writePolicyUnauthorized(w http.ResponseWriter, r *http.Request, nextPath, message string) {
	if wantsJSONResponse(r) {
		h.writeJSON(w, http.StatusForbidden, map[string]string{
			"status": "error",
			"error":  message,
		})
		return
	}
	http.Redirect(w, r, "/login?next="+url.QueryEscape(nextPath), http.StatusSeeOther)
}

func (h *inviteHandler) showInvitesNav(r *http.Request) bool {
	policyCtx, err := h.policyContext(r)
	if err != nil {
		return false
	}
	return h.canManageInvites(policyCtx)
}

func isMemberOrHigher(role roomdb.Role) bool {
	return role == roomdb.RoleMember || role == roomdb.RoleModerator || role == roomdb.RoleAdmin
}

func isModeratorOrHigher(role roomdb.Role) bool {
	return role == roomdb.RoleModerator || role == roomdb.RoleAdmin
}

type inviteManagementRow struct {
	ID        int64  `json:"id"`
	Status    string `json:"status"`
	CreatedAt string `json:"createdAt"`
	CreatedBy int64  `json:"createdBy"`
}

type inviteManagementPageData struct {
	ShowInvitesNav  bool
	ModeLabel       string
	PermissionHint  string
	CanCreateInvite bool
	CanRevokeInvite bool
	ActiveInvites   []inviteManagementRow
	InactiveInvites []inviteManagementRow
	Message         string
	Error           string
}

func splitInviteRows(invites []roomdb.Invite) ([]inviteManagementRow, []inviteManagementRow) {
	active := make([]inviteManagementRow, 0, len(invites))
	inactive := make([]inviteManagementRow, 0, len(invites))
	for _, inv := range invites {
		row := inviteManagementRow{
			ID:        inv.ID,
			Status:    inviteStatus(inv.Active),
			CreatedAt: formatInviteCreatedAt(inv.CreatedAt),
			CreatedBy: inv.CreatedBy,
		}
		if inv.Active {
			active = append(active, row)
		} else {
			inactive = append(inactive, row)
		}
	}
	return active, inactive
}

func inviteStatus(active bool) string {
	if active {
		return "active"
	}
	return "consumed"
}

func formatInviteCreatedAt(unix int64) string {
	if unix <= 0 {
		return "unknown"
	}
	return time.Unix(unix, 0).UTC().Format(time.RFC3339)
}

func modeLabel(mode roomdb.PrivacyMode) string {
	switch mode {
	case roomdb.ModeOpen:
		return "Open"
	case roomdb.ModeCommunity:
		return "Community"
	case roomdb.ModeRestricted:
		return "Restricted"
	default:
		return "Unknown"
	}
}

func invitePermissionHint(policy invitePolicyContext, canCreate, canRevoke bool) string {
	role := "anonymous"
	if policy.authenticated {
		role = policy.member.Role.String()
	}
	parts := []string{"Mode: " + modeLabel(policy.mode), "Role: " + role}
	if canCreate {
		parts = append(parts, "can create invites")
	} else {
		parts = append(parts, "cannot create invites")
	}
	if canRevoke {
		parts = append(parts, "can revoke invites")
	} else {
		parts = append(parts, "cannot revoke invites")
	}
	return strings.Join(parts, " · ")
}

func decodeQueryMsg(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	decoded, err := url.QueryUnescape(raw)
	if err != nil {
		return raw
	}
	return decoded
}

func parseInviteIDFromRequest(r *http.Request) (int64, error) {
	contentType := strings.ToLower(strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0]))
	switch contentType {
	case "application/json":
		var payload struct {
			ID       int64  `json:"id"`
			InviteID int64  `json:"inviteID"`
			IDStr    string `json:"idStr"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			return 0, err
		}
		switch {
		case payload.ID > 0:
			return payload.ID, nil
		case payload.InviteID > 0:
			return payload.InviteID, nil
		case strings.TrimSpace(payload.IDStr) != "":
			return strconv.ParseInt(strings.TrimSpace(payload.IDStr), 10, 64)
		default:
			return 0, fmt.Errorf("missing id")
		}
	default:
		if err := r.ParseForm(); err != nil {
			return 0, err
		}
		val := strings.TrimSpace(r.FormValue("id"))
		if val == "" {
			val = strings.TrimSpace(r.FormValue("inviteID"))
		}
		if val == "" {
			return 0, fmt.Errorf("missing id")
		}
		return strconv.ParseInt(val, 10, 64)
	}
}

func boolString(v bool) string {
	if v {
		return "true"
	}
	return ""
}

func resolveInviteTokenQuery(r *http.Request) string {
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	if token != "" {
		return token
	}
	return strings.TrimSpace(r.URL.Query().Get("invite"))
}

func resolveInviteTokenForm(r *http.Request) string {
	token := strings.TrimSpace(r.FormValue("invite"))
	if token != "" {
		return token
	}
	return strings.TrimSpace(r.FormValue("token"))
}

func (h *inviteHandler) claimURI(r *http.Request, token string) template.URL {
	queryVals := make(url.Values)
	queryVals.Set("action", "claim-http-invite")
	queryVals.Set("invite", token)
	queryVals.Set("postTo", h.consumeURL(r))
	return template.URL("ssb:experimental?" + queryVals.Encode())
}

func (h *inviteHandler) consumeURL(r *http.Request) string {
	return h.externalBaseURL(r) + "/invite/consume"
}

func (h *inviteHandler) externalBaseURL(r *http.Request) string {
	domain := strings.TrimSpace(h.domain)
	if domain != "" {
		if strings.HasPrefix(domain, "http://") || strings.HasPrefix(domain, "https://") {
			return strings.TrimRight(domain, "/")
		}
		return "https://" + domain
	}

	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	host := r.Host
	if strings.TrimSpace(host) == "" {
		host = "localhost"
	}
	return scheme + "://" + host
}

func (h *inviteHandler) aliasPayload(alias roomdb.Alias) map[string]string {
	return map[string]string{
		"status":             "successful",
		"multiserverAddress": h.multiserverAddress(),
		"roomId":             h.keyPair.FeedRef().String(),
		"userId":             alias.Owner.String(),
		"alias":              alias.Name,
		"signature":          base64.StdEncoding.EncodeToString(alias.Signature) + ".sig.ed25519",
	}
}

func (h *inviteHandler) consumeAliasURI(alias roomdb.Alias) string {
	query := make(url.Values)
	query.Set("action", "consume-alias")
	query.Set("alias", alias.Name)
	query.Set("userId", alias.Owner.String())
	query.Set("signature", base64.StdEncoding.EncodeToString(alias.Signature)+".sig.ed25519")
	query.Set("roomId", h.keyPair.FeedRef().String())
	query.Set("multiserverAddress", h.multiserverAddress())
	return "ssb:experimental?" + query.Encode()
}

func inviteAdvertisedHost(domain string) string {
	raw := strings.TrimSpace(domain)
	if raw == "" {
		return ""
	}

	if strings.Contains(raw, "://") {
		parsed, err := url.Parse(raw)
		if err != nil {
			return ""
		}
		return strings.TrimSpace(parsed.Hostname())
	}

	parsed, err := url.Parse("//" + raw)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(parsed.Hostname())
}

func (h *inviteHandler) multiserverAddress() string {
	addr := strings.TrimSpace(h.muxrpcAddr)
	if addr == "" {
		addr = defaultMUXRPCListenAddr
	}

	host, port, err := net.SplitHostPort(addr)
	if err == nil {
		if advertisedHost := inviteAdvertisedHost(h.domain); advertisedHost != "" {
			host = advertisedHost
		}
		if host == "" {
			addr = ":" + port
		} else {
			addr = net.JoinHostPort(host, port)
		}
	}

	if h.keyPair == nil {
		return "net:" + addr
	}

	return fmt.Sprintf("net:%s~shs:%s", addr, base64.StdEncoding.EncodeToString(h.keyPair.FeedRef().PubKey()))
}

func (h *inviteHandler) writeInviteError(w http.ResponseWriter, r *http.Request, statusCode int, msg string) {
	if wantsJSONResponse(r) {
		h.writeJSON(w, statusCode, map[string]string{
			"status": "error",
			"error":  msg,
		})
		return
	}
	http.Error(w, msg, statusCode)
}

func (h *inviteHandler) writeJSON(w http.ResponseWriter, statusCode int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(payload)
}

func withContext(f func(context.Context) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := f(r.Context()); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}
