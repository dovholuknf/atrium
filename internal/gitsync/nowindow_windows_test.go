//go:build windows

package gitsync

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/dovholuknf/atrium/internal/nowindow"
)

func hasNoWindow(cmd *exec.Cmd) bool {
	a := cmd.SysProcAttr
	return a != nil && a.HideWindow && a.CreationFlags&nowindow.CreateNoWindow != 0
}

// The hub and the rooms have no console, so a git child without CREATE_NO_WINDOW opens one
// on the operator's desktop every time gitsync runs it.
func TestGitCommandHasNoWindow(t *testing.T) {
	cmd := gitCommand(context.Background(), t.TempDir(), nil, []string{"version"})
	if !hasNoWindow(cmd) {
		t.Fatalf("git child would open a console window: SysProcAttr = %+v", cmd.SysProcAttr)
	}
}

// `git http-backend` runs for every fetch the hub and a room serve, so it is the flash seen most. A real
// `git ls-remote` and `git clone` go through ServeHTTP, and every http-backend it started is checked.
func TestHTTPBackendServesAFetchWithNoWindow(t *testing.T) {
	exe, err := exec.LookPath("git")
	if err != nil {
		t.Skip("no git")
	}
	root := t.TempDir()
	src, bare := filepath.Join(root, "src"), filepath.Join(root, "r.git")
	git := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git(root, "init", "-q", "-b", "main", src)
	git(src, "-c", "user.email=t@x", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", "one")
	sha := git(src, "rev-parse", "HEAD")
	git(root, "clone", "-q", "--bare", src, bare)

	var mu sync.Mutex
	var started []*exec.Cmd
	cgiStarted = func(cmd *exec.Cmd) { mu.Lock(); started = append(started, cmd); mu.Unlock() }
	t.Cleanup(func() { cgiStarted = nil })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serveCGI(gitCGI(exe, bare, [][2]string{{"http.receivepack", "false"}}, cgiEnv()), w, r)
	}))
	defer srv.Close()

	if got := git(root, "ls-remote", srv.URL+"/r.git", "refs/heads/main"); !strings.HasPrefix(got, sha) {
		t.Fatalf("ls-remote through the CGI = %q, want %s", got, sha)
	}
	git(root, "clone", "-q", srv.URL+"/r.git", filepath.Join(root, "clone"))
	if got := git(filepath.Join(root, "clone"), "rev-parse", "HEAD"); got != sha {
		t.Fatalf("clone through the CGI is at %s, want %s", got, sha)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(started) < 2 {
		t.Fatalf("the CGI started %d http-backend children, want one per request", len(started))
	}
	for _, cmd := range started {
		if !hasNoWindow(cmd) {
			t.Fatalf("http-backend %v would open a console window: SysProcAttr = %+v", cmd.Args, cmd.SysProcAttr)
		}
	}
}
