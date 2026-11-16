package modelprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Probe performs one small, non-streaming inference request. Only the explicit
// operator "test connection" action calls it; saves/listing never spend tokens.
func (s *Service) Probe(ctx context.Context, id, model string) error {
	row, err := s.store.GetModelProvider(ctx, id)
	if err != nil {
		return err
	}
	var c Config
	if json.Unmarshal([]byte(row.ConfigJSON), &c) != nil {
		return errors.New("invalid provider configuration")
	}
	if len(c.Models) == 0 {
		return errors.New("provider has no configured models")
	}
	if model == "" {
		model = c.Models[0].ID
	}
	found := false
	for _, m := range c.Models {
		if m.ID == model {
			found = true
		}
	}
	if !found {
		return errors.New("model is not configured for this connection")
	}
	payload, _ := json.Marshal(map[string]any{
		"model": model, "max_tokens": 8, "stream": false,
		"messages": []map[string]string{{"role": "user", "content": "Reply OK."}},
	})
	suffix := "/chat/completions"
	if c.Protocol == Anthropic {
		suffix = "/messages"
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(c.BaseURL, "/")+suffix, bytes.NewReader(payload))
	if err != nil {
		return errors.New("invalid provider URL")
	}
	req.Header.Set("Content-Type", "application/json")
	if c.Protocol == Anthropic {
		req.Header.Set("Anthropic-Version", "2023-06-01")
	}
	if len(row.KeyCiphertext) > 0 {
		key, err := s.cipher.Open(row.KeyCiphertext, row.KeyNonce)
		if err != nil {
			return errors.New("provider credential unavailable")
		}
		if c.Protocol == Anthropic {
			req.Header.Set("X-Api-Key", string(key))
		} else {
			req.Header.Set("Authorization", "Bearer "+string(key))
		}
	}
	resp, err := s.transport.RoundTrip(req)
	if err != nil {
		if isOllama(c.BaseURL) {
			// Seen on Windows: Ollama answers only on the PC itself by
			// default, so sandboxd's engine cannot reach it.
			return errors.New("sandboxd can't reach Ollama. Make sure Ollama is running. On Windows, set " +
				"OLLAMA_HOST to 0.0.0.0 in your environment variables and restart Ollama, so sandboxd can reach it")
		}
		return errors.New("sandboxd can't reach this AI service. Check its address, and that it is running")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return errors.New("the AI service refused the test. Check its address, API key, model and credit")
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	var result struct {
		Choices []json.RawMessage `json:"choices"`
		Content []json.RawMessage `json:"content"`
	}
	if err != nil || json.Unmarshal(body, &result) != nil ||
		(c.Protocol == OpenAI && len(result.Choices) == 0) || (c.Protocol == Anthropic && len(result.Content) == 0) {
		return errors.New("provider returned an unexpected inference response")
	}
	return nil
}

// isOllama: the address is Ollama's (its default port).
func isOllama(baseURL string) bool {
	u, err := url.Parse(baseURL)
	return err == nil && u.Port() == "11434"
}
