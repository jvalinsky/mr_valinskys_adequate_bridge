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
{{define "pageTitle"}}Create Invite{{end}}
{{define "content"}}
  <section class="hero hero-grid">
    <div>
      <p class="eyebrow">Invite creation</p>
      <h1>Create a shareable room invite</h1>
      <p class="lead">Generate a one-time invite link, copy it, and share it with the person who should join the room.</p>
      <div class="button-row">
        <a href="/bots" class="btn-secondary">Browse bots</a>
        <a href="/" class="btn-secondary">Back to room</a>
      </div>
    </div>
    <aside class="hero-spotlight">
      <p class="eyebrow">How it works</p>
      <p class="copy-note">Create the link first. The result appears inline on this page, so you can copy it without leaving the flow.</p>
      <p class="copy-note">If invite creation is blocked by room policy, you will be sent back to sign in first.</p>
    </aside>
  </section>

  <section class="panel">
    <div class="section-head">
      <div>
        <p class="eyebrow">Create invite</p>
        <h2>Generate a room link</h2>
      </div>
    </div>

    <form id="inviteForm" method="post">
      <div class="button-row">
        <button type="submit" class="btn-primary" id="inviteCreateButton">Create invite</button>
      </div>
    </form>

    <div id="inviteSuccess" class="notice success" role="status" hidden>
      <div class="notice-title">Invite ready</div>
      <p>Copy the room link below and share it once.</p>
      <div class="copy-grid">
        <input type="text" id="inviteUrl" readonly class="copy-input" />
        <button type="button" class="btn-copy" data-copy-target="inviteUrl" data-copy-label="Copy link">Copy link</button>
      </div>
      <p class="copy-note">The invite link is one-time use.</p>
    </div>

    <div id="inviteError" class="notice error" role="alert" hidden></div>
  </section>

  <script>
  (function () {
    var form = document.getElementById('inviteForm');
    var success = document.getElementById('inviteSuccess');
    var error = document.getElementById('inviteError');
    var urlInput = document.getElementById('inviteUrl');
    var submitButton = document.getElementById('inviteCreateButton');

    if (!form) {
      return;
    }

    form.addEventListener('submit', async function (event) {
      event.preventDefault();
      success.hidden = true;
      error.hidden = true;
      submitButton.disabled = true;
      var originalLabel = submitButton.textContent;
      submitButton.textContent = 'Creating...';

      try {
        var resp = await fetch('/create-invite', {
          method: 'POST',
          headers: { 'Accept': 'application/json' }
        });
        var data = await resp.json();

        if (!resp.ok || data.error) {
          error.textContent = data.error || 'Failed to create invite. Please try again.';
          error.hidden = false;
          return;
        }

        urlInput.value = data.url;
        success.hidden = false;
      } catch (err) {
        error.textContent = 'Failed to create invite. Please try again.';
        error.hidden = false;
      } finally {
        submitButton.disabled = false;
        submitButton.textContent = originalLabel;
      }
    });
  })();
  </script>
{{end}}
`

const aliasPageHTML = `
{{define "pageTitle"}}Alias{{end}}
{{define "content"}}
  <section class="hero hero-grid">
    <div>
      <p class="eyebrow">Room alias</p>
      <h1>Connect with {{.Alias}}</h1>
      <p class="lead">Use the alias details below to connect this ATProto identity to the bridge room.</p>
    </div>
    <aside class="hero-spotlight">
      <p class="eyebrow">Connection summary</p>
      <p class="copy-note">The room ID and multiserver address are provided here for client setup and troubleshooting.</p>
    </aside>
  </section>

  <section class="panel">
    <div class="section-head">
      <div>
        <p class="eyebrow">Alias details</p>
        <h2>Bridge connection values</h2>
      </div>
    </div>
    <div class="stat-list">
      <div class="info-row">
        <dt>User ID</dt>
        <dd><code>{{.UserID}}</code></dd>
      </div>
      <div class="info-row">
        <dt>Room ID</dt>
        <dd><code>{{.RoomID}}</code></dd>
      </div>
      <div class="info-row">
        <dt>Multiserver address</dt>
        <dd><code>{{.MultiserverAddress}}</code></dd>
      </div>
    </div>
    <div class="button-row spaced-actions">
      <a class="btn-primary" href="{{.ConsumeURI}}">Connect with me</a>
    </div>
  </section>
{{end}}
`

const inviteCreatedHTML = `
<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1.0" />
  <title>Invite Ready</title>
  <style>
    :root {
      --bg: #eef3ee;
      --bg-accent: #f6efe2;
      --surface: rgba(255, 255, 255, 0.96);
      --surface-soft: #fbfcfb;
      --text: #10221c;
      --muted: #5a6a61;
      --border: #d8e0db;
      --accent: #17624f;
      --accent-strong: #0f473a;
      --success-bg: #e8f6ec;
      --success-text: #1e5f37;
      --shadow: 0 18px 44px rgba(14, 35, 28, 0.12);
      --shadow-soft: 0 10px 24px rgba(14, 35, 28, 0.08);
    }

    * { box-sizing: border-box; }
    body {
      margin: 0;
      min-height: 100vh;
      color: var(--text);
      font-family: "Avenir Next", "Avenir", "Segoe UI", sans-serif;
      background:
        radial-gradient(circle at top left, rgba(23, 98, 79, 0.12), transparent 34%),
        radial-gradient(circle at top right, rgba(247, 196, 105, 0.2), transparent 28%),
        linear-gradient(180deg, var(--bg-accent) 0%, var(--bg) 100%);
    }
    a { color: var(--accent); text-decoration: none; }
    a:hover { color: var(--accent-strong); }
    code { font-family: "SFMono-Regular", Consolas, "Liberation Mono", Menlo, monospace; }
    .page-shell { width: min(760px, calc(100% - 32px)); margin: 0 auto; padding: 24px 0 40px; }
    .site-header, .hero, .panel {
      background: var(--surface);
      border: 1px solid var(--border);
      border-radius: 24px;
      box-shadow: var(--shadow);
    }
    .site-header { display: flex; justify-content: space-between; gap: 16px; align-items: center; padding: 18px 22px; margin-bottom: 18px; box-shadow: var(--shadow-soft); }
    .brand { font-size: 1rem; font-weight: 700; letter-spacing: -0.02em; }
    .brand-subtitle { margin: 4px 0 0; color: var(--muted); font-size: 0.92rem; }
    .hero, .panel { padding: 24px; }
    .stack { display: grid; gap: 16px; }
    .eyebrow { margin: 0 0 10px; color: var(--muted); font-size: 0.76rem; font-weight: 700; letter-spacing: 0.16em; text-transform: uppercase; }
    h1, h2, p { margin-top: 0; }
    h1 { margin-bottom: 10px; font-size: clamp(2rem, 4.3vw, 3.2rem); line-height: 1.02; letter-spacing: -0.04em; }
    h2 { margin-bottom: 10px; font-size: 1.5rem; }
    .lead, .copy-note { color: var(--muted); line-height: 1.65; }
    .button-row { display: flex; gap: 10px; flex-wrap: wrap; align-items: center; }
    .spaced-actions { margin-top: 16px; }
    .btn-primary, .btn-secondary, .btn-copy {
      display: inline-flex;
      align-items: center;
      justify-content: center;
      min-height: 42px;
      padding: 10px 16px;
      border-radius: 14px;
      border: 1px solid transparent;
      font-size: 0.96rem;
      font-weight: 600;
      cursor: pointer;
      white-space: nowrap;
    }
    .btn-primary { background: var(--accent); color: #fff; }
    .btn-secondary, .btn-copy { background: #fff; border-color: var(--border); color: var(--text); }
    .copy-grid { display: grid; grid-template-columns: minmax(0, 1fr) auto; gap: 10px; align-items: center; }
    .copy-input {
      width: 100%;
      min-height: 44px;
      padding: 12px 14px;
      border: 1px solid var(--border);
      border-radius: 16px;
      background: var(--surface-soft);
      font-family: "SFMono-Regular", Consolas, "Liberation Mono", Menlo, monospace;
      font-size: 0.94rem;
      word-break: break-all;
      overflow-wrap: anywhere;
    }
    .notice {
      padding: 16px 18px;
      border-radius: 18px;
      border: 1px solid transparent;
      box-shadow: var(--shadow-soft);
    }
    .notice.success { background: var(--success-bg); border-color: #c9e7d2; color: var(--success-text); }
    .notice-title { margin-bottom: 6px; font-weight: 700; }
    @media (max-width: 640px) {
      .page-shell { width: min(100%, calc(100% - 18px)); padding: 14px 0 30px; }
      .site-header, .hero, .panel { padding: 18px; border-radius: 20px; }
      .site-header { flex-direction: column; align-items: flex-start; }
      .button-row, .copy-grid { grid-template-columns: 1fr; flex-direction: column; align-items: stretch; }
      .btn-primary, .btn-secondary, .btn-copy { width: 100%; }
    }
  </style>
</head>
<body>
  <div class="page-shell">
    <header class="site-header">
      <div>
        <div class="brand"><a href="/">ATProto to SSB Bridge Room</a></div>
        <p class="brand-subtitle">Invite created successfully</p>
      </div>
      <nav><a href="/create-invite">Create another</a></nav>
    </header>

    <main class="stack">
      <section class="hero">
        <p class="eyebrow">Success</p>
        <h1>Invite ready</h1>
        <p class="lead">Copy the room link below and share it once. The link is one-time use.</p>
      </section>

      <section class="panel">
        <div class="notice success">
          <div class="notice-title">Your invite link is ready to share.</div>
          <p class="copy-note">Keep the link close until the other person has joined.</p>
        </div>
        <div class="copy-grid spaced-actions">
          <input type="text" id="createdInviteUrl" value="{{.URL}}" readonly class="copy-input" />
          <button type="button" class="btn-copy" data-copy-target="createdInviteUrl" data-copy-label="Copy link">Copy link</button>
        </div>
        <div class="button-row spaced-actions">
          <a href="/create-invite" class="btn-primary">Create another invite</a>
          <a href="/" class="btn-secondary">Back to room</a>
        </div>
      </section>
    </main>
  </div>
  <script>
  (function () {
    function copyText(value) {
      if (navigator.clipboard && navigator.clipboard.writeText) {
        return navigator.clipboard.writeText(value);
      }

      var temp = document.createElement('textarea');
      temp.value = value;
      temp.setAttribute('readonly', 'readonly');
      temp.style.position = 'fixed';
      temp.style.left = '-9999px';
      document.body.appendChild(temp);
      temp.select();
      document.execCommand('copy');
      document.body.removeChild(temp);
      return Promise.resolve();
    }

    document.addEventListener('click', function (event) {
      var button = event.target.closest('[data-copy-target]');
      if (!button) {
        return;
      }
      event.preventDefault();
      var target = document.getElementById(button.getAttribute('data-copy-target'));
      if (!target) {
        return;
      }
      var original = button.textContent;
      copyText(target.value || target.textContent || '').then(function () {
        button.textContent = 'Copied';
        window.setTimeout(function () {
          button.textContent = original;
        }, 1400);
      });
    });
  })();
  </script>
</body>
</html>
`

const joinPageHTML = `
{{define "pageTitle"}}Join with an Invite{{end}}
{{define "content"}}
  <section class="hero hero-grid">
    <div>
      <p class="eyebrow">Join flow</p>
      <h1>Join with an invite</h1>
      <p class="lead">Open the deep link in your SSB client first. If the client does not open, use the fallback and manual options below.</p>
      <div class="button-row">
        <a id="claim-invite-uri" class="btn-primary" href="{{.ClaimURI}}">Open in SSB client</a>
        <a href="{{.FallbackURL}}" class="btn-secondary">Fallback instructions</a>
        <a href="{{.ManualURL}}" class="btn-secondary">Manual claim</a>
      </div>
    </div>
    <aside class="hero-spotlight">
      <p class="eyebrow">Invite token</p>
      <p class="copy-note">{{.Token}}</p>
      <p class="copy-note">If the deep link succeeds, the fallback page will not be needed.</p>
    </aside>
  </section>

  <section class="panel">
    <div class="section-head">
      <div>
        <p class="eyebrow">What happens next</p>
        <h2>Pick the path that fits your client</h2>
      </div>
    </div>
    <div class="summary-grid">
      <div class="hero-spotlight">
        <p class="eyebrow">Deep link</p>
        <p class="copy-note">Use this if your client can handle HTTP invite claim links.</p>
      </div>
      <div class="hero-spotlight">
        <p class="eyebrow">Fallback</p>
        <p class="copy-note">Retry the claim link from a browser if the client did not open automatically.</p>
      </div>
      <div class="hero-spotlight">
        <p class="eyebrow">Manual claim</p>
        <p class="copy-note">Paste your feed ID and submit it directly if the client needs a manual step.</p>
      </div>
    </div>
  </section>

  <script>
  setTimeout(function () {
    window.location.href = "{{.FallbackURL}}";
  }, 4500);
  </script>
{{end}}
`

const joinFallbackHTML = `
{{define "pageTitle"}}Invite Fallback{{end}}
{{define "content"}}
  <section class="hero hero-grid">
    <div>
      <p class="eyebrow">Fallback route</p>
      <h1>Deep link did not open?</h1>
      <p class="lead">Retry the claim link or continue with the manual flow below. The invite token is preserved either way.</p>
      <div class="button-row">
        <a class="btn-primary" href="{{.ClaimURL}}">Retry claim link</a>
        <a class="btn-secondary" href="{{.ManualURL}}">Open manual claim</a>
      </div>
    </div>
    <aside class="hero-spotlight">
      <p class="eyebrow">Invite token</p>
      <p class="copy-note">{{.Token}}</p>
      <p class="copy-note">Use this page when the client opens slowly or not at all.</p>
    </aside>
  </section>

  <section class="panel">
    <div class="section-head">
      <div>
        <p class="eyebrow">Next step</p>
        <h2>Manual claim is still available</h2>
      </div>
    </div>
    <div class="button-row">
      <a class="btn-primary" href="{{.ManualURL}}">Continue manually</a>
      <a class="btn-secondary" href="{{.ClaimURL}}">Try the deep link again</a>
    </div>
  </section>
{{end}}
`

const joinManualHTML = `
{{define "pageTitle"}}Manual Claim{{end}}
{{define "content"}}
  <section class="hero hero-grid">
    <div>
      <p class="eyebrow">Manual claim</p>
      <h1>Paste your feed ID to consume the invite</h1>
      <p class="lead">Use this form if your client needs you to enter the feed ID by hand.</p>
    </div>
    <aside class="hero-spotlight">
      <p class="eyebrow">Invite token</p>
      <p class="copy-note">{{.Token}}</p>
      <p class="copy-note">The token stays attached to this form so you can complete the claim without returning to the deep link.</p>
    </aside>
  </section>

  <section class="panel">
    <div class="section-head">
      <div>
        <p class="eyebrow">Consume invite</p>
        <h2>Manual room claim</h2>
      </div>
    </div>
    <form method="post" action="{{.ConsumeTo}}">
      <input type="hidden" name="invite" value="{{.Token}}" />
      <label for="id" class="eyebrow">SSB feed ID</label>
      <input id="id" name="id" type="text" required placeholder="@...ed25519" class="mono-input" />
      <div class="button-row spaced-actions">
        <button type="submit" class="btn-primary">Claim invite</button>
      </div>
    </form>
  </section>
{{end}}
`

const inviteConsumedHTML = `
{{define "pageTitle"}}Invite Claimed{{end}}
{{define "content"}}
  <section class="hero hero-grid">
    <div>
      <p class="eyebrow">Success</p>
      <h1>Invite consumed</h1>
      <p class="lead">Copy the multiserver address below and add it to your SSB client.</p>
    </div>
    <aside class="hero-spotlight">
      <p class="eyebrow">Back to room</p>
      <p class="copy-note">The invite has been used successfully. You can return to the room at any time.</p>
      <a href="{{.HomeURL}}" class="btn-secondary">Back to room</a>
    </aside>
  </section>

  <section class="panel">
    <div class="section-head">
      <div>
        <p class="eyebrow">Connection details</p>
        <h2>Your multiserver address</h2>
      </div>
    </div>
    <div class="copy-grid">
      <input type="text" id="multiserverAddress" value="{{.MultiserverAddress}}" readonly class="copy-input" />
      <button type="button" class="btn-copy" data-copy-target="multiserverAddress" data-copy-label="Copy address">Copy address</button>
    </div>
    <p class="copy-note spaced-actions">Paste the address into your SSB client to connect to the room.</p>
  </section>
{{end}}
`

const inviteManagementHTML = `
{{define "pageTitle"}}Invite Dashboard{{end}}
{{define "content"}}
  <section class="hero hero-grid">
    <div>
      <p class="eyebrow">Invite dashboard</p>
      <h1>Manage room invites</h1>
      <p class="lead">Create new links, review active invites, and revoke any invite that should no longer work.</p>
      <p class="copy-note">{{.PermissionHint}}</p>
    </div>
    <aside class="hero-spotlight">
      <p class="eyebrow">Room mode</p>
      <span class="mode-badge {{if eq .ModeLabel "Open"}}mode-open{{else if eq .ModeLabel "Community"}}mode-community{{else if eq .ModeLabel "Restricted"}}mode-restricted{{else}}mode-unknown{{end}}">{{.ModeLabel}}</span>
      <p class="copy-note">Invite creation and revocation follow the policy for this mode.</p>
    </aside>
  </section>

  {{if .Message}}
  <div class="notice success" role="status">
    <div class="notice-title">{{.Message}}</div>
  </div>
  {{end}}

  {{if .Error}}
  <div class="notice error" role="alert">
    <div class="notice-title">{{.Error}}</div>
  </div>
  {{end}}

  <section class="panel">
    <div class="section-head">
      <div>
        <p class="eyebrow">Create invite</p>
        <h2>Generate a new share link</h2>
      </div>
    </div>
    {{if .CanCreateInvite}}
    <form id="manageInviteCreateForm" method="post" action="/create-invite">
      <div class="button-row">
        <button type="submit" class="btn-primary" id="manageInviteCreateButton">Create invite</button>
      </div>
    </form>
    <div id="manageInviteResult" class="notice success" role="status" hidden>
      <div class="notice-title">Invite ready</div>
      <p>Copy the room link now. Historical invite URLs are not recoverable later.</p>
      <div class="copy-grid">
        <input type="text" id="manageInviteURL" readonly class="copy-input" />
        <button type="button" class="btn-copy" data-copy-target="manageInviteURL" data-copy-label="Copy link">Copy link</button>
      </div>
    </div>
    <div id="manageInviteError" class="notice error" role="alert" hidden></div>
    {{else}}
    <p class="copy-note">Invite creation is not available in this room mode.</p>
    {{end}}
  </section>

  <section class="panel">
    <div class="section-head">
      <div>
        <p class="eyebrow">Active invites</p>
        <h2>{{len .ActiveInvites}} active</h2>
      </div>
    </div>
    <p class="copy-note">Copy links when they are created. The full URL is not stored in historical records.</p>
    {{if .ActiveInvites}}
    <div class="table-shell">
      <table class="invite-table">
        <thead>
          <tr>
            <th>ID</th>
            <th>Status</th>
            <th>Created at</th>
            <th>Creator</th>
            <th>Action</th>
          </tr>
        </thead>
        <tbody>
          {{range .ActiveInvites}}
          <tr>
            <td><code>{{.ID}}</code></td>
            <td>{{.Status}}</td>
            <td><code>{{.CreatedAt}}</code></td>
            <td><code>{{.CreatedBy}}</code></td>
            <td>
              {{if $.CanRevokeInvite}}
              <form method="post" action="/invites/revoke">
                <input type="hidden" name="id" value="{{.ID}}" />
                <button type="submit" class="btn-secondary">Revoke</button>
              </form>
              {{else}}
              <span class="copy-note">No revoke access</span>
              {{end}}
            </td>
          </tr>
          {{end}}
        </tbody>
      </table>
    </div>
    {{else}}
    <p class="copy-note">No active invites.</p>
    {{end}}
  </section>

  <section class="panel">
    <div class="section-head">
      <div>
        <p class="eyebrow">Consumed and revoked</p>
        <h2>{{len .InactiveInvites}} archived</h2>
      </div>
    </div>
    {{if .InactiveInvites}}
    <div class="table-shell">
      <table class="invite-table">
        <thead>
          <tr>
            <th>ID</th>
            <th>Status</th>
            <th>Created at</th>
            <th>Creator</th>
          </tr>
        </thead>
        <tbody>
          {{range .InactiveInvites}}
          <tr>
            <td><code>{{.ID}}</code></td>
            <td>{{.Status}}</td>
            <td><code>{{.CreatedAt}}</code></td>
            <td><code>{{.CreatedBy}}</code></td>
          </tr>
          {{end}}
        </tbody>
      </table>
    </div>
    {{else}}
    <p class="copy-note">No consumed or revoked invites yet.</p>
    {{end}}
  </section>

  {{if .CanCreateInvite}}
  <script>
  (function () {
    var form = document.getElementById('manageInviteCreateForm');
    var success = document.getElementById('manageInviteResult');
    var error = document.getElementById('manageInviteError');
    var urlInput = document.getElementById('manageInviteURL');
    var submitButton = document.getElementById('manageInviteCreateButton');

    if (!form) {
      return;
    }

    form.addEventListener('submit', async function (event) {
      event.preventDefault();
      success.hidden = true;
      error.hidden = true;
      submitButton.disabled = true;
      var originalLabel = submitButton.textContent;
      submitButton.textContent = 'Creating...';

      try {
        var resp = await fetch('/create-invite', {
          method: 'POST',
          headers: { 'Accept': 'application/json' }
        });
        var data = await resp.json();

        if (!resp.ok || data.error) {
          error.textContent = data.error || 'Failed to create invite. Please try again.';
          error.hidden = false;
          return;
        }

        urlInput.value = data.url;
        success.hidden = false;
      } catch (err) {
        error.textContent = 'Failed to create invite. Please try again.';
        error.hidden = false;
      } finally {
        submitButton.disabled = false;
        submitButton.textContent = originalLabel;
      }
    });
  })();
  </script>
  {{end}}
{{end}}
`
