// Package templates renders HTML views for the bridge admin UI.
package templates

import (
	"fmt"
	"html/template"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/presentation"
)

const pageLayout = `
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>ATProto ↔ SSB Bridge Admin</title>
    <link rel="preconnect" href="https://fonts.googleapis.com">
    <link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
    <link href="https://fonts.googleapis.com/css2?family=Sora:wght@400;600;700;800&family=JetBrains+Mono:wght@400;500;600&display=swap" rel="stylesheet">
    <style>
        :root {
            color-scheme: light;
            --bg: #f4f1ea;
            --bg-subtle: #ebe6dd;
            --panel: #fffdf9;
            --panel-solid: #ffffff;
            --ink: #1a2622;
            --ink-strong: #0f1814;
            --muted: #5c6b65;
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
            --shadow: 0 4px 12px rgba(28, 41, 36, 0.06), 0 1px 3px rgba(28, 41, 36, 0.04);
            --shadow-hover: 0 8px 24px rgba(28, 41, 36, 0.1), 0 2px 8px rgba(28, 41, 36, 0.06);
            --radius: 12px;
            --radius-sm: 8px;
            --transition: 180ms ease;
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
            --shadow: 0 4px 12px rgba(0, 0, 0, 0.3), 0 1px 3px rgba(0, 0, 0, 0.2);
            --shadow-hover: 0 8px 24px rgba(0, 0, 0, 0.4), 0 2px 8px rgba(0, 0, 0, 0.25);
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
                --shadow: 0 4px 12px rgba(0, 0, 0, 0.3), 0 1px 3px rgba(0, 0, 0, 0.2);
                --shadow-hover: 0 8px 24px rgba(0, 0, 0, 0.4), 0 2px 8px rgba(0, 0, 0, 0.25);
            }
        }

        * {
            box-sizing: border-box;
        }

        body {
            margin: 0;
            min-height: 100vh;
            font-family: "Sora", system-ui, sans-serif;
            background: var(--bg);
            color: var(--ink);
            transition: background var(--transition), color var(--transition);
        }

        code, .mono, pre {
            font-family: "JetBrains Mono", ui-monospace, SFMono-Regular, Menlo, monospace;
        }

        a {
            color: inherit;
        }

        .skip-link {
            position: absolute;
            left: 10px;
            top: -100px;
            background: var(--ink);
            color: #fff;
            padding: 10px 12px;
            border-radius: 10px;
            z-index: 1000;
            text-decoration: none;
        }

        .skip-link:focus {
            top: 10px;
        }

        .app-header {
            background: linear-gradient(135deg, var(--brand), var(--brand-strong));
            color: #f8fffd;
            box-shadow: var(--shadow);
        }

        .app-shell {
            width: min(1300px, calc(100vw - 24px));
            margin: 0 auto;
        }

        .header-row {
            display: flex;
            gap: 16px;
            align-items: center;
            justify-content: space-between;
            padding: 16px 0;
            flex-wrap: wrap;
        }

        .brand {
            font-size: 1.1rem;
            font-weight: 800;
            letter-spacing: 0.02em;
        }

        .nav-row {
            display: flex;
            flex-wrap: wrap;
            gap: 8px;
        }

        .nav-link {
            text-decoration: none;
            font-size: 0.9rem;
            font-weight: 700;
            padding: 8px 12px;
            border-radius: 999px;
            border: 1px solid transparent;
            color: #f0f8f5;
        }

        .nav-link:hover,
        .nav-link:focus-visible {
            border-color: rgba(240, 248, 245, 0.35);
            outline: none;
            background: rgba(255, 255, 255, 0.10);
        }

        .nav-link.is-active {
            background: var(--panel-solid);
            color: var(--brand-strong);
        }

        [data-theme="dark"] .nav-link.is-active {
            background: var(--brand);
            color: var(--ink-strong);
        }

        .theme-toggle {
            display: flex;
            align-items: center;
            justify-content: center;
            width: 36px;
            height: 36px;
            border: 1px solid rgba(240, 248, 245, 0.3);
            border-radius: 50%;
            background: transparent;
            color: #f0f8f5;
            cursor: pointer;
            transition: transform var(--transition), background var(--transition);
            flex-shrink: 0;
            margin-left: 8px;
        }

        .theme-toggle:hover {
            background: rgba(255, 255, 255, 0.15);
            transform: scale(1.05);
        }

        [data-theme="dark"] .icon-sun { display: block; }
        [data-theme="dark"] .icon-moon { display: none; }
        :root:not([data-theme]) .icon-sun { display: none; }
        :root:not([data-theme]) .icon-moon { display: block; }

        @media (prefers-color-scheme: dark) {
            :root:not([data-theme]) .icon-sun { display: block; }
            :root:not([data-theme]) .icon-moon { display: none; }
        }

        .app-main {
            width: min(1300px, calc(100vw - 24px));
            margin: 20px auto 32px;
            display: grid;
            gap: 18px;
        }

        .status-strip {
            border-radius: var(--radius);
            border: 1px solid var(--line);
            background: var(--panel);
            padding: 14px 16px;
            box-shadow: var(--shadow);
        }

        .status-strip h2 {
            margin: 0;
            font-size: 1rem;
        }

        .status-strip p {
            margin: 6px 0 0;
            font-size: 0.92rem;
            color: var(--muted);
        }

        .status-strip.tone-success { 
            border-left: 1px solid var(--ok);
            background: var(--ok-bg);
        }
        .status-strip.tone-warning { 
            border-left: 1px solid var(--warn);
            background: var(--warn-bg);
        }
        .status-strip.tone-danger { 
            border-left: 1px solid var(--danger);
            background: var(--danger-bg);
        }
        .status-strip.tone-neutral { 
            border-left: 1px solid var(--brand);
            background: var(--panel);
        }

        .section {
            background: var(--panel);
            border: 1px solid var(--line);
            border-radius: var(--radius);
            box-shadow: var(--shadow);
            overflow: hidden;
        }

        .section-pad {
            padding: 16px;
        }

        .page-title {
            margin: 0;
            font-size: clamp(1.5rem, 3vw, 2rem);
            letter-spacing: -0.02em;
        }

        .subtitle {
            margin: 8px 0 0;
            color: var(--muted);
            font-size: 0.95rem;
            line-height: 1.5;
        }

        .metric-grid {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
            gap: 12px;
        }

        .metric-card {
            text-decoration: none;
            border: 1px solid var(--line);
            border-radius: 12px;
            background: var(--panel-solid);
            padding: 14px;
            display: grid;
            gap: 5px;
            transition: transform var(--transition), box-shadow var(--transition), background var(--transition);
        }

        .metric-card:hover,
        .metric-card:focus-visible {
            transform: translateY(-2px);
            box-shadow: var(--shadow-hover);
            outline: none;
            background: var(--panel-solid);
        }

        .metric-label {
            color: var(--muted);
            font-size: 0.78rem;
            text-transform: uppercase;
            letter-spacing: 0.08em;
            font-weight: 700;
        }

        .metric-value {
            font-size: 1.65rem;
            font-weight: 800;
            line-height: 1.1;
        }

        .metric-note {
            color: var(--muted);
            font-size: 0.84rem;
        }

        .tone-warning .metric-value { color: var(--warn); }
        .tone-danger .metric-value { color: var(--danger); }
        .tone-success .metric-value { color: var(--ok); }

        .grid-two {
            display: grid;
            gap: 12px;
            grid-template-columns: repeat(auto-fit, minmax(400px, 1fr));
        }

        .breadcrumbs {
            display: flex;
            align-items: center;
            gap: 8px;
            font-size: 0.85rem;
            color: var(--muted);
            margin-bottom: 12px;
        }

        .breadcrumbs a {
            text-decoration: none;
            color: var(--brand);
            font-weight: 600;
        }

        .breadcrumbs a:hover {
            text-decoration: underline;
        }

        .breadcrumb-sep {
            color: var(--line);
        }

        .mini-list {
            margin: 0;
            padding: 0;
            list-style: none;
            display: grid;
            gap: 8px;
        }

        .mini-list li {
            border: 1px solid var(--line);
            border-radius: 10px;
            padding: 10px 12px;
            background: var(--panel-solid);
            display: flex;
            justify-content: space-between;
            gap: 10px;
            align-items: center;
        }

        .pill {
            border-radius: 999px;
            padding: 2px 8px;
            font-size: 0.75rem;
            font-weight: 700;
            border: 1px solid transparent;
            white-space: nowrap;
        }

        .pill.state-published { background: var(--ok-bg); color: var(--ok); }
        .pill.state-failed { background: var(--danger-bg); color: var(--danger); }
        .pill.state-deferred { background: var(--warn-bg); color: var(--warn); }
        .pill.state-deleted { background: var(--bg-subtle); color: var(--muted); }
        .pill.state-pending { background: var(--bg-subtle); color: var(--muted); }

        .table-wrap {
            overflow-x: auto;
        }

        table {
            width: 100%;
            border-collapse: collapse;
            min-width: 780px;
        }

        th,
        td {
            padding: 10px 12px;
            vertical-align: top;
            border-top: 1px solid var(--line);
            text-align: left;
            font-size: 0.9rem;
        }

        th {
            border-top: none;
            font-size: 0.74rem;
            text-transform: uppercase;
            letter-spacing: 0.08em;
            color: var(--muted);
            font-weight: 700;
            background: var(--bg-subtle);
        }

        tbody tr:nth-child(even) {
            background: var(--bg-subtle);
        }

        tbody tr:hover {
            background: rgba(26, 107, 90, 0.08);
        }

        .mono {
            font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", "Courier New", monospace;
            font-size: 0.82rem;
        }

        .truncate {
            display: inline-block;
            max-width: 100%;
            overflow: hidden;
            text-overflow: ellipsis;
            white-space: nowrap;
            vertical-align: bottom;
        }

        .copy-btn,
        .button,
        .button-link {
            border: 1px solid var(--line);
            border-radius: 9px;
            background: var(--panel-solid);
            color: var(--ink);
            font-size: 0.82rem;
            font-weight: 700;
            padding: 5px 9px;
            text-decoration: none;
            cursor: pointer;
            white-space: nowrap;
        }

        .copy-btn:hover,
        .button:hover,
        .button-link:hover,
        .button-danger:hover,
        .copy-btn:focus-visible,
        .button:focus-visible,
        .button-link:focus-visible,
        .button-danger:focus-visible {
            outline: none;
            border-color: var(--brand);
            color: var(--brand-strong);
        }

        .button-danger {
            border: 1px solid var(--line);
            border-radius: 9px;
            background: var(--panel-solid);
            color: var(--ink);
            font-size: 0.82rem;
            font-weight: 700;
            padding: 5px 9px;
            text-decoration: none;
            cursor: pointer;
            white-space: nowrap;
        }

        .button-danger:hover,
        .button-danger:focus-visible {
            outline: none;
            border-color: var(--brand);
            color: var(--brand-strong);
            background: var(--danger-bg);
        }

        .toolbar {
            display: flex;
            gap: 10px;
            flex-wrap: wrap;
            align-items: center;
            justify-content: space-between;
            margin-top: 10px;
        }

        .room-subnav {
            display: flex;
            gap: 8px;
            flex-wrap: wrap;
            margin-top: 10px;
        }

        .room-subnav a {
            border: 1px solid var(--line);
            border-radius: 999px;
            background: var(--panel-solid);
            padding: 7px 12px;
            text-decoration: none;
            font-size: 0.84rem;
            font-weight: 700;
        }

        .room-subnav a:hover,
        .room-subnav a:focus-visible {
            border-color: var(--brand);
            outline: none;
        }

        .room-subnav a.is-active {
            background: var(--brand);
            color: var(--ink-strong);
            border-color: var(--brand-strong);
        }

        .filter-panel {
            position: sticky;
            top: 8px;
            z-index: 20;
            background: var(--panel);
            border: 1px solid var(--line);
            border-radius: var(--radius);
            box-shadow: var(--shadow);
        }

        .filter-grid {
            display: grid;
            gap: 10px;
            grid-template-columns: repeat(auto-fit, minmax(170px, 1fr));
            padding: 14px;
        }

        .field {
            display: grid;
            gap: 4px;
        }

        .field label {
            font-size: 0.74rem;
            text-transform: uppercase;
            letter-spacing: 0.08em;
            color: var(--muted);
            font-weight: 700;
        }

        .field input,
        .field select {
            width: 100%;
            border: 1px solid var(--line);
            border-radius: 9px;
            padding: 8px;
            background: var(--panel-solid);
            color: var(--ink);
        }

        .field input:focus-visible,
        .field select:focus-visible {
            outline: 2px solid rgba(26, 107, 90, 0.22);
            border-color: var(--brand);
        }

        .inline-toggle {
            display: inline-flex;
            gap: 6px;
            align-items: center;
            font-size: 0.86rem;
            color: var(--ink);
            font-weight: 600;
        }

        .active-filters {
            display: flex;
            flex-wrap: wrap;
            gap: 6px;
            align-items: center;
        }

        .active-filters .chip {
            display: inline-flex;
            gap: 6px;
            align-items: center;
            border: 1px solid var(--line);
            border-radius: 999px;
            padding: 3px 10px;
            background: var(--panel-solid);
            font-size: 0.8rem;
        }

        .issue-text {
            color: var(--danger);
        }

        .issue-text.warning {
            color: var(--warn);
        }

        .issue-text.muted {
            color: var(--muted);
        }

        .details-grid {
            display: grid;
            gap: 12px;
            grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
        }

        .detail-card {
            border: 1px solid var(--line);
            border-radius: 12px;
            background: var(--panel-solid);
            padding: 12px;
            display: grid;
            gap: 6px;
        }

        .detail-card dt {
            color: var(--muted);
            font-size: 0.74rem;
            text-transform: uppercase;
            letter-spacing: 0.08em;
            font-weight: 700;
        }

        .detail-card dd {
            margin: 0;
            font-size: 0.92rem;
            line-height: 1.45;
            word-break: break-word;
        }

        .detail-card dd.empty,
        .detail-card .empty {
            color: var(--muted);
            font-style: italic;
            opacity: 0.7;
        }

        pre {
            margin: 0;
            white-space: pre-wrap;
            word-break: break-word;
            background: #f7f5ef;
            border: 1px solid var(--line);
            border-radius: 10px;
            padding: 12px;
            max-height: 380px;
            overflow: auto;
            font-size: 0.78rem;
            line-height: 1.5;
        }

        .pagination {
            display: flex;
            align-items: center;
            justify-content: space-between;
            gap: 10px;
            padding: 12px 14px;
            border-top: 1px solid var(--line);
            background: #fbfaf6;
        }

        .empty {
            text-align: center;
            color: var(--muted);
            padding: 20px;
            font-style: italic;
        }

        @keyframes fade-in {
            from { opacity: 0; transform: translateY(8px); }
            to { opacity: 1; transform: translateY(0); }
        }

        .section-pad {
            animation: fade-in 300ms ease-out;
        }

        @media (max-width: 900px) {
            .grid-two {
                grid-template-columns: 1fr;
            }

            .header-row {
                align-items: flex-start;
            }

            .toolbar {
                align-items: stretch;
            }

            .app-shell,
            .app-main {
                width: min(1300px, calc(100vw - 16px));
            }
        }
    </style>
</head>
<body>
    <a class="skip-link" href="#main-content">Skip to main content</a>
    <header class="app-header">
        <div class="app-shell header-row">
            <div class="brand">Bridge Admin</div>
            <nav class="nav-row" aria-label="Primary">
                <a href="/" class="{{navClass .Chrome.ActiveNav "dashboard"}}" {{navCurrent .Chrome.ActiveNav "dashboard"}}>Dashboard</a>
                <a href="/accounts" class="{{navClass .Chrome.ActiveNav "accounts"}}" {{navCurrent .Chrome.ActiveNav "accounts"}}>Accounts</a>
                <a href="/messages" class="{{navClass .Chrome.ActiveNav "messages"}}" {{navCurrent .Chrome.ActiveNav "messages"}}>Messages</a>
                <a href="/feed" class="{{navClass .Chrome.ActiveNav "feed"}}" {{navCurrent .Chrome.ActiveNav "feed"}}>Global Feed</a>
                <a href="/post" class="{{navClass .Chrome.ActiveNav "post"}}" {{navCurrent .Chrome.ActiveNav "post"}}>Compose Post</a>
                <a href="/failures" class="{{navClass .Chrome.ActiveNav "failures"}}" {{navCurrent .Chrome.ActiveNav "failures"}}>Failures</a>
                <a href="/blobs" class="{{navClass .Chrome.ActiveNav "blobs"}}" {{navCurrent .Chrome.ActiveNav "blobs"}}>Blobs</a>
                <a href="/room" class="{{navClass .Chrome.ActiveNav "room"}}" {{navCurrent .Chrome.ActiveNav "room"}}>Room</a>
                <a href="/connections" class="{{navClass .Chrome.ActiveNav "connections"}}" {{navCurrent .Chrome.ActiveNav "connections"}}>Connections</a>
                <a href="/reverse" class="{{navClass .Chrome.ActiveNav "reverse"}}" {{navCurrent .Chrome.ActiveNav "reverse"}}>Reverse Sync</a>
                <a href="/state" class="{{navClass .Chrome.ActiveNav "state"}}" {{navCurrent .Chrome.ActiveNav "state"}}>State</a>
                <button class="theme-toggle" aria-label="Toggle dark mode" title="Toggle theme">
                    <svg class="icon-sun" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="5"></circle><line x1="12" y1="1" x2="12" y2="3"></line><line x1="12" y1="21" x2="12" y2="23"></line><line x1="4.22" y1="4.22" x2="5.64" y2="5.64"></line><line x1="18.36" y1="18.36" x2="19.78" y2="19.78"></line><line x1="1" y1="12" x2="3" y2="12"></line><line x1="21" y1="12" x2="23" y2="12"></line><line x1="4.22" y1="19.78" x2="5.64" y2="18.36"></line><line x1="18.36" y1="5.64" x2="19.78" y2="4.22"></line></svg>
                    <svg class="icon-moon" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z"></path></svg>
                </button>
            </nav>

        </div>
    </header>

    <main id="main-content" class="app-main">
            {{if .Chrome.Breadcrumbs}}
            <nav class="breadcrumbs" aria-label="Breadcrumb">
                {{range $i, $bc := .Chrome.Breadcrumbs}}
                    {{if $i}}<span class="breadcrumb-sep">/</span>{{end}}
                    {{if $bc.Href}}
                        <a href="{{$bc.Href}}">{{$bc.Label}}</a>
                    {{else}}
                        <span>{{$bc.Label}}</span>
                    {{end}}
                {{end}}
            </nav>
            {{end}}

            {{if .Chrome.Status.Visible}}
        <section class="status-strip {{statusToneClass .Chrome.Status.Tone}}" role="status" aria-live="polite">
            <h2>{{.Chrome.Status.Title}}</h2>
            <p>{{.Chrome.Status.Body}}</p>
        </section>
        {{end}}
        {{template "content" .}}
    </main>

    <script>
      (function() {
        var saved = localStorage.getItem("theme");
        var prefers = window.matchMedia("(prefers-color-scheme: dark)").matches;
        if (saved === "dark" || (!saved && prefers)) {
          document.documentElement.setAttribute("data-theme", "dark");
        }
        document.querySelector(".theme-toggle").addEventListener("click", function() {
          var isDark = document.documentElement.getAttribute("data-theme") === "dark";
          if (isDark) {
            document.documentElement.setAttribute("data-theme", "light");
            localStorage.setItem("theme", "light");
          } else {
            document.documentElement.setAttribute("data-theme", "dark");
            localStorage.setItem("theme", "dark");
          }
        });
      })();
      document.querySelectorAll(".filter-panel select").forEach(function (sel) {
        sel.addEventListener("change", function () { sel.closest("form").submit(); });
      });
      document.addEventListener("click", function (event) {
        var button = event.target.closest("[data-copy]");
        if (!button) return;
        event.preventDefault();
        var value = button.getAttribute("data-copy");
        var original = button.textContent;
        function done() {
          button.textContent = "Copied";
          setTimeout(function () { button.textContent = original; }, 1000);
        }
        if (navigator.clipboard && window.isSecureContext) {
          navigator.clipboard.writeText(value).then(done).catch(function () { window.prompt("Copy", value); });
          return;
        }
        window.prompt("Copy", value);
      });
    </script>
</body>
</html>
`

const connectionsContent = `
{{define "content"}}
<section class="section section-pad">
    <h1 class="page-title">SSB Connections & Replication</h1>
    <p class="subtitle">Live MUXRPC peers and EBT frontier synchronization state.</p>
</section>

<section class="section section-pad">
    <h2 class="page-title" style="font-size:1.2rem">Active Peers</h2>
    {{if .Peers}}
    <div class="table-wrap" style="margin-top:10px">
        <table>
            <thead>
                <tr>
                    <th>Remote Address</th>
                    <th>SSB Feed ID</th>
                    <th>Received</th>
                    <th>Sent</th>
                    <th>Latency</th>
                </tr>
            </thead>
            <tbody>
                {{range .Peers}}
                <tr>
                    <td class="mono">{{.Addr}}</td>
                    <td class="mono">{{.Feed}}</td>
                    <td>{{humanizeBytes .ReadBytes}}</td>
                    <td>{{humanizeBytes .WriteBytes}}</td>
                    <td>{{if .Latency}}{{.Latency}}{{else}}-{{end}}</td>
                </tr>
                {{end}}
            </tbody>
        </table>
    </div>
    {{else}}
    <div class="empty">No active SSB peers connected.</div>
    {{end}}
</section>

<section class="section section-pad">
    <h2 class="page-title" style="font-size:1.2rem">Known Peers</h2>
    <p class="subtitle">Persisted peers that the bridge will attempt to maintain connections with.</p>
    {{if .KnownPeers}}
    <div class="table-wrap" style="margin-top:10px">
        <table>
            <thead>
                <tr>
                    <th>Address</th>
                    <th>Public Key (B64)</th>
                    <th>Created</th>
                    <th>Actions</th>
                </tr>
            </thead>
            <tbody>
                {{range .KnownPeers}}
                <tr>
                    <td class="mono">{{.Addr}}</td>
                    <td class="mono truncate" title="{{.PubKey}}">{{.PubKey}}</td>
                    <td>{{fmtTime .CreatedAt}}</td>
	                    <td>
	                        <form action="/connections/connect" method="POST" style="margin:0">
	                            {{csrfField $.Chrome.CSRFToken}}
	                            <input type="hidden" name="addr" value="{{.Addr}}">
	                            <input type="hidden" name="pubkey" value="{{.PubKey}}">
	                            <button type="submit" class="button button-small">Connect</button>
	                        </form>
                    </td>
                </tr>
                {{end}}
            </tbody>
        </table>
    </div>
    {{else}}
    <div class="empty">No known peers saved.</div>
    {{end}}

	    <div style="margin-top:24px; border-top:1px solid var(--line); padding-top:20px">
	        <h3 class="metric-label">Add Known Peer</h3>
	        <form action="/connections/add" method="POST" style="display:grid; gap:12px; grid-template-columns: 1fr 1fr auto; align-items: end; margin-top:8px">
	            {{csrfField .Chrome.CSRFToken}}
	            <div class="field">
	                <label>Address (host:port)</label>
                <input type="text" name="addr" placeholder="1.2.3.4:8008" required>
            </div>
            <div class="field">
                <label>Public Key (Base64)</label>
                <input type="text" name="pubkey" placeholder="...ed25519" required>
            </div>
            <button type="submit" class="button">Add Peer</button>
        </form>
    </div>
</section>

<section class="section section-pad">
    <h2 class="page-title" style="font-size:1.2rem">EBT Frontiers</h2>
    <p class="subtitle">Latest sequence numbers tracked for each feed, grouped by tracking context (local or remote peer).</p>
    {{if .EBTState}}
    {{range $context, $frontier := .EBTState}}
    <div style="margin-top:20px">
        <h3 class="metric-label">{{$context}}</h3>
        <div class="table-wrap">
            <table>
                <thead>
                    <tr>
                        <th>Feed</th>
                        <th>Sequence</th>
                    </tr>
                </thead>
                <tbody>
                    {{range $feed, $seq := $frontier}}
                    <tr>
                        <td class="mono">{{$feed}}</td>
                        <td>{{$seq}}</td>
                    </tr>
                    {{end}}
                </tbody>
            </table>
        </div>
    </div>
    {{end}}
    {{else}}
    <div class="empty">No EBT state available.</div>
    {{end}}
</section>
{{end}}
`

const dashboardContent = `
{{define "content"}}
<section class="section section-pad">
    <h1 class="page-title">Dashboard</h1>
    <p class="subtitle">Triage-first runtime view with direct pivots into issue-heavy streams.</p>
</section>

<section class="section section-pad">
    <div class="metric-grid">
        {{range .Metrics}}
        <a class="metric-card {{statusToneClass .Tone}}" href="{{.Href}}">
            <span class="metric-label">{{.Label}}</span>
            <span class="metric-value" id="metric-{{.ID}}">{{.Value}}</span>
            {{if .Note}}<span class="metric-note">{{.Note}}</span>{{end}}
        </a>
        {{end}}
    </div>
</section>

<section class="grid-two">
    <article class="section section-pad">
        <h2 class="page-title" style="font-size:1.2rem">Runtime Health</h2>
        <p class="subtitle" id="health-subtitle"><strong>{{.RuntimeHealth}}</strong> &middot; {{.RuntimeHealthDescription}}</p>
        <dl class="details-grid" style="margin-top:10px">
            <div class="detail-card"><dt>Bridge Status</dt><dd id="health-bridge-status">{{.BridgeStatus}}</dd></div>
            <div class="detail-card"><dt>Last Heartbeat</dt><dd id="health-last-heartbeat">{{if .LastHeartbeat}}{{.LastHeartbeat}}{{else}}<span class="empty">(not set)</span>{{end}}</dd></div>
            <div class="detail-card"><dt>Bridge Replay Cursor</dt><dd id="health-replay-cursor">{{if .BridgeReplayCursor}}{{.BridgeReplayCursor}}{{else}}<span class="empty">(not set)</span>{{end}}</dd></div>
            <div class="detail-card"><dt>Relay Source Cursor</dt><dd id="health-relay-cursor">{{if .RelaySourceCursor}}{{.RelaySourceCursor}}{{else}}<span class="empty">(not set)</span>{{end}}</dd></div>
            <div class="detail-card"><dt>Event-Log Head</dt><dd id="health-event-log-head">{{if .EventLogHeadCursor}}{{.EventLogHeadCursor}}{{else}}<span class="empty">(not set)</span>{{end}}</dd></div>
        </dl>
    </article>

    <article class="section section-pad">
        <h2 class="page-title" style="font-size:1.2rem">Top Deferred Reasons</h2>
        <ul class="mini-list" style="margin-top:10px; {{if not .TopDeferredReasons}}display:none{{end}}" id="list-deferred-reasons">
            {{range .TopDeferredReasons}}
            <li>
                <div class="mono truncate" title="{{.Reason}}">{{.Reason}}</div>
                <a class="button-link" href="{{.MessagesURL}}">{{.Count}} msgs</a>
            </li>
            {{end}}
        </ul>
        <div class="empty" id="empty-deferred-reasons" {{if .TopDeferredReasons}}style="display:none"{{end}}>No deferred reasons recorded.</div>
    </article>
</section>

<section class="section section-pad">
    <h2 class="page-title" style="font-size:1.2rem">Accounts With Highest Issue Volume</h2>
    <div class="table-wrap" style="margin-top:10px; {{if not .TopIssueAccounts}}display:none{{end}}" id="wrap-issue-accounts">
        <table>
            <thead>
                <tr>
                    <th>AT DID</th>
                    <th>Status</th>
                    <th>Issue Msgs</th>
                    <th>Total Msgs</th>
                    <th>Breakdown</th>
                    <th>Pivot</th>
                </tr>
            </thead>
            <tbody id="table-issue-accounts">
                {{range .TopIssueAccounts}}
                <tr>
                    <td class="mono"><span class="truncate" title="{{.ATDID}}">{{.ATDID}}</span></td>
                    <td>{{if .Active}}active{{else}}inactive{{end}}</td>
                    <td>{{.IssueMessages}}</td>
                    <td>{{.TotalMessages}}</td>
                    <td>F{{.FailedMessages}} / D{{.DeferredCount}} / X{{.DeletedCount}}</td>
                    <td><a class="button-link" href="{{.MessagesURL}}">View Messages</a></td>
                </tr>
                {{end}}
            </tbody>
        </table>
    </div>
    <div class="empty" id="empty-issue-accounts" {{if .TopIssueAccounts}}style="display:none"{{end}}>No issue-heavy accounts yet.</div>
</section>
</section>

<script>
    if (!!window.EventSource) {
        var source = new EventSource('/events');
        source.onmessage = function(e) {
            try {
                var stats = JSON.parse(e.data);
                
                // Update Metrics counters
                if (stats.Metrics) {
                    stats.Metrics.forEach(function(m) {
                        var el = document.getElementById('metric-' + m.ID);
                        if (el) el.innerText = m.Value;
                    });
                }
                
                // Update Health Details
                var subtitle = document.getElementById('health-subtitle');
                if (subtitle) {
                    subtitle.innerHTML = '<strong>' + stats.RuntimeHealth + '</strong> &middot; ' + stats.RuntimeHealthDescription;
                }
                var updateText = function(id, text) {
                    var el = document.getElementById(id);
                    if (el) el.innerText = text || "(not set)";
                };
                updateText('health-bridge-status', stats.BridgeStatus);
                updateText('health-last-heartbeat', stats.LastHeartbeat);
                updateText('health-replay-cursor', stats.BridgeReplayCursor);
                updateText('health-relay-cursor', stats.RelaySourceCursor);
                updateText('health-event-log-head', stats.EventLogHeadCursor);
                
                // Update Deferred Reasons
                var defList = document.getElementById('list-deferred-reasons');
                var defEmpty = document.getElementById('empty-deferred-reasons');
                if (defList && defEmpty) {
                    if (stats.TopDeferredReasons && stats.TopDeferredReasons.length > 0) {
                        defList.style.display = '';
                        defEmpty.style.display = 'none';
                        var html = '';
                        stats.TopDeferredReasons.forEach(function(r) {
                            html += '<li><div class="mono truncate" title="'+r.Reason+'">'+r.Reason+'</div>' +
                                '<a class="button-link" href="'+r.MessagesURL+'">'+r.Count+' msgs</a></li>';
                        });
                        defList.innerHTML = html;
                    } else {
                        defList.style.display = 'none';
                        defEmpty.style.display = '';
                    }
                }
                
                // Update Accounts
                var accWrap = document.getElementById('wrap-issue-accounts');
                var accBody = document.getElementById('table-issue-accounts');
                var accEmpty = document.getElementById('empty-issue-accounts');
                if (accWrap && accBody && accEmpty) {
                    if (stats.TopIssueAccounts && stats.TopIssueAccounts.length > 0) {
                        accWrap.style.display = '';
                        accEmpty.style.display = 'none';
                        var html = '';
                        stats.TopIssueAccounts.forEach(function(a) {
                            var status = a.Active ? "active" : "inactive";
                            html += '<tr>' +
                                '<td class="mono"><span class="truncate" title="'+a.ATDID+'">'+a.ATDID+'</span></td>' +
                                '<td>'+status+'</td>' +
                                '<td>'+a.IssueMessages+'</td>' +
                                '<td>'+a.TotalMessages+'</td>' +
                                '<td>F'+a.FailedMessages+' / D'+a.DeferredCount+' / X'+a.DeletedCount+'</td>' +
                                '<td><a class="button-link" href="'+a.MessagesURL+'">View Messages</a></td>' +
                                '</tr>';
                        });
                        accBody.innerHTML = html;
                    } else {
                        accWrap.style.display = 'none';
                        accEmpty.style.display = '';
                    }
                }

            } catch (err) {
                console.error("SSE parse error", err);
            }
        };
    }
</script>
{{end}}
`

const accountsContent = `
{{define "content"}}
<section class="section section-pad">
    <h1 class="page-title">Accounts</h1>
    <p class="subtitle">Bridged account registry with per-account message and issue statistics.</p>
</section>

<section class="section">
    <div class="table-wrap">
        <table>
            <thead>
                <tr>
                    <th>AT DID</th>
                    <th style="min-width:140px">Status</th>
                    <th>Total</th>
                    <th>Published</th>
                    <th>Failed</th>
                    <th>Deferred</th>
                    <th>Last Published</th>
                    <th>Created</th>
                    <th>Actions</th>
                </tr>
            </thead>
            <tbody>
                {{range .Accounts}}
                <tr>
                    <td class="mono">
                        <span class="truncate" title="{{.ATDID}}">{{.ATDID}}</span>
                        <div style="font-size:0.7rem;color:var(--muted);margin-top:2px">{{.SSBFeedID}}</div>
                    </td>
                    <td>
                        <div class="status-badge {{.SyncStateClass}}">{{.SyncStateLabel}}</div>
                        {{if .LastError}}
                        <div class="issue-text" style="font-size:0.7rem;margin-top:4px" title="{{.LastError}}">
                            Error: {{.LastError}}
                        </div>
                        {{end}}
                    </td>
                    <td>{{.TotalMessages}}</td>
                    <td>{{.PublishedMessages}}</td>
                    <td class="{{if gt .FailedMessages 0}}tone-danger{{end}}">{{.FailedMessages}}</td>
                    <td class="{{if gt .DeferredMessages 0}}tone-warning{{end}}">{{.DeferredMessages}}</td>
                        <td>{{if .LastPublishedAt}}{{.LastPublishedAt}}{{else}}<span class="empty">(none)</span>{{end}}</td>
                    <td style="font-size:0.8rem;white-space:nowrap">{{fmtTime .CreatedAt}}</td>
	                    <td>
	                        <div style="display:flex;gap:6px;align-items:center">
	                            <a class="button-link" href="{{.MessagesURL}}">Messages</a>
	                            <form method="POST" action="/accounts/backfill?at_did={{.ATDID}}" style="margin:0">
	                                {{csrfField $.Chrome.CSRFToken}}
	                                <button type="submit" class="button-link tone-success" title="Force immediate backfill/resync">Backfill</button>
	                            </form>
	                            <form method="POST" action="/accounts/remove" onsubmit="return confirm('Remove this bridged account?')" style="margin:0">
	                                {{csrfField $.Chrome.CSRFToken}}
	                                <input type="hidden" name="at_did" value="{{.ATDID}}">
	                                <button type="submit" class="button-link tone-danger">Remove</button>
	                            </form>
                        </div>
                    </td>
                </tr>
                {{else}}
                <tr><td colspan="9" class="empty">No bridged accounts yet.</td></tr>
                {{end}}
            </tbody>
        </table>
    </div>
</section>

	<section class="section section-pad">
	    <h2 class="page-title" style="font-size:1.2rem">Add Bridged Account</h2>
	    <p class="subtitle">Register a new ATProto DID; the SSB feed identity will be derived automatically.</p>
	    <form action="/accounts" method="POST" style="display:grid; gap:12px; grid-template-columns: 1fr auto; align-items: end; margin-top:8px">
	        {{csrfField .Chrome.CSRFToken}}
	        <div class="field">
            <label for="at_did">ATProto DID</label>
            <input type="text" id="at_did" name="at_did" placeholder="did:plc:abcdef..." required>
        </div>
        <button type="submit" class="button">Add Account</button>
    </form>
</section>
{{end}}
`

const messagesContent = `
{{define "content"}}
<section class="section section-pad">
    <h1 class="page-title">Messages</h1>
    <p class="subtitle">Filter and paginate bridged records, then pivot to detail views for payload and lifecycle diagnostics.</p>
</section>

<section class="filter-panel" aria-label="Message filters">
    <form method="GET" action="/messages">
        <div class="filter-grid">
            <div class="field" style="grid-column: span 2;">
                <label for="messages-search">Search</label>
                <input id="messages-search" type="search" name="q" value="{{.Filters.Search}}" placeholder="URI, DID, SSB ref, error text">
            </div>
            <div class="field">
                <label for="messages-did">Author DID</label>
                <input id="messages-did" type="text" name="did" value="{{.Filters.ATDID}}" placeholder="did:plc:...">
            </div>
            <div class="field">
                <label for="messages-type">Type</label>
                <select id="messages-type" name="type">
                    {{range .TypeOptions}}<option value="{{.Value}}" {{if .Selected}}selected{{end}}>{{.Label}}</option>{{end}}
                </select>
            </div>
            <div class="field">
                <label for="messages-state">State</label>
                <select id="messages-state" name="state">
                    {{range .StateOptions}}<option value="{{.Value}}" {{if .Selected}}selected{{end}}>{{.Label}}</option>{{end}}
                </select>
            </div>
            <div class="field">
                <label for="messages-sort">Sort</label>
                <select id="messages-sort" name="sort">
                    {{range .SortOptions}}<option value="{{.Value}}" {{if .Selected}}selected{{end}}>{{.Label}}</option>{{end}}
                </select>
            </div>
            <div class="field">
                <label for="messages-limit">Page Size</label>
                <select id="messages-limit" name="limit">
                    {{range .LimitOptions}}<option value="{{.Value}}" {{if .Selected}}selected{{end}}>{{.Label}}</option>{{end}}
                </select>
            </div>
            <div class="field" style="align-content:end">
                <label class="inline-toggle"><input type="checkbox" name="has_issue" value="1" {{if .Filters.HasIssue}}checked{{end}}>Only rows with issue text</label>
            </div>
            <div class="field" style="align-content:end">
                <div class="toolbar">
                    <button class="button" type="submit">Apply</button>
                    <a class="button-link" href="/messages">Reset</a>
                </div>
            </div>
        </div>
    </form>
    <div class="toolbar" style="padding:0 14px 12px">
        <div class="subtitle">Showing <strong>{{.ResultCount}}</strong> rows{{if .UnsupportedKeysetSort}} · keyset pagination available on newest/oldest sorts{{end}}</div>
        {{if .ActiveFilters}}
        <div class="active-filters" aria-label="Active filters">
            {{range .ActiveFilters}}<span class="chip"><strong>{{.Label}}</strong> {{.Value}}</span>{{end}}
        </div>
        {{end}}
    </div>
</section>

<section class="section">
    <div class="table-wrap">
        <table>
            <thead>
                <tr>
                    <th>AT URI</th>
                    <th>Author DID</th>
                    <th>Type</th>
                    <th>State</th>
                    <th>SSB Ref</th>
                    <th>Retries</th>
                    <th>Issue</th>
                    <th>Created</th>
                </tr>
            </thead>
            <tbody>
                {{range .Messages}}
                <tr>
                    <td>
                        <div class="mono">
                            <a href="{{.DetailURL}}" title="{{.ATURI}}"><span class="truncate">{{.ShortATURI}}</span></a>
                        </div>
                        <div class="toolbar" style="margin-top:6px">
                            <a class="button-link" href="{{.DetailURL}}">Detail</a>
                            <button class="copy-btn" type="button" data-copy="{{.ATURI}}">Copy</button>
                        </div>
                    </td>
                    <td>
                        <div class="mono"><span class="truncate" title="{{.ATDID}}">{{.ShortATDID}}</span></div>
                        <button class="copy-btn" type="button" data-copy="{{.ATDID}}" style="margin-top:6px">Copy</button>
                    </td>
                    <td>
                        <div><strong>{{.TypeLabel}}</strong></div>
                        <div class="mono"><span class="truncate" title="{{.Type}}">{{.Type}}</span></div>
                    </td>
                    <td><span class="pill {{.StateClass}}">{{.StateLabel}}</span></td>
                    <td>
                        {{if .SSBMsgRef}}
                        <div class="mono"><span class="truncate" title="{{.SSBMsgRef}}">{{.ShortSSBMsgRef}}</span></div>
                        <button class="copy-btn" type="button" data-copy="{{.SSBMsgRef}}" style="margin-top:6px">Copy</button>
                        {{else}}pending{{end}}
                    </td>
                    <td>{{.TotalAttempts}}<br><span class="subtitle">P{{.PublishAttempts}} / D{{.DeferAttempts}}</span></td>
                    <td>
                        <div class="issue-text {{.IssueClass}}">{{.IssueText}}</div>
                        {{if .IssueDetail}}
                        <details style="margin-top:6px"><summary class="subtitle">Show full issue</summary><div class="mono" style="margin-top:5px">{{.IssueDetail}}</div></details>
                        {{end}}
                    </td>
                    <td>{{fmtTime .CreatedAt}}</td>
                </tr>
                {{else}}
                <tr><td colspan="8" class="empty">No bridged messages matched the current filters.</td></tr>
                {{end}}
            </tbody>
        </table>
    </div>
    <div class="pagination">
        <div class="subtitle">Use newest/oldest sort for stable cursor pagination.</div>
        <div class="toolbar">
            {{if .Pagination.HasPrev}}<a class="button-link" href="{{.Pagination.PrevURL}}">Previous</a>{{end}}
            {{if .Pagination.HasNext}}<a class="button-link" href="{{.Pagination.NextURL}}">Next</a>{{end}}
        </div>
    </div>
</section>
{{end}}
`

const messageDetailContent = `
{{define "content"}}
<section class="section section-pad">
    <div class="toolbar">
        <div>
            <a class="button-link" href="/messages">Back to Messages</a>
            <h1 class="page-title" style="margin-top:10px">Message Detail</h1>
            <p class="subtitle mono" title="{{.ATURI}}"><span class="truncate">{{.ATURI}}</span></p>
        </div>
        <div>
            <span class="pill {{stateClass .State}}">{{.State}}</span>
            {{if .ThreadURL}}
            <a href="{{.ThreadURL}}" class="button-link" style="margin-left:8px;font-weight:700;color:var(--brand);">View Thread</a>
            {{end}}
	            {{if or (eq .State "failed") (eq .State "deferred")}}
	            <form action="/messages/retry" method="POST" style="display:inline-block;margin-left:8px">
	                {{csrfField .Chrome.CSRFToken}}
	                <input type="hidden" name="at_uri" value="{{.ATURI}}">
	                <button type="submit" class="button-link tone-success" style="cursor:pointer;font-weight:700">Retry Publishing</button>
	            </form>
            {{end}}
        </div>
    </div>
</section>

<section class="section section-pad">
    <div class="toolbar">
        <a class="button-link" href="{{.FilterByDIDURL}}">More from this DID</a>
        <a class="button-link" href="{{.FilterByStateURL}}">More in this state</a>
        <a class="button-link" href="{{.FilterByTypeURL}}">More of this type</a>
    </div>
</section>

<section class="section section-pad">
    <h2 class="page-title" style="font-size:1.2rem">Lifecycle Timeline</h2>
    <div class="details-grid" style="margin-top:10px">
        <div class="detail-card"><dt>Created</dt><dd>{{.CreatedAt}}</dd></div>
        <div class="detail-card"><dt>Published</dt><dd>{{if .PublishedAt}}{{.PublishedAt}}{{else}}<span class="empty">(not published)</span>{{end}}</dd></div>
        <div class="detail-card"><dt>Last Publish Attempt</dt><dd>{{if .LastPublishAttemptAt}}{{.LastPublishAttemptAt}}{{else}}<span class="empty">(none)</span>{{end}}</dd></div>
        <div class="detail-card"><dt>Last Defer Attempt</dt><dd>{{if .LastDeferAttemptAt}}{{.LastDeferAttemptAt}}{{else}}<span class="empty">(none)</span>{{end}}</dd></div>
        <div class="detail-card"><dt>Deleted At</dt><dd>{{if .DeletedAt}}{{.DeletedAt}}{{else}}<span class="empty">(not deleted)</span>{{end}}</dd></div>
        <div class="detail-card"><dt>Deleted Seq</dt><dd>{{if .DeletedSeq}}{{.DeletedSeq}}{{else}}<span class="empty">(none)</span>{{end}}</dd></div>
    </div>
</section>

<section class="grid-two">
    <article class="section section-pad">
        <h2 class="page-title" style="font-size:1.2rem">Record Metadata</h2>
        <dl class="details-grid" style="margin-top:10px">
            <div class="detail-card"><dt>Author DID</dt><dd class="mono">{{.ATDID}}</dd></div>
            <div class="detail-card"><dt>Record Type</dt><dd>{{.Type}}</dd></div>
            <div class="detail-card"><dt>AT CID</dt><dd class="mono">{{if .ATCID}}{{.ATCID}}{{else}}<span class="empty">(none)</span>{{end}}</dd></div>
            <div class="detail-card"><dt>SSB Ref</dt><dd class="mono">{{if .SSBMsgRef}}{{.SSBMsgRef}}{{else}}<span class="empty">pending</span>{{end}}</dd></div>
            <div class="detail-card"><dt>Publish Attempts</dt><dd>{{.PublishAttempts}}</dd></div>
            <div class="detail-card"><dt>Defer Attempts</dt><dd>{{.DeferAttempts}}</dd></div>
        </dl>
    </article>

    <article class="section section-pad">
        <h2 class="page-title" style="font-size:1.2rem">Lifecycle Issues</h2>
        <dl class="details-grid" style="margin-top:10px">
            {{if .PublishError}}<div class="detail-card"><dt>Publish Error</dt><dd class="issue-text">{{.PublishError}}</dd></div>{{end}}
            {{if .DeferReason}}<div class="detail-card"><dt>Defer Reason</dt><dd class="issue-text warning">{{.DeferReason}}</dd></div>{{end}}
            {{if .DeletedReason}}<div class="detail-card"><dt>Deleted Reason</dt><dd>{{.DeletedReason}}</dd></div>{{end}}
            {{if and (eq .PublishError "") (eq .DeferReason "") (eq .DeletedReason "")}}<div class="detail-card"><dt>Issues</dt><dd class="issue-text muted">No issue details recorded.</dd></div>{{end}}
        </dl>
    </article>
</section>

<section class="grid-two">
    <article class="section section-pad">
        <h2 class="page-title" style="font-size:1.2rem">Original ATProto Message</h2>
        <dl class="details-grid" style="margin-top:10px">
            {{range .OriginalMessageFields}}
            <div class="detail-card"><dt>{{.Label}}</dt><dd>{{.Value}}</dd></div>
            {{else}}
            <div class="empty">No structured ATProto fields available.</div>
            {{end}}
        </dl>
    </article>

    <article class="section section-pad">
        <h2 class="page-title" style="font-size:1.2rem">Bridged SSB Message</h2>
        <dl class="details-grid" style="margin-top:10px">
            {{range .BridgedMessageFields}}
            <div class="detail-card"><dt>{{.Label}}</dt><dd>{{.Value}}</dd></div>
            {{else}}
            <div class="empty">No structured SSB fields available.</div>
            {{end}}
        </dl>
    </article>
</section>

<section class="grid-two">
    <article class="section">
        <div class="section-pad"><h2 class="page-title" style="font-size:1.2rem">Raw ATProto JSON</h2></div>
        <pre class="section-pad" style="background:var(--bg-subtle);overflow:auto;max-height:600px;font-size:0.85rem">{{.RawATProtoJSON}}</pre>
    </article>
 
    <article class="section">
        <div class="section-pad"><h2 class="page-title" style="font-size:1.2rem">Raw SSB JSON</h2></div>
        <pre class="section-pad" style="background:var(--bg-subtle);overflow:auto;max-height:600px;font-size:0.85rem">{{.RawSSBJSON}}</pre>
    </article>
</section>

{{if .AssociatedBlobs}}
<section class="section section-pad">
    <h2 class="page-title" style="font-size:1.2rem">Associated Blobs</h2>
    <div class="table-wrap" style="margin-top:10px">
        <table>
            <thead>
                <tr><th>AT CID</th><th>SSB Blob Ref</th><th>Size</th><th>MIME</th><th>Actions</th></tr>
            </thead>
            <tbody>
                {{range .AssociatedBlobs}}
                <tr>
                    <td class="mono"><span class="truncate" title="{{.ATCID}}">{{.ATCID}}</span></td>
                    <td class="mono"><span class="truncate" title="{{.SSBBlobRef}}">{{.SSBBlobRef}}</span></td>
                    <td>{{humanizeBytes .Size}}</td>
                    <td>{{.MimeType}}</td>
                    <td>
                        <a href="/blobs/view?ref={{.SSBBlobRef | urlquery}}" target="_blank" class="button button-small">View</a>
                        {{if hasPrefix .MimeType "image/"}}
                        <div style="margin-top:8px">
                            <img src="/blobs/view?ref={{.SSBBlobRef | urlquery}}" style="max-width:200px; max-height:200px; border-radius:4px; border:1px solid var(--line)" alt="Preview">
                        </div>
                        {{end}}
                    </td>
                </tr>
                {{end}}
            </tbody>
        </table>
    </div>
</section>
{{end}}
{{end}}
`

const feedContent = `
{{define "content"}}
<section class="section section-pad">
    <h1 class="page-title">Global Feed</h1>
    <p class="subtitle">Unified chronological stream of all bridged messages.</p>
</section>

{{if .Feed}}
<section class="section">
    {{range .Feed}}
    <article class="section section-pad" style="border-bottom:1px solid var(--line);margin-bottom:16px;padding-bottom:16px;">
        <div style="display:flex;gap:12px;align-items:flex-start;">
            {{if .HasImage}}
            <div style="flex-shrink:0;">
                <img src="/blobs/view?ref={{.ImageRef | urlquery}}" alt="SSB image" style="max-width:150px;border-radius:8px;">
            </div>
            {{end}}
            <div style="flex:1;">
                <div class="mono" title="{{.ATDID}}"><span class="truncate">{{.ATDID}}</span></div>
                <div style="font-size:0.9rem;color:var(--muted);margin-bottom:4px;">{{.Type}}</div>
                <div class="mono" style="font-size:1.1rem;line-height:1.4;">{{formatSSBText .Text}}</div>
                <div style="font-size:0.8rem;color:var(--muted);margin-top:8px;">
                    {{fmtTime .CreatedAt}}
                    {{if .ThreadURL}}
                    <span style="margin-left:8px;">·</span>
                    <a href="{{.ThreadURL}}" style="color:var(--brand);font-weight:700;text-decoration:none;">View Thread</a>
                    {{end}}
                    <span style="margin-left:8px;">·</span>
                    <a href="/messages/detail?at_uri={{.ATURI | urlquery}}" style="color:inherit;text-decoration:none;">Details</a>
                </div>
            </div>
        </div>
    </article>
    {{end}}
</section>
{{else}}
<div class="empty">No messages in the global feed yet.</div>
{{end}}

<div class="toolbar" style="margin-top:16px;">
    <a class="button-link" href="/feed?limit=25">Show 25</a>
    <a class="button-link" href="/feed?limit=50" style="margin-left:8px;">Show 50</a>
    <a class="button-link" href="/feed?limit=100" style="margin-left:8px;">Show 100</a>
</div>
{{end}}
`

const threadContent = `
{{define "content"}}
<section class="section section-pad">
    <div class="toolbar">
        <div>
            <h1 class="page-title">Conversation Thread</h1>
            <p class="subtitle mono" title="{{.RootURI}}"><span class="truncate">{{.RootURI}}</span></p>
            {{if .TangleStats}}
            <div style="margin-top:12px;padding:10px;background:var(--bg-subtle);border-radius:8px;font-size:0.85rem;">
                <span style="color:var(--muted);">Tangle:</span>
                <span style="font-weight:600;">{{.TangleStats.Name}}</span>
                <span style="color:var(--muted);margin-left:12px;">Messages:</span>
                <span>{{.TangleStats.MessageCount}}</span>
                <span style="color:var(--muted);margin-left:12px;">Tips:</span>
                <span>{{.TangleStats.TipCount}}</span>
                <span style="color:var(--muted);margin-left:12px;">Depth:</span>
                <span>{{.TangleStats.Depth}}</span>
            </div>
            {{end}}
        </div>
        <a class="button-link" href="/feed">Back to Feed</a>
    </div>
</section>

<section class="section" style="background:transparent;border:none;box-shadow:none;">
    {{range .Nodes}}
        {{template "thread-node" .}}
    {{end}}
</section>
{{end}}

{{define "thread-node"}}
<div class="thread-node" style="margin-left: {{multiply .Depth 24}}px; border-left: {{if gt .Depth 0}}2px solid var(--line){{else}}none{{end}}; padding-left: {{if gt .Depth 0}}16px{{else}}0{{end}}; margin-top: 12px; margin-bottom: 20px;">
    <article style="background: var(--panel); border: 1px solid var(--line); border-radius: 12px; padding: 14px; box-shadow: var(--shadow); position: relative;">
        {{if gt .Depth 0}}
        <div style="position: absolute; left: -16px; top: 30px; width: 16px; height: 2px; background: var(--line);"></div>
        {{end}}
        <div style="display:flex;gap:12px;align-items:flex-start;">
            <div style="flex:1;">
                <div class="mono" title="{{.ATDID}}" style="font-size:0.85rem;font-weight:700;color:var(--brand-strong);margin-bottom:2px;"><span class="truncate">{{.ATDID}}</span></div>
                <div class="mono" style="font-size:1.05rem;line-height:1.5;">{{formatSSBText .Text}}</div>
                <div style="font-size:0.75rem;color:var(--muted);margin-top:8px;display:flex;align-items:center;gap:8px;">
                     <span>{{fmtTime .CreatedAt}}</span>
                     <span>·</span>
                     <a href="/messages/detail?at_uri={{.ATURI | urlquery}}" style="color:inherit;text-decoration:none;font-weight:600;">Details</a>
                </div>
            </div>
        </div>
    </article>
    {{range .Replies}}
        {{template "thread-node" .}}
    {{end}}
</div>
{{end}}
`

const failuresContent = `
{{define "content"}}
<section class="section section-pad">
    <h1 class="page-title">Publish/Defer Issues</h1>
    <p class="subtitle">Split by lifecycle state with grouped reason hotspots for rapid triage.</p>
</section>

<section class="section section-pad">
    <div class="toolbar">
        <div class="metric-card tone-danger" style="max-width:220px"><span class="metric-label">Failed</span><span class="metric-value">{{.FailedCount}}</span></div>
        <div class="metric-card tone-warning" style="max-width:220px"><span class="metric-label">Deferred</span><span class="metric-value">{{.DeferredCount}}</span></div>
    </div>
</section>

<section class="section section-pad">
    <h2 class="page-title" style="font-size:1.2rem">Reason Groups</h2>
    {{if .ReasonGroups}}
    <ul class="mini-list" style="margin-top:10px">
        {{range .ReasonGroups}}
        <li>
            <span class="mono truncate" title="{{.Reason}}">{{.State}} · {{.Reason}}</span>
            <span class="pill">{{.Count}}</span>
        </li>
        {{end}}
    </ul>
    {{else}}
    <div class="empty">No grouped reasons available.</div>
    {{end}}
</section>

<section class="grid-two">
    <article class="section">
        <div class="section-pad"><h2 class="page-title" style="font-size:1.2rem">Failed</h2></div>
        <div class="table-wrap">
            <table>
                <thead><tr><th>AT URI</th><th>DID</th><th>Type</th><th>Attempts</th><th>Reason</th><th>Created</th></tr></thead>
                <tbody>
                    {{range .FailedRows}}
                    <tr>
                        <td class="mono"><span class="truncate" title="{{.ATURI}}">{{.ATURI}}</span></td>
                        <td class="mono">{{.ATDID}}</td>
                        <td>{{.Type}}</td>
                        <td>{{.PublishAttempts}}</td>
                        <td class="issue-text">{{.Reason}}</td>
                        <td>{{fmtTime .CreatedAt}}</td>
                    </tr>
                    {{else}}<tr><td colspan="6" class="empty">No failed rows.</td></tr>{{end}}
                </tbody>
            </table>
        </div>
    </article>

    <article class="section">
        <div class="section-pad"><h2 class="page-title" style="font-size:1.2rem">Deferred</h2></div>
        <div class="table-wrap">
            <table>
                <thead><tr><th>AT URI</th><th>DID</th><th>Type</th><th>Attempts</th><th>Reason</th><th>Created</th></tr></thead>
                <tbody>
                    {{range .DeferredRows}}
                    <tr>
                        <td class="mono"><span class="truncate" title="{{.ATURI}}">{{.ATURI}}</span></td>
                        <td class="mono">{{.ATDID}}</td>
                        <td>{{.Type}}</td>
                        <td>{{.PublishAttempts}}</td>
                        <td class="issue-text warning">{{.Reason}}</td>
                        <td>{{fmtTime .CreatedAt}}</td>
                    </tr>
                    {{else}}<tr><td colspan="6" class="empty">No deferred rows.</td></tr>{{end}}
                </tbody>
            </table>
        </div>
    </article>
</section>
{{end}}
`

const blobsContent = `
{{define "content"}}
<section class="section section-pad">
    <h1 class="page-title">Blob Sync Status</h1>
    <p class="subtitle">Most recent bridged blob mappings and metadata.</p>
</section>

<section class="section">
    <div class="table-wrap">
        <table>
            <thead>
                <tr><th>AT CID</th><th>SSB Blob Ref</th><th>Size</th><th>MIME</th><th>Downloaded</th><th>Actions</th></tr>
            </thead>
            <tbody>
                {{range .Blobs}}
                <tr>
                    <td class="mono"><span class="truncate" title="{{.ATCID}}">{{.ATCID}}</span></td>
                    <td class="mono"><span class="truncate" title="{{.SSBBlobRef}}">{{.SSBBlobRef}}</span></td>
                    <td>{{humanizeBytes .Size}}</td>
                    <td>{{.MimeType}}</td>
                    <td>{{fmtTime .DownloadedAt}}</td>
                    <td>
                        <a href="/blobs/view?ref={{.SSBBlobRef | urlquery}}" target="_blank" class="button button-small">View</a>
                        {{if hasPrefix .MimeType "image/"}}
                        <div style="margin-top:8px">
                            <img src="/blobs/view?ref={{.SSBBlobRef | urlquery}}" style="max-width:100px; max-height:100px; border-radius:4px; border:1px solid var(--line)" alt="Preview">
                        </div>
                        {{end}}
                    </td>
                </tr>
                {{else}}
                <tr><td colspan="5" class="empty">No blobs bridged yet.</td></tr>
                {{end}}
            </tbody>
        </table>
    </div>
</section>
{{end}}
`

const stateContent = `
{{define "content"}}
<section class="section section-pad">
    <h1 class="page-title">Bridge State</h1>
    <p class="subtitle">Grouped runtime keys with stale heartbeat visibility for operational triage.</p>
</section>

<section class="section section-pad">
    <div class="metric-grid">
        <div class="metric-card tone-warning"><span class="metric-label">Deferred Count</span><span class="metric-value">{{.DeferredCount}}</span></div>
        <div class="metric-card"><span class="metric-label">Deleted Count</span><span class="metric-value">{{.DeletedCount}}</span></div>
        <div class="metric-card {{if .HeartbeatStale}}tone-warning{{else}}tone-success{{end}}"><span class="metric-label">Heartbeat</span><span class="metric-value">{{if .HeartbeatStale}}stale{{else}}fresh{{end}}</span><span class="metric-note">{{if .HeartbeatAge}}{{.HeartbeatAge}}{{else}}unknown{{end}}</span></div>
        <div class="metric-card"><span class="metric-label">Latest Defer Reason</span><span class="metric-note mono" title="{{.LatestDeferReason}}">{{if .LatestDeferReason}}{{.LatestDeferReason}}{{else}}(none){{end}}</span></div>
    </div>
</section>

<section class="grid-two">
    <article class="section">
        <div class="section-pad"><h2 class="page-title" style="font-size:1.2rem">Runtime Keys</h2></div>
        {{template "stateRows" .RuntimeState}}
    </article>
    <article class="section">
        <div class="section-pad"><h2 class="page-title" style="font-size:1.2rem">ATProto / Firehose Keys</h2></div>
        {{template "stateRows" .FirehoseState}}
    </article>
</section>

<section class="section">
    <div class="section-pad"><h2 class="page-title" style="font-size:1.2rem">Other Keys</h2></div>
    {{template "stateRows" .OtherState}}
</section>
{{end}}

{{define "stateRows"}}
<div class="table-wrap">
    <table>
        <thead><tr><th>Key</th><th>Value</th><th>Updated</th></tr></thead>
        <tbody>
            {{range .}}
            <tr>
                <td class="mono">{{.Key}}</td>
                <td class="mono"><span class="truncate" title="{{.Value}}">{{.Value}}</span></td>
                <td>{{fmtTime .UpdatedAt}}</td>
            </tr>
            {{else}}
            <tr><td colspan="3" class="empty">No entries.</td></tr>
            {{end}}
        </tbody>
    </table>
</div>
{{end}}
`

const reverseContent = `
{{define "content"}}
<section class="section section-pad">
    <h1 class="page-title">Reverse Sync</h1>
    <p class="subtitle">Manage the SSB allowlist and inspect the durable SSB-to-ATProto queue.</p>
</section>

<section class="section section-pad">
    <div class="metric-grid">
        <div class="metric-card {{if .Enabled}}tone-success{{else}}tone-warning{{end}}">
            <span class="metric-label">Runtime</span>
            <span class="metric-value">{{if .Enabled}}enabled{{else}}disabled{{end}}</span>
        </div>
        <div class="metric-card">
            <span class="metric-label">Mappings</span>
            <span class="metric-value">{{len .Mappings}}</span>
        </div>
        <div class="metric-card">
            <span class="metric-label">Queue Rows</span>
            <span class="metric-value">{{len .Events}}</span>
        </div>
    </div>
</section>

<section class="section section-pad" style="max-width:960px">
    <h2 class="page-title" style="font-size:1.2rem">Add Or Update Mapping</h2>
    <form action="/reverse/mappings" method="POST" style="display:grid;gap:16px;grid-template-columns:repeat(auto-fit,minmax(220px,1fr));align-items:end">
        {{csrfField .Chrome.CSRFToken}}
        <label>
            <span class="metric-label">SSB Feed</span>
            <input type="text" name="ssb_feed_id" placeholder="@alice.ed25519" style="width:100%;padding:12px;border:1px solid var(--line);border-radius:8px">
        </label>
        <label>
            <span class="metric-label">AT DID</span>
            <input type="text" name="at_did" placeholder="did:plc:example" style="width:100%;padding:12px;border:1px solid var(--line);border-radius:8px">
        </label>
        <label><input type="checkbox" name="active" value="true" checked> Active</label>
        <label><input type="checkbox" name="allow_posts" value="true" checked> Posts</label>
        <label><input type="checkbox" name="allow_replies" value="true" checked> Replies</label>
        <label><input type="checkbox" name="allow_follows" value="true" checked> Follows</label>
        <div><button type="submit" class="button-link tone-success" style="padding:12px 24px">Save Mapping</button></div>
    </form>
</section>

<section class="section">
    <div class="section-pad"><h2 class="page-title" style="font-size:1.2rem">Allowlisted Feeds</h2></div>
    <div class="table-wrap">
        <table>
            <thead><tr><th>SSB Feed</th><th>AT DID</th><th>Scope</th><th>Credentials</th><th>Updated</th><th>Disable</th></tr></thead>
            <tbody>
                {{range .Mappings}}
                <tr>
                    <td class="mono"><span class="truncate" title="{{.SSBFeedID}}">{{.SSBFeedID}}</span></td>
                    <td class="mono"><span class="truncate" title="{{.ATDID}}">{{.ATDID}}</span></td>
                    <td>{{if .AllowPosts}}post{{end}}{{if .AllowReplies}} {{if .AllowPosts}}/{{end}}reply{{end}}{{if .AllowFollows}} {{if or .AllowPosts .AllowReplies}}/{{end}}follow{{end}}</td>
                    <td><span class="state-pill {{.CredentialClass}}">{{.CredentialStatus}}</span></td>
                    <td>{{fmtTime .UpdatedAt}}</td>
                    <td>
                        {{if .Active}}
                        <form action="/reverse/mappings/remove" method="POST">
                            {{csrfField $.Chrome.CSRFToken}}
                            <input type="hidden" name="ssb_feed_id" value="{{.SSBFeedID}}">
                            <button type="submit" class="button-link">Disable</button>
                        </form>
                        {{else}}
                        <span class="state-pill state-deleted">inactive</span>
                        {{end}}
                    </td>
                </tr>
                {{else}}
                <tr><td colspan="6" class="empty">No reverse mappings yet.</td></tr>
                {{end}}
            </tbody>
        </table>
    </div>
</section>

<section class="section section-pad">
    <h2 class="page-title" style="font-size:1.2rem">Queue Filters</h2>
    <form action="/reverse" method="GET" style="display:grid;gap:16px;grid-template-columns:2fr 1fr 1fr auto;align-items:end">
        <label>
            <span class="metric-label">Search</span>
            <input type="text" name="q" value="{{.Filters.Search}}" placeholder="msg ref, feed, DID, AT URI" style="width:100%;padding:12px;border:1px solid var(--line);border-radius:8px">
        </label>
        <label>
            <span class="metric-label">State</span>
            <select name="state" style="width:100%;padding:12px;border:1px solid var(--line);border-radius:8px">
                {{range .StateOptions}}<option value="{{.Value}}" {{if .Selected}}selected{{end}}>{{.Label}}</option>{{end}}
            </select>
        </label>
        <label>
            <span class="metric-label">Action</span>
            <select name="action" style="width:100%;padding:12px;border:1px solid var(--line);border-radius:8px">
                {{range .ActionOptions}}<option value="{{.Value}}" {{if .Selected}}selected{{end}}>{{.Label}}</option>{{end}}
            </select>
        </label>
        <div><button type="submit" class="button-link" style="padding:12px 24px">Apply</button></div>
    </form>
</section>

<section class="section">
    <div class="section-pad"><h2 class="page-title" style="font-size:1.2rem">Reverse Event Queue</h2></div>
    <div class="table-wrap">
        <table>
            <thead><tr><th>Seq</th><th>Source</th><th>Author</th><th>Action</th><th>State</th><th>Target</th><th>Result</th><th>Issue</th><th>Attempts</th><th>Retry</th></tr></thead>
            <tbody>
                {{range .Events}}
                <tr>
                    <td class="mono">{{.ReceiveLogSeq}}</td>
                    <td class="mono"><span class="truncate" title="{{.SourceSSBMsgRef}}">{{.SourceSSBMsgRef}}</span></td>
                    <td class="mono"><span class="truncate" title="{{.SourceSSBAuthor}}">{{.SourceSSBAuthor}}</span></td>
                    <td>{{.Action}}</td>
                    <td><span class="state-pill {{.StateClass}}">{{.State}}</span></td>
                    <td class="mono"><span class="truncate" title="{{if .TargetATURI}}{{.TargetATURI}}{{else}}{{if .TargetATDID}}{{.TargetATDID}}{{else}}{{.TargetSSBFeedID}}{{end}}{{end}}">{{if .TargetATURI}}{{.TargetATURI}}{{else}}{{if .TargetATDID}}{{.TargetATDID}}{{else}}{{.TargetSSBFeedID}}{{end}}{{end}}</span></td>
                    <td class="mono"><span class="truncate" title="{{.ResultATURI}}">{{if .ResultATURI}}{{.ResultATURI}}{{else}}-{{end}}</span></td>
                    <td>{{if .Issue}}<span class="truncate" title="{{.Issue}}">{{.Issue}}</span>{{else}}-{{end}}</td>
                    <td>{{.Attempts}}</td>
                    <td>
                        {{if .Retryable}}
                        <form action="/reverse/events/retry" method="POST">
                            {{csrfField $.Chrome.CSRFToken}}
                            <input type="hidden" name="source_ssb_msg_ref" value="{{.SourceSSBMsgRef}}">
                            <button type="submit" class="button-link">Retry</button>
                        </form>
                        {{else}}-{{end}}
                    </td>
                </tr>
                {{else}}
                <tr><td colspan="10" class="empty">No reverse events matched the current filters.</td></tr>
                {{end}}
            </tbody>
        </table>
    </div>
</section>
{{end}}
`

// PageChrome controls global page shell behavior.
type PageChrome struct {
	ActiveNav   string
	Status      PageStatus
	Breadcrumbs []Breadcrumb
	CSRFToken   string
}

// Breadcrumb is one segment of a navigation breadcrumb trail.
type Breadcrumb struct {
	Label string
	Href  string
}

// PageStatus controls the optional top status strip.
type PageStatus struct {
	Visible bool
	Tone    string
	Title   string
	Body    string
}

// DashboardMetric is one linked KPI tile on the dashboard.
type DashboardMetric struct {
	ID    string
	Label string
	Value int
	Tone  string
	Href  string
	Note  string
}

// DeferredReasonView is one dashboard deferred-reason summary row.
type DeferredReasonView struct {
	Reason      string
	Count       int
	MessagesURL string
}

// IssueAccountView is one dashboard issue-heavy account summary row.
type IssueAccountView struct {
	ATDID          string
	Active         bool
	TotalMessages  int
	IssueMessages  int
	FailedMessages int
	DeferredCount  int
	DeletedCount   int
	MessagesURL    string
}

// DashboardData contains summary metrics for the dashboard page.
type DashboardData struct {
	Chrome                   PageChrome
	Metrics                  []DashboardMetric
	BridgeStatus             string
	LastHeartbeat            string
	BridgeReplayCursor       string
	RelaySourceCursor        string
	EventLogHeadCursor       string
	RuntimeHealth            string
	RuntimeHealthDescription string
	TopDeferredReasons       []DeferredReasonView
	TopIssueAccounts         []IssueAccountView
}

// AccountRow is one bridged account row in the accounts table.
type AccountRow struct {
	ATDID             string
	SSBFeedID         string
	Active            bool
	TotalMessages     int
	PublishedMessages int
	FailedMessages    int
	DeferredMessages  int
	LastPublishedAt   string
	SyncState         string
	SyncStateLabel    string
	SyncStateClass    string
	LastError         string
	CreatedAt         time.Time
	MessagesURL       string
}

// AccountsData is the template model for the accounts page.
type AccountsData struct {
	Chrome   PageChrome
	Accounts []AccountRow
}

// MessageRow is one bridged message row in the messages table.
type MessageRow struct {
	ATURI           string
	ShortATURI      string
	DetailURL       string
	ATDID           string
	ShortATDID      string
	Type            string
	TypeLabel       string
	State           string
	StateLabel      string
	StateClass      string
	SSBMsgRef       string
	ShortSSBMsgRef  string
	IssueText       string
	IssueClass      string
	IssueDetail     string
	PublishAttempts int
	DeferAttempts   int
	TotalAttempts   int
	CreatedAt       time.Time
	ThreadURL       string
}

// FilterOption is one string-valued select option in the UI.
type FilterOption struct {
	Value    string
	Label    string
	Selected bool
}

// IntFilterOption is one integer-valued select option in the UI.
type IntFilterOption struct {
	Value    int
	Label    string
	Selected bool
}

// ActiveFilter is one applied filter badge shown above the table.
type ActiveFilter struct {
	Label string
	Value string
}

// MessagesFilterState preserves current query-param state in the messages view.
type MessagesFilterState struct {
	Search   string
	ATDID    string
	Type     string
	State    string
	Sort     string
	Limit    int
	HasIssue bool
}

// MessagePagination stores next/previous links for the keyset UI.
type MessagePagination struct {
	HasPrev bool
	HasNext bool
	PrevURL string
	NextURL string
}

// MessagesData is the template model for the messages page.
type MessagesData struct {
	Chrome                PageChrome
	Messages              []MessageRow
	Filters               MessagesFilterState
	TypeOptions           []FilterOption
	StateOptions          []FilterOption
	SortOptions           []FilterOption
	LimitOptions          []IntFilterOption
	ActiveFilters         []ActiveFilter
	ResultCount           int
	Pagination            MessagePagination
	UnsupportedKeysetSort bool
}

// DetailField is one labeled value rendered in message detail sections.
type DetailField = presentation.DetailField

// FeedRow is one message row in the global feed view.
type FeedRow struct {
	ATURI     string
	ATDID     string
	Type      string
	CreatedAt time.Time
	Text      string
	HasImage  bool
	ImageRef  string
	ThreadURL string
}

// FeedData is the template model for the global feed page.
type FeedData struct {
	Chrome PageChrome
	Feed   []FeedRow
}

// MessageDetailData is the template model for a per-message detail page.
type MessageDetailData struct {
	Chrome                PageChrome
	ATURI                 string
	ATCID                 string
	ATDID                 string
	Type                  string
	State                 string
	SSBMsgRef             string
	PublishAttempts       int
	DeferAttempts         int
	CreatedAt             string
	PublishedAt           string
	LastPublishAttemptAt  string
	LastDeferAttemptAt    string
	DeletedAt             string
	DeletedSeq            string
	PublishError          string
	DeferReason           string
	DeletedReason         string
	OriginalMessageFields []DetailField
	BridgedMessageFields  []DetailField
	RawATProtoJSON        string
	RawSSBJSON            string
	RawWireFormat         string
	ShowRawWire           bool
	FilterByDIDURL        string
	FilterByStateURL      string
	FilterByTypeURL       string
	ThreadURL             string
	AssociatedBlobs       []BlobRow
}

// FailureRow is one failed/deferred row in the failures table.
type FailureRow struct {
	ATURI           string
	ATDID           string
	Type            string
	State           string
	Reason          string
	PublishAttempts int
	CreatedAt       time.Time
}

// FailureReasonGroup is one grouped failure/defer reason bucket.
type FailureReasonGroup struct {
	State  string
	Reason string
	Count  int
}

// FailuresData is the template model for the failures page.
type FailuresData struct {
	Chrome        PageChrome
	FailedRows    []FailureRow
	DeferredRows  []FailureRow
	ReasonGroups  []FailureReasonGroup
	FailedCount   int
	DeferredCount int
}

// BlobRow is one bridged blob row in the blobs table.
type BlobRow struct {
	ATCID        string
	SSBBlobRef   string
	Size         int64
	MimeType     string
	DownloadedAt time.Time
}

// BlobsData is the template model for the blobs page.
type BlobsData struct {
	Chrome PageChrome
	Blobs  []BlobRow
}

// StateRow is one key/value entry from bridge state.
type StateRow struct {
	Key       string
	Value     string
	UpdatedAt time.Time
}

// ThreadNode is a single message in a recursive thread tree.
type ThreadNode struct {
	FeedRow
	Depth        int
	Replies      []ThreadNode
	TangleName   string
	TangleRoot   string
	TipCount     int
	MessageCount int
}

// ThreadData is the template model for the conversation thread view.
type ThreadData struct {
	Chrome      PageChrome
	RootURI     string
	Nodes       []ThreadNode
	TangleStats *TangleStats
}

// TangleStats holds tangle metadata for thread display
type TangleStats struct {
	Name         string
	Root         string
	TipCount     int
	MessageCount int
	Depth        int
}

// StateData is the template model for the state page.
type StateData struct {
	Chrome            PageChrome
	RuntimeState      []StateRow
	FirehoseState     []StateRow
	OtherState        []StateRow
	DeferredCount     int
	DeletedCount      int
	LatestDeferReason string
	HeartbeatStale    bool
	HeartbeatAge      string
}

type ReverseMappingRow struct {
	SSBFeedID        string
	ATDID            string
	Active           bool
	AllowPosts       bool
	AllowReplies     bool
	AllowFollows     bool
	CredentialStatus string
	CredentialClass  string
	UpdatedAt        time.Time
}

type ReverseEventRow struct {
	SourceSSBMsgRef string
	SourceSSBAuthor string
	ATDID           string
	Action          string
	State           string
	StateClass      string
	Attempts        int
	ReceiveLogSeq   int64
	TargetSSBFeedID string
	TargetATDID     string
	TargetATURI     string
	ResultATURI     string
	Issue           string
	UpdatedAt       time.Time
	Retryable       bool
}

type ReverseFilterState struct {
	State  string
	Action string
	Search string
}

type ReverseData struct {
	Chrome        PageChrome
	Enabled       bool
	Mappings      []ReverseMappingRow
	Events        []ReverseEventRow
	Filters       ReverseFilterState
	StateOptions  []FilterOption
	ActionOptions []FilterOption
}

// PostData is the template model for the compose post page.
type PostData struct {
	Chrome         PageChrome
	Accounts       []AccountRow
	PostingEnabled bool
}

// ConnectionsData is the template model for the connections page.
type ConnectionsData struct {
	Chrome     PageChrome
	Peers      []PeerStatus
	KnownPeers []KnownPeer
	EBTState   map[string]map[string]int64
}

// PeerStatus represents the status of a single connected SSB peer.
type PeerStatus struct {
	Addr       string
	Feed       string
	ReadBytes  int64
	WriteBytes int64
	Latency    time.Duration
}

// KnownPeer represents a persisted known peer in the database.
type KnownPeer struct {
	Addr      string
	PubKey    string
	CreatedAt time.Time
}

const postContent = `
{{define "content"}}
<section class="section section-pad">
    <h1 class="page-title">Compose Post</h1>
    <p class="subtitle">Publish a record to the local PDS for end-to-end bridge testing.</p>
</section>

<section class="section section-pad" style="max-width:800px">
    {{if not .PostingEnabled}}
    <div class="alert tone-warning" style="margin-bottom:24px;padding:16px;border-radius:8px;border:1px solid var(--line)">
        <h3 style="margin-top:0;font-size:1.1rem;display:flex;align-items:center;gap:8px">
            <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M10.29 3.86L1.82 18a2 2 0 001.71 3h16.94a2 2 0 001.71-3L13.71 3.86a2 2 0 00-3.42 0zM12 9v4M12 17h.01"/></svg>
            ATProto Posting Disabled
        </h3>
        <p style="margin-bottom:0">The PDS client is not configured. To enable posting, please restart the bridge-cli with <code>--pds-host</code> and <code>--pds-password</code> flags.</p>
    </div>
    {{end}}

    <form action="/post" method="POST" enctype="multipart/form-data" style="display:grid;gap:20px">
        {{csrfField .Chrome.CSRFToken}}
        <div class="form-group">
            <label class="metric-label" style="display:block;margin-bottom:8px">Author Account</label>
            <select name="at_did" required {{if not .PostingEnabled}}disabled{{end}} style="width:100%;padding:12px;border:1px solid var(--line);border-radius:8px">
                <option value="">Select an account...</option>
                {{range .Accounts}}
                <option value="{{.ATDID}}">{{.ATDID}}</option>
                {{end}}
            </select>
        </div>

        <div class="form-group">
            <label class="metric-label" style="display:block;margin-bottom:8px">Message Text</label>
            <textarea name="text" required rows="4" {{if not .PostingEnabled}}disabled{{end}} style="width:100%;padding:12px;border:1px solid var(--line);border-radius:8px;font-family:inherit" placeholder="What's happening?"></textarea>
        </div>

        <div class="form-group">
            <label class="metric-label" style="display:block;margin-bottom:8px">Image (Optional)</label>
            <input type="file" name="image" accept="image/*" {{if not .PostingEnabled}}disabled{{end}} style="width:100%;padding:8px">
        </div>

        <div style="margin-top:10px">
            <button type="submit" class="button-link tone-success" {{if not .PostingEnabled}}disabled style="opacity:0.5;cursor:not-allowed"{{else}}style="padding:12px 24px;font-size:1rem;font-weight:700;cursor:pointer"{{end}}>Publish to ATProto</button>
            <a href="/" class="button-link" style="margin-left:12px">Cancel</a>
        </div>
    </form>
</section>
{{end}}
`

// RenderDashboard renders the dashboard page.
func RenderDashboard(w io.Writer, data DashboardData) error {
	return dashboardTemplate.Execute(w, data)
}

// RenderAccounts renders the accounts page.
func RenderAccounts(w io.Writer, data AccountsData) error {
	return accountsTemplate.Execute(w, data)
}

// RenderMessages renders the messages page.
func RenderMessages(w io.Writer, data MessagesData) error {
	return messagesTemplate.Execute(w, data)
}

// RenderMessageDetail renders the message detail page.
func RenderMessageDetail(w io.Writer, data MessageDetailData) error {
	return messageDetailTemplate.Execute(w, data)
}

// RenderFeed renders the global feed page.
func RenderFeed(w io.Writer, data FeedData) error {
	return feedTemplate.Execute(w, data)
}

// RenderFailures renders the failures page.
func RenderFailures(w io.Writer, data FailuresData) error {
	return failuresTemplate.Execute(w, data)
}

// RenderBlobs renders the blobs page.
func RenderBlobs(w io.Writer, data BlobsData) error {
	return blobsTemplate.Execute(w, data)
}

// RenderState renders the bridge-state page.
func RenderState(w io.Writer, data StateData) error {
	return stateTemplate.Execute(w, data)
}

// RenderReverse renders the reverse-sync admin page.
func RenderReverse(w io.Writer, data ReverseData) error {
	return reverseTemplate.Execute(w, data)
}

// RenderPost renders the compose post page.
func RenderPost(w io.Writer, data PostData) error {
	return postTemplate.Execute(w, data)
}

// RenderConnections renders the connections page.
func RenderConnections(w io.Writer, data ConnectionsData) error {
	return connectionsTemplate.Execute(w, data)
}

// RenderThread renders the conversation thread page.
func RenderThread(w io.Writer, data ThreadData) error {
	return threadTemplate.Execute(w, data)
}

var (
	dashboardTemplate     = mustPageTemplate("dashboard", dashboardContent)
	accountsTemplate      = mustPageTemplate("accounts", accountsContent)
	messagesTemplate      = mustPageTemplate("messages", messagesContent)
	messageDetailTemplate = mustPageTemplate("message-detail", messageDetailContent)
	failuresTemplate      = mustPageTemplate("failures", failuresContent)
	feedTemplate          = mustPageTemplate("feed", feedContent)
	blobsTemplate         = mustPageTemplate("blobs", blobsContent)
	stateTemplate         = mustPageTemplate("state", stateContent)
	reverseTemplate       = mustPageTemplate("reverse", reverseContent)
	postTemplate          = mustPageTemplate("post", postContent)
	connectionsTemplate   = mustPageTemplate("connections", connectionsContent)
	threadTemplate        = mustPageTemplate("thread", threadContent)
)

func mustPageTemplate(name, content string) *template.Template {
	return template.Must(template.New(name).Funcs(template.FuncMap{
		"fmtTime": func(t time.Time) string {
			if t.IsZero() {
				return "-"
			}
			return t.Format("2006-01-02 15:04:05")
		},
		"hasPrefix": strings.HasPrefix,
		"multiply": func(a, b int) int {
			return a * b
		},
		"humanizeBytes": func(b int64) string {
			const unit = 1024
			if b < unit {
				return fmt.Sprintf("%d B", b)
			}
			div, exp := int64(unit), 0
			for n := b / unit; n >= unit; n /= unit {
				div *= unit
				exp++
			}
			return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
		},
		"navClass": func(activeNav, tab string) string {
			if activeNav == tab {
				return "nav-link is-active"
			}
			return "nav-link"
		},
		"navCurrent": func(activeNav, tab string) template.HTMLAttr {
			if activeNav == tab {
				return template.HTMLAttr(`aria-current="page"`)
			}
			return template.HTMLAttr("")
		},
		"csrfField": func(token string) template.HTML {
			token = strings.TrimSpace(token)
			if token == "" {
				return ""
			}
			escaped := template.HTMLEscapeString(token)
			return template.HTML(fmt.Sprintf(`<input type="hidden" name="csrf_token" value="%s">`, escaped))
		},
		"statusToneClass": func(tone string) string {
			switch tone {
			case "success":
				return "tone-success"
			case "warning":
				return "tone-warning"
			case "danger":
				return "tone-danger"
			default:
				return "tone-neutral"
			}
		},
		"stateClass": func(state string) string {
			switch state {
			case "published":
				return "state-published"
			case "failed":
				return "state-failed"
			case "deferred":
				return "state-deferred"
			case "deleted":
				return "state-deleted"
			default:
				return "state-pending"
			}
		},
		"formatSSBText": func(text string) template.HTML {
			if text == "" {
				return ""
			}
			// Preserve newlines
			text = template.HTMLEscapeString(text)
			text = strings.ReplaceAll(text, "\n", "<br>")

			// Simple URL detection
			urlRegex := regexp.MustCompile(`https?://[^\s<>"{}|\\^` + "`" + `\[\]]+`)
			text = urlRegex.ReplaceAllStringFunc(text, func(u string) string {
				return fmt.Sprintf("<a href=\"%s\" target=\"_blank\" rel=\"noopener\">%s</a>", u, u)
			})
			return template.HTML(text)
		},
	}).Parse(pageLayout + content))
}
