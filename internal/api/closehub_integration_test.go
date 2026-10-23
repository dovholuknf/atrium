//go:build integration

package api

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

func hubRowFor(t *testing.T, st *store.Store, card, branch string) *store.CardResource {
	t.Helper()
	rows, _ := st.Resources(card)
	for _, r := range rows {
		if r.Kind == store.ResHubBranch && r.Ref == branch {
			return r
		}
	}
	t.Fatalf("no hub branch row for %s", branch)
	return nil
}

func TestCloseDeletesAMergedHubBranchAndAsksAboutAnUnmergedOne(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: a real server with git worktrees and a database")
	}
	t.Parallel()
	oh, card, wt, _ := openForClose(t)
	ctx := context.Background()
	// claude/done is at HEAD, which claude/main has, so it is merged. claude/wip has a commit claude/main lacks.
	// One fast-import makes all three refs, so the worktree's own HEAD does not move.
	stream := "reset refs/heads/claude/main\nfrom HEAD\n\nreset refs/heads/claude/done\nfrom HEAD\n\n" +
		"commit refs/heads/claude/wip\ncommitter t <t@t> 1 +0000\ndata 4\nwip\nfrom HEAD\n\n"
	fi := exec.CommandContext(ctx, "git", "fast-import", "--quiet")
	fi.Dir = filepath.FromSlash(wt)
	fi.Stdin = strings.NewReader(stream)
	if out, err := fi.CombinedOutput(); err != nil {
		t.Fatalf("git fast-import: %v\n%s", err, out)
	}
	for _, b := range []string{"claude/done", "claude/wip"} {
		if _, err := oh.srv.st.AddResource(card, store.ResHubBranch, b, "github/o/r"); err != nil {
			t.Fatal(err)
		}
	}
	var deleted []string
	oh.srv.HubDelete = func(_ context.Context, c, _, repo, branch string) error {
		if c != card || repo != "github/o/r" {
			t.Errorf("asked for %s %s of %s", repo, branch, c)
		}
		deleted = append(deleted, branch)
		return nil
	}

	code, out := closeCall(t, oh, card, map[string]any{"confirm": true})
	if code != 409 || out.Code != "needs_answer" {
		t.Fatalf("the unmerged hub branch was not asked about: %d %+v", code, out)
	}
	wip := hubRowFor(t, oh.srv.st, card, "claude/wip")
	if len(deleted) != 0 {
		t.Fatalf("something was deleted before the answer: %v", deleted)
	}

	// Keep leaves it on the hub and the card, and the merged one still goes.
	answers := map[string]string{strconv.Itoa(wip.Seq): "keep"}
	code, out = closeCall(t, oh, card, map[string]any{"confirm": true, "answers": answers})
	if code != 200 || !out.Closed {
		t.Fatalf("%d %+v", code, out)
	}
	if strings.Join(deleted, ",") != "claude/done" {
		t.Fatalf("deleted %v, want only the merged one", deleted)
	}
	if !hubRowFor(t, oh.srv.st, card, "claude/wip").Live() || hubRowFor(t, oh.srv.st, card, "claude/done").Live() {
		t.Fatal("close did not free the merged row and keep the unmerged one")
	}
}

func TestCloseReportsAFailedHubDeleteAndFinishesTheRest(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: a real server with git worktrees and a database")
	}
	t.Parallel()
	oh, card, wt, _ := openForClose(t)
	ctx := context.Background()
	if _, err := gitIn(ctx, wt, "branch", "claude/main", "HEAD"); err != nil {
		t.Fatal(err)
	}
	if _, err := gitIn(ctx, wt, "branch", "claude/done", "HEAD"); err != nil {
		t.Fatal(err)
	}
	if _, err := oh.srv.st.AddResource(card, store.ResHubBranch, "claude/done", "github/o/r"); err != nil {
		t.Fatal(err)
	}
	oh.srv.HubDelete = func(context.Context, string, string, string, string) error {
		return errors.New("the hub did not answer")
	}
	code, out := closeCall(t, oh, card, map[string]any{"confirm": true})
	if code != 200 || !out.Closed {
		t.Fatalf("%d %+v", code, out)
	}
	if !strings.Contains(strings.Join(out.Warn, "\n"), "claude/done was not deleted") {
		t.Errorf("the failure was not reported: %v", out.Warn)
	}
	row := hubRowFor(t, oh.srv.st, card, "claude/done")
	if !row.Live() || !strings.Contains(row.FreedErr, "did not answer") {
		t.Errorf("the row should stay live with the error: %+v", row)
	}
	if exists(wt) {
		t.Error("the rest of the close did not run: the worktree is still there")
	}
}
