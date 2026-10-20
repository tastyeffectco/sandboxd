package modelprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"
)

// ServeHTTP only forwards inference requests for a signed connection/model.
// Sandboxes cannot choose an upstream URL, forward credentials, discover other
// connections, or use a deleted/updated connection's previous capability.
func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if token == "" {
		token = r.Header.Get("X-Api-Key")
	}
	g, err := s.verify(token)
	if err != nil {
		http.Error(w, "invalid or expired provider capability", http.StatusUnauthorized)
		return
	}
	ctx, cancel := context.WithDeadline(r.Context(), time.Unix(g.Expires, 0))
	defer cancel()
	r = r.WithContext(ctx)
	// Also bound a slow incoming request body; a derived context alone does not
	// interrupt reads from the server-side HTTP connection.
	_ = http.NewResponseController(w).SetReadDeadline(time.Unix(g.Expires, 0))
	row, err := s.store.GetModelProvider(r.Context(), g.Provider)
	if err != nil || row.Revision != g.Revision {
		http.Error(w, "provider connection changed; start a new task", http.StatusUnauthorized)
		return
	}
	var c Config
	if json.Unmarshal([]byte(row.ConfigJSON), &c) != nil {
		http.Error(w, "provider configuration unavailable", http.StatusServiceUnavailable)
		return
	}
	suffix := "/chat/completions"
	if c.Protocol == Anthropic {
		suffix = "/messages"
	}
	allowedQuery := r.URL.RawQuery == "" || (c.Protocol == Anthropic && r.URL.RawQuery == "beta=true")
	if r.Method != http.MethodPost || r.URL.Path != "/providers/"+g.Provider+"/v1"+suffix ||
		!allowedQuery || r.URL.RawPath != "" {
		http.Error(w, "unsupported provider operation", http.StatusBadRequest)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 16<<20))
	if err != nil {
		http.Error(w, "inference request is too large", http.StatusRequestEntityTooLarge)
		return
	}
	var input map[string]json.RawMessage
	var model string
	if json.Unmarshal(body, &input) != nil || json.Unmarshal(input["model"], &model) != nil || model != g.Model {
		http.Error(w, "model does not match task capability", http.StatusForbidden)
		return
	}
	// Gateways may accept routing/credential overrides (e.g. LiteLLM api_base).
	// Permit inference fields only, and normalize duplicate JSON keys before
	// forwarding so the model check and upstream parser cannot disagree.
	fields := " model messages stream tools tool_choice temperature top_p "
	if c.Protocol == Anthropic {
		fields += " system max_tokens thinking top_k stop_sequences "
	} else {
		fields += " stream_options max_tokens max_completion_tokens frequency_penalty presence_penalty stop seed response_format parallel_tool_calls reasoning_effort logprobs top_logprobs n store prompt_cache_key "
	}
	allowedFields := make(map[string]bool)
	for _, field := range strings.Fields(fields) {
		allowedFields[field] = true
	}
	for field := range input {
		if !allowedFields[field] {
			http.Error(w, "unsupported inference field", http.StatusBadRequest)
			return
		}
	}
	body, err = json.Marshal(input)
	if err != nil {
		http.Error(w, "invalid inference request", http.StatusBadRequest)
		return
	}
	var key []byte
	if len(row.KeyCiphertext) > 0 {
		key, err = s.cipher.Open(row.KeyCiphertext, row.KeyNonce)
		if err != nil {
			http.Error(w, "provider credential unavailable", http.StatusServiceUnavailable)
			return
		}
	}
	target, err := url.Parse(c.BaseURL)
	if err != nil {
		http.Error(w, "provider endpoint unavailable", http.StatusServiceUnavailable)
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	r.ContentLength = int64(len(body))
	proxy := &httputil.ReverseProxy{
		Transport:     s.transport,
		FlushInterval: -1,
		Rewrite: func(p *httputil.ProxyRequest) {
			p.Out.URL = &url.URL{Scheme: target.Scheme, Host: target.Host,
				Path: strings.TrimRight(target.Path, "/") + suffix, RawQuery: r.URL.RawQuery}
			p.Out.Host = target.Host
			// An allowlist prevents cookies, client auth, forwarding headers and
			// proxy capabilities from reaching the upstream.
			p.Out.Header = http.Header{"Content-Type": {"application/json"}, "Accept": {"application/json, text/event-stream"}}
			if c.Protocol == Anthropic {
				p.Out.Header.Set("Anthropic-Version", "2023-06-01")
				if beta := p.In.Header.Get("Anthropic-Beta"); len(beta) <= 1024 && beta != "" {
					p.Out.Header.Set("Anthropic-Beta", beta)
				}
				if len(key) > 0 {
					p.Out.Header.Set("X-Api-Key", string(key))
				}
			} else if len(key) > 0 {
				p.Out.Header.Set("Authorization", "Bearer "+string(key))
			}
		},
		ModifyResponse: func(resp *http.Response) error {
			contentType := resp.Header.Get("Content-Type")
			resp.Header = http.Header{"Content-Type": {contentType}}
			if resp.StatusCode >= 300 {
				// Never follow redirects or expose upstream error bodies/headers:
				// gateways sometimes echo their authorization/configuration.
				resp.Body.Close()
				message := `{"error":{"type":"provider_error","message":"Provider rejected the request. Check the connection URL, API key, model and quota in Settings."}}`
				if resp.StatusCode >= 500 {
					message = `{"error":{"type":"provider_error","message":"Provider is unavailable. Try again later."}}`
				}
				resp.StatusCode = http.StatusBadRequest // fail fast in CLI retry loops
				resp.Body = io.NopCloser(strings.NewReader(message))
				resp.ContentLength = int64(len(message))
				resp.Header.Set("Content-Type", "application/json")
			}
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, _ error) {
			http.Error(w, "cannot reach provider endpoint", http.StatusBadGateway)
		},
	}
	proxy.ServeHTTP(w, r)
}
