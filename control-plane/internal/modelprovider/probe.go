package modelprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
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
		return errors.New("cannot reach provider; check the URL and network access from the control plane")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return errors.New("provider rejected the test; check URL, API key, model and quota")
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
