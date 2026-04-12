package templates

import "io"

const roomSubnavContent = `
<div class="room-subnav" role="navigation" aria-label="Room sections">
    <a href="/room" class="{{if eq .Section "overview"}}is-active{{end}}">Overview</a>
    <a href="/room/members" class="{{if eq .Section "members"}}is-active{{end}}">Members &amp; Roles</a>
    <a href="/room/attendants" class="{{if eq .Section "attendants"}}is-active{{end}}">Attendants &amp; Tunnels</a>
    <a href="/room/aliases" class="{{if eq .Section "aliases"}}is-active{{end}}">Aliases &amp; Invites</a>
    <a href="/room/moderation" class="{{if eq .Section "moderation"}}is-active{{end}}">Moderation</a>
</div>
`

const roomOverviewContent = `
{{define "content"}}
<section class="section section-pad">
    <h1 class="page-title">Room Overview</h1>
    <p class="subtitle">Operational snapshot of room mode, membership, invites, aliases, moderation, attendants, and tunnel endpoints.</p>
` + roomSubnavContent + `
</section>

{{if .ActionMessage}}
<section class="status-strip tone-success" role="status"><h2>Action Complete</h2><p>{{.ActionMessage}}</p></section>
{{end}}
{{if .ActionError}}
<section class="status-strip tone-danger" role="alert"><h2>Action Failed</h2><p>{{.ActionError}}</p></section>
{{end}}

{{if not .Available}}
<section class="status-strip tone-warning" role="status">
    <h2>Room Data Unavailable</h2>
    <p>{{.DegradedReason}}</p>
</section>
{{else}}
<section class="section section-pad">
    <div class="metric-grid">
        <div class="metric-card"><span class="metric-label">Mode</span><span class="metric-value">{{.ModeLabel}}</span><span class="metric-note">{{.ModeSummary}}</span></div>
        <div class="metric-card"><span class="metric-label">Members</span><span class="metric-value">{{.MembersCount}}</span><span class="metric-note">All room members</span></div>
        <div class="metric-card"><span class="metric-label">Invites</span><span class="metric-value">{{.InvitesActive}} / {{.InvitesTotal}}</span><span class="metric-note">Active / total</span></div>
        <div class="metric-card"><span class="metric-label">Aliases</span><span class="metric-value">{{.AliasesCount}}</span><span class="metric-note">Resolvable aliases</span></div>
        <div class="metric-card"><span class="metric-label">Denied Keys</span><span class="metric-value">{{.DeniedCount}}</span><span class="metric-note">Blocked feeds</span></div>
        <div class="metric-card"><span class="metric-label">Attendants</span><span class="metric-value">{{.AttendantsActive}} / {{.AttendantsTotal}}</span><span class="metric-note">Active / known</span></div>
        <div class="metric-card"><span class="metric-label">Tunnel Endpoints</span><span class="metric-value">{{.TunnelsActive}} / {{.TunnelsTotal}}</span><span class="metric-note">Active / known</span></div>
    </div>
</section>

<section class="section section-pad">
    <h2 class="page-title" style="font-size:1.1rem">Policy &amp; Health</h2>
    <ul class="mini-list">
        <li><span>Operator Role</span><span class="pill state-pending">{{.OperatorRole}}</span></li>
        <li><span>Policy Hint</span><span>{{.PolicyHint}}</span></li>
        <li><span>Room Health</span><span class="pill state-pending">{{.HealthStatus}}</span></li>
        <li><span>Health Detail</span><span>{{.HealthDetail}}</span></li>
        {{if .StatusEndpointURL}}<li><span>Status Endpoint</span><span class="mono"><a href="{{.StatusEndpointURL}}" target="_blank" rel="noopener">{{.StatusEndpointURL}}</a></span></li>{{end}}
    </ul>
</section>
{{end}}
{{end}}
`

const roomMembersRolesContent = `
{{define "content"}}
<section class="section section-pad">
    <h1 class="page-title">Members &amp; Roles</h1>
    <p class="subtitle">Set room member roles and remove members. All mutations are policy-checked server-side.</p>
` + roomSubnavContent + `
</section>

{{if .ActionMessage}}
<section class="status-strip tone-success" role="status"><h2>Action Complete</h2><p>{{.ActionMessage}}</p></section>
{{end}}
{{if .ActionError}}
<section class="status-strip tone-danger" role="alert"><h2>Action Failed</h2><p>{{.ActionError}}</p></section>
{{end}}

{{if not .Available}}
<section class="status-strip tone-warning"><h2>Room Data Unavailable</h2><p>{{.DegradedReason}}</p></section>
{{else}}
<section class="section section-pad">
    <p class="subtitle">Mode: <strong>{{.ModeLabel}}</strong> · {{.ModeSummary}}</p>
    <p class="subtitle">{{.PolicyHint}}</p>
    <div class="table-wrap">
        <table>
            <thead><tr><th>ID</th><th>Feed</th><th>Role</th><th>Set Role</th><th>Remove</th></tr></thead>
            <tbody>
                {{range .Members}}
                <tr>
                    <td class="mono">{{.ID}}</td>
                    <td class="mono"><span class="truncate" title="{{.FeedID}}">{{.FeedID}}</span></td>
                    <td><span class="pill state-pending">{{.Role}}</span></td>
	                    <td>
	                        <form method="post" action="/room/members/role" class="toolbar" style="justify-content:flex-start;margin:0">
	                            {{csrfField $.Chrome.CSRFToken}}
	                            <input type="hidden" name="member_id" value="{{.ID}}">
	                            <label class="field" style="min-width:170px">
                                <span class="metric-label">Role</span>
                                <select name="role">
                                    <option value="member" {{if eq .RoleRaw "member"}}selected{{end}}>member</option>
                                    <option value="moderator" {{if eq .RoleRaw "moderator"}}selected{{end}}>moderator</option>
                                    <option value="admin" {{if eq .RoleRaw "admin"}}selected{{end}}>admin</option>
                                </select>
                            </label>
                            <button type="submit" class="button">Save</button>
                        </form>
	                    </td>
	                    <td>
	                        <form method="post" action="/room/members/remove" onsubmit="return confirm('Remove this member from room membership?')">
	                            {{csrfField $.Chrome.CSRFToken}}
	                            <input type="hidden" name="member_id" value="{{.ID}}">
	                            <button type="submit" class="button">Remove</button>
	                        </form>
                    </td>
                </tr>
                {{else}}
                <tr><td colspan="5" class="empty">No room members found.</td></tr>
                {{end}}
            </tbody>
        </table>
    </div>
</section>
{{end}}
{{end}}
`

const roomAttendantsTunnelsContent = `
{{define "content"}}
<section class="section section-pad">
    <h1 class="page-title">Attendants &amp; Tunnels</h1>
    <p class="subtitle">Operational snapshots persisted from attendant lifecycle and tunnel announce/leave hooks.</p>
` + roomSubnavContent + `
</section>

{{if .ActionMessage}}
<section class="status-strip tone-success" role="status"><h2>Action Complete</h2><p>{{.ActionMessage}}</p></section>
{{end}}
{{if .ActionError}}
<section class="status-strip tone-danger" role="alert"><h2>Action Failed</h2><p>{{.ActionError}}</p></section>
{{end}}

{{if not .Available}}
<section class="status-strip tone-warning"><h2>Room Data Unavailable</h2><p>{{.DegradedReason}}</p></section>
{{else}}
<section class="section section-pad">
    <div class="toolbar">
        <p class="subtitle" style="margin:0">Mode: <strong>{{.ModeLabel}}</strong> · {{.ModeSummary}}</p>
        <a class="button-link" href="{{.ConnectionsURL}}">Open /connections</a>
    </div>
    <p class="subtitle">{{.PolicyHint}}</p>

    <h2 class="page-title" style="font-size:1.1rem">Attendants</h2>
    <div class="table-wrap">
        <table>
            <thead><tr><th>Feed</th><th>Addr</th><th>Connected</th><th>Last Seen</th><th>Active</th></tr></thead>
            <tbody>
            {{range .Attendants}}
                <tr>
                    <td class="mono"><span class="truncate" title="{{.FeedID}}">{{.FeedID}}</span></td>
                    <td class="mono"><span class="truncate" title="{{.Addr}}">{{.Addr}}</span></td>
                    <td class="mono">{{.ConnectedAt}}</td>
                    <td class="mono">{{.LastSeenAt}}</td>
                    <td>{{if .Active}}<span class="pill state-published">active</span>{{else}}<span class="pill state-deleted">inactive</span>{{end}}</td>
                </tr>
            {{else}}
                <tr><td colspan="5" class="empty">No attendants snapshot rows found.</td></tr>
            {{end}}
            </tbody>
        </table>
    </div>

    <h2 class="page-title" style="font-size:1.1rem">Tunnel Endpoints</h2>
    <div class="table-wrap">
        <table>
            <thead><tr><th>Target Feed</th><th>Addr</th><th>Announced</th><th>Last Seen</th><th>Active</th></tr></thead>
            <tbody>
            {{range .Tunnels}}
                <tr>
                    <td class="mono"><span class="truncate" title="{{.TargetFeed}}">{{.TargetFeed}}</span></td>
                    <td class="mono"><span class="truncate" title="{{.Addr}}">{{.Addr}}</span></td>
                    <td class="mono">{{.AnnouncedAt}}</td>
                    <td class="mono">{{.LastSeenAt}}</td>
                    <td>{{if .Active}}<span class="pill state-published">active</span>{{else}}<span class="pill state-deleted">inactive</span>{{end}}</td>
                </tr>
            {{else}}
                <tr><td colspan="5" class="empty">No tunnel endpoint snapshot rows found.</td></tr>
            {{end}}
            </tbody>
        </table>
    </div>
</section>
{{end}}
{{end}}
`

const roomAliasesInvitesContent = `
{{define "content"}}
<section class="section section-pad">
    <h1 class="page-title">Aliases &amp; Invites</h1>
    <p class="subtitle">Create/revoke invites and revoke aliases with mode-aware policy enforcement.</p>
` + roomSubnavContent + `
</section>

{{if .ActionMessage}}
<section class="status-strip tone-success" role="status"><h2>Action Complete</h2><p>{{.ActionMessage}}</p></section>
{{end}}
{{if .ActionError}}
<section class="status-strip tone-danger" role="alert"><h2>Action Failed</h2><p>{{.ActionError}}</p></section>
{{end}}

{{if not .Available}}
<section class="status-strip tone-warning"><h2>Room Data Unavailable</h2><p>{{.DegradedReason}}</p></section>
{{else}}
<section class="section section-pad">
    <p class="subtitle">Mode: <strong>{{.ModeLabel}}</strong> · {{.ModeSummary}}</p>
    <p class="subtitle">{{.PolicyHint}}</p>

	    <h2 class="page-title" style="font-size:1.1rem">Create Invite</h2>
	    {{if .CanCreateInvite}}
	    <form method="post" action="/room/invites/create" onsubmit="return confirm('Create a new invite token?')">
	        {{csrfField .Chrome.CSRFToken}}
	        <button type="submit" class="button-link">Create Invite</button>
	    </form>
    {{if .InviteJoinURL}}
    <div class="toolbar" style="justify-content:flex-start">
        <span class="metric-label">New Join URL</span>
        <code class="mono">{{.InviteJoinURL}}</code>
        <button class="copy-btn" data-copy="{{.InviteJoinURL}}">Copy</button>
    </div>
    {{end}}
    {{else}}
    <p class="subtitle">Invite creation is blocked by current mode/role policy.</p>
    {{end}}
</section>

<section class="section section-pad">
    <h2 class="page-title" style="font-size:1.1rem">Invites</h2>
    <div class="table-wrap">
        <table>
            <thead><tr><th>ID</th><th>Status</th><th>Created At</th><th>Created By</th><th>Revoke</th></tr></thead>
            <tbody>
            {{range .Invites}}
                <tr>
                    <td class="mono">{{.ID}}</td>
                    <td>{{if .Active}}<span class="pill state-published">active</span>{{else}}<span class="pill state-deleted">consumed</span>{{end}}</td>
                    <td class="mono">{{.CreatedAt}}</td>
                    <td class="mono">{{.CreatedBy}}</td>
	                    <td>
	                        {{if and $.CanRevokeInvite .Active}}
	                        <form method="post" action="/room/invites/revoke" onsubmit="return confirm('Revoke this invite?')">
	                            {{csrfField $.Chrome.CSRFToken}}
	                            <input type="hidden" name="invite_id" value="{{.ID}}">
	                            <button type="submit" class="button">Revoke</button>
	                        </form>
                        {{else}}
                        <span class="empty">-</span>
                        {{end}}
                    </td>
                </tr>
            {{else}}
                <tr><td colspan="5" class="empty">No invites found.</td></tr>
            {{end}}
            </tbody>
        </table>
    </div>
</section>

<section class="section section-pad">
    <h2 class="page-title" style="font-size:1.1rem">Aliases</h2>
    <div class="table-wrap">
        <table>
            <thead><tr><th>Alias</th><th>Owner Feed</th><th>Reverse PTR</th><th>Revoke</th></tr></thead>
            <tbody>
            {{range .Aliases}}
                <tr>
                    <td class="mono">{{.Name}}</td>
                    <td class="mono"><span class="truncate" title="{{.OwnerFeed}}">{{.OwnerFeed}}</span></td>
                    <td class="mono"><span class="truncate" title="{{.ReversePTR}}">{{.ReversePTR}}</span></td>
	                    <td>
	                        {{if $.CanRevokeAlias}}
	                        <form method="post" action="/room/aliases/revoke" onsubmit="return confirm('Revoke alias {{.Name}}?')">
	                            {{csrfField $.Chrome.CSRFToken}}
	                            <input type="hidden" name="alias" value="{{.Name}}">
	                            <button type="submit" class="button">Revoke</button>
	                        </form>
                        {{else}}
                        <span class="empty">blocked</span>
                        {{end}}
                    </td>
                </tr>
            {{else}}
                <tr><td colspan="4" class="empty">No aliases found.</td></tr>
            {{end}}
            </tbody>
        </table>
    </div>
</section>
{{end}}
{{end}}
`

const roomModerationContent = `
{{define "content"}}
<section class="section section-pad">
    <h1 class="page-title">Moderation</h1>
    <p class="subtitle">Manage denied feed keys. Denied keys block invite consume and room announce behavior.</p>
` + roomSubnavContent + `
</section>

{{if .ActionMessage}}
<section class="status-strip tone-success" role="status"><h2>Action Complete</h2><p>{{.ActionMessage}}</p></section>
{{end}}
{{if .ActionError}}
<section class="status-strip tone-danger" role="alert"><h2>Action Failed</h2><p>{{.ActionError}}</p></section>
{{end}}

{{if not .Available}}
<section class="status-strip tone-warning"><h2>Room Data Unavailable</h2><p>{{.DegradedReason}}</p></section>
{{else}}
<section class="section section-pad">
    <p class="subtitle">Mode: <strong>{{.ModeLabel}}</strong> · {{.ModeSummary}}</p>
    <p class="subtitle">{{.PolicyHint}}</p>

	    <h2 class="page-title" style="font-size:1.1rem">Add Denied Key</h2>
	    {{if .CanMutateDenied}}
	    <form method="post" action="/room/denied/add" class="filter-grid" style="padding:0;grid-template-columns:repeat(auto-fit,minmax(240px,1fr))">
	        {{csrfField .Chrome.CSRFToken}}
	        <label class="field">
	            <span>Feed ID</span>
            <input type="text" name="feed_id" required placeholder="@...ed25519">
        </label>
        <label class="field">
            <span>Comment</span>
            <input type="text" name="comment" placeholder="Reason for deny">
        </label>
        <div class="field">
            <span>&nbsp;</span>
            <button class="button-link" type="submit">Add Denied Key</button>
        </div>
    </form>
    {{else}}
    <p class="subtitle">Denied-key mutations are blocked by current mode/role policy.</p>
    {{end}}
</section>

<section class="section section-pad">
    <h2 class="page-title" style="font-size:1.1rem">Denied Keys</h2>
    <div class="table-wrap">
        <table>
            <thead><tr><th>ID</th><th>Feed</th><th>Comment</th><th>Added At</th><th>Remove</th></tr></thead>
            <tbody>
            {{range .Denied}}
                <tr>
                    <td class="mono">{{.ID}}</td>
                    <td class="mono"><span class="truncate" title="{{.FeedID}}">{{.FeedID}}</span></td>
                    <td>{{.Comment}}</td>
                    <td class="mono">{{.AddedAt}}</td>
	                    <td>
	                        {{if $.CanMutateDenied}}
	                        <form method="post" action="/room/denied/remove" onsubmit="return confirm('Remove denied key {{.FeedID}}?')">
	                            {{csrfField $.Chrome.CSRFToken}}
	                            <input type="hidden" name="denied_id" value="{{.ID}}">
	                            <button type="submit" class="button">Remove</button>
	                        </form>
                        {{else}}
                        <span class="empty">blocked</span>
                        {{end}}
                    </td>
                </tr>
            {{else}}
                <tr><td colspan="5" class="empty">No denied keys configured.</td></tr>
            {{end}}
            </tbody>
        </table>
    </div>
</section>
{{end}}
{{end}}
`

// RoomOverviewData is the template model for room overview.
type RoomOverviewData struct {
	Chrome            PageChrome
	Section           string
	Available         bool
	DegradedReason    string
	ModeLabel         string
	ModeSummary       string
	PolicyHint        string
	OperatorRole      string
	MembersCount      int
	InvitesActive     int
	InvitesTotal      int
	AliasesCount      int
	DeniedCount       int
	AttendantsActive  int
	AttendantsTotal   int
	TunnelsActive     int
	TunnelsTotal      int
	HealthStatus      string
	HealthDetail      string
	StatusEndpointURL string
	ActionMessage     string
	ActionError       string
}

// RoomMemberRow is one member/role row.
type RoomMemberRow struct {
	ID      int64
	FeedID  string
	Role    string
	RoleRaw string
}

// RoomInviteRow is one invite row.
type RoomInviteRow struct {
	ID        int64
	Status    string
	Active    bool
	CreatedBy int64
	CreatedAt string
}

// RoomAliasRow is one alias row.
type RoomAliasRow struct {
	Name       string
	OwnerFeed  string
	ReversePTR string
}

// RoomDeniedKeyRow is one denied-key row.
type RoomDeniedKeyRow struct {
	ID      int64
	FeedID  string
	Comment string
	AddedAt string
}

// RoomAttendantRow is one attendant snapshot row.
type RoomAttendantRow struct {
	FeedID      string
	Addr        string
	ConnectedAt string
	LastSeenAt  string
	Active      bool
}

// RoomTunnelEndpointRow is one tunnel endpoint snapshot row.
type RoomTunnelEndpointRow struct {
	TargetFeed  string
	Addr        string
	AnnouncedAt string
	LastSeenAt  string
	Active      bool
}

// RoomMembersRolesData is the template model for members/roles page.
type RoomMembersRolesData struct {
	Chrome         PageChrome
	Section        string
	Available      bool
	DegradedReason string
	ModeLabel      string
	ModeSummary    string
	PolicyHint     string
	OperatorRole   string
	Members        []RoomMemberRow
	ActionMessage  string
	ActionError    string
}

// RoomAttendantsTunnelsData is the template model for attendants/tunnels page.
type RoomAttendantsTunnelsData struct {
	Chrome         PageChrome
	Section        string
	Available      bool
	DegradedReason string
	ModeLabel      string
	ModeSummary    string
	PolicyHint     string
	OperatorRole   string
	ConnectionsURL string
	Attendants     []RoomAttendantRow
	Tunnels        []RoomTunnelEndpointRow
	ActionMessage  string
	ActionError    string
}

// RoomAliasesInvitesData is the template model for aliases/invites page.
type RoomAliasesInvitesData struct {
	Chrome          PageChrome
	Section         string
	Available       bool
	DegradedReason  string
	ModeLabel       string
	ModeSummary     string
	PolicyHint      string
	OperatorRole    string
	CanCreateInvite bool
	CanRevokeInvite bool
	CanRevokeAlias  bool
	InviteJoinURL   string
	Invites         []RoomInviteRow
	Aliases         []RoomAliasRow
	ActionMessage   string
	ActionError     string
}

// RoomModerationData is the template model for moderation page.
type RoomModerationData struct {
	Chrome          PageChrome
	Section         string
	Available       bool
	DegradedReason  string
	ModeLabel       string
	ModeSummary     string
	PolicyHint      string
	OperatorRole    string
	CanMutateDenied bool
	Denied          []RoomDeniedKeyRow
	ActionMessage   string
	ActionError     string
}

// RenderRoomOverview renders the room overview page.
func RenderRoomOverview(w io.Writer, data RoomOverviewData) error {
	return roomOverviewTemplate.Execute(w, data)
}

// RenderRoomMembersRoles renders the room members/roles page.
func RenderRoomMembersRoles(w io.Writer, data RoomMembersRolesData) error {
	return roomMembersRolesTemplate.Execute(w, data)
}

// RenderRoomAttendantsTunnels renders the room attendants/tunnels page.
func RenderRoomAttendantsTunnels(w io.Writer, data RoomAttendantsTunnelsData) error {
	return roomAttendantsTunnelsTemplate.Execute(w, data)
}

// RenderRoomAliasesInvites renders the room aliases/invites page.
func RenderRoomAliasesInvites(w io.Writer, data RoomAliasesInvitesData) error {
	return roomAliasesInvitesTemplate.Execute(w, data)
}

// RenderRoomModeration renders the room moderation page.
func RenderRoomModeration(w io.Writer, data RoomModerationData) error {
	return roomModerationTemplate.Execute(w, data)
}

var (
	roomOverviewTemplate          = mustPageTemplate("room-overview", roomOverviewContent)
	roomMembersRolesTemplate      = mustPageTemplate("room-members", roomMembersRolesContent)
	roomAttendantsTunnelsTemplate = mustPageTemplate("room-attendants", roomAttendantsTunnelsContent)
	roomAliasesInvitesTemplate    = mustPageTemplate("room-aliases", roomAliasesInvitesContent)
	roomModerationTemplate        = mustPageTemplate("room-moderation", roomModerationContent)
)
