//go:build integration

package api

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/gitsync"
	"github.com/dovholuknf/atrium/internal/store"
)

// launched is what the open sent the launcher.
type launched struct {
	Cwd, Prompt, Title, Branch, Repo, Org, Host string
	Tags                                        []string
}

func lastLaunch(t *testing.T, oh *openHarness) launched {
	t.Helper()
	if len(oh.launched) == 0 {
		t.Fatal("nothing was launched")
	}
	var l launched
	_ = json.Unmarshal(oh.launched[len(oh.launched)-1], &l)
	return l
}

func inventory(t *testing.T, st *store.Store, card string) string {
	t.Helper()
	rows, _ := st.Resources(card)
	out := []string{}
	for _, r := range rows {
		out = append(out, r.Kind+" "+r.Ref)
	}
	return strings.Join(out, ",")
}

// AN ISSUE IS A WORKTREE ON ITS OWN BRANCH OFF THE DEFAULT BRANCH, and a card. No review row, the prompt carries the
// note about someone else's text, and a second paste answers the same card.
func TestOpenAnIssueMakesABranchOffTheDefaultAndACardWithNoReview(t *testing.T) {
	oh := newOpenHarness(t, &fakeForge{})
	repo := oh.checkout(t, "o", "r")
	code, out := oh.open(t, "https://github.com/o/r/issues/3")
	if code != 201 || out["kind"] != "issue" || out["key"] != "github.com/o/r/i3" || out["repo"] != "github.com/o/r" ||
		out["pr"] != nil {
		t.Fatalf("%d %v", code, out)
	}
	path := out["worktree"].(string)
	if !strings.HasSuffix(path, "/o/r/issue-3") {
		t.Fatalf("worktree %q", path)
	}
	if got := git(t, path, "rev-parse", "--abbrev-ref", "HEAD"); got != "issue-3" {
		t.Errorf("the worktree is on %q", got)
	}
	if git(t, path, "rev-parse", "HEAD") != git(t, repo, "rev-parse", "origin/main") {
		t.Error("the branch is not off the default branch")
	}
	l := lastLaunch(t, oh)
	if l.Cwd != path || l.Branch != "issue-3" || l.Repo != "r" || !strings.HasPrefix(l.Prompt, "work on it\n\n") ||
		!strings.Contains(l.Prompt, strangerNote) || strings.Join(l.Tags, ",") != "issue,link:github.com/o/r/i3" {
		t.Errorf("the launch was %+v", l)
	}
	if rows, _ := oh.srv.st.PRs(store.PRFilter{}); len(rows) != 0 {
		t.Errorf("an issue made a review row: %+v", rows[0])
	}
	if got := inventory(t, oh.srv.st, out["card"].(string)); got != "worktree "+path+",branch issue-3" {
		t.Errorf("the inventory is %s", got)
	}
	code, again := oh.open(t, "https://github.com/o/r/issues/3")
	if code != 200 || again["created"] != false || again["card"] != out["card"] || len(oh.launched) != 1 {
		t.Errorf("the second paste: %d %v", code, again)
	}
}

// A BRANCH LINK checks out the branch the remote has, and refuses one it does not.
func TestOpenABranchChecksOutTheRemoteBranch(t *testing.T) {
	oh := newOpenHarness(t, &fakeForge{})
	git(t, oh.up, "branch", "feat/y")
	oh.checkout(t, "o", "r")
	code, out := oh.open(t, "https://github.com/o/r/tree/feat/y")
	if code != 201 || out["kind"] != "branch" || out["key"] != "github.com/o/r@feat/y" {
		t.Fatalf("%d %v", code, out)
	}
	path := out["worktree"].(string)
	if got := git(t, path, "rev-parse", "--abbrev-ref", "HEAD"); got != "feat/y" {
		t.Errorf("the worktree is on %q", got)
	}

	other := newOpenHarness(t, &fakeForge{})
	other.checkout(t, "o", "r")
	code, out = other.open(t, "https://github.com/o/r/tree/feat/y")
	if code != 400 || out["step"] != "worktree" || !strings.Contains(out["error"].(string), "is not in") {
		t.Errorf("a branch nowhere: %d %v", code, out)
	}
	if len(other.launched) != 0 {
		t.Error("a refused branch launched")
	}
}

// A SUPPORT LINK NAMES NO REPO: it opens in the row's default repo, or the one the request names.
func TestOpenATicketUsesTheDefaultRepoOrTheOneAsked(t *testing.T) {
	oh := newOpenHarness(t, &fakeForge{})
	oh.defaultRepo = "github.com/o/r"
	oh.checkout(t, "o", "r")
	code, out := oh.open(t, "https://acme.zendesk.com/agent/tickets/9")
	if code != 201 || out["kind"] != "support" || out["key"] != "zendesk:acme.zendesk.com/9" ||
		out["repo"] != "github.com/o/r" || !strings.HasSuffix(out["worktree"].(string), "/o/r/zendesk-9") {
		t.Fatalf("%d %v", code, out)
	}
	l := lastLaunch(t, oh)
	if l.Host != "github.com" || l.Org != "o" || l.Repo != "r" || !strings.Contains(l.Prompt, strangerNote) {
		t.Errorf("the launch was %+v", l)
	}

	bad := newOpenHarness(t, &fakeForge{})
	rec := post(t, bad.h, "/v1/open", map[string]any{"url": "https://acme.zendesk.com/agent/tickets/9",
		"repo": "github.com/../x"})
	if rec.Code != 400 || len(bad.launched) != 0 {
		t.Errorf("a bad repo: %d %s", rec.Code, rec.Body.String())
	}
}

// NO REPO IS A SCRATCH FOLDER of the card's own under the scm root, which a close deletes.
func TestOpenATicketWithNoRepoMakesAScratchFolderThatCloseRemoves(t *testing.T) {
	oh := newOpenHarness(t, &fakeForge{})
	scm := realSlash(t.TempDir())
	if err := oh.srv.st.SetSetting(gitsync.SettingSCMRoot, scm); err != nil {
		t.Fatal(err)
	}
	oh.defaultRepo = "github.com/o/r"
	rec := post(t, oh.h, "/v1/open", map[string]any{"url": "https://acme.zendesk.com/agent/tickets/9", "repo": "none"})
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	want := scm + "/scratch/acme.zendesk.com/zendesk-9"
	if rec.Code != 201 || out["worktree"] != want || out["repo"] != "" || !exists(want) {
		t.Fatalf("%d %v, want %s", rec.Code, out, want)
	}
	if l := lastLaunch(t, oh); l.Repo != "" || l.Branch != "" || l.Cwd != want {
		t.Errorf("the launch was %+v", l)
	}
	card := out["card"].(string)
	if got := inventory(t, oh.srv.st, card); got != "dir "+want {
		t.Errorf("the inventory is %s", got)
	}
	code, closed := closeCall(t, oh, card, map[string]any{"confirm": true})
	if code != 200 || !closed.Closed || closed.Left != 0 || exists(want) {
		t.Errorf("the close: %d %+v, still there %v", code, closed, exists(want))
	}
}

// A scratch folder needs the scm root, and says so.
func TestOpenATicketWithNoRepoAndNoSCMRootSaysWhy(t *testing.T) {
	// A home with no ~/git, or the room would use the one the machine has. See gitsync.EffectiveSCMRoot.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	oh := newOpenHarness(t, &fakeForge{})
	code, out := oh.open(t, "https://acme.zendesk.com/agent/tickets/9")
	if code != 422 || out["code"] != "no_scm_root" || out["step"] != "worktree" || !strings.Contains(out["error"].(string), "git.scm_root") {
		t.Errorf("%d %v", code, out)
	}
}

// The kind comes from the captures and the pull-request tag, never the text.
func TestClassifyReadsTheKindOffTheCaptures(t *testing.T) {
	for _, c := range []struct {
		got  store.Resolved
		want string
	}{
		{store.Resolved{Tags: []string{"pull-request"}, Vars: map[string]string{"host": "h", "org": "o", "repo": "r", "num": "1"}}, linkPR},
		{store.Resolved{Tags: []string{"pull-request"}, Vars: map[string]string{"host": "h", "org": "o", "repo": "r"}}, ""},
		{store.Resolved{Branch: "issue-1", Vars: map[string]string{"host": "h", "org": "o", "repo": "r", "num": "1"}}, linkIssue},
		{store.Resolved{Branch: "{ref}", Vars: map[string]string{"host": "h", "org": "o", "repo": "r"}}, ""},
		{store.Resolved{Branch: "x", Vars: map[string]string{"host": "h", "org": "o", "repo": "r"}}, linkBranch},
		{store.Resolved{Branch: "d-1", Vars: map[string]string{"host": "h", "num": "1"}}, linkSupport},
		{store.Resolved{Vars: map[string]string{"host": "h", "org": "o", "repo": "r"}}, ""},
	} {
		c := c
		if got := classify(&c.got); got.kind != c.want {
			t.Errorf("%+v is %q, want %q", c.got, got.kind, c.want)
		}
	}
}
