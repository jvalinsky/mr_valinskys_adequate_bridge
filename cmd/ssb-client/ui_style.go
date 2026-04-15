package main

import (
	"fmt"
	"html"
)

const cssStyle = `
:root {
    color-scheme: light;
    --proto-at: oklch(0.67 0.13 246);
    --proto-at-deep: oklch(0.55 0.12 248);
    --proto-ssb: oklch(0.74 0.13 70);
    --proto-ssb-deep: oklch(0.61 0.12 60);
    --proto-bridge: oklch(0.69 0.11 184);
    --proto-bridge-deep: oklch(0.56 0.10 186);
    --status-verified: oklch(0.71 0.13 152);
    --status-error: oklch(0.63 0.17 25);

    --bg-canvas: oklch(0.86 0.06 193);
    --bg-surface: oklch(0.97 0.01 220);
    --bg-surface-raised: oklch(0.93 0.02 220);
    --fg-primary: oklch(0.27 0.02 230);
    --fg-muted: oklch(0.50 0.03 220);
    --stroke: oklch(0.64 0.03 215);

    --status-ingress: var(--proto-at);
    --status-egress: var(--proto-ssb);
    --status-bridge: var(--proto-bridge);
    --status-warn: var(--proto-ssb-deep);
    --status-success: var(--status-verified);
    --status-failure: var(--status-error);

    --bg: var(--bg-canvas);
    --bg-subtle: color-mix(in oklch, var(--bg-surface) 74%, var(--bg-canvas));
    --panel: var(--bg-surface);
    --panel-solid: var(--bg-surface-raised);
    --ink: var(--fg-primary);
    --ink-strong: color-mix(in oklch, var(--fg-primary) 86%, black);
    --muted: var(--fg-muted);
    --line: var(--stroke);
    --line-strong: color-mix(in oklch, var(--stroke) 84%, var(--fg-primary));
    --brand: var(--status-bridge);
    --brand-strong: var(--proto-bridge-deep);
    --brand-soft: color-mix(in oklch, var(--status-bridge) 70%, var(--status-ingress));
    --warn: var(--status-warn);
    --warn-bg: color-mix(in oklch, var(--status-warn) 14%, var(--panel));
    --danger: var(--status-failure);
    --danger-bg: color-mix(in oklch, var(--status-failure) 12%, var(--panel));
    --ok: var(--status-success);
    --ok-bg: color-mix(in oklch, var(--status-success) 14%, var(--panel));
    --shadow: 0 10px 24px rgba(28, 41, 36, 0.08);
    --shadow-hover: 0 14px 32px rgba(28, 41, 36, 0.12);
    --radius: 14px;
    --radius-sm: 8px;
    --transition: 180ms ease;
}

@media (prefers-color-scheme: dark) {
    :root:not([data-theme="light"]) {
        color-scheme: dark;
        --bg-canvas: oklch(0.22 0.02 220);
        --bg-surface: oklch(0.28 0.02 220);
        --bg-surface-raised: oklch(0.33 0.03 220);
        --fg-primary: oklch(0.94 0.01 220);
        --fg-muted: oklch(0.74 0.02 220);
        --stroke: oklch(0.48 0.03 220);
        --shadow: 0 10px 24px rgba(0, 0, 0, 0.3);
        --shadow-hover: 0 14px 32px rgba(0, 0, 0, 0.4);
    }
}

[data-theme="dark"] {
    color-scheme: dark;
    --bg-canvas: oklch(0.22 0.02 220);
    --bg-surface: oklch(0.28 0.02 220);
    --bg-surface-raised: oklch(0.33 0.03 220);
    --fg-primary: oklch(0.94 0.01 220);
    --fg-muted: oklch(0.74 0.02 220);
    --stroke: oklch(0.48 0.03 220);
    --shadow: 0 10px 24px rgba(0, 0, 0, 0.3);
    --shadow-hover: 0 14px 32px rgba(0, 0, 0, 0.4);
}

* { box-sizing: border-box; }

body { font-family: "Avenir Next", "Segoe UI", sans-serif; max-width: 1300px; margin: 0 auto; padding: 20px; background: var(--bg); color: var(--ink); }

.app-header { background: linear-gradient(135deg, var(--brand), var(--brand-strong)); color: #f8fffd; box-shadow: var(--shadow); border-radius: var(--radius); margin-bottom: 20px; }
.header-content { display: flex; justify-content: space-between; align-items: flex-start; padding: 16px 20px; flex-wrap: wrap; gap: 12px; }
.theme-toggle { display: flex; align-items: center; justify-content: center; width: 36px; height: 36px; border: 1px solid rgba(255,255,255,0.3); border-radius: 50%; background: transparent; color: rgba(255,255,255,0.85); cursor: pointer; flex-shrink: 0; margin-left: 8px; transition: background 150ms ease; }
.theme-toggle:hover { background: rgba(255,255,255,0.2); }
.theme-toggle:focus-visible { outline: 2px solid rgba(255,255,255,0.5); outline-offset: 2px; }
.theme-toggle svg { display: none; }
[data-theme="dark"] .theme-toggle .icon-sun { display: block; }
[data-theme="dark"] .theme-toggle .icon-moon { display: none; }
:root:not([data-theme]) .theme-toggle .icon-sun { display: none; }
:root:not([data-theme]) .theme-toggle .icon-moon { display: block; }

@media (prefers-color-scheme: dark) {
    :root:not([data-theme]) .theme-toggle .icon-sun { display: block; }
    :root:not([data-theme]) .theme-toggle .icon-moon { display: none; }
}
.brand { font-size: 1.1rem; font-weight: 800; letter-spacing: 0.02em; }
.nav-row { display: flex; flex-wrap: wrap; gap: 12px; align-items: center; margin-top: 12px; }
.nav-group { display: inline-flex; gap: 2px; align-items: center; }
.nav-group-label { color: rgba(255,255,255,0.6); font-size: 0.65rem; text-transform: uppercase; letter-spacing: 0.06em; font-weight: 700; margin-right: 4px; padding-left: 6px; border-left: 1px solid rgba(255,255,255,0.2); }
.nav-group:first-child .nav-group-label { padding-left: 0; border-left: none; }
.nav-link { text-decoration: none; font-size: 0.85rem; font-weight: 700; padding: 5px 10px; border-radius: var(--radius-sm); border: 1px solid transparent; color: rgba(255,255,255,0.85); transition: background 150ms ease, color 150ms ease; }
.nav-link:hover { background: rgba(255,255,255,0.15); color: #fff; }
.nav-link:focus-visible { outline: 2px solid rgba(255,255,255,0.5); outline-offset: 2px; }
.nav-link.is-active { background: var(--panel-solid); color: var(--brand-strong); border-color: var(--brand-strong); box-shadow: 0 1px 3px rgba(0,0,0,0.15); }
[data-theme="dark"] .nav-link.is-active { background: var(--brand); color: var(--ink-strong); border-color: var(--brand-strong); }

h1 { color: var(--brand-strong); border-bottom: 2px solid var(--brand); padding-bottom: 10px; margin-top: 20px; }
h2 { color: var(--brand); margin-top: 24px; }
a { color: var(--brand); }

.panel { background: var(--panel); border-radius: var(--radius); border: 1px solid var(--line); padding: 20px; box-shadow: var(--shadow); margin: 15px 0; }
.post {
    --post-accent: var(--status-bridge);
    background: linear-gradient(180deg, color-mix(in oklch, var(--post-accent) 10%, var(--panel)) 0%, var(--panel) 62%);
    border-radius: var(--radius);
    border: 1px solid color-mix(in oklch, var(--post-accent) 34%, var(--line));
    padding: 15px;
    margin: 10px 0;
    box-shadow: var(--shadow);
}
.post-header { color: var(--muted); font-size: 0.85em; margin-bottom: 8px; }
.post-content { color: var(--ink); white-space: pre-wrap; line-height: 1.5; }
.post-content p { margin: 0.5em 0; }
.post-content a { color: var(--brand); text-decoration: underline; }
.post-content code { background: var(--bg-subtle); padding: 2px 6px; border-radius: 4px; font-size: 0.9em; }
.author { color: var(--brand); font-weight: bold; }
.contact-ref { color: var(--brand-strong); font-weight: 600; }
.post-type-post { --post-accent: var(--status-bridge); }
.post-type-follow, .post-type-contact, .post-type-unfollow { --post-accent: var(--status-success); }
.post-type-like, .post-type-vote { --post-accent: var(--status-ingress); }
.post-type-blocking { --post-accent: var(--status-failure); }
.post-type-about { --post-accent: var(--status-ingress); }
.post-type-pub { --post-accent: var(--status-warn); }
.post-type-gist { --post-accent: var(--status-egress); }
.post-type-raw, .post-type-unknown { --post-accent: var(--line-strong); }

.message-action { color: var(--muted); font-style: italic; }

.empty { background: var(--panel); border: 1px dashed var(--line); padding: 30px; text-align: center; border-radius: var(--radius); color: var(--muted); }
.empty-content { color: var(--muted); font-style: italic; }

.post-actions { margin-top: 10px; padding-top: 10px; border-top: 1px solid var(--line); }
.post-actions a { color: var(--muted); font-size: 0.85em; margin-right: 15px; }
.post-actions a:hover { color: var(--brand); }

details { margin-top: 10px; }
details summary { cursor: pointer; color: var(--muted); font-size: 0.8em; font-weight: 600; }
details pre { background: var(--bg); padding: 10px; border-radius: var(--radius-sm); overflow-x: auto; font-size: 0.8em; }

.stat-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(180px, 1fr)); gap: 15px; margin: 15px 0; }
.stat-card { background: var(--panel); border-radius: var(--radius); border: 1px solid var(--line); padding: 15px; text-align: center; box-shadow: var(--shadow); }
.stat-card .value { font-size: 2em; color: var(--brand-strong); font-weight: bold; }
.stat-card .label { font-size: 0.85em; color: var(--muted); margin-top: 5px; }

button { background: var(--brand); color: #fff; padding: 10px 20px; border: none; border-radius: var(--radius-sm); cursor: pointer; margin-top: 10px; font-weight: 700; font-size: 0.95em; }
button:hover { background: var(--brand-strong); }
.field { margin: 15px 0; }
.field label { display: block; font-weight: 700; margin-bottom: 5px; color: var(--ink); }
code { background: var(--bg); padding: 2px 6px; border-radius: 4px; font-size: 0.9em; font-family: monospace; }
pre { background: var(--bg); padding: 15px; border-radius: var(--radius); overflow-x: auto; font-size: 0.85em; border: 1px solid var(--line); font-family: monospace; }
table { width: 100%%; border-collapse: collapse; margin: 15px 0; }
th, td { padding: 12px 14px; text-align: left; border-bottom: 1px solid var(--line); }
th { color: var(--muted); font-size: 0.85em; text-transform: uppercase; font-weight: 700; }
tr:hover td { background: color-mix(in oklch, var(--status-bridge) 9%, var(--panel)); }
.badge { display: inline-block; padding: 3px 10px; border-radius: 999px; font-size: 0.8em; background: color-mix(in oklch, var(--status-bridge) 84%, var(--proto-bridge-deep)); color: oklch(0.98 0.004 220); font-weight: 600; }
.badge.warn { background: var(--warn); }
.badge.danger { background: var(--danger); }
.badge.ok { background: var(--ok); }
.status { border-radius: var(--radius); padding: 12px 14px; margin: 12px 0; border: 1px solid var(--line); background: var(--panel); }
.status.info { border-color: color-mix(in oklch, var(--status-bridge) 42%, var(--line)); background: color-mix(in oklch, var(--status-bridge) 12%, var(--panel)); }
.status.success { border-color: color-mix(in oklch, var(--status-success) 42%, var(--line)); background: color-mix(in oklch, var(--status-success) 12%, var(--panel)); }
.status.error { border-color: color-mix(in oklch, var(--status-failure) 42%, var(--line)); background: color-mix(in oklch, var(--status-failure) 12%, var(--panel)); }
details summary { cursor: pointer; color: var(--brand); font-size: 0.9em; margin-top: 12px; font-weight: 600; }
ul { list-style: none; padding: 0; }
li { background: var(--panel); border: 1px solid var(--line); padding: 12px 15px; margin: 8px 0; border-radius: var(--radius); }
.pagination { display: flex; gap: 8px; margin-top: 15px; }
.pagination a { background: var(--panel); padding: 8px 14px; border-radius: var(--radius-sm); margin: 0 3px; color: var(--brand); text-decoration: none; border: 1px solid var(--line); font-weight: 600; }
.pagination a:hover { background: var(--brand); color: #fff; }

@media (max-width: 840px) {
  body { padding: 10px; }
  .header-content { padding: 12px; }
  .nav-row { gap: 6px; }
  .nav-group { width: 100%; justify-content: flex-start; border-radius: 12px; }
  table { display: block; overflow-x: auto; white-space: nowrap; }
  .stat-grid { grid-template-columns: repeat(auto-fit, minmax(140px, 1fr)); }
}
`

func navHTML() string {
	return `<header class="app-header">
<div class="header-content">
  <span class="brand">SSB Client</span>
  <nav class="nav-row">
    <span class="nav-group"><span class="nav-group-label">Social</span><a href="/feed" class="nav-link">Feed</a><a href="/feeds" class="nav-link">Feeds</a><a href="/compose" class="nav-link">Compose</a><a href="/following" class="nav-link">Following</a><a href="/followers" class="nav-link">Followers</a><a href="/messages" class="nav-link">Messages</a></span>
    <span class="nav-group"><span class="nav-group-label">Network</span><a href="/peers" class="nav-link">Peers</a><a href="/room" class="nav-link">Room</a><a href="/replication" class="nav-link">Replication</a></span>
    <span class="nav-group"><span class="nav-group-label">Storage</span><a href="/blobs" class="nav-link">Blobs</a><a href="/profile" class="nav-link">Profile</a></span>
    <span class="nav-group"><span class="nav-group-label">Debug</span><a href="/" class="nav-link">Dashboard</a><a href="/settings" class="nav-link">Settings</a></span>
  </nav>
  <button id="theme-toggle" class="theme-toggle" aria-label="Toggle dark mode">
    <svg class="icon-sun" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="5"/><path d="M12 1v2M12 21v2M4.22 4.22l1.42 1.42M18.36 18.36l1.42 1.42M1 12h2M21 12h2M4.22 19.78l1.42-1.42M18.36 5.64l1.42-1.42"/></svg>
    <svg class="icon-moon" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z"/></svg>
  </button>
</div>
</header>`
}

func htmlPage(title, body string) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>%s - SSB Client</title>
    <style>%s</style>
</head>
<body>
%s
<main class="app-main">
%s
</main>
<script>
(function() {
  var saved = localStorage.getItem('theme');
  var prefers = window.matchMedia('(prefers-color-scheme: dark)').matches;
  if (saved === 'dark' || (!saved && prefers)) {
    document.documentElement.setAttribute('data-theme', 'dark');
  }
  var btn = document.getElementById('theme-toggle');
  if (btn) {
    btn.addEventListener('click', function() {
      var isDark = document.documentElement.getAttribute('data-theme') === 'dark';
      document.documentElement.setAttribute('data-theme', isDark ? 'light' : 'dark');
      localStorage.setItem('theme', isDark ? 'light' : 'dark');
    });
  }
})();
document.addEventListener('keydown', function (ev) {
  if (ev.defaultPrevented || ev.metaKey || ev.ctrlKey || ev.altKey) return;
  var tag = (ev.target && ev.target.tagName || '').toLowerCase();
  if (tag === 'input' || tag === 'textarea' || tag === 'select') return;
  if (ev.key === 'c') window.location.href = '/compose';
  if (ev.key === 'p') window.location.href = '/peers';
  if (ev.key === 'f') window.location.href = '/feed';
  if (ev.key === 'm') window.location.href = '/messages';
});
</script>
</body>
</html>`, html.EscapeString(title), cssStyle, navHTML(), body)
}
