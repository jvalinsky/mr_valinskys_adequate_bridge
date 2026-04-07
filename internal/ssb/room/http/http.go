package http

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"strings"

	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/muxrpc"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/refs"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/roomdb"
	"github.com/jvalinsky/mr_valinskys_adequate_bridge/internal/ssb/roomstate"
)

type Server struct {
	keyPair    *refs.FeedRef
	members    roomdb.MembersService
	aliases    roomdb.AliasesService
	invites    roomdb.InvitesService
	config     roomdb.RoomConfig
	authTokens roomdb.AuthWithSSBService
	state      *roomstate.Manager

	muxrpcHandler muxrpc.Handler

	templates *template.Template
}

type Options struct {
	KeyPair    *refs.FeedRef
	Members    roomdb.MembersService
	Aliases    roomdb.AliasesService
	Invites    roomdb.InvitesService
	Config     roomdb.RoomConfig
	AuthTokens roomdb.AuthWithSSBService
	State      *roomstate.Manager
}

func New(opts Options) *Server {
	s := &Server{
		keyPair:    opts.KeyPair,
		members:    opts.Members,
		aliases:    opts.Aliases,
		invites:    opts.Invites,
		config:     opts.Config,
		authTokens: opts.AuthTokens,
		state:      opts.State,
	}
	s.initTemplates()
	return s
}

func (s *Server) initTemplates() {
	tmpl := template.New("")
	template.Must(tmpl.New("home").Parse(homeTemplate))
	template.Must(tmpl.New("join").Parse(joinTemplate))
	template.Must(tmpl.New("login").Parse(loginTemplate))
	template.Must(tmpl.New("bots").Parse(botsTemplate))
	template.Must(tmpl.New("botDetail").Parse(botDetailTemplate))
	s.templates = tmpl
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/", s.handleHome)
	mux.HandleFunc("/healthz", s.handleHealthz)
	mux.HandleFunc("/join", s.handleJoin)
	mux.HandleFunc("/join/", s.handleJoinToken)
	mux.HandleFunc("/login", s.handleLogin)
	mux.HandleFunc("/login/", s.handleLoginSSB)
	mux.HandleFunc("/bots", s.handleBots)
	mux.HandleFunc("/bots/", s.handleBotDetail)
	mux.HandleFunc("/create-invite", s.handleCreateInvite)

	return mux
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	io.WriteString(w, "ok\n")
}

func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	members, _ := s.members.List(r.Context())
	aliases, _ := s.aliases.List(r.Context())
	peers := s.state.Peers()

	data := struct {
		RoomID      string
		MemberCount int
		AliasCount  int
		OnlineCount int
	}{
		RoomID:      s.keyPair.String(),
		MemberCount: len(members),
		AliasCount:  len(aliases),
		OnlineCount: len(peers),
	}

	s.render(w, r, "home", data)
}

func (s *Server) handleJoin(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, "join", nil)
}

func (s *Server) handleJoinToken(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimPrefix(r.URL.Path, "/join/")
	if token == "" {
		http.Redirect(w, r, "/join", http.StatusFound)
		return
	}

	data := struct {
		Token string
	}{Token: token}

	s.render(w, r, "join", data)
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, "login", nil)
}

func (s *Server) handleLoginSSB(w http.ResponseWriter, r *http.Request) {
	alias := strings.TrimPrefix(r.URL.Path, "/login/")
	if alias == "" {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}

	io.WriteString(w, fmt.Sprintf("SSB login for %s - challenge mechanism not implemented\n", alias))
}

func (s *Server) handleBots(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/bots" {
		http.NotFound(w, r)
		return
	}

	members, _ := s.members.List(r.Context())

	type BotInfo struct {
		ID    string
		Role  string
		Count int
	}
	bots := make([]BotInfo, 0, len(members))
	for _, m := range members {
		bots = append(bots, BotInfo{
			ID:    m.PubKey.String(),
			Role:  m.Role.String(),
			Count: 0,
		})
	}

	s.render(w, r, "bots", bots)
}

func (s *Server) handleBotDetail(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/bots/")
	if id == "" {
		http.NotFound(w, r)
		return
	}

	feedRef, err := refs.ParseFeedRef(id)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	member, err := s.members.GetByFeed(r.Context(), *feedRef)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	aliases, _ := s.aliases.List(r.Context())
	var memberAliases []string
	for _, a := range aliases {
		if a.Owner.Equal(*feedRef) {
			memberAliases = append(memberAliases, a.Name)
		}
	}

	data := struct {
		ID      string
		Role    string
		Aliases []string
	}{
		ID:      member.PubKey.String(),
		Role:    member.Role.String(),
		Aliases: memberAliases,
	}

	s.render(w, r, "botDetail", data)
}

func (s *Server) handleCreateInvite(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}

	mode, _ := s.config.GetPrivacyMode(r.Context())
	if mode != roomdb.ModeOpen {
		http.Error(w, "invites disabled", http.StatusForbidden)
		return
	}

	token, err := s.invites.Create(r.Context(), -1)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	inviteURL := fmt.Sprintf("%s://%s/join?token=%s", scheme, r.Host, token)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"url": inviteURL})
}

func (s *Server) render(w http.ResponseWriter, r *http.Request, name string, data interface{}) {
	if err := s.templates.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("auth_token")
		if err != nil {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}

		memberID, err := s.authTokens.CheckToken(r.Context(), cookie.Value)
		if err != nil {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}

		ctx := context.WithValue(r.Context(), "memberID", memberID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) CheckAuth(r *http.Request) (int64, bool) {
	cookie, err := r.Cookie("auth_token")
	if err != nil {
		return 0, false
	}

	memberID, err := s.authTokens.CheckToken(r.Context(), cookie.Value)
	if err != nil {
		return 0, false
	}

	return memberID, true
}

func verifySignature(feed refs.FeedRef, challenge, response []byte) bool {
	return subtle.ConstantTimeCompare(response, challenge) == 1
}

const homeTemplate = `
<!DOCTYPE html>
<html>
<head><title>SSB Room</title></head>
<body>
<h1>SSB Room</h1>
<p>Room ID: {{.RoomID}}</p>
<p>Members: {{.MemberCount}} | Aliases: {{.AliasCount}} | Online: {{.OnlineCount}}</p>
<nav>
  <a href="/">Home</a> |
  <a href="/join">Join</a> |
  <a href="/login">Login</a> |
  <a href="/bots">Bots</a>
</nav>
</body>
</html>
`

const joinTemplate = `
<!DOCTYPE html>
<html>
<head><title>Join Room</title></head>
<body>
<h1>Join Room</h1>
{{if .Token}}
<p>Invite token: {{.Token}}</p>
<p>Token-based joining requires SSB client support.</p>
{{else}}
<p>Open rooms accept connections from any SSB peer.</p>
<form method="POST" action="/create-invite">
  <button type="submit">Create Invite Link</button>
</form>
{{end}}
<nav><a href="/">Back</a></nav>
</body>
</html>
`

const loginTemplate = `
<!DOCTYPE html>
<html>
<head><title>Login</title></head>
<body>
<h1>Login to Room</h1>
<p>Use your SSB identity to sign in.</p>
<nav><a href="/">Back</a></nav>
</body>
</html>
`

const botsTemplate = `
<!DOCTYPE html>
<html>
<head><title>Bridged Bots</title></head>
<body>
<h1>Bridged Bots</h1>
{{if .}}
<ul>
{{range .}}<li><a href="/bots/{{.ID}}">{{.ID}}</a> ({{.Role}})</li>{{end}}
</ul>
{{else}}
<p>No bots registered.</p>
{{end}}
<nav><a href="/">Back</a></nav>
</body>
</html>
`

const botDetailTemplate = `
<!DOCTYPE html>
<html>
<head><title>Bot Detail</title></head>
<body>
<h1>Bot Detail</h1>
<p>Feed: {{.ID}}</p>
<p>Role: {{.Role}}</p>
{{if .Aliases}}
<p>Aliases: {{range .Aliases}}{{.}} {{end}}</p>
{{end}}
<nav><a href="/bots">Back</a> | <a href="/">Home</a></nav>
</body>
</html>
`

func (s *Server) SetMuxRPCHandler(h muxrpc.Handler) {
	s.muxrpcHandler = h
}

func (s *Server) ServeMUXRPC(ctx context.Context) error {
	if s.muxrpcHandler == nil {
		return fmt.Errorf("muxrpc handler not set")
	}
	return nil
}

func GenerateAuthToken() string {
	b := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		panic(fmt.Errorf("failed to generate auth token: %w", err))
	}
	return base64.URLEncoding.EncodeToString(b)
}
