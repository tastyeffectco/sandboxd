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
		if got := s.previewURL(id); got != c.want {
			t.Errorf("%s: previewURL = %q; want %q", c.name, got, c.want)
		}
	}
}
