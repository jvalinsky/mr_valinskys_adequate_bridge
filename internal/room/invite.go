package room

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/roomdb"
)

type inviteHandler struct {
	roomDB  roomdb.InvitesService
	config  roomdb.RoomConfig
	keyPair inviteKeyPair
	domain  string
}

type inviteKeyPair interface {
	FeedRef() refs.FeedRef
}

func newInviteHandler(roomDB roomdb.InvitesService, config roomdb.RoomConfig, keyPair inviteKeyPair, domain string) *inviteHandler {
	return &inviteHandler{
		roomDB:  roomDB,
		config:  config,
		keyPair: keyPair,
		domain:  domain,
	}
}

type invitePageData struct {
	InviteURL string
	HomeURL   string
	SignInURL string
}

func (h *inviteHandler) handleCreateInvite(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		h.serveInvitePage(w, r)
		return
	}

	if r.Method == http.MethodPost {
		h.handleCreateInviteSubmit(w, r)
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	return
}

func (h *inviteHandler) serveInvitePage(w http.ResponseWriter, r *http.Request) {
	mode, err := h.config.GetPrivacyMode(r.Context())
	if err != nil {
		http.Error(w, "Failed to check room mode", http.StatusInternalServerError)
		return
	}

	if mode != roomdb.ModeOpen {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	data := invitePageData{
		InviteURL: "/create-invite",
		HomeURL:   "/",
		SignInURL: "/login",
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if err := invitePageTemplate.Execute(w, data); err != nil {
		http.Error(w, "Template error", http.StatusInternalServerError)
	}
}

func (h *inviteHandler) handleCreateInviteSubmit(w http.ResponseWriter, r *http.Request) {
	mode, err := h.config.GetPrivacyMode(r.Context())
	if err != nil {
		http.Error(w, "Failed to check room mode", http.StatusInternalServerError)
		return
	}

	if mode != roomdb.ModeOpen {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]string{
			"status": "failed",
			"error":  "room mode is not open",
		})
		return
	}

	token, err := h.roomDB.Create(r.Context(), 0)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"status": "failed",
			"error":  "Failed to create invite",
		})
		return
	}

	feedRef := h.keyPair.FeedRef()
	domain := h.domain
	if domain == "" {
		domain = "localhost"
	}

	inviteURL := fmt.Sprintf("https://%s/%s/join?token=%s", domain, feedRef.String(), token)

	if wantsJSONResponse(r) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{
			"url": inviteURL,
		})
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if err := inviteCreatedTemplate.Execute(w, map[string]string{"URL": inviteURL}); err != nil {
		http.Error(w, "Template error", http.StatusInternalServerError)
	}
}

func (h *inviteHandler) handleJoin(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		http.NotFound(w, r)
		return
	}

	if r.Method == http.MethodGet {
		h.serveJoinPage(w, r, token)
		return
	}

	if r.Method == http.MethodPost {
		h.handleJoinSubmit(w, r, token)
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

func (h *inviteHandler) serveJoinPage(w http.ResponseWriter, r *http.Request, token string) {
	_, err := h.roomDB.GetByToken(r.Context(), token)
	if err != nil {
		http.Error(w, "Invalid or expired invite", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if err := joinPageTemplate.Execute(w, map[string]string{"Token": token}); err != nil {
		http.Error(w, "Template error", http.StatusInternalServerError)
	}
}

func (h *inviteHandler) handleJoinSubmit(w http.ResponseWriter, r *http.Request, token string) {
	http.Error(w, "Use SSB client to accept invite", http.StatusNotImplemented)
}

var invitePageTemplate = template.Must(template.New("invite-create").Parse(publicLayoutTemplate + invitePageHTML))
var inviteCreatedTemplate = template.Must(template.New("invite-created").Parse(inviteCreatedHTML))
var joinPageTemplate = template.Must(template.New("join-room").Parse(publicLayoutTemplate + joinPageHTML))

const invitePageHTML = `
{{define "pageTitle"}}Create Invite - ATProto to SSB Bridge{{end}}
{{define "content"}}
<div class="hero">
  <h1>Create an Invite</h1>
  <p class="eyebrow">Room Access</p>
</div>

<div class="panel">
  <h2 style="margin-top: 0">Get a Room Invite Link</h2>
  <p>Create an invite link to share with others. Recipients can use it to join this Secure Scuttlebutt room.</p>
  
  <form id="inviteForm" method="post">
    <button type="submit" class="btn-primary" style="font-size: 1em; padding: 12px 24px;">
      Create Invite
    </button>
  </form>
  
  <div id="result" style="margin-top: 24px; display: none;">
    <div class="action-row-compact">
      <input type="text" id="inviteUrl" readonly style="flex: 1; padding: 12px; border: 1px solid #ddd; border-radius: 6px; font-family: monospace; font-size: 0.9em;" />
      <button type="button" class="btn-copy" onclick="copyInvite()">Copy</button>
    </div>
    <p style="color: #666; font-size: 0.9em; margin-top: 12px;">
      Share this link with anyone you want to invite. The invite link will expire after use.
    </p>
  </div>
  
  <div id="error" style="margin-top: 24px; display: none; color: #721c24; background: #f8d7da; padding: 12px; border-radius: 6px;"></div>
</div>

<script>
document.getElementById('inviteForm').addEventListener('submit', async function(e) {
  e.preventDefault();
  const result = document.getElementById('result');
  const error = document.getElementById('error');
  const urlInput = document.getElementById('inviteUrl');
  
  result.style.display = 'none';
  error.style.display = 'none';
  
  try {
    const resp = await fetch('/create-invite', {
      method: 'POST',
      headers: { 'Accept': 'application/json' }
    });
    const data = await resp.json();
    
    if (data.error) {
      error.textContent = data.error;
      error.style.display = 'block';
    } else {
      urlInput.value = data.url;
      result.style.display = 'block';
    }
  } catch (err) {
    error.textContent = 'Failed to create invite. Please try again.';
    error.style.display = 'block';
  }
});

function copyInvite() {
  const input = document.getElementById('inviteUrl');
  input.select();
  document.execCommand('copy');
  const btn = document.querySelector('.btn-copy');
  btn.textContent = 'Copied!';
  setTimeout(() => btn.textContent = 'Copy', 2000);
}
</script>
{{end}}
`

const inviteCreatedHTML = `
<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1.0" />
  <title>Invite Created - ATProto to SSB Bridge</title>
  <style>
    :root { --paper: #f3ebdd; --accent: #0d7f64; }
    body { font-family: system-ui, sans-serif; margin: 0; min-height: 100vh; background: var(--paper); color: #132820; }
    .page-shell { max-width: 600px; margin: 0 auto; padding: 48px 24px; }
    .hero, .panel { background: white; border-radius: 12px; padding: 32px; margin-bottom: 24px; box-shadow: 0 2px 8px rgba(0,0,0,0.1); }
    h1 { margin: 0 0 8px 0; color: var(--accent); }
    .eyebrow { color: #666; font-size: 0.85em; text-transform: uppercase; letter-spacing: 0.05em; margin-bottom: 24px; }
    p { line-height: 1.6; }
    .action-row-compact { display: flex; gap: 8px; margin-bottom: 16px; }
    input { flex: 1; padding: 12px; border: 1px solid #ddd; border-radius: 6px; font-family: monospace; font-size: 0.9em; }
    .btn-copy { padding: 12px 20px; background: #f5f5f5; border: 1px solid #ddd; border-radius: 6px; cursor: pointer; font-size: 0.9em; }
    .btn-primary { padding: 12px 24px; background: var(--accent); color: white; border: none; border-radius: 6px; text-decoration: none; display: inline-block; }
    .success { background: #d4edda; color: #155724; padding: 12px; border-radius: 6px; margin-bottom: 16px; }
    a { color: var(--accent); }
  </style>
</head>
<body>
  <div class="page-shell">
    <header style="margin-bottom: 24px;">
      <div style="font-weight: bold;"><a href="/">ATProto to SSB Bridge Room</a></div>
    </header>
    <div class="hero">
      <h1>Invite Created!</h1>
      <p class="eyebrow">Success</p>
      <div class="success">Your invite link is ready to share.</div>
      <div class="action-row-compact">
        <input type="text" value="{{.URL}}" readonly onclick="this.select()" />
        <button class="btn-copy" onclick="copyLink()">Copy</button>
      </div>
      <p style="color: #666; font-size: 0.9em;">
        Share this link with anyone you want to invite to the room. The link works for one-time use.
      </p>
    </div>
    <div style="text-align: center;">
      <a href="/create-invite" class="btn-primary">Create Another</a>
      <span style="margin: 0 12px;">or</span>
      <a href="/">Back to Room</a>
    </div>
  </div>
  <script>
    function copyLink() {
      const input = document.querySelector('input');
      input.select();
      document.execCommand('copy');
      const btn = document.querySelector('.btn-copy');
      btn.textContent = 'Copied!';
      setTimeout(() => btn.textContent = 'Copy', 2000);
    }
  </script>
</body>
</html>
`

const joinPageHTML = `
{{define "pageTitle"}}Join Room - ATProto to SSB Bridge{{end}}
{{define "content"}}
<div class="hero">
  <h1>Join this Room</h1>
  <p class="eyebrow">Room Access</p>
</div>

<div class="panel">
  <h2 style="margin-top: 0">You're Invited!</h2>
  <p>To join this Secure Scuttlebutt room, you'll need an SSB client.</p>
  
  <div style="background: #f5f5f5; padding: 16px; border-radius: 8px; margin: 24px 0;">
    <p style="margin: 0 0 12px 0;"><strong>How to use this invite:</strong></p>
    <ol style="margin: 0; padding-left: 24px; line-height: 1.8;">
      <li>Open your SSB client (e.g., Patchwork, SSB Room client)</li>
      <li>Look for an option to "Accept Invite" or "Join Room"</li>
      <li>Paste this invite token when prompted</li>
    </ol>
  </div>
  
  <div class="action-row-compact">
    <input type="text" value="{{.Token}}" readonly onclick="this.select()" style="flex: 1; padding: 12px; border: 1px solid #ddd; border-radius: 6px; font-family: monospace; font-size: 0.9em;" />
    <button type="button" class="btn-copy" onclick="copyToken()">Copy Token</button>
  </div>
  
  <p style="color: #666; font-size: 0.9em; margin-top: 16px;">
    <a href="/">Back to Room</a>
  </p>
</div>

<script>
function copyToken() {
  const input = document.querySelector('input');
  input.select();
  document.execCommand('copy');
  const btn = document.querySelector('.btn-copy');
  btn.textContent = 'Copied!';
  setTimeout(() => btn.textContent = 'Copy Token', 2000);
}
</script>
{{end}}
`

func withContext(f func(context.Context) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := f(r.Context()); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}
