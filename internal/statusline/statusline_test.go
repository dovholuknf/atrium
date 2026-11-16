package statusline

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

func TestRenderPostsToTheRoomOncePerTwelveSeconds(t *testing.T) {
	got := make(chan string, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got <- r.Method + " " + r.URL.Path + " " + r.Header.Get("Content-Type") + " " + string(b)
	}))
	defer srv.Close()
	home := t.TempDir()
	t.Setenv("ATRIUM_HUB_URL", srv.URL)
	t.Setenv("ATRIUM_TASK_ID", "card-1")
	spawn := func(url string, body []byte) { Post(url, string(body)) }
	now := time.Now()

	t.Setenv("ATRIUM_PERM_GATE", "off")
	Render(payloadFor("sess-off"), home, now, spawn)
	if _, err := os.Stat(filepath.Join(home, ".claude", "usage-log.state", "sess-off.post")); err == nil {
		t.Fatal("ATRIUM_PERM_GATE=off still posted")
	}

	t.Setenv("ATRIUM_PERM_GATE", "on")
	line := Render(payloadFor("sess-1"), home, now, spawn)
	if !strings.Contains(line, " wk ") || !strings.Contains(line, "/x") {
		t.Fatalf("the line lost its segments: %q", line)
	}
	select {
	case g := <-got:
		if want := "POST /telemetry application/json " + wantBody; g != want {
			t.Fatalf("post\n got  %s\n want %s", g, want)
		}
	default:
		t.Fatal("no telemetry post reached the room")
	}

	Render(payloadFor("sess-1"), home, now.Add(5*time.Second), spawn)
	select {
	case g := <-got:
		t.Fatalf("posted again inside 12 seconds: %s", g)
	default:
	}
}

func TestADeadRoomDoesNotBlockTheDraw(t *testing.T) {
	t.Setenv("ATRIUM_HUB_URL", "http://127.0.0.1:1")
	t.Setenv("ATRIUM_PERM_GATE", "on")
	start := time.Now()
	Render(payloadFor("sess-dead"), t.TempDir(), start, func(string, []byte) {})
	if d := time.Since(start); d > 500*time.Millisecond {
		t.Fatalf("Render took %v", d)
	}
}
