package room

import "html/template"

var templateFuncs = template.FuncMap{
	"abbreviateDID":  abbreviateDID,
	"abbreviateFeed": abbreviateFeed,
}

var landingTemplate = template.Must(template.New("room-landing").Funcs(templateFuncs).Parse(publicLayoutTemplate + landingContentTemplate))
var botsTemplate = template.Must(template.New("room-bots").Funcs(templateFuncs).Parse(publicLayoutTemplate + botsContentTemplate))
var botDetailTemplate = template.Must(template.New("room-bot-detail").Funcs(templateFuncs).Parse(publicLayoutTemplate + botDetailContentTemplate))

const publicLayoutTemplate = `
<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1.0" />
  <title>{{template "pageTitle" .}}</title>
  <link rel="icon" type="image/png" sizes="32x32" href="/assets/favicon/favicon-32x32.png">
  <style>
    :root {
      --bg: #eef3ee;
      --bg-accent: #f6efe2;
      --surface: rgba(255, 255, 255, 0.94);
      --surface-soft: #fbfcfb;
      --surface-tint: #f3f7f4;
      --text: #10221c;
      --muted: #5a6a61;
      --border: #d8e0db;
      --accent: #17624f;
      --accent-strong: #0f473a;
      --accent-soft: #ddeee7;
      --success-bg: #e8f6ec;
      --success-text: #1e5f37;
      --warning-bg: #fdf2d7;
      --warning-text: #7f5d10;
      --danger-bg: #fae4e5;
      --danger-text: #8a2132;
      --shadow: 0 18px 44px rgba(14, 35, 28, 0.12);
      --shadow-soft: 0 10px 24px rgba(14, 35, 28, 0.08);
    }

    * {
      box-sizing: border-box;
    }

    html {
      color-scheme: light;
    }

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

    a {
      color: var(--accent);
    }

    a:hover {
      color: var(--accent-strong);
    }

    code,
    pre {
      font-family: "SFMono-Regular", Consolas, "Liberation Mono", Menlo, monospace;
    }

    .page-shell {
      width: min(1180px, calc(100% - 32px));
      margin: 0 auto;
      padding: 24px 0 40px;
    }

    .site-header {
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 16px;
      padding: 18px 22px;
      margin-bottom: 22px;
      border: 1px solid rgba(16, 34, 28, 0.08);
      border-radius: 24px;
      background: rgba(255, 255, 255, 0.82);
      box-shadow: var(--shadow-soft);
      backdrop-filter: blur(12px);
    }

    .brand {
      font-size: 1rem;
      font-weight: 700;
      letter-spacing: -0.02em;
    }

    .brand-subtitle {
      margin: 4px 0 0;
      color: var(--muted);
      font-size: 0.92rem;
    }

    .site-nav {
      display: flex;
      align-items: center;
      justify-content: flex-end;
      gap: 8px;
      flex-wrap: wrap;
    }

    .site-nav a {
      display: inline-flex;
      align-items: center;
      justify-content: center;
      min-height: 40px;
      padding: 8px 14px;
      border-radius: 999px;
      text-decoration: none;
      background: rgba(23, 98, 79, 0.08);
      color: var(--accent-strong);
      border: 1px solid transparent;
    }

    .site-nav a:hover {
      border-color: rgba(23, 98, 79, 0.14);
      background: rgba(23, 98, 79, 0.12);
    }

    .page-stack {
      display: grid;
      gap: 16px;
    }

    .hero,
    .panel,
    .page-title-wide,
    .bot-card,
    details.panel {
      background: var(--surface);
      border: 1px solid var(--border);
      border-radius: 24px;
      box-shadow: var(--shadow);
    }

    .hero,
    .panel,
    .page-title-wide,
    details.panel {
      padding: 24px;
    }

    .hero-grid,
    .detail-grid,
    .summary-grid {
      display: grid;
      gap: 16px;
    }

    .hero-grid {
      grid-template-columns: minmax(0, 1.4fr) minmax(260px, 0.9fr);
      align-items: start;
    }

    .detail-grid {
      grid-template-columns: minmax(0, 1.2fr) minmax(280px, 0.8fr);
      align-items: start;
    }

    .summary-grid {
      grid-template-columns: repeat(3, minmax(0, 1fr));
    }

    .eyebrow {
      margin: 0 0 10px;
      color: var(--muted);
      font-size: 0.76rem;
      font-weight: 700;
      letter-spacing: 0.16em;
      text-transform: uppercase;
    }

    h1,
    h2,
    h3,
    p,
    dl {
      margin-top: 0;
    }

    h1 {
      margin-bottom: 12px;
      font-size: clamp(2rem, 4.4vw, 3.45rem);
      line-height: 1.02;
      letter-spacing: -0.04em;
    }

    h2 {
      margin-bottom: 10px;
      font-size: clamp(1.3rem, 2.1vw, 1.65rem);
      line-height: 1.15;
      letter-spacing: -0.02em;
    }

    h3 {
      margin-bottom: 8px;
      font-size: 1.05rem;
      line-height: 1.2;
    }

    .lead,
    .copy-note,
    .help-text,
    .mode-summary,
    .page-header-copy,
    .card-copy {
      color: var(--muted);
      line-height: 1.65;
    }

    .lead {
      max-width: 70ch;
      font-size: 1.04rem;
    }

    .button-row,
    .action-row-compact,
    .page-header-actions {
      display: flex;
      gap: 10px;
      flex-wrap: wrap;
      align-items: center;
    }

    .spaced-actions {
      margin-top: 16px;
    }

    .button-row {
      margin-top: 18px;
    }

    .action-row-compact,
    .page-header-actions {
      margin-bottom: 0;
    }

    .btn-primary,
    .btn-secondary,
    .btn-copy,
    .btn-small,
    .chip {
      display: inline-flex;
      align-items: center;
      justify-content: center;
      min-height: 42px;
      padding: 10px 16px;
      border-radius: 14px;
      border: 1px solid transparent;
      font-size: 0.96rem;
      font-weight: 600;
      text-decoration: none;
      transition: transform 0.15s ease, box-shadow 0.15s ease, background 0.15s ease, border-color 0.15s ease;
      cursor: pointer;
      white-space: nowrap;
    }

    .btn-primary:hover,
    .btn-secondary:hover,
    .btn-copy:hover,
    .btn-small:hover,
    .chip:hover {
      transform: translateY(-1px);
    }

    .btn-primary {
      background: var(--accent);
      color: #fff;
      box-shadow: 0 10px 20px rgba(23, 98, 79, 0.18);
    }

    .btn-primary:hover {
      background: var(--accent-strong);
      color: #fff;
    }

    .btn-secondary,
    .btn-copy,
    .btn-small {
      background: #fff;
      border-color: var(--border);
      color: var(--text);
      box-shadow: none;
    }

    .btn-secondary:hover,
    .btn-copy:hover,
    .btn-small:hover {
      border-color: rgba(23, 98, 79, 0.22);
      color: var(--accent-strong);
    }

    .btn-small {
      min-height: 38px;
      padding: 8px 12px;
      border-radius: 12px;
      font-size: 0.9rem;
    }

    .btn-copy[disabled],
    .btn-primary[disabled] {
      cursor: progress;
      opacity: 0.72;
      transform: none;
    }

    .mode-badge,
    .chip,
    .stat-pill {
      display: inline-flex;
      align-items: center;
      gap: 8px;
      border-radius: 999px;
      font-weight: 700;
    }

    .mode-badge {
      min-height: 34px;
      padding: 6px 12px;
      font-size: 0.86rem;
      letter-spacing: 0.02em;
      background: var(--accent-soft);
      color: var(--accent-strong);
    }

    .mode-badge.mode-community {
      background: #edf2fb;
      color: #25416b;
    }

    .mode-badge.mode-restricted {
      background: #f3ebf8;
      color: #5d2f72;
    }

    .mode-badge.mode-unknown {
      background: #f5f5f5;
      color: #505050;
    }

    .hero-spotlight {
      display: grid;
      gap: 14px;
      padding: 18px;
      border-radius: 20px;
      background: linear-gradient(180deg, rgba(23, 98, 79, 0.06), rgba(23, 98, 79, 0.02));
      border: 1px solid rgba(23, 98, 79, 0.12);
    }

    .hero-spotlight p {
      margin-bottom: 0;
    }

    .stat-list {
      display: grid;
      gap: 12px;
      margin: 18px 0 0;
    }

    .stat-item,
    .info-row {
      padding-top: 12px;
      border-top: 1px solid rgba(16, 34, 28, 0.08);
    }

    .stat-item dt,
    .info-row dt {
      color: var(--muted);
      font-size: 0.88rem;
      font-weight: 700;
      margin-bottom: 6px;
    }

    .stat-item dd,
    .info-row dd {
      margin: 0;
      font-size: 1.02rem;
      font-weight: 700;
      word-break: break-word;
    }

    .section-head {
      display: flex;
      align-items: flex-start;
      justify-content: space-between;
      gap: 16px;
      margin-bottom: 16px;
      flex-wrap: wrap;
    }

    .directory-actions form {
      display: flex;
      gap: 10px;
      flex-wrap: wrap;
      width: 100%;
    }

    .directory-actions input,
    .directory-actions select,
    .copy-input,
    .message-input,
    .invite-input,
    .mono-input {
      width: 100%;
      min-height: 44px;
      padding: 12px 14px;
      border: 1px solid var(--border);
      border-radius: 16px;
      background: var(--surface-soft);
      color: var(--text);
      font: inherit;
    }

    .directory-actions input,
    .directory-actions select {
      flex: 1 1 220px;
    }

    .directory-actions button {
      min-height: 44px;
      padding: 0 18px;
      border: 1px solid transparent;
      border-radius: 16px;
      background: var(--accent);
      color: #fff;
      font-weight: 700;
      cursor: pointer;
    }

    .directory-actions button:hover {
      background: var(--accent-strong);
    }

    .copy-grid {
      display: grid;
      grid-template-columns: minmax(0, 1fr) auto;
      gap: 10px;
      align-items: center;
    }

    .copy-input,
    .invite-input,
    .mono-input {
      font-family: "SFMono-Regular", Consolas, "Liberation Mono", Menlo, monospace;
      font-size: 0.94rem;
      word-break: break-all;
      overflow-wrap: anywhere;
    }

    .copy-input[readonly],
    .invite-input[readonly] {
      cursor: text;
      background: #f8fbf9;
    }

    .bot-grid {
      display: grid;
      grid-template-columns: repeat(auto-fit, minmax(260px, 1fr));
      gap: 16px;
    }

    a.bot-card {
      display: grid;
      gap: 12px;
      padding: 20px;
      text-decoration: none;
      color: inherit;
      transition: transform 0.16s ease, box-shadow 0.16s ease, border-color 0.16s ease;
    }

    a.bot-card:hover {
      transform: translateY(-2px);
      border-color: rgba(23, 98, 79, 0.14);
      box-shadow: 0 22px 36px rgba(14, 35, 28, 0.15);
    }

    .bot-card h2 {
      margin: 0;
      font-size: 1.08rem;
      word-break: break-word;
    }

    .bot-card-meta,
    .bot-card-feed,
    .message-card p,
    .page-header-copy,
    .card-copy {
      margin-bottom: 0;
      word-break: break-word;
      overflow-wrap: anywhere;
    }

    .stats-bar {
      display: flex;
      flex-wrap: wrap;
      gap: 8px;
    }

    .stat-pill {
      min-height: 30px;
      padding: 5px 10px;
      border-radius: 999px;
      font-size: 0.8rem;
      background: var(--surface-tint);
      color: var(--text);
    }

    .stat-total {
      background: rgba(23, 98, 79, 0.1);
      color: var(--accent-strong);
    }

    .stat-published {
      background: var(--success-bg);
      color: var(--success-text);
    }

    .stat-failed {
      background: var(--danger-bg);
      color: var(--danger-text);
    }

    .stat-deferred {
      background: var(--warning-bg);
      color: var(--warning-text);
    }

    .table-shell {
      overflow-x: auto;
      border: 1px solid var(--border);
      border-radius: 20px;
      background: #fff;
    }

    .invite-table {
      width: 100%;
      min-width: 760px;
      border-collapse: collapse;
    }

    .invite-table th,
    .invite-table td {
      padding: 14px 16px;
      border-bottom: 1px solid #e8eeea;
      text-align: left;
      vertical-align: top;
    }

    .invite-table thead th {
      background: #f8fbf9;
      color: var(--muted);
      font-size: 0.82rem;
      text-transform: uppercase;
      letter-spacing: 0.08em;
    }

    .invite-table tbody tr:last-child td {
      border-bottom: none;
    }

    .invite-table code,
    .code-block,
    .message-card code {
      overflow-wrap: anywhere;
      word-break: break-all;
    }

    .notice {
      padding: 16px 18px;
      border-radius: 18px;
      border: 1px solid transparent;
      box-shadow: var(--shadow-soft);
    }

    .notice.success {
      background: var(--success-bg);
      border-color: #c9e7d2;
      color: var(--success-text);
    }

    .notice.error {
      background: var(--danger-bg);
      border-color: #f0c4c7;
      color: var(--danger-text);
    }

    .notice.info {
      background: #eef5fb;
      border-color: #d7e6f4;
      color: #274765;
    }

    .notice-title {
      margin-bottom: 6px;
      font-weight: 700;
    }

    .page-header-main {
      display: grid;
      gap: 6px;
    }

    .page-header-main .back-link {
      margin-bottom: 6px;
      display: inline-flex;
      width: fit-content;
    }

    .page-title-wide {
      display: grid;
      gap: 12px;
    }

    .page-title-wide code,
    .code-block,
    .copy-value {
      display: block;
      padding: 14px 16px;
      border-radius: 18px;
      border: 1px solid var(--border);
      background: #f8fbf9;
      overflow-x: auto;
      white-space: nowrap;
      text-overflow: ellipsis;
    }

    .copy-value {
      font-weight: 700;
    }

    .message-list {
      display: grid;
      gap: 12px;
    }

    .message-card {
      padding: 16px 18px;
      border-radius: 18px;
      border: 1px solid var(--border);
      background: linear-gradient(180deg, rgba(255, 255, 255, 0.98), rgba(243, 247, 244, 0.92));
    }

    .message-card h3 {
      margin-bottom: 6px;
    }

    .raw-payload {
      margin-top: 12px;
      padding: 16px;
      border-radius: 18px;
      border: 1px solid #1f312a;
      background: #0f1b17;
      color: #dfeee6;
      overflow-x: auto;
    }

    .raw-payload h4 {
      margin-bottom: 12px;
      color: #f3faf6;
    }

    .raw-payload pre {
      margin: 0;
      white-space: pre-wrap;
      word-break: break-word;
    }

    details.panel summary {
      cursor: pointer;
      font-weight: 700;
      list-style: none;
    }

    details.panel summary::-webkit-details-marker {
      display: none;
    }

    .visually-hidden {
      position: absolute;
      width: 1px;
      height: 1px;
      padding: 0;
      margin: -1px;
      overflow: hidden;
      clip: rect(0, 0, 0, 0);
      white-space: nowrap;
      border: 0;
    }

    @media (max-width: 920px) {
      .hero-grid,
      .detail-grid,
      .summary-grid {
        grid-template-columns: 1fr;
      }

      .site-header {
        align-items: flex-start;
        flex-direction: column;
      }

      .site-nav {
        justify-content: flex-start;
      }
    }

    @media (max-width: 700px) {
      .page-shell {
        width: min(100%, calc(100% - 18px));
        padding: 14px 0 30px;
      }

      .hero,
      .panel,
      .page-title-wide,
      details.panel {
        padding: 18px;
        border-radius: 20px;
      }

      .site-header {
        padding: 16px 18px;
        border-radius: 20px;
      }

      .button-row,
      .action-row-compact,
      .page-header-actions,
      .copy-grid,
      .directory-actions form {
        grid-template-columns: 1fr;
        flex-direction: column;
        align-items: stretch;
      }

      .btn-primary,
      .btn-secondary,
      .btn-copy,
      .btn-small,
      .site-nav a,
      .directory-actions button {
        width: 100%;
      }

      .directory-actions input,
      .directory-actions select,
      .copy-input,
      .invite-input,
      .mono-input {
        width: 100%;
      }

      .invite-table {
        min-width: 700px;
      }
    }
  </style>
</head>
<body>
  <div class="page-shell">
    <header class="site-header">
      <div>
        <div class="brand">ATProto to SSB Bridge Room</div>
        <p class="brand-subtitle">Public room and invite console</p>
      </div>
      <nav class="site-nav">
        <a href="/">Room</a>
        <a href="/bots">Bots</a>
        {{if .ShowInvitesNav}}<a href="/invites">Invites</a>{{end}}
        <a href="/login">Sign In</a>
      </nav>
    </header>
    <main class="page-stack">
      {{template "content" .}}
    </main>
  </div>
  <script>
  (function () {
    function copyText(value) {
      if (navigator.clipboard && navigator.clipboard.writeText) {
        return navigator.clipboard.writeText(value);
      }

      return new Promise(function (resolve, reject) {
        var temp = document.createElement('textarea');
        temp.value = value;
        temp.setAttribute('readonly', 'readonly');
        temp.style.position = 'fixed';
        temp.style.left = '-9999px';
        temp.style.top = '0';
        document.body.appendChild(temp);
        temp.select();
        try {
          var ok = document.execCommand('copy');
          document.body.removeChild(temp);
          if (ok) {
            resolve();
            return;
          }
        } catch (err) {
          document.body.removeChild(temp);
          reject(err);
          return;
        }
        reject(new Error('copy failed'));
      });
    }

    function copyValueForButton(button) {
      var targetId = button.getAttribute('data-copy-target');
      if (targetId) {
        var target = document.getElementById(targetId);
        if (!target) {
          return '';
        }
        if (typeof target.value === 'string') {
          return target.value;
        }
        return (target.textContent || '').trim();
      }
      return button.getAttribute('data-copy-value') || '';
    }

    function updateButton(button, label, resetLabel) {
      button.textContent = label;
      window.setTimeout(function () {
        button.textContent = resetLabel;
      }, 1400);
    }

    document.addEventListener('click', function (event) {
      var button = event.target.closest('[data-copy-target], [data-copy-value]');
      if (!button) {
        return;
      }
      event.preventDefault();

      var value = copyValueForButton(button);
      if (!value) {
        updateButton(button, 'Nothing to copy', button.getAttribute('data-copy-label') || button.textContent);
        return;
      }

      var original = button.getAttribute('data-copy-label') || button.textContent;
      copyText(value).then(function () {
        updateButton(button, 'Copied', original);
      }).catch(function () {
        updateButton(button, 'Copy failed', original);
      });
    });
  })();
  </script>
</body>
</html>
`

const landingContentTemplate = `
{{define "pageTitle"}}Bridge Room{{end}}
{{define "content"}}
  <section class="hero hero-grid">
    <div>
      <p class="eyebrow">Public room</p>
      <h1>{{if .Mode.CanSelfServeInvite}}Open room: anyone can create an invite{{else}}Invite creation requires sign-in{{end}}</h1>
      <p class="lead">{{if .Mode.CanSelfServeInvite}}Anyone visiting this room can create a shareable invite and join through the stock flow.{{else}}{{.Mode.Summary}}{{end}}</p>
      <div class="button-row">
        {{if .Mode.CanSelfServeInvite}}
        <a href="{{.InviteURL}}" class="btn-primary">Create invite</a>
        {{else}}
        <a href="{{.SignInURL}}" class="btn-primary">Sign in to create invite</a>
        {{end}}
        <a href="{{.BotsURL}}" class="btn-secondary">Browse bridged bots</a>
      </div>
    </div>
    <aside class="hero-spotlight">
      <p class="eyebrow">Room mode</p>
      <span class="mode-badge {{if eq .Mode.Label "Open"}}mode-open{{else if eq .Mode.Label "Community"}}mode-community{{else if eq .Mode.Label "Restricted"}}mode-restricted{{else}}mode-unknown{{end}}">{{.Mode.Label}}</span>
      <p class="mode-summary">{{.Mode.Summary}}</p>
      <dl class="stat-list">
        <div class="stat-item">
          <dt>Invite access</dt>
          <dd>{{if .Mode.CanSelfServeInvite}}Open to everyone{{else}}Signed-in members only{{end}}</dd>
        </div>
        <div class="stat-item">
          <dt>Active bots</dt>
          <dd>{{.BotCount}}</dd>
        </div>
      </dl>
    </aside>
  </section>

  <section class="panel">
    <div class="section-head">
      <div>
        <p class="eyebrow">Directory snapshot</p>
        <h2>{{.BotCount}} active bridged bot{{if ne .BotCount 1}}s{{end}}</h2>
      </div>
      <a href="/bots" class="btn-small">Open directory</a>
    </div>
    <p class="copy-note">Browse active bots, inspect feed IDs, and open individual message histories.</p>
  </section>
{{end}}
`

const botsContentTemplate = `
{{define "pageTitle"}}Bridged Bots{{end}}
{{define "content"}}
  <header class="page-header-main">
    <p class="eyebrow">Directory</p>
    <h1>Bridged bots</h1>
    <p class="page-header-copy">Search the live directory by DID, feed ID, or feed URI.</p>
  </header>

  <section class="panel directory-actions">
    <form method="get" action="/bots">
      <input type="search" name="q" placeholder="Search DID, feed ID, or URI" value="{{.Query}}">
      <select name="sort">
        <option value="activity_desc" {{if eq .Sort "activity_desc"}}selected{{end}}>Most active</option>
        <option value="newest" {{if eq .Sort "newest"}}selected{{end}}>Newest bridged</option>
        <option value="deferred_desc" {{if eq .Sort "deferred_desc"}}selected{{end}}>Most deferred</option>
      </select>
      <button type="submit">Search</button>
    </form>
  </section>

  <div class="action-row-compact directory-actions">
    {{if .Mode.CanSelfServeInvite}}
    <a href="/create-invite" class="btn-primary">Create invite</a>
    {{else}}
    <a href="/login" class="btn-primary">Sign in to create invite</a>
    {{end}}
  </div>

  {{if .Bots}}
    <div class="bot-grid">
      {{range .Bots}}
        <a class="bot-card" href="{{.DetailURL}}">
          <div>
            <p class="eyebrow">Bot</p>
            <h2>{{abbreviateDID .ATDID}}</h2>
            <p class="bot-card-meta">DID: {{.ATDID}}</p>
            <p class="bot-card-feed">Feed: {{abbreviateFeed .SSBFeedID}}</p>
          </div>
          <div class="stats-bar">
            <span class="stat-pill stat-total">{{.TotalMessages}} msgs</span>
            <span class="stat-pill stat-published">{{.PublishedMessages}} published</span>
            {{if gt .FailedMessages 0}}<span class="stat-pill stat-failed">{{.FailedMessages}} failed</span>{{end}}
            {{if gt .DeferredMessages 0}}<span class="stat-pill stat-deferred">{{.DeferredMessages}} deferred</span>{{end}}
          </div>
          <span class="btn-small">View details</span>
        </a>
      {{end}}
    </div>
  {{else}}
    <section class="panel">
      <p class="copy-note">No active bridged bots yet.</p>
    </section>
  {{end}}
{{end}}
`

const botDetailContentTemplate = `
{{define "pageTitle"}}Bot · {{abbreviateDID .Bot.ATDID}}{{end}}
{{define "content"}}
  <header class="page-header-main">
    <a class="back-link" href="/bots">← Back to directory</a>
    <p class="eyebrow">Bot detail</p>
    <h1>{{.Bot.ATDID}}</h1>
    <p class="page-header-copy">Copy the bot identifiers, open the feed URI, and review the latest published messages.</p>
  </header>

  <section class="detail-grid">
    <div class="panel">
      <div class="page-title-wide">
        <p class="eyebrow">Bridge feed</p>
        <h2>SSB feed identifier</h2>
        <code>{{.Bot.SSBFeedID}}</code>
      </div>
      <div class="page-header-actions action-row-compact spaced-actions">
        <button type="button" class="btn-copy" data-copy-value="{{.Bot.ATDID}}" data-copy-label="Copy DID">Copy DID</button>
        <button type="button" class="btn-copy" data-copy-value="{{.Bot.SSBFeedID}}" data-copy-label="Copy feed ID">Copy feed ID</button>
        <button type="button" class="btn-copy" data-copy-value="{{.Bot.FeedURI}}" data-copy-label="Copy feed URI">Copy feed URI</button>
        <a href="{{.Bot.FeedHref}}" class="btn-secondary">Open feed URI</a>
      </div>
    </div>

    <section class="panel">
      <p class="eyebrow">Bridge summary</p>
      <dl class="stat-list">
        <div class="info-row">
          <dt>ATProto DID</dt>
          <dd><code>{{.Bot.ATDID}}</code></dd>
        </div>
        <div class="info-row">
          <dt>Feed URI</dt>
          <dd><code>{{.Bot.FeedURI}}</code></dd>
        </div>
        <div class="info-row">
          <dt>Messages published</dt>
          <dd>{{.Bot.PublishedMessages}}</dd>
        </div>
      </dl>
    </section>
  </section>

  <section class="panel">
    <div class="section-head">
      <div>
        <p class="eyebrow">Published messages</p>
        <h2>Latest bridged posts</h2>
      </div>
    </div>
    {{if .PublishedMessages}}
    <div class="message-list">
      {{range .PublishedMessages}}
      <article class="message-card">
        <h3>{{.Type}}</h3>
        <p>{{if .PublishedAt}}Published {{.PublishedAt}}{{end}}{{if and .PublishedAt .SSBMsgRef}} · {{end}}{{if .SSBMsgRef}}SSB {{.SSBMsgRef}}{{end}}</p>
        {{if .ATURI}}<p class="copy-note">AT URI: <code>{{.ATURI}}</code></p>{{end}}
      </article>
      {{end}}
    </div>
    {{else}}
    <p class="copy-note">No published messages yet.</p>
    {{end}}
  </section>

  <details class="panel">
    <summary>Show stored payloads</summary>
    {{range .PublishedMessages}}
    {{if .HasRawATProto}}
    <div class="raw-payload">
      <h4>ATProto source</h4>
      <pre>{{.RawATProtoJSON}}</pre>
    </div>
    {{end}}
    {{if .HasRawSSB}}
    <div class="raw-payload">
      <h4>SSB bridge record</h4>
      <pre>{{.RawSSBJSON}}</pre>
    </div>
    {{end}}
    {{end}}
  </details>
{{end}}
`
