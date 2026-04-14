package main

import (
	"fmt"
	"html"
)

const cssStyle = `
:root {
    color-scheme: light;
    --bg: #f4f1ea;
    --bg-subtle: #ebe6dd;
    --panel: #fffdf9;
    --panel-solid: #ffffff;
    --ink: #1a2622;
    --ink-strong: #0f1814;
    --muted: #4a5753;
    --line: #d7d2c6;
    --line-strong: #c4bdb0;
    --brand: #1a6b5a;
    --brand-strong: #124a3e;
    --brand-soft: #2a8f7a;
    --warn: #8a5d12;
    --warn-bg: #fff8e6;
    --danger: #b33030;
    --danger-bg: #fdeaea;
    --ok: #1a6b35;
    --ok-bg: #e8f6ec;
    --shadow: 0 10px 24px rgba(28, 41, 36, 0.08);
    --shadow-hover: 0 14px 32px rgba(28, 41, 36, 0.12);
    --radius: 14px;
    --radius-sm: 8px;
    --transition: 180ms ease;
}

@media (prefers-color-scheme: dark) {
    :root:not([data-theme="light"]) {
        color-scheme: dark;
        --bg: #121a17;
        --bg-subtle: #1a2521;
        --panel: #1e2925;
        --panel-solid: #232d28;
        --ink: #e4ebe8;
        --ink-strong: #f0f6f3;
        --muted: #8a9b94;
        --line: #2d3a34;
        --line-strong: #3d4f46;
        --brand: #3db892;
        --brand-strong: #5ccda8;
        --brand-soft: #2a9d7a;
        --warn: #e8a935;
        --warn-bg: #2a2215;
        --danger: #e85a5a;
        --danger-bg: #2a1a1a;
        --ok: #4cc76a;
        --ok-bg: #1a2a1e;
        --shadow: 0 10px 24px rgba(0, 0, 0, 0.3);
        --shadow-hover: 0 14px 32px rgba(0, 0, 0, 0.4);
    }
}

[data-theme="dark"] {
    color-scheme: dark;
    --bg: #121a17;
    --bg-subtle: #1a2521;
    --panel: #1e2925;
    --panel-solid: #232d28;
    --ink: #e4ebe8;
    --ink-strong: #f0f6f3;
    --muted: #8a9b94;
    --line: #2d3a34;
    --line-strong: #3d4f46;
    --brand: #3db892;
    --brand-strong: #5ccda8;
    --brand-soft: #2a9d7a;
    --warn: #e8a935;
    --warn-bg: #2a2215;
    --danger: #e85a5a;
    --danger-bg: #2a1a1a;
    --ok: #4cc76a;
    --ok-bg: #1a2a1e;
    --shadow: 0 10px 24px rgba(0, 0, 0, 0.3);
    --shadow-hover: 0 14px 32px rgba(0, 0, 0, 0.4);
}

* { box-sizing: border-box; }

body { font-family: "Avenir Next", "Segoe UI", sans-serif; max-width: 1300px; margin: 0 auto; padding: 20px; background: var(--bg); color: var(--ink); }

.app-header { background: linear-gradient(135deg, var(--brand), var(--brand-strong)); color: #f8fffd; box-shadow: var(--shadow); border-radius: var(--radius); margin-bottom: 20px; }
.header-content { display: flex; justify-content: space-between; align-items: flex-start; padding: 16px 20px; flex-wrap: wrap; gap: 12px; }
.theme-toggle { display: flex; align-items: center; justify-content: center; width: 36px; height: 36px; border: 1px solid rgba(240, 248, 245, 0.3); border-radius: 50%; background: transparent; color: #f0f8f5; cursor: pointer; flex-shrink: 0; margin-left: 8px; }
.theme-toggle:hover { background: rgba(255, 255, 255, 0.15); }
.theme-toggle svg { display: none; }
[data-theme="dark"] .theme-toggle .icon-sun,
:root:not([data-theme="dark"]) .theme-toggle .icon-moon { display: block; }
[data-theme="dark"] .theme-toggle .icon-moon,
:root:not([data-theme="dark"]) .theme-toggle .icon-sun { display: none; }
.brand { font-size: 1.1rem; font-weight: 800; letter-spacing: 0.02em; }
.nav-row { display: flex; flex-wrap: wrap; gap: 8px; align-items: center; }
.nav-group { display: inline-flex; gap: 6px; align-items: center; padding: 4px 6px; border: 1px solid rgba(240, 248, 245, 0.2); border-radius: 999px; }
.nav-group-label { color: rgba(240, 248, 245, 0.8); font-size: 0.72rem; text-transform: uppercase; letter-spacing: 0.04em; margin-right: 3px; }
.nav-link { text-decoration: none; font-size: 0.9rem; font-weight: 700; padding: 8px 12px; border-radius: 999px; border: 1px solid transparent; color: #f0f8f5; }
.nav-link:hover { border-color: rgba(240, 248, 245, 0.35); background: rgba(255, 255, 255, 0.10); }
.nav-link.is-active { background: var(--panel-solid); color: var(--brand-strong); }
[data-theme="dark"] .nav-link.is-active { background: var(--brand); color: var(--ink-strong); }

h1 { color: var(--brand-strong); border-bottom: 2px solid var(--brand); padding-bottom: 10px; margin-top: 20px; }
h2 { color: var(--brand); margin-top: 24px; }
a { color: var(--brand); }

.panel { background: var(--panel); border-radius: var(--radius); border: 1px solid var(--line); padding: 20px; box-shadow: var(--shadow); margin: 15px 0; }
.post { background: var(--panel); border-radius: var(--radius); border: 1px solid var(--line); padding: 15px; margin: 10px 0; border-left: 4px solid var(--brand); box-shadow: var(--shadow); }
.post-header { color: var(--muted); font-size: 0.85em; margin-bottom: 8px; }
.post-content { color: var(--ink); white-space: pre-wrap; line-height: 1.5; }
.post-content p { margin: 0.5em 0; }
.post-content a { color: var(--brand); text-decoration: underline; }
.post-content code { background: var(--bg-subtle); padding: 2px 6px; border-radius: 4px; font-size: 0.9em; }
.author { color: var(--brand); font-weight: bold; }
.contact-ref { color: var(--brand-strong); font-weight: 600; }
.post-type-post { border-left-color: var(--brand); }
.post-type-follow, .post-type-contact, .post-type-unfollow { border-left-color: var(--ok); }
.post-type-like, .post-type-vote { border-left-color: #9b59b6; }
.post-type-blocking { border-left-color: var(--danger); }
.post-type-about { border-left-color: #3498db; }
.post-type-pub { border-left-color: var(--warn); }
.post-type-gist { border-left-color: #e67e22; }
.post-type-raw, .post-type-unknown { border-left-color: var(--line-strong); }

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

button { background: var(--brand); color: #fff; padding: 10px 20px; border: none; border-radius: 8px; cursor: pointer; margin-top: 10px; font-weight: 700; font-size: 0.95em; }
button:hover { background: var(--brand-strong); }
.field { margin: 15px 0; }
.field label { display: block; font-weight: 700; margin-bottom: 5px; color: var(--ink); }
code { background: var(--bg); padding: 2px 6px; border-radius: 4px; font-size: 0.9em; font-family: monospace; }
pre { background: var(--bg); padding: 15px; border-radius: var(--radius); overflow-x: auto; font-size: 0.85em; border: 1px solid var(--line); font-family: monospace; }
table { width: 100%%; border-collapse: collapse; margin: 15px 0; }
th, td { padding: 12px 14px; text-align: left; border-bottom: 1px solid var(--line); }
th { color: var(--muted); font-size: 0.85em; text-transform: uppercase; font-weight: 700; }
tr:hover td { background: var(--bg); }
.badge { display: inline-block; padding: 3px 10px; border-radius: 999px; font-size: 0.8em; background: var(--brand); color: #fff; font-weight: 600; }
.badge.warn { background: var(--warn); }
.badge.danger { background: var(--danger); }
.badge.ok { background: var(--ok); }
.status { border-radius: var(--radius); padding: 12px 14px; margin: 12px 0; border: 1px solid var(--line); background: var(--panel); }
.status.info { border-left: 4px solid var(--brand); }
.status.success { border-left: 4px solid var(--ok); }
.status.error { border-left: 4px solid var(--danger); }
details summary { cursor: pointer; color: var(--brand); font-size: 0.9em; margin-top: 12px; font-weight: 600; }
ul { list-style: none; padding: 0; }
li { background: var(--panel); border: 1px solid var(--line); padding: 12px 15px; margin: 8px 0; border-radius: var(--radius); }
.pagination { display: flex; gap: 8px; margin-top: 15px; }
.pagination a { background: var(--panel); padding: 8px 14px; border-radius: 8px; margin: 0 3px; color: var(--brand); text-decoration: none; border: 1px solid var(--line); font-weight: 600; }
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
