package room

import (
	"context"
	"html/template"
	"net/http"
	"strings"
)

type authHandler struct {
	authFallback authFallbackService
	authTokens   authTokenService
}

type authFallbackService interface {
	Check(ctx context.Context, username, password string) (int64, error)
	SetPassword(ctx context.Context, memberID int64, password string) error
	CreateResetToken(ctx context.Context, createdByMember, forMember int64) (string, error)
	SetPasswordWithToken(ctx context.Context, resetToken, password string) error
}

type authTokenService interface {
	CreateToken(ctx context.Context, memberID int64) (string, error)
}

const authTokenCookieName = "auth_token"

func newAuthHandler(authFallback authFallbackService, authTokens authTokenService) *authHandler {
	return &authHandler{
		authFallback: authFallback,
		authTokens:   authTokens,
	}
}

func (h *authHandler) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		h.serveLoginPage(w, r)
		return
	}

	if r.Method == http.MethodPost {
		h.handleLoginSubmit(w, r)
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

func (h *authHandler) serveLoginPage(w http.ResponseWriter, r *http.Request) {
	next := normalizeNextPath(r.URL.Query().Get("next"))

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if err := loginPageTemplate.Execute(w, struct {
		Next string
	}{
		Next: next,
	}); err != nil {
		http.Error(w, "Template error", http.StatusInternalServerError)
	}
}

func (h *authHandler) handleLoginSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	username := r.FormValue("username")
	password := r.FormValue("password")

	if username == "" || password == "" {
		http.Error(w, "Username and password required", http.StatusBadRequest)
		return
	}

	memberID, err := h.authFallback.Check(r.Context(), username, password)
	if err != nil {
		http.Error(w, "Invalid credentials", http.StatusUnauthorized)
		return
	}

	if h.authTokens != nil {
		token, err := h.authTokens.CreateToken(r.Context(), memberID)
		if err != nil {
			http.Error(w, "Failed to create auth token", http.StatusInternalServerError)
			return
		}

		http.SetCookie(w, &http.Cookie{
			Name:     authTokenCookieName,
			Value:    token,
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			Secure:   r.TLS != nil,
		})
	}

	next := normalizeNextPath(r.FormValue("next"))
	if next == "" {
		next = "/"
	}
	http.Redirect(w, r, next, http.StatusSeeOther)
}

func (h *authHandler) handleResetPassword(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		h.serveResetPasswordPage(w, r)
		return
	}

	if r.Method == http.MethodPost {
		h.handleResetPasswordSubmit(w, r)
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

func (h *authHandler) serveResetPasswordPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(resetPasswordPageHTML))
}

func (h *authHandler) handleResetPasswordSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	token := r.FormValue("token")
	password := r.FormValue("password")

	if token == "" || password == "" {
		http.Error(w, "Token and password required", http.StatusBadRequest)
		return
	}

	if err := h.authFallback.SetPasswordWithToken(r.Context(), token, password); err != nil {
		http.Error(w, "Invalid or expired token", http.StatusBadRequest)
		return
	}

	http.Redirect(w, r, "/login?reset=success", http.StatusSeeOther)
}

const loginPageHTML = `
<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <title>Sign In</title>
  <style>
    body { font-family: system-ui, sans-serif; max-width: 400px; margin: 50px auto; padding: 20px; }
    h1 { color: #0d7f64; }
    input { width: 100%; padding: 10px; margin: 10px 0; box-sizing: border-box; }
    button { background: #0d7f64; color: white; padding: 10px 20px; border: none; cursor: pointer; width: 100%; }
    button:hover { background: #0a6654; }
    .fallback-link { margin-top: 20px; text-align: center; }
    .fallback-link a { color: #0d7f64; }
  </style>
</head>
<body>
  <h1>Sign In</h1>
  <p>Sign in with your room member account.</p>
  <form method="post" action="/login">
    {{if .Next}}<input type="hidden" name="next" value="{{.Next}}" />{{end}}
    <input type="text" name="username" placeholder="Username" required />
    <input type="password" name="password" placeholder="Password" required />
    <button type="submit">Sign In</button>
  </form>
  <div class="fallback-link">
    <a href="/fallback/login">Use SSB identity</a>
  </div>
</body>
</html>
`

var loginPageTemplate = template.Must(template.New("login-page").Parse(loginPageHTML))

func normalizeNextPath(raw string) string {
	next := strings.TrimSpace(raw)
	if next == "" {
		return ""
	}
	if !strings.HasPrefix(next, "/") {
		return ""
	}
	if strings.HasPrefix(next, "//") {
		return ""
	}
	if strings.Contains(next, "://") {
		return ""
	}
	return next
}

const resetPasswordPageHTML = `
<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <title>Reset Password</title>
  <style>
    body { font-family: system-ui, sans-serif; max-width: 400px; margin: 50px auto; padding: 20px; }
    h1 { color: #0d7f64; }
    input { width: 100%; padding: 10px; margin: 10px 0; box-sizing: border-box; }
    button { background: #0d7f64; color: white; padding: 10px 20px; border: none; cursor: pointer; width: 100%; }
    button:hover { background: #0a6654; }
  </style>
</head>
<body>
  <h1>Reset Password</h1>
  <form method="post" action="/reset-password">
    <input type="text" name="token" placeholder="Reset token" required />
    <input type="password" name="password" placeholder="New password" required />
    <button type="submit">Reset Password</button>
  </form>
</body>
</html>
`
