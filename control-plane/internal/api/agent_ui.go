package api

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tastyeffectco/sandboxd/control-plane/internal/agentcatalog"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/store"
)

// An agent's own web app (OpenClaw's Control panel) is shown to its owner
// through sandboxd: the owner signs in to sandboxd as usual and sandboxd
// signs them in to the agent with an identity header the agent accepts only
// from sandboxd's address. No token to paste, no device to pair, and the
// agent's port is never published.

// agentConfigs remembers, per agent, the sandboxd address its config trusts.
type agentConfigs struct {
	mu     sync.Mutex
	ip     map[string]string
	jars   map[string]http.CookieJar // sandboxd's sessions with agent apps
	tokens map[string]string         // sandboxd's session tokens with agent apps
}

// setAgentIdentity signs a request to an agent in as its owner, through
// sandboxd. Agents that do not use proxy sign-in ignore these headers.
func setAgentIdentity(h http.Header) {
	for _, k := range []string{"Forwarded", "X-Forwarded-For", "X-Forwarded-Host", "X-Forwarded-Proto", "X-Real-Ip"} {
		h.Del(k)
	}
	h.Set(agentcatalog.UserHeader, agentcatalog.User)
	h.Set("X-Forwarded-For", agentcatalog.ClientIP)
	h.Set("X-Forwarded-Proto", "http")
}

// agentProxyIP is sandboxd's own address on the agents' network: the host of
// the agent proxy URL, resolved now (it can change when sandboxd restarts).
func (s *Server) agentProxyIP(ctx context.Context) (string, error) {
	u, err := url.Parse(s.AgentProxyURL)
	if err != nil || u.Hostname() == "" {
		return "", errors.New("sandboxd's agent address is not set")
	}
	if ip := net.ParseIP(u.Hostname()); ip != nil {
		return ip.String(), nil
	}
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip4", u.Hostname())
	if err != nil || len(ips) == 0 {
		return "", fmt.Errorf("find sandboxd's agent address: %w", err)
	}
	return ips[0].String(), nil
}

// refreshAgentConfig rewrites a proxy-sign-in agent's config when sandboxd's
// address changed, and re-creates one made before proxy sign-in.
func (s *Server) refreshAgentConfig(ctx context.Context, h *store.AgentInstance, pkg agentcatalog.Package) error {
	if !pkg.ProxyAuth || s.AgentRoot == "" {
		return nil
	}
	// An agent made before proxy sign-in still carries a shared token, which
	// OpenClaw refuses next to proxy sign-in: make its container again (its
	// memory and skills stay in its folder).
	if s.Docker != nil {
		if c, err := s.Docker.Inspect(ctx, agentContainer(h.ID)); err == nil &&
			c.Config.Labels["sandboxd.agent.ui"] != containerRev(pkg) {
			s.dropUISession(h.ID)
			return s.provisionAgent(ctx, h, pkg)
		}
	}
	ip, err := s.agentProxyIP(ctx)
	if err != nil {
		return err
	}
	s.agentCfg.mu.Lock()
	defer s.agentCfg.mu.Unlock()
	if s.agentCfg.ip[h.ID] == ip {
		return nil
	}
	// ProxyAuth packages keep their secret in the environment, so the config
	// can be rendered again without rotating it.
	config, err := pkg.Render(agentcatalog.Config{ProxyURL: s.AgentProxyURL, AgentID: h.ID, Model: h.Model, ProxyIP: ip,
		UIOrigin: "http://" + agentUIHost(h.ID, pkg.UIPort)})
	if err != nil {
		return err
	}
	path := filepath.Join(s.AgentRoot, h.ID, pkg.ConfigFile)
	old, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	// OpenClaw picks up a new trusted address without a restart.
	if !bytes.Equal(old, []byte(config)) {
		if err := writeAgentConfig(path, config, pkg.User); err != nil {
			return err
		}
	}
	if s.agentCfg.ip == nil {
		s.agentCfg.ip = map[string]string{}
	}
	s.agentCfg.ip[h.ID] = ip
	return nil
}

// writeAgentConfig replaces the config in one step, owned by the agent's user.
func writeAgentConfig(path, config, user string) error {
	uid, gid := 0, 0
	if parts := strings.SplitN(user, ":", 2); len(parts) == 2 {
		uid, _ = strconv.Atoi(parts[0])
		gid, _ = strconv.Atoi(parts[1])
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(config), 0o600); err != nil {
		return err
	}
	// sandboxd runs as root and hands the file to the agent's user; a
	// non-root sandboxd (tests, development) keeps it as its own.
	if err := os.Chown(tmp, uid, gid); err != nil && os.Geteuid() == 0 {
		return err
	}
	return os.Rename(tmp, path)
}

// agentUIHost is where an agent's app answers on the agents' network.
var agentUIHost = func(id string, port int) string { return fmt.Sprintf("%s:%d", agentContainer(id), port) }

var frameAncestors = regexp.MustCompile(`(?i)\s*frame-ancestors[^;]*;?`)

// uiVersion marks agent containers made with their own app switched on.
const uiVersion = "1"

// containerRev is the label value of an up-to-date agent container.
func containerRev(pkg agentcatalog.Package) string { return uiVersion + pkg.Revision }

// uiPassword is the password sandboxd sets for an agent app with its own
// login, derived from the agent's key so only sandboxd knows it.
func uiPassword(apiKey []byte, purpose string) string {
	m := hmac.New(sha256.New, apiKey)
	m.Write([]byte("sandboxd agent ui " + purpose))
	return hex.EncodeToString(m.Sum(nil))
}

// uiSession is sandboxd's signed-in session with an agent app that has its
// own login. The person's browser never holds it.
func (s *Server) uiSession(h *store.AgentInstance) http.CookieJar {
	s.agentCfg.mu.Lock()
	defer s.agentCfg.mu.Unlock()
	if s.agentCfg.jars == nil {
		s.agentCfg.jars = map[string]http.CookieJar{}
	}
	j := s.agentCfg.jars[h.ID]
	if j == nil {
		j, _ = cookiejar.New(nil)
		s.agentCfg.jars[h.ID] = j
	}
	return j
}

func (s *Server) dropUISession(id string) {
	s.agentCfg.mu.Lock()
	delete(s.agentCfg.jars, id)
	delete(s.agentCfg.tokens, id)
	s.agentCfg.mu.Unlock()
}

// uiSignIn signs sandboxd in to the agent's app with its password.
func (s *Server) uiSignIn(ctx context.Context, h *store.AgentInstance, base *url.URL, jar http.CookieJar) error {
	if s.Secrets == nil {
		return errors.New("no secrets")
	}
	apiKey, err := s.Secrets.Open(h.APIKeyCiphertext, h.APIKeyNonce)
	if err != nil {
		return err
	}
	body, _ := json.Marshal(map[string]string{"provider": "basic", "username": agentcatalog.User,
		"password": uiPassword(apiKey, "password")})
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, base.String()+"/auth/password-login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}).Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("sign-in answered HTTP %d", resp.StatusCode)
	}
	return nil
}

// pairScript runs inside OpenMausBot's container, where a request from its
// own loopback is its owner: it opens a pairing and redeems it for a full
// session token, which only sandboxd keeps.
const pairScript = `set -e
c=$(curl -sf -X POST -H 'content-type: application/json' -d '{"label":"sandboxd"}' http://127.0.0.1:8799/api/auth/pairing)
code=$(printf '%s' "$c" | sed -n 's/.*"code":"\([^"]*\)".*/\1/p')
r=$(curl -sf -X POST -H 'content-type: application/json' -d "{\"code\":\"$code\",\"label\":\"sandboxd\"}" http://127.0.0.1:8799/api/auth/pair)
printf '%s' "$r" | sed -n 's/.*"token":"\([^"]*\)".*/\1/p'`

var pairToken = regexp.MustCompile(`^omb_sess_[A-Za-z0-9_-]{20,200}$`)

// uiToken is sandboxd's session token for an app it signs in to by pairing.
// It is kept in memory only; a fresh one is made after a restart.
func (s *Server) uiToken(ctx context.Context, h *store.AgentInstance) (string, error) {
	s.agentCfg.mu.Lock()
	t := s.agentCfg.tokens[h.ID]
	s.agentCfg.mu.Unlock()
	if t != "" {
		return t, nil
	}
	if s.Docker == nil {
		return "", errors.New("no docker")
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	res, err := s.Docker.Exec(ctx, agentContainer(h.ID), []string{"sh", "-c", pairScript})
	if err != nil {
		return "", err
	}
	t = strings.TrimSpace(res.Stdout)
	if !pairToken.MatchString(t) {
		return "", fmt.Errorf("pairing did not return a session (exit %d)", res.ExitCode)
	}
	s.agentCfg.mu.Lock()
	if s.agentCfg.tokens == nil {
		s.agentCfg.tokens = map[string]string{}
	}
	s.agentCfg.tokens[h.ID] = t
	s.agentCfg.mu.Unlock()
	return t, nil
}

// uiWait answers the agent's window while its app cannot open yet: a plain
// page that tries again by itself.
func uiWait(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	fmt.Fprintf(w, `<!doctype html><meta charset="utf-8"><meta http-equiv="refresh" content="5">`+
		`<title>One moment</title><body style="font:15px system-ui,sans-serif;color:#3f3f46;display:grid;place-items:center;height:90vh;margin:0">`+
		`<p>%s</p></body>`, html.EscapeString(message))
}

// /v1/agent-instances/{id}/ui/{path...} — pages, files and WebSockets.
func (s *Server) v1AgentUI(w http.ResponseWriter, r *http.Request) {
	h, ok := s.ownAgentInstance(w, r)
	if !ok {
		return
	}
	pkg, ok := agentcatalog.Get(h.Package)
	if !ok || pkg.UIPort == 0 {
		writeV1Err(w, http.StatusNotFound, "not_found", "this agent has no app of its own")
		return
	}
	label := pkg.UILabel
	// An agent made before its app was switched on is made again (its
	// memory and skills stay in its folder).
	if s.Docker != nil {
		c, err := s.Docker.Inspect(r.Context(), agentContainer(h.ID))
		if err != nil || !c.State.Running {
			uiWait(w, http.StatusServiceUnavailable, h.Name+" is paused. Start it to open its "+label+".")
			return
		}
		if c.Config.Labels["sandboxd.agent.ui"] != containerRev(pkg) {
			if err := s.provisionAgent(r.Context(), h, pkg); err != nil {
				s.Log.Warn("agent ui: re-create", "agent", h.ID, "err", err.Error())
				uiWait(w, http.StatusBadGateway, h.Name+"’s "+label+" can’t open right now. Try again in a minute.")
				return
			}
			s.dropUISession(h.ID)
			uiWait(w, http.StatusServiceUnavailable, "Getting "+h.Name+"’s "+label+" ready… this takes about a minute.")
			return
		}
	}
	if err := s.refreshAgentConfig(r.Context(), h, pkg); err != nil {
		if s.Log != nil {
			s.Log.Warn("agent ui: refresh config", "agent", h.ID, "err", err.Error())
		}
		uiWait(w, http.StatusBadGateway, h.Name+"’s "+label+" can’t open right now. Try again in a minute.")
		return
	}
	target := &url.URL{Scheme: "http", Host: agentUIHost(h.ID, pkg.UIPort)}
	var jar http.CookieJar
	var bearer string
	if pkg.UILogin == "pairing" {
		t, err := s.uiToken(r.Context(), h)
		if err != nil {
			if s.Log != nil {
				s.Log.Warn("agent ui: pair", "agent", h.ID, "err", err.Error())
			}
			uiWait(w, http.StatusServiceUnavailable, h.Name+"’s "+label+" is starting… this takes about a minute.")
			return
		}
		bearer = t
	}
	if pkg.UILogin == "password" {
		jar = s.uiSession(h)
		if len(jar.Cookies(target)) == 0 {
			if err := s.uiSignIn(r.Context(), h, target, jar); err != nil {
				s.dropUISession(h.ID)
				if s.Log != nil {
					s.Log.Warn("agent ui: sign in", "agent", h.ID, "err", err.Error())
				}
				uiWait(w, http.StatusServiceUnavailable, h.Name+"’s "+label+" is starting… this takes about a minute.")
				return
			}
		}
	}
	rp := &httputil.ReverseProxy{
		Rewrite: func(p *httputil.ProxyRequest) {
			p.Out.URL.Scheme, p.Out.URL.Host = target.Scheme, target.Host
			p.Out.URL.Path = "/" + p.In.PathValue("path")
			p.Out.URL.RawPath = ""
			p.Out.Host = target.Host
			// sandboxd's own credentials never reach the agent.
			for _, k := range []string{"Authorization", "Cookie", "X-Sandboxd-Desktop", agentcatalog.UserHeader} {
				p.Out.Header.Del(k)
			}
			setAgentIdentity(p.Out.Header)
			if bearer != "" {
				p.Out.Header.Set("Authorization", "Bearer "+bearer)
			}
			if jar != nil {
				for _, c := range jar.Cookies(target) {
					p.Out.AddCookie(c)
				}
			}
			// The app sees itself called on its own address.
			if p.Out.Header.Get("Origin") != "" {
				p.Out.Header.Set("Origin", "http://"+target.Host)
			}
		},
		ModifyResponse: func(resp *http.Response) error {
			// A move to its own address stays a move within the app.
			if loc, err := url.Parse(resp.Header.Get("Location")); err == nil && loc.Host == target.Host {
				loc.Scheme, loc.Host = "", ""
				resp.Header.Set("Location", loc.String())
			}
			// The app is shown inside the agent's window.
			resp.Header.Del("X-Frame-Options")
			if csp := resp.Header.Get("Content-Security-Policy"); csp != "" {
				resp.Header.Set("Content-Security-Policy", strings.TrimSpace(frameAncestors.ReplaceAllString(csp, "")))
			}
			if bearer != "" && resp.StatusCode == http.StatusUnauthorized {
				s.dropUISession(h.ID) // signed in again on the next load
			}
			if jar != nil {
				// sandboxd keeps the session; a lapsed one is renewed on the next load.
				jar.SetCookies(target, resp.Cookies())
				resp.Header.Del("Set-Cookie")
				loc := resp.Header.Get("Location")
				if resp.StatusCode == http.StatusUnauthorized || strings.Contains(loc, "/login") {
					s.dropUISession(h.ID)
				}
				if strings.Contains(loc, "/login") {
					resp.Header.Set("Location", "/")
				}
			}
			return nil
		},
		FlushInterval: -1,
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			if s.Log != nil {
				s.Log.Warn("agent ui: proxy", "agent", h.ID, "err", err.Error())
			}
			uiWait(w, http.StatusBadGateway, h.Name+"’s "+label+" isn’t answering yet. If it was just started, it opens in about a minute.")
		},
	}
	rp.ServeHTTP(w, r)
}
