package authproxy

import (
	"context"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/agentauth"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCodexSubscriptionCatalogAndInference(t *testing.T) {
	st := agentauth.NewStore(t.TempDir())
	if err := st.ImportCredential("codex", ".codex/auth.json", []byte(`{"tokens":{"access_token":"private-token","account_id":"connected-account"}}`)); err != nil {
		t.Fatal(err)
	}
	var paths []string
	stubUpstream(t, "codex", func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.Header.Get("Authorization") != "Bearer private-token" || r.Header.Get("ChatGPT-Account-Id") != "connected-account" {
			t.Error("did not use connected account")
		}
		if r.URL.Path == "/models" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"models":[{"slug":"account-coder","display_name":"Account coder","visibility":"list","context_window":12345},{"slug":"internal-model","visibility":"hide"}]}`))
		} else {
			w.Write([]byte("ok"))
		}
	})
	proxy := New(st, nil)
	models, err := proxy.Models(context.Background(), "codex")
	if err != nil || len(models.Models) != 1 || models.Models[0].ID != "account-coder" {
		t.Fatalf("catalog=%+v err=%v", models, err)
	}
	req := httptest.NewRequest("POST", "/codex/openai/responses", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer dummy")
	req.Header.Set("ChatGPT-Account-Id", "forged-account")
	response := httptest.NewRecorder()
	proxy.ServeHTTP(response, req)
	if response.Code != 200 || len(paths) != 2 || paths[1] != "/responses" {
		t.Fatalf("status=%d paths=%v", response.Code, paths)
	}
}

func TestCatalogDoesNotGuessOnProviderFailure(t *testing.T) {
	st := agentauth.NewStore(t.TempDir())
	writeAPIKey(t, st, "claude-code", "test-key")
	stubUpstream(t, "anthropic", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		w.Write([]byte("sensitive upstream details"))
	})
	catalog, err := New(st, nil).Models(context.Background(), "claude-code")
	if err == nil || len(catalog.Models) != 0 || strings.Contains(err.Error(), "sensitive") {
		t.Fatalf("catalog=%+v error=%v", catalog, err)
	}
}

func TestImportedOpenCodeAndPiUseExactAccountCatalog(t *testing.T) {
	for _, agent := range []string{"opencode", "pi"} {
		t.Run(agent, func(t *testing.T) {
			account := "openai-codex"
			if agent == "opencode" {
				account = "openai"
			}
			st := agentauth.NewStore(t.TempDir())
			rel, _ := agentauth.CredentialFile(agent)
			if err := st.ImportCredential(agent, rel, []byte(`{"`+account+`":{"type":"oauth","access":"private-account-token","accountId":"account-id","expires":4102444800000}}`)); err != nil {
				t.Fatal(err)
			}
			stubUpstream(t, "codex", func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer private-account-token" || r.Header.Get("ChatGPT-Account-Id") != "account-id" {
					t.Error("wrong account authorization")
				}
				w.Write([]byte(`{"models":[{"slug":"team/model:exact","visibility":"list"}]}`))
			})
			proxy := New(st, nil)
			catalog, err := proxy.Models(context.Background(), agent)
			if err != nil || len(catalog.Models) != 1 || catalog.Models[0].ID != account+"/team/model:exact" {
				t.Fatalf("catalog=%+v error=%v", catalog, err)
			}
			connection, err := proxy.BuiltinConnection(context.Background(), agent, account+"/team/model:exact", "http://proxy:9100")
			if err != nil || connection.Protocol != "openai-codex-responses" || connection.Model.ID != "team/model:exact" || connection.Token == "private-account-token" {
				t.Fatalf("invalid connection: %+v err=%v", connection, err)
			}
			if _, err := proxy.BuiltinConnection(context.Background(), agent, "guessed-model", "http://proxy:9100"); err == nil {
				t.Fatal("accepted guessed model")
			}
		})
	}
}

func TestAnthropicCatalogPaginationAndDefault(t *testing.T) {
	st := agentauth.NewStore(t.TempDir())
	writeAPIKey(t, st, "claude-code", "account-key")
	stubUpstream(t, "anthropic", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != "account-key" {
			t.Error("missing key")
		}
		if r.URL.Query().Get("after_id") == "first/model" {
			w.Write([]byte(`{"data":[{"id":"second/model"}],"has_more":false}`))
			return
		}
		w.Write([]byte(`{"data":[{"id":"first/model"}],"has_more":true,"last_id":"first/model"}`))
	})
	proxy := New(st, nil)
	catalog, err := proxy.Models(context.Background(), "claude-code")
	if err != nil || len(catalog.Models) != 2 {
		t.Fatalf("catalog=%+v error=%v", catalog, err)
	}
	selected, err := proxy.ResolveModel(context.Background(), "claude-code", "")
	if err != nil || selected != "first/model" {
		t.Fatalf("default=%q err=%v", selected, err)
	}
	if _, err := proxy.ResolveModel(context.Background(), "claude-code", "guessed-alias"); err == nil {
		t.Fatal("accepted unlisted alias")
	}
}

func TestAccountKeysAreAgentSpecific(t *testing.T) {
	oauth := importedCredential{Type: "oauth", Access: "token"}
	if importedUpstream("opencode", "openai-codex", oauth) != "" {
		t.Fatal("OpenCode must not alias a second credential to its openai subscription route")
	}
	if importedUpstream("opencode", "openai", oauth) != "codex" || importedUpstream("pi", "openai-codex", oauth) != "codex" || importedUpstream("pi", "openai", oauth) != "openai" {
		t.Fatal("account schema was guessed across agents")
	}
}
