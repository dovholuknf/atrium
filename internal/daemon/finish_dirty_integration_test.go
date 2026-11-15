//go:build integration

package daemon

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A done is accepted whatever the worktree holds, and the card and the launcher's notice say what was left uncommitted.
func TestADoneSaysWhatItLeftUncommitted(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name  string
		make  func(dir string)
		files int
		note  string
	}{
		{"clean", func(string) {}, 0, ""},
		{"modified", func(dir string) { putFile(t, dir, "tracked.go", "changed") }, 1, "1 file not committed (tracked.go)"},
		{"untracked", func(dir string) { putFile(t, dir, "new/a.go", "x"); putFile(t, dir, "new/b.go", "y") }, 2,
			"2 files not committed (new/a.go, new/b.go)"},
		{"brief only", func(dir string) { putFile(t, dir, "BRIEF.md", "the brief") }, 0, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			d := testDaemon(t)
			launcher, worker := launchedPair(t, d)
			dir := t.TempDir()
			gitRepo(t, dir)
			putFile(t, dir, "tracked.go", "first")
			for _, a := range [][]string{{"add", "tracked.go"}, {"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "y"}} {
				if out, err := exec.Command("git", append([]string{"-C", dir}, a...)...).CombinedOutput(); err != nil {
					t.Fatalf("git: %v %s", err, out)
				}
			}
			if err := d.st.SetWorktree(worker.ID, dir); err != nil {
				t.Fatal(err)
			}
			c.make(dir)
			d.hk.commitExists = func(string, string) bool { return true }

			rec, _ := finishWith(t, d, FinishRequest{Agent: "worker", Status: ReportDone, SHA: "abc1234"})
			if rec.Code != http.StatusOK {
				t.Fatalf("answered %d: %s", rec.Code, rec.Body)
			}
			got, _ := d.st.Get(worker.ID)
			if got.ReportDirty != c.files || got.ReportSHA != "abc1234" {
				t.Fatalf("card: dirty %d sha %q, want %d", got.ReportDirty, got.ReportSHA, c.files)
			}
			evs, err := d.st.Events(worker.ID, 0)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, e := range evs {
				var p struct {
					Kind  string `json:"kind"`
					Dirty *bool  `json:"dirty"`
					Files int    `json:"dirty_files"`
				}
				if json.Unmarshal(e.Payload, &p) == nil && p.Kind == "finished" && p.Dirty != nil {
					found = *p.Dirty == (c.files > 0) && p.Files == c.files
				}
			}
			if !found {
				t.Fatalf("no finished event with dirty_files %d in %+v", c.files, evs)
			}
			msgs := pendingFrom(t, d, launcher.ID)
			if len(msgs) != 1 {
				t.Fatalf("launcher has %v", msgs)
			}
			if c.note == "" && strings.Contains(msgs[0].Text, "not committed") || !strings.Contains(msgs[0].Text, c.note) {
				t.Fatalf("notice %q, want %q", msgs[0].Text, c.note)
			}
		})
	}
}

func TestDirtyNoteNamesFive(t *testing.T) {
	t.Parallel()
	got := dirtyNote([]string{"a", "b", "c", "d", "e", "f", "g"})
	if want := "7 files not committed (a, b, c, d, e and 2 more)"; got != want {
		t.Fatalf("%q, want %q", got, want)
	}
}

func putFile(t *testing.T, dir, name, text string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}
