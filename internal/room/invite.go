package room

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/roomdb"
)

type inviteHandler struct {
	roomDB     roomdb.InvitesService
	members    roomdb.MembersService
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
	return
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
		h.renderConsumeError(w, r, respondJSON, statusCode, errors.New(msg))
		return
	}

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

func (h *inviteHandler) multiserverAddress() string {
	addr := strings.TrimSpace(h.muxrpcAddr)
	if addr == "" {
		addr = defaultMUXRPCListenAddr
	}

	if h.keyPair == nil {
		return "net:" + addr
	}

	return fmt.Sprintf("net:%s~shs:%s", addr, h.keyPair.FeedRef().String())
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

var invitePageTemplate = template.Must(template.New("invite-create").Parse(publicLayoutTemplate + invitePageHTML))
var inviteCreatedTemplate = template.Must(template.New("invite-created").Parse(inviteCreatedHTML))
var joinPageTemplate = template.Must(template.New("join-room").Parse(publicLayoutTemplate + joinPageHTML))
var joinFallbackTemplate = template.Must(template.New("join-fallback").Parse(publicLayoutTemplate + joinFallbackHTML))
var joinManualTemplate = template.Must(template.New("join-manual").Parse(publicLayoutTemplate + joinManualHTML))
var inviteConsumedTemplate = template.Must(template.New("invite-consumed").Parse(publicLayoutTemplate + inviteConsumedHTML))
var inviteManagementTemplate = template.Must(template.New("invite-management").Parse(publicLayoutTemplate + inviteManagementHTML))

const invitePageHTML = `
{{define "pageTitle"}}Create Invite - ATProto to SSB Bridge{{end}}
{{define "content"}}
<div class="hero">
  <h1>Create an Invite</h1>
  <p class="eyebrow">Room Access</p>
</div>

<div class="panel">
  <h2 style="margin-top: 0">Get a Room Invite Link</h2>
  <p>Create an invite link to share with others. Recipients can use it to join this Secure Scuttlebutt room.</p>
  
  <form id="inviteForm" method="post">
    <button type="submit" class="btn-primary" style="font-size: 1em; padding: 12px 24px;">
      Create Invite
    </button>
  </form>
  
  <div id="result" style="margin-top: 24px; display: none;">
    <div class="action-row-compact">
      <input type="text" id="inviteUrl" readonly style="flex: 1; padding: 12px; border: 1px solid #ddd; border-radius: 6px; font-family: monospace; font-size: 0.9em;" />
      <button type="button" class="btn-copy" onclick="copyInvite()">Copy</button>
    </div>
    <p style="color: #666; font-size: 0.9em; margin-top: 12px;">
      Share this link with anyone you want to invite. The invite link will expire after use.
    </p>
  </div>
  
  <div id="error" style="margin-top: 24px; display: none; color: #721c24; background: #f8d7da; padding: 12px; border-radius: 6px;"></div>
</div>

<script>
document.getElementById('inviteForm').addEventListener('submit', async function(e) {
  e.preventDefault();
  const result = document.getElementById('result');
  const error = document.getElementById('error');
  const urlInput = document.getElementById('inviteUrl');
  
  result.style.display = 'none';
  error.style.display = 'none';
  
  try {
    const resp = await fetch('/create-invite', {
      method: 'POST',
      headers: { 'Accept': 'application/json' }
    });
    const data = await resp.json();
    
    if (data.error) {
      error.textContent = data.error;
      error.style.display = 'block';
    } else {
      urlInput.value = data.url;
      result.style.display = 'block';
    }
  } catch (err) {
    error.textContent = 'Failed to create invite. Please try again.';
    error.style.display = 'block';
  }
});

function copyInvite() {
  const input = document.getElementById('inviteUrl');
  input.select();
  document.execCommand('copy');
  const btn = document.querySelector('.btn-copy');
  btn.textContent = 'Copied!';
  setTimeout(() => btn.textContent = 'Copy', 2000);
}
</script>
{{end}}
`

const inviteCreatedHTML = `
<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1.0" />
  <title>Invite Created - ATProto to SSB Bridge</title>
  <style>
    :root { --paper: #f3ebdd; --accent: #0d7f64; }
    body { font-family: system-ui, sans-serif; margin: 0; min-height: 100vh; background: var(--paper); color: #132820; }
    .page-shell { max-width: 600px; margin: 0 auto; padding: 48px 24px; }
    .hero, .panel { background: white; border-radius: 12px; padding: 32px; margin-bottom: 24px; box-shadow: 0 2px 8px rgba(0,0,0,0.1); }
    h1 { margin: 0 0 8px 0; color: var(--accent); }
    .eyebrow { color: #666; font-size: 0.85em; text-transform: uppercase; letter-spacing: 0.05em; margin-bottom: 24px; }
    p { line-height: 1.6; }
    .action-row-compact { display: flex; gap: 8px; margin-bottom: 16px; }
    input { flex: 1; padding: 12px; border: 1px solid #ddd; border-radius: 6px; font-family: monospace; font-size: 0.9em; }
    .btn-copy { padding: 12px 20px; background: #f5f5f5; border: 1px solid #ddd; border-radius: 6px; cursor: pointer; font-size: 0.9em; }
    .btn-primary { padding: 12px 24px; background: var(--accent); color: white; border: none; border-radius: 6px; text-decoration: none; display: inline-block; }
    .success { background: #d4edda; color: #155724; padding: 12px; border-radius: 6px; margin-bottom: 16px; }
    a { color: var(--accent); }
  </style>
</head>
<body>
  <div class="page-shell">
    <header style="margin-bottom: 24px;">
      <div style="font-weight: bold;"><a href="/">ATProto to SSB Bridge Room</a></div>
    </header>
    <div class="hero">
      <h1>Invite Created!</h1>
      <p class="eyebrow">Success</p>
      <div class="success">Your invite link is ready to share.</div>
      <div class="action-row-compact">
        <input type="text" value="{{.URL}}" readonly onclick="this.select()" />
        <button class="btn-copy" onclick="copyLink()">Copy</button>
      </div>
      <p style="color: #666; font-size: 0.9em;">
        Share this link with anyone you want to invite to the room. The link works for one-time use.
      </p>
    </div>
    <div style="text-align: center;">
      <a href="/create-invite" class="btn-primary">Create Another</a>
      <span style="margin: 0 12px;">or</span>
      <a href="/">Back to Room</a>
    </div>
  </div>
  <script>
    function copyLink() {
      const input = document.querySelector('input');
      input.select();
      document.execCommand('copy');
      const btn = document.querySelector('.btn-copy');
      btn.textContent = 'Copied!';
      setTimeout(() => btn.textContent = 'Copy', 2000);
    }
  </script>
</body>
</html>
`

const joinPageHTML = `
{{define "pageTitle"}}Join Room - ATProto to SSB Bridge{{end}}
{{define "content"}}
<div class="hero">
  <h1>Join this Room</h1>
  <p class="eyebrow">Room Access</p>
</div>

<div class="panel">
  <h2 style="margin-top: 0">Claim Invite in Your SSB Client</h2>
  <p>If your SSB client supports HTTP invite claiming, click the claim link below.</p>

  <p>
    <a id="claim-invite-uri" class="btn-primary" href="{{.ClaimURI}}">Claim Invite</a>
  </p>

  <p style="font-size: 0.95em; color: #555;">
    If your app does not open automatically, we'll take you to fallback instructions in a few seconds.
  </p>

  <div class="action-row-compact">
    <a href="{{.FallbackURL}}" class="btn-copy">Open fallback now</a>
    <a href="{{.ManualURL}}" class="btn-copy">Manual claim</a>
  </div>
</div>

<script>
setTimeout(function () {
  window.location.href = "{{.FallbackURL}}";
}, 4500);
</script>
{{end}}
`

const joinFallbackHTML = `
{{define "pageTitle"}}Invite Fallback - ATProto to SSB Bridge{{end}}
{{define "content"}}
<div class="hero">
  <h1>Invite Claim Fallback</h1>
  <p class="eyebrow">Room Access</p>
</div>

<div class="panel">
  <h2 style="margin-top: 0">Couldn’t open your SSB client?</h2>
  <p>You can retry the claim link or continue with manual claim by entering your feed ID.</p>
  <div class="action-row-compact">
    <a class="btn-primary" href="{{.ClaimURL}}">Retry claim link</a>
    <a class="btn-copy" href="{{.ManualURL}}">Claim manually</a>
  </div>
  <p style="color: #666; font-size: 0.9em; margin-top: 16px;">
    Token: <code>{{.Token}}</code>
  </p>
</div>
{{end}}
`

const joinManualHTML = `
{{define "pageTitle"}}Manual Invite Claim - ATProto to SSB Bridge{{end}}
{{define "content"}}
<div class="hero">
  <h1>Manual Invite Claim</h1>
  <p class="eyebrow">Room Access</p>
</div>

<div class="panel">
  <h2 style="margin-top: 0">Enter Your SSB Feed ID</h2>
  <p>Paste your feed ID and submit to consume this invite.</p>
  <form method="post" action="{{.ConsumeTo}}">
    <input type="hidden" name="invite" value="{{.Token}}" />
    <label for="id" style="display:block; margin-bottom:8px;">Feed ID</label>
    <input id="id" name="id" type="text" required placeholder="@...ed25519" style="width:100%; padding: 12px; border: 1px solid #ddd; border-radius: 6px; font-family: monospace; box-sizing: border-box;" />
    <button type="submit" class="btn-primary" style="margin-top: 12px;">Claim Invite</button>
  </form>
</div>
{{end}}
`

const inviteConsumedHTML = `
{{define "pageTitle"}}Invite Consumed - ATProto to SSB Bridge{{end}}
{{define "content"}}
<div class="hero">
  <h1>Invite Consumed</h1>
  <p class="eyebrow">Success</p>
</div>

<div class="panel">
  <h2 style="margin-top: 0">Connection Details</h2>
  <p>Use this multiserver address in your SSB client:</p>
  <div class="action-row-compact">
    <input type="text" value="{{.MultiserverAddress}}" readonly onclick="this.select()" style="flex:1; padding:12px; border:1px solid #ddd; border-radius:6px; font-family: monospace;" />
    <button type="button" class="btn-copy" onclick="copyAddress()">Copy</button>
  </div>
  <p style="font-size: 0.9em; color: #666; margin-top: 12px;">
    <a href="{{.HomeURL}}">Back to room</a>
  </p>
</div>
<script>
function copyAddress() {
  const input = document.querySelector('input');
  input.select();
  document.execCommand('copy');
}
</script>
{{end}}
`

const inviteManagementHTML = `
{{define "pageTitle"}}Invite Management - ATProto to SSB Bridge{{end}}
{{define "content"}}
<div class="hero">
  <h1>Invite Management</h1>
  <p class="eyebrow">Room Access</p>
  <p>{{.PermissionHint}}</p>
</div>

{{if .Message}}
<div class="panel" style="background:#d4edda; color:#155724;">
  <strong>{{.Message}}</strong>
</div>
{{end}}

{{if .Error}}
<div class="panel" style="background:#f8d7da; color:#721c24;">
  <strong>{{.Error}}</strong>
</div>
{{end}}

<div class="panel">
  <h2 style="margin-top: 0;">Create Invite</h2>
  {{if .CanCreateInvite}}
  <form id="manageInviteCreateForm" method="post" action="/create-invite">
    <button type="submit" class="btn-primary">Create Invite</button>
  </form>
  <div id="manageInviteResult" style="display:none; margin-top: 16px;">
    <div class="action-row-compact">
      <input type="text" id="manageInviteURL" readonly style="flex:1; padding:12px; border:1px solid #ddd; border-radius:6px; font-family: monospace;" />
      <button type="button" class="btn-copy" onclick="copyManageInvite()">Copy</button>
    </div>
  </div>
  <div id="manageInviteError" style="display:none; margin-top: 12px; color:#721c24;"></div>
  {{else}}
  <p>You do not have permission to create invites in this room mode.</p>
  {{end}}
</div>

<div class="panel">
  <h2 style="margin-top: 0;">Active Invites</h2>
  <p style="color:#666; font-size:0.9em;">Historical invite URLs are not recoverable from storage. Copy links at creation time.</p>
  {{if .ActiveInvites}}
  <table style="width:100%; border-collapse: collapse;">
    <thead>
      <tr>
        <th style="text-align:left; padding:8px; border-bottom:1px solid #ddd;">ID</th>
        <th style="text-align:left; padding:8px; border-bottom:1px solid #ddd;">Status</th>
        <th style="text-align:left; padding:8px; border-bottom:1px solid #ddd;">Created At</th>
        <th style="text-align:left; padding:8px; border-bottom:1px solid #ddd;">Creator ID</th>
        <th style="text-align:left; padding:8px; border-bottom:1px solid #ddd;">Action</th>
      </tr>
    </thead>
    <tbody>
      {{range .ActiveInvites}}
      <tr>
        <td style="padding:8px; border-bottom:1px solid #eee;">{{.ID}}</td>
        <td style="padding:8px; border-bottom:1px solid #eee;">{{.Status}}</td>
        <td style="padding:8px; border-bottom:1px solid #eee;"><code>{{.CreatedAt}}</code></td>
        <td style="padding:8px; border-bottom:1px solid #eee;">{{.CreatedBy}}</td>
        <td style="padding:8px; border-bottom:1px solid #eee;">
          {{if $.CanRevokeInvite}}
          <form method="post" action="/invites/revoke" style="display:inline;">
            <input type="hidden" name="id" value="{{.ID}}" />
            <button type="submit" class="btn-secondary">Revoke</button>
          </form>
          {{else}}
          <span style="color:#666; font-size:0.85em;">No revoke access</span>
          {{end}}
        </td>
      </tr>
      {{end}}
    </tbody>
  </table>
  {{else}}
  <p>No active invites.</p>
  {{end}}
</div>

<div class="panel">
  <h2 style="margin-top: 0;">Consumed / Inactive Invites</h2>
  {{if .InactiveInvites}}
  <table style="width:100%; border-collapse: collapse;">
    <thead>
      <tr>
        <th style="text-align:left; padding:8px; border-bottom:1px solid #ddd;">ID</th>
        <th style="text-align:left; padding:8px; border-bottom:1px solid #ddd;">Status</th>
        <th style="text-align:left; padding:8px; border-bottom:1px solid #ddd;">Created At</th>
        <th style="text-align:left; padding:8px; border-bottom:1px solid #ddd;">Creator ID</th>
      </tr>
    </thead>
    <tbody>
      {{range .InactiveInvites}}
      <tr>
        <td style="padding:8px; border-bottom:1px solid #eee;">{{.ID}}</td>
        <td style="padding:8px; border-bottom:1px solid #eee;">{{.Status}}</td>
        <td style="padding:8px; border-bottom:1px solid #eee;"><code>{{.CreatedAt}}</code></td>
        <td style="padding:8px; border-bottom:1px solid #eee;">{{.CreatedBy}}</td>
      </tr>
      {{end}}
    </tbody>
  </table>
  {{else}}
  <p>No consumed or revoked invites yet.</p>
  {{end}}
</div>

{{if .CanCreateInvite}}
<script>
document.getElementById('manageInviteCreateForm').addEventListener('submit', async function(e) {
  e.preventDefault();
  const result = document.getElementById('manageInviteResult');
  const error = document.getElementById('manageInviteError');
  const urlInput = document.getElementById('manageInviteURL');
  result.style.display = 'none';
  error.style.display = 'none';
  try {
    const resp = await fetch('/create-invite', {
      method: 'POST',
      headers: { 'Accept': 'application/json' }
    });
    const data = await resp.json();
    if (!resp.ok || data.error) {
      error.textContent = data.error || 'Failed to create invite.';
      error.style.display = 'block';
      return;
    }
    urlInput.value = data.url;
    result.style.display = 'block';
  } catch (err) {
    error.textContent = 'Failed to create invite.';
    error.style.display = 'block';
  }
});

function copyManageInvite() {
  const input = document.getElementById('manageInviteURL');
  input.select();
  document.execCommand('copy');
}
</script>
{{end}}
{{end}}
`

func withContext(f func(context.Context) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := f(r.Context()); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}
