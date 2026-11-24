//go:build integration

package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/api"
	"github.com/dovholuknf/atrium/internal/base/testwait"
	"github.com/dovholuknf/atrium/internal/daemon/home"
	"github.com/dovholuknf/atrium/internal/daemon/shared"
	"github.com/dovholuknf/atrium/internal/small"
	"github.com/dovholuknf/atrium/internal/store"
)

func endCard(t *testing.T, d *Daemon, id, status string) {
	t.Helper()
	if err := d.st.SetStatus(id, status); err != nil {
		t.Fatal(err)
	}
}

func sweptPaths(out []FolderSwept) string {
	var p []string
	for _, s := range out {
		p = append(p, s.Path)
	}
	return strings.Join(p, ", ")
}

func homeGone(t *testing.T, d *Daemon, row store.CardHome, dd home.HomeDirs) {
	t.Helper()
	if _, err := os.Stat(dd.Dir); !os.IsNotExist(err) {
		t.Fatalf("the home %s is still there", dd.Dir)
	}
	if got, _, _ := d.st.CardHomeByPath(row.Path); got.State != store.CardHomeRemoved {
		t.Fatalf("row %+v", got)
	}
}

func homeStays(t *testing.T, dd home.HomeDirs) {
	t.Helper()
	if _, err := os.Stat(dd.Dir); err != nil {
		t.Fatalf("the home %s went: %v", dd.Dir, err)
	}
}

func TestTheSweepRemovesAnArchivedCardsHomeAndBranch(t *testing.T) {
	t.Parallel()
	d, _ := homeRoom(t)
	repo := homeRepo(t)
	task, row, dd := worktreeHome(t, d, repo)
	endCard(t, d, task.ID, store.StatusDone)
	if err := archiveEnded(d, task.ID); err != nil {
		t.Fatal(err)
	}
	if out := d.sweepEndedFolders(context.Background()); len(out) != 1 || out[0].Card != task.ID {
		t.Fatalf("swept %q", sweptPaths(out))
	}
	homeGone(t, d, row, dd)
	if got := cullGit(t, repo, "branch", "--list", "claude/fix-the-thing"); got != "" {
		t.Fatalf("the branch is left: %q", got)
	}
}

func TestTheSweepRemovesAClosedCardsHomeAtOnce(t *testing.T) {
	t.Parallel()
	d, _ := homeRoom(t)
	task, row, dd := worktreeHome(t, d, homeRepo(t))
	endCard(t, d, task.ID, store.StatusDone)
	if err := d.st.SetTags(task.ID, []string{api.ClosedTag}); err != nil {
		t.Fatal(err)
	}
	if err := d.st.BackdateActivity(task.ID, WokenGuard+time.Minute); err != nil {
		t.Fatal(err)
	}
	d.sweepEndedFolders(context.Background())
	homeGone(t, d, row, dd)
}

func TestADoneOrDeadCardKeepsItsHomeForTheGraceThenLosesIt(t *testing.T) {
	t.Parallel()
	d, _ := homeRoom(t)
	repo := homeRepo(t)
	done, doneRow, doneDirs := worktreeHome(t, d, repo)
	endCard(t, d, done.ID, store.StatusDone)
	req := homeLaunch(t, d, repo, "second")
	dead, deadRow, deadDirs := req.task, req.row, req.dd
	endCard(t, d, dead.ID, store.StatusDead)

	d.sweepEndedFolders(context.Background())
	homeStays(t, doneDirs)
	homeStays(t, deadDirs)

	for _, id := range []string{done.ID, dead.ID} {
		if err := d.st.SetTags(id, []string{api.ClosedTag}); err != nil {
			t.Fatal(err)
		}
		if err := d.st.BackdateActivity(id, EndedFolderGrace+time.Hour); err != nil {
			t.Fatal(err)
		}
	}
	d.sweepEndedFolders(context.Background())
	homeGone(t, d, doneRow, doneDirs)
	homeGone(t, d, deadRow, deadDirs)
}

func TestARunningOrPinnedCardKeepsItsHome(t *testing.T) {
	t.Parallel()
	d, _ := homeRoom(t)
	repo := homeRepo(t)
	running, _, runDirs := worktreeHome(t, d, repo)
	endCard(t, d, running.ID, store.StatusRunning)
	pinned := homeLaunch(t, d, repo, "pinned")
	endCard(t, d, pinned.task.ID, store.StatusDone)
	if err := d.st.SetPinned(pinned.task.ID, true); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{running.ID, pinned.task.ID} {
		if err := d.st.BackdateActivity(id, EndedFolderGrace+time.Hour); err != nil {
			t.Fatal(err)
		}
	}
	if out := d.sweepEndedFolders(context.Background()); len(out) != 0 {
		t.Fatalf("swept %q", sweptPaths(out))
	}
	homeStays(t, runDirs)
	homeStays(t, pinned.dd)
}

func TestAHomeWhoseCardIsGoneIsRemoved(t *testing.T) {
	t.Parallel()
	d, _ := homeRoom(t)
	row, dd, err := d.MakeHome(store.CardHomeScratch, "lost", "w")
	if err != nil {
		t.Fatal(err)
	}
	d.sweepEndedFolders(context.Background())
	homeStays(t, dd)
	none := func(string, bool) ([]*store.Task, bool) { return nil, true }
	d.sweepHome(context.Background(), row, none, nil, time.Now().Add(WokenGuard+time.Minute))
	homeGone(t, d, row, dd)
}

func TestTwoRepoLaunchesAtOnceKeepTheirCheckoutsThroughASweep(t *testing.T) {
	t.Parallel()
	d, _ := homeRoom(t)
	repo := homeRepo(t)
	var wg sync.WaitGroup
	dds := make([]home.HomeDirs, 2)
	errs := make([]error, 2)
	for i, title := range []string{"bug launch missing cwd", "u failed paste toast"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := shared.LaunchRequest{Repo: repo, Title: title}
			_, dds[i], errs[i] = d.LaunchHome(store.CardHomeWorktree, &req)
		}()
	}
	wg.Wait()
	for i := range errs {
		if errs[i] != nil {
			t.Fatal(errs[i])
		}
	}
	if out := d.sweepEndedFolders(context.Background()); len(out) != 0 {
		t.Fatalf("the sweep took homes still being launched: %s", sweptPaths(out))
	}
	for _, dd := range dds {
		if !shared.HasDotGit(dd.Work) {
			t.Fatalf("%s is not a checkout", dd.Work)
		}
	}
}

func TestUnsavedWorkIsStashedBeforeTheHomeGoes(t *testing.T) {
	t.Parallel()
	d, _ := homeRoom(t)
	repo := homeRepo(t)
	task, row, dd := worktreeHome(t, d, repo)
	endCard(t, d, task.ID, store.StatusDone)
	if err := archiveEnded(d, task.ID); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dd.Work, "wip.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	var asked []string
	d.sweepStash = func(_ context.Context, card, dir, branch string) (string, string, error) {
		if _, err := os.Stat(filepath.Join(filepath.FromSlash(dir), "wip.txt")); err != nil {
			t.Errorf("the stash ran after the work went: %v", err)
		}
		asked = append(asked, card+" "+branch)
		return "stash/" + cardShortID(card) + "/" + branch, "", nil
	}
	out := d.sweepEndedFolders(context.Background())
	if len(asked) != 1 || asked[0] != task.ID+" claude/fix-the-thing" {
		t.Fatalf("stashes %q", asked)
	}
	if len(out) != 1 || !strings.HasPrefix(out[0].Stashed, "stash/") {
		t.Fatalf("swept %+v", out)
	}
	homeGone(t, d, row, dd)
}

func TestAStashThatDoesNotLandKeepsTheHome(t *testing.T) {
	t.Parallel()
	d, _ := homeRoom(t)
	task, row, dd := worktreeHome(t, d, homeRepo(t))
	endCard(t, d, task.ID, store.StatusDone)
	if err := archiveEnded(d, task.ID); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dd.Work, "wip.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	d.sweepStash = func(context.Context, string, string, string) (string, string, error) {
		return "", "", os.ErrPermission
	}
	if out := d.sweepEndedFolders(context.Background()); len(out) != 0 {
		t.Fatalf("swept %q", sweptPaths(out))
	}
	if _, err := os.Stat(filepath.Join(dd.Work, "wip.txt")); err != nil {
		t.Fatal("the work went")
	}
	if got, _, _ := d.st.CardHomeByPath(row.Path); got.State != store.CardHomeKept || !strings.Contains(got.Reason, "stash") {
		t.Fatalf("row %+v", got)
	}
}

func TestTheSweepRemovesEndedWorktreesBesideTheClone(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	base := t.TempDir()
	clone := filepath.Join(base, "repo")
	if err := os.MkdirAll(clone, 0o755); err != nil {
		t.Fatal(err)
	}
	cullGit(t, clone, "init", "-q", "-b", "main")
	cullGit(t, clone, "commit", "-q", "--allow-empty", "-m", "root")
	cullGit(t, clone, "branch", "claude/main")
	root := filepath.Join(base, "repo-worktrees")
	add := func(name string) string {
		dir := filepath.Join(root, name)
		cullGit(t, clone, "worktree", "add", "-q", "-b", "claude/"+name, dir, "claude/main")
		return dir
	}
	ended, live, orphanDirty, foreign := add("ended"), add("live"), add("orphan"), filepath.Join(root, "notes")
	if err := os.MkdirAll(foreign, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(orphanDirty, "wip.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	e := dirCard(t, d, "ended", ended)
	endCard(t, d, e.ID, store.StatusDone)
	if err := archiveEnded(d, e.ID); err != nil {
		t.Fatal(err)
	}
	l := dirCard(t, d, "live", live)
	endCard(t, d, l.ID, store.StatusRunning)

	out := d.sweepEndedFolders(context.Background())
	if len(out) != 1 || !strings.EqualFold(out[0].Path, filepath.ToSlash(ended)) {
		t.Fatalf("swept %q", sweptPaths(out))
	}
	if _, err := os.Stat(ended); !os.IsNotExist(err) {
		t.Fatal("the ended card's worktree is still there")
	}
	if got := cullGit(t, clone, "branch", "--list", "claude/ended"); got != "" {
		t.Fatalf("its branch is left: %q", got)
	}
	for _, keep := range []string{live, orphanDirty, foreign} {
		if _, err := os.Stat(keep); err != nil {
			t.Fatalf("%s went", keep)
		}
	}
}

func TestALiveCardAtTheWorktreeRootKeepsOnlyUnclaimedWorktrees(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	base := t.TempDir()
	clone := filepath.Join(base, "repo")
	if err := os.MkdirAll(clone, 0o755); err != nil {
		t.Fatal(err)
	}
	cullGit(t, clone, "init", "-q", "-b", "main")
	cullGit(t, clone, "commit", "-q", "--allow-empty", "-m", "root")
	cullGit(t, clone, "branch", "claude/main")
	root := filepath.Join(base, "repo-worktrees")
	add := func(name string) string {
		dir := filepath.Join(root, name)
		cullGit(t, clone, "worktree", "add", "-q", "-b", "claude/"+name, dir, "claude/main")
		return dir
	}
	ended, fresh := add("ended"), add("fresh")
	e := dirCard(t, d, "ended", ended)
	endCard(t, d, e.ID, store.StatusDone)
	if err := archiveEnded(d, e.ID); err != nil {
		t.Fatal(err)
	}
	l := dirCard(t, d, "setup", root)
	endCard(t, d, l.ID, store.StatusRunning)

	out := d.sweepEndedFolders(context.Background())
	if len(out) != 1 || !strings.EqualFold(out[0].Path, filepath.ToSlash(ended)) {
		t.Fatalf("swept %q", sweptPaths(out))
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatalf("the unclaimed worktree went: %v", err)
	}
}

func TestTheRoomSweepsOnStart(t *testing.T) {
	t.Parallel()
	d, _ := homeRoom(t)
	task, row, dd := worktreeHome(t, d, homeRepo(t))
	endCard(t, d, task.ID, store.StatusDone)
	if err := archiveEnded(d, task.ID); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		d.folderSweeper(ctx, 0, time.Hour)
	}()
	testwait.PollEvery(testwait.HTTP, 30*time.Second, func() bool {
		got, _, _ := d.st.CardHomeByPath(row.Path)
		return got.State == store.CardHomeRemoved
	})
	cancel()
	<-stopped
	homeGone(t, d, row, dd)
}

type launchedHome struct {
	task *store.Task
	row  store.CardHome
	dd   home.HomeDirs
}

func homeLaunch(t *testing.T, d *Daemon, repo, title string) launchedHome {
	t.Helper()
	req := shared.LaunchRequest{Repo: repo, Title: title}
	row, dd, err := d.LaunchHome(store.CardHomeWorktree, &req)
	if err != nil {
		t.Fatal(err)
	}
	task, _, err := d.st.Register(store.Observed{WireName: title, Worktree: filepath.ToSlash(dd.Work), Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.st.CardHomeCard(row.Path, task.ID); err != nil {
		t.Fatal(err)
	}
	row, _, _ = d.st.CardHomeByPath(row.Path)
	return launchedHome{task, row, dd}
}

func dirCard(t *testing.T, d *Daemon, name, dir string) *store.Task {
	t.Helper()
	task, _, err := d.st.Register(store.Observed{WireName: name, Worktree: filepath.ToSlash(dir), Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	return task
}

func archiveEnded(d *Daemon, id string) error {
	if err := d.st.ArchiveCulled(id, "test"); err != nil {
		return err
	}
	return d.st.BackdateActivity(id, WokenGuard+time.Minute)
}

func TestACardWokenOrBeingResumedKeepsItsFolder(t *testing.T) {
	t.Parallel()
	d, _ := homeRoom(t)
	repo := homeRepo(t)
	woken, _, wokenDirs := worktreeHome(t, d, repo)
	resuming := homeLaunch(t, d, repo, "resuming")
	active := homeLaunch(t, d, repo, "active")
	for _, id := range []string{woken.ID, resuming.task.ID, active.task.ID} {
		endCard(t, d, id, store.StatusDone)
		if err := archiveEnded(d, id); err != nil {
			t.Fatal(err)
		}
	}
	d.startedAt.Store(woken.ID, time.Now())
	if err := d.st.Touch(active.task.ID); err != nil {
		t.Fatal(err)
	}
	unlock := d.launching.Lock(small.LaunchKeys(resuming.task.ID, "")...)
	if out := d.sweepEndedFolders(context.Background()); len(out) != 0 {
		unlock()
		t.Fatalf("swept %q", sweptPaths(out))
	}
	homeStays(t, wokenDirs)
	homeStays(t, resuming.dd)
	homeStays(t, active.dd)

	unlock()
	d.startedAt.Store(woken.ID, time.Now().Add(-WokenGuard-time.Minute))
	if out := d.sweepEndedFolders(context.Background()); len(out) != 2 {
		t.Fatalf("swept %q, want the woken and the resuming card's folders", sweptPaths(out))
	}
	homeStays(t, active.dd)
}

func TestCheckoutsInsideAHomeAreStashedAndReaped(t *testing.T) {
	t.Parallel()
	d, _ := homeRoom(t)
	repo := homeRepo(t)
	task, row, dd := worktreeHome(t, d, repo)
	endCard(t, d, task.ID, store.StatusDone)
	if err := archiveEnded(d, task.ID); err != nil {
		t.Fatal(err)
	}
	side := filepath.Join(dd.Tmp, "side")
	cullGit(t, repo, "worktree", "add", "-q", "-b", "claude/side", side)
	if err := os.WriteFile(filepath.Join(side, "wip.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	cullGit(t, dd.Tmp, "clone", "-q", repo, "clean")
	var asked []string
	d.sweepStash = func(_ context.Context, card, dir, branch string) (string, string, error) {
		asked = append(asked, filepath.Base(filepath.FromSlash(dir))+" "+branch)
		return "stash/" + cardShortID(card) + "/" + branch, "", nil
	}
	out := d.sweepEndedFolders(context.Background())
	if len(asked) != 1 || asked[0] != "side claude/side" {
		t.Fatalf("stashes %q", asked)
	}
	if len(out) != 1 || !strings.Contains(out[0].Stashed, "claude/side") {
		t.Fatalf("swept %+v", out)
	}
	homeGone(t, d, row, dd)
	if got := cullGit(t, repo, "worktree", "list", "--porcelain"); strings.Contains(got, "claude/side") {
		t.Fatalf("the side worktree is still registered:\n%s", got)
	}
}

func TestACheckoutInsideAHomeWhoseStashFailsKeepsTheHome(t *testing.T) {
	t.Parallel()
	d, _ := homeRoom(t)
	repo := homeRepo(t)
	task, row, dd := worktreeHome(t, d, repo)
	endCard(t, d, task.ID, store.StatusDone)
	if err := archiveEnded(d, task.ID); err != nil {
		t.Fatal(err)
	}
	cullGit(t, dd.Tmp, "clone", "-q", repo, "mine")
	cullGit(t, filepath.Join(dd.Tmp, "mine"), "commit", "-q", "--allow-empty", "-m", "local only")
	d.sweepStash = func(context.Context, string, string, string) (string, string, error) {
		return "", "", os.ErrPermission
	}
	if out := d.sweepEndedFolders(context.Background()); len(out) != 0 {
		t.Fatalf("swept %q", sweptPaths(out))
	}
	homeStays(t, dd)
	if got, _, _ := d.st.CardHomeByPath(row.Path); got.State != store.CardHomeKept || !strings.Contains(got.Reason, "tmp/mine") {
		t.Fatalf("row %+v", got)
	}
}

func scratchHome(t *testing.T, d *Daemon, name string, files bool) (*store.Task, store.CardHome, home.HomeDirs) {
	t.Helper()
	row, dd, err := d.MakeHome(store.CardHomeScratch, name, "w")
	if err != nil {
		t.Fatal(err)
	}
	task := dirCard(t, d, name, dd.Work)
	if err := d.st.CardHomeCard(row.Path, task.ID); err != nil {
		t.Fatal(err)
	}
	if files {
		if err := os.WriteFile(filepath.Join(dd.Work, "notes.txt"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	endCard(t, d, task.ID, store.StatusDone)
	if err := archiveEnded(d, task.ID); err != nil {
		t.Fatal(err)
	}
	row, _, _ = d.st.CardHomeByPath(row.Path)
	return task, row, dd
}

func TestAClosedCardsScratchHomeWithFilesGoes(t *testing.T) {
	t.Parallel()
	d, _ := homeRoom(t)
	task, row, dd := scratchHome(t, d, "closed", true)
	if err := d.st.SetTags(task.ID, []string{api.ClosedTag}); err != nil {
		t.Fatal(err)
	}
	d.sweepEndedFolders(context.Background())
	homeGone(t, d, row, dd)
}

func TestASelfEndedCardKeepsAScratchHomeWithFiles(t *testing.T) {
	t.Parallel()
	d, _ := homeRoom(t)
	_, row, dd := scratchHome(t, d, "kept", true)
	_, emptyRow, emptyDirs := scratchHome(t, d, "empty", false)
	if out := d.sweepEndedFolders(context.Background()); len(out) != 1 || out[0].Path != emptyRow.Path {
		t.Fatalf("swept %q, want only the empty home", sweptPaths(out))
	}
	if _, err := os.Stat(filepath.Join(dd.Work, "notes.txt")); err != nil {
		t.Fatalf("the files went: %v", err)
	}
	if got, _, _ := d.st.CardHomeByPath(row.Path); got.State != store.CardHomeKept || !strings.Contains(got.Reason, "ended by itself") {
		t.Fatalf("row %+v", got)
	}
	homeGone(t, d, emptyRow, emptyDirs)
}

func TestAFinishedWorkersHomeGoesAfterTheShortGrace(t *testing.T) {
	t.Parallel()
	d, _ := homeRoom(t)
	repo := homeRepo(t)
	worker, row, dd := worktreeHome(t, d, repo)
	kept := homeLaunch(t, d, repo, "kept open")
	for _, c := range []struct {
		id   string
		tags []string
	}{{worker.ID, []string{OriginAgentTag, SubagentTag, api.ClosedTag}}, {kept.task.ID, []string{OriginAgentTag, SubagentTag, KeepOpenTag}}} {
		if err := d.st.SetTags(c.id, c.tags); err != nil {
			t.Fatal(err)
		}
		task, _ := d.st.Get(c.id)
		if _, err := d.st.CreateWorkItem(task, store.NewWorkItem{Brief: "do it"}); err != nil {
			t.Fatal(err)
		}
		if _, err := d.st.RecordReport(store.ReportWrite{TaskID: c.id, ReportStatus: ReportDone,
			MoveStatus: true, Status: store.StatusDone}); err != nil {
			t.Fatal(err)
		}
		if err := d.st.BackdateActivity(c.id, FinishedWorkerGrace+time.Minute); err != nil {
			t.Fatal(err)
		}
	}
	d.sweepEndedFolders(context.Background())
	homeGone(t, d, row, dd)
	homeStays(t, kept.dd)
}

func TestAHomeWhoseWorkIsAnEmptyFolderGoes(t *testing.T) {
	t.Parallel()
	d, _ := homeRoom(t)
	repo := homeRepo(t)
	task, row, dd := worktreeHome(t, d, repo)
	endCard(t, d, task.ID, store.StatusDone)
	if err := archiveEnded(d, task.ID); err != nil {
		t.Fatal(err)
	}
	cullGit(t, repo, "worktree", "remove", "--force", dd.Work)
	if err := os.MkdirAll(dd.Work, 0o755); err != nil {
		t.Fatal(err)
	}
	d.sweepStash = func(context.Context, string, string, string) (string, string, error) {
		t.Error("an empty work/ was stashed")
		return "", "", os.ErrPermission
	}
	d.sweepEndedFolders(context.Background())
	homeGone(t, d, row, dd)
}

func TestAFinishedUnclosedCardKeepsAnIgnoredMock(t *testing.T) {
	t.Parallel()
	d, _ := homeRoom(t)
	task, _, dd := worktreeHome(t, d, homeRepo(t))
	endCard(t, d, task.ID, store.StatusDone)
	mock := filepath.Join(dd.Work, "build.claude", "bringup-mock", "index.html")
	if err := os.MkdirAll(filepath.Dir(mock), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mock, []byte("<html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dd.Work, ".gitignore"), []byte("build.claude/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := d.st.BackdateActivity(task.ID, EndedFolderGrace+time.Hour); err != nil {
		t.Fatal(err)
	}
	if out := d.sweepEndedFolders(context.Background()); len(out) != 0 {
		t.Fatalf("swept %q", sweptPaths(out))
	}
	if _, err := os.Stat(mock); err != nil {
		t.Fatalf("the mock went: %v", err)
	}
}

func TestFinishingNeverFreesAPinnedCardOrOneHoldingAnIgnoredFile(t *testing.T) {
	t.Parallel()
	d, _ := homeRoom(t)
	repo := homeRepo(t)
	mock, _, mockDirs := worktreeHome(t, d, repo)
	pinned := homeLaunch(t, d, repo, "pinned")
	plain := homeLaunch(t, d, repo, "plain")
	for _, id := range []string{mock.ID, pinned.task.ID, plain.task.ID} {
		if err := d.st.SetTags(id, []string{OriginAgentTag, SubagentTag}); err != nil {
			t.Fatal(err)
		}
		task, _ := d.st.Get(id)
		if _, err := d.st.CreateWorkItem(task, store.NewWorkItem{Brief: "do it"}); err != nil {
			t.Fatal(err)
		}
		if _, err := d.st.RecordReport(store.ReportWrite{TaskID: id, ReportStatus: ReportDone,
			MoveStatus: true, Status: store.StatusDone}); err != nil {
			t.Fatal(err)
		}
	}
	if err := d.st.SetPinned(pinned.task.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mockDirs.Work, ".gitignore"), []byte("build.claude/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(mockDirs.Work, "build.claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mockDirs.Work, "build.claude", "mock.html"), []byte("<html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]string{mock.ID: "ignored", pinned.task.ID: "pinned"} {
		task, _ := d.st.Get(id)
		if got := d.reclaimBlockIdle(task); !strings.Contains(got, want) {
			t.Errorf("reclaimBlockIdle(%s) = %q, want it to say %q", id, got, want)
		}
	}
	task, _ := d.st.Get(plain.task.ID)
	if got := d.reclaimBlockIdle(task); got != "" {
		t.Errorf("a plain finished card is blocked: %q", got)
	}
}

func TestAClosedCardWithAnEmptyWorkFolderIsFreed(t *testing.T) {
	t.Parallel()
	d, _ := homeRoom(t)
	repo := homeRepo(t)
	task, row, dd := worktreeHome(t, d, repo)
	endCard(t, d, task.ID, store.StatusDone)
	if err := d.st.SetTags(task.ID, []string{api.ClosedTag}); err != nil {
		t.Fatal(err)
	}
	if err := d.st.BackdateActivity(task.ID, WokenGuard+time.Minute); err != nil {
		t.Fatal(err)
	}
	cullGit(t, repo, "worktree", "remove", "--force", dd.Work)
	if err := os.MkdirAll(dd.Work, 0o755); err != nil {
		t.Fatal(err)
	}
	d.sweepEndedFolders(context.Background())
	homeGone(t, d, row, dd)
}

func TestAnUntrackedBriefIsNotWorkToStash(t *testing.T) {
	t.Parallel()
	d, _ := homeRoom(t)
	task, row, dd := worktreeHome(t, d, homeRepo(t))
	endCard(t, d, task.ID, store.StatusDone)
	if err := archiveEnded(d, task.ID); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dd.Work, "BRIEF.md"), []byte("the job"), 0o644); err != nil {
		t.Fatal(err)
	}
	d.sweepStash = func(context.Context, string, string, string) (string, string, error) {
		t.Error("BRIEF.md was stashed as work")
		return "", "", os.ErrPermission
	}
	d.sweepEndedFolders(context.Background())
	homeGone(t, d, row, dd)
}

func TestTheAuditCountsACardHomesWorktreeAsAtriums(t *testing.T) {
	t.Parallel()
	d, _ := homeRoom(t)
	task, _, dd := worktreeHome(t, d, homeRepo(t))
	endCard(t, d, task.ID, store.StatusDone)
	if err := archiveEnded(d, task.ID); err != nil {
		t.Fatal(err)
	}
	if j := d.worktreeJudge()(filepath.ToSlash(dd.Work)); !j.Atriums {
		t.Fatalf("judged %+v", j)
	}
}
