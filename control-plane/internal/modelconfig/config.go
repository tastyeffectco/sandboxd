// Package modelconfig defines the credential-free configuration shared by the
// control plane and coding-agent adapters. It has no storage or HTTP dependencies.
package modelconfig

type Model struct {
	ID            string `json:"id"`
	ContextWindow int    `json:"context_window"`
	MaxTokens     int    `json:"max_tokens"`
}

// Connection carries a temporary proxy capability, never an upstream API key.
type Connection struct {
	ID       string `json:"id"`
	Protocol string `json:"protocol"`
	BaseURL  string `json:"base_url"`
	Token    string `json:"token"`
	Model    Model  `json:"model"`
}
