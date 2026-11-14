package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

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
	mu sync.Mutex
	ip map[string]string
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
// address changed (or the agent predates proxy sign-in). OpenClaw reloads
// its trusted address live; a change of sign-in mode needs a restart.
func (s *Server) refreshAgentConfig(ctx context.Context, h *store.AgentInstance, pkg agentcatalog.Package) error {
	if !pkg.ProxyAuth || s.AgentRoot == "" {
		return nil
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
	config, err := pkg.Render(agentcatalog.Config{ProxyURL: s.AgentProxyURL, AgentID: h.ID, Model: h.Model, ProxyIP: ip})
	if err != nil {
		return err
	}
	path := filepath.Join(s.AgentRoot, h.ID, pkg.ConfigFile)
	old, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if !bytes.Equal(old, []byte(config)) {
		if err := writeAgentConfig(path, config, pkg.User); err != nil {
			return err
		}
		if !bytes.Contains(old, []byte(`"trusted-proxy"`)) && s.Docker != nil {
			name := agentContainer(h.ID)
			if c, err := s.Docker.Inspect(ctx, name); err == nil && c.State.Running {
				_ = s.Docker.Stop(ctx, name, 20)
				if err := s.Docker.Start(ctx, name); err != nil {
					return err
				}
			}
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

// /v1/agent-instances/{id}/ui/{path...} — any method, WebSockets included.
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
	if err := s.refreshAgentConfig(r.Context(), h, pkg); err != nil {
		if s.Log != nil {
			s.Log.Warn("agent ui: refresh config", "agent", h.ID, "err", err.Error())
		}
		writeV1Err(w, http.StatusBadGateway, "unavailable", h.Name+"’s app can’t open right now. Try again in a minute.")
		return
	}
	target := &url.URL{Scheme: "http", Host: agentUIHost(h.ID, pkg.UIPort)}
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
			if p.Out.Header.Get("Origin") != "" {
				p.Out.Header.Set("Origin", agentcatalog.UIOrigin)
			}
		},
		ModifyResponse: func(resp *http.Response) error {
			// The app is shown inside the agent's window.
			resp.Header.Del("X-Frame-Options")
			if csp := resp.Header.Get("Content-Security-Policy"); csp != "" {
				resp.Header.Set("Content-Security-Policy", strings.TrimSpace(frameAncestors.ReplaceAllString(csp, "")))
			}
			return nil
		},
		FlushInterval: -1,
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			if s.Log != nil {
				s.Log.Warn("agent ui: proxy", "agent", h.ID, "err", err.Error())
			}
			writeV1Err(w, http.StatusBadGateway, "unavailable",
				h.Name+" isn’t answering. If it was just started, wait a minute and try again.")
		},
	}
	rp.ServeHTTP(w, r)
}
