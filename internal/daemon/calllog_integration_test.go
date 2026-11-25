//go:build integration

package daemon

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/base/calllog"
)

func TestAgentCallsAreLoggedBeforeAndAfterTheHandler(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "room-calls.log")
	var inside string
	lg := calllog.New(dir, "room-calls.log", 0)
	t.Cleanup(lg.Close)
	h := logged(lg, func() string { return "sg4" },
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			b, _ := os.ReadFile(p)
			inside = string(b)
			w.WriteHeader(http.StatusInternalServerError)
		}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/gate?agent=card-1", nil))
	if !strings.Contains(inside, `"event":"start"`) || !strings.Contains(inside, `"name":"POST /gate"`) ||
		!strings.Contains(inside, `"caller":"card-1"`) || !strings.Contains(inside, `"room":"sg4"`) ||
		strings.Contains(inside, `"event":"end"`) {
		t.Fatalf("log while the handler ran = %q", inside)
	}
	lg.Begin("x", "x", "", "")(calllog.OK, nil)
	body := httptest.NewRequest("POST", "/session", strings.NewReader(`{"agent":"card-2"}`))
	var seen string
	logged(lg, func() string { return "" }, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bb, _ := io.ReadAll(r.Body)
		seen = string(bb)
	})).ServeHTTP(httptest.NewRecorder(), body)
	b, _ := os.ReadFile(p)
	if !strings.Contains(string(b), `"caller":"card-2"`) || seen != `{"agent":"card-2"}` {
		t.Fatalf("body caller: log %q, handler saw %q", b, seen)
	}
	if !strings.Contains(string(b), `"event":"end"`) || !strings.Contains(string(b), `"outcome":"error"`) {
		t.Fatalf("log after = %q", b)
	}
}
