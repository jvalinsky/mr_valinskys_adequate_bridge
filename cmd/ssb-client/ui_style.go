package main

import (
	"fmt"
	"html"
)

const cssStyle = `
:root {
    color-scheme: light;
    --bg: #f4f1ea;
    --panel: #fffdf9;
    --ink: #1a2622;
    --muted: #4a5753;
    --line: #d7d2c6;
    --brand: #1a6b5a;
    --brand-strong: #124a3e;
    --warn: #8a5d12;
    --danger: #b33030;
    --ok: #1a6b35;
    --shadow: 0 10px 24px rgba(28, 41, 36, 0.08);
    --radius: 14px;
}

* { box-sizing: border-box; }

body { font-family: "Avenir Next", "Segoe UI", sans-serif; max-width: 1300px; margin: 0 auto; padding: 20px; background: var(--bg); color: var(--ink); }

.app-header { background: linear-gradient(135deg, var(--brand), var(--brand-strong)); color: #f8fffd; box-shadow: var(--shadow); border-radius: var(--radius); margin-bottom: 20px; }
.header-content { display: flex; justify-content: space-between; align-items: center; padding: 16px 20px; flex-wrap: wrap; gap: 12px; }
.brand { font-size: 1.1rem; font-weight: 800; letter-spacing: 0.02em; }
.nav-row { display: flex; flex-wrap: wrap; gap: 8px; }
.nav-link { text-decoration: none; font-size: 0.9rem; font-weight: 700; padding: 8px 12px; border-radius: 999px; border: 1px solid transparent; color: #f0f8f5; }
.nav-link:hover { border-color: rgba(240, 248, 245, 0.35); background: rgba(255, 255, 255, 0.10); }
.nav-link.is-active { background: #f8fffd; color: var(--brand-strong); }

h1 { color: var(--brand-strong); border-bottom: 2px solid var(--brand); padding-bottom: 10px; margin-top: 20px; }
h2 { color: var(--brand); margin-top: 24px; }
a { color: var(--brand); }

.panel { background: var(--panel); border-radius: var(--radius); border: 1px solid var(--line); padding: 20px; box-shadow: var(--shadow); margin: 15px 0; }
.post { background: var(--panel); border-radius: var(--radius); border: 1px solid var(--line); padding: 15px; margin: 10px 0; border-left: 4px solid var(--brand); box-shadow: var(--shadow); }
.post-header { color: var(--muted); font-size: 0.85em; margin-bottom: 8px; }
.post-content { color: var(--ink); white-space: pre-wrap; }
.author { color: var(--brand); font-weight: bold; }
.empty { background: var(--panel); border: 1px dashed var(--line); padding: 30px; text-align: center; border-radius: var(--radius); color: var(--muted); }

.stat-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(180px, 1fr)); gap: 15px; margin: 15px 0; }
.stat-card { background: var(--panel); border-radius: var(--radius); border: 1px solid var(--line); padding: 15px; text-align: center; box-shadow: var(--shadow); }
.stat-card .value { font-size: 2em; color: var(--brand-strong); font-weight: bold; }
.stat-card .label { font-size: 0.85em; color: var(--muted); margin-top: 5px; }

input, textarea { width: 100%%; padding: 10px 12px; margin: 8px 0; border: 1px solid var(--line); border-radius: 8px; box-sizing: border-box; background: var(--panel); color: var(--ink); font-size: 0.95em; }
input:focus, textarea:focus { outline: none; border-color: var(--brand); box-shadow: 0 0 0 3px rgba(26, 107, 90, 0.15); }
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
details summary { cursor: pointer; color: var(--brand); font-size: 0.9em; margin-top: 12px; font-weight: 600; }
ul { list-style: none; padding: 0; }
li { background: var(--panel); border: 1px solid var(--line); padding: 12px 15px; margin: 8px 0; border-radius: var(--radius); }
.pagination { display: flex; gap: 8px; margin-top: 15px; }
.pagination a { background: var(--panel); padding: 8px 14px; border-radius: 8px; margin: 0 3px; color: var(--brand); text-decoration: none; border: 1px solid var(--line); font-weight: 600; }
.pagination a:hover { background: var(--brand); color: #fff; }
`

func navHTML() string {
	return `<header class="app-header">
<div class="header-content">
  <span class="brand">SSB Client</span>
  <nav class="nav-row">
    <a href="/" class="nav-link">Dashboard</a>
    <a href="/feed" class="nav-link">Feed</a>
    <a href="/feeds" class="nav-link">Feeds</a>
    <a href="/compose" class="nav-link">Compose</a>
    <a href="/profile" class="nav-link">Profile</a>
    <a href="/following" class="nav-link">Following</a>
    <a href="/blobs" class="nav-link">Blobs</a>
    <a href="/peers" class="nav-link">Peers</a>
    <a href="/replication" class="nav-link">Replication</a>
    <a href="/room" class="nav-link">Room</a>
    <a href="/messages" class="nav-link">Messages</a>
    <a href="/settings" class="nav-link">Settings</a>
  </nav>
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
</body>
</html>`, html.EscapeString(title), cssStyle, navHTML(), body)
}
