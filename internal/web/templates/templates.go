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
</head>
<body class="bg-gray-100 min-h-screen">
    <nav class="bg-indigo-700 text-white shadow-lg">
        <div class="max-w-7xl mx-auto px-4">
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

    <main class="max-w-7xl mx-auto py-6 sm:px-6 lg:px-8">
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
<div class="px-4 py-6 sm:px-0">
    <h1 class="text-3xl font-bold text-gray-900 mb-6">Messages</h1>

    <div class="bg-white shadow rounded-lg overflow-hidden">
        <table class="min-w-full divide-y divide-gray-200">
            <thead class="bg-gray-50">
                <tr>
                    <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">AT URI</th>
                    <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Author DID</th>
                    <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Type</th>
                    <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">State</th>
                    <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">SSB Ref</th>
                    <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Attempts</th>
                    <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Defer Reason</th>
                    <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Error</th>
                    <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">Created</th>
                </tr>
            </thead>
            <tbody class="bg-white divide-y divide-gray-200">
                {{range .Messages}}
                <tr>
                    <td class="px-6 py-4 text-sm text-gray-900">{{.ATURI}}</td>
                    <td class="px-6 py-4 text-sm text-gray-700">{{.ATDID}}</td>
                    <td class="px-6 py-4 text-sm text-gray-700">{{.Type}}</td>
                    <td class="px-6 py-4 text-sm text-gray-700">{{.State}}</td>
                    <td class="px-6 py-4 text-sm text-gray-700">{{if .SSBMsgRef}}{{.SSBMsgRef}}{{else}}pending{{end}}</td>
                    <td class="px-6 py-4 text-sm text-gray-700">{{.PublishAttempts}}</td>
                    <td class="px-6 py-4 text-sm text-amber-700">{{.DeferReason}}</td>
                    <td class="px-6 py-4 text-sm text-red-700">{{.PublishError}}</td>
                    <td class="px-6 py-4 text-sm text-gray-500">{{fmtTime .CreatedAt}}</td>
                </tr>
                {{else}}
                <tr>
                    <td colspan="9" class="px-6 py-8 text-sm text-gray-500 text-center">No bridged messages yet.</td>
                </tr>
                {{end}}
            </tbody>
        </table>
    </div>
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
	ATDID           string
	Type            string
	State           string
	SSBMsgRef       string
	PublishError    string
	DeferReason     string
	PublishAttempts int
	CreatedAt       time.Time
}

// MessagesData is the template model for the messages page.
type MessagesData struct {
	Messages []MessageRow
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
	dashboardTemplate = mustPageTemplate("dashboard", dashboardContent)
	accountsTemplate  = mustPageTemplate("accounts", accountsContent)
	messagesTemplate  = mustPageTemplate("messages", messagesContent)
	failuresTemplate  = mustPageTemplate("failures", failuresContent)
	blobsTemplate     = mustPageTemplate("blobs", blobsContent)
	stateTemplate     = mustPageTemplate("state", stateContent)
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
