// Package templates renders HTML views for the bridge admin UI.
package templates

import (
	"html/template"
	"io"
	"time"
)

const pageLayout = `
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>ATProto ↔ SSB Bridge Admin</title>
    <script src="https://cdn.tailwindcss.com"></script>
    <script src="https://unpkg.com/htmx.org@1.9.10"></script>
    <style>
        :root {
            color-scheme: light;
        }

        .mono-data {
            font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", "Courier New", monospace;
        }

        .clamp-2 {
            display: -webkit-box;
            -webkit-box-orient: vertical;
            -webkit-line-clamp: 2;
            overflow: hidden;
        }
    </style>
</head>
<body class="bg-gray-100 min-h-screen">
    <nav class="bg-indigo-700 text-white shadow-lg">
        <div class="max-w-screen-2xl mx-auto px-4">
            <div class="flex items-center justify-between h-16">
                <div class="flex items-center">
                    <span class="font-bold text-xl">Bridge Admin</span>
                    <div class="ml-10 flex items-baseline space-x-4">
                        <a href="/" class="hover:bg-indigo-600 px-3 py-2 rounded-md text-sm font-medium">Dashboard</a>
                        <a href="/accounts" class="hover:bg-indigo-600 px-3 py-2 rounded-md text-sm font-medium">Accounts</a>
                        <a href="/messages" class="hover:bg-indigo-600 px-3 py-2 rounded-md text-sm font-medium">Messages</a>
                        <a href="/failures" class="hover:bg-indigo-600 px-3 py-2 rounded-md text-sm font-medium">Failures</a>
                        <a href="/blobs" class="hover:bg-indigo-600 px-3 py-2 rounded-md text-sm font-medium">Blobs</a>
                        <a href="/state" class="hover:bg-indigo-600 px-3 py-2 rounded-md text-sm font-medium">Cursor</a>
                    </div>
                </div>
            </div>
        </div>
    </nav>

    <main class="max-w-screen-2xl mx-auto py-6 sm:px-6 lg:px-8">
        {{template "content" .}}
    </main>
</body>
</html>
`

const dashboardContent = `
{{define "content"}}
<div class="px-4 py-6 sm:px-0">
    <h1 class="text-3xl font-bold text-gray-900 mb-6">Dashboard</h1>

    <div class="grid grid-cols-1 md:grid-cols-3 gap-6">
        <div class="bg-white overflow-hidden shadow rounded-lg">
            <div class="p-5">
                <div class="text-sm text-gray-500">Bridged Accounts</div>
                <div class="text-3xl font-semibold text-gray-900">{{.AccountCount}}</div>
            </div>
        </div>
        <div class="bg-white overflow-hidden shadow rounded-lg">
            <div class="p-5">
                <div class="text-sm text-gray-500">Messages Bridged</div>
                <div class="text-3xl font-semibold text-gray-900">{{.MessageCount}}</div>
            </div>
        </div>
        <div class="bg-white overflow-hidden shadow rounded-lg">
            <div class="p-5">
                <div class="text-sm text-gray-500">Messages Published</div>
                <div class="text-3xl font-semibold text-gray-900">{{.PublishedCount}}</div>
            </div>
        </div>
        <div class="bg-white overflow-hidden shadow rounded-lg">
            <div class="p-5">
                <div class="text-sm text-gray-500">Publish Failures</div>
                <div class="text-3xl font-semibold text-red-700">{{.PublishFailureCount}}</div>
            </div>
        </div>
        <div class="bg-white overflow-hidden shadow rounded-lg">
            <div class="p-5">
                <div class="text-sm text-gray-500">Messages Deferred</div>
                <div class="text-3xl font-semibold text-amber-700">{{.DeferredCount}}</div>
            </div>
        </div>
        <div class="bg-white overflow-hidden shadow rounded-lg">
            <div class="p-5">
                <div class="text-sm text-gray-500">Messages Deleted</div>
                <div class="text-3xl font-semibold text-gray-900">{{.DeletedCount}}</div>
            </div>
        </div>
        <div class="bg-white overflow-hidden shadow rounded-lg">
            <div class="p-5">
                <div class="text-sm text-gray-500">Blobs Bridged</div>
                <div class="text-3xl font-semibold text-gray-900">{{.BlobCount}}</div>
            </div>
        </div>
        <div class="bg-white overflow-hidden shadow rounded-lg">
            <div class="p-5">
                <div class="text-sm text-gray-500">Bridge Status</div>
                <div class="text-xl font-semibold text-gray-900">{{.BridgeStatus}}</div>
            </div>
        </div>
        <div class="bg-white overflow-hidden shadow rounded-lg">
            <div class="p-5">
                <div class="text-sm text-gray-500">Last Heartbeat</div>
                <div class="text-lg font-semibold text-gray-900">{{if .LastHeartbeat}}{{.LastHeartbeat}}{{else}}(not set){{end}}</div>
            </div>
        </div>
    </div>

    <div class="mt-6 bg-white overflow-hidden shadow rounded-lg">
        <div class="p-5">
            <div class="text-sm text-gray-500">Firehose Cursor</div>
            <div class="text-lg font-semibold text-gray-900">{{if .FirehoseCursor}}{{.FirehoseCursor}}{{else}}(not set){{end}}</div>
        </div>
    </div>
</div>
{{end}}
`

const accountsContent = `
{{define "content"}}
<div class="px-4 py-6 sm:px-0">
    <h1 class="text-3xl font-bold text-gray-900 mb-6">Accounts</h1>

    <div class="bg-white shadow rounded-lg overflow-hidden">
        <table class="min-w-full divide-y divide-gray-200">
            <thead class="bg-gray-50">
                <tr>
                    <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">AT DID</th>
                    <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">SSB Feed ID</th>
                    <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Status</th>
                    <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Created</th>
                </tr>
            </thead>
            <tbody class="bg-white divide-y divide-gray-200">
                {{range .Accounts}}
                <tr>
                    <td class="px-6 py-4 text-sm text-gray-900">{{.ATDID}}</td>
                    <td class="px-6 py-4 text-sm text-gray-700">{{.SSBFeedID}}</td>
                    <td class="px-6 py-4 text-sm">{{if .Active}}<span class="text-green-700">active</span>{{else}}<span class="text-gray-500">inactive</span>{{end}}</td>
                    <td class="px-6 py-4 text-sm text-gray-500">{{fmtTime .CreatedAt}}</td>
                </tr>
                {{else}}
                <tr>
                    <td colspan="4" class="px-6 py-8 text-sm text-gray-500 text-center">No bridged accounts yet.</td>
                </tr>
                {{end}}
            </tbody>
        </table>
    </div>
</div>
{{end}}
`

const messagesContent = `
{{define "content"}}
<div class="px-4 py-6 sm:px-0 space-y-6">
    <div>
        <h1 class="text-3xl font-bold text-gray-900">Messages</h1>
        <p class="mt-2 text-sm text-gray-600">Filter by record type or lifecycle state, search across IDs and issue text, then open a message for the full ATProto and SSB payloads.</p>
    </div>

    <section class="bg-white shadow rounded-lg">
        <form method="GET" action="/messages" class="p-5 space-y-4">
            <div class="flex flex-col gap-4 xl:flex-row xl:items-start xl:justify-between">
                <div>
                    <h2 class="text-sm font-semibold text-gray-900">Browse Message Stream</h2>
                    <p class="mt-1 text-sm text-gray-500">Search AT URIs, DIDs, SSB refs, publish failures, and defer reasons.</p>
                </div>
                <div class="flex items-center gap-3">
                    <a href="/messages" class="text-sm font-medium text-gray-600 hover:text-gray-900">Reset</a>
                    <button type="submit" class="inline-flex items-center rounded-md bg-indigo-700 px-4 py-2 text-sm font-medium text-white hover:bg-indigo-800">Apply</button>
                </div>
            </div>

            <div class="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-6 gap-4">
                <div class="xl:col-span-2">
                    <label for="messages-search" class="block text-xs font-semibold uppercase tracking-wide text-gray-500">Search</label>
                    <input id="messages-search" type="search" name="q" value="{{.Filters.Search}}" placeholder="AT URI, DID, SSB ref, error, defer reason" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm text-gray-900 shadow-sm focus:border-indigo-500 focus:outline-none focus:ring-1 focus:ring-indigo-500">
                </div>
                <div>
                    <label for="messages-type" class="block text-xs font-semibold uppercase tracking-wide text-gray-500">Type</label>
                    <select id="messages-type" name="type" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm text-gray-900 shadow-sm focus:border-indigo-500 focus:outline-none focus:ring-1 focus:ring-indigo-500">
                        {{range .TypeOptions}}
                        <option value="{{.Value}}" {{if .Selected}}selected{{end}}>{{.Label}}</option>
                        {{end}}
                    </select>
                </div>
                <div>
                    <label for="messages-state" class="block text-xs font-semibold uppercase tracking-wide text-gray-500">State</label>
                    <select id="messages-state" name="state" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm text-gray-900 shadow-sm focus:border-indigo-500 focus:outline-none focus:ring-1 focus:ring-indigo-500">
                        {{range .StateOptions}}
                        <option value="{{.Value}}" {{if .Selected}}selected{{end}}>{{.Label}}</option>
                        {{end}}
                    </select>
                </div>
                <div>
                    <label for="messages-sort" class="block text-xs font-semibold uppercase tracking-wide text-gray-500">Sort</label>
                    <select id="messages-sort" name="sort" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm text-gray-900 shadow-sm focus:border-indigo-500 focus:outline-none focus:ring-1 focus:ring-indigo-500">
                        {{range .SortOptions}}
                        <option value="{{.Value}}" {{if .Selected}}selected{{end}}>{{.Label}}</option>
                        {{end}}
                    </select>
                </div>
                <div>
                    <label for="messages-limit" class="block text-xs font-semibold uppercase tracking-wide text-gray-500">Page Size</label>
                    <select id="messages-limit" name="limit" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm text-gray-900 shadow-sm focus:border-indigo-500 focus:outline-none focus:ring-1 focus:ring-indigo-500">
                        {{range .LimitOptions}}
                        <option value="{{.Value}}" {{if .Selected}}selected{{end}}>{{.Label}}</option>
                        {{end}}
                    </select>
                </div>
            </div>

            <div class="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
                <div class="text-sm text-gray-600">
                    Showing <span class="font-semibold text-gray-900">{{.ResultCount}}</span> matching messages{{if .ReachedLimit}} <span class="text-gray-500">(limit {{.Filters.Limit}})</span>{{end}}
                </div>
                {{if .ActiveFilters}}
                <div class="flex flex-wrap gap-2">
                    {{range .ActiveFilters}}
                    <span class="inline-flex items-center gap-2 rounded-full bg-gray-100 px-3 py-1 text-xs text-gray-700">
                        <span class="font-semibold uppercase tracking-wide text-gray-500">{{.Label}}</span>
                        <span class="font-medium text-gray-900">{{.Value}}</span>
                    </span>
                    {{end}}
                </div>
                {{end}}
            </div>
        </form>
    </section>

    <div class="bg-white shadow rounded-lg overflow-hidden">
        <div class="overflow-x-auto">
            <table class="min-w-[96rem] table-fixed divide-y divide-gray-200">
                <thead class="bg-gray-50">
                    <tr>
                        <th class="w-80 px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">AT URI</th>
                        <th class="w-56 px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Author DID</th>
                        <th class="w-44 px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Type</th>
                        <th class="w-28 px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">State</th>
                        <th class="w-52 px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">SSB Ref</th>
                        <th class="w-24 px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Retries</th>
                        <th class="w-[28rem] px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Issue</th>
                        <th class="w-40 px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Created</th>
                    </tr>
                </thead>
                <tbody class="bg-white divide-y divide-gray-200">
                    {{range .Messages}}
                    <tr class="align-top hover:bg-gray-50">
                        <td class="px-6 py-4 text-sm text-gray-900">
                            <a href="{{.DetailURL}}" class="block" title="{{.ATURI}}">
                                <span class="block truncate mono-data text-xs font-medium text-indigo-700 hover:text-indigo-900 hover:underline">{{.ATURI}}</span>
                                <span class="mt-1 block text-xs text-gray-500">Open detail view</span>
                            </a>
                        </td>
                        <td class="px-6 py-4 text-sm text-gray-700">
                            <div class="truncate mono-data text-xs text-gray-700" title="{{.ATDID}}">{{.ATDID}}</div>
                        </td>
                        <td class="px-6 py-4 text-sm text-gray-700">
                            <div class="font-medium text-gray-900">{{.TypeLabel}}</div>
                            <div class="truncate text-xs text-gray-500" title="{{.Type}}">{{.Type}}</div>
                        </td>
                        <td class="px-6 py-4 text-sm">
                            <span class="inline-flex rounded-full px-2.5 py-1 text-xs font-semibold {{.StateClass}}">{{.StateLabel}}</span>
                        </td>
                        <td class="px-6 py-4 text-sm text-gray-700">
                            {{if .SSBMsgRef}}
                            <div class="truncate mono-data text-xs text-gray-700" title="{{.SSBMsgRef}}">{{.SSBMsgRef}}</div>
                            {{else}}
                            <span class="text-gray-500">pending</span>
                            {{end}}
                        </td>
                        <td class="px-6 py-4 text-sm text-gray-700">
                            <div class="font-medium text-gray-900">{{.TotalAttempts}}</div>
                            <div class="text-xs text-gray-500">P{{.PublishAttempts}} / D{{.DeferAttempts}}</div>
                        </td>
                        <td class="px-6 py-4 text-sm {{.IssueClass}}">
                            <div class="clamp-2 leading-6">{{.IssueText}}</div>
                        </td>
                        <td class="px-6 py-4 text-sm text-gray-500">{{fmtTime .CreatedAt}}</td>
                    </tr>
                    {{else}}
                    <tr>
                        <td colspan="8" class="px-6 py-8 text-sm text-gray-500 text-center">No bridged messages matched the current filters.</td>
                    </tr>
                    {{end}}
                </tbody>
            </table>
        </div>
    </div>
</div>
{{end}}
`

const messageDetailContent = `
{{define "content"}}
<div class="px-4 py-6 sm:px-0 space-y-6">
    <div class="flex items-start justify-between gap-4">
        <div>
            <div class="text-sm font-medium text-indigo-700"><a href="/messages" class="hover:underline">Back to Messages</a></div>
            <h1 class="text-3xl font-bold text-gray-900 mt-2">Message Detail</h1>
            <p class="mt-3 text-sm text-gray-600 break-all">{{.ATURI}}</p>
        </div>
        <div class="text-right text-sm text-gray-500">
            <div>State</div>
            <div class="text-lg font-semibold text-gray-900">{{.State}}</div>
        </div>
    </div>

    <div class="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-4 gap-4">
        <div class="bg-white overflow-hidden shadow rounded-lg"><div class="p-4"><div class="text-sm text-gray-500">Author DID</div><div class="mt-1 text-sm font-semibold text-gray-900 break-all">{{.ATDID}}</div></div></div>
        <div class="bg-white overflow-hidden shadow rounded-lg"><div class="p-4"><div class="text-sm text-gray-500">Record Type</div><div class="mt-1 text-sm font-semibold text-gray-900 break-all">{{.Type}}</div></div></div>
        <div class="bg-white overflow-hidden shadow rounded-lg"><div class="p-4"><div class="text-sm text-gray-500">AT CID</div><div class="mt-1 text-sm font-semibold text-gray-900 break-all">{{if .ATCID}}{{.ATCID}}{{else}}(none){{end}}</div></div></div>
        <div class="bg-white overflow-hidden shadow rounded-lg"><div class="p-4"><div class="text-sm text-gray-500">SSB Ref</div><div class="mt-1 text-sm font-semibold text-gray-900 break-all">{{if .SSBMsgRef}}{{.SSBMsgRef}}{{else}}pending{{end}}</div></div></div>
        <div class="bg-white overflow-hidden shadow rounded-lg"><div class="p-4"><div class="text-sm text-gray-500">Created</div><div class="mt-1 text-sm font-semibold text-gray-900">{{.CreatedAt}}</div></div></div>
        <div class="bg-white overflow-hidden shadow rounded-lg"><div class="p-4"><div class="text-sm text-gray-500">Published</div><div class="mt-1 text-sm font-semibold text-gray-900">{{if .PublishedAt}}{{.PublishedAt}}{{else}}(not published){{end}}</div></div></div>
        <div class="bg-white overflow-hidden shadow rounded-lg"><div class="p-4"><div class="text-sm text-gray-500">Publish Attempts</div><div class="mt-1 text-sm font-semibold text-gray-900">{{.PublishAttempts}}</div></div></div>
        <div class="bg-white overflow-hidden shadow rounded-lg"><div class="p-4"><div class="text-sm text-gray-500">Defer Attempts</div><div class="mt-1 text-sm font-semibold text-gray-900">{{.DeferAttempts}}</div></div></div>
    </div>

    <div class="grid grid-cols-1 lg:grid-cols-2 gap-6">
        <section class="bg-white shadow rounded-lg overflow-hidden">
            <div class="border-b border-gray-200 px-5 py-4">
                <h2 class="text-lg font-semibold text-gray-900">Original ATProto Message</h2>
            </div>
            <div class="p-5 space-y-3">
                {{range .OriginalMessageFields}}
                <div>
                    <div class="text-xs font-semibold uppercase tracking-wide text-gray-500">{{.Label}}</div>
                    <div class="mt-1 text-sm text-gray-900 break-words whitespace-pre-wrap">{{.Value}}</div>
                </div>
                {{else}}
                <div class="text-sm text-gray-500">No structured ATProto fields available.</div>
                {{end}}
            </div>
        </section>

        <section class="bg-white shadow rounded-lg overflow-hidden">
            <div class="border-b border-gray-200 px-5 py-4">
                <h2 class="text-lg font-semibold text-gray-900">Bridged SSB Message</h2>
            </div>
            <div class="p-5 space-y-3">
                {{range .BridgedMessageFields}}
                <div>
                    <div class="text-xs font-semibold uppercase tracking-wide text-gray-500">{{.Label}}</div>
                    <div class="mt-1 text-sm text-gray-900 break-words whitespace-pre-wrap">{{.Value}}</div>
                </div>
                {{else}}
                <div class="text-sm text-gray-500">No structured SSB fields available.</div>
                {{end}}
            </div>
        </section>
    </div>

    <div class="grid grid-cols-1 lg:grid-cols-2 gap-6">
        <section class="bg-white shadow rounded-lg overflow-hidden">
            <div class="border-b border-gray-200 px-5 py-4">
                <h2 class="text-lg font-semibold text-gray-900">Raw ATProto JSON</h2>
            </div>
            <pre class="p-5 overflow-x-auto text-xs leading-6 text-gray-900 bg-gray-50 whitespace-pre-wrap break-all">{{.RawATProtoJSON}}</pre>
        </section>

        <section class="bg-white shadow rounded-lg overflow-hidden">
            <div class="border-b border-gray-200 px-5 py-4">
                <h2 class="text-lg font-semibold text-gray-900">Raw SSB JSON</h2>
            </div>
            <pre class="p-5 overflow-x-auto text-xs leading-6 text-gray-900 bg-gray-50 whitespace-pre-wrap break-all">{{.RawSSBJSON}}</pre>
        </section>
    </div>

    {{if or .PublishError .DeferReason .DeletedReason .LastPublishAttemptAt .LastDeferAttemptAt .DeletedAt .DeletedSeq}}
    <section class="bg-white shadow rounded-lg overflow-hidden">
        <div class="border-b border-gray-200 px-5 py-4">
            <h2 class="text-lg font-semibold text-gray-900">Lifecycle Details</h2>
        </div>
        <div class="p-5 grid grid-cols-1 md:grid-cols-2 gap-4">
            {{if .LastPublishAttemptAt}}<div><div class="text-xs font-semibold uppercase tracking-wide text-gray-500">Last Publish Attempt</div><div class="mt-1 text-sm text-gray-900">{{.LastPublishAttemptAt}}</div></div>{{end}}
            {{if .LastDeferAttemptAt}}<div><div class="text-xs font-semibold uppercase tracking-wide text-gray-500">Last Defer Attempt</div><div class="mt-1 text-sm text-gray-900">{{.LastDeferAttemptAt}}</div></div>{{end}}
            {{if .DeletedAt}}<div><div class="text-xs font-semibold uppercase tracking-wide text-gray-500">Deleted At</div><div class="mt-1 text-sm text-gray-900">{{.DeletedAt}}</div></div>{{end}}
            {{if .DeletedSeq}}<div><div class="text-xs font-semibold uppercase tracking-wide text-gray-500">Deleted Sequence</div><div class="mt-1 text-sm text-gray-900">{{.DeletedSeq}}</div></div>{{end}}
            {{if .PublishError}}<div class="md:col-span-2"><div class="text-xs font-semibold uppercase tracking-wide text-gray-500">Publish Error</div><div class="mt-1 text-sm text-red-700 break-words whitespace-pre-wrap">{{.PublishError}}</div></div>{{end}}
            {{if .DeferReason}}<div class="md:col-span-2"><div class="text-xs font-semibold uppercase tracking-wide text-gray-500">Defer Reason</div><div class="mt-1 text-sm text-amber-700 break-words whitespace-pre-wrap">{{.DeferReason}}</div></div>{{end}}
            {{if .DeletedReason}}<div class="md:col-span-2"><div class="text-xs font-semibold uppercase tracking-wide text-gray-500">Deleted Reason</div><div class="mt-1 text-sm text-gray-900 break-words whitespace-pre-wrap">{{.DeletedReason}}</div></div>{{end}}
        </div>
    </section>
    {{end}}
</div>
{{end}}
`

const failuresContent = `
{{define "content"}}
<div class="px-4 py-6 sm:px-0">
    <h1 class="text-3xl font-bold text-gray-900 mb-6">Publish/Defer Issues</h1>

    <div class="bg-white shadow rounded-lg overflow-hidden">
        <table class="min-w-full divide-y divide-gray-200">
            <thead class="bg-gray-50">
                <tr>
                    <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">AT URI</th>
                    <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Author DID</th>
                    <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Type</th>
                    <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">State</th>
                    <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Attempts</th>
                    <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Reason</th>
                    <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Created</th>
                </tr>
            </thead>
            <tbody class="bg-white divide-y divide-gray-200">
                {{range .Failures}}
                <tr>
                    <td class="px-6 py-4 text-sm text-gray-900">{{.ATURI}}</td>
                    <td class="px-6 py-4 text-sm text-gray-700">{{.ATDID}}</td>
                    <td class="px-6 py-4 text-sm text-gray-700">{{.Type}}</td>
                    <td class="px-6 py-4 text-sm text-gray-700">{{.State}}</td>
                    <td class="px-6 py-4 text-sm text-gray-700">{{.PublishAttempts}}</td>
                    <td class="px-6 py-4 text-sm text-red-700">{{.Reason}}</td>
                    <td class="px-6 py-4 text-sm text-gray-500">{{fmtTime .CreatedAt}}</td>
                </tr>
                {{else}}
                <tr>
                    <td colspan="7" class="px-6 py-8 text-sm text-gray-500 text-center">No publish/defer issues.</td>
                </tr>
                {{end}}
            </tbody>
        </table>
    </div>
</div>
{{end}}
`

const blobsContent = `
{{define "content"}}
<div class="px-4 py-6 sm:px-0">
    <h1 class="text-3xl font-bold text-gray-900 mb-6">Blob Sync Status</h1>

    <div class="bg-white shadow rounded-lg overflow-hidden">
        <table class="min-w-full divide-y divide-gray-200">
            <thead class="bg-gray-50">
                <tr>
                    <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">AT CID</th>
                    <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">SSB Blob Ref</th>
                    <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Size</th>
                    <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">MIME</th>
                    <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Downloaded</th>
                </tr>
            </thead>
            <tbody class="bg-white divide-y divide-gray-200">
                {{range .Blobs}}
                <tr>
                    <td class="px-6 py-4 text-sm text-gray-900">{{.ATCID}}</td>
                    <td class="px-6 py-4 text-sm text-gray-700">{{.SSBBlobRef}}</td>
                    <td class="px-6 py-4 text-sm text-gray-700">{{.Size}}</td>
                    <td class="px-6 py-4 text-sm text-gray-700">{{.MimeType}}</td>
                    <td class="px-6 py-4 text-sm text-gray-500">{{fmtTime .DownloadedAt}}</td>
                </tr>
                {{else}}
                <tr>
                    <td colspan="5" class="px-6 py-8 text-sm text-gray-500 text-center">No blobs bridged yet.</td>
                </tr>
                {{end}}
            </tbody>
        </table>
    </div>
</div>
{{end}}
`

const stateContent = `
{{define "content"}}
<div class="px-4 py-6 sm:px-0">
    <h1 class="text-3xl font-bold text-gray-900 mb-6">Bridge State</h1>

    <div class="grid grid-cols-1 md:grid-cols-3 gap-4 mb-6">
        <div class="bg-white overflow-hidden shadow rounded-lg">
            <div class="p-4">
                <div class="text-sm text-gray-500">Deferred Count</div>
                <div class="text-2xl font-semibold text-amber-700">{{.DeferredCount}}</div>
            </div>
        </div>
        <div class="bg-white overflow-hidden shadow rounded-lg">
            <div class="p-4">
                <div class="text-sm text-gray-500">Deleted Count</div>
                <div class="text-2xl font-semibold text-gray-900">{{.DeletedCount}}</div>
            </div>
        </div>
        <div class="bg-white overflow-hidden shadow rounded-lg">
            <div class="p-4">
                <div class="text-sm text-gray-500">Latest Defer Reason</div>
                <div class="text-sm font-semibold text-amber-700 break-words">{{if .LatestDeferReason}}{{.LatestDeferReason}}{{else}}(none){{end}}</div>
            </div>
        </div>
    </div>

    <div class="bg-white shadow rounded-lg overflow-hidden">
        <table class="min-w-full divide-y divide-gray-200">
            <thead class="bg-gray-50">
                <tr>
                    <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Key</th>
                    <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Value</th>
                    <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Updated</th>
                </tr>
            </thead>
            <tbody class="bg-white divide-y divide-gray-200">
                {{range .State}}
                <tr>
                    <td class="px-6 py-4 text-sm text-gray-900">{{.Key}}</td>
                    <td class="px-6 py-4 text-sm text-gray-700">{{.Value}}</td>
                    <td class="px-6 py-4 text-sm text-gray-500">{{fmtTime .UpdatedAt}}</td>
                </tr>
                {{else}}
                <tr>
                    <td colspan="3" class="px-6 py-8 text-sm text-gray-500 text-center">No bridge state entries.</td>
                </tr>
                {{end}}
            </tbody>
        </table>
    </div>
</div>
{{end}}
`

// DashboardData contains summary metrics for the dashboard page.
type DashboardData struct {
	AccountCount        int
	MessageCount        int
	PublishedCount      int
	PublishFailureCount int
	DeferredCount       int
	DeletedCount        int
	BlobCount           int
	FirehoseCursor      string
	BridgeStatus        string
	LastHeartbeat       string
}

// AccountRow is one bridged account row in the accounts table.
type AccountRow struct {
	ATDID     string
	SSBFeedID string
	Active    bool
	CreatedAt time.Time
}

// AccountsData is the template model for the accounts page.
type AccountsData struct {
	Accounts []AccountRow
}

// MessageRow is one bridged message row in the messages table.
type MessageRow struct {
	ATURI           string
	DetailURL       string
	ATDID           string
	Type            string
	TypeLabel       string
	State           string
	StateLabel      string
	StateClass      string
	SSBMsgRef       string
	IssueText       string
	IssueClass      string
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
	Search string
	Type   string
	State  string
	Sort   string
	Limit  int
}

// MessagesData is the template model for the messages page.
type MessagesData struct {
	Messages      []MessageRow
	Filters       MessagesFilterState
	TypeOptions   []FilterOption
	StateOptions  []FilterOption
	SortOptions   []FilterOption
	LimitOptions  []IntFilterOption
	ActiveFilters []ActiveFilter
	ResultCount   int
	ReachedLimit  bool
}

// DetailField is one labeled value rendered in message detail sections.
type DetailField struct {
	Label string
	Value string
}

// MessageDetailData is the template model for a per-message detail page.
type MessageDetailData struct {
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
}

// FailureRow is one failed publish row in the failures table.
type FailureRow struct {
	ATURI           string
	ATDID           string
	Type            string
	State           string
	Reason          string
	PublishAttempts int
	CreatedAt       time.Time
}

// FailuresData is the template model for the failures page.
type FailuresData struct {
	Failures []FailureRow
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
	Blobs []BlobRow
}

// StateRow is one key/value entry from bridge state.
type StateRow struct {
	Key       string
	Value     string
	UpdatedAt time.Time
}

// StateData is the template model for the state page.
type StateData struct {
	State             []StateRow
	DeferredCount     int
	DeletedCount      int
	LatestDeferReason string
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
	}).Parse(pageLayout + content))
}
