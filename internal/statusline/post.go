package statusline

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/base/addrfile"
)

// The post goes out at most once every 12 seconds per session, from a detached copy of atrium with a one second cap.

type telLimit struct {
	Pct      int64   `json:"pct"`
	ResetsAt *string `json:"resets_at,omitempty"`
}

// telemetry is the body the room reads. Percents are floored because the room reads ints, an epoch reset is RFC 3339
// because that is what it parses. Empty fields are left out.
type telemetry struct {
	TaskID     string    `json:"task_id,omitempty"`
	SessionID  string    `json:"session_id,omitempty"`
	ContextPct *int64    `json:"context_pct,omitempty"`
	FiveHour   *telLimit `json:"five_hour,omitempty"`
	Weekly     *telLimit `json:"weekly,omitempty"`
}

// number is the raw value when it is a JSON number.
func number(raw json.RawMessage) (float64, bool) {
	s := bytes.TrimSpace(raw)
	if len(s) == 0 || !(s[0] == '-' || s[0] >= '0' && s[0] <= '9') {
		return 0, false
	}
	var f float64
	return f, json.Unmarshal(s, &f) == nil
}

func telLim(l limit) *telLimit {
	f, ok := number(l.UsedPercentage)
	if !ok {
		return nil
	}
	t := &telLimit{Pct: int64(math.Floor(f))}
	if at, ok := number(l.ResetsAt); ok {
		s := time.Unix(int64(math.Floor(at)), 0).UTC().Format("2006-01-02T15:04:05Z")
		t.ResetsAt = &s
	} else if s, ok := strBody(l.ResetsAt); ok {
		t.ResetsAt = &s
	}
	return t
}

func strBody(raw json.RawMessage) (string, bool) {
	var s string
	return s, json.Unmarshal(raw, &s) == nil
}

// telemetryBody returns the body to post, or nil when the payload carries no percent at all.
func telemetryBody(p *payload, taskID string) []byte {
	t := telemetry{TaskID: taskID, SessionID: str(p.SessionID),
		FiveHour: telLim(p.RateLimits.FiveHour), Weekly: telLim(p.RateLimits.SevenDay)}
	if f, ok := number(p.ContextWindow.UsedPercentage); ok {
		c := int64(math.Floor(f))
		t.ContextPct = &c
	}
	if t.ContextPct == nil && t.FiveHour == nil && t.Weekly == nil {
		return nil
	}
	body, err := json.Marshal(t)
	if err != nil {
		return nil
	}
	return body
}

// postTelemetry stamps the throttle and starts the detached post. ATRIUM_PERM_GATE=off is a terminal outside atrium.
func postTelemetry(home, key string, p *payload, now time.Time, spawn func(url string, body []byte)) {
	if os.Getenv("ATRIUM_PERM_GATE") == "off" || key == "" {
		return
	}
	body := telemetryBody(p, os.Getenv("ATRIUM_TASK_ID"))
	if body == nil {
		return
	}
	dir := filepath.Join(home, ".claude", "usage-log.state")
	stamp := filepath.Join(dir, key+".post")
	if lts := field(readState(stamp), 0); intRe.MatchString(lts) && now.Unix()-atoi(lts) < 12 {
		return
	}
	if os.MkdirAll(dir, 0o755) != nil || os.WriteFile(stamp, fmt.Appendf(nil, "%d\n", now.Unix()), 0o644) != nil {
		return
	}
	hub := os.Getenv("ATRIUM_HUB_URL")
	if hub == "" {
		// Another user's room may own the default port, so no address file means no post.
		loc, ok := addrfile.ReadLocation()
		if !ok || strings.TrimSpace(loc.Agent) == "" {
			return
		}
		hub = strings.TrimRight(loc.Agent, "/")
	}
	spawn(hub+"/telemetry", body)
}

// Post is the detached copy: one POST, one second cap, nothing printed.
func Post(url, body string) {
	c := http.Client{Timeout: time.Second}
	if res, err := c.Post(url, "application/json", strings.NewReader(body)); err == nil {
		res.Body.Close()
	}
}
