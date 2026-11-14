package daemon

import (
	"context"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/dovholuknf/atrium/internal/api"
	"github.com/dovholuknf/atrium/internal/base/gitexec"
	"github.com/dovholuknf/atrium/internal/base/safepath"
	"github.com/dovholuknf/atrium/internal/daemon/home"
	"github.com/dovholuknf/atrium/internal/daemon/shared"
	"github.com/dovholuknf/atrium/internal/small"
	"github.com/dovholuknf/atrium/internal/store"
	"github.com/dovholuknf/atrium/internal/worktrees"
)

// The folder sweep removes every folder of an ended card under the card root and each <clone>-worktrees root, after
// stashing work nowhere else to the hub. A stash that does not land keeps the folder. A scratch home whose card ended by
// itself, not closed by the operator, stays while it has no git and its work/ holds files.

// FolderSweepEvery is how often the sweep runs, and FolderSweepStart how long after the room starts it first runs.
var (
	FolderSweepEvery = 10 * time.Minute
	FolderSweepStart = time.Minute
)

// EndedFolderGrace is how long a done or dead card that is still on the board keeps its folder, the window in which
// a launcher sends the work back. An archived card has none.
var EndedFolderGrace = ArchiveWorkersDefaultAfter

// WokenGuard is how long after a card was woken, resumed or last active its folder is safe from the sweep.
var WokenGuard = 10 * time.Minute

var sweepKept sync.Map

// FolderSwept is one folder a sweep removed.
type FolderSwept struct {
	Path    string `json:"path"`
	Card    string `json:"card,omitempty"`
	Bytes   int64  `json:"bytes"`
	Stashed string `json:"stashed,omitempty"`
}

func (d *Daemon) folderSweeper(ctx context.Context, start, every time.Duration) {
	if d.reattached != nil {
		select {
		case <-ctx.Done():
			return
		case <-d.reattached:
		}
	}
	wait := start
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = every
		d.sweepEndedFolders(ctx)
	}
}

// cardEnded says whether a card's folder may go. nil is a card the room has no row for.
func (d *Daemon) cardEnded(t *store.Task, fixtures map[string]bool, now time.Time) bool {
	if t == nil {
		return true
	}
	if fixtures[t.ID] || d.wokenLately(t, now) || d.liveness(t) == store.Live || d.Nctx.Holding(t.ID) {
		return false
	}
	switch t.Status {
	case store.StatusRunning, store.StatusNeedsInput, store.StatusNeedsPermission:
		return t.ArchivedAt != nil && d.liveness(t) == store.Gone
	}
	if t.ArchivedAt != nil || small.HasTag(t.Tags, api.ClosedTag) {
		return true
	}
	if t.Pinned || (t.Status != store.StatusDone && t.Status != store.StatusDead) {
		return false
	}
	return now.Sub(t.LastActivityAt) >= EndedFolderGrace
}

func (d *Daemon) wokenLately(t *store.Task, now time.Time) bool {
	if now.Sub(t.LastActivityAt) < WokenGuard {
		return true
	}
	if v, ok := d.startedAt.Load(t.ID); ok {
		if at, ok := v.(time.Time); ok && now.Sub(at) < WokenGuard {
			return true
		}
	}
	return false
}

// holdCards takes the launch lock of every card, so no resume starts while their folder goes, and re-reads each
// under it. False when a launch holds one now or a card is no longer ended.
func (d *Daemon) holdCards(cards []*store.Task, fixtures map[string]bool) (func(), bool) {
	var keys []string
	for _, t := range cards {
		if t != nil {
			keys = append(keys, small.LaunchKeys(t.ID, t.ResumeID)...)
		}
	}
	release, ok := d.launching.TryLock(keys...)
	if !ok {
		return nil, false
	}
	now := time.Now()
	for _, t := range cards {
		if t == nil {
			continue
		}
		fresh, err := d.st.Get(t.ID)
		if err != nil || !d.cardEnded(fresh, fixtures, now) {
			release()
			return nil, false
		}
	}
	return release, true
}

// sweepEndedFolders removes every folder of an ended card under the card root and the worktree roots, and logs what
// it freed.
func (d *Daemon) sweepEndedFolders(ctx context.Context) []FolderSwept {
	live, err := d.st.List()
	if err != nil {
		log.Printf("[atrium] folder sweep: %v", err)
		return nil
	}
	archived, err := d.st.ListArchived(1 << 20)
	if err != nil {
		log.Printf("[atrium] folder sweep: %v", err)
		return nil
	}
	tasks := append(live, archived...)
	fixtures := map[string]bool{}
	if fx, err := d.st.Fixtures(); err == nil {
		for _, f := range fx {
			fixtures[f.TaskID] = true
		}
	}
	now := time.Now()
	users := func(path string, exact bool) (all []*store.Task, ended bool) {
		ended = true
		for _, t := range tasks {
			if strings.TrimSpace(t.Worktree) == "" {
				continue
			}
			if worktrees.SamePath(t.Worktree, path) || (!exact && insideRoot(filepath.FromSlash(path), t.Worktree)) {
				all = append(all, t)
				if !d.cardEnded(t, fixtures, now) {
					ended = false
				}
			}
		}
		return all, ended
	}

	var out []FolderSwept
	if rows, err := d.st.CardHomesOpen(); err == nil {
		for _, row := range rows {
			if ctx.Err() != nil {
				return out
			}
			if row.Kind == store.CardHomeTask {
				continue
			}
			if s, ok := d.sweepHome(ctx, row, users, fixtures, now); ok {
				out = append(out, s)
			}
		}
	}
	for _, root := range worktreeRoots(tasks) {
		if ctx.Err() != nil {
			return out
		}
		out = append(out, d.sweepWorktreeRoot(ctx, root, users)...)
	}
	if len(out) > 0 {
		var bytes int64
		for _, s := range out {
			bytes += s.Bytes
		}
		log.Printf("[atrium] folder sweep freed %d folder(s) of ended cards, %s", len(out), humanBytes(bytes))
		d.ap.Broadcast("task-removed", nil)
	}
	return out
}

func (d *Daemon) sweepHome(ctx context.Context, row store.CardHome, users func(string, bool) ([]*store.Task, bool),
	fixtures map[string]bool, now time.Time) (FolderSwept, bool) {
	var t *store.Task
	if row.Card != "" {
		if got, err := d.st.Get(row.Card); err == nil {
			t = got
		}
	}
	if !d.cardEnded(t, fixtures, now) {
		return FolderSwept{}, false
	}
	cards, ended := users(row.Path, false)
	if !ended {
		return FolderSwept{}, false
	}
	release, ok := d.holdCards(append(cards, t), fixtures)
	if !ok {
		d.sweepKeep(row.Path, "a launch or resume holds its card")
		return FolderSwept{}, false
	}
	defer release()
	if _, err := os.Lstat(filepath.FromSlash(row.Path)); os.IsNotExist(err) {
		_ = d.st.CardHomeSet(row.Path, store.CardHomeRemoved, "the folder was already gone", row.Owed)
		return FolderSwept{}, false
	}
	dd := home.DirsOf(row.Path)
	if row.Kind == store.CardHomeScratch && (t == nil || !small.HasTag(t.Tags, api.ClosedTag)) && filesNoGit(dd) {
		const why = "its card ended by itself and work/ holds files with no git"
		d.sweepKeep(row.Path, why)
		if row.State != store.CardHomeKept || row.Reason != why {
			_ = d.st.CardHomeSet(row.Path, store.CardHomeKept, why, row.Owed)
		}
		return FolderSwept{}, false
	}
	force, stashed, branch := false, "", ""
	if row.Kind == store.CardHomeWorktree {
		var why string
		why, branch, _ = d.HomeWorkState(row, dd)
		if branch == "" {
			branch, _ = shared.GitIn(dd.Work, gitexec.SymbolicRef{Name: "HEAD", Short: true})
		}
		if why != "" {
			name, err := d.sweepStashOf(ctx, t, dd.Work, branch, why)
			if err != nil {
				d.sweepKeep(row.Path, err.Error())
				if row.State != store.CardHomeKept || row.Reason != err.Error() {
					_ = d.st.CardHomeSet(row.Path, store.CardHomeKept, err.Error(), row.Owed)
				}
				return FolderSwept{}, false
			}
			force, stashed = true, name
		}
	}
	more, clones, err := d.sweepCheckouts(ctx, t, row, dd)
	if err != nil {
		d.sweepKeep(row.Path, err.Error())
		if row.State != store.CardHomeKept || row.Reason != err.Error() {
			_ = d.st.CardHomeSet(row.Path, store.CardHomeKept, err.Error(), row.Owed)
		}
		return FolderSwept{}, false
	}
	stashed = strings.Join(append(strings.Fields(stashed), more...), " ")
	bytes := home.HomeBytes(row.Path)
	removed, kept, br, gone := d.ReclaimHomeBranch(t, row, force)
	if !removed {
		d.sweepKeep(row.Path, kept)
		return FolderSwept{}, false
	}
	for _, clone := range clones {
		_, _ = shared.GitIn(clone, gitexec.WorktreePrune{})
	}
	if br == "" {
		br = branch
	}
	if !gone && br != "" {
		if clone := d.HomeClone(row, dd); clone != "" && !shared.ProtectedBranch(br, shared.DefaultCullInto) {
			_, _ = shared.GitIn(clone, gitexec.BranchDelete{Branch: br})
		}
	}
	return d.sweptOne(row.Path, row.Card, bytes, stashed), true
}

// filesNoGit says whether a home has a file under work/ and no .git anywhere.
func filesNoGit(dd home.HomeDirs) bool {
	files, git := false, false
	_ = filepath.WalkDir(dd.Work, func(_ string, e fs.DirEntry, err error) error {
		if err == nil && !e.IsDir() {
			files = true
			return fs.SkipAll
		}
		return nil
	})
	_ = filepath.WalkDir(dd.Dir, func(_ string, e fs.DirEntry, err error) error {
		if err == nil && e.Name() == ".git" {
			git = true
			return fs.SkipAll
		}
		return nil
	})
	return files && !git
}

// sweepCheckouts stashes the unsaved work of every git checkout a card made inside its home beside its own work/, so
// removing the home loses nothing. It answers the stash names and the clones of the linked worktrees among them,
// which want a prune once the home is gone. An error keeps the home.
func (d *Daemon) sweepCheckouts(ctx context.Context, t *store.Task, row store.CardHome, dd home.HomeDirs) ([]string, []string, error) {
	var stashed, clones []string
	var dirs []string
	_ = filepath.WalkDir(dd.Dir, func(path string, e fs.DirEntry, err error) error {
		if ctx.Err() != nil {
			return fs.SkipAll
		}
		if err != nil || e.Name() != ".git" {
			return nil
		}
		dir := filepath.Dir(path)
		if row.Kind == store.CardHomeWorktree && worktrees.SamePath(dir, dd.Work) {
			return nil
		}
		if e.IsDir() {
			dirs = append(dirs, dir)
			return fs.SkipDir
		}
		if e.Type()&(fs.ModeSymlink|fs.ModeIrregular) != 0 {
			return nil
		}
		gitDir, err1 := shared.GitIn(dir, gitexec.RevParseGitDirs{Absolute: true, GitDir: true})
		common, err2 := shared.GitIn(dir, gitexec.RevParseGitDirs{Absolute: true, Common: true})
		if err1 == nil && err2 == nil && !worktrees.SamePath(gitDir, common) {
			dirs = append(dirs, dir)
			clones = append(clones, filepath.Dir(filepath.Clean(filepath.FromSlash(common))))
		}
		return nil
	})
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	for _, dir := range dirs {
		why, branch := checkoutWork(dir)
		if why == "" {
			continue
		}
		rel, _ := filepath.Rel(dd.Dir, dir)
		name, err := d.sweepStashOf(ctx, t, dir, branch, filepath.ToSlash(rel)+" "+why)
		if err != nil {
			return nil, nil, err
		}
		stashed = append(stashed, name)
	}
	return stashed, clones, nil
}

// checkoutWork is why a checkout holds work nowhere else, "" when it holds none, and its branch.
func checkoutWork(dir string) (why, branch string) {
	status, err := shared.GitIn(dir, gitexec.Status{V1: true, Untracked: "normal"})
	if err != nil {
		return "could not be read: " + small.FirstLine(err.Error()), ""
	}
	branch, _ = shared.GitIn(dir, gitexec.SymbolicRef{Name: "HEAD", Short: true})
	if status != "" {
		lines := len(strings.Split(status, "\n"))
		return "has " + shared.Plural(lines, "uncommitted file"), branch
	}
	tip, err := shared.GitIn(dir, gitexec.RevParseVerify{Rev: "HEAD", Quiet: true})
	if err != nil {
		return "", branch
	}
	ahead, err := shared.GitIn(dir, gitexec.RevListCount{Revs: []string{tip}, NotRemotes: true})
	if err != nil {
		return "could not be read: " + small.FirstLine(err.Error()), branch
	}
	if ahead != "0" {
		return "is " + ahead + " commits ahead of every remote", branch
	}
	return "", branch
}

// sweepStashOf stashes a folder's work to the hub under its card, or says why it could not.
func (d *Daemon) sweepStashOf(ctx context.Context, t *store.Task, dir, branch, why string) (string, error) {
	if t == nil {
		return "", fmt.Errorf("%s and its card is gone, so there is nothing to stash it under", why)
	}
	stash := d.sweepStash
	if stash == nil {
		stash = d.StashTo
	}
	name, _, err := stash(ctx, t.ID, filepath.ToSlash(dir), branch)
	if err != nil {
		return "", fmt.Errorf("%s and the stash to the hub did not land: %s", why, small.FirstLine(err.Error()))
	}
	return name, nil
}

func (d *Daemon) sweepKeep(path, why string) {
	if prev, ok := sweepKept.Load(path); ok && prev == why {
		return
	}
	sweepKept.Store(path, why)
	log.Printf("[atrium] folder sweep kept %s: %s", path, why)
}

func (d *Daemon) sweptOne(path, card string, bytes int64, stashed string) FolderSwept {
	sweepKept.Delete(path)
	if stashed != "" {
		log.Printf("[atrium] folder sweep removed %s (%s), its work stashed to the hub as %s", path, humanBytes(bytes), stashed)
	} else {
		log.Printf("[atrium] folder sweep removed %s (%s)", path, humanBytes(bytes))
	}
	return FolderSwept{Path: filepath.ToSlash(path), Card: card, Bytes: bytes, Stashed: stashed}
}

// worktreeRoots are the `<clone>-worktrees` folders any card worked in, whose clone is beside them.
func worktreeRoots(tasks []*store.Task) []string {
	seen := map[string]bool{}
	var out []string
	for _, t := range tasks {
		w := strings.TrimSpace(t.Worktree)
		if w == "" {
			continue
		}
		for _, root := range []string{filepath.Clean(filepath.FromSlash(w)), filepath.Dir(filepath.Clean(filepath.FromSlash(w)))} {
			if !strings.HasSuffix(root, "-worktrees") || seen[strings.ToLower(root)] {
				continue
			}
			seen[strings.ToLower(root)] = true
			if shared.HasDotGit(strings.TrimSuffix(root, "-worktrees")) {
				out = append(out, root)
			}
		}
	}
	return out
}

// worktreeKey names a checkout the way git lists it, with links resolved (macOS lists /private/var for /var).
func worktreeKey(p string) string {
	p = filepath.Clean(filepath.FromSlash(p))
	if r, err := filepath.EvalSymlinks(p); err == nil {
		p = r
	}
	return strings.ToLower(p)
}

// sweepWorktreeRoot removes each git worktree of the clone beside root, directly under root, whose cards all ended.
func (d *Daemon) sweepWorktreeRoot(ctx context.Context, root string, users func(string, bool) ([]*store.Task, bool)) []FolderSwept {
	clone := strings.TrimSuffix(root, "-worktrees")
	list, err := shared.GitIn(clone, gitexec.WorktreeList{})
	if err != nil {
		return nil
	}
	branches := map[string]string{}
	at := ""
	for _, line := range strings.Split(list, "\n") {
		line = strings.TrimSpace(line)
		if p, ok := strings.CutPrefix(line, "worktree "); ok {
			at = p
			branches[worktreeKey(p)] = ""
		} else if b, ok := strings.CutPrefix(line, "branch refs/heads/"); ok && at != "" {
			branches[worktreeKey(at)] = b
		}
	}
	if _, ended := users(root, true); !ended {
		return nil
	}
	ents, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var out []FolderSwept
	for _, e := range ents {
		if ctx.Err() != nil {
			return out
		}
		if !e.IsDir() || e.Type()&(fs.ModeSymlink|fs.ModeIrregular) != 0 {
			continue
		}
		dir := filepath.Join(root, e.Name())
		branch, ok := branches[worktreeKey(dir)]
		if !ok {
			continue
		}
		if _, err := safepath.Contained(root, dir); err != nil {
			continue
		}
		if s, ok := d.sweepWorktree(ctx, clone, dir, branch, users); ok {
			out = append(out, s)
		}
	}
	return out
}

func (d *Daemon) sweepWorktree(ctx context.Context, clone, dir, branch string, users func(string, bool) ([]*store.Task, bool)) (FolderSwept, bool) {
	cards, ended := users(dir, false)
	if !ended {
		return FolderSwept{}, false
	}
	release, ok := d.holdCards(cards, nil)
	if !ok {
		d.sweepKeep(dir, "a launch or resume holds its card")
		return FolderSwept{}, false
	}
	defer release()
	var last *store.Task
	for _, t := range cards {
		if last == nil || t.LastActivityAt.After(last.LastActivityAt) {
			last = t
		}
	}
	row := store.CardHome{Path: filepath.ToSlash(dir), Kind: store.CardHomeWorktree, Clone: filepath.ToSlash(clone), Branch: branch}
	dd := home.HomeDirs{Dir: dir, Work: dir}
	why, br, onHub := d.HomeWorkState(row, dd)
	if br != "" {
		branch = br
	}
	stashed := ""
	if why != "" {
		name, err := d.sweepStashOf(ctx, last, dir, branch, why)
		if err != nil {
			d.sweepKeep(dir, err.Error())
			return FolderSwept{}, false
		}
		stashed = name
	}
	bytes := home.HomeBytes(dir)
	if _, err := shared.GitIn(clone, gitexec.WorktreeRemove{Path: dir, Force: 2}); err != nil {
		log.Printf("[atrium] git kept the worktree %s: %v", dir, err)
	}
	_, _ = shared.GitIn(clone, gitexec.WorktreePrune{})
	if err := home.RemoveTree(dir); err != nil {
		d.sweepKeep(dir, "the folder could not be removed: "+small.FirstLine(err.Error()))
		return FolderSwept{}, false
	}
	_, _ = shared.GitIn(clone, gitexec.WorktreePrune{})
	if branch != "" && (onHub || stashed != "" || why == "") && !shared.ProtectedBranch(branch, shared.DefaultCullInto) {
		_, _ = shared.GitIn(clone, gitexec.BranchDelete{Branch: branch})
	}
	card := ""
	if last != nil {
		card = last.ID
	}
	return d.sweptOne(dir, card, bytes, stashed), true
}
