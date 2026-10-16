package gitsync

import (
	"context"
	"errors"
	"log"
	"path"
	"sort"
	"strings"
	"sync"
	"time"
)

// WHICH BRANCHES ARE LANDED, BY WHAT THEY CHANGE, NOT BY WHERE THEY SIT. See docs/fabric/branch-status-design.md, phase 1.
//
// Landing rewrites history (a cherry-pick onto claude/main, then a re-signed copy onto origin's main), so every sha
// changes and ancestry calls nearly every branch unmerged. A commit of a branch is CARRIED by claude/main when:
//
//  1. its patch id (`git patch-id --stable`) is one of main's, or
//  2. its subject is one of main's AND the files it touches are the files a main commit of that subject touches. The
//     subject alone is not enough: "Fix tests" is on main a hundred times. A false landed loses work and a false orphan
//     only nags, so this errs toward orphaned (decision 4). An empty commit has no patch id and an empty file set.
//  3. the branch added a changelog entry that is at the same path on main. That is per BRANCH, not per commit: it is
//     what is left after a squash.
//
// A branch with nothing beyond main is landed by ancestry. A branch with an uncarried commit is orphaned in this phase:
// there is no live-card set (phase 3) and no room refs (phase 4) yet, so open and room-only are not computed. It carries
// a kind, `code` when an uncarried commit touches a path outside the docs list, `docs` otherwise (decision 5).
//
// ONE PASS PER REPOSITORY, NEVER ONE CALL PER BRANCH. Main's patch ids are hashed once and extended by the commits main
// gained, each branch commit is hashed once however many branches hold it, and the commit to facts map is kept in
// memory between passes (decision 1), so a pass after one push hashes a handful of commits. The answer is a snapshot
// as the repos list is (viewcache.go): a GET never runs git, and a pass that fails leaves the last answer.

// Branch statuses, as the API spells them. Open and room-only are not computed yet.
const (
	BranchLanded   = "landed"
	BranchOpen     = "open"
	BranchOrphaned = "orphaned"
	BranchRoomOnly = "room-only"
)

// How a branch was found to be landed, weakest evidence last.
const (
	HowAncestry  = "ancestry"
	HowPatchID   = "patch-id"
	HowSubject   = "subject"
	HowChangelog = "changelog"
)

// The kind of an orphan.
const (
	KindCode = "code"
	KindDocs = "docs"
)

const (
	// hashBatch is how many commits one `git log -p` is asked for, so the diff text held at once stays bounded.
	hashBatch = 200
	// hashCap is the most diff text one batch may produce. A batch past it is split, and a single commit past it has no
	// patch id, so only its subject and files can carry it.
	hashCap = 32 << 20
	// branchPassBound is how long one whole pass may take, over and above the bound on each command.
	branchPassBound = 10 * time.Minute
	// branchFilesShown is how many files a branch row names.
	branchFilesShown = 20
)

// BranchOwner is who pushed a branch, from the push log.
type BranchOwner struct {
	Room   string `json:"room"`
	Card   string `json:"card"`
	Pushed string `json:"pushed,omitempty"`
}

// BranchStatus is one branch of the hub's store.
type BranchStatus struct {
	Repo   string `json:"repo"`
	Name   string `json:"name"`
	Status string `json:"status"`
	// Kind is `code` or `docs` for an orphan, and empty otherwise.
	Kind    string `json:"kind,omitempty"`
	HubSHA  string `json:"hub_sha"`
	Commits int    `json:"commits"`
	// Uncarried is how many of Commits main does not carry.
	Uncarried int `json:"uncarried"`
	// How is the weakest evidence a landed branch needed.
	How        string       `json:"how,omitempty"`
	LastCommit string       `json:"last_commit"`
	Files      []string     `json:"files"`
	Owner      *BranchOwner `json:"owner"`
}

// BranchMain is the main the pass measured against.
type BranchMain struct {
	SHA string `json:"sha"`
	Ref string `json:"ref"`
}

// RepoBranches is one repository's statuses. Why is set when the repository could not be measured at all.
type RepoBranches struct {
	Repo     string         `json:"repo"`
	Main     BranchMain     `json:"main"`
	Counts   map[string]int `json:"counts"`
	Branches []BranchStatus `json:"branches"`
	Why      string         `json:"why,omitempty"`
}

// BranchesView is what GET /_hub/git/branches answers: the snapshot and how old it is. Computed is empty until a first
// pass has finished, and Branches is then empty.
type BranchesView struct {
	Computed string         `json:"computed"`
	Building bool           `json:"building"`
	Main     *BranchMain    `json:"main,omitempty"`
	Counts   map[string]int `json:"counts"`
	Branches []BranchStatus `json:"branches"`
	Why      []string       `json:"why,omitempty"`
}

// BranchFilter narrows the rows and not the counts, which are always over every branch.
type BranchFilter struct {
	Statuses []string
	Kind     string
	Limit    int
}

// DocPath says a path is documentation: under docs/ or changelog/, or a markdown file. One list, kept here.
func DocPath(p string) bool {
	return strings.HasPrefix(p, "docs/") || strings.HasPrefix(p, "changelog/") || strings.EqualFold(path.Ext(p), ".md")
}

// commitFacts is what a pass learns of one commit once, and a commit never changes.
type commitFacts struct {
	// pid is empty for a commit that changes nothing, or whose diff was past hashCap.
	pid     string
	subject string
	files   []string
	// key is the file set as one string, for comparing.
	key string
}

// branchState is what a pass keeps for the next. Losing it costs one slow pass.
type branchState struct {
	mainTip  string
	mainPIDs map[string]bool
	// mainSubj is each subject on main and the file sets of the commits that carry it.
	mainSubj map[string]map[string]bool
	facts    map[string]commitFacts
	sig      string
	last     RepoBranches
}

// branchPass measures every claude/* branch of the repository against claude/main.
func (s *Store) branchPass(ctx context.Context, name string, st *branchState) (RepoBranches, error) {
	res := RepoBranches{Repo: canonical(name), Counts: zeroCounts(), Branches: []BranchStatus{}}
	dir, err := s.bareFor(name)
	if err != nil {
		return res, err
	}
	if dir == "" {
		res.Why = "the hub does not hold this repository"
		return res, nil
	}
	ctx, cancel := context.WithTimeout(ctx, branchPassBound)
	defer cancel()
	out, err := s.git(ctx, dir, "rev-parse", "--verify", "-q", ClaudeMainRef+"^{commit}")
	if err != nil {
		if exitCode(err) == 1 {
			res.Why = "the repository has no claude/main to measure against"
			return res, nil
		}
		return res, err
	}
	m := strings.TrimSpace(out)
	res.Main = BranchMain{SHA: m, Ref: strings.TrimPrefix(ClaudeMainRef, headsPrefix)}
	all, err := s.heads(ctx, dir)
	if err != nil {
		return res, err
	}
	var heads []head
	var sig strings.Builder
	sig.WriteString(m)
	for _, h := range all {
		if strings.HasPrefix(h.name, "claude/") && h.name != "claude/main" && CheckPushBranch(h.name) == "" {
			heads = append(heads, h)
		}
	}
	sort.Slice(heads, func(i, j int) bool { return heads[i].name < heads[j].name })
	for _, h := range heads {
		sig.WriteString("\n" + h.name + " " + h.SHA)
	}
	if st.sig == sig.String() && st.last.Repo != "" {
		return st.last, nil
	}
	started := time.Now()
	if err := s.extendMain(ctx, dir, m, st); err != nil {
		return res, err
	}

	// What each branch has that main does not, and the facts of those commits, each read once.
	own := make(map[string][]string, len(heads))
	seen := map[string]bool{}
	var need []string
	for _, h := range heads {
		out, err := s.git(ctx, dir, "rev-list", "--no-merges", h.SHA, "^"+m)
		if err != nil {
			return res, err
		}
		shas := strings.Fields(out)
		own[h.name] = shas
		for _, c := range shas {
			if _, ok := st.facts[c]; !ok && !seen[c] {
				need = append(need, c)
			}
			seen[c] = true
		}
	}
	got, err := s.readFacts(ctx, dir, need)
	if err != nil {
		return res, err
	}
	facts := make(map[string]commitFacts, len(seen))
	for c := range seen {
		if f, ok := st.facts[c]; ok {
			facts[c] = f
		} else {
			facts[c] = got[c]
		}
	}

	owners := s.branchOwners(ctx, res.Repo)
	var changelog map[string]bool
	for _, h := range heads {
		b := BranchStatus{Repo: res.Repo, Name: h.name, HubSHA: h.SHA, Commits: len(own[h.name]), Files: []string{},
			LastCommit: h.at.UTC().Format(time.RFC3339), Owner: owners[h.name]}
		var left []string
		how := ""
		if len(own[h.name]) == 0 {
			how = HowAncestry
		}
		for _, c := range own[h.name] {
			switch carriedBy(facts[c], st) {
			case "":
				left = append(left, c)
			default:
				how = weaker(how, carriedBy(facts[c], st))
			}
		}
		if len(left) > 0 {
			if changelog == nil {
				if changelog, err = s.changelogOnMain(ctx, dir, m); err != nil {
					return res, err
				}
			}
			if ok, err := s.addedOnMain(ctx, dir, h.SHA, m, changelog); err != nil {
				return res, err
			} else if ok {
				how, left = weaker(how, HowChangelog), nil
			}
		}
		b.Uncarried = len(left)
		if len(left) == 0 {
			b.Status, b.How = BranchLanded, how
		} else {
			b.Status, b.Kind, b.Files = BranchOrphaned, KindDocs, uncarriedFiles(left, facts)
			for _, c := range left {
				for _, f := range facts[c].files {
					if !DocPath(f) {
						b.Kind = KindCode
					}
				}
			}
			if len(b.Files) > branchFilesShown {
				b.Files = b.Files[:branchFilesShown]
			}
		}
		res.Counts[b.Status]++
		res.Branches = append(res.Branches, b)
	}
	st.facts, st.sig, st.last = facts, sig.String(), res
	log.Printf("[hub] branches %s: landed %d, open %d, orphaned %d, room-only %d, %d commits read, %s", res.Repo,
		res.Counts[BranchLanded], res.Counts[BranchOpen], res.Counts[BranchOrphaned], res.Counts[BranchRoomOnly],
		len(need), time.Since(started).Round(time.Millisecond))
	return res, nil
}

func zeroCounts() map[string]int {
	return map[string]int{BranchLanded: 0, BranchOpen: 0, BranchOrphaned: 0, BranchRoomOnly: 0}
}

// carriedBy is the rule that carries a commit on main, or empty when none does. Rule 3 is per branch and not here.
func carriedBy(f commitFacts, st *branchState) string {
	if f.pid != "" && st.mainPIDs[f.pid] {
		return HowPatchID
	}
	if keys, ok := st.mainSubj[f.subject]; ok && keys[f.key] {
		return HowSubject
	}
	return ""
}

// weaker is the weaker of two pieces of evidence, the empty one being no evidence yet.
func weaker(a, b string) string {
	rank := map[string]int{"": 0, HowAncestry: 1, HowPatchID: 2, HowSubject: 3, HowChangelog: 4}
	if rank[b] > rank[a] {
		return b
	}
	return a
}

func uncarriedFiles(shas []string, facts map[string]commitFacts) []string {
	seen := map[string]bool{}
	var files []string
	for _, c := range shas {
		for _, f := range facts[c].files {
			if !seen[f] {
				seen[f] = true
				files = append(files, f)
			}
		}
	}
	sort.Strings(files)
	return files
}

// extendMain brings main's patch ids and subjects up to m. When main is only ahead of what was read the new commits
// alone are hashed, and when main was rewritten the whole of it is read again.
func (s *Store) extendMain(ctx context.Context, dir, m string, st *branchState) error {
	if st.mainTip == m {
		return nil
	}
	spec := []string{m}
	if st.mainTip != "" {
		if up, err := s.isAncestor(ctx, dir, st.mainTip, m); err != nil {
			return err
		} else if up {
			spec = []string{m, "^" + st.mainTip}
		} else {
			st.mainPIDs, st.mainSubj = nil, nil
		}
	}
	if st.mainPIDs == nil {
		st.mainPIDs, st.mainSubj = map[string]bool{}, map[string]map[string]bool{}
	}
	out, err := s.git(ctx, dir, append([]string{"rev-list", "--no-merges"}, spec...)...)
	if err != nil {
		return err
	}
	got, err := s.readFacts(ctx, dir, strings.Fields(out))
	if err != nil {
		return err
	}
	for _, f := range got {
		if f.pid != "" {
			st.mainPIDs[f.pid] = true
		}
		if st.mainSubj[f.subject] == nil {
			st.mainSubj[f.subject] = map[string]bool{}
		}
		st.mainSubj[f.subject][f.key] = true
	}
	st.mainTip = m
	return nil
}

// readFacts is the patch id, subject and files of each commit, in batches.
func (s *Store) readFacts(ctx context.Context, dir string, shas []string) (map[string]commitFacts, error) {
	out := make(map[string]commitFacts, len(shas))
	for i := 0; i < len(shas); i += hashBatch {
		batch := shas[i:min(i+hashBatch, len(shas))]
		if err := s.readMeta(ctx, dir, batch, out); err != nil {
			return nil, err
		}
		pids, err := s.hashBatch(ctx, dir, batch)
		if err != nil {
			return nil, err
		}
		for c, p := range pids {
			f := out[c]
			f.pid = p
			out[c] = f
		}
	}
	return out, nil
}

// readMeta fills in subject and files, which cost no diff.
func (s *Store) readMeta(ctx context.Context, dir string, shas []string, into map[string]commitFacts) error {
	out, err := s.h.runner().gitWith(ctx, dir, s.env(), []byte(strings.Join(shas, "\n")+"\n"), 0,
		"-c", "core.quotepath=off", "log", "--no-walk=unsorted", "--stdin", "--no-merges", "--no-renames", "--name-only",
		"--format=%x00%H%x00%s")
	if err != nil {
		return err
	}
	parts := strings.Split(out, "\x00")
	for i := 1; i+1 < len(parts); i += 2 {
		sha, rest := parts[i], parts[i+1]
		subject, list, _ := strings.Cut(rest, "\n")
		var files []string
		for _, l := range strings.Split(list, "\n") {
			if l = strings.TrimSpace(l); l != "" {
				files = append(files, l)
			}
		}
		sort.Strings(files)
		into[sha] = commitFacts{subject: strings.TrimSpace(subject), files: files, key: strings.Join(files, "\x00")}
	}
	return nil
}

// hashBatch is the patch id of each commit that has one. A batch whose diff is past hashCap is halved, and a single
// commit past it has none.
func (s *Store) hashBatch(ctx context.Context, dir string, shas []string) (map[string]string, error) {
	if len(shas) == 0 {
		return nil, nil
	}
	diff, err := s.h.runner().gitWith(ctx, dir, s.env(), []byte(strings.Join(shas, "\n")+"\n"), hashCap,
		"log", "--no-walk=unsorted", "--stdin", "-p", "--no-merges", "--no-renames", "--format=commit %H")
	if err != nil {
		if !errors.Is(err, ErrOutputCap) {
			return nil, err
		}
		if len(shas) == 1 {
			return nil, nil
		}
		a, err := s.hashBatch(ctx, dir, shas[:len(shas)/2])
		if err != nil {
			return nil, err
		}
		b, err := s.hashBatch(ctx, dir, shas[len(shas)/2:])
		if err != nil {
			return nil, err
		}
		for c, p := range b {
			a[c] = p
		}
		return a, nil
	}
	if strings.TrimSpace(diff) == "" {
		return nil, nil
	}
	ids, err := s.h.runner().GitInput(ctx, dir, []byte(diff), "patch-id", "--stable")
	if err != nil {
		return nil, err
	}
	res := map[string]string{}
	for _, l := range strings.Split(ids, "\n") {
		if f := strings.Fields(l); len(f) == 2 {
			res[f[1]] = f[0]
		}
	}
	return res, nil
}

// changelogOnMain is every changelog entry path main holds.
func (s *Store) changelogOnMain(ctx context.Context, dir, m string) (map[string]bool, error) {
	out, err := s.git(ctx, dir, "ls-tree", "-r", "--name-only", m, "--", "changelog")
	if err != nil {
		return nil, err
	}
	set := map[string]bool{}
	for _, l := range strings.Split(out, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			set[l] = true
		}
	}
	return set, nil
}

// addedOnMain says whether the branch added a changelog entry that main has at the same path (rule 3).
func (s *Store) addedOnMain(ctx context.Context, dir, tip, m string, onMain map[string]bool) (bool, error) {
	if len(onMain) == 0 {
		return false, nil
	}
	out, err := s.git(ctx, dir, "log", "--no-renames", "--diff-filter=A", "--name-only", "--format=", tip, "^"+m, "--",
		"changelog")
	if err != nil {
		return false, err
	}
	for _, l := range strings.Split(out, "\n") {
		l = strings.TrimSpace(l)
		if strings.Count(l, "/") == 2 && strings.HasSuffix(l, ".md") && onMain[l] {
			return true, nil
		}
	}
	return false, nil
}

// branchOwners is who pushed each branch, by branch name.
func (s *Store) branchOwners(ctx context.Context, repo string) map[string]*BranchOwner {
	out := map[string]*BranchOwner{}
	if s.h.PushLog == nil {
		return out
	}
	rs, err := s.h.PushLog.Branches(ctx, repo)
	if err != nil {
		return out
	}
	for _, r := range rs {
		if r.Room == "" && r.Card == "" {
			continue
		}
		o := &BranchOwner{Room: r.Room, Card: r.Card}
		if !r.At.IsZero() {
			o.Pushed = r.At.UTC().Format(time.RFC3339)
		}
		out[strings.TrimPrefix(r.Ref, headsPrefix)] = o
	}
	return out
}

// ── the snapshot ────────────────────────────────────────

type branchEntry struct {
	val      RepoBranches
	err      error
	at       time.Time
	asked    time.Time
	building bool
	again    bool
	st       branchState
}

type branchCache struct {
	mu sync.Mutex
	m  map[string]*branchEntry
}

// BranchesSnapshot answers the statuses of the repository `name`, or of every repository the hub holds when it is
// empty, from memory: it runs no git. The first ask for a repository starts its pass and answers `building` with no
// branches. The answers are rebuilt on a push, an init and the view timer for as long as somebody keeps asking, and a
// rebuild when nothing moved costs one `for-each-ref`.
func (s *Store) BranchesSnapshot(name string, f BranchFilter) (BranchesView, error) {
	var names []string
	if name != "" {
		if _, err := ParseName(name); err != nil {
			return BranchesView{}, refuse("repo is host/owner/repo, like github/o/r")
		}
		names = []string{canonical(name)}
	} else {
		ents, err := s.List()
		if err != nil {
			return BranchesView{}, err
		}
		for _, e := range ents {
			names = append(names, e.Ref.Name())
		}
	}
	s.ensureViewLoop()
	v := BranchesView{Counts: zeroCounts(), Branches: []BranchStatus{}}
	var oldest time.Time
	pending := false
	for _, n := range names {
		r, at, building := s.branchEntryFor(n)
		v.Building = v.Building || building
		if at.IsZero() {
			pending = true
			continue
		}
		if oldest.IsZero() || at.Before(oldest) {
			oldest = at
		}
		if r.Why != "" {
			v.Why = append(v.Why, r.Repo+": "+r.Why)
		}
		for k, c := range r.Counts {
			v.Counts[k] += c
		}
		if len(names) == 1 {
			m := r.Main
			v.Main = &m
		}
		v.Branches = append(v.Branches, r.Branches...)
	}
	if pending {
		v.Building = true
	}
	if !pending && !oldest.IsZero() {
		v.Computed = oldest.UTC().Format(time.RFC3339)
	}
	v.Branches = filterBranches(v.Branches, f)
	return v, nil
}

// branchEntryFor is the last answer for a repository, starting its pass when nobody has.
func (s *Store) branchEntryFor(name string) (RepoBranches, time.Time, bool) {
	c := &s.branches
	c.mu.Lock()
	if c.m == nil {
		c.m = map[string]*branchEntry{}
	}
	e := c.m[name]
	if e == nil {
		e = &branchEntry{}
		c.m[name] = e
	}
	e.asked = time.Now()
	start := e.at.IsZero() && !e.building
	if start {
		e.building = true
	}
	val, at, building := e.val, e.at, e.building
	c.mu.Unlock()
	if start {
		go s.buildBranches(name, e)
	}
	return val, at, building
}

// refreshBranches rebuilds every answer somebody asked about lately. It returns at once.
func (s *Store) refreshBranches() {
	c := &s.branches
	c.mu.Lock()
	var names []string
	var es []*branchEntry
	now := time.Now()
	for n, e := range c.m {
		if now.Sub(e.asked) > pushedKeep {
			if !e.building {
				delete(c.m, n)
			}
			continue
		}
		if e.building {
			e.again = true
			continue
		}
		e.building = true
		names, es = append(names, n), append(es, e)
	}
	c.mu.Unlock()
	for i := range names {
		go s.buildBranches(names[i], es[i])
	}
}

func (s *Store) buildBranches(name string, e *branchEntry) {
	c := &s.branches
	for {
		v, err := s.branchPass(context.Background(), name, &e.st)
		c.mu.Lock()
		if err == nil {
			e.val, e.at, e.err = v, time.Now(), nil
		} else {
			e.err = err
			log.Printf("[hub] branches %s: %v", name, err)
		}
		if e.again {
			e.again = false
			c.mu.Unlock()
			continue
		}
		e.building = false
		c.mu.Unlock()
		return
	}
}

// filterBranches keeps the rows the filter names, orphaned first and the oldest first within a status, then cuts to the
// limit. Counts are not touched: they are over every branch.
func filterBranches(in []BranchStatus, f BranchFilter) []BranchStatus {
	want := map[string]bool{}
	for _, s := range f.Statuses {
		want[s] = true
	}
	out := []BranchStatus{}
	for _, b := range in {
		if len(want) > 0 && !want[b.Status] || f.Kind != "" && b.Kind != f.Kind {
			continue
		}
		out = append(out, b)
	}
	rank := map[string]int{BranchRoomOnly: 0, BranchOrphaned: 1, BranchOpen: 2, BranchLanded: 3}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		switch {
		case rank[a.Status] != rank[b.Status]:
			return rank[a.Status] < rank[b.Status]
		case a.Kind != b.Kind:
			return a.Kind == KindCode
		case a.LastCommit != b.LastCommit:
			return a.LastCommit < b.LastCommit
		}
		return a.Name < b.Name
	})
	if f.Limit > 0 && len(out) > f.Limit {
		out = out[:f.Limit]
	}
	return out
}
