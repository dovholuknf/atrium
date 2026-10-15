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
	Notes []string `json:"notes,omitempty"`
	Error string   `json:"error,omitempty"`
	// Stale means this is the last answer, handed back because a newer read is still running. Never ready to act on.
	Stale     bool      `json:"stale,omitempty"`
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
	// MaxCommits bounds how many commits one range may walk, for the commits since the installed build and for each
	// verdict's range. Zero takes DefaultMaxCommits.
	MaxCommits int

	mu     sync.Mutex
	last   *memo
	cache  map[string]commitInfo
	covers map[string]coverResult
}

// DefaultMaxCommits is well past what lands between two deploys. A range longer than this answers unknown.
const DefaultMaxCommits = 2000

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
	// internal/link is NOT here: the room runs link.Join, Dialer, Direct, RelayRequest, Keys, Zrok and Ziti.
	hubOnly  = []string{"internal/hubstore/"}
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

	tip, inst, err := c.revs(ctx, installed)
	if err != nil {
		return fail("%v", err)
	}
	rep.Tip, rep.Installed = tip, inst

	// The same tip against the same build is the same answer, so a pass that finds nothing moved runs one git.
	if m, ok := c.memoFor(tip, inst); ok {
		m.CheckedAt = rep.CheckedAt
		return m
	}

	cands, err := c.candidates(ctx, inst, tip)
	if err == nil {
		err = ctx.Err() // a merge read that was cut off is skipped quietly, and the pass must not go on without it
	}
	if errors.Is(err, errNotAncestor) {
		return fail("the installed build %s is not an ancestor of %s", short(inst), c.Branch)
	}
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
		return c.remember(rep, true)
	}

	verdicts, notes, pinned, err := c.verdicts(ctx)
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
		return c.remember(rep, pinned)
	}
	rep.State, rep.Ready = StateReady, true
	rep.Line = fmt.Sprintf("deploy ready: %s, %d commit%s, all verdicts in", short(tip), rep.Commits, plural(rep.Commits))
	return c.remember(rep, pinned)
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
	// ONE LOG NAMES THE RANGE, its parents and its subjects. The symmetric range marks a commit of the installed build
	// that the tip lacks with `<`, which is the installed build not being an ancestor, so no separate ancestry check.
	limit := c.maxCommits()
	out, err := c.Git.Git(ctx, c.Dir, "log", "--reverse", "--left-right", fmt.Sprintf("--max-count=%d", limit+1),
		"--format=%m%x1f%H%x1f%P%x1f%s", base+"..."+tip)
	if err != nil {
		return nil, err
	}
	type listed struct{ sha, subject string }
	var all []listed
	var nonMerge []string
	merges := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		f := strings.SplitN(strings.TrimSpace(line), "\x1f", 4)
		if len(f) < 4 {
			continue
		}
		if f[0] == "<" {
			return nil, errNotAncestor
		}
		all = append(all, listed{f[1], f[3]})
		if len(strings.Fields(f[2])) > 1 {
			merges[f[1]] = true
		} else {
			nonMerge = append(nonMerge, f[1])
		}
	}
	if len(all) > limit {
		return nil, fmt.Errorf("more than the %d commits that are read", limit)
	}
	spec := base + ".." + tip
	if err := c.fillRangeOf(ctx, spec, nonMerge); err != nil {
		return nil, err
	}
	if err := c.fillMerges(ctx, merges, spec); err != nil {
		return nil, err
	}
	var res []candidate
	for _, k := range all {
		info := c.info(k.sha)
		if len(info.paths) == 0 {
			continue
		}
		res = append(res, candidate{sha: k.sha, subject: k.subject, commitInfo: info})
	}
	return res, nil
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

func (c *Checker) maxCommits() int {
	if c.MaxCommits > 0 {
		return c.MaxCommits
	}
	return DefaultMaxCommits
}

// fillRangeOf caches the patch-id and the paths of every non-merge commit of a range, in two git calls plus the
// patch-id. No renames, so a move is a delete and an add in both the paths and the patch. A range whose commits are all
// cached costs nothing, which is what makes the second pass fast. The caller has the range's non-merge commits.
func (c *Checker) fillRangeOf(ctx context.Context, spec string, nonMerge []string, extra ...string) error {
	limit := fmt.Sprintf("--max-count=%d", c.maxCommits())
	allKnown := true
	for _, sha := range nonMerge {
		if !c.known(sha) {
			allKnown = false
			break
		}
	}
	if allKnown {
		return nil
	}
	// ONE LOG gives the paths (the raw lines) and the patch under them, and what patch-id reads is the same log
	// without the raw lines.
	args := append([]string{"log", "--no-merges", "--no-renames", limit}, extra...)
	text, err := c.Git.Git(ctx, c.Dir, append(args, "--raw", "-p", "--format=%x1ecommit %H", spec)...)
	if err != nil {
		return err
	}
	paths := map[string][]string{}
	var order []string
	var patchText strings.Builder
	for _, chunk := range strings.Split(text, "\x1e") {
		chunk = strings.TrimLeft(chunk, "\r\n")
		if chunk == "" {
			continue
		}
		var sha string
		for _, l := range strings.Split(chunk, "\n") {
			switch {
			case strings.HasPrefix(l, "commit "):
				sha = strings.TrimSpace(strings.TrimPrefix(l, "commit "))
				order = append(order, sha)
			case strings.HasPrefix(l, ":"):
				if _, p, ok := strings.Cut(l, "\t"); ok && sha != "" {
					paths[sha] = append(paths[sha], strings.TrimSpace(p))
				}
				continue // a raw line is for the paths, and patch-id is not given it
			}
			patchText.WriteString(l)
			patchText.WriteByte('\n')
		}
	}
	patches, err := c.patchIDs(ctx, patchText.String())
	if err != nil {
		return err
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

// fillMerges caches a merge by its remerge-diff, which is empty for a merge that resolved nothing. A git too old to
// know --remerge-diff leaves the merge with no paths, so it counts as carrying no change of its own. That is the ONLY
// failure read as "carries nothing". Any other, a cut-off included, fails the pass and caches nothing, because a
// conflict resolution cached as empty would need no verdict for the life of the process.
//
// The merges of a range are read together, two git calls however many there are. One process per merge was most of
// the time a cold pass took.
func (c *Checker) fillMerges(ctx context.Context, merges map[string]bool, spec string, extra ...string) error {
	var todo []string
	for sha := range merges {
		if !c.known(sha) {
			todo = append(todo, sha)
		}
	}
	if len(todo) == 0 {
		return nil
	}
	base := append(append([]string{"log", "--merges", "--remerge-diff", "--no-renames"}, extra...), spec)
	names, err := c.Git.Git(ctx, c.Dir, append([]string{base[0], "--name-only", "--format=%x1e%H"}, base[1:]...)...)
	if err != nil {
		if ctx.Err() == nil && gitTooOld(err) {
			for _, sha := range todo {
				c.put(sha, commitInfo{})
			}
			return nil
		}
		return fmt.Errorf("could not read the merges: %w", err)
	}
	paths := parseNamed(names)
	var patches map[string]string
	for _, p := range paths {
		if len(p) > 0 {
			text, err := c.Git.Git(ctx, c.Dir, append([]string{base[0], "-p"}, base[1:]...)...)
			if err == nil {
				patches, err = c.patchIDs(ctx, text)
			}
			if err != nil {
				return fmt.Errorf("could not read the merges: %w", err)
			}
			break
		}
	}
	for _, sha := range todo {
		if p, ok := paths[sha]; ok { // only a merge git actually listed
			c.put(sha, commitInfo{patch: patches[sha], paths: p})
		}
	}
	return nil
}

// gitTooOld recognises git refusing --remerge-diff as an option it does not know, which is not a read that failed.
func gitTooOld(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "remerge-diff") &&
		(strings.Contains(msg, "unknown option") || strings.Contains(msg, "unrecognized argument") ||
			strings.Contains(msg, "invalid option") || strings.Contains(msg, "bad argument"))
}

// parseNamed reads `git log --name-only --format=%x1e%H` into the paths of each commit.
func parseNamed(out string) map[string][]string {
	res := map[string][]string{}
	for _, chunk := range strings.Split(out, "\x1e") {
		lines := strings.Split(strings.TrimSpace(chunk), "\n")
		if len(lines) == 0 || lines[0] == "" {
			continue
		}
		sha := strings.TrimSpace(lines[0])
		res[sha] = nil
		for _, l := range lines[1:] {
			if l = strings.TrimSpace(l); l != "" {
				res[sha] = append(res[sha], l)
			}
		}
	}
	return res
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
func (c *Checker) verdicts(ctx context.Context) (map[string]dims, []string, bool, error) {
	return c.verdictsOn(ctx, "refs/heads/"+c.Branch)
}

// verdictsOn is verdicts read from the named refs together, newest first across all of them. An item's own branch is read
// beside the integration branch, since a reviewer may put the verdict commit on either.
func (c *Checker) verdictsOn(ctx context.Context, refs ...string) (map[string]dims, []string, bool, error) {
	max := c.MaxVerdicts
	if max <= 0 {
		max = DefaultMaxVerdicts
	}
	args := append([]string{"log", "--no-merges", "--no-renames", "--name-only", "--date-order",
		"--extended-regexp", "--grep=^Atrium-Verdict:", fmt.Sprintf("--max-count=%d", max),
		"--format=%x1e%H%x1f%B%x1f"}, refs...)
	out, err := c.Git.Git(ctx, c.Dir, args...)
	if err != nil {
		return nil, nil, false, err
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
	pinned := true
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
			pinned = pinned && pinnedSpec.MatchString(t.spec)
			ids, ok := covers[t.spec]
			if !ok {
				var err error
				var note string
				ids, note, err = c.cover(ctx, t.spec)
				if ctx.Err() != nil {
					return nil, nil, false, ctx.Err()
				}
				if err != nil {
					notes = append(notes, fmt.Sprintf("verdict %s %s on %s ignored: %v", t.kind, t.spec, short(v.sha), err))
					continue
				}
				if note != "" {
					notes = append(notes, note)
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
	return res, notes, pinned, nil
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
// The note is set when the range was cut at the commit limit, so the commits past it are not covered.
func (c *Checker) cover(ctx context.Context, spec string) (ids []string, note string, err error) {
	if r, ok := c.coverCached(spec); ok {
		return r.ids, r.note, nil
	}
	ids, note, err = c.coverRead(ctx, spec)
	if err == nil && ctx.Err() == nil {
		c.coverPut(spec, coverResult{ids, note})
	}
	return ids, note, err
}

type coverResult struct {
	ids  []string
	note string
}

// pinnedSpec is a spec made only of commit names, which mean the same thing every time they are read. A branch name
// or a tag can move, so a spec that has one is read again.
var pinnedSpec = regexp.MustCompile(`^[0-9a-f]{7,40}(?:[~^][0-9]*)*(?:\.\.[0-9a-f]{7,40}(?:[~^][0-9]*)*)?$`)

func (c *Checker) coverCached(spec string) (coverResult, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	r, ok := c.covers[spec]
	return r, ok
}

func (c *Checker) coverPut(spec string, r coverResult) {
	if !pinnedSpec.MatchString(spec) {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.covers == nil {
		c.covers = map[string]coverResult{}
	}
	c.covers[spec] = r
}

func (c *Checker) coverRead(ctx context.Context, spec string) ([]string, string, error) {
	rng := spec
	if !strings.Contains(spec, "..") {
		rng = spec + "^!"
	}
	limit := fmt.Sprintf("--max-count=%d", c.maxCommits())
	// One call names the range's commits and, by their parents, which are merges.
	listed, err := c.Git.Git(ctx, c.Dir, "rev-list", "--first-parent", "--parents", limit, rng)
	if err != nil {
		if ctx.Err() != nil {
			return nil, "", ctx.Err()
		}
		return nil, "", errors.New("that range is not in this checkout")
	}
	var shas, nonMerge []string
	merges := map[string]bool{}
	for _, line := range strings.Split(listed, "\n") {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		shas = append(shas, f[0])
		if len(f) > 2 {
			merges[f[0]] = true
		} else {
			nonMerge = append(nonMerge, f[0])
		}
	}
	if err := c.fillRangeOf(ctx, rng, nonMerge, "--first-parent"); err != nil {
		return nil, "", err
	}
	if err := c.fillMerges(ctx, merges, rng, "--first-parent", limit); err != nil {
		return nil, "", err
	}
	var ids []string
	for _, sha := range shas {
		if p := c.info(sha).patch; p != "" {
			ids = append(ids, p)
		}
	}
	var note string
	if len(shas) >= c.maxCommits() {
		note = fmt.Sprintf("verdict range %s has at least %d commits, and only the newest %d are covered", spec, len(shas),
			c.maxCommits())
	}
	return ids, note, nil
}

// memo is the last finished answer and what it was read against.
type memo struct {
	tip, inst string
	rep       Report
}

// remember keeps a finished answer for memoFor. `pinned` is false when a verdict named a branch or a tag, which can
// move without the tip moving, so that answer is read again.
func (c *Checker) remember(rep Report, pinned bool) Report {
	if !pinned {
		return rep
	}
	rep.Notes = append([]string(nil), rep.Notes...)
	c.mu.Lock()
	c.last = &memo{tip: rep.Tip, inst: rep.Installed, rep: rep}
	c.mu.Unlock()
	return rep
}

// memoFor is the kept answer when the tip and the installed commit are the ones it was read against. Everything the
// answer rests on is reachable from the tip, so it cannot have changed under the same tip.
func (c *Checker) memoFor(tip, inst string) (Report, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.last == nil || c.last.tip != tip || c.last.inst != inst {
		return Report{}, false
	}
	rep := c.last.rep
	rep.Notes = append([]string(nil), rep.Notes...)
	return rep, true
}

// errNotAncestor is the installed build not being part of the branch.
var errNotAncestor = errors.New("not an ancestor")

// revs names the branch tip and the installed build in one git. When that fails it asks one at a time, which costs
// more and says which of the two it was.
func (c *Checker) revs(ctx context.Context, installed string) (tip, inst string, err error) {
	out, gerr := c.Git.Git(ctx, c.Dir, "rev-parse", "--quiet", "refs/heads/"+c.Branch+"^{commit}", installed+"^{commit}")
	if f := strings.Fields(out); gerr == nil && len(f) == 2 {
		return f[0], f[1], nil
	}
	if tip, err = c.rev(ctx, "refs/heads/"+c.Branch); err != nil {
		return "", "", fmt.Errorf("could not read %s in %s: %v", c.Branch, c.Dir, err)
	}
	if inst, err = c.rev(ctx, installed); err != nil {
		return "", "", fmt.Errorf("the installed build %s is not in this checkout", short(installed))
	}
	return tip, inst, nil
}
