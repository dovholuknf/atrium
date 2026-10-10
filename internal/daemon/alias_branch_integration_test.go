//go:build integration

package daemon

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

// repoOn is a directory that reads as a checkout on the branch, which is all
// the store looks at.
func repoOn(t *testing.T, branch string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	head := "ref: refs/heads/" + branch + "\n"
	if err := os.WriteFile(filepath.Join(dir, ".git", "HEAD"), []byte(head), 0o644); err != nil {
		t.Fatal(err)
	}
	return filepath.ToSlash(dir)
}

// A session atrium had never heard of is adopted onto a card named for its branch.
func TestAnAdoptedSessionTakesItsBranchAsAlias(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	if err := d.onSession(SessionEvent{Agent: "seen-a", Event: "start", Cwd: repoOn(t, "claude/u-foo")}); err != nil {
		t.Fatal(err)
	}
	if err := d.onSession(SessionEvent{Agent: "seen-b", Event: "start", Cwd: repoOn(t, "other/u-foo")}); err != nil {
		t.Fatal(err)
	}
	if err := d.onSession(SessionEvent{Agent: "seen-c", Event: "start", Cwd: repoOn(t, "main")}); err != nil {
		t.Fatal(err)
	}
	for agent, want := range map[string]string{"seen-a": "u-foo", "seen-b": "u-foo-2", "seen-c": ""} {
		task, err := d.st.GetByWireName(d.st.Qualify(agent))
		if err != nil {
			t.Fatal(err)
		}
		if task.Alias != want {
			t.Errorf("%s alias = %q, want %q", agent, task.Alias, want)
		}
	}
}

// Joining a card that has no alias gives it one, and a card that has one keeps it.
func TestJoinTakesTheBranchAsAliasUnlessThereIsOne(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	bare, _, err := d.st.Register(store.Observed{WireName: "bare", Worktree: "/tmp/atrium-test", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	named, _, err := d.st.Register(store.Observed{WireName: "named", Worktree: "/tmp/atrium-test", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{bare.ID, named.ID} {
		if err := d.st.SetPlace(id, store.Place{Branch: "claude/u-join"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := d.st.SetAlias(named.ID, "mine"); err != nil {
		t.Fatal(err)
	}
	for _, agent := range []string{"bare", "named"} {
		if err := d.onSession(SessionEvent{Agent: agent, Event: "join"}); err != nil {
			t.Fatal(err)
		}
	}
	if got, _ := d.st.Get(bare.ID); got.Alias != "u-join" {
		t.Errorf("joined card alias = %q", got.Alias)
	}
	if got, _ := d.st.Get(named.ID); got.Alias != "mine" {
		t.Errorf("a typed alias was replaced: %q", got.Alias)
	}

	// A title with a prefix alias beats the branch.
	titled, _, _ := d.st.Register(store.Observed{WireName: "titled", Worktree: "/tmp/atrium-test", Runner: "claude"})
	if err := d.st.SetPlace(titled.ID, store.Place{Branch: "claude/u-other"}); err != nil {
		t.Fatal(err)
	}
	if err := d.onSession(SessionEvent{Agent: "titled", Event: "join", Title: "sa89: typing gate"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := d.st.Get(titled.ID); got.Alias != "sa89" {
		t.Errorf("title did not win on join: %q", got.Alias)
	}
}
