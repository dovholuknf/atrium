package daemon

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

func TestAStashIsRefusedWhenTheRoomOrTheCardSaysNo(t *testing.T) {
	d := testDaemon(t)
	task := plainTask(t, d, "closer")
	ctx := context.Background()
	if _, _, err := d.StashTo(ctx, "no-such-card", t.TempDir(), "fix/x"); err == nil {
		t.Fatal("stashed for a card that is not there")
	}
	d.setAgentAddr(&net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 7777})
	if err := d.st.SetSetting(store.SettingGitPush, "none"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := d.StashTo(ctx, task.ID, t.TempDir(), "fix/x"); err == nil || !strings.Contains(err.Error(), "git.push is none") {
		t.Fatalf("git.push none: %v", err)
	}
	if err := d.st.SetSetting(store.SettingGitPush, "hub"); err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetTags(task.ID, []string{OutsideCodeTag}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := d.StashTo(ctx, task.ID, t.TempDir(), "fix/x"); err == nil || !strings.Contains(err.Error(), "not the room's own") {
		t.Fatalf("outside code: %v", err)
	}
}

// The dirty files go into one WIP commit on the worktree's branch, and the local stash name is gone again whether
// the push landed or not. The push here has nowhere to go.
func TestAStashCommitsTheDirtyFilesAndLeavesNoLocalStashBranch(t *testing.T) {
	d := testDaemon(t)
	d.setAgentAddr(&net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 7777})
	task := plainTask(t, d, "closer")
	dir := t.TempDir()
	gitT(t, dir, time.Time{}, "init", "-q", "-b", "fix/x")
	gitT(t, dir, time.Time{}, "commit", "-q", "--allow-empty", "-m", "base")
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := d.StashTo(context.Background(), task.ID, dir, "fix/x"); err == nil {
		t.Fatal("a push with no hub remote landed")
	}
	if got := gitT(t, dir, time.Time{}, "log", "-1", "--format=%s"); !strings.HasPrefix(got, "atrium stash:") {
		t.Errorf("the WIP commit is %q", got)
	}
	if got := gitT(t, dir, time.Time{}, "status", "--porcelain"); strings.TrimSpace(got) != "" {
		t.Errorf("still dirty: %q", got)
	}
	if got := gitT(t, dir, time.Time{}, "branch", "--list", "stash/*"); strings.TrimSpace(got) != "" {
		t.Errorf("the local stash branch is left: %q", got)
	}
}

// A card being closed may push its stash though its session ended, and only while the stash is going.
func TestAClosedCardsTokenIsGoodOnlyWhileItsStashIsPushed(t *testing.T) {
	d := testDaemon(t)
	task := plainTask(t, d, "closer")
	if err := d.st.SetStatus(task.ID, store.StatusDead); err != nil {
		t.Fatal(err)
	}
	tok, err := d.hubGit.cards.Mint(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := d.hubGit.cards.Check(tok); ok {
		t.Fatal("a dead card's token was accepted")
	}
	d.hubGit.stashing.Store(task.ID, true)
	if _, ok := d.hubGit.cards.Check(tok); !ok {
		t.Fatal("a stashing card's token was refused")
	}
	d.hubGit.stashing.Delete(task.ID)
	if _, ok := d.hubGit.cards.Check(tok); ok {
		t.Fatal("the token outlived the stash")
	}
}
