package home

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/base/cardroot"
	"github.com/dovholuknf/atrium/internal/base/safepath"
	"github.com/dovholuknf/atrium/internal/daemon/shared"
	"github.com/dovholuknf/atrium/internal/gitsync"
	"github.com/dovholuknf/atrium/internal/small"
	"github.com/dovholuknf/atrium/internal/store"
	"github.com/dovholuknf/atrium/internal/wtfree"
)

// CARD HOMES: one folder per launched card under the room's card root. See docs/runtime/card-home-design.md.
//
// ATRIUM MAKES THE FOLDER AND RECORDS IT FIRST, which is the whole reason it may remove the folder whole: the ledger
// row proves the folder is atrium's. A folder under the root with no row is never removed, and a path never comes from a
// request, only from a row.

// homeGoneWait is how long a reclaim waits for the runner to exit before it keeps the home.
var HomeGoneWait = 60 * time.Second

// SourceHomeKept is the notice a launcher gets when a home it accepted could not be removed.
const SourceHomeKept = "home-kept"

const (
	HomeWork  = "work"
	HomeTmp   = "tmp"
	HomeMeta  = ".atrium"
	homeBrief = "BRIEF.md"
)

var SlugRun = regexp.MustCompile(`[^a-z0-9]+`)

// homeSlug is a card's title, else its wire name, as a folder name part: lowercase, runs of anything else turned into
// one dash, trimmed, cut at 40.
func HomeSlug(title, wire string) string {
	for _, s := range []string{title, wire} {
		g := strings.Trim(SlugRun.ReplaceAllString(strings.ToLower(s), "-"), "-")
		if len(g) > 40 {
			g = strings.Trim(g[:40], "-")
		}
		if g != "" {
			return g
		}
	}
	return "card"
}

// homeID6 is six random characters that tell two cards with one title apart.
func HomeID6() string {
	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return string(b)
}

// cardRoot is this room's card root, refusing one a home's removal could do harm in.
func (d *Service) CardRoot() (string, error) {
	set, _ := d.St.Setting(cardroot.Setting)
	scm, _ := d.St.Setting(gitsync.SettingSCMRoot)
	return cardroot.Resolve(set, gitsync.EffectiveSCMRoot(scm))
}

// homeDirs are the places inside one home.
type HomeDirs struct {
	Dir, Work, Tmp, Meta string
}

func DirsOf(path string) HomeDirs {
	dir := filepath.FromSlash(path)
	return HomeDirs{Dir: dir, Work: filepath.Join(dir, HomeWork), Tmp: filepath.Join(dir, HomeTmp), Meta: filepath.Join(dir, HomeMeta)}
}

// homeOf is the open home a card works in, false for a card with none.
func (d *Service) HomeOf(cardID string) (store.CardHome, bool) {
	row, ok, err := d.St.CardHomeByCard(cardID)
	if err != nil || !ok || row.State == store.CardHomeRemoved || row.Kind == store.CardHomeTask {
		return store.CardHome{}, false
	}
	return row, true
}

// saysDir is where a long say from a launcher is written: the card's .atrium/says, "" for a card with no home.
func (d *Service) SaysDir(cardID string) string {
	if h, ok := d.HomeOf(cardID); ok {
		return filepath.Join(DirsOf(h.Path).Meta, "says")
	}
	return ""
}

// homeRequest says which kind of home a launch wants, "" for none: a launch with a directory of its own, a launch onto
// a card that exists and a resume get none.
func HomeRequest(req shared.LaunchRequest, cwd string, onto bool) string {
	if cwd != "" || onto || req.Resume != "" {
		return ""
	}
	switch {
	case req.Throwaway:
		return store.CardHomeThrowaway
	case strings.TrimSpace(req.Repo) != "" && RepoIsRef(req.Repo):
		return store.CardHomeWorktree
	case req.Home:
		return store.CardHomeScratch
	}
	return ""
}

// RepoIsRef says whether a repo names something atrium can resolve (`host/owner/repo`, or an absolute path) and not a
// label such as `owner/repo`, which every launcher that records where it started has always sent.
func RepoIsRef(repo string) bool {
	repo = strings.TrimSpace(repo)
	if filepath.IsAbs(filepath.FromSlash(repo)) {
		return true
	}
	parts := strings.Split(repo, "/")
	return len(parts) == 3 && strings.Contains(parts[0], ".")
}

// homeLaunchRefusal is why a launch cannot have the home it asked for, before anything is made.
func HomeLaunchRefusal(req shared.LaunchRequest, cwd string) error {
	if cwd != "" && RepoIsRef(req.Repo) {
		return errors.New("pass cwd for a directory you made, or repo for one atrium makes, not both")
	}
	return nil
}

// makeHome records and makes a home. A worktree home leaves work/ to git, a scratch or throwaway home makes it empty.
// The row is written first, so a crash between the two leaves a row pointing at nothing and never a folder with none.
func (d *Service) MakeHome(kind, title, wire string) (store.CardHome, HomeDirs, error) {
	root, err := d.CardRoot()
	if err != nil {
		return store.CardHome{}, HomeDirs{}, err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return store.CardHome{}, HomeDirs{}, fmt.Errorf("could not make the card root %s: %v", filepath.ToSlash(root), err)
	}
	slug := HomeSlug(title, wire)
	var path string
	for i := 0; ; i++ {
		path = filepath.Join(root, slug+"-"+HomeID6())
		if _, err := os.Lstat(path); os.IsNotExist(err) {
			if _, taken, _ := d.St.CardHomeByPath(filepath.ToSlash(path)); !taken {
				break
			}
		}
		if i == 8 {
			return store.CardHome{}, HomeDirs{}, errors.New("could not find a free name for the card's home")
		}
	}
	if _, err := safepath.Contained(root, path); err != nil {
		return store.CardHome{}, HomeDirs{}, fmt.Errorf("the card's home %s is not inside the card root", filepath.ToSlash(path))
	}
	row, err := d.St.CardHomeNew(store.CardHome{Path: filepath.ToSlash(path), Kind: kind})
	if err != nil {
		return store.CardHome{}, HomeDirs{}, err
	}
	dd := DirsOf(row.Path)
	fail := func(err error) (store.CardHome, HomeDirs, error) {
		_ = RemoveTree(dd.Dir)
		_ = d.St.CardHomeSet(row.Path, store.CardHomeRemoved, "the home could not be made", "")
		return store.CardHome{}, HomeDirs{}, fmt.Errorf("could not make the card's home: %v", err)
	}
	for _, p := range []string{dd.Dir, dd.Tmp, dd.Meta} {
		if err := os.Mkdir(p, 0o755); err != nil {
			return fail(err)
		}
	}
	if kind != store.CardHomeWorktree {
		if err := os.Mkdir(dd.Work, 0o755); err != nil {
			return fail(err)
		}
	}
	return row, dd, nil
}

// resolveRepo is the clone a `repo` names: an absolute path to a clone on this room, or `host/owner/repo`, found under
// git.scm_root and cloned from the hub when it is not there yet.
func (d *Service) ResolveRepo(ctx context.Context, repo string) (string, error) {
	repo = strings.TrimSpace(repo)
	if filepath.IsAbs(filepath.FromSlash(repo)) {
		dir := filepath.FromSlash(repo)
		if !shared.HasDotGit(dir) {
			return "", fmt.Errorf("%s is not a git clone on this room", repo)
		}
		return dir, nil
	}
	ref, err := gitsync.ParseURL("https://" + repo)
	if err != nil {
		return "", err
	}
	_, dest, err := d.Scm(nil).ClonePath(ref)
	if err != nil {
		return "", err
	}
	if shared.HasDotGit(dest) {
		return dest, nil
	}
	res, err := d.CloneFromHub(ctx, "https://"+repo)
	if err != nil {
		return "", err
	}
	if res.Path != "" {
		return filepath.FromSlash(res.Path), nil
	}
	return dest, nil
}

var BranchOK = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)

// addHomeWorktree puts `<home>/work` on a branch of the clone, made from base when the branch is new. Argv, never a shell.
func AddHomeWorktree(clone string, dd HomeDirs, branch, base, slug string) (usedBranch, usedBase string, err error) {
	if branch = strings.TrimSpace(branch); branch == "" {
		branch = "claude/" + slug
	}
	if !BranchOK.MatchString(branch) || strings.Contains(branch, "..") {
		return "", "", fmt.Errorf("%q is not a branch name atrium will make a worktree on", branch)
	}
	if _, err := shared.GitIn(clone, "check-ref-format", "--branch", branch); err != nil {
		return "", "", fmt.Errorf("%q is not a branch name: %v", branch, err)
	}
	if base = strings.TrimSpace(base); base == "" {
		base = DefaultBase(clone)
	}
	if !BranchOK.MatchString(base) || strings.Contains(base, "..") {
		return "", "", fmt.Errorf("%q is not a start atrium will branch from", base)
	}
	if _, err := shared.GitIn(clone, "rev-parse", "--verify", "-q", "refs/heads/"+branch); err == nil {
		_, err = shared.GitIn(clone, "worktree", "add", dd.Work, branch)
		return branch, "", WrapGit("add the worktree", err)
	}
	_, err = shared.GitIn(clone, "worktree", "add", "-b", branch, dd.Work, base)
	return branch, base, WrapGit("add the worktree", err)
}

func WrapGit(what string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("could not %s: %v", what, err)
}

// defaultBase is where a new branch starts: the clone's claude/main, else its default branch.
func DefaultBase(clone string) string {
	if _, err := shared.GitIn(clone, "rev-parse", "--verify", "-q", "refs/heads/"+shared.DefaultCullInto); err == nil {
		return shared.DefaultCullInto
	}
	if out, err := shared.GitIn(clone, "symbolic-ref", "--short", "-q", "refs/remotes/origin/HEAD"); err == nil && out != "" {
		return out
	}
	if out, err := shared.GitIn(clone, "symbolic-ref", "--short", "-q", "HEAD"); err == nil && out != "" {
		return out
	}
	return "HEAD"
}

// homeEnv is what a runner in a home sees, over the harness and the machine: its temp folders and where its home is.
func HomeEnv(dd HomeDirs) map[string]string {
	tmp := dd.Tmp
	return map[string]string{
		"TMP": tmp, "TEMP": tmp, "TMPDIR": tmp,
		"ATRIUM_HOME": filepath.ToSlash(dd.Dir),
	}
}

// removeTree removes a folder and everything in it. A link or junction inside is removed as a link and never followed,
// so what it points at is untouched.
func RemoveTree(path string) error {
	fi, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !fi.IsDir() || fi.Mode()&(fs.ModeSymlink|fs.ModeIrregular) != 0 {
		return RemoveOne(path)
	}
	ents, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	for _, e := range ents {
		if err := RemoveTree(filepath.Join(path, e.Name())); err != nil {
			return err
		}
	}
	return RemoveOne(path)
}

func RemoveOne(path string) error {
	err := os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		_ = os.Chmod(path, 0o666)
		err = os.Remove(path)
	}
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// homeGuard is the folder a ledger row names, once it is known to be a real folder inside the card root and not the root.
func (d *Service) HomeGuard(row store.CardHome) (string, error) {
	root, err := d.CardRoot()
	if err != nil {
		return "", err
	}
	path := filepath.FromSlash(row.Path)
	real, err := safepath.Contained(root, path)
	if err != nil {
		return "", fmt.Errorf("%s is not inside the card root, so atrium will not remove it", row.Path)
	}
	if shared.SamePath(real, root) || shared.SamePath(path, root) {
		return "", errors.New("that is the card root itself, which atrium never removes")
	}
	if fi, err := os.Lstat(path); err == nil && fi.Mode()&(fs.ModeSymlink|fs.ModeIrregular) != 0 {
		return "", fmt.Errorf("%s is a link, so atrium will not remove it", row.Path)
	}
	return path, nil
}

// hubHas says whether the hub's mirror, as read back into the clone, holds the commit.
func HubHas(clone, tip string) bool {
	if remotes, err := shared.GitIn(clone, "remote"); err != nil || !ContainsLine(remotes, "hub") {
		return false
	}
	_, _ = shared.GitIn(clone, "fetch", "--quiet", "hub")
	out, err := shared.GitIn(clone, "for-each-ref", "--contains", tip, "--format=%(refname)", "refs/remotes/hub")
	return err == nil && out != ""
}

func ContainsLine(s, line string) bool {
	for _, l := range strings.Split(s, "\n") {
		if strings.TrimSpace(l) == line {
			return true
		}
	}
	return false
}

// homeWorkState reads a worktree home's work/ before anything goes: why it cannot be removed ("" when it can), and
// whether the branch tip is on the hub, which decides whether the local branch goes too.
func (d *Service) HomeWorkState(row store.CardHome, dd HomeDirs) (why, branch string, onHub bool) {
	if _, err := os.Stat(dd.Work); err != nil {
		return "", "", false
	}
	status, err := shared.GitIn(dd.Work, "status", "--porcelain=v1", "--untracked-files=normal")
	if err != nil {
		return "work/ could not be read: " + small.FirstLine(err.Error()), "", false
	}
	if status != "" {
		lines := 0
		for _, l := range strings.Split(status, "\n") {
			if strings.TrimSpace(l) != "" {
				lines++
			}
		}
		return "work/ has " + shared.Plural(lines, "uncommitted file"), "", false
	}
	branch, _ = shared.GitIn(dd.Work, "symbolic-ref", "--short", "-q", "HEAD")
	if branch == "" {
		branch = row.Branch
	}
	tip, err := shared.GitIn(dd.Work, "rev-parse", "--verify", "-q", "HEAD")
	if err != nil {
		return "work/ has no commit to check", branch, false
	}
	clone := row.Clone
	if clone == "" {
		clone = dd.Work
	}
	if HubHas(clone, tip) {
		return "", branch, true
	}
	if branch != "" {
		if ok, err := d.LandedInto(dd.Work, branch, shared.DefaultCullInto); err == nil && ok {
			return "", branch, false
		}
	}
	ahead, _ := shared.GitIn(dd.Work, "rev-list", "--count", tip, "--not", "--remotes=hub")
	if ahead == "" {
		ahead = "some"
	}
	return fmt.Sprintf("%s is %s commits ahead of the hub and not landed on %s", shared.OrDefault(branch, "its branch"), ahead,
		shared.DefaultCullInto), branch, false
}

// reclaimHome removes a home whole, after the checks, and records what became of it. force skips the checks on the
// work and never the safety rules: the board's Disk view is the one caller that may. A failed check keeps the home with
// the reason, and the card's launcher gets one notice.
func (d *Service) ReclaimHome(t *store.Task, row store.CardHome, force bool) (removed bool, kept, branch string) {
	removed, kept, branch, _ = d.ReclaimHomeBranch(t, row, force)
	return
}

// reclaimHomeBranch is reclaimHome that also says whether the local branch was in fact deleted.
func (d *Service) ReclaimHomeBranch(t *store.Task, row store.CardHome, force bool) (removed bool, kept, branch string, branchGone bool) {
	keep := func(reason string) (bool, string, string, bool) {
		if row.State != store.CardHomeKept || row.Reason != reason {
			_ = d.St.CardHomeSet(row.Path, store.CardHomeKept, reason, row.Owed)
			if t != nil {
				d.NotifyLauncher(t, SourceHomeKept, row.Path+"|"+reason,
					fmt.Sprintf("the home of %s was kept: %s. It is at %s", t.DisplayTitle(), reason, row.Path))
			}
			log.Printf("[atrium] kept the home %s: %s", row.Path, reason)
		}
		return false, reason, branch, false
	}
	path, err := d.HomeGuard(row)
	if err != nil {
		return keep(err.Error())
	}
	if t != nil && !d.WaitRunnerGone(t.ID, HomeGoneWait) {
		return keep("its runner was still up after " + HomeGoneWait.String())
	}
	dd := DirsOf(row.Path)
	onHub := false
	if row.Kind == store.CardHomeWorktree {
		var why string
		why, branch, onHub = d.HomeWorkState(row, dd)
		if why != "" && !force {
			return keep(why)
		}
	}
	if t != nil {
		d.CloseShell(t.ID)
	}
	if row.Kind == store.CardHomeWorktree {
		if clone := d.HomeClone(row, dd); clone != "" {
			if _, err := os.Stat(dd.Work); err == nil {
				if _, err := shared.GitIn(clone, "worktree", "remove", "--force", dd.Work); err != nil {
					log.Printf("[atrium] git kept the worktree %s: %v", dd.Work, err)
				}
			}
			_, _ = shared.GitIn(clone, "worktree", "prune")
			if onHub && branch != "" && !shared.ProtectedBranch(branch, shared.DefaultCullInto) {
				if _, err := shared.GitIn(clone, "branch", "-D", branch); err == nil {
					branchGone = true
				} else {
					log.Printf("[atrium] kept the branch %s of %s: %v", branch, row.Path, err)
				}
			}
		}
	}
	if err := RemoveTree(path); err != nil {
		return keep("the folder could not be removed: " + small.FirstLine(err.Error()))
	}
	_ = d.St.CardHomeSet(row.Path, store.CardHomeRemoved, "", row.Owed)
	log.Printf("[atrium] removed the home %s", row.Path)
	return true, "", branch, branchGone
}

// homeClone is the clone a worktree home was added from: the ledger's, else the one its .git file points back to.
func (d *Service) HomeClone(row store.CardHome, dd HomeDirs) string {
	if row.Clone != "" && shared.HasDotGit(row.Clone) {
		return filepath.FromSlash(row.Clone)
	}
	common, err := shared.GitIn(dd.Work, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return ""
	}
	return filepath.Dir(filepath.Clean(common))
}

// cullHome is atrium_cull on a card that has a home: the runner leaves, then the home goes by its ledger row. The
// launcher's call is the acceptance, so the card closes as accepted even when a check kept the home.
func (d *Service) CullHome(t *store.Task, into string, row store.CardHome) (*shared.CullResult, error) {
	res := &shared.CullResult{Card: t.ID, Into: into, Worktree: t.Worktree}
	if err := d.LeaveForCull(t, res); err != nil {
		return nil, err
	}
	freed := HomeBytes(row.Path)
	removed, kept, branch, gone := d.ReclaimHomeBranch(t, row, false)
	res.Branch = branch
	res.WorktreeRemoved, res.ScratchRemoved, res.BriefRemoved, res.BranchDeleted = removed, removed, removed, gone
	res.Kept = kept
	if removed {
		res.BytesFreed = freed
	}
	return d.Culled(t, res), nil
}

// sweepHomes is the sweep's step for the ledger: a live row whose folder is gone is marked removed, a kept row is
// tried again, and a throwaway whose runner has exited is removed, as a backstop for a restart between exit and cleanup.
func (d *Service) SweepHomes() error {
	rows, err := d.St.CardHomesOpen()
	if err != nil {
		return err
	}
	for _, row := range rows {
		if row.Kind == store.CardHomeTask {
			continue
		}
		if _, err := os.Lstat(filepath.FromSlash(row.Path)); os.IsNotExist(err) {
			_ = d.St.CardHomeSet(row.Path, store.CardHomeRemoved, "the folder was already gone", row.Owed)
			continue
		}
		if row.Card == "" {
			continue
		}
		t, err := d.St.Get(row.Card)
		if err != nil {
			continue
		}
		switch {
		case row.Kind == store.CardHomeThrowaway && !t.Throwaway:
		case row.Kind == store.CardHomeThrowaway && !d.HasSession(t.ID) && t.PID <= 0:
			d.EndThrowaway(t.ID)
		case row.State == store.CardHomeKept && strings.HasPrefix(row.Reason, "the folder could not be removed") && !d.HasSession(t.ID):
			d.ReclaimHome(t, row, false)
		}
	}
	return nil
}

// homeBytes is the size of a home on disk, links not followed.
func HomeBytes(path string) int64 {
	return wtfree.DirSize(context.Background(), filepath.FromSlash(path))
}

// launchHome makes the home a launch asked for. A worktree home adds `work/` to the clone named by repo, a scratch home
// wears the keep-open tag as a scratch launch does, and a throwaway home is removed when its runner exits.
func (d *Service) LaunchHome(kind string, req *shared.LaunchRequest) (store.CardHome, HomeDirs, error) {
	clone := ""
	if kind == store.CardHomeWorktree {
		ctx, cancel := context.WithTimeout(context.Background(), CloneTimeout)
		defer cancel()
		var err error
		if clone, err = d.ResolveRepo(ctx, req.Repo); err != nil {
			return store.CardHome{}, HomeDirs{}, err
		}
	}
	leaf := "card"
	if r := strings.Trim(strings.ReplaceAll(strings.TrimSpace(req.Repo), `\`, "/"), "/"); r != "" {
		leaf = r[strings.LastIndex(r, "/")+1:]
	}
	row, dd, err := d.MakeHome(kind, req.Title, leaf)
	if err != nil {
		return store.CardHome{}, HomeDirs{}, err
	}
	if kind == store.CardHomeWorktree {
		branch, base, err := AddHomeWorktree(clone, dd, req.Branch, req.Base, HomeSlug(req.Title, leaf))
		if err != nil {
			d.ReclaimHome(nil, row, true)
			return store.CardHome{}, HomeDirs{}, err
		}
		row.Clone, row.Branch, row.Base = filepath.ToSlash(clone), branch, base
		if err := d.St.CardHomeWorktree(row.Path, row.Clone, branch, base); err != nil {
			d.ReclaimHome(nil, row, true)
			return store.CardHome{}, HomeDirs{}, err
		}
		req.Branch = branch
	}
	if kind == store.CardHomeScratch && !small.HasTag(req.Tags, shared.KeepOpenTag) {
		req.Tags = append(append([]string{}, req.Tags...), shared.KeepOpenTag)
	}
	return row, dd, nil
}
