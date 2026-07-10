package api

import "testing"

// previewURL must append the host-facing port unless it's the scheme default
// (80 for http, 443 for https), so a shared-host deploy on e.g. :18080 returns
// a URL the browser/console iframe/open-link actually reach.
func TestPreviewURLPort(t *testing.T) {
	const id = "01ABC"
	cases := []struct {
		name string
		tls  bool
		port string
		want string
	}{
		{"http shared-host port", false, "18080", "http://s-01ABC-3000.preview.ex.sslip.io:18080"},
		{"http default 80 omitted", false, "80", "http://s-01ABC-3000.preview.ex.sslip.io"},
		{"http empty omitted", false, "", "http://s-01ABC-3000.preview.ex.sslip.io"},
		{"https default 443 omitted", true, "443", "https://s-01ABC-3000.preview.ex.sslip.io"},
		{"https custom port", true, "18443", "https://s-01ABC-3000.preview.ex.sslip.io:18443"},
		{"https empty omitted", true, "", "https://s-01ABC-3000.preview.ex.sslip.io"},
		// A bare 80 must NOT be appended even under https, and 443 not under http.
		{"http 443 appended (non-default for http)", false, "443", "http://s-01ABC-3000.preview.ex.sslip.io:443"},
	}
	for _, c := range cases {
		s := &Server{PreviewDomain: "ex.sslip.io", PreviewTLS: c.tls, PublicHTTPPort: c.port}
		if got := s.previewURL(id, 3000); got != c.want {
			t.Errorf("%s: previewURL = %q; want %q", c.name, got, c.want)
		}
	}
}

// A1.5a: the preview hostname uses the RESOLVED web port (not a constant 3000),
// so a non-3000 app (e.g. Astro on 4321) gets a reachable URL; 0 falls back to 3000.
func TestPreviewURLResolvedPort(t *testing.T) {
	s := &Server{PreviewDomain: "ex.sslip.io"}
	if got := s.previewURL("01ABC", 4321); got != "http://s-01ABC-4321.preview.ex.sslip.io" {
		t.Errorf("4321: got %q", got)
	}
	if got := s.previewURL("01ABC", 0); got != "http://s-01ABC-3000.preview.ex.sslip.io" {
		t.Errorf("0->3000 fallback: got %q", got)
	}
	if got := s.previewURL("01ABC", 8080); got != "http://s-01ABC-8080.preview.ex.sslip.io" {
		t.Errorf("8080: got %q", got)
	}
}
