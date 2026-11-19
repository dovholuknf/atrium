package statusline

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

const wantBody = `{"task_id":"card-1","session_id":"sess-1","context_pct":12,` +
	`"five_hour":{"pct":0,"resets_at":"2026-11-16T23:00:00Z"},"weekly":{"pct":1,"resets_at":"2026-11-22T04:00:00Z"}}`

func payloadFor(sid string) []byte {
	return []byte(`{"session_id":"` + sid + `","cwd":"/x","context_window":{"used_percentage":12.7},
	  "rate_limits":{"five_hour":{"used_percentage":0,"resets_at":1794870000},"seven_day":{"used_percentage":1.6,"resets_at":1795320000}}}`)
}

func TestTelemetryBody(t *testing.T) {
	var p payload
	unmarshal(payloadFor("sess-1"), &p)
	if got := string(telemetryBody(&p, "card-1")); got != wantBody {
		t.Fatalf("body\n got  %s\n want %s", got, wantBody)
	}
	var q payload
	unmarshal([]byte(`{"session_id":"s","rate_limits":{"five_hour":{"used_percentage":"x"}}}`), &q)
	if b := telemetryBody(&q, ""); b != nil {
		t.Fatalf("a payload with no percent still posts %s", b)
	}
}

func TestTelemetryWithNoAddressFilePostsNowhere(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("ATRIUM_HUB_URL", "")
	t.Setenv("ATRIUM_PERM_GATE", "on")
	t.Setenv("ATRIUM_LOCATION", filepath.Join(home, "none.json"))
	t.Setenv("ATRIUM_SHARED_LOCATION", "-")
	var urls []string
	spawn := func(url string, _ []byte) { urls = append(urls, url) }

	line := Render(payloadFor("sess-1"), home, time.Now(), spawn)
	if !strings.Contains(line, " wk ") {
		t.Fatalf("the line lost its segments: %q", line)
	}
	if len(urls) != 0 {
		t.Fatalf("posted to %v with no address file", urls)
	}

	loc := filepath.Join(home, "daemon.json")
	if err := os.WriteFile(loc, []byte(`{"agent":"http://127.0.0.1:1/"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ATRIUM_LOCATION", loc)
	Render(payloadFor("sess-2"), home, time.Now(), spawn)
	if want := []string{"http://127.0.0.1:1/telemetry"}; !slices.Equal(urls, want) {
		t.Fatalf("posted to %v, want %v", urls, want)
	}
}
