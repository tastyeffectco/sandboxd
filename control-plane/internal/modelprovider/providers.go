// Package modelprovider manages operator-configured model connections. Agents
// receive a short-lived model-scoped capability; upstream credentials stay here.
package modelprovider

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"

	"github.com/tastyeffectco/sandboxd/control-plane/internal/modelconfig"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/secrets"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/store"
)

const (
	OpenAI    = "openai-completions"
	Anthropic = "anthropic-messages"
)

type Model = modelconfig.Model

// Config never contains a credential.
type Config struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Protocol  string  `json:"protocol"`
	BaseURL   string  `json:"base_url"`
	Models    []Model `json:"models"`
	HasAPIKey bool    `json:"has_api_key"`
}

// Input uses nil to preserve an existing key; an empty string removes it.
type Input struct {
	Name     string  `json:"name"`
	Protocol string  `json:"protocol"`
	BaseURL  string  `json:"base_url"`
	Models   []Model `json:"models"`
	APIKey   *string `json:"api_key,omitempty"`
}

type Service struct {
	store     *store.Store
	cipher    *secrets.Cipher
	signKey   [32]byte
	transport *http.Transport
}

func New(st *store.Store, cipher *secrets.Cipher) (*Service, error) {
	if st == nil || cipher == nil {
		return nil, errors.New("provider storage and encryption are required")
	}
	s := &Service{store: st, cipher: cipher}
	if _, err := rand.Read(s.signKey[:]); err != nil {
		return nil, err
	}
	s.transport = http.DefaultTransport.(*http.Transport).Clone()
	// Resolve and validate at dial time, including on redirects/reconnections.
	// Explicit local and private endpoints are supported; metadata, link-local,
	// multicast and unspecified addresses are never valid model endpoints.
	s.transport.Proxy = nil
	s.transport.DialContext = safeDial
	// Inference can spend minutes prefilling on a local CPU. The signed grant
	// bounds the whole request; Probe adds its own shorter context deadline.
	s.transport.ResponseHeaderTimeout = 0
	return s, nil
}

func allowedIP(ip net.IP) bool {
	return ip != nil && !ip.IsUnspecified() && !ip.IsMulticast() &&
		!ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast() &&
		!ip.Equal(net.ParseIP("100.100.100.200")) &&
		(ip.IsGlobalUnicast() || ip.IsLoopback())
}

func safeDial(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, errors.New("invalid provider address")
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, errors.New("cannot resolve provider host")
	}
	if len(ips) == 0 {
		return nil, errors.New("provider host has no addresses")
	}
	for _, ip := range ips {
		if ip.Zone != "" || !allowedIP(ip.IP) {
			return nil, errors.New("provider address is not allowed")
		}
	}
	dialer := &net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}
	for _, ip := range ips {
		conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
		if err == nil {
			return conn, nil
		}
	}
	return nil, errors.New("cannot connect to provider")
}

func validate(in *Input) error {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len(in.Name) > 100 {
		return errors.New("name must contain 1–100 bytes")
	}
	if in.Protocol != OpenAI && in.Protocol != Anthropic {
		return errors.New("protocol must be openai-completions or anthropic-messages")
	}
	u, err := url.Parse(strings.TrimRight(strings.TrimSpace(in.BaseURL), "/"))
	if err != nil || u.Hostname() == "" || (u.Scheme != "https" && u.Scheme != "http") ||
		u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" ||
		strings.Contains(u.Host, "%") || strings.Contains(u.Path, "..") || u.RawPath != "" {
		return errors.New("base_url must be an HTTP(S) API base URL without credentials, query or fragment")
	}
	if ip := net.ParseIP(u.Hostname()); ip != nil && !allowedIP(ip) {
		return errors.New("provider address is not allowed")
	}
	in.BaseURL = u.String()
	if len(in.BaseURL) > 2048 || len(in.Models) == 0 || len(in.Models) > 100 {
		return errors.New("provide 1–100 models and a base URL of at most 2048 bytes")
	}
	seen := map[string]bool{}
	for i := range in.Models {
		m := &in.Models[i]
		if m.ID == "" || len(m.ID) > 200 || strings.TrimSpace(m.ID) != m.ID ||
			strings.IndexFunc(m.ID, unicode.IsControl) >= 0 || seen[m.ID] {
			return errors.New("model IDs must be unique, nonempty and at most 200 bytes")
		}
		seen[m.ID] = true
		if m.ContextWindow == 0 {
			m.ContextWindow = 32768
		}
		if m.MaxTokens == 0 {
			m.MaxTokens = 4096
		}
		if m.ContextWindow < 512 || m.ContextWindow > 10000000 ||
			m.MaxTokens < 1 || m.MaxTokens >= m.ContextWindow {
			return errors.New("model output limit must be positive and smaller than its context window (512–10000000)")
		}
	}
	if in.APIKey != nil && (len(*in.APIKey) > 16384 || strings.IndexFunc(*in.APIKey, unicode.IsControl) >= 0) {
		return errors.New("API key is too long or contains control characters")
	}
	return nil
}

func randomID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// Save creates a connection when id is empty, otherwise replaces an existing
// one. No network requests occur while saving.
func (s *Service) Save(ctx context.Context, id string, in Input) (Config, error) {
	if err := validate(&in); err != nil {
		return Config{}, err
	}
	row := &store.ModelProvider{}
	var err error
	if id == "" {
		id, err = randomID()
	} else {
		row, err = s.store.GetModelProvider(ctx, id)
	}
	if err != nil {
		return Config{}, err
	}
	if in.APIKey != nil {
		row.KeyCiphertext, row.KeyNonce = nil, nil
		if *in.APIKey != "" {
			row.KeyCiphertext, row.KeyNonce, err = s.cipher.Seal([]byte(*in.APIKey))
			if err != nil {
				return Config{}, errors.New("could not encrypt provider key")
			}
		}
	}
	c := Config{ID: id, Name: in.Name, Protocol: in.Protocol, BaseURL: in.BaseURL,
		Models: in.Models, HasAPIKey: len(row.KeyCiphertext) != 0}
	raw, err := json.Marshal(c)
	if err != nil {
		return Config{}, err
	}
	row.ID, row.ConfigJSON = id, string(raw)
	row.Revision, err = randomID()
	if err != nil {
		return Config{}, err
	}
	return c, s.store.SaveModelProvider(ctx, row)
}

func (s *Service) List(ctx context.Context) ([]Config, error) {
	rows, err := s.store.ListModelProviders(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Config, 0, len(rows))
	for _, row := range rows {
		var c Config
		if err := json.Unmarshal([]byte(row.ConfigJSON), &c); err != nil {
			return nil, errors.New("invalid stored provider configuration")
		}
		out = append(out, c)
	}
	return out, nil
}

func (s *Service) Delete(ctx context.Context, id string) error {
	return s.store.DeleteModelProvider(ctx, id)
}

type grant struct {
	Provider, Revision, Model string
	Expires                   int64
}

// Connection is safe to send to runtimed: Token is a scoped proxy capability,
// never an upstream API key. BaseURL points only at the control-plane proxy.
type Connection = modelconfig.Connection

func (s *Service) Connection(ctx context.Context, id, model, proxyURL string, ttl time.Duration) (*Connection, error) {
	row, err := s.store.GetModelProvider(ctx, id)
	if err != nil {
		return nil, err
	}
	var c Config
	if err := json.Unmarshal([]byte(row.ConfigJSON), &c); err != nil {
		return nil, errors.New("invalid stored provider configuration")
	}
	if len(c.Models) == 0 {
		return nil, errors.New("provider has no configured models")
	}
	if proxyURL == "" || ttl <= 0 || ttl > 24*time.Hour+time.Minute {
		return nil, errors.New("provider proxy is unavailable or task lifetime is invalid")
	}
	if model == "" {
		model = c.Models[0].ID
	}
	for _, m := range c.Models {
		if m.ID != model {
			continue
		}
		payload, _ := json.Marshal(grant{id, row.Revision, model, time.Now().Add(ttl).Unix()})
		encoded := base64.RawURLEncoding.EncodeToString(payload)
		mac := hmac.New(sha256.New, s.signKey[:])
		mac.Write([]byte(encoded))
		token := encoded + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
		return &Connection{ID: id, Protocol: c.Protocol, BaseURL: strings.TrimRight(proxyURL, "/") + "/providers/" + id + "/v1",
			Token: token, Model: m}, nil
	}
	return nil, fmt.Errorf("model is not configured for this connection")
}

func (s *Service) verify(token string) (*grant, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 || len(token) > 2048 {
		return nil, errors.New("invalid capability")
	}
	mac := hmac.New(sha256.New, s.signKey[:])
	mac.Write([]byte(parts[0]))
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || !hmac.Equal(sig, mac.Sum(nil)) {
		return nil, errors.New("invalid capability")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	var g grant
	if err != nil || json.Unmarshal(raw, &g) != nil || g.Expires <= time.Now().Unix() {
		return nil, errors.New("expired or invalid capability")
	}
	return &g, nil
}
