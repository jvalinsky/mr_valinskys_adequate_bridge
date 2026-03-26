package templates

import (
	"html/template"
	"io"
)

const layoutTmpl = `
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
    <nav class="bg-indigo-600 text-white shadow-lg">
        <div class="max-w-7xl mx-auto px-4">
            <div class="flex items-center justify-between h-16">
                <div class="flex items-center">
                    <span class="font-bold text-xl">Bridge Admin</span>
                    <div class="ml-10 flex items-baseline space-x-4">
                        <a href="/" class="hover:bg-indigo-500 px-3 py-2 rounded-md text-sm font-medium">Dashboard</a>
                        <a href="/accounts" class="hover:bg-indigo-500 px-3 py-2 rounded-md text-sm font-medium">Accounts</a>
                        <a href="/messages" class="hover:bg-indigo-500 px-3 py-2 rounded-md text-sm font-medium">Messages</a>
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

const dashboardTmpl = `
{{define "content"}}
<div class="px-4 py-6 sm:px-0">
    <h1 class="text-3xl font-bold text-gray-900 mb-6">Dashboard</h1>
    
    <div class="grid grid-cols-1 md:grid-cols-3 gap-6">
        <div class="bg-white overflow-hidden shadow rounded-lg">
            <div class="p-5">
                <div class="flex items-center">
                    <div class="flex-shrink-0 bg-indigo-500 rounded-md p-3">
                        <svg class="h-6 w-6 text-white" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 4.354a4 4 0 110 5.292M15 21H3v-1a6 6 0 0112 0v1zm0 0h6v-1a6 6 0 00-9-5.197M13 7a4 4 0 11-8 0 4 4 0 018 0z" />
                        </svg>
                    </div>
                    <div class="ml-5 w-0 flex-1">
                        <dl>
                            <dt class="text-sm font-medium text-gray-500 truncate">Bridged Accounts</dt>
                            <dd class="text-3xl font-semibold text-gray-900">{{.AccountCount}}</dd>
                        </dl>
                    </div>
                </div>
            </div>
            <div class="bg-gray-50 px-5 py-3">
                <div class="text-sm">
                    <a href="/accounts" class="font-medium text-indigo-700 hover:text-indigo-900">View all</a>
                </div>
            </div>
        </div>

        <div class="bg-white overflow-hidden shadow rounded-lg">
            <div class="p-5">
                <div class="flex items-center">
                    <div class="flex-shrink-0 bg-green-500 rounded-md p-3">
                        <svg class="h-6 w-6 text-white" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M8 10h.01M12 10h.01M16 10h.01M9 16H5a2 2 0 01-2-2V6a2 2 0 012-2h14a2 2 0 012 2v8a2 2 0 01-2 2h-5l-5 5v-5z" />
                        </svg>
                    </div>
                    <div class="ml-5 w-0 flex-1">
                        <dl>
                            <dt class="text-sm font-medium text-gray-500 truncate">Messages Bridged</dt>
                            <dd class="text-3xl font-semibold text-gray-900">{{.MessageCount}}</dd>
                        </dl>
                    </div>
                </div>
            </div>
            <div class="bg-gray-50 px-5 py-3">
                <div class="text-sm">
                    <a href="/messages" class="font-medium text-indigo-700 hover:text-indigo-900">View stream</a>
                </div>
            </div>
        </div>

        <div class="bg-white overflow-hidden shadow rounded-lg">
            <div class="p-5">
                <div class="flex items-center">
                    <div class="flex-shrink-0 bg-yellow-500 rounded-md p-3">
                        <svg class="h-6 w-6 text-white" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M13 10V3L4 14h7v7l9-11h-7z" />
                        </svg>
                    </div>
                    <div class="ml-5 w-0 flex-1">
                        <dl>
                            <dt class="text-sm font-medium text-gray-500 truncate">Bridge Status</dt>
                            <dd class="text-xl font-semibold text-gray-900">{{if .Active}}Active{{else}}Stopped{{end}}</dd>
                        </dl>
                    </div>
                </div>
            </div>
        </div>
    </div>
</div>
{{end}}
`

var tmpl = template.Must(template.New("layout").Parse(layoutTmpl))

func init() {
	template.Must(tmpl.New("dashboard").Parse(dashboardTmpl))
}

type DashboardData struct {
	AccountCount int
	MessageCount int
	Active       bool
}

func RenderDashboard(w io.Writer, data DashboardData) error {
	return tmpl.ExecuteTemplate(w, "dashboard", data)
}
