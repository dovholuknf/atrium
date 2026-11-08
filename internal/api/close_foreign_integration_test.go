//go:build integration

package api

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCloseRemovesAWorktreeAtriumDidNotMakeOnlyWhenAskedAndNeverTheMainCheckout(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: a real server with git worktrees and a database")
	}
	t.Parallel()
	oh, card, wt, _ := openForClose(t)
	repo := filepath.ToSlash(filepath.Dir(git(t, wt, "rev-parse", "--path-format=absolute", "--git-common-dir")))
	preview := func() *closeForeign {
		t.Helper()
		rec := post(t, oh.h, "/v1/tasks/"+card+"/close", map[string]any{})
		var pv closePreview
		if err := json.Unmarshal(rec.Body.Bytes(), &pv); err != nil {
			t.Fatal(err)
		}
		return pv.Foreign
	}

	if err := oh.srv.st.SetWorktree(card, repo); err != nil {
		t.Fatal(err)
	}
	if f := preview(); f == nil || !f.Main || f.Why == "" {
		t.Fatalf("the main checkout is offered: %+v", f)
	}

	gwt := filepath.ToSlash(filepath.Join(t.TempDir(), "gwt"))
	git(t, repo, "worktree", "add", "-b", "gwt-branch", gwt)
	if err := oh.srv.st.SetWorktree(card, gwt); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.FromSlash(gwt), "notes.txt"), []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := preview()
	if f == nil || f.Main || f.Why != "" || f.Branch != "gwt-branch" || f.Dirty == 0 {
		t.Fatalf("%+v", f)
	}
	if code, out := closeCall(t, oh, card, map[string]any{"confirm": true, "adopt": true}); code != 409 || out.Code != "needs_answer" {
		t.Fatalf("work only that folder has went unasked: %d %+v", code, out)
	}
	rows, _ := oh.srv.st.Resources(card)
	for _, r := range rows {
		if samePathFold(r.Ref, gwt) {
			t.Fatalf("an unanswered close adopted the folder: %+v", r)
		}
	}
	code, out := closeCall(t, oh, card, map[string]any{"confirm": true, "adopt": true,
		"answers": map[string]string{"adopt": "delete"}})
	if code != 200 || !out.Closed {
		t.Fatalf("%d %+v", code, out)
	}
	if exists(gwt) {
		t.Error("the worktree is still on disk")
	}
	if strings.Contains(git(t, repo, "branch", "--list", "gwt-branch"), "gwt-branch") {
		t.Error("the branch is still there")
	}
}
