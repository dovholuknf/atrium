//go:build integration

package daemon

import (
	"bytes"
	"github.com/dovholuknf/atrium/internal/base/safepath"
	"github.com/dovholuknf/atrium/internal/daemon/home"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/base/cardroot"
	"github.com/dovholuknf/atrium/internal/daemon/shared"
	"github.com/dovholuknf/atrium/internal/store"
)

func homeRoom(t *testing.T) (*Daemon, string) {
	t.Helper()
	d := testDaemon(t)
	root := filepath.Join(t.TempDir(), "cards")
	if err := d.st.SetSetting(cardroot.Setting, root); err != nil {
		t.Fatal(err)
	}
	return d, root
}

func homeRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	cullGit(t, repo, "init", "-q", "-b", "main")
	cullGit(t, repo, "commit", "-q", "--allow-empty", "-m", "root")
	cullGit(t, repo, "branch", "claude/main")
	return repo
}

func worktreeHome(t *testing.T, d *Daemon, repo string) (*store.Task, store.CardHome, home.HomeDirs) {
	t.Helper()
	req := shared.LaunchRequest{Repo: repo, Title: "Fix The Thing"}
	row, dd, err := d.LaunchHome(store.CardHomeWorktree, &req)
	if err != nil {
		t.Fatal(err)
	}
	task, _, err := d.st.Register(store.Observed{WireName: "worker", Worktree: filepath.ToSlash(dd.Work), Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.st.CardHomeCard(row.Path, task.ID); err != nil {
		t.Fatal(err)
	}
	row, _, _ = d.st.CardHomeByPath(row.Path)
	return task, row, dd
}

func TestHomeSlugAndName(t *testing.T) {
	t.Parallel()
	if got := home.HomeSlug("Fix: the  THING!!", "w"); got != "fix-the-thing" {
		t.Fatalf("slug %q", got)
	}
	if got := home.HomeSlug("", "Worker_1"); got != "worker-1" {
		t.Fatalf("wire slug %q", got)
	}
	if got := home.HomeSlug("!!!", ""); got != "card" {
		t.Fatalf("empty slug %q", got)
	}
	if got := home.HomeSlug(strings.Repeat("ab ", 40), ""); len(got) > 40 || strings.HasSuffix(got, "-") {
		t.Fatalf("long slug %q", got)
	}
}

func TestACardRootNobodyMayRemoveInIsRefused(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	home, _ := os.UserHomeDir()
	for _, bad := range []string{filepath.VolumeName(home) + string(filepath.Separator), home} {
		if err := d.st.SetSetting(cardroot.Setting, bad); err != nil {
			t.Fatal(err)
		}
		if _, _, err := d.MakeHome(store.CardHomeScratch, "t", "w"); err == nil {
			t.Fatalf("a home was made under %s", bad)
		}
	}
}

func TestAHomeIsFlatWithARow(t *testing.T) {
	t.Parallel()
	d, root := homeRoom(t)
	row, dd, err := d.MakeHome(store.CardHomeScratch, "Fix The Thing", "w")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(dd.Dir) != root || !strings.HasPrefix(filepath.Base(dd.Dir), "fix-the-thing-") {
		t.Fatalf("home at %s", dd.Dir)
	}
	for _, p := range []string{dd.Work, dd.Tmp, dd.Meta} {
		if fi, err := os.Stat(p); err != nil || !fi.IsDir() {
			t.Fatalf("%s is missing", p)
		}
	}
	got, ok, _ := d.st.CardHomeByPath(row.Path)
	if !ok || got.Kind != store.CardHomeScratch || got.State != store.CardHomeLive || got.ID == "" {
		t.Fatalf("row %+v", got)
	}
}

func TestARepoLaunchMakesWorkAsAWorktree(t *testing.T) {
	t.Parallel()
	d, _ := homeRoom(t)
	repo := homeRepo(t)
	_, row, dd := worktreeHome(t, d, repo)
	if _, err := os.Stat(filepath.Join(dd.Work, ".git")); err != nil {
		t.Fatalf("work/ is not a worktree: %v", err)
	}
	if got := cullGit(t, dd.Work, "symbolic-ref", "--short", "HEAD"); got != "claude/fix-the-thing" {
		t.Fatalf("branch %q", got)
	}
	if row.Kind != store.CardHomeWorktree || row.Branch != "claude/fix-the-thing" || row.Base != "claude/main" ||
		filepath.ToSlash(row.Clone) != filepath.ToSlash(repo) {
		t.Fatalf("row %+v", row)
	}
}

func TestACardHomeKeysToItsCloneOrigin(t *testing.T) {
	t.Parallel()
	d, _ := homeRoom(t)
	repo := homeRepo(t)
	cullGit(t, repo, "remote", "add", "origin", "https://github.com/openziti/ziti-sdk-csharp.git")
	req := shared.LaunchRequest{Repo: repo, Title: "csharp gaps"}
	if _, _, err := d.LaunchHome(store.CardHomeWorktree, &req); err != nil {
		t.Fatal(err)
	}
	if req.Host != "github" || req.Org != "openziti" {
		t.Fatalf("launch place host %q org %q", req.Host, req.Org)
	}

	task, row, _ := worktreeHome(t, d, repo)
	if task.Host != "" || task.Org != "" {
		t.Fatalf("a registered card already has host %q org %q", task.Host, task.Org)
	}
	if n := d.BackfillHomeOrigins(); n != 1 {
		t.Fatalf("backfilled %d, want 1 (row %+v)", n, row)
	}
	got, err := d.st.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Host != "github" || got.Org != "openziti" || got.Repo != "repo" {
		t.Fatalf("card host %q org %q repo %q, want github openziti and the clone's name", got.Host, got.Org, got.Repo)
	}
	if n := d.BackfillHomeOrigins(); n != 0 {
		t.Fatalf("a second backfill changed %d card(s)", n)
	}
}

func TestCwdWithARepoIsRefused(t *testing.T) {
	t.Parallel()
	if err := home.HomeLaunchRefusal(shared.LaunchRequest{Repo: "github.com/o/r"}, "/x"); err == nil {
		t.Fatal("cwd with repo was accepted")
	}
	if err := home.HomeLaunchRefusal(shared.LaunchRequest{Repo: "o/r"}, "/x"); err != nil {
		t.Fatalf("a label repo with cwd: %v", err)
	}
	if err := home.HomeLaunchRefusal(shared.LaunchRequest{}, "/x"); err != nil {
		t.Fatal(err)
	}
}

func TestTheRunnerSeesItsHomeForTemp(t *testing.T) {
	t.Parallel()
	d, _ := homeRoom(t)
	_, dd, err := d.MakeHome(store.CardHomeScratch, "t", "w")
	if err != nil {
		t.Fatal(err)
	}
	env := home.HomeEnv(dd)
	for _, k := range []string{"TMP", "TEMP", "TMPDIR"} {
		if env[k] != dd.Tmp {
			t.Fatalf("%s=%q, want %q", k, env[k], dd.Tmp)
		}
	}
	if env["ATRIUM_HOME"] != filepath.ToSlash(dd.Dir) {
		t.Fatalf("ATRIUM_HOME=%q", env["ATRIUM_HOME"])
	}
}

func TestReclaimRemovesACleanLandedHome(t *testing.T) {
	t.Parallel()
	d, _ := homeRoom(t)
	repo := homeRepo(t)
	task, row, dd := worktreeHome(t, d, repo)

	removed, kept, _ := d.ReclaimHome(task, row, false)
	if !removed || kept != "" {
		t.Fatalf("removed=%v kept=%q", removed, kept)
	}
	if _, err := os.Stat(dd.Dir); !os.IsNotExist(err) {
		t.Fatal("the home is still there")
	}
	if got, _, _ := d.st.CardHomeByPath(row.Path); got.State != store.CardHomeRemoved || got.RemovedAt == "" {
		t.Fatalf("row %+v", got)
	}
	if out := cullGit(t, repo, "worktree", "list", "--porcelain"); strings.Contains(out, "fix-the-thing") {
		t.Fatalf("git still lists the worktree:\n%s", out)
	}
}

func TestReclaimKeepsADirtyOrUnlandedHomeAndSaysSoOnce(t *testing.T) {
	t.Parallel()
	d, _ := homeRoom(t)
	repo := homeRepo(t)
	task, row, dd := worktreeHome(t, d, repo)
	launcher := peerCard(t, d, "orchestrator")
	if err := d.st.SetTags(task.ID, []string{"sdk", OriginAgentTag}); err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetLineage(task.ID, "orchestrator", launcher.ID); err != nil {
		t.Fatal(err)
	}
	task, _ = d.st.Get(task.ID)

	if err := os.WriteFile(filepath.Join(dd.Work, "wip.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		got, _, _ := d.st.CardHomeByPath(row.Path)
		if removed, kept, _ := d.ReclaimHome(task, got, false); removed || !strings.Contains(kept, "uncommitted") {
			t.Fatalf("dirty home: removed=%v kept=%q", removed, kept)
		}
	}
	if n := len(pendingFrom(t, d, launcher.ID)); n != 1 {
		t.Fatalf("the launcher has %d notices, want one", n)
	}

	cullGit(t, dd.Work, "add", "wip.txt")
	cullGit(t, dd.Work, "commit", "-q", "-m", "wip")
	got, _, _ := d.st.CardHomeByPath(row.Path)
	if removed, kept, _ := d.ReclaimHome(task, got, false); removed || !strings.Contains(kept, "not landed") {
		t.Fatalf("unlanded home: removed=%v kept=%q", removed, kept)
	}
	if _, err := os.Stat(filepath.Join(dd.Work, "wip.txt")); err != nil {
		t.Fatal("the work was touched")
	}

	cullGit(t, repo, "branch", "-f", "claude/main", "claude/fix-the-thing")
	got, _, _ = d.st.CardHomeByPath(row.Path)
	if removed, kept, _ := d.ReclaimHome(task, got, false); !removed {
		t.Fatalf("a landed home was kept: %q", kept)
	}
}

func TestACardWithNoHomeIsNeverTouched(t *testing.T) {
	t.Parallel()
	d, _ := homeRoom(t)
	dir := t.TempDir()
	task := cullCard(t, d, dir)
	if _, ok := d.HomeOf(task.ID); ok {
		t.Fatal("a card with no row has a home")
	}
	if err := d.SweepHomes(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatal("the folder went")
	}
}

func TestAHomeOutsideTheRootIsNotRemoved(t *testing.T) {
	t.Parallel()
	d, _ := homeRoom(t)
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "keep.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	row, err := d.st.CardHomeNew(store.CardHome{Path: filepath.ToSlash(outside), Kind: store.CardHomeScratch})
	if err != nil {
		t.Fatal(err)
	}
	if removed, _, _ := d.ReclaimHome(nil, row, true); removed {
		t.Fatal("a folder outside the card root was removed")
	}
	if _, err := os.Stat(filepath.Join(outside, "keep.txt")); err != nil {
		t.Fatal("its contents went")
	}
}

func TestRemovalDoesNotFollowALink(t *testing.T) {
	t.Parallel()
	d, _ := homeRoom(t)
	row, dd, err := d.MakeHome(store.CardHomeScratch, "t", "w")
	if err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "precious.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dd.Work, "link")); err != nil {
		t.Skipf("no links here: %v", err)
	}
	if removed, kept, _ := d.ReclaimHome(nil, row, false); !removed {
		t.Fatalf("kept: %q", kept)
	}
	if _, err := os.Stat(filepath.Join(target, "precious.txt")); err != nil {
		t.Fatal("removal followed the link")
	}
	if _, err := os.Stat(dd.Dir); !os.IsNotExist(err) {
		t.Fatal("the home is still there")
	}
}

func aHomeThrowaway(t *testing.T, d *Daemon) (*store.Task, store.CardHome, home.HomeDirs) {
	t.Helper()
	req := shared.LaunchRequest{Title: "tryout", Throwaway: true}
	row, dd, err := d.LaunchHome(store.CardHomeThrowaway, &req)
	if err != nil {
		t.Fatal(err)
	}
	task, _, err := d.st.Register(store.Observed{WireName: "tryout", Worktree: filepath.ToSlash(dd.Work), Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetThrowaway(task.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := d.st.CardHomeCard(row.Path, task.ID); err != nil {
		t.Fatal(err)
	}
	task, _ = d.st.Get(task.ID)
	row, _, _ = d.st.CardHomeByPath(row.Path)
	return task, row, dd
}

func TestAThrowawayHomeGoesWhenItsRunnerExits(t *testing.T) {
	t.Parallel()
	d, _ := homeRoom(t)
	task, row, dd := aHomeThrowaway(t, d)
	d.endThrowaway(task.ID)
	if _, err := os.Stat(dd.Dir); !os.IsNotExist(err) {
		t.Fatal("the home is still there")
	}
	if _, err := d.st.Get(task.ID); err == nil {
		t.Fatal("the card outlived its home")
	}
	if got, _, _ := d.st.CardHomeByPath(row.Path); got.State != store.CardHomeRemoved {
		t.Fatalf("row %+v", got)
	}
}

func TestAPromotedThrowawayHomeIsKeptWhereItIs(t *testing.T) {
	t.Parallel()
	d, _ := homeRoom(t)
	task, row, dd := aHomeThrowaway(t, d)
	if err := d.st.SetPromoteTo(task.ID, filepath.ToSlash(t.TempDir())); err != nil {
		t.Fatal(err)
	}
	d.endThrowaway(task.ID)
	if _, err := os.Stat(dd.Work); err != nil {
		t.Fatal("a promoted home was removed")
	}
	if got, _, _ := d.st.CardHomeByPath(row.Path); got.State != store.CardHomeKept {
		t.Fatalf("row %+v", got)
	}
}

func TestABranchCheckSaysWhoHoldsABranchAndOffersAFreeOne(t *testing.T) {
	t.Parallel()
	d, _ := homeRoom(t)
	repo := homeRepo(t)
	if bs, err := d.BranchCheck(repo, "claude/fix-the-thing"); err != nil || bs.Exists || bs.HeldAt != "" || bs.Free != "claude/fix-the-thing" {
		t.Fatalf("a new branch: %+v %v", bs, err)
	}
	task, _, dd := worktreeHome(t, d, repo)
	cullGit(t, dd.Work, "commit", "-q", "--allow-empty", "-m", "work")
	cullGit(t, repo, "branch", "claude/fix-the-thing-2")
	bs, err := d.BranchCheck(repo, "claude/fix-the-thing")
	if err != nil {
		t.Fatal(err)
	}
	if !bs.Exists || bs.Ahead != 1 || bs.Last == "" || bs.HeldBy != task.ID || bs.Free != "claude/fix-the-thing-3" ||
		!sameFolder(bs.HeldAt, dd.Work) {
		t.Fatalf("a held branch: %+v, work %s, card %s", bs, filepath.ToSlash(dd.Work), task.ID)
	}
	if bs, err := d.BranchCheck(repo, "claude/fix-the-thing-2"); err != nil || !bs.Exists || bs.HeldAt != "" || bs.Ahead != 0 {
		t.Fatalf("a free existing branch: %+v %v", bs, err)
	}
	if _, err := d.BranchCheck(repo, "-bad..name"); err == nil {
		t.Fatal("a bad branch name was checked")
	}
	if bs, err := d.BranchCheck("github.com/nobody/nothing", "claude/x"); err != nil || bs.Exists || bs.Free != "claude/x" {
		t.Fatalf("a repo not on this room: %+v %v", bs, err)
	}
}

func TestALaunchOnABranchAnotherCardHoldsIsRefused(t *testing.T) {
	t.Parallel()
	d, _ := homeRoom(t)
	repo := homeRepo(t)
	worktreeHome(t, d, repo)
	req := shared.LaunchRequest{Repo: repo, Title: "Again", Branch: "claude/fix-the-thing"}
	if _, _, err := d.LaunchHome(store.CardHomeWorktree, &req); err == nil || !strings.Contains(err.Error(), "already checked out") {
		t.Fatalf("a held branch was launched on: %v", err)
	}
	cullGit(t, repo, "branch", "claude/old")
	req = shared.LaunchRequest{Repo: repo, Title: "Continue", Branch: "claude/old"}
	if _, _, err := d.LaunchHome(store.CardHomeWorktree, &req); err != nil {
		t.Fatalf("an existing free branch: %v", err)
	}
}

// sameFolder is whether two spellings name one folder: a temp dir can be a symlink or an 8.3 name.
func sameFolder(a, b string) bool {
	ra, ea := safepath.Real(filepath.FromSlash(a))
	rb, eb := safepath.Real(filepath.FromSlash(b))
	return ea == nil && eb == nil && strings.EqualFold(ra, rb)
}

func hubClone(t *testing.T) (clone, hub string) {
	t.Helper()
	clone = homeRepo(t)
	hub = filepath.Join(t.TempDir(), "hub.git")
	cullGit(t, filepath.Dir(clone), "clone", "-q", "--bare", clone, hub)
	cullGit(t, clone, "remote", "add", "hub", hub)
	return clone, hub
}

func TestALaunchWithNoBaseStartsFromTheHubsClaudeMain(t *testing.T) {
	t.Parallel()
	d, _ := homeRoom(t)
	clone, hub := hubClone(t)
	tip := cullGit(t, hub, "-c", "user.name=t", "-c", "user.email=t@t", "commit-tree", "-m", "ahead", "-p", "claude/main", "claude/main^{tree}")
	cullGit(t, hub, "update-ref", "refs/heads/claude/main", tip)
	req := shared.LaunchRequest{Repo: clone, Title: "Fresh Start"}
	_, dd, err := d.LaunchHome(store.CardHomeWorktree, &req)
	if err != nil {
		t.Fatal(err)
	}
	if got := cullGit(t, dd.Work, "rev-parse", "HEAD"); got != tip {
		t.Fatalf("started from %s, the hub's claude/main is %s", got, tip)
	}
}

func TestALaunchWithNoBaseIsRefusedWhenTheHubCannotBeFetched(t *testing.T) {
	t.Parallel()
	d, _ := homeRoom(t)
	clone, hub := hubClone(t)
	if err := os.RemoveAll(hub); err != nil {
		t.Fatal(err)
	}
	req := shared.LaunchRequest{Repo: clone, Title: "Stale Start"}
	if _, _, err := d.LaunchHome(store.CardHomeWorktree, &req); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("want a refusal naming the stale base, got %v", err)
	}
}

type uploadPackHub struct{ repo string }

func (u uploadPackHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	run := func(body []byte, args ...string) []byte {
		cmd := exec.Command("git", append(args, u.repo)...)
		cmd.Stdin = bytes.NewReader(body)
		out, _ := cmd.Output()
		return out
	}
	if r.Method == http.MethodGet {
		w.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement")
		_, _ = w.Write([]byte("001e# service=git-upload-pack\n0000"))
		_, _ = w.Write(run(nil, "upload-pack", "--stateless-rpc", "--advertise-refs"))
		return
	}
	body, _ := io.ReadAll(r.Body)
	w.Header().Set("Content-Type", "application/x-git-upload-pack-result")
	_, _ = w.Write(run(body, "upload-pack", "--stateless-rpc"))
}

func TestALaunchWithNoBaseFetchesThroughTheTokenCheckingForwarder(t *testing.T) {
	t.Parallel()
	d, _ := homeRoom(t)
	clone, hub := hubClone(t)
	tip := cullGit(t, hub, "-c", "user.name=t", "-c", "user.email=t@t", "commit-tree", "-m", "ahead", "-p", "claude/main", "claude/main^{tree}")
	cullGit(t, hub, "update-ref", "refs/heads/claude/main", tip)
	srv := httptest.NewServer(uploadPackHub{repo: hub})
	t.Cleanup(srv.Close)
	d.SetHubGit(func() (http.RoundTripper, error) {
		return roundTripFunc(func(r *http.Request) (*http.Response, error) {
			u, _ := url.Parse(srv.URL)
			out := r.Clone(r.Context())
			out.URL.Scheme, out.URL.Host, out.Host = u.Scheme, u.Host, u.Host
			return http.DefaultTransport.RoundTrip(out)
		}), nil
	})
	fw := httptest.NewServer(d.HubRemote())
	t.Cleanup(fw.Close)
	d.SetAgentAddr(fw.Listener.Addr())
	cullGit(t, clone, "remote", "set-url", "hub", d.HubRemoteURL("github/o/r"))
	cullGit(t, clone, "config", "protocol.version", "0")
	req := shared.LaunchRequest{Repo: clone, Title: "Fresh Start"}
	_, dd, err := d.LaunchHome(store.CardHomeWorktree, &req)
	if err != nil {
		t.Fatal(err)
	}
	if got := cullGit(t, dd.Work, "rev-parse", "HEAD"); got != tip {
		t.Fatalf("started from %s, the hub's claude/main is %s", got, tip)
	}
}

func TestALaunchWithNoBaseFetchesThroughItsOwnForwarderWhenTheCloneNamesAnotherRooms(t *testing.T) {
	t.Parallel()
	d, _ := homeRoom(t)
	clone, hub := hubClone(t)
	tip := cullGit(t, hub, "-c", "user.name=t", "-c", "user.email=t@t", "commit-tree", "-m", "ahead", "-p", "claude/main", "claude/main^{tree}")
	cullGit(t, hub, "update-ref", "refs/heads/claude/main", tip)
	srv := httptest.NewServer(uploadPackHub{repo: hub})
	t.Cleanup(srv.Close)
	d.SetHubGit(func() (http.RoundTripper, error) {
		return roundTripFunc(func(r *http.Request) (*http.Response, error) {
			u, _ := url.Parse(srv.URL)
			out := r.Clone(r.Context())
			out.URL.Scheme, out.URL.Host, out.Host = u.Scheme, u.Host, u.Host
			return http.DefaultTransport.RoundTrip(out)
		}), nil
	})
	fw := httptest.NewServer(d.HubRemote())
	t.Cleanup(fw.Close)
	d.SetAgentAddr(fw.Listener.Addr())
	cullGit(t, clone, "remote", "set-url", "hub", strings.Replace(d.HubRemoteURL("github/o/r"), "127.0.0.1:", "127.0.0.1:1", 1))
	cullGit(t, clone, "config", "protocol.version", "0")
	req := shared.LaunchRequest{Repo: clone, Title: "Fresh Start"}
	_, dd, err := d.LaunchHome(store.CardHomeWorktree, &req)
	if err != nil {
		t.Fatal(err)
	}
	if got := cullGit(t, dd.Work, "rev-parse", "HEAD"); got != tip {
		t.Fatalf("started from %s, the hub's claude/main is %s", got, tip)
	}
}

func TestALaunchWithNoBaseFetchesThroughItsOwnForwarderWhenTheCloneSpellsTheHostAnotherWay(t *testing.T) {
	t.Parallel()
	d, _ := homeRoom(t)
	clone, hub := hubClone(t)
	tip := cullGit(t, hub, "-c", "user.name=t", "-c", "user.email=t@t", "commit-tree", "-m", "ahead", "-p", "claude/main", "claude/main^{tree}")
	cullGit(t, hub, "update-ref", "refs/heads/claude/main", tip)
	srv := httptest.NewServer(uploadPackHub{repo: hub})
	t.Cleanup(srv.Close)
	d.SetHubGit(func() (http.RoundTripper, error) {
		return roundTripFunc(func(r *http.Request) (*http.Response, error) {
			u, _ := url.Parse(srv.URL)
			out := r.Clone(r.Context())
			out.URL.Scheme, out.URL.Host, out.Host = u.Scheme, u.Host, u.Host
			return http.DefaultTransport.RoundTrip(out)
		}), nil
	})
	fw := httptest.NewServer(d.HubRemote())
	t.Cleanup(fw.Close)
	d.SetAgentAddr(fw.Listener.Addr())
	cullGit(t, clone, "remote", "set-url", "hub", strings.Replace(d.HubRemoteURL("github/o/r"), "127.0.0.1:", "localhost:", 1))
	cullGit(t, clone, "config", "protocol.version", "0")
	req := shared.LaunchRequest{Repo: clone, Title: "Fresh Start"}
	_, dd, err := d.LaunchHome(store.CardHomeWorktree, &req)
	if err != nil {
		t.Fatal(err)
	}
	if got := cullGit(t, dd.Work, "rev-parse", "HEAD"); got != tip {
		t.Fatalf("started from %s, the hub's claude/main is %s", got, tip)
	}
}

func TestALaunchWithNoBaseFetchesThroughItsOwnForwarderWhenTheHubRemoteIsSetInTheWorktreeConfig(t *testing.T) {
	t.Parallel()
	d, _ := homeRoom(t)
	clone, hub := hubClone(t)
	tip := cullGit(t, hub, "-c", "user.name=t", "-c", "user.email=t@t", "commit-tree", "-m", "ahead", "-p", "claude/main", "claude/main^{tree}")
	cullGit(t, hub, "update-ref", "refs/heads/claude/main", tip)
	srv := httptest.NewServer(uploadPackHub{repo: hub})
	t.Cleanup(srv.Close)
	d.SetHubGit(func() (http.RoundTripper, error) {
		return roundTripFunc(func(r *http.Request) (*http.Response, error) {
			u, _ := url.Parse(srv.URL)
			out := r.Clone(r.Context())
			out.URL.Scheme, out.URL.Host, out.Host = u.Scheme, u.Host, u.Host
			return http.DefaultTransport.RoundTrip(out)
		}), nil
	})
	fw := httptest.NewServer(d.HubRemote())
	t.Cleanup(fw.Close)
	d.SetAgentAddr(fw.Listener.Addr())
	cullGit(t, clone, "config", "--unset", "remote.hub.url")
	cullGit(t, clone, "config", "extensions.worktreeConfig", "true")
	cullGit(t, clone, "config", "--worktree", "remote.hub.url", strings.Replace(d.HubRemoteURL("github/o/r"), "127.0.0.1:", "127.0.0.1:1", 1))
	cullGit(t, clone, "config", "protocol.version", "0")
	req := shared.LaunchRequest{Repo: clone, Title: "Fresh Start"}
	_, dd, err := d.LaunchHome(store.CardHomeWorktree, &req)
	if err != nil {
		t.Fatal(err)
	}
	if got := cullGit(t, dd.Work, "rev-parse", "HEAD"); got != tip {
		t.Fatalf("started from %s, the hub's claude/main is %s", got, tip)
	}
}

func TestALaunchWithNoBaseFetchesThroughItsOwnForwarderWhenTheCloneHoldsAStaleTokenHeader(t *testing.T) {
	t.Parallel()
	d, _ := homeRoom(t)
	clone, hub := hubClone(t)
	tip := cullGit(t, hub, "-c", "user.name=t", "-c", "user.email=t@t", "commit-tree", "-m", "ahead", "-p", "claude/main", "claude/main^{tree}")
	cullGit(t, hub, "update-ref", "refs/heads/claude/main", tip)
	srv := httptest.NewServer(uploadPackHub{repo: hub})
	t.Cleanup(srv.Close)
	d.SetHubGit(func() (http.RoundTripper, error) {
		return roundTripFunc(func(r *http.Request) (*http.Response, error) {
			u, _ := url.Parse(srv.URL)
			out := r.Clone(r.Context())
			out.URL.Scheme, out.URL.Host, out.Host = u.Scheme, u.Host, u.Host
			return http.DefaultTransport.RoundTrip(out)
		}), nil
	})
	fw := httptest.NewServer(d.HubRemote())
	t.Cleanup(fw.Close)
	d.SetAgentAddr(fw.Listener.Addr())
	own := d.HubRemoteURL("github/o/r")
	cullGit(t, clone, "remote", "set-url", "hub", own)
	cullGit(t, clone, "config", "--add", "http."+own[:strings.Index(own, "/git/")+len("/git/")]+".extraHeader", "X-Atrium-Card-Token: gone.00")
	cullGit(t, clone, "config", "protocol.version", "0")
	req := shared.LaunchRequest{Repo: clone, Title: "Fresh Start"}
	_, dd, err := d.LaunchHome(store.CardHomeWorktree, &req)
	if err != nil {
		t.Fatal(err)
	}
	if got := cullGit(t, dd.Work, "rev-parse", "HEAD"); got != tip {
		t.Fatalf("started from %s, the hub's claude/main is %s", got, tip)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
