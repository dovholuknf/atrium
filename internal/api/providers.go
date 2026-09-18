package api

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/safepath"
	"github.com/dovholuknf/atrium/internal/store"
)

// Reading the disk on a provider's behalf.
//
// The store holds what somebody declared. This file holds every question that
// can only be answered by looking, which is why it is here: `internal/api` is
// where atrium already reads filesystems (`browse.go`, `browseroots.go`,
// `safepath`), and the store touches disk in exactly one place on a hot
// registration path.
//
// Three jobs, and they are deliberately separate:
//
//   - DISCOVERY adopts what is already under a root. It only ever adds.
//   - PRESENCE says whether a row's directory is there right now. Never stored.
//   - THE REFUSAL decides whether worktree support may be turned off.

const (
	// providerDepth is fixed and is no longer a setting.
	//
	// The mechanism this replaces needed a depth knob because it was INFERRING
	// a layout from whatever directories happened to exist. A provider
	// DECLARES the layout, so `root/org/repo` makes the depth a consequence
	// rather than something for somebody to tune. Removing the knob is part of
	// the point of the feature.
	providerDepth = 2
	// The walk is bounded WHILE it reads rather than after, so a root pointed
	// at something enormous costs a moment rather than the whole of it. This is
	// the one thing the old scan got right and it carries over unchanged.
	maxProviderDirs = 4000
	// defaultAdopt is per provider, because the failure is per root: one
	// holding five repositories beside one holding four hundred is ordinary.
	defaultAdopt = 400
	maxAdoptEver = 2000
	// discoverDeadline bounds the WHOLE walk, which the old scan did not do. A
	// dead network share answers `ReadDir` slowly rather than never, so a root
	// on one could hold the request open for as long as it had directories.
	discoverDeadline = 30 * time.Second
	// blockersShown bounds the refusal's list. Past this it is a count, because
	// an error listing four hundred directories is not a message.
	blockersShown = 20
)

// discovery is what one run saw, and every field is reported.
//
// SKIPPED DIRECTORIES ARE COUNTED RATHER THAN DROPPED. Silently ignoring a
// directory that is not a checkout is how somebody spends an afternoon working
// out why one repository is missing from the list.
type discovery struct {
	Adopted   int `json:"adopted"`
	AlreadyIn int `json:"already_known"`
	Skipped   int `json:"skipped_not_checkouts"`
	Hidden    int `json:"hidden"`
	Excluded  int `json:"excluded"`
	// Truncated means a bound stopped the walk, so the answer is partial and
	// says so rather than looking complete.
	Truncated bool   `json:"truncated"`
	Error     string `json:"error,omitempty"`
	// Summary is the sentence the board shows and the store records.
	Summary string `json:"summary"`
}

// discoverProvider walks a provider's root and adopts what it finds.
//
// IT NEVER DELETES A ROW and never un-hides one. A root that cannot be read
// adopts nothing and leaves every existing row exactly as it was, which is what
// makes a run against an unplugged drive harmless rather than destructive. See
// the header on `store.ProviderRepo`.
func discoverProvider(st *store.Store, p *store.Provider) discovery {
	var d discovery

	root := filepath.FromSlash(p.Root)
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		// NOT AN ERROR THAT CHANGES ANYTHING. Reported, recorded on the row,
		// and every repository stays where it is.
		d.Error = "could not read that root: " + p.Root
		d.Summary = d.Error
		return d
	}
	// A ROOT THAT IS ITSELF A CHECKOUT IS NOT A ROOT. Adopting it would make
	// both org and repo meaningless, since a root is a container of orgs and a
	// checkout is a leaf.
	if isCheckout(root) {
		d.Error = p.Root + " is itself a git checkout, so it is a repository rather than a root. " +
			"point the provider at the directory above it"
		d.Summary = d.Error
		return d
	}

	ctx, cancel := context.WithTimeout(context.Background(), discoverDeadline)
	defer cancel()

	adoptCap := p.MaxRepos
	if adoptCap <= 0 {
		adoptCap = defaultAdopt
	}
	if adoptCap > maxAdoptEver {
		adoptCap = maxAdoptEver
	}

	globs := excludeGlobs(p.Exclude)
	known := knownRepos(st, p.Name)

	var found []store.ProviderRepo
	seen := map[string]bool{}
	visited := 0

	// The walk is written out rather than using filepath.WalkDir, because the
	// bound has to stop it WHILE reading and WalkDir's error return is a
	// clumsier way to say that.
	var walk func(dir string, level int, org string)
	walk = func(dir string, level int, org string) {
		if d.Truncated || ctx.Err() != nil {
			return
		}
		if visited++; visited > maxProviderDirs {
			d.Truncated = true
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			// One unreadable directory is skipped, the same way the file
			// picker skips one. An unreadable ROOT was already refused above,
			// which is the case that matters.
			return
		}
		for _, e := range entries {
			if d.Truncated || ctx.Err() != nil {
				return
			}
			if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			child := filepath.Join(dir, e.Name())
			rel := relTo(root, child)
			if excluded(globs, rel) {
				d.Excluded++
				continue
			}
			if !isCheckout(child) {
				if level+1 >= providerDepth {
					// A leaf that is not a checkout. Counted and reported.
					d.Skipped++
					continue
				}
				// An org folder. Descend, carrying its name.
				walk(child, level+1, e.Name())
				continue
			}

			// A CHECKOUT, AND THE WALK DOES NOT DESCEND INTO IT. A checkout's
			// subdirectories are its source, and a vendored or submodule `.git`
			// inside one is never reached.
			//
			// This covers both shapes: `root/org/repo` where `org` is the
			// directory above, and `root/repo` where an org folder is itself a
			// checkout and is adopted with no org at all.
			key := dedupeKey(child)
			if seen[key] {
				continue
			}
			seen[key] = true

			repo := store.ProviderRepo{Org: org, Repo: e.Name()}
			switch state := known[repoKey(repo.Org, repo.Repo)]; state {
			case repoHidden:
				d.Hidden++
			case repoKnown:
				d.AlreadyIn++
			default:
				if len(found) >= adoptCap {
					d.Truncated = true
					return
				}
				found = append(found, repo)
			}
		}
	}
	walk(root, 0, "")

	if ctx.Err() != nil {
		d.Truncated = true
		d.Error = "that root took longer than " + discoverDeadline.String() + " to read"
	}

	added, err := st.AdoptProviderRepos(p.Name, found)
	if err != nil {
		d.Error = err.Error()
		d.Summary = d.Error
		return d
	}
	d.Adopted = added
	d.Summary = summarise(d)
	return d
}

// summarise is the one sentence a person reads.
func summarise(d discovery) string {
	parts := []string{fmt.Sprintf("adopted %d", d.Adopted)}
	if d.AlreadyIn > 0 {
		parts = append(parts, fmt.Sprintf("already knew %d", d.AlreadyIn))
	}
	if d.Hidden > 0 {
		parts = append(parts, fmt.Sprintf("left %d hidden", d.Hidden))
	}
	if d.Skipped > 0 {
		parts = append(parts, fmt.Sprintf("ignored %d that are not checkouts", d.Skipped))
	}
	if d.Excluded > 0 {
		parts = append(parts, fmt.Sprintf("excluded %d", d.Excluded))
	}
	out := strings.Join(parts, ", ")
	if d.Truncated {
		out += ", and stopped early because there was more than the limits allow"
	}
	return out
}

// What is already recorded about a repository, so discovery can tell three
// cases apart: new, known, and deliberately dismissed.
const (
	repoNew = iota
	repoKnown
	repoHidden
)

func knownRepos(st *store.Store, provider string) map[string]int {
	out := map[string]int{}
	rows, err := st.ProviderRepos(provider)
	if err != nil {
		return out
	}
	for _, r := range rows {
		state := repoKnown
		if r.Hidden {
			state = repoHidden
		}
		out[repoKey(r.Org, r.Repo)] = state
	}
	return out
}

func repoKey(org, repo string) string {
	return strings.ToLower(strings.TrimSpace(org)) + "\x00" + strings.ToLower(strings.TrimSpace(repo))
}

// dedupeKey is the RESOLVED path, so a junction pointing back up the tree
// cannot adopt the same directory twice under two names.
//
// A lexical comparison passes for a junction, because a junction is a real
// directory entry whose string has nothing in common with its target.
func dedupeKey(dir string) string {
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		dir = real
	}
	return strings.ToLower(filepath.Clean(dir))
}

func relTo(root, child string) string {
	rel, err := filepath.Rel(root, child)
	if err != nil {
		return filepath.Base(child)
	}
	return filepath.ToSlash(rel)
}

func excludeGlobs(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(strings.ReplaceAll(line, "\\", "/"))
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

// excluded matches a glob against the path relative to the root, and against
// the last segment, so both `archive` and `openziti/*` do what they look like
// they do.
func excluded(globs []string, rel string) bool {
	if len(globs) == 0 {
		return false
	}
	base := path.Base(rel)
	for _, g := range globs {
		if ok, _ := path.Match(g, rel); ok {
			return true
		}
		if ok, _ := path.Match(g, base); ok {
			return true
		}
		// A glob naming a directory excludes everything under it.
		if strings.HasPrefix(rel+"/", strings.TrimSuffix(g, "/")+"/") {
			return true
		}
	}
	return false
}

// fillPresence says whether each row's directory is there RIGHT NOW.
//
// Derived on every read and never written back, which is the half of
// `store.ProviderRepo` that stops a row from becoming a lie. See the header
// there for why the other half is durable.
func fillPresence(repos []*store.ProviderRepo) {
	for _, r := range repos {
		if r.Path == "" {
			continue
		}
		info, err := os.Stat(filepath.FromSlash(r.Path))
		r.Present = err == nil && info.IsDir()
	}
}

// ── the refusal ─────────────────────────────────────────

// blocker is one thing standing in the way of turning worktree support off.
type blocker struct {
	Name string `json:"name"`
	What string `json:"what"`
}

// worktreeRootBlockers answers whether a worktree root is empty, and when it is
// not, what is in it.
//
// DIRECTORY ENTRIES RATHER THAN `git worktree list`, and that is the
// load-bearing choice. Asking git would look more precise and would be weaker:
// git can only see worktrees whose repository is still present and still has
// them registered, and the thing this refusal exists to prevent is orphaning
// directories that still exist and are still checked out. A worktree whose
// repository was deleted is invisible to git and is precisely the case that
// must not be silently abandoned. Counting entries is stronger and cheaper.
//
// The cost, said plainly: a stray `notes.txt` in the worktree root blocks the
// toggle. That is why the refusal names every entry rather than saying "not
// empty", so somebody deletes one file instead of hunting for it.
func worktreeRootBlockers(root string) ([]blocker, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(filepath.FromSlash(root))
	if err != nil {
		if os.IsNotExist(err) {
			// A DIRECTORY THAT IS NOT THERE HOLDS NOTHING.
			return nil, nil
		}
		// UNREADABLE IS NOT EMPTY. Permission denied, a disconnected drive, a
		// dead share. Treating it as empty fails open on exactly the case where
		// the directories are most likely to be there and least likely to be
		// noticed.
		return nil, fmt.Errorf("could not read %s, so whether it is empty is unknown: %w", root, err)
	}

	var out []blocker
	for _, e := range entries {
		if ignoredByTheOS(e.Name()) {
			continue
		}
		out = append(out, blocker{Name: e.Name(), What: describeEntry(root, e)})
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

// ignoredByTheOS is a NAMED LIST rather than a dotfile rule.
//
// These three are written by an operating system without being asked, so
// treating them as evidence of a worktree would mean a directory that can never
// be emptied by the person looking at it. A dotfile rule would be broader and
// wrong: a `.git` under the worktree root is a real thing and must block.
func ignoredByTheOS(name string) bool {
	switch name {
	case ".DS_Store", "Thumbs.db", "desktop.ini":
		return true
	}
	return false
}

// describeEntry says what one blocker is, so the refusal explains rather than
// merely lists.
func describeEntry(root string, e os.DirEntry) string {
	if !e.IsDir() {
		return "a file"
	}
	dir := filepath.Join(filepath.FromSlash(root), e.Name())
	if !isCheckout(dir) {
		// A directory holding worktrees for one repository, which is the shape
		// atrium itself creates.
		if n := checkoutsUnder(dir); n > 0 {
			return fmt.Sprintf("a folder holding %d checkouts", n)
		}
		return "a folder"
	}
	if br := branchOf(dir); br != "" {
		return "a checkout, on branch " + br
	}
	return "a checkout"
}

// checkoutsUnder counts checkouts one and two levels down, bounded, so an org
// folder full of worktrees is described as what it is.
func checkoutsUnder(dir string) int {
	n := 0
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		child := filepath.Join(dir, e.Name())
		if isCheckout(child) {
			n++
			continue
		}
		inner, err := os.ReadDir(child)
		if err != nil {
			continue
		}
		for _, g := range inner {
			if g.IsDir() && isCheckout(filepath.Join(child, g.Name())) {
				n++
			}
		}
	}
	return n
}

// branchOf reads `.git` without spawning anything.
//
// A worktree's `.git` is a FILE pointing at the repository's admin directory,
// and a checkout's is a directory. Both hold a `HEAD` somewhere, and reading it
// is cheaper and more reliable than a process per entry on a refusal path that
// is already listing a directory.
func branchOf(dir string) string {
	head := filepath.Join(dir, ".git", "HEAD")
	if info, err := os.Stat(filepath.Join(dir, ".git")); err == nil && !info.IsDir() {
		raw, err := os.ReadFile(filepath.Join(dir, ".git"))
		if err != nil {
			return ""
		}
		line := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(raw)), "gitdir:"))
		if line == "" {
			return ""
		}
		if !filepath.IsAbs(line) {
			line = filepath.Join(dir, line)
		}
		head = filepath.Join(line, "HEAD")
	}
	raw, err := os.ReadFile(head)
	if err != nil {
		return ""
	}
	ref := strings.TrimSpace(string(raw))
	if !strings.HasPrefix(ref, "ref:") {
		// Detached. The commit is not a branch and saying so is more use than
		// printing forty hex characters.
		return ""
	}
	return strings.TrimPrefix(strings.TrimSpace(strings.TrimPrefix(ref, "ref:")), "refs/heads/")
}

// blockerMessage is the refusal, in the operator's terms.
//
// The last line names the way out, the same way `errOutsideRoots` names the
// setting to change rather than just saying no.
func blockerMessage(name, root string, all []blocker) string {
	var b strings.Builder
	fmt.Fprintf(&b, "worktree support for %s cannot be turned off while %s holds %d thing",
		name, root, len(all))
	if len(all) != 1 {
		b.WriteString("s")
	}
	b.WriteString(".\n\n")
	shown := all
	if len(shown) > blockersShown {
		shown = shown[:blockersShown]
	}
	for _, x := range shown {
		fmt.Fprintf(&b, "  %s: %s\n", x.Name, x.What)
	}
	if len(all) > len(shown) {
		fmt.Fprintf(&b, "  and %d more\n", len(all)-len(shown))
	}
	b.WriteString("\nturning it off would leave those on disk with nothing describing them. " +
		"remove them, or move them somewhere atrium has not been told about, and try again. " +
		"the check button beside the toggle asks this question without saving anything.")
	return b.String()
}

// ── containment between providers ───────────────────────

// errProviderOverlap refuses two providers that describe the same directories.
//
// Two roots that nest mean one directory adopted twice under two names, and a
// toggle-off check that reads a directory the other provider still describes.
// `safepath.Contained` answers "is this inside that" exactly, resolving
// symlinks on both sides, which a string prefix does not.
func providerOverlap(all []*store.Provider, p store.Provider) error {
	mine := []string{p.Root}
	if p.Worktrees && p.WorktreeRoot != "" {
		mine = append(mine, p.WorktreeRoot)
	}
	for _, other := range all {
		if strings.EqualFold(other.Name, p.Name) {
			continue
		}
		theirs := []string{other.Root}
		if other.WorktreeRoot != "" {
			theirs = append(theirs, other.WorktreeRoot)
		}
		for _, a := range mine {
			for _, b := range theirs {
				if nests(a, b) {
					return fmt.Errorf(
						"%s and %s would describe the same directories: %s and %s. "+
							"give each provider a root of its own", p.Name, other.Name, a, b)
				}
			}
		}
	}
	return nil
}

// nests is containment in either direction, decided by `safepath` so a junction
// cannot pass a comparison it should fail.
func nests(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	if _, err := safepath.Contained(a, b); err == nil {
		return true
	}
	if _, err := safepath.Contained(b, a); err == nil {
		return true
	}
	return false
}

// errNoProvider is what the handlers answer with when a name matches nothing.
var errNoProvider = errors.New("no provider by that name")

// ── asking git, which is the only thing a provider cannot work out ──
//
// Moved here from the scan this feature replaced. A provider computes every
// path it needs from what somebody declared, so the one question left for git
// is which worktrees a repository already has, and that is asked per repository
// when one is opened rather than for all of them at once.
//
// The eight-worker fan-out that used to sit beside these went with the scan. It
// existed because the old list had to ask EVERY repository before it could draw
// anything, which is a process spawn per repository. Nothing asks that question
// any more.

// worktreeListTimeout is per repository. `git worktree list` reads a file in
// `.git`, so a second is generous, and a repository on a share that cannot
// answer in one must not hold the request.
const worktreeListTimeout = 2 * time.Second

// projectWorktree is one worktree that already exists for a repository.
type projectWorktree struct {
	Branch string `json:"branch"`
	Path   string `json:"path"`
}

// isCheckout reports whether a directory holds a `.git`, EITHER KIND.
//
// A `.git` directory is an ordinary checkout and a `.git` file is a worktree or
// a submodule. Both are working directories a card can start in, so both count.
// A bare repository has neither and is correctly not a checkout: a card cannot
// start in one.
func isCheckout(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

// worktreesOf lists worktrees except the main checkout, which is already shown
// on its own row.
func worktreesOf(repo string) []projectWorktree {
	if repo == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), worktreeListTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "worktree", "list", "--porcelain")
	cmd.Dir = filepath.FromSlash(repo)
	out, err := cmd.Output()
	if err != nil {
		// A repository git will not talk about contributes no worktrees. It is
		// still a repository and still a directory to start in, so this is not
		// a reason to drop it.
		return nil
	}
	return parseWorktreeList(string(out), repo)
}

// parseWorktreeList reads `--porcelain`, which is the stable one: the human
// format puts the branch in brackets and has changed its mind about spacing.
func parseWorktreeList(out, repo string) []projectWorktree {
	var found []projectWorktree
	var cur projectWorktree
	flush := func() {
		if cur.Path != "" && !eqPath(filepath.FromSlash(cur.Path), filepath.FromSlash(repo)) {
			if cur.Branch == "" {
				// A detached worktree has no branch, so it is labelled by its
				// directory. Nameless rows are unpickable.
				cur.Branch = filepath.Base(filepath.FromSlash(cur.Path))
			}
			found = append(found, cur)
		}
		cur = projectWorktree{}
	}
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "worktree "):
			flush()
			cur.Path = filepath.ToSlash(strings.TrimPrefix(line, "worktree "))
		case strings.HasPrefix(line, "branch "):
			cur.Branch = strings.TrimPrefix(
				strings.TrimPrefix(line, "branch "), "refs/heads/")
		}
	}
	flush()
	sort.Slice(found, func(i, j int) bool {
		return strings.ToLower(found[i].Branch) < strings.ToLower(found[j].Branch)
	})
	return found
}
