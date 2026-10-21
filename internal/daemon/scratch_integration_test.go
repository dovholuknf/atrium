//go:build integration

package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/scratch"
	"github.com/dovholuknf/atrium/internal/store"
)

func scratchCard(t *testing.T, d *Daemon, wire, dir string) *store.Task {
	t.Helper()
	task, _, err := d.st.Register(store.Observed{WireName: wire, Worktree: filepath.ToSlash(dir), Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	return task
}

func TestScratchRootSetsTheFolderTheEnvAndTheLine(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	root := filepath.Join(t.TempDir(), "tmp")
	if err := d.st.SetSetting(store.SettingScratchRoot, root); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	task := scratchCard(t, d, "hot-loop", home)

	dir := d.scratchFor(task, home)
	want := scratch.Dir(root, task.DisplayTitle(), task.WireName, task.ID)
	if dir != want {
		t.Fatalf("scratchFor = %q, want %q", dir, want)
	}
	if fi, err := os.Stat(filepath.FromSlash(dir)); err != nil || !fi.IsDir() {
		t.Fatalf("the folder was not made: %v", err)
	}
	if row, ok, _ := d.st.CardScratchOf(task.ID); !ok || row.Path != dir {
		t.Fatalf("no ledger row for the folder: %+v %v", row, ok)
	}
	line := d.scratchLine(task)
	if !strings.Contains(line, "go in "+dir+".") || !strings.Contains(line, "Never C:\\ or the system temp directory.") {
		t.Fatalf("line = %q", line)
	}
	if again := d.scratchFor(task, home); again != dir {
		t.Fatalf("a second call named %q, want the same folder %q", again, dir)
	}
}

func TestUnsetScratchRootFallsBackToTheCardHome(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	home := t.TempDir()
	task := scratchCard(t, d, "plain", home)

	if got := d.scratchFor(task, home); got != filepath.ToSlash(home) {
		t.Fatalf("scratchFor = %q, want the card home %q", got, filepath.ToSlash(home))
	}
	if !strings.Contains(d.scratchLine(task), filepath.ToSlash(home)) {
		t.Fatalf("line = %q, want the card home", d.scratchLine(task))
	}
	if _, ok, _ := d.st.CardScratchOf(task.ID); ok {
		t.Fatal("a ledger row was made with no scratch root")
	}
}

func TestAJoinedCardIsToldWhereScratchIs(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	root := filepath.Join(t.TempDir(), "tmp")
	if err := d.st.SetSetting(store.SettingScratchRoot, root); err != nil {
		t.Fatal(err)
	}
	for _, event := range []string{"start", "join"} {
		body, _ := json.Marshal(SessionEvent{Agent: "joiner-" + event, Event: event, Cwd: t.TempDir(), PID: 4242})
		rec := httptest.NewRecorder()
		d.handleSession(rec, httptest.NewRequest(http.MethodPost, "/session", strings.NewReader(string(body))))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status %d: %s", event, rec.Code, rec.Body)
		}
		var got struct {
			OK      bool   `json:"ok"`
			Context string `json:"context"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || !got.OK {
			t.Fatalf("%s: answer %q: %v", event, rec.Body, err)
		}
		if !strings.Contains(got.Context, filepath.ToSlash(root)) {
			t.Fatalf("%s: context %q does not name the scratch root %q", event, got.Context, root)
		}
	}
}

func TestCullOfAWorkerRemovesItsScratchFolder(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	root := filepath.Join(t.TempDir(), "tmp")
	if err := d.st.SetSetting(store.SettingScratchRoot, root); err != nil {
		t.Fatal(err)
	}
	r := newCullRepo(t, true)
	task := cullCard(t, d, r.wt, OriginAgentTag, SubagentTag)
	dir := d.scratchFor(task, r.wt)
	if err := os.WriteFile(filepath.Join(filepath.FromSlash(dir), "notes.md"), []byte("n"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := d.Cull(task.ID, "")
	if err != nil {
		t.Fatalf("cull: %v", err)
	}
	if !res.ScratchDirRemoved {
		t.Fatalf("result = %+v, want the scratch folder removed", res)
	}
	if _, err := os.Stat(filepath.FromSlash(dir)); !os.IsNotExist(err) {
		t.Fatalf("the scratch folder is still there: %v", err)
	}
}

func TestCullRemovesOnlyAScratchFolderAtriumMade(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	root := filepath.Join(t.TempDir(), "tmp")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetSetting(store.SettingScratchRoot, root); err != nil {
		t.Fatal(err)
	}
	made := scratchCard(t, d, "made", t.TempDir())
	madeDir := d.scratchFor(made, "")
	if err := os.WriteFile(filepath.Join(filepath.FromSlash(madeDir), "oidc.patch"), []byte("diff"), 0o644); err != nil {
		t.Fatal(err)
	}

	theirs := scratchCard(t, d, "theirs", t.TempDir())
	theirsDir := scratch.Dir(root, theirs.DisplayTitle(), theirs.WireName, theirs.ID)
	if err := os.MkdirAll(filepath.FromSlash(theirsDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := d.scratchFor(theirs, ""); got != theirsDir {
		t.Fatalf("scratchFor = %q, want the existing folder %q", got, theirsDir)
	}

	if !d.removeScratch(made) {
		t.Fatal("the folder atrium made was not removed")
	}
	if _, err := os.Stat(filepath.FromSlash(madeDir)); !os.IsNotExist(err) {
		t.Fatalf("the folder atrium made is still there: %v", err)
	}
	if d.removeScratch(theirs) {
		t.Fatal("a folder atrium did not make was removed")
	}
	if _, err := os.Stat(filepath.FromSlash(theirsDir)); err != nil {
		t.Fatalf("a folder atrium did not make is gone: %v", err)
	}
}
