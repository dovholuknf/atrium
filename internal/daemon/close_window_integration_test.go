//go:build integration

package daemon

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/base/testwait"
	"github.com/dovholuknf/atrium/internal/daemon/shared"
	"github.com/dovholuknf/atrium/internal/store"
)

func TestCloseStopsAWindowModeResidentAndRemovesItsWorktree(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: a real runner, git worktree and close")
	}
	t.Parallel()
	d := testDaemon(t)
	d.hk.terminal = ownConsole
	repo := homeRepo(t)
	wt := filepath.Join(t.TempDir(), "wt")
	cullGit(t, repo, "worktree", "add", "-q", "-b", "claude/win", wt)

	pidFile := filepath.Join(t.TempDir(), "pid")
	cmd, args := "sh", []string{"-c", "echo $$ > '" + filepath.ToSlash(pidFile) + "'; exec sleep 600"}
	if runtime.GOOS == "windows" {
		cmd, args = "powershell.exe", []string{"-NoProfile", "-NonInteractive", "-Command",
			"Set-Content -Path '" + pidFile + "' -Value $PID; Start-Sleep 600"}
	}
	home := t.TempDir()
	if _, err := d.st.SaveHarness(store.Harness{ID: "shelltest", Label: "shell test", Enabled: true,
		Cmd: cmd, Args: args, LaunchMode: store.LaunchWindow, Env: map[string]string{"HOME": home,
			"USERPROFILE": home, "ATRIUM_HUB_URL": "http://127.0.0.1:1", "ATRIUM_BOARD_URL": "http://127.0.0.1:1"}}); err != nil {
		t.Fatal(err)
	}
	task, err := d.Launch(shared.LaunchRequest{Harness: "shelltest", Cwd: wt, Tags: []string{ResidentTag}})
	if err != nil {
		t.Skipf("could not spawn a test runner on this machine: %v", err)
	}
	pid := 0
	testwait.Eventually(t, 30*time.Second, "the runner to write its pid", func() bool {
		raw, _ := os.ReadFile(pidFile)
		pid, _ = strconv.Atoi(strings.TrimSpace(string(raw)))
		return pid > 0
	})
	t.Cleanup(func() { killTree(pid) })

	if err := d.st.SetResumeID(task.ID, "old"); err != nil {
		t.Fatal(err)
	}
	if err := d.onSession(SessionEvent{Agent: task.WireName, TaskID: task.ID, Runner: "shelltest", Cwd: wt, PID: pid,
		Resume: "new"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := d.st.Register(store.Observed{WireName: task.WireName, Worktree: task.Worktree,
		Runner: "shelltest", Resume: "new"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := d.st.Get(task.ID); got.PID != 0 {
		t.Fatalf("the card still has pid %d, so the test proves nothing", got.PID)
	}
	if _, err := d.st.AddResource(task.ID, store.ResWorktree, filepath.ToSlash(wt), filepath.ToSlash(repo)); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(d.ap.Handler())
	t.Cleanup(srv.Close)
	resp, err := http.Post(srv.URL+"/v1/tasks/"+task.ID+"/close", "application/json",
		strings.NewReader(`{"confirm":true,"answers":{"1":"delete"}}`))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("close answered %d: %s", resp.StatusCode, body)
	}
	if processAlive(pid) {
		t.Errorf("the runner %d still runs after the close: %s", pid, body)
	}
	if _, err := os.Stat(wt); err == nil {
		t.Errorf("the worktree is still on disk: %s", body)
	}
}
