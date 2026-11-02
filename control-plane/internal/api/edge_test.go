package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tastyeffectco/sandboxd/control-plane/internal/previewhost"
	"github.com/tastyeffectco/sandboxd/control-plane/internal/store"
)

func newEdgeTestServer(t *testing.T, style, tag string) *Server {
	t.Helper()
	st, err := store.Open(context.Background(), "file::memory:?_fk=1", "../../migrations")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	for _, id := range []string{"01HZXEDGE0000000000000000A", "demo01"} {
		if err := st.Create(context.Background(), &store.Sandbox{ID: id, Status: "running"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.Create(context.Background(), &store.Sandbox{ID: "01HZXPUB00000000000000000A", Status: "running",
		Mode: store.ModeProduction, PublicHosts: []string{"my-shop.1-2-3-4.sslip.io"}}); err != nil {
		t.Fatal(err)
	}
	return &Server{Store: st, PreviewDomain: "1-2-3-4.sslip.io", PreviewHostStyle: style, PreviewHostTag: tag}
}

func TestHostServed(t *testing.T) {
	const d = "1-2-3-4.sslip.io"
	cases := []struct {
		name       string
		style, tag string
		console    string
		host       string
		want       bool
	}{
		{"flat preview, existing sandbox", previewhost.StyleFlat, "", "", "s-01hzxedge0000000000000000a-3000." + d, true},
		{"flat preview, uppercase as sent", previewhost.StyleFlat, "", "", "S-01HZXEDGE0000000000000000A-3000." + d, true},
		{"flat preview, lowercase id kept", previewhost.StyleFlat, "", "", "s-demo01-5173." + d, true},
		{"flat preview, trailing dot and port", previewhost.StyleFlat, "", "", "s-demo01-3000." + d + ".:443", true},
		{"flat preview, unknown sandbox", previewhost.StyleFlat, "", "", "s-01HZXNOPE000000000000000Z-3000." + d, false},
		{"flat preview, other domain", previewhost.StyleFlat, "", "", "s-demo01-3000.example.com", false},
		{"flat preview, suffix attack", previewhost.StyleFlat, "", "", "s-demo01-3000." + d + ".evil.com", false},
		{"flat preview, nested shape refused", previewhost.StyleFlat, "", "", "s-demo01-3000.preview." + d, false},
		{"nested preview", previewhost.StyleNested, "", "", "s-demo01-3000.preview." + d, true},
		{"nested refuses flat shape", previewhost.StyleNested, "", "", "s-demo01-3000." + d, false},
		{"flat tag", previewhost.StyleFlat, "eu1", "", "s-demo01-3000--eu1." + d, true},
		{"flat tag, wrong tag", previewhost.StyleFlat, "eu1", "", "s-demo01-3000--eu2." + d, false},
		{"console flat", previewhost.StyleFlat, "", "", "console." + d, true},
		{"api flat", previewhost.StyleFlat, "", "", "api." + d, true},
		{"api nested", previewhost.StyleNested, "", "", "api.preview." + d, true},
		{"console tagged", previewhost.StyleFlat, "eu1", "", "console--eu1." + d, true},
		{"console override", previewhost.StyleFlat, "", "Admin.Example.com", "admin.example.com", true},
		{"published app", previewhost.StyleFlat, "", "", "my-shop." + d, true},
		{"published app, mixed case", previewhost.StyleFlat, "", "", "My-Shop." + d, true},
		{"unpublished name", previewhost.StyleFlat, "", "", "other-shop." + d, false},
		{"bare domain", previewhost.StyleFlat, "", "", d, false},
		{"random sibling", previewhost.StyleFlat, "", "", "www." + d, false},
		{"empty", previewhost.StyleFlat, "", "", "", false},
		{"ip literal", previewhost.StyleFlat, "", "", "1.2.3.4", false},
		{"wildcard", previewhost.StyleFlat, "", "", "*." + d, false},
		{"underscore", previewhost.StyleFlat, "", "", "s_demo01-3000." + d, false},
		{"empty label", previewhost.StyleFlat, "", "", "s-demo01-3000.." + d, false},
		{"sql-ish", previewhost.StyleFlat, "", "", "s-demo01'or'1-3000." + d, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := newEdgeTestServer(t, c.style, c.tag)
			s.ConsoleHost = c.console
			if got := s.hostServed(context.Background(), c.host); got != c.want {
				t.Errorf("hostServed(%q) = %v; want %v", c.host, got, c.want)
			}
		})
	}
}

func TestHostServedNoDomainOrStore(t *testing.T) {
	if (&Server{}).hostServed(context.Background(), "console.localhost") {
		t.Error("no PreviewDomain must serve nothing")
	}
	s := &Server{PreviewDomain: "ex.com", PreviewHostStyle: previewhost.StyleFlat}
	if s.hostServed(context.Background(), "s-demo01-3000.ex.com") {
		t.Error("no store: previews cannot be confirmed")
	}
	if !s.hostServed(context.Background(), "console.ex.com") {
		t.Error("console is served without a store")
	}
}

// The handler answers Caddy's ask: 200 for served hosts, 404 otherwise, and
// 404 for anything relayed through Traefik (it carries X-Forwarded-For).
func TestEdgeTLSAllowedHandler(t *testing.T) {
	s := newEdgeTestServer(t, previewhost.StyleFlat, "")
	h := s.Handler()
	cases := []struct {
		url  string
		xff  string
		want int
	}{
		{"/edge/tls-allowed?domain=s-demo01-3000.1-2-3-4.sslip.io", "", http.StatusOK},
		{"/edge/tls-allowed?domain=console.1-2-3-4.sslip.io", "", http.StatusOK},
		{"/edge/tls-allowed?domain=evil.example.com", "", http.StatusNotFound},
		{"/edge/tls-allowed", "", http.StatusNotFound},
		{"/edge/tls-allowed?domain=console.1-2-3-4.sslip.io", "203.0.113.9", http.StatusNotFound},
	}
	for _, c := range cases {
		req := httptest.NewRequest(http.MethodGet, c.url, nil)
		if c.xff != "" {
			req.Header.Set("X-Forwarded-For", c.xff)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != c.want {
			t.Errorf("GET %s (xff %q) = %d; want %d", c.url, c.xff, rec.Code, c.want)
		}
	}
}

// Behind a front door the router listens on 127.0.0.1:8088, but public URLs
// are https on 443: no :8088 may leak into them.
func TestPreviewURLBehindFrontDoor(t *testing.T) {
	s := &Server{PreviewDomain: "1-2-3-4.sslip.io", PreviewHostStyle: previewhost.StyleFlat,
		PreviewURLScheme: "https", PublicHTTPPort: "8088"}
	if got, want := s.previewURL("01ABC", 3000), "https://s-01ABC-3000.1-2-3-4.sslip.io"; got != want {
		t.Errorf("previewURL = %q; want %q", got, want)
	}
}

func TestValidMiddlewareRef(t *testing.T) {
	for in, want := range map[string]bool{
		"": true, "sandbox-ratelimit": true, "sandbox-ratelimit@file": true, "sandbox-ratelimit-proxied@file": true,
		"My_Mw@docker": true, "a@": false, "@file": false, "a@File": false, "a b": false, "a,b@file": false,
		"a@file@x": false, "a`b": false, "a\nb": false,
	} {
		if got := ValidMiddlewareRef(in); got != want {
			t.Errorf("ValidMiddlewareRef(%q) = %v; want %v", in, got, want)
		}
	}
}
