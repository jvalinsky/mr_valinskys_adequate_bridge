package room

import "html/template"

var invitePageTemplate = template.Must(template.New("invite-create").Parse(publicLayoutTemplate + invitePageHTML))
var inviteCreatedTemplate = template.Must(template.New("invite-created").Parse(inviteCreatedHTML))
var joinPageTemplate = template.Must(template.New("join-room").Parse(publicLayoutTemplate + joinPageHTML))
var joinFallbackTemplate = template.Must(template.New("join-fallback").Parse(publicLayoutTemplate + joinFallbackHTML))
var joinManualTemplate = template.Must(template.New("join-manual").Parse(publicLayoutTemplate + joinManualHTML))
var inviteConsumedTemplate = template.Must(template.New("invite-consumed").Parse(publicLayoutTemplate + inviteConsumedHTML))
var inviteManagementTemplate = template.Must(template.New("invite-management").Parse(publicLayoutTemplate + inviteManagementHTML))
var aliasPageTemplate = template.Must(template.New("alias-page").Parse(publicLayoutTemplate + aliasPageHTML))

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

const aliasPageHTML = `
{{define "pageTitle"}}Alias - ATProto to SSB Bridge{{end}}
{{define "content"}}
<div class="hero">
  <h1>Connect with {{.Alias}}</h1>
  <p class="eyebrow">Room Alias</p>
</div>

<div class="panel">
  <p>This alias resolves to the Secure Scuttlebutt identity below.</p>
  <dl>
    <dt>User ID</dt>
    <dd><code>{{.UserID}}</code></dd>
    <dt>Room ID</dt>
    <dd><code>{{.RoomID}}</code></dd>
    <dt>Multiserver Address</dt>
    <dd><code>{{.MultiserverAddress}}</code></dd>
  </dl>
  <p><a class="btn-primary" href="{{.ConsumeURI}}">Connect with me</a></p>
</div>
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
