// Package deployready answers one question for the hub: is the tip of the integration branch safe to deploy, and if
// not, which commits are in the way. See docs/rnd/factory-shape.md, "(a) The hub deploys itself", and @review's five
// required changes in docs/backlog/rnd/rd-new-review-ed68cd81.md, which this follows point by point.
//
// It reads git and writes nothing. It never deploys, and nothing here is on a timer that does.
//
// A VERDICT IS A TRAILER ON A REVIEW COMMIT:
//
//	Atrium-Verdict: hub-ok  <base>..<tip>
//	Atrium-Verdict: room-ok <base>..<tip>
//	Atrium-Verdict: hold    <sha>
//
// and what it covers is decided by PATCH, not by SHA. Landings rebase, and the same change reaches the branch under
// a new SHA, so a verdict is matched to a landed commit by `git patch-id --stable`. A change that was altered on the
// way gets another patch-id and so has no verdict, which is the safe answer: the code that was read is not the code
// that landed.
package deployready

import (
	"context"
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Git is what a check needs of git. *gitsync.Runner has both methods.
type Git interface {
	Git(ctx context.Context, dir string, args ...string) (string, error)
	GitInput(ctx context.Context, dir string, stdin []byte, args ...string) (string, error)
}

// Why a commit blocks a deploy.
const (
	WhyNoVerdict     = "no-verdict"
	WhyHold          = "hold"
	WhyNoRoomVerdict = "no-room-verdict"
	WhyRoomHold      = "room-hold"
)

// States of a Report.
const (
	// StateReady means every commit that needs a verdict has one, and there is something to deploy.
	StateReady = "ready"
	// StateBlocked means at least one commit is still in the way. Blocking names them.
	StateBlocked = "blocked"
	// StateCurrent means nothing that needs a verdict has landed since the installed build.
	StateCurrent = "current"
	// StateUnknown means the check could not be made. Error says why. It is never ready.
	StateUnknown = "unknown"
)

// Blocker is one commit that keeps the deploy from being ready.
type Blocker struct {
	SHA     string `json:"sha"`
	Short   string `json:"short"`
	Subject string `json:"subject"`
	Why     string `json:"why"`
	// Detail is the sentence the board shows.
	Detail string `json:"detail"`
}

// Report is the answer. Ready is true only in StateReady.
type Report struct {
	State     string `json:"state"`
	Ready     bool   `json:"ready"`
	Branch    string `json:"branch,omitempty"`
	Tip       string `json:"tip,omitempty"`
	Installed string `json:"installed,omitempty"`
	// Commits is how many commits since the installed build need a verdict.
	Commits  int       `json:"commits"`
	Blocking []Blocker `json:"blocking,omitempty"`
	// Notes are things that did not stop the check but are worth seeing: a verdict that was ignored and why.
	Notes     []string  `json:"notes,omitempty"`
	Error     string    `json:"error,omitempty"`
	CheckedAt time.Time `json:"checked_at"`
	// Line is the one sentence a board puts on the button.
	Line string `json:"line"`
}

// Signature is what changes when the answer does, for deciding whether to tell a watching board.
func (r Report) Signature() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s|%s|%s|%d|%s", r.State, r.Tip, r.Installed, r.Commits, r.Error)
	for _, k := range r.Blocking {
		fmt.Fprintf(&b, "|%s:%s", k.SHA, k.Why)
	}
	return b.String()
}

// Checker reads one repository's integration branch. Safe for concurrent use.
type Checker struct {
	Git    Git
	Dir    string
	Branch string

	// MaxVerdicts bounds how many review commits are read. Zero takes DefaultMaxVerdicts.
	MaxVerdicts int

	mu    sync.Mutex
	cache map[string]commitInfo
}

// DefaultMaxVerdicts is far more review commits than sit between two deploys.
const DefaultMaxVerdicts = 1000

// commitInfo is what a SHA says. It never changes, so it is cached forever.
type commitInfo struct {
	patch string
	paths []string
}

// Path rules, section 4 of the review. A commit needs a verdict when it changes something that ships in the binary or
// runs the deploy.
var (
	needsPrefix = []string{"internal/", "cmd/", "scripts/"}
	// needsFile are single files outside those trees that change what is built.
	needsFile = []string{"go.mod", "go.sum"}
	// testOnly are files under a needs-verdict tree that cannot reach a deployed binary.
	testOnly = []string{"scripts/test-board-headless.js"}
	// hubOnly are the packages only the hub runs. A commit inside them is covered by a hub verdict alone. Any other
	// Go or board file also runs in the room, which restarts on this binary, so it needs a room verdict too.
	hubOnly  = []string{"internal/link/", "internal/hubstore/"}
	imageExt = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".svg": true, ".ico": true,
		".webp": true}
)

// Needs reports whether a changed path needs a verdict.
func Needs(p string) bool {
	p = strings.TrimPrefix(path.Clean(strings.ReplaceAll(p, "\\", "/")), "./")
	ext := strings.ToLower(path.Ext(p))
	if ext == ".md" || imageExt[ext] {
		return false
	}
	for _, t := range testOnly {
		if p == t {
			return false
		}
	}
	for _, f := range needsFile {
		if p == f {
			return true
		}
	}
	for _, pre := range needsPrefix {
		if strings.HasPrefix(p, pre) {
			return true
		}
	}
	return false
}

// RoomSide reports whether a path that needs a verdict also runs in the room.
func RoomSide(p string) bool {
	p = strings.TrimPrefix(path.Clean(strings.ReplaceAll(p, "\\", "/")), "./")
	if !strings.HasPrefix(p, "internal/") && !strings.HasPrefix(p, "cmd/") && p != "go.mod" && p != "go.sum" {
		return false
	}
	for _, h := range hubOnly {
		if strings.HasPrefix(p, h) {
			return false
		}
	}
	return true
}

// reviewFile is a path a review commit may touch: docs/backlog/<dept>/<anything>-review-<anything>.md. A verdict on a
// commit that touches anything else is refused, which stops one folded into a code commit and nothing more. Every
// session shares one git author, so this is the same trust the prose line had.
func reviewFile(p string) bool {
	p = strings.ReplaceAll(p, "\\", "/")
	parts := strings.Split(p, "/")
	if len(parts) != 4 || parts[0] != "docs" || parts[1] != "backlog" {
		return false
	}
	name := parts[3]
	return strings.HasSuffix(name, ".md") && strings.Contains(name, "-review-")
}

// Verdict kinds a trailer may carry.
const (
	kindHub  = "hub-ok"
	kindRoom = "room-ok"
	kindHold = "hold"
)

type trailer struct {
	kind string
	spec string
}

var (
	trailerRe = regexp.MustCompile(`(?m)^Atrium-Verdict:[ \t]+(hub-ok|room-ok|hold)[ \t]+(\S+)[ \t]*$`)
	specRe    = regexp.MustCompile(`^[0-9A-Za-z_./~^@{}-]+$`)
	shaRe     = regexp.MustCompile(`^[0-9a-f]{7,64}$`)
)

// parseTrailers reads every well-formed verdict trailer in a commit message. A malformed one is not a verdict.
func parseTrailers(msg string) []trailer {
	var out []trailer
	for _, m := range trailerRe.FindAllStringSubmatch(msg, -1) {
		spec := m[2]
		if !specRe.MatchString(spec) || strings.HasPrefix(spec, "-") {
			continue
		}
		out = append(out, trailer{kind: m[1], spec: spec})
	}
	return out
}

// dims is what the newest verdicts say about one patch. "" none, "ok", or "hold".
type dims struct{ hub, room string }

func (d *dims) apply(kind string) {
	switch kind {
	case kindHub:
		d.hub = "ok"
	case kindRoom:
		d.room = "ok"
	case kindHold:
		d.hub, d.room = "hold", "hold"
	}
}

// Check is the answer for the branch tip against the build installed, named by its commit.
func (c *Checker) Check(ctx context.Context, installed string) Report {
	rep := Report{Branch: c.Branch, CheckedAt: time.Now().UTC()}
	fail := func(format string, a ...any) Report {
		rep.State, rep.Ready = StateUnknown, false
		rep.Error = fmt.Sprintf(format, a...)
		rep.Line = "deploy readiness unknown: " + rep.Error
		return rep
	}
	if !shaRe.MatchString(strings.ToLower(strings.TrimSpace(installed))) {
		return fail("the installed build does not name a commit (%q)", installed)
	}
	installed = strings.ToLower(strings.TrimSpace(installed))

	tip, err := c.rev(ctx, "refs/heads/"+c.Branch)
	if err != nil {
		return fail("could not read %s in %s: %v", c.Branch, c.Dir, err)
	}
	rep.Tip = tip
	inst, err := c.rev(ctx, installed)
	if err != nil {
		return fail("the installed build %s is not in this checkout", short(installed))
	}
	rep.Installed = inst
	if _, err := c.Git.Git(ctx, c.Dir, "merge-base", "--is-ancestor", inst, tip); err != nil {
		return fail("the installed build %s is not an ancestor of %s", short(inst), c.Branch)
	}

	cands, err := c.candidates(ctx, inst, tip)
	if err != nil {
		return fail("could not read the commits since %s: %v", short(inst), err)
	}
	var need []candidate
	for _, k := range cands {
		for _, p := range k.paths {
			if Needs(p) {
				need = append(need, k)
				break
			}
		}
	}
	rep.Commits = len(need)
	if len(need) == 0 {
		rep.State = StateCurrent
		rep.Line = fmt.Sprintf("nothing to deploy: no code has landed on %s since %s", c.Branch, short(inst))
		return rep
	}

	verdicts, notes, err := c.verdicts(ctx)
	rep.Notes = notes
	if err != nil {
		return fail("could not read the verdicts: %v", err)
	}
	for _, k := range need {
		d := verdicts[k.patch]
		if k.patch == "" {
			d = dims{}
		}
		why, detail := "", ""
		switch {
		case d.hub == "hold":
			why, detail = WhyHold, "held by a review verdict"
		case d.hub != "ok":
			why, detail = WhyNoVerdict, "no verdict covers this patch"
		case anyRoomSide(k.paths) && d.room == "hold":
			why, detail = WhyRoomHold, "held for the room by a review verdict"
		case anyRoomSide(k.paths) && d.room != "ok":
			why, detail = WhyNoRoomVerdict, "room-side code with no room verdict"
		}
		if why != "" {
			rep.Blocking = append(rep.Blocking, Blocker{SHA: k.sha, Short: short(k.sha), Subject: k.subject,
				Why: why, Detail: detail})
		}
	}
	if len(rep.Blocking) > 0 {
		rep.State = StateBlocked
		rep.Line = blockedLine(rep)
		return rep
	}
	rep.State, rep.Ready = StateReady, true
	rep.Line = fmt.Sprintf("deploy ready: %s, %d commit%s, all verdicts in", short(tip), rep.Commits, plural(rep.Commits))
	return rep
}

func blockedLine(r Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "not ready: %d of %d commit%s blocking", len(r.Blocking), r.Commits, plural(r.Commits))
	for i, k := range r.Blocking {
		if i == 3 {
			fmt.Fprintf(&b, ", and %d more", len(r.Blocking)-3)
			break
		}
		sep := ": "
		if i > 0 {
			sep = ", "
		}
		fmt.Fprintf(&b, "%s%s %s (%s)", sep, k.Short, k.Subject, k.Why)
	}
	return b.String()
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func anyRoomSide(paths []string) bool {
	for _, p := range paths {
		if Needs(p) && RoomSide(p) {
			return true
		}
	}
	return false
}

func short(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}

func (c *Checker) rev(ctx context.Context, name string) (string, error) {
	out, err := c.Git.Git(ctx, c.Dir, "rev-parse", "--verify", "--quiet", name+"^{commit}")
	if err != nil {
		return "", err
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return "", errors.New("no such commit")
	}
	return out, nil
}

type candidate struct {
	sha, subject string
	commitInfo
}

// candidates are the commits in base..tip, oldest first: every non-merge commit, and a merge only when it carries a
// change of its own (a conflict resolution, which --remerge-diff shows).
func (c *Checker) candidates(ctx context.Context, base, tip string) ([]candidate, error) {
	spec := base + ".." + tip
	if err := c.fillRange(ctx, spec); err != nil {
		return nil, err
	}
	out, err := c.Git.Git(ctx, c.Dir, "log", "--reverse", "--format=%H%x1f%s", spec)
	if err != nil {
		return nil, err
	}
	merges, err := c.mergeSet(ctx, spec)
	if err != nil {
		return nil, err
	}
	var res []candidate
	for _, line := range strings.Split(out, "\n") {
		sha, subj, ok := strings.Cut(strings.TrimSpace(line), "\x1f")
		if !ok {
			continue
		}
		if merges[sha] {
			c.fillMerge(ctx, sha)
		}
		info := c.info(sha)
		if len(info.paths) == 0 {
			continue
		}
		res = append(res, candidate{sha: sha, subject: subj, commitInfo: info})
	}
	return res, nil
}

func (c *Checker) mergeSet(ctx context.Context, spec string, extra ...string) (map[string]bool, error) {
	args := append([]string{"rev-list", "--merges"}, extra...)
	out, err := c.Git.Git(ctx, c.Dir, append(args, spec)...)
	if err != nil {
		return nil, err
	}
	set := map[string]bool{}
	for _, l := range strings.Fields(out) {
		set[l] = true
	}
	return set, nil
}

func (c *Checker) info(sha string) commitInfo {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cache[sha]
}

func (c *Checker) put(sha string, i commitInfo) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cache == nil {
		c.cache = map[string]commitInfo{}
	}
	c.cache[sha] = i
}

func (c *Checker) known(sha string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.cache[sha]
	return ok
}

// fillRange caches the patch-id and the paths of every non-merge commit in a range, in two git calls. No renames, so
// a move is a delete and an add in both the paths and the patch, whoever computes it.
func (c *Checker) fillRange(ctx context.Context, spec string, extra ...string) error {
	base := append([]string{"log", "--no-merges", "--no-renames"}, extra...)
	names, err := c.Git.Git(ctx, c.Dir, append(append([]string{}, base...), "--name-only", "--format=%x1e%H", spec)...)
	if err != nil {
		return err
	}
	paths := map[string][]string{}
	var order []string
	for _, chunk := range strings.Split(names, "\x1e") {
		lines := strings.Split(strings.TrimSpace(chunk), "\n")
		if len(lines) == 0 || lines[0] == "" {
			continue
		}
		sha := strings.TrimSpace(lines[0])
		order = append(order, sha)
		for _, l := range lines[1:] {
			if l = strings.TrimSpace(l); l != "" {
				paths[sha] = append(paths[sha], l)
			}
		}
	}
	patches := map[string]string{}
	if len(order) > 0 {
		patchText, err := c.Git.Git(ctx, c.Dir, append(append([]string{}, base...), "-p", spec)...)
		if err != nil {
			return err
		}
		pids, err := c.patchIDs(ctx, patchText)
		if err != nil {
			return err
		}
		patches = pids
	}
	for _, sha := range order {
		c.put(sha, commitInfo{patch: patches[sha], paths: paths[sha]})
	}
	return nil
}

// patchIDs maps commit to `git patch-id --stable` for a stream of `git log -p` output.
func (c *Checker) patchIDs(ctx context.Context, patchText string) (map[string]string, error) {
	res := map[string]string{}
	if strings.TrimSpace(patchText) == "" {
		return res, nil
	}
	out, err := c.Git.GitInput(ctx, c.Dir, []byte(patchText), "patch-id", "--stable")
	if err != nil {
		return nil, err
	}
	for _, l := range strings.Split(out, "\n") {
		f := strings.Fields(l)
		if len(f) == 2 {
			res[f[1]] = f[0]
		}
	}
	return res, nil
}

// fillMerge caches a merge by its remerge-diff, which is empty for a merge that resolved nothing. A git too old to
// know --remerge-diff leaves the merge with no paths, so it counts as carrying no change of its own.
func (c *Checker) fillMerge(ctx context.Context, sha string) {
	if c.known(sha) {
		return
	}
	args := []string{"show", "--remerge-diff", "--no-renames"}
	names, err := c.Git.Git(ctx, c.Dir, append(append([]string{}, args...), "--name-only", "--format=", sha)...)
	if err != nil {
		c.put(sha, commitInfo{})
		return
	}
	var paths []string
	for _, l := range strings.Split(names, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			paths = append(paths, l)
		}
	}
	info := commitInfo{paths: paths}
	if len(paths) > 0 {
		if text, err := c.Git.Git(ctx, c.Dir, append(append([]string{}, args...), "--format=commit "+sha, sha)...); err == nil {
			if ids, err := c.patchIDs(ctx, text); err == nil {
				info.patch = ids[sha]
			}
		}
	}
	c.put(sha, info)
}

// verdictCommit is one review commit and what it said.
type verdictCommit struct {
	sha      string
	trailers []trailer
	files    []string
}

// verdicts reads every verdict on the branch and returns, per patch-id, what the newest ones say.
//
// Newest wins, in the branch's own order, and a conditional OK is an OK. The condition stays in the review's prose,
// and the reviewer holds the deploy if it is not met. A range covers `git rev-list --first-parent base..tip`, which
// is how a review reads: one branch tip. Without --first-parent a verdict would cover every claude/main commit that
// was merged into the reviewed branch, none of which the reviewer read.
func (c *Checker) verdicts(ctx context.Context) (map[string]dims, []string, error) {
	max := c.MaxVerdicts
	if max <= 0 {
		max = DefaultMaxVerdicts
	}
	out, err := c.Git.Git(ctx, c.Dir, "log", "--no-merges", "--no-renames", "--name-only", "--date-order",
		"--extended-regexp", "--grep=^Atrium-Verdict:", fmt.Sprintf("--max-count=%d", max),
		"--format=%x1e%H%x1f%B%x1f", "refs/heads/"+c.Branch)
	if err != nil {
		return nil, nil, err
	}
	var vcs []verdictCommit
	for _, chunk := range strings.Split(out, "\x1e") {
		parts := strings.Split(chunk, "\x1f")
		if len(parts) < 3 {
			continue
		}
		v := verdictCommit{sha: strings.TrimSpace(parts[0]), trailers: parseTrailers(parts[1])}
		for _, l := range strings.Split(parts[2], "\n") {
			if l = strings.TrimSpace(l); l != "" {
				v.files = append(v.files, l)
			}
		}
		if len(v.trailers) > 0 {
			vcs = append(vcs, v)
		}
	}
	var notes []string
	res := map[string]dims{}
	covers := map[string][]string{}
	// The log is newest first. Apply oldest first so the newest lands last.
	for i := len(vcs) - 1; i >= 0; i-- {
		v := vcs[i]
		if len(v.files) == 0 || !allReview(v.files) {
			notes = append(notes, fmt.Sprintf("verdict on %s ignored: a verdict may only sit on a commit that touches "+
				"review files and nothing else", short(v.sha)))
			continue
		}
		for _, t := range v.trailers {
			ids, ok := covers[t.spec]
			if !ok {
				var err error
				ids, err = c.cover(ctx, t.spec)
				if err != nil {
					notes = append(notes, fmt.Sprintf("verdict %s %s on %s ignored: %v", t.kind, t.spec, short(v.sha), err))
					continue
				}
				covers[t.spec] = ids
			}
			for _, id := range ids {
				d := res[id]
				d.apply(t.kind)
				res[id] = d
			}
		}
	}
	return res, notes, nil
}

func allReview(files []string) bool {
	for _, f := range files {
		if !reviewFile(f) {
			return false
		}
	}
	return true
}

// cover is the patch-ids a verdict spec covers: `<base>..<tip>` as a first-parent range, or one commit.
func (c *Checker) cover(ctx context.Context, spec string) ([]string, error) {
	rng := spec
	if !strings.Contains(spec, "..") {
		rng = spec + "^!"
	}
	if _, err := c.Git.Git(ctx, c.Dir, "rev-list", "--first-parent", "--max-count=1", rng); err != nil {
		return nil, errors.New("that range is not in this checkout")
	}
	if err := c.fillRange(ctx, rng, "--first-parent"); err != nil {
		return nil, err
	}
	shas, err := c.Git.Git(ctx, c.Dir, "rev-list", "--first-parent", rng)
	if err != nil {
		return nil, err
	}
	merges, err := c.mergeSet(ctx, rng, "--first-parent")
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, sha := range strings.Fields(shas) {
		if merges[sha] {
			c.fillMerge(ctx, sha)
		}
		if p := c.info(sha).patch; p != "" {
			ids = append(ids, p)
		}
	}
	return ids, nil
}
