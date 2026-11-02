package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestAppLabels(t *testing.T) {
	s := newPresetTestServer(t)
	create := func(body string) (int, v1App) {
		w := presetReq(s, "POST", "/v1/apps", body, cfgTenant, nil, s.v1CreateApp)
		var a v1App
		_ = json.Unmarshal(w.Body.Bytes(), &a)
		return w.Code, a
	}
	code, live := create(`{"name":"shop","labels":{"env":"live","source":"01LOCAL"}}`)
	if code != http.StatusCreated || live.Labels["env"] != "live" || live.Labels["source"] != "01LOCAL" {
		t.Fatalf("create with labels: %d %+v", code, live)
	}
	if code, plain := create(`{"name":"notes"}`); code != http.StatusCreated || plain.Labels == nil || len(plain.Labels) != 0 {
		t.Fatalf("no labels must read as {}: %d %+v", code, plain)
	}
	for _, bad := range []string{`{"name":"x","labels":{"Env":"live"}}`, `{"name":"x","labels":{"":"v"}}`, `{"name":"x","labels":{"env=x":"v"}}`} {
		if code, _ := create(bad); code != http.StatusBadRequest {
			t.Errorf("%s: HTTP %d", bad, code)
		}
	}

	list := func(q string) []v1App {
		w := presetReq(s, "GET", "/v1/apps"+q, "", cfgTenant, nil, s.v1ListApps)
		var out struct{ Apps []v1App }
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return out.Apps
	}
	if got := list("?label=env=live"); len(got) != 1 || got[0].ID != live.ID {
		t.Errorf("filter env=live: %+v", got)
	}
	if got := list("?label=env=live&label=source=other"); len(got) != 0 {
		t.Errorf("all filters must match: %+v", got)
	}
	if got := list(""); len(got) != 2 {
		t.Errorf("unfiltered: %d apps", len(got))
	}
	if w := presetReq(s, "GET", "/v1/apps?label=nokey", "", cfgTenant, nil, s.v1ListApps); w.Code != http.StatusBadRequest {
		t.Errorf("bad filter: HTTP %d", w.Code)
	}
	if got := list("?label=env=live"); len(got) != 1 {
		t.Fatal("setup")
	}
	// Another tenant never sees them.
	w := presetReq(s, "GET", "/v1/apps?label=env=live", "", "someone-else", nil, s.v1ListApps)
	var other struct{ Apps []v1App }
	_ = json.Unmarshal(w.Body.Bytes(), &other)
	if len(other.Apps) != 0 {
		t.Errorf("cross-tenant leak: %+v", other.Apps)
	}

	w = presetReq(s, "PATCH", "/v1/apps/"+live.ID, `{"labels":{"env":"test"}}`, cfgTenant, map[string]string{"id": live.ID}, s.v1PatchApp)
	var patched v1App
	_ = json.Unmarshal(w.Body.Bytes(), &patched)
	if w.Code != http.StatusOK || patched.Labels["env"] != "test" || patched.Labels["source"] != "" || patched.Name != "shop" {
		t.Errorf("patch replaces labels: %d %+v", w.Code, patched)
	}
}
