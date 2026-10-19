//go:build integration

package cli

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func eventScripts(t *testing.T, hub string) map[string]*exec.Cmd {
	t.Helper()
	dir, err := filepath.Abs("../../scripts")
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]*exec.Cmd{}
	if bash, err := exec.LookPath("bash"); err == nil {
		if _, err := exec.LookPath("curl"); err == nil {
			out["sh"] = exec.Command(bash, filepath.Join(dir, "atrium-event.sh"), "--kind", "land", "--repo", "o/r", "--branch", "main",
				"--outcome", "pushed", "--from", "a", "--to", "b", "--commits", "2", "--hub-url", hub)
		}
	}
	for _, ps := range []string{"pwsh", "powershell"} {
		if p, err := exec.LookPath(ps); err == nil {
			out["ps1"] = exec.Command(p, "-NoProfile", "-File", filepath.Join(dir, "atrium-event.ps1"), "-Kind", "land", "-Repo", "o/r",
				"-Branch", "main", "-Outcome", "pushed", "-From", "a", "-To", "b", "-Commits", "2", "-HubUrl", hub)
			break
		}
	}
	return out
}

func TestTheEventScriptsPostOneEventAndPrintOneLine(t *testing.T) {
	var mu sync.Mutex
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 1024)
		n, _ := r.Body.Read(buf)
		mu.Lock()
		bodies = append(bodies, r.URL.Path+" "+string(buf[:n]))
		mu.Unlock()
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"event":"e1","subscribers":2}`))
	}))
	defer srv.Close()
	for name, cmd := range eventScripts(t, srv.URL) {
		out, err := cmd.Output()
		if err != nil || strings.TrimSpace(string(out)) != "event: 2 subscribers" {
			t.Fatalf("%s: %q %v", name, out, err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	for _, b := range bodies {
		if !strings.HasPrefix(b, "/_hub/events ") || !strings.Contains(b, `"outcome":"pushed"`) || !strings.Contains(b, `"commits":2`) {
			t.Fatalf("body %q", b)
		}
	}
}

func TestTheEventScriptsWarnOnceAndExitZeroWhenNothingListens(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	for name, cmd := range eventScripts(t, url) {
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s exited non-zero: %v", name, err)
		}
		if lines := strings.Split(strings.TrimSpace(string(out)), "\n"); len(lines) != 1 || !strings.Contains(lines[0], "atrium-event") {
			t.Fatalf("%s: %q", name, out)
		}
	}
}

func TestSubscribeWithoutACardRefuses(t *testing.T) {
	t.Setenv("ATRIUM_TASK_ID", "")
	_, err := subscribeCall(context.Background(), SubscribeInput{Action: "subscribe", Kind: "land"})
	if err == nil || !strings.Contains(err.Error(), "ATRIUM_TASK_ID") {
		t.Fatalf("err %v", err)
	}
	if _, err := subscribeCall(context.Background(), SubscribeInput{Action: "nonsense"}); err == nil {
		t.Fatal("an unknown action was taken")
	}
}
