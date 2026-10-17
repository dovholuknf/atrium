// Package worktrees audits the git worktrees of one repository: which are clean, which carry work the integration
// branch does not, and which could be removed. See docs/runtime/worktrees-tool-design.md.
//
// It runs git and nothing else. What only the room knows, whether atrium made a worktree and whether a card is still
// using it, comes in through Input.Judge, so the git half is tested against a real repository and the card half where
// the cards are.
package worktrees

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dovholuknf/atrium/internal/gitsync"
)

const (
	// CallBound is how long a whole audit may take, and CommandBound how long any one git command of it may.
	CallBound    = 60 * time.Second
	CommandBound = 20 * time.Second
	// MaxLines is how many worktrees an answer lists. The rest are counted in the trailer and not measured.
	MaxLines = 100
	// statusWorkers is how many `git status` run at once.
	statusWorkers = 6
	// subjectCut is how many characters of a subject a line shows.
	subjectCut = 40

	refHub   = "refs/remotes/hub/claude/main"
	refLocal = "refs/heads/claude/main"
)

// Reasons a worktree is kept, as they read on a line.
const (
	ReasonNotAtriums = "not atrium's"
	ReasonMain       = "main checkout"
	ReasonProtected  = "protected branch"
	ReasonDirty      = "dirty"
	ReasonUnlanded   = "unlanded"
	ReasonLocked     = "locked"
	ReasonNotListed  = "not a worktree"
	ReasonUnknown    = "unknown"
)

// protected is the branches no worktree's removal may take, besides the repo's default branch.
var protected = map[string]bool{"main": true, "master": true, "claude/main": true, "hub-main": true}

// Judgment is what the room says of a worktree path.
type Judgment struct {
	// Atriums is whether atrium made the worktree, by the ledger rule.
	Atriums bool
	// Later are the reasons that come after the git ones: a card in use, and reclaimBlock's sentence.
	Later []string
}

// Input is one audit.
type Input struct {
	// Repo is a directory inside any checkout or worktree of the repository.
	Repo string
	// Prefix and Paths narrow the answer. Both together are refused.
	Prefix string
	Paths  []string
	// Judge answers for the room. Nil reads as every worktree being atrium's, with nothing after.
	Judge func(path string) Judgment
	// Runner runs git. Nil takes a runner bounded at CommandBound.
	Runner *gitsync.Runner
}

// Line is one worktree, with the fields the line prints.
type Line struct {
	Path   string `json:"path"`
	Branch string `json:"branch"`
	// Missing is a directory git lists that is gone.
	Missing  bool `json:"missing,omitempty"`
	Main     bool `json:"main,omitempty"`
	Locked   bool `json:"locked,omitempty"`
	Detached bool `json:"detached,omitempty"`
	// Dirty is the count of status lines, less an untracked root BRIEF.md or REPORT.md, which are Flags.
	Dirty int      `json:"dirty"`
	Flags []string `json:"flags,omitempty"`
	// Landed is the weakest evidence, ancestry, patch or subject. Empty when Unlanded is not 0 or nothing was measured.
	Landed   string `json:"landed,omitempty"`
	Unlanded int    `json:"unlanded"`
	Newest   string `json:"newest,omitempty"`
	Subject  string `json:"subject,omitempty"`
	// Unknown is why git could not answer, and the line is never removable.
	Unknown   string   `json:"unknown,omitempty"`
	Removable bool     `json:"removable"`
	Prune     bool     `json:"prune,omitempty"`
	Keep      []string `json:"keep,omitempty"`
	// NotListed is a path that was asked for and git does not list.
	NotListed bool `json:"not_listed,omitempty"`

	head string
}

// Result is an audit. Text is the answer as the tool says it.
type Result struct {
	Ref    string `json:"ref"`
	RefSHA string `json:"ref_sha"`
	// From is `hub` or `local`.
	From  string `json:"from"`
	Total int    `json:"total"`
	Lines []Line `json:"lines"`
	// More is how many worktrees were left out past MaxLines.
	More         int    `json:"more,omitempty"`
	Unknown      int    `json:"unknown,omitempty"`
	FirstUnknown string `json:"first_unknown,omitempty"`
	Text         string `json:"text"`
}

// Refusal is an audit that was not run, and the sentence that says why.
type Refusal struct{ Why string }

func (r *Refusal) Error() string { return r.Why }

// Audit measures the worktrees of the repository.
func Audit(ctx context.Context, in Input) (*Result, error) {
	if strings.TrimSpace(in.Prefix) != "" && len(in.Paths) > 0 {
		return nil, &Refusal{"prefix and paths together are refused: give one"}
	}
	run := in.Runner
	if run == nil {
		run = gitsync.NewRunner()
		defer run.Stop(time.Second)
	}
	if run.Bound == 0 {
		run.Bound = CommandBound
	}
	ctx, cancel := context.WithTimeout(ctx, CallBound)
	defer cancel()
	repo := filepath.FromSlash(in.Repo)

	res := &Result{}
	for _, c := range []struct{ ref, from string }{{refHub, "hub"}, {refLocal, "local"}} {
		out, err := run.Git(ctx, repo, "rev-parse", "--verify", "-q", c.ref+"^{commit}")
		if err != nil {
			var ge *gitsync.Error
			if errors.As(err, &ge) && ge.First() == "" {
				continue
			}
			return nil, &Refusal{"git could not read " + c.ref + ": " + firstLine(err)}
		}
		res.Ref, res.RefSHA, res.From = c.ref, strings.TrimSpace(out), c.from
		break
	}
	if res.Ref == "" {
		return nil, &Refusal{"this repository has neither " + refHub + " nor " + refLocal + ", so nothing was measured"}
	}

	out, err := run.Git(ctx, repo, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, &Refusal{"git could not list the worktrees: " + firstLine(err)}
	}
	listed := parseList(out)
	defBranch := defaultBranch(ctx, run, repo)

	lines := selectLines(listed, in)
	res.Total = len(lines)
	lines, res.More = capLines(lines)
	measure(ctx, run, repo, res.Ref, lines)
	for i := range lines {
		verdict(&lines[i], defBranch, in.Judge)
		if lines[i].Unknown != "" {
			res.Unknown++
			if res.FirstUnknown == "" {
				res.FirstUnknown = lines[i].Unknown
			}
		}
	}
	res.Lines = lines
	res.Text = Format(res)
	return res, nil
}

// capLines keeps the first MaxLines and says how many it left out.
func capLines(lines []Line) ([]Line, int) {
	if len(lines) <= MaxLines {
		return lines, 0
	}
	return lines[:MaxLines], len(lines) - MaxLines
}

// parseList reads `git worktree list --porcelain`. The first entry is the main checkout.
func parseList(out string) []Line {
	var lines []Line
	var cur *Line
	flush := func() {
		if cur != nil {
			lines = append(lines, *cur)
			cur = nil
		}
	}
	for _, l := range strings.Split(strings.ReplaceAll(out, "\r", ""), "\n") {
		key, val, _ := strings.Cut(l, " ")
		switch key {
		case "":
			flush()
		case "worktree":
			flush()
			cur = &Line{Path: filepath.ToSlash(val), Main: len(lines) == 0}
		case "HEAD":
			if cur != nil {
				cur.head = val
			}
		case "branch":
			if cur != nil {
				cur.Branch = strings.TrimPrefix(val, "refs/heads/")
			}
		case "detached":
			if cur != nil {
				cur.Detached = true
			}
		case "bare":
			if cur != nil {
				cur.Branch = "(bare)"
			}
		case "locked":
			if cur != nil {
				cur.Locked = true
			}
		case "prunable":
			if cur != nil {
				cur.Missing = true
			}
		}
	}
	flush()
	for i := range lines {
		l := &lines[i]
		if l.Detached {
			l.Branch = "detached@" + short(l.head, 7)
		}
		if !l.Missing {
			if _, err := os.Stat(filepath.FromSlash(l.Path)); err != nil {
				l.Missing = true
			}
		}
		if l.Missing {
			l.Branch = "(missing)"
		}
	}
	return lines
}

// selectLines narrows the listing by prefix or paths. A path asked for and not listed is a line of its own.
func selectLines(listed []Line, in Input) []Line {
	var out []Line
	switch {
	case len(in.Paths) > 0:
		for _, p := range in.Paths {
			found := false
			for _, l := range listed {
				if SamePath(l.Path, p) {
					out, found = append(out, l), true
					break
				}
			}
			if !found {
				out = append(out, Line{Path: filepath.ToSlash(filepath.Clean(p)), Branch: "-", NotListed: true})
			}
		}
	case strings.TrimSpace(in.Prefix) != "":
		pre := norm(in.Prefix)
		for _, l := range listed {
			if strings.HasPrefix(norm(l.Path), pre) {
				out = append(out, l)
			}
		}
	default:
		out = listed
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// measure fills in dirt and landing for each line that has a directory, with the whole call's bound over it.
func measure(ctx context.Context, run *gitsync.Runner, repo, ref string, lines []Line) {
	var wg sync.WaitGroup
	sem := make(chan struct{}, statusWorkers)
	var tips []string
	seen := map[string]bool{}
	for i := range lines {
		l := &lines[i]
		if l.Missing || l.NotListed {
			continue
		}
		if l.head != "" && !seen[l.head] {
			seen[l.head] = true
			tips = append(tips, l.head)
		}
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			out, err := run.Git(ctx, filepath.FromSlash(l.Path), "-c", "core.quotepath=off", "status", "--porcelain")
			if err != nil {
				l.Unknown = "status: " + firstLine(err)
				return
			}
			l.Dirty, l.Flags = countDirty(out)
		}()
	}
	var landed map[string]gitsync.Landing
	var lerr error
	if len(tips) > 0 {
		landed, lerr = gitsync.LandedBy(ctx, run, repo, nil, ref, tips)
	}
	wg.Wait()
	for i := range lines {
		l := &lines[i]
		if l.Missing || l.NotListed {
			continue
		}
		if lerr != nil {
			if l.Unknown == "" {
				l.Unknown = "landed: " + firstLine(lerr)
			}
			continue
		}
		d, ok := landed[l.head]
		if !ok {
			if l.Unknown == "" {
				l.Unknown = "landed: no answer for " + short(l.head, 7)
			}
			continue
		}
		l.Landed, l.Unlanded, l.Newest, l.Subject = landedName(d.How), d.Unlanded, d.Newest, d.Subject
	}
}

// countDirty reads `git status --porcelain`: every line counts one but an untracked BRIEF.md or REPORT.md at the root.
func countDirty(out string) (n int, flags []string) {
	for _, l := range strings.Split(strings.ReplaceAll(out, "\r", ""), "\n") {
		switch strings.TrimRight(l, " ") {
		case "":
		case "?? BRIEF.md":
			flags = append(flags, "BRIEF")
		case "?? REPORT.md":
			flags = append(flags, "REPORT")
		default:
			n++
		}
	}
	return n, flags
}

func landedName(how string) string {
	if how == gitsync.HowPatchID {
		return "patch"
	}
	return how
}

// verdict decides a line's last column from what the audit can compute.
func verdict(l *Line, defBranch string, judge func(string) Judgment) {
	j := Judgment{Atriums: true}
	if judge != nil && !l.NotListed {
		j = judge(l.Path)
	}
	var keep []string
	add := func(r string) { keep = append(keep, r) }
	switch {
	case l.NotListed:
		add(ReasonNotListed)
	case l.Missing:
		if !j.Atriums {
			add(ReasonNotAtriums)
		}
		if l.Locked {
			add(ReasonLocked)
		}
		if len(keep) == 0 {
			l.Prune = true
		}
	default:
		if !j.Atriums {
			add(ReasonNotAtriums)
		}
		if l.Main {
			add(ReasonMain)
		}
		if !l.Detached && (protected[l.Branch] || (defBranch != "" && l.Branch == defBranch)) {
			add(ReasonProtected)
		}
		if l.Dirty > 0 {
			add(ReasonDirty)
		}
		if l.Unlanded > 0 {
			add(ReasonUnlanded)
		}
		if l.Locked {
			add(ReasonLocked)
		}
		if l.Unknown != "" {
			add(ReasonUnknown)
		}
		keep = append(keep, j.Later...)
	}
	l.Keep = keep
	l.Removable = len(keep) == 0
}

// defaultBranch is the branch origin's HEAD names, or empty when origin says none.
func defaultBranch(ctx context.Context, run *gitsync.Runner, repo string) string {
	out, err := run.Git(ctx, repo, "symbolic-ref", "--short", "refs/remotes/origin/HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(strings.TrimSpace(out), "origin/")
}

// Format is the answer as text: a header, a line per worktree and at most one trailer.
func Format(res *Result) string {
	var b strings.Builder
	n := fmt.Sprintf("%d worktrees", res.Total)
	if res.Total == 1 {
		n = "1 worktree"
	}
	fmt.Fprintf(&b, "%s %s from %s, %s\n", strings.TrimPrefix(strings.TrimPrefix(res.Ref, "refs/remotes/hub/"), "refs/heads/"), short(res.RefSHA, 8), res.From, n)
	width := 0
	for _, l := range res.Lines {
		width = max(width, len([]rune(l.Path)))
	}
	for _, l := range res.Lines {
		b.WriteString(l.Format(width))
		b.WriteByte('\n')
	}
	var tr []string
	if res.More > 0 {
		tr = append(tr, fmt.Sprintf("%d more, narrow with prefix or paths", res.More))
	}
	if res.Unknown > 0 {
		tr = append(tr, fmt.Sprintf("%d unknown: %s", res.Unknown, res.FirstUnknown))
	}
	if len(tr) > 0 {
		b.WriteString(strings.Join(tr, "  ") + "\n")
	}
	return b.String()
}

// Format is one worktree's line, its path padded to width.
func (l Line) Format(width int) string {
	pad := max(0, width-len([]rune(l.Path)))
	dirt, landed := "-", "-"
	if !l.Missing && !l.NotListed {
		dirt = "clean"
		if l.Dirty > 0 {
			dirt = fmt.Sprintf("dirty %d", l.Dirty)
		}
		for _, f := range l.Flags {
			dirt += " +" + f
		}
		switch {
		case l.Unknown != "":
			dirt, landed = "unknown", "unknown"
		case l.Unlanded > 0:
			landed = fmt.Sprintf("unlanded %d %s %q", l.Unlanded, short(l.Newest, 7), cut(l.Subject, subjectCut))
			if l.Unlanded > 1 {
				landed += fmt.Sprintf(" +%d", l.Unlanded-1)
			}
		default:
			landed = "landed:" + l.Landed
		}
	}
	v := "removable"
	switch {
	case l.Prune:
		v = "removable: prune"
	case !l.Removable:
		v = "keep: " + strings.Join(l.Keep, ", ")
	}
	return l.Path + strings.Repeat(" ", pad) + "  " + l.Branch + "  " + dirt + "  " + landed + "  " + v
}

func cut(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) > n {
		r = r[:n]
	}
	return string(r)
}

func short(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// firstLine is the first line of a git error.
func firstLine(err error) string {
	var ge *gitsync.Error
	if errors.As(err, &ge) {
		if s := ge.First(); s != "" {
			return s
		}
	}
	s, _, _ := strings.Cut(strings.TrimSpace(err.Error()), "\n")
	return s
}

// SamePath is whether two paths are one, after Clean and slashes, and case where the filesystem folds it.
func SamePath(a, b string) bool { return norm(a) == norm(b) }

// resolve follows symlinks and expands Windows short names (RUNNER~1) in the part of p that exists, so /var/x and
// /private/var/x on macOS, or a short and a long Windows path, name the same place. A directory that is gone keeps its
// last element: its parent is resolved and the name is put back.
func resolve(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	dir, base := filepath.Split(p)
	if dir == "" || dir == p || base == "" {
		return p
	}
	return filepath.Join(resolve(filepath.Clean(dir)), base)
}

func norm(p string) string {
	p = strings.TrimSpace(strings.ReplaceAll(p, `\`, "/"))
	p = strings.TrimPrefix(p, "//?/")
	if p == "" {
		return ""
	}
	p = filepath.ToSlash(resolve(filepath.Clean(filepath.FromSlash(p))))
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		p = strings.ToLower(p)
	}
	return p
}
