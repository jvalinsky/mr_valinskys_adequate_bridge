// Package templates renders HTML views for the bridge admin UI.
package templates

import (
	"html/template"
	"io"
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
    <style>
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

        * {
            box-sizing: border-box;
        }

        body {
            margin: 0;
            min-height: 100vh;
            font-family: "Avenir Next", "Segoe UI", sans-serif;
            background: var(--bg);
            color: var(--ink);
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
            background: #f8fffd;
            color: var(--brand-strong);
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

        .status-strip.tone-success { border-left: 6px solid var(--ok); }
        .status-strip.tone-warning { border-left: 6px solid var(--warn); }
        .status-strip.tone-danger { border-left: 6px solid var(--danger); }
        .status-strip.tone-neutral { border-left: 6px solid var(--brand); }

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
            background: #fff;
            padding: 14px;
            display: grid;
            gap: 5px;
            transition: transform 120ms ease, box-shadow 120ms ease;
        }

        .metric-card:hover,
        .metric-card:focus-visible {
            transform: translateY(-1px);
            box-shadow: var(--shadow);
            outline: none;
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
            grid-template-columns: repeat(2, minmax(0, 1fr));
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
            background: #fff;
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

        .pill.state-published { background: #e6f6eb; color: #1a6b35; }
        .pill.state-failed { background: #fde9e9; color: #b33030; }
        .pill.state-deferred { background: #fff2dd; color: #7a5f00; }
        .pill.state-deleted { background: #eef0f2; color: #3f4b57; }
        .pill.state-pending { background: #eceff2; color: #47505b; }

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
            background: #f8f6f1;
        }

        tbody tr:nth-child(even) {
            background: #f9f7f2;
        }

        tbody tr:hover {
            background: rgba(26, 107, 90, 0.04);
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
            background: #fff;
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
        .copy-btn:focus-visible,
        .button:focus-visible,
        .button-link:focus-visible {
            outline: none;
            border-color: var(--brand);
            color: var(--brand-strong);
        }

        .toolbar {
            display: flex;
            gap: 10px;
            flex-wrap: wrap;
            align-items: center;
            justify-content: space-between;
            margin-top: 10px;
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
            background: #fff;
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
            background: #fff;
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
            background: #fff;
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
                <a href="/failures" class="{{navClass .Chrome.ActiveNav "failures"}}" {{navCurrent .Chrome.ActiveNav "failures"}}>Failures</a>
                <a href="/blobs" class="{{navClass .Chrome.ActiveNav "blobs"}}" {{navCurrent .Chrome.ActiveNav "blobs"}}>Blobs</a>
                <a href="/state" class="{{navClass .Chrome.ActiveNav "state"}}" {{navCurrent .Chrome.ActiveNav "state"}}>State</a>
            </nav>
        </div>
    </header>

    <main id="main-content" class="app-main">
        {{if .Chrome.Status.Visible}}
        <section class="status-strip {{statusToneClass .Chrome.Status.Tone}}" role="status" aria-live="polite">
            <h2>{{.Chrome.Status.Title}}</h2>
            <p>{{.Chrome.Status.Body}}</p>
        </section>
        {{end}}
        {{template "content" .}}
    </main>

    <script>
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
            <span class="metric-value">{{.Value}}</span>
            {{if .Note}}<span class="metric-note">{{.Note}}</span>{{end}}
        </a>
        {{end}}
    </div>
</section>

<section class="grid-two">
    <article class="section section-pad">
        <h2 class="page-title" style="font-size:1.2rem">Runtime Health</h2>
        <p class="subtitle"><strong>{{.RuntimeHealth}}</strong> · {{.RuntimeHealthDescription}}</p>
        <dl class="details-grid" style="margin-top:10px">
            <div class="detail-card"><dt>Bridge Status</dt><dd>{{.BridgeStatus}}</dd></div>
            <div class="detail-card"><dt>Last Heartbeat</dt><dd>{{if .LastHeartbeat}}{{.LastHeartbeat}}{{else}}(not set){{end}}</dd></div>
            <div class="detail-card"><dt>Firehose Cursor</dt><dd>{{if .FirehoseCursor}}{{.FirehoseCursor}}{{else}}(not set){{end}}</dd></div>
        </dl>
    </article>

    <article class="section section-pad">
        <h2 class="page-title" style="font-size:1.2rem">Top Deferred Reasons</h2>
        {{if .TopDeferredReasons}}
        <ul class="mini-list" style="margin-top:10px">
            {{range .TopDeferredReasons}}
            <li>
                <div class="mono truncate" title="{{.Reason}}">{{.Reason}}</div>
                <a class="button-link" href="{{.MessagesURL}}">{{.Count}} msgs</a>
            </li>
            {{end}}
        </ul>
        {{else}}
        <div class="empty">No deferred reasons recorded.</div>
        {{end}}
    </article>
</section>

<section class="section section-pad">
    <h2 class="page-title" style="font-size:1.2rem">Accounts With Highest Issue Volume</h2>
    {{if .TopIssueAccounts}}
    <div class="table-wrap" style="margin-top:10px">
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
            <tbody>
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
    {{else}}
    <div class="empty">No issue-heavy accounts yet.</div>
    {{end}}
</section>
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
                    <th>SSB Feed ID</th>
                    <th>Status</th>
                    <th>Total</th>
                    <th>Published</th>
                    <th>Failed</th>
                    <th>Deferred</th>
                    <th>Last Published</th>
                    <th>Created</th>
                    <th>Pivot</th>
                </tr>
            </thead>
            <tbody>
                {{range .Accounts}}
                <tr>
                    <td class="mono"><span class="truncate" title="{{.ATDID}}">{{.ATDID}}</span></td>
                    <td class="mono"><span class="truncate" title="{{.SSBFeedID}}">{{.SSBFeedID}}</span></td>
                    <td>{{if .Active}}active{{else}}inactive{{end}}</td>
                    <td>{{.TotalMessages}}</td>
                    <td>{{.PublishedMessages}}</td>
                    <td>{{.FailedMessages}}</td>
                    <td>{{.DeferredMessages}}</td>
                    <td>{{if .LastPublishedAt}}{{.LastPublishedAt}}{{else}}(none){{end}}</td>
                    <td>{{fmtTime .CreatedAt}}</td>
                    <td><a class="button-link" href="{{.MessagesURL}}">Messages</a></td>
                </tr>
                {{else}}
                <tr><td colspan="10" class="empty">No bridged accounts yet.</td></tr>
                {{end}}
            </tbody>
        </table>
    </div>
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
        <div class="detail-card"><dt>Published</dt><dd>{{if .PublishedAt}}{{.PublishedAt}}{{else}}(not published){{end}}</dd></div>
        <div class="detail-card"><dt>Last Publish Attempt</dt><dd>{{if .LastPublishAttemptAt}}{{.LastPublishAttemptAt}}{{else}}(none){{end}}</dd></div>
        <div class="detail-card"><dt>Last Defer Attempt</dt><dd>{{if .LastDeferAttemptAt}}{{.LastDeferAttemptAt}}{{else}}(none){{end}}</dd></div>
        <div class="detail-card"><dt>Deleted At</dt><dd>{{if .DeletedAt}}{{.DeletedAt}}{{else}}(not deleted){{end}}</dd></div>
        <div class="detail-card"><dt>Deleted Seq</dt><dd>{{if .DeletedSeq}}{{.DeletedSeq}}{{else}}(none){{end}}</dd></div>
    </div>
</section>

<section class="grid-two">
    <article class="section section-pad">
        <h2 class="page-title" style="font-size:1.2rem">Record Metadata</h2>
        <dl class="details-grid" style="margin-top:10px">
            <div class="detail-card"><dt>Author DID</dt><dd class="mono">{{.ATDID}}</dd></div>
            <div class="detail-card"><dt>Record Type</dt><dd>{{.Type}}</dd></div>
            <div class="detail-card"><dt>AT CID</dt><dd class="mono">{{if .ATCID}}{{.ATCID}}{{else}}(none){{end}}</dd></div>
            <div class="detail-card"><dt>SSB Ref</dt><dd class="mono">{{if .SSBMsgRef}}{{.SSBMsgRef}}{{else}}pending{{end}}</dd></div>
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
    <article class="section section-pad">
        <h2 class="page-title" style="font-size:1.2rem">Raw ATProto JSON</h2>
        <pre>{{.RawATProtoJSON}}</pre>
    </article>

    <article class="section section-pad">
        <h2 class="page-title" style="font-size:1.2rem">Raw SSB JSON</h2>
        <pre>{{.RawSSBJSON}}</pre>
    </article>
</section>
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
                <tr><th>AT CID</th><th>SSB Blob Ref</th><th>Size</th><th>MIME</th><th>Downloaded</th></tr>
            </thead>
            <tbody>
                {{range .Blobs}}
                <tr>
                    <td class="mono"><span class="truncate" title="{{.ATCID}}">{{.ATCID}}</span></td>
                    <td class="mono"><span class="truncate" title="{{.SSBBlobRef}}">{{.SSBBlobRef}}</span></td>
                    <td>{{.Size}}</td>
                    <td>{{.MimeType}}</td>
                    <td>{{fmtTime .DownloadedAt}}</td>
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
        <div class="section-pad"><h2 class="page-title" style="font-size:1.2rem">Firehose Keys</h2></div>
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

// PageChrome controls global page shell behavior.
type PageChrome struct {
	ActiveNav string
	Status    PageStatus
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
	FirehoseCursor           string
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
	FilterByDIDURL        string
	FilterByStateURL      string
	FilterByTypeURL       string
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

var (
	dashboardTemplate     = mustPageTemplate("dashboard", dashboardContent)
	accountsTemplate      = mustPageTemplate("accounts", accountsContent)
	messagesTemplate      = mustPageTemplate("messages", messagesContent)
	messageDetailTemplate = mustPageTemplate("message-detail", messageDetailContent)
	failuresTemplate      = mustPageTemplate("failures", failuresContent)
	blobsTemplate         = mustPageTemplate("blobs", blobsContent)
	stateTemplate         = mustPageTemplate("state", stateContent)
)

func mustPageTemplate(name, content string) *template.Template {
	return template.Must(template.New(name).Funcs(template.FuncMap{
		"fmtTime": func(t time.Time) string {
			if t.IsZero() {
				return ""
			}
			return t.Format(time.RFC3339)
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
	}).Parse(pageLayout + content))
}
