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
      --paper: #f3ebdd;
      --accent: #0d7f64;
    }
    body {
      font-family: system-ui, sans-serif;
      margin: 0;
      min-height: 100vh;
      background: var(--paper);
      color: #132820;
    }
    .page-shell {
      max-width: 1200px;
      margin: 0 auto;
      padding: 24px;
    }
    .topbar {
      display: flex;
      justify-content: space-between;
      align-items: center;
      margin-bottom: 24px;
    }
    .brand { font-weight: bold; }
    nav a {
      margin-left: 16px;
      color: var(--accent);
      text-decoration: none;
    }
    .hero, .panel, .bot-card, .page-title-wide {
      background: white;
      border-radius: 12px;
      padding: 24px;
      margin-bottom: 24px;
      box-shadow: 0 2px 8px rgba(0,0,0,0.1);
    }
    .page-header-main { margin-bottom: 24px; }
    .page-header-actions { margin-bottom: 24px; display: flex; gap: 8px; flex-wrap: wrap; }
    .action-row-compact { margin-bottom: 24px; display: flex; gap: 8px; flex-wrap: wrap; }
    .eyebrow { color: #666; font-size: 0.85em; text-transform: uppercase; letter-spacing: 0.05em; }
    .bot-grid {
      display: grid;
      grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
      gap: 16px;
    }
    .stats-bar { margin-top: 12px; }
    .stat-pill {
      display: inline-block;
      padding: 4px 8px;
      border-radius: 4px;
      font-size: 0.85em;
      margin-right: 8px;
    }
    .stat-total { background: #eee; }
    .stat-published { background: #d4edda; color: #155724; }
    .stat-failed { background: #f8d7da; color: #721c24; }
    .stat-deferred { background: #fff3cd; color: #856404; }
    a.bot-card { text-decoration: none; color: inherit; display: block; }
    a.bot-card:hover { box-shadow: 0 4px 16px rgba(0,0,0,0.15); }
    .btn-primary, .btn-secondary, .btn-copy, .btn-small {
      padding: 8px 16px;
      border-radius: 6px;
      border: none;
      cursor: pointer;
      font-size: 0.9em;
    }
    .btn-primary { background: var(--accent); color: white; text-decoration: none; }
    .btn-secondary { background: #e0e0e0; color: #333; text-decoration: none; }
    .btn-copy { background: #f5f5f5; border: 1px solid #ddd; }
    .btn-small { background: var(--accent); color: white; font-size: 0.8em; }
    .directory-actions form { display: flex; gap: 8px; }
    .directory-actions input, .directory-actions select { padding: 8px; border: 1px solid #ddd; border-radius: 4px; }
    .directory-actions button { padding: 8px 16px; background: var(--accent); color: white; border: none; border-radius: 4px; cursor: pointer; }
    .message-card { background: #f9f9f9; padding: 12px; margin-bottom: 12px; border-radius: 6px; }
    .raw-payload { background: #f5f5f5; padding: 16px; margin: 12px 0; border-radius: 6px; overflow-x: auto; }
    .raw-payload pre { margin: 0; font-size: 0.85em; }
    details.panel { background: white; border-radius: 12px; padding: 24px; margin-bottom: 24px; box-shadow: 0 2px 8px rgba(0,0,0,0.1); }
    details.panel summary { cursor: pointer; font-weight: bold; }
  </style>
</head>
<body>
  <div class="page-shell">
    <header class="topbar">
      <div class="brand">ATProto to SSB Bridge Room</div>
      <nav>
        <a href="/">Room</a>
        <a href="/bots">Bots</a>
        {{if .ShowInvitesNav}}<a href="/invites">Invites</a>{{end}}
        <a href="/login">Sign In</a>
      </nav>
    </header>
    {{template "content" .}}
  </div>
</body>
</html>
`

const landingContentTemplate = `
{{define "pageTitle"}}Bridge Room{{end}}
{{define "content"}}
  <section class="hero">
    {{if .Mode.CanSelfServeInvite}}
    <h1>Create room invite</h1>
    <p>Anyone visiting this page can create a room invite.</p>
    {{else}}
    <h1>Self-serve invites disabled</h1>
    <p>Existing room members can sign in to create invites.</p>
    {{end}}
    <p>Mode: {{.Mode.Label}}</p>
    <a href="/bots">Browse bridged bots</a>
    {{if .Mode.CanSelfServeInvite}}
    <a href="/create-invite">Open room sign-in</a>
    {{end}}
  </section>
  <section class="panel">
    <h2>{{.BotCount}} active bridged bot{{if ne .BotCount 1}}s{{end}} currently listed in the directory.</h2>
    <a href="/bots">Browse bridged bots</a>
  </section>
{{end}}
`

const botsContentTemplate = `
{{define "pageTitle"}}Bridged Bots{{end}}
{{define "content"}}
  <header class="page-header-main">
    <h1>Bridged Bots</h1>
  </header>
  <header class="page-header-actions directory-actions">
    <form method="get" action="/bots">
      <input type="search" name="q" placeholder="Search DID/feed" value="{{.Query}}">
      <select name="sort">
        <option value="activity_desc" {{if eq .Sort "activity_desc"}}selected{{end}}>Most active</option>
        <option value="newest" {{if eq .Sort "newest"}}selected{{end}}>Newest bridged</option>
        <option value="deferred_desc" {{if eq .Sort "deferred_desc"}}selected{{end}}>Most deferred</option>
      </select>
      <button type="submit">Search</button>
    </form>
  </header>
  <div class="action-row-compact directory-actions">
    {{if .Mode.CanSelfServeInvite}}
    <a href="/create-invite" class="btn-primary">Create room invite</a>
    {{end}}
  </div>
  {{if .Bots}}
    <div class="bot-grid">
      {{range .Bots}}
        <a class="bot-card" href="{{.DetailURL}}">
          <strong>{{abbreviateDID .ATDID}}</strong>
          <p>{{abbreviateFeed .SSBFeedID}}</p>
          <div class="stats-bar">
            <span class="stat-pill stat-total">{{.TotalMessages}} msgs</span>
            <span class="stat-pill stat-published">{{.PublishedMessages}} published</span>
            {{if gt .FailedMessages 0}}<span class="stat-pill stat-failed">{{.FailedMessages}} failed</span>{{end}}
            {{if gt .DeferredMessages 0}}<span class="stat-pill stat-deferred">{{.DeferredMessages}} deferred</span>{{end}}
          </div>
          <button class="btn-small">View details</button>
        </a>
      {{end}}
    </div>
  {{else}}
    <p>No active bridged bots yet.</p>
  {{end}}
{{end}}
`

const botDetailContentTemplate = `
{{define "pageTitle"}}Bot · {{abbreviateDID .Bot.ATDID}}{{end}}
{{define "content"}}
  <header class="page-header-main">
    <a href="/bots">← Back to directory</a>
    <p class="eyebrow">Bot detail</p>
    <h1>{{.Bot.ATDID}}</h1>
  </header>
  <header class="page-header-actions">
    <button class="btn-copy" data-copy="{{.Bot.ATDID}}">Copy DID</button>
    <button class="btn-copy" data-copy="{{.Bot.SSBFeedID}}">Copy feed ID</button>
    <button class="btn-copy" data-copy="{{.Bot.FeedURI}}">Copy feed URI</button>
    <a href="{{.Bot.FeedHref}}" class="btn-secondary">Open feed URI</a>
  </header>
  <div class="page-title-wide">
    <h2>SSB Feed</h2>
    <code>{{.Bot.SSBFeedID}}</code>
  </div>
  <div class="panel">
    <h2>Published messages</h2>
    {{if .PublishedMessages}}
    {{range .PublishedMessages}}
    <div class="message-card">
      <p>{{.Type}}</p>
      {{if .SSBMsgRef}}<code>{{.SSBMsgRef}}</code>{{end}}
    </div>
    {{end}}
    {{else}}
    <p>No published messages yet.</p>
    {{end}}
  </div>
  <details class="panel">
    <summary>Show stored payloads</summary>
    {{range .PublishedMessages}}
    {{if .HasRawATProto}}
    <div class="raw-payload">
      <h4>ATProto (source)</h4>
      <pre>{{.RawATProtoJSON}}</pre>
    </div>
    {{end}}
    {{if .HasRawSSB}}
    <div class="raw-payload">
      <h4>SSB (bridged)</h4>
      <pre>{{.RawSSBJSON}}</pre>
    </div>
    {{end}}
    {{end}}
  </details>
{{end}}
`
