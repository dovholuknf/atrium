package daemon

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/api"
	"github.com/dovholuknf/atrium/internal/gitsync"
	"github.com/dovholuknf/atrium/internal/safepath"
	"github.com/dovholuknf/atrium/internal/store"
)

// GET /v1/tasks/{id}/changes?against=head|base, or ?turn=<at>
// (docs/rnd/changes-view-design.md, section 1, and the `turn` addition from r-changes).
//
// IT RUNS IN THE CARD'S WORKTREE AND TAKES NO PATH. The only paths git is ever given are
// the ones a transcript named, and each of those went through safepath first (turns.go).
//
// GIT RUNS THROUGH gitsync.Runner: a clean environment, every GIT_* stripped, bounded, killed at
// the bound. Read-only commands only. `--no-ext-diff` and `--no-textconv` stop a repository's own
// configuration from naming a program to run, `core.fsmonitor` is turned off for the same reason,
// and `--no-optional-locks` stops a read from rewriting the index under the agent.
// GIT_LITERAL_PATHSPECS=1 makes a file named `:(top)x` or `*.go` a file and not a pattern.

const (
	changesFilesMax = 400
	changesHunksMax = 2 << 20
	changesFileMax  = 256 << 10
	// untrackedReadMax is the biggest untracked file read to count its lines.
	untrackedReadMax = 8 << 20
	// listMax bounds the file lists git prints, which are paths and counts and never hunks.
	// patchMax is what is read of one patch: the total hunk bound, a file that would be cut anyway, and slack.
	patchMax = changesHunksMax + changesFileMax + 64<<10
	// argvMax is how many characters of paths go on one command line. Windows stops at 32767.
	argvMax         = 12000
	changesBound    = 30 * time.Second
	emptyTree       = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"
	notAWorktree    = "this card's directory is not a git worktree"
	shellNotCounted = "changes made by a shell command are not included"
)

// ChangeFile is one file in an answer.
type ChangeFile struct {
	Path    string `json:"path"`
	OldPath string `json:"old_path,omitempty"`
	// Status is added, modified, deleted, renamed or binary.
	Status  string `json:"status"`
	Added   int    `json:"added"`
	Removed int    `json:"removed"`
	// Hunks is the unified diff as text. Empty for a binary file, and for a file whose diff was
	// over the bound, which HunksCut says.
	Hunks    string `json:"hunks"`
	HunksCut bool   `json:"hunks_cut,omitempty"`
	// Via, in a `turn` answer, is where the file's change was found: `edits` (uncommitted) or
	// `commits` (the turn committed it).
	Via string `json:"via,omitempty"`
	// Cumulative says the numbers include more than this turn: the transcript shows the file
	// edited in another turn since the last commit.
	Cumulative bool `json:"cumulative,omitempty"`
}

// ChangesCut says what the bounds left out, so the view never claims to be the whole change.
type ChangesCut struct {
	// Files is how many files were left off the list.
	Files int `json:"files"`
	// Hunks is how many listed files carry counts and no hunks.
	Hunks int    `json:"hunks"`
	Why   string `json:"why"`
}

// ChangesView is the answer, for a card or for one turn.
type ChangesView struct {
	// Against is head, base, or turn.
	Against string `json:"against"`
	// Note says why the answer is not the one asked for.
	Note  string       `json:"note,omitempty"`
	Base  string       `json:"base"`
	Head  string       `json:"head"`
	Dirty bool         `json:"dirty"`
	Total int          `json:"total"`
	Files []ChangeFile `json:"files"`
	Cut   *ChangesCut  `json:"cut,omitempty"`
	// Partial, Why and Outside are set on a `turn` answer. Outside counts edit paths that
	// resolved outside the worktree. It never names them.
	Partial *bool  `json:"partial,omitempty"`
	Why     string `json:"why,omitempty"`
	Outside int    `json:"outside,omitempty"`
}

type gitAt struct {
	ctx context.Context
	run *gitsync.Runner
	dir string
}

func (g gitAt) git(args ...string) (string, error) {
	full := append([]string{"--no-optional-locks", "-c", "core.fsmonitor=false"}, args...)
	return g.run.GitEnv(g.ctx, g.dir, []string{"GIT_LITERAL_PATHSPECS=1"}, full...)
}

func refused(status int, msg string) error { return &api.ChangesError{Status: status, Msg: msg} }

// capped is git with a bound on its output, which is stopped at it. See gitsync.Runner.GitCapped.
func (g gitAt) capped(max int, args ...string) (string, error) {
	full := append([]string{"--no-optional-locks", "-c", "core.fsmonitor=false"}, args...)
	return g.run.GitCapped(g.ctx, g.dir, []string{"GIT_LITERAL_PATHSPECS=1"}, max, full...)
}

// listMax is a variable so a test can use a small cap.
var listMax = 16 << 20

// changesFor answers the endpoint for one card.
func (d *Daemon) changesFor(ctx context.Context, taskID, against string, turn time.Time) (any, error) {
	t, err := d.st.Get(taskID)
	if err != nil {
		return nil, err
	}
	if t == nil {
		return nil, errNoSuchCard
	}
	ctx, cancel := context.WithTimeout(ctx, changesBound)
	defer cancel()
	dir := filepath.FromSlash(strings.TrimSpace(t.Worktree))
	if dir == "" {
		return nil, refused(http.StatusNotFound, notAWorktree)
	}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return nil, refused(http.StatusNotFound, notAWorktree)
	}
	g := gitAt{ctx: ctx, run: gitsync.Default, dir: dir}
	if out, err := g.git("rev-parse", "--is-inside-work-tree"); err != nil || strings.TrimSpace(out) != "true" {
		return nil, refused(http.StatusNotFound, notAWorktree)
	}
	v := &ChangesView{Against: "head", Files: []ChangeFile{}}
	ref := emptyTree
	if out, err := g.git("rev-parse", "--verify", "--quiet", "HEAD"); err == nil {
		v.Head = strings.TrimSpace(out)
		ref = v.Head
	}
	if out, err := g.git("status", "--porcelain"); err == nil {
		v.Dirty = strings.TrimSpace(out) != ""
	}
	v.Base = v.Head

	if !turn.IsZero() {
		return d.turnChanges(g, t, v, ref, turn)
	}
	if against == "base" {
		if mb, branch := mergeBase(g, v.Head); mb != "" {
			v.Against, ref, v.Base = "base", mb, mb
			_ = branch
		} else {
			v.Note = "this worktree has no claude/main or main to compare against, so this shows uncommitted changes"
		}
	}
	files, err := fileDiff(g, []string{"diff"}, []string{ref}, nil)
	if err != nil {
		return nil, err
	}
	files = append(files, untracked(g, dir, nil)...)
	sort.SliceStable(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	v.Total = len(files)
	v.Files, v.Cut = bound(files)
	return v, nil
}

// mergeBase is the commit where HEAD left the integration branch, claude/main or else main,
// and that branch's name. Empty when there is none, or when the card has no commit yet.
func mergeBase(g gitAt, head string) (string, string) {
	if head == "" {
		return "", ""
	}
	for _, br := range []string{"claude/main", "main"} {
		if _, err := g.git("rev-parse", "--verify", "--quiet", "refs/heads/"+br); err != nil {
			continue
		}
		if out, err := g.git("merge-base", "HEAD", "refs/heads/"+br); err == nil {
			if sha := strings.TrimSpace(out); sha != "" {
				return sha, br
			}
		}
	}
	return "", ""
}

// turnChanges is the files one reply's turn changed, found from its edit tool calls.
func (d *Daemon) turnChanges(g gitAt, t *store.Task, v *ChangesView, ref string, turn time.Time) (any, error) {
	turn = turn.UTC()
	path := d.cardTranscript(t)
	if path == "" {
		return nil, refused(http.StatusNotFound, "this card has no transcript atrium can read, so there is no turn to show")
	}
	x, err := turnsOf(path)
	if err != nil {
		return nil, refused(http.StatusNotFound, "this card's transcript could not be read, so there is no turn to show")
	}
	if !x.hasReply(turn) {
		return nil, refused(http.StatusNotFound, "no reply of this card was written at "+turn.Format(time.RFC3339Nano))
	}
	start, end := x.span(turn)
	rels, outside := insideWorktree(t.Worktree, x.editsIn(start, end))
	yes := true
	v.Against, v.Partial, v.Why, v.Outside = "turn", &yes, shellNotCounted, outside
	why := []string{shellNotCounted}
	if len(rels) == 0 {
		return v, nil
	}

	// Uncommitted first: what the turn's edits left in the tree.
	files, err := fileDiff(g, []string{"diff"}, []string{ref}, rels)
	if err != nil {
		return nil, err
	}
	files = append(files, untracked(g, g.dir, rels)...)
	found := map[string]bool{}
	for i := range files {
		files[i].Via = "edits"
		found[files[i].Path] = true
	}
	var rest []string
	for _, p := range rels {
		if !found[p] {
			rest = append(rest, p)
		}
	}

	// A file with no uncommitted change may have been committed by the turn. Commits are
	// found by their date in the turn's window, and a Bash `git commit` is never parsed.
	// With no prompt before the reply there is no window, and every commit in history would count,
	// so a turn with a zero start has no committed changes to find.
	if len(rest) > 0 && v.Head != "" && !start.IsZero() {
		cf, rewritten, err := committedInTurn(g, rest, start, end)
		if err != nil {
			return nil, err
		}
		files = append(files, cf...)
		if rewritten {
			why = append(why, "a commit in the turn's window has an author date outside it, so dates were rewritten and commits may be missing")
		}
	}

	// A file edited in another turn since the last commit carries that turn's lines too.
	since := lastCommit(g)
	var others []editCall
	for _, e := range x.edits {
		if e.at.After(since) && (e.at.Before(start) || (!end.IsZero() && !e.at.Before(end))) {
			others = append(others, e)
		}
	}
	if len(others) > 0 {
		other, _ := insideWorktree(t.Worktree, others)
		in := map[string]bool{}
		for _, p := range other {
			in[p] = true
		}
		for i := range files {
			if files[i].Via == "edits" && in[files[i].Path] {
				files[i].Cumulative = true
			}
		}
	}
	sort.SliceStable(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	v.Why = strings.Join(why, "; ")
	v.Total = len(files)
	v.Files, v.Cut = bound(files)
	return v, nil
}

// lastCommit is when HEAD was committed, or the zero time (so every edit counts) with no commit.
func lastCommit(g gitAt) time.Time {
	out, err := g.git("log", "-1", "--format=%ct", "HEAD")
	if err != nil {
		return time.Time{}
	}
	n, err := strconv.ParseInt(strings.TrimSpace(out), 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.Unix(n, 0).UTC()
}

// committedInTurn is what the commits dated inside [start, end) changed in paths. rewritten
// says one of them has an author date outside the window: a rebase keeps the author date and
// moves the committer date, so the window found a commit that was not made in the turn.
func committedInTurn(g gitAt, paths []string, start, end time.Time) ([]ChangeFile, bool, error) {
	args := []string{"rev-list", "--no-merges", "--max-count=200", "--format=%at"}
	if !start.IsZero() {
		args = append(args, "--after="+strconv.FormatInt(start.Unix(), 10))
	}
	if !end.IsZero() {
		args = append(args, "--before="+strconv.FormatInt(end.Add(time.Second-1).Unix(), 10))
	}
	out, err := g.git(append(args, "HEAD")...)
	if err != nil {
		return nil, false, err
	}
	type commit struct {
		sha string
		at  time.Time
	}
	var commits []commit
	lines := strings.Split(strings.ReplaceAll(out, "\r", ""), "\n")
	for i := 0; i+1 < len(lines); i++ {
		if sha, ok := strings.CutPrefix(lines[i], "commit "); ok {
			n, _ := strconv.ParseInt(strings.TrimSpace(lines[i+1]), 10, 64)
			commits = append(commits, commit{sha: strings.TrimSpace(sha), at: time.Unix(n, 0).UTC()})
		}
	}
	rewritten := false
	byPath := map[string]*ChangeFile{}
	var order []string
	for i := len(commits) - 1; i >= 0; i-- {
		c := commits[i]
		if (!start.IsZero() && c.at.Before(start.Truncate(time.Second))) ||
			(!end.IsZero() && c.at.After(end.Add(time.Second))) {
			rewritten = true
		}
		fs, err := fileDiff(g, []string{"show", "--format="}, []string{c.sha}, paths)
		if err != nil {
			return nil, false, err
		}
		for _, f := range fs {
			cur, ok := byPath[f.Path]
			if !ok {
				f.Via = "commits"
				byPath[f.Path] = &f
				order = append(order, f.Path)
				continue
			}
			cur.Added += f.Added
			cur.Removed += f.Removed
			cur.Hunks += f.Hunks
			cur.HunksCut = cur.HunksCut || f.HunksCut
			if f.Status == "deleted" || f.Status == "binary" {
				cur.Status = f.Status
			}
		}
	}
	var res []ChangeFile
	for _, p := range order {
		res = append(res, *byPath[p])
	}
	return res, rewritten, nil
}

// fileDiff runs name-status, numstat and the patch for one comparison and joins them, in the
// order git printed them. head is the subcommand (diff, or show --format=), tail the commit or
// ref, and paths limit it, or nothing when nil.
func fileDiff(g gitAt, head, tail, paths []string) ([]ChangeFile, error) {
	if len(paths) == 0 {
		return fileDiffOne(g, head, tail, nil)
	}
	var all []ChangeFile
	for _, chunk := range chunkPaths(paths) {
		fs, err := fileDiffOne(g, head, tail, chunk)
		if err != nil {
			return nil, err
		}
		all = append(all, fs...)
	}
	return all, nil
}

// chunkPaths cuts paths into groups that fit on one command line. Git has no way to read a
// pathspec from standard input for diff, show or ls-files, so they are batched.
func chunkPaths(paths []string) [][]string {
	var out [][]string
	var cur []string
	n := 0
	for _, p := range paths {
		if len(cur) > 0 && n+len(p)+1 > argvMax {
			out = append(out, cur)
			cur, n = nil, 0
		}
		cur = append(cur, p)
		n += len(p) + 1
	}
	if len(cur) > 0 {
		out = append(out, cur)
	}
	return out
}

func fileDiffOne(g gitAt, head, tail, paths []string) ([]ChangeFile, error) {
	run := func(max int, mode ...string) (string, error) {
		args := append([]string{}, head...)
		args = append(args, "--relative", "-M", "--no-ext-diff", "--no-textconv", "--no-color")
		args = append(args, mode...)
		args = append(args, tail...)
		if len(paths) > 0 {
			args = append(args, "--")
			args = append(args, paths...)
		}
		return g.capped(max, args...)
	}
	tooMany := refused(http.StatusRequestEntityTooLarge, "this card changed too many files to list")
	ns, err := run(listMax, "--name-status", "-z")
	if errors.Is(err, gitsync.ErrOutputCap) {
		return nil, tooMany
	}
	if err != nil {
		return nil, err
	}
	num, err := run(listMax, "--numstat", "-z")
	if errors.Is(err, gitsync.ErrOutputCap) {
		return nil, tooMany
	}
	if err != nil {
		return nil, err
	}
	var files []ChangeFile
	tok := strings.Split(ns, "\x00")
	for i := 0; i < len(tok) && tok[i] != ""; {
		s := tok[i][:1]
		f := ChangeFile{}
		switch s {
		case "R", "C":
			if i+2 >= len(tok) {
				return nil, fmt.Errorf("git name-status output ended mid-record")
			}
			f.OldPath, f.Path = tok[i+1], tok[i+2]
			f.Status = "renamed"
			if s == "C" {
				f.Status, f.OldPath = "added", ""
			}
			i += 3
		default:
			if i+1 >= len(tok) {
				return nil, fmt.Errorf("git name-status output ended mid-record")
			}
			f.Path = tok[i+1]
			f.Status = map[string]string{"A": "added", "D": "deleted"}[s]
			if f.Status == "" {
				f.Status = "modified"
			}
			i += 2
		}
		files = append(files, f)
	}
	nt := strings.Split(num, "\x00")
	n := 0
	for i := 0; i < len(nt) && nt[i] != "" && n < len(files); n++ {
		parts := strings.SplitN(nt[i], "\t", 3)
		if len(parts) < 3 {
			return nil, fmt.Errorf("git numstat output is not what was expected")
		}
		if parts[2] == "" {
			i += 3
		} else {
			i++
		}
		if parts[0] == "-" {
			files[n].Status = "binary"
			continue
		}
		files[n].Added, _ = strconv.Atoi(parts[0])
		files[n].Removed, _ = strconv.Atoi(parts[1])
	}
	patch, err := run(patchMax)
	blocks := splitPatch(patch)
	if errors.Is(err, gitsync.ErrOutputCap) {
		// Git was stopped partway. The last block is incomplete and the files after the complete ones
		// have no hunks, which the cut says.
		if len(blocks) > 0 {
			blocks = blocks[:len(blocks)-1]
		}
		for i := range files {
			switch {
			case files[i].Status == "binary":
			case i < len(blocks):
				files[i].Hunks = blocks[i]
			default:
				files[i].HunksCut = true
			}
		}
		return files, nil
	}
	if err != nil {
		return nil, err
	}
	if len(blocks) == len(files) {
		for i := range files {
			if files[i].Status != "binary" {
				files[i].Hunks = blocks[i]
			}
		}
	} else {
		for i := range files {
			files[i].HunksCut = files[i].Status != "binary"
		}
	}
	return files, nil
}

// splitPatch cuts a patch at its `diff --git` lines.
func splitPatch(p string) []string {
	var out []string
	for len(p) > 0 {
		if !strings.HasPrefix(p, "diff --git ") {
			break
		}
		i := strings.Index(p[len("diff --git "):], "\ndiff --git ")
		if i < 0 {
			out = append(out, p)
			break
		}
		i += len("diff --git ")
		out = append(out, p[:i+1])
		p = p[i+1:]
	}
	return out
}

// untracked is every untracked file that is not ignored (limited to paths when given) as a
// whole-file addition. A symlink or anything but a regular file is listed with no content, and
// no file is read that safepath does not place inside dir.
func untracked(g gitAt, dir string, paths []string) []ChangeFile {
	args := []string{"ls-files", "--others", "--exclude-standard", "-z"}
	if len(paths) > 0 {
		args = append(args, "--")
		args = append(args, paths...)
	}
	// A listing stopped at the cap ends mid-name, so each one is cut back to its last whole name.
	list := func(args ...string) (string, bool) {
		o, err := g.capped(listMax, args...)
		if errors.Is(err, gitsync.ErrOutputCap) {
			return o[:strings.LastIndexByte(o, 0)+1], true
		}
		return o, err == nil
	}
	var out string
	if len(paths) == 0 {
		o, ok := list(args...)
		if !ok {
			return nil
		}
		out = o
	} else {
		head := args[:len(args)-len(paths)-1]
		for _, chunk := range chunkPaths(paths) {
			o, ok := list(append(append(append([]string{}, head...), "--"), chunk...)...)
			if !ok {
				return nil
			}
			out += o
		}
	}
	toks := strings.Split(out, "\x00")
	var files []ChangeFile
	for _, p := range toks {
		if p == "" {
			continue
		}
		if len(files) >= changesFilesMax {
			// The list is cut at this many, so reading more files would only be thrown away.
			files = append(files, ChangeFile{Path: p, Status: "added", HunksCut: true})
			continue
		}
		files = append(files, newFile(dir, p))
	}
	return files
}

func newFile(dir, rel string) ChangeFile {
	f := ChangeFile{Path: rel, Status: "added"}
	full := filepath.Join(dir, filepath.FromSlash(rel))
	info, err := os.Lstat(full)
	if err != nil || !info.Mode().IsRegular() {
		return f
	}
	if _, err := safepath.Contained(dir, full); err != nil {
		return f
	}
	if info.Size() > untrackedReadMax {
		f.HunksCut = true
		return f
	}
	b, err := os.ReadFile(full)
	if err != nil {
		return f
	}
	if bytes.IndexByte(b[:min(len(b), 8000)], 0) >= 0 {
		f.Status = "binary"
		return f
	}
	text := string(b)
	lines := strings.SplitAfter(text, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	f.Added = len(lines)
	if f.Added == 0 {
		return f
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "diff --git a/%s b/%s\nnew file mode 100644\n--- /dev/null\n+++ b/%s\n@@ -0,0 +1,%d @@\n", rel, rel, rel, f.Added)
	for _, l := range lines {
		sb.WriteString("+" + strings.TrimSuffix(l, "\n") + "\n")
	}
	if !strings.HasSuffix(text, "\n") {
		sb.WriteString("\\ No newline at end of file\n")
	}
	f.Hunks = sb.String()
	return f
}

// bound cuts the list to the file, per-file and total hunk bounds, and says what it cut.
func bound(files []ChangeFile) ([]ChangeFile, *ChangesCut) {
	if files == nil {
		return []ChangeFile{}, nil
	}
	cut := &ChangesCut{}
	if len(files) > changesFilesMax {
		cut.Files = len(files) - changesFilesMax
		files = files[:changesFilesMax]
	}
	total := 0
	for i := range files {
		f := &files[i]
		if len(f.Hunks) > changesFileMax || total+len(f.Hunks) > changesHunksMax {
			f.Hunks, f.HunksCut = "", true
		}
		total += len(f.Hunks)
		if f.HunksCut {
			cut.Hunks++
		}
	}
	if cut.Files == 0 && cut.Hunks == 0 {
		return files, nil
	}
	var why []string
	if cut.Files > 0 {
		why = append(why, fmt.Sprintf("%d files past the first %d are not listed", cut.Files, changesFilesMax))
	}
	if cut.Hunks > 0 {
		why = append(why, fmt.Sprintf("%d files show counts only, because a file over %d KB or hunks past %d MB are not sent",
			cut.Hunks, changesFileMax>>10, changesHunksMax>>20))
	}
	cut.Why = strings.Join(why, "; ")
	return files, cut
}
