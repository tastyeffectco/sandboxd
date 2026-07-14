package telemetry

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// defaultReleasesURL is the GitHub API endpoint for the latest published release.
const defaultReleasesURL = "https://api.github.com/repos/tastyeffectco/sandboxd/releases/latest"

// defaultCheckTTL is how long a fetched "latest release" result is trusted before
// Latest will fetch again.
const defaultCheckTTL = 6 * time.Hour

// CompareSemver compares two semantic versions and returns -1, 0, or 1 for
// a<b, a==b, a>b. A leading "v" is ignored, and pre-release/build metadata
// (anything after "-" or "+") is stripped. Missing or non-numeric components are
// treated as 0, so malformed input compares as "0.0.0" rather than erroring.
func CompareSemver(a, b string) int {
	av, bv := parseSemver(a), parseSemver(b)
	for i := 0; i < 3; i++ {
		switch {
		case av[i] < bv[i]:
			return -1
		case av[i] > bv[i]:
			return 1
		}
	}
	return 0
}

func parseSemver(v string) [3]int {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	v = strings.TrimPrefix(v, "V")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	var out [3]int
	for i, part := range strings.SplitN(v, ".", 3) {
		if i >= 3 {
			break
		}
		if n, err := strconv.Atoi(strings.TrimSpace(part)); err == nil && n > 0 {
			out[i] = n
		}
	}
	return out
}

// Checker fetches the latest published release (cached ~6h) and answers whether
// a newer version than the running build exists. It is best-effort: on any fetch
// error UpdateAvailable simply reports false. It is safe for concurrent use.
type Checker struct {
	// URL overrides the GitHub releases endpoint (tests point this elsewhere).
	URL string
	// Fetch overrides the network fetch entirely (used by tests). It returns the
	// release tag and its html_url.
	Fetch func(ctx context.Context) (tag, url string, err error)
	// TTL overrides the cache lifetime; zero means the 6h default.
	TTL time.Duration
	// Now overrides the clock (tests); zero-value means time.Now.
	Now func() time.Time

	mu        sync.Mutex
	haveData  bool
	tag       string
	url       string
	lastFetch time.Time
}

func (c *Checker) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c *Checker) ttl() time.Duration {
	if c.TTL > 0 {
		return c.TTL
	}
	return defaultCheckTTL
}

// Latest returns the latest release tag and its changelog/release URL, fetching
// from GitHub at most once per TTL. Callers run this from a background goroutine;
// the request-path UpdateAvailable never fetches.
func (c *Checker) Latest(ctx context.Context) (version, url string, err error) {
	c.mu.Lock()
	if c.haveData && c.now().Sub(c.lastFetch) < c.ttl() {
		v, u := c.tag, c.url
		c.mu.Unlock()
		return v, u, nil
	}
	c.mu.Unlock()

	fetch := c.Fetch
	if fetch == nil {
		fetch = c.githubFetch
	}
	tag, u, ferr := fetch(ctx)
	if ferr != nil {
		return "", "", ferr
	}

	c.mu.Lock()
	c.tag, c.url, c.haveData, c.lastFetch = tag, u, true, c.now()
	c.mu.Unlock()
	return tag, u, nil
}

// UpdateAvailable reports, from the cached result only (never the network),
// whether the latest release is newer than current. It is best-effort: with no
// cached result yet it returns (false, "", ""). When a result is cached it always
// returns the latest version + changelog URL so callers can surface them.
func (c *Checker) UpdateAvailable(current string) (available bool, latest, changelogURL string) {
	c.mu.Lock()
	have, tag, url := c.haveData, c.tag, c.url
	c.mu.Unlock()
	if !have || tag == "" {
		return false, "", ""
	}
	return CompareSemver(tag, current) > 0, tag, url
}

func (c *Checker) githubFetch(ctx context.Context) (tag, url string, err error) {
	endpoint := c.URL
	if endpoint == "" {
		endpoint = defaultReleasesURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	var body struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return "", "", err
	}
	return body.TagName, body.HTMLURL, nil
}
