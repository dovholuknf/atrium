package link

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"sync"
	"time"
)

// The newest atrium release on GitHub, for the rooms menu's update warning.
//
//	GET /_hub/release   {"latest": "v1.8.0", "url": "...", "state": "ok", "hub": "1.7.4", "checked_at": "..."}
//
// ONE ASK A DAY, BY THE HUB ONLY. Rooms dial out to the hub and never to GitHub, so a room's version is the one it
// already reported when it attached and nothing here opens a port or a request on a room. No token: the releases
// endpoint is public. A failed ask is quiet, the board draws neutral markers and the last good answer's time.
// ATRIUM_NO_UPDATE_CHECK=1 turns the ask off.
const (
	releaseTTL   = 24 * time.Hour
	releaseRetry = time.Hour
	releaseURL   = "https://api.github.com/repos/dovholuknf/atrium/releases/latest"
)

// ReleaseCheck holds what the hub last read.
type ReleaseCheck struct {
	hub   string
	fetch func(ctx context.Context) (tag, page string, err error)

	mu       sync.Mutex
	latest   string
	page     string
	okAt     time.Time
	triedAt  time.Time
	failed   bool
	inflight bool
}

// NewReleaseCheck is the check for a hub running hubVersion.
func NewReleaseCheck(hubVersion string) *ReleaseCheck {
	return &ReleaseCheck{hub: hubVersion, fetch: githubLatest}
}

func githubLatest(ctx context.Context) (string, string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, releaseURL, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "atrium")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", "", &statusError{res.StatusCode}
	}
	var out struct {
		Tag  string `json:"tag_name"`
		Page string `json:"html_url"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil || out.Tag == "" {
		return "", "", err
	}
	return out.Tag, out.Page, nil
}

type statusError struct{ code int }

func (e *statusError) Error() string { return http.StatusText(e.code) }

// view is the answer, and starts a read in the background when the last one is due.
func (c *ReleaseCheck) view(now time.Time) map[string]any {
	c.mu.Lock()
	defer c.mu.Unlock()
	wait := releaseTTL
	if c.failed {
		wait = releaseRetry
	}
	if os.Getenv("ATRIUM_NO_UPDATE_CHECK") == "" && !c.inflight && (c.triedAt.IsZero() || now.Sub(c.triedAt) >= wait) {
		c.inflight = true
		go c.read()
	}
	state := "ok"
	switch {
	case c.inflight && c.latest == "":
		state = "checking"
	case c.latest == "" || c.failed:
		state = "unknown"
	}
	out := map[string]any{"latest": c.latest, "url": c.page, "state": state, "hub": c.hub}
	if !c.okAt.IsZero() {
		out["checked_at"] = c.okAt.UTC().Format(time.RFC3339)
	}
	return out
}

func (c *ReleaseCheck) read() {
	tag, page, err := c.fetch(context.Background())
	c.mu.Lock()
	defer c.mu.Unlock()
	c.inflight, c.triedAt = false, time.Now()
	if err != nil {
		c.failed = true
		return
	}
	c.latest, c.page, c.okAt, c.failed = tag, page, c.triedAt, false
}

// SetRelease wires the check. Without it `/_hub/release` is a 404 and the board shows no versions.
func (p *Proxy) SetRelease(c *ReleaseCheck) { p.release = c }

func (p *Proxy) serveRelease(w http.ResponseWriter, r *http.Request) {
	if p.release == nil {
		http.NotFound(w, r)
		return
	}
	_ = json.NewEncoder(w).Encode(p.release.view(time.Now()))
}
