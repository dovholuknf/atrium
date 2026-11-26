//go:build integration

package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/api"
	"github.com/dovholuknf/atrium/internal/daemon/hublink"
	"github.com/dovholuknf/atrium/internal/gitsync"
	"github.com/dovholuknf/atrium/internal/store"
)

type posted struct{ from, path, body string }

type pathHub struct {
	*fakeRelay
	mu   sync.Mutex
	got  []posted
	fail bool
}

func (h *pathHub) Hub(_ context.Context, from, _, path string, body json.RawMessage) (hublink.RelayResult, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.got = append(h.got, posted{from, path, string(body)})
	if h.fail {
		return hublink.RelayResult{Code: 503, Error: "the hub is away"}, nil
	}
	return hublink.RelayResult{OK: true, Data: json.RawMessage(`{"url":"/d/saved","slug":"saved","version":1}`)}, nil
}

func (h *pathHub) posts(path string) []posted {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []posted
	for _, p := range h.got {
		if p.path == path {
			out = append(out, p)
		}
	}
	return out
}

func writeIn(t *testing.T, dir, name, text string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCloseSavesALooseReportToTheHubThenRemovesTheHome(t *testing.T) {
	t.Parallel()
	d, _ := homeRoom(t)
	hub := &pathHub{fakeRelay: &fakeRelay{}}
	d.SetRelay(hub)
	task, row, dd := aHomeThrowaway(t, d)
	if err := d.st.SetThrowaway(task.ID, false); err != nil {
		t.Fatal(err)
	}
	writeIn(t, dd.Work, "REPORT.md", "# found it")
	writeIn(t, dd.Work, "node_modules/pkg/index.js", "regenerable")
	writeIn(t, dd.Dir, "BRIEF.md", "brief")
	if why := d.closeEmptyHome(task.ID); why != "" {
		t.Fatalf("kept: %q", why)
	}
	homeGone(t, d, row, dd)
	got := hub.posts("/_hub/publish")
	if len(got) != 1 || !strings.Contains(got[0].body, "# found it") || strings.Contains(got[0].body, "regenerable") ||
		strings.Contains(got[0].body, "brief") || got[0].from != task.WireName {
		t.Fatalf("posts %+v", got)
	}
}

func TestCloseSavesNothingForRegenerableContent(t *testing.T) {
	t.Parallel()
	d, _ := homeRoom(t)
	hub := &pathHub{fakeRelay: &fakeRelay{}}
	d.SetRelay(hub)
	task, row, dd := aHomeThrowaway(t, d)
	if err := d.st.SetThrowaway(task.ID, false); err != nil {
		t.Fatal(err)
	}
	writeIn(t, dd.Work, "node_modules/pkg/index.js", "x")
	writeIn(t, dd.Work, "dist/app.js", "x")
	writeIn(t, dd.Dir, "BRIEF.md", "brief")
	writeIn(t, dd.Work, "BRIEF.md", "brief")
	writeIn(t, dd.Meta, "says/1.md", "a say")
	if why := d.closeEmptyHome(task.ID); why != "" {
		t.Fatalf("kept: %q", why)
	}
	homeGone(t, d, row, dd)
	if n := len(hub.posts("/_hub/publish")); n != 0 {
		t.Fatalf("saved %d times", n)
	}
}

func TestCloseKeepsTheHomeWhenTheFilesCannotBeSaved(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		file, text string
		fail       bool
		want       string
	}{
		"hub away":    {"REPORT.md", "x", true, "did not save"},
		"secret name": {".env", "KEY=1", false, ".env"},
		"binary":      {"out.bin", "a\x00b", false, "out.bin"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			d, _ := homeRoom(t)
			d.SetRelay(&pathHub{fakeRelay: &fakeRelay{}, fail: tc.fail})
			task, _, dd := aHomeThrowaway(t, d)
			if err := d.st.SetThrowaway(task.ID, false); err != nil {
				t.Fatal(err)
			}
			writeIn(t, dd.Work, tc.file, tc.text)
			if why := d.closeEmptyHome(task.ID); !strings.Contains(why, tc.want) {
				t.Fatalf("why %q", why)
			}
			if _, err := os.Stat(filepath.Join(dd.Work, tc.file)); err != nil {
				t.Fatal("the file went")
			}
		})
	}
}

func TestTheCruftPassFreesWhatHoldsNothingAndKeepsWhatDoes(t *testing.T) {
	t.Parallel()
	d, root := homeRoom(t)
	repo := homeRepo(t)
	_, _, _ = worktreeHome(t, d, repo)
	cullGit(t, repo, "branch", "claude/landed")
	cullGit(t, repo, "checkout", "-q", "-b", "claude/unique")
	cullGit(t, repo, "commit", "-q", "--allow-empty", "-m", "mine")
	cullGit(t, repo, "checkout", "-q", "main")
	if err := os.MkdirAll(filepath.Join(root, "stranger-abc123"), 0o755); err != nil {
		t.Fatal(err)
	}
	rep := d.cruftPass(context.Background())
	var freed, needs []string
	for _, f := range rep.Freed {
		freed = append(freed, f.Kind+" "+f.Ref)
	}
	for _, k := range rep.Kept {
		if k.Needs {
			needs = append(needs, k.Ref+" "+k.Why)
		}
	}
	if !strings.Contains(strings.Join(freed, "\n"), "claude/landed") || rep.FreedCount != 1 {
		t.Fatalf("freed %v", freed)
	}
	if got := cullGit(t, repo, "branch", "--list", "claude/landed"); strings.TrimSpace(got) != "" {
		t.Fatalf("the landed branch is still there: %q", got)
	}
	all := strings.Join(needs, "\n")
	if !strings.Contains(all, "claude/unique") || !strings.Contains(all, "stranger-abc123") {
		t.Fatalf("needs %v", needs)
	}
	if got := cullGit(t, repo, "branch", "--list", "claude/unique"); strings.TrimSpace(got) == "" {
		t.Fatal("a branch with unique commits was deleted")
	}
}

func TestTheCruftReportIsMarkedDoneOnlyOnceTheHubTookIt(t *testing.T) {
	t.Parallel()
	d, _ := homeRoom(t)
	hub := &pathHub{fakeRelay: &fakeRelay{}, fail: true}
	d.SetRelay(hub)
	d.cruftNightly(context.Background())
	if len(hub.posts("/_hub/cruft")) != 1 {
		t.Fatalf("posts %+v", hub.got)
	}
	if day, _ := d.st.Setting(settingCruftDay); day != "" {
		t.Fatalf("a refused report marked %q", day)
	}
	hub.mu.Lock()
	hub.fail = false
	hub.mu.Unlock()
	d.cruftNightly(context.Background())
	if day, _ := d.st.Setting(settingCruftDay); day != time.Now().Format(time.DateOnly) {
		t.Fatalf("day %q", day)
	}
}

func TestACruftPassIsDueOncePerDayAfterItsHour(t *testing.T) {
	t.Parallel()
	d, _ := homeRoom(t)
	at := time.Date(2026, 11, 24, CruftHour, 30, 0, 0, time.Local)
	if d.cruftDue(at.Add(-2 * time.Hour)) {
		t.Fatal("due before its hour")
	}
	if !d.cruftDue(at) {
		t.Fatal("not due at its hour")
	}
	if err := d.st.SetSetting(settingCruftDay, "2026-11-24"); err != nil {
		t.Fatal(err)
	}
	if d.cruftDue(at.Add(time.Hour)) || !d.cruftDue(at.Add(24*time.Hour)) {
		t.Fatal("not once a day")
	}
}

func TestAnIgnoredBuildDirDoesNotKeepAWorkFolderButAnIgnoredEnvFileDoes(t *testing.T) {
	t.Parallel()
	repo := homeRepo(t)
	writeIn(t, repo, ".gitignore", "build.claude/\n.env\n")
	cullGit(t, repo, "add", ".gitignore")
	cullGit(t, repo, "commit", "-q", "-m", "ignore")
	writeIn(t, repo, "build.claude/atrium", "binary")
	writeIn(t, repo, "node_modules/x/index.js", "x")
	if why := unseenWork(nil, repo); why != "" {
		t.Fatalf("kept for build output: %q", why)
	}
	writeIn(t, repo, ".env", "KEY=1")
	if why := unseenWork(nil, repo); !strings.Contains(why, "ignored files") {
		t.Fatalf("an ignored .env did not keep it: %q", why)
	}
}

// The refresh before a close judges a branch merged runs for a card that is done by then, so its token has to be good
// for that one fetch. A refused token left claude/main stale and a landed branch was asked about.
func TestTheHubRefreshOfAClosedCardIsMadeWithATokenTheForwarderAccepts(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	task := plainTask(t, d, "closer")
	if err := d.st.SetStatus(task.ID, store.StatusDone); err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetSetting(store.SettingGitPush, "hub"); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var accepted []bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, ok := d.HubGit.Cards.Check(r.Header.Get(gitsync.HeaderCardToken))
		mu.Lock()
		accepted = append(accepted, ok)
		mu.Unlock()
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	d.SetAgentAddr(srv.Listener.Addr())
	repo := homeRepo(t)
	_ = d.HubRefreshBranch(context.Background(), task.ID, repo, "github.com/o/r", "claude/x")
	mu.Lock()
	defer mu.Unlock()
	if len(accepted) == 0 {
		t.Fatal("the forwarder was never asked")
	}
	for _, ok := range accepted {
		if !ok {
			t.Fatalf("the forwarder refused the token of a closed card: %v", accepted)
		}
	}
}

func TestTheNightlyPassIsOffUntilTheRoomSettingIsOn(t *testing.T) {
	t.Parallel()
	d, _ := homeRoom(t)
	if d.cruftOn() {
		t.Fatal("a new room runs the nightly pass")
	}
	if err := d.st.SetSetting(api.SettingCruftNightly, "on"); err != nil {
		t.Fatal(err)
	}
	if !d.cruftOn() {
		t.Fatal("the setting on did not turn the pass on")
	}
}
