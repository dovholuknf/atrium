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

// A join that brings a session onto the board takes the branch as its alias, or the title's prefix when it has one.
// A card that was already there keeps what it has: a person's alias, or the lack of one they chose.
func TestJoinTakesTheBranchAsAliasOnlyForANewCard(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	if err := d.onSession(SessionEvent{Agent: "fresh", Event: "join", Cwd: repoOn(t, "claude/u-join")}); err != nil {
		t.Fatal(err)
	}
	if got, err := d.st.GetByWireName(d.st.Qualify("fresh")); err != nil || got.Alias != "u-join" {
		t.Errorf("a new joined card alias = %+v, %v", got, err)
	}

	// A title with a prefix alias beats the branch.
	if err := d.onSession(SessionEvent{Agent: "titled", Event: "join", Title: "sa89: typing gate", Cwd: repoOn(t, "claude/u-other")}); err != nil {
		t.Fatal(err)
	}
	if got, err := d.st.GetByWireName(d.st.Qualify("titled")); err != nil || got.Alias != "sa89" {
		t.Errorf("title did not win on join: %+v, %v", got, err)
	}

	named, _, err := d.st.Register(store.Observed{WireName: "named", Worktree: "/tmp/atrium-test", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetPlace(named.ID, store.Place{Branch: "claude/u-join"}); err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetAlias(named.ID, "mine"); err != nil {
		t.Fatal(err)
	}
	if err := d.onSession(SessionEvent{Agent: "named", Event: "join"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := d.st.Get(named.ID); got.Alias != "mine" {
		t.Errorf("a typed alias was replaced: %q", got.Alias)
	}
}

// A card whose alias a person cleared does not get one back when its session joins again.
func TestARejoinDoesNotGiveAClearedAliasBack(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	cwd := repoOn(t, "claude/u-rejoin")
	if err := d.onSession(SessionEvent{Agent: "again", Event: "join", Cwd: cwd}); err != nil {
		t.Fatal(err)
	}
	card, err := d.st.GetByWireName(d.st.Qualify("again"))
	if err != nil || card.Alias != "u-rejoin" {
		t.Fatalf("the first join gave %+v, %v", card, err)
	}
	if err := d.st.SetAlias(card.ID, ""); err != nil {
		t.Fatal(err)
	}
	for _, title := range []string{"", "sa90: second look"} {
		if err := d.onSession(SessionEvent{Agent: "again", Event: "join", Title: title, Cwd: cwd}); err != nil {
			t.Fatal(err)
		}
		if got, _ := d.st.Get(card.ID); got.Alias != "" {
			t.Errorf("a cleared alias came back on a rejoin (title %q): %q", title, got.Alias)
		}
	}
}
