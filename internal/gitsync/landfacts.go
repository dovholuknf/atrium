package gitsync

import (
	"context"
	"strings"
	"sync"
	"time"
)

// WHAT THE HUB'S MIRROR SAYS OF AN ITEM'S LANDING AND PUSHING, for the item lifecycle's `landed` and `pushed` steps and
// for the scope of `deployed`. See docs/rnd/item-lifecycle-design.md, stage L3.
//
//   - A COMMIT COUNTS AS THERE WHEN IT IS AN ANCESTOR OR HAS ITS PATCH THERE. Nearly everything lands on claude/main by
//     cherry-pick and is re-signed onto origin's main, and both give the same change a new sha. So every commit of the
//     branch must be an ancestor of the ref, or have a commit in the range the ref has beyond the branch's fork point
//     with the same patch id (`git patch-id --stable`, as flowfacts.go's `git cherry` does). The sha recorded is the
//     commit on the ref that carries the branch's tip: the tip itself for an ancestor.
//   - PUSHED ASKS THE FORGE, ONCE FOR ALL ITEMS. One `ls-remote` for its main, shared by a whole refresh pass and
//     remembered for forgeMainTTL unless a mirror pass or a collect says to look again. A forge that cannot be asked, or a
//     tip the mirror has not fetched yet, falls back to the mirror's own main and says so.

// LandFacts is what the mirror says of one item's branch.
type LandFacts struct {
	// SHA is the branch tip the facts are about, empty when the hub holds neither the branch nor the sha.
	SHA string `json:"sha"`
	// Landed is that claude/main has all of SHA, by ancestry or by patch, and LandedSHA is the commit on claude/main that
	// carries it: SHA itself for a fast-forward, the merge commit for a merge, the cherry-picked commit for a pick.
	Landed    bool   `json:"landed"`
	LandedSHA string `json:"landed_sha,omitempty"`
	// Pushed is that origin's main has all of SHA the same way, and PushedSHA is the commit on it that carries it (a
	// re-signed copy has its own). PushedVia is `origin` when the forge's answer was used and `mirror` when the mirror's
	// own main had to answer. PushedNote says why it had to.
	Pushed     bool   `json:"pushed"`
	PushedSHA  string `json:"pushed_sha,omitempty"`
	PushedVia  string `json:"pushed_via,omitempty"`
	PushedNote string `json:"pushed_note,omitempty"`
	// Paths are the files the item's own commits changed, which is what decides where it deploys.
	Paths []string `json:"paths,omitempty"`
	// Why is set when no repository could be read at all, and every pending step then says it.
	Why string `json:"why,omitempty"`
}

// LandFacts reads the mirror of the repository `name` for the item's branch, or for sha when the branch is empty. workID is the item's backlog id, which finds its commits once the branch is on main. A
// repository the hub does not hold is no facts and no error.
//
// forge is how the pass asks the forge for its main. It is called only when an item has landed and so has something to be
// pushed, and a pass hands every item the same one. Nil asks nothing, and the mirror answers.
func (s *Store) LandFacts(ctx context.Context, name, branch, sha, workID string, forge func() ForgeMain) (LandFacts, error) {
	var f LandFacts
	dir, err := s.bareFor(name)
	if err != nil || dir == "" {
		return f, err
	}
	ctx, cancel := context.WithTimeout(ctx, s.forgeBound()+readBound)
	defer cancel()
	if branch != "" {
		if why := CheckPushBranch(branch); why != "" {
			return f, refuse("%s", why)
		}
		tip, err := s.resolveTip(ctx, dir, headsPrefix+branch)
		if err != nil {
			return f, err
		}
		if tip != "" {
			sha = tip
		}
	}
	known := false
	if ValidSHA(sha) {
		if known, err = s.knownCommit(ctx, dir, sha); err != nil {
			return f, err
		}
	}
	if !known {
		// The hub never fetched the builder's commit, which is the usual case for a cherry-picked item. Its changelog entry
		// still names it on main.
		return s.landedByItem(ctx, dir, f, workID, forge)
	}
	f.SHA = sha
	f.Paths, _ = s.itemPaths(ctx, dir, sha, workID)
	var carrier string
	if f.Landed, carrier, err = s.carriedBy(ctx, dir, sha, ClaudeMainRef); err != nil {
		return f, err
	}
	if !f.Landed {
		// Picked with a conflict resolved, so no patch id matches: the item's own changelog entry says it landed.
		return s.landedByItem(ctx, dir, f, workID, forge)
	}
	if f.LandedSHA = carrier; carrier == sha {
		if f.LandedSHA, err = s.firstContaining(ctx, dir, sha, ClaudeMainRef); err != nil {
			return f, err
		}
	}
	var fm ForgeMain
	if forge != nil {
		fm = forge()
	}
	f.Pushed, f.PushedSHA, f.PushedVia, f.PushedNote = s.pushedToOrigin(ctx, dir, sha, fm)
	return f, nil
}

// landedByItem reads landing off the item's changelog entry, `changelog/<dept>/<yyyy-mm-dd>-<workID>.md`, which every change
// ships: the commit on claude/main that added it is the landing, whatever sha the builder had. f keeps what it already knew.
// The same entry on origin's main (the forge's tip when it answered) says pushed.
func (s *Store) landedByItem(ctx context.Context, dir string, f LandFacts, workID string, forge func() ForgeMain) (LandFacts, error) {
	if workID == "" || strings.ContainsAny(workID, "*?[]\\ \n") || strings.HasPrefix(workID, "adhoc-") {
		return f, nil
	}
	at, err := s.addedChangelog(ctx, dir, ClaudeMainRef, workID)
	if err != nil || at == "" {
		return f, err
	}
	f.Landed, f.LandedSHA = true, at
	if len(f.Paths) == 0 {
		f.Paths, _ = s.itemPaths(ctx, dir, "", workID)
	}
	ref, via, note := MainRef, "mirror", "the forge was not asked"
	if forge != nil {
		fm := forge()
		known := false
		if fm.Tip != "" {
			known, _ = s.knownCommit(ctx, dir, fm.Tip)
		}
		switch {
		case known:
			ref, via, note = fm.Tip, "origin", ""
		case fm.Tip != "":
			note = "the mirror has not fetched the forge's main yet"
		case fm.Why != "":
			note = fm.Why
		}
	}
	f.PushedVia, f.PushedNote = via, note
	if p, err := s.addedChangelog(ctx, dir, ref, workID); err == nil && p != "" {
		f.Pushed, f.PushedSHA = true, p
	}
	return f, nil
}

// addedChangelog is the first commit on ref's history that added the item's changelog entry, empty when there is none.
func (s *Store) addedChangelog(ctx context.Context, dir, ref, workID string) (string, error) {
	x, err := s.landIndexAt(ctx, dir, ref)
	if err != nil || x == nil {
		return "", err
	}
	return x.added[workID], nil
}

// ancestorOfRef is isAncestor against a ref the repository may not have, which holds nothing.
func (s *Store) ancestorOfRef(ctx context.Context, dir, sha, ref string) (bool, error) {
	if _, err := s.git(ctx, dir, "rev-parse", "--verify", "-q", ref+"^{commit}"); err != nil {
		if exitCode(err) == 1 {
			return false, nil
		}
		return false, err
	}
	return s.isAncestor(ctx, dir, sha, ref)
}

// firstContaining is the first commit on ref's first-parent line that holds sha. When sha is on that line it is sha
// (a fast-forward), and otherwise the merge that brought it in.
func (s *Store) firstContaining(ctx context.Context, dir, sha, ref string) (string, error) {
	tip, err := s.resolveTip(ctx, dir, ref)
	if err != nil {
		return "", err
	}
	v, err := s.memo(ctx, "first\x00"+dir+"\x00"+sha+"\x00"+tip, tip != "", func() (any, error) {
		return s.firstContainingAt(ctx, dir, sha, tip)
	})
	if err != nil {
		return "", err
	}
	return v.(string), nil
}

func (s *Store) firstContainingAt(ctx context.Context, dir, sha, tip string) (string, error) {
	out, err := s.git(ctx, dir, "rev-list", "--reverse", "--first-parent", "--ancestry-path", sha+".."+tip)
	if err != nil {
		return "", err
	}
	first := strings.Fields(out)
	if len(first) == 0 {
		return sha, nil
	}
	parents, err := s.git(ctx, dir, "rev-list", "--parents", "-n", "1", first[0])
	if err != nil {
		return "", err
	}
	if f := strings.Fields(parents); len(f) > 1 && f[1] == sha {
		return sha, nil
	}
	return first[0], nil
}

// itemPaths are the files the item's own commits changed: what the branch holds that its fork point from origin's main
// does not. Once the branch is on main that range is empty, so the commits that carry the item's changelog entry stand
// in, which is how every change here names its item.
func (s *Store) itemPaths(ctx context.Context, dir, sha, workID string) ([]string, error) {
	if main, err := s.resolveTip(ctx, dir, MainRef); err == nil && main != "" && ValidSHA(sha) {
		v, err := s.memo(ctx, "paths\x00"+dir+"\x00"+sha+"\x00"+main, true, func() (any, error) {
			if base, err := s.git(ctx, dir, "merge-base", sha, main); err == nil {
				if b := strings.TrimSpace(base); ValidSHA(b) && b != sha {
					return s.pathsOf(ctx, dir, "--no-merges", sha, "^"+b)
				}
			}
			return []string(nil), nil
		})
		if err != nil {
			return nil, err
		}
		if p := v.([]string); p != nil {
			return p, nil
		}
	}
	if workID == "" {
		return nil, nil
	}
	x, err := s.landIndexAt(ctx, dir, ClaudeMainRef)
	if err != nil || x == nil {
		return nil, err
	}
	return x.paths[workID], nil
}

// pathsOf lists the files changed by the commits `git log <args>` selects, each once.
func (s *Store) pathsOf(ctx context.Context, dir string, args ...string) ([]string, error) {
	out, err := s.git(ctx, dir, append([]string{"log", "--no-renames", "--name-only", "--format=", "--max-count=500"}, args...)...)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var paths []string
	for _, l := range strings.Split(out, "\n") {
		if l = strings.TrimSpace(l); l != "" && !seen[l] {
			seen[l] = true
			paths = append(paths, l)
		}
	}
	return paths, nil
}

// ForgeMain is what the forge said its main is.
type ForgeMain struct {
	// Tip is the sha of main on the forge, empty when it was not got. Why says why not.
	Tip string
	Why string
}

type forgeMainCache struct {
	mu sync.Mutex
	at map[string]time.Time
	v  map[string]ForgeMain
}

// forgeMainTTL is how long the forge's answer is kept when nobody says to look again. It is asked of a forge over the
// network, and every landed item that is not on main yet wants the same one answer.
const forgeMainTTL = 5 * time.Minute

// ForgeMainTip asks the forge for its main with one `ls-remote`, or answers the last answer when it is less than
// forgeMainTTL old and force is false. A mirror pass and a collect force it. A failure is kept for the same time, so a
// forge that is down is not asked again every pass.
func (s *Store) ForgeMainTip(ctx context.Context, name string, force bool) ForgeMain {
	c := &s.forgeMain
	c.mu.Lock()
	defer c.mu.Unlock()
	if !force {
		if at, ok := c.at[name]; ok && time.Since(at) < forgeMainTTL {
			return c.v[name]
		}
	}
	got := func() ForgeMain {
		ref, err := ParseName(name)
		if err != nil {
			return ForgeMain{Why: "the repository name does not name a forge"}
		}
		ctx, cancel := context.WithTimeout(ctx, s.forgeBound())
		defer cancel()
		heads, err := s.forgeHeads(ctx, ref, s.helper(ref), []string{"main"})
		switch tip, ok := heads["main"]; {
		case err != nil:
			return ForgeMain{Why: "the forge did not answer"}
		case !ok:
			return ForgeMain{Why: "the forge has no main"}
		default:
			return ForgeMain{Tip: tip}
		}
	}()
	if c.at == nil {
		c.at, c.v = map[string]time.Time{}, map[string]ForgeMain{}
	}
	c.at[name], c.v[name] = time.Now(), got
	return got
}

// pushedToOrigin says whether the forge's main, as fm told it, has all of sha, and the commit on it that carries it. A
// forge that did not answer, or a tip the mirror has not got, leaves the mirror's own main to answer, and the last result
// says why.
func (s *Store) pushedToOrigin(ctx context.Context, dir, sha string, fm ForgeMain) (ok bool, carrier, via, note string) {
	viaMirror := func(why string) (bool, string, string, string) {
		ok, c, err := s.carriedBy(ctx, dir, sha, MainRef)
		if err != nil {
			return false, "", "mirror", why + ", and the mirror could not be read"
		}
		return ok, c, "mirror", why
	}
	if fm.Tip == "" {
		why := fm.Why
		if why == "" {
			why = "the forge was not asked"
		}
		return viaMirror(why)
	}
	if known, err := s.knownCommit(ctx, dir, fm.Tip); err != nil || !known {
		return viaMirror("the mirror has not fetched the forge's main yet")
	}
	ok, c, err := s.carriedBy(ctx, dir, sha, fm.Tip)
	if err != nil {
		return viaMirror("the mirror could not compare against the forge's main")
	}
	return ok, c, "origin", ""
}

// carriedBy says whether `ref` (a ref or a commit) has all of the commits of sha, each as itself or by patch id, and the
// commit of ref that carries sha's own change. A sha that is an ancestor of ref is carried by itself. Otherwise the
// commits sha has beyond its fork point with ref are matched, by `git patch-id --stable`, to the commits ref has beyond
// it. Commits that change nothing have no patch id and are not asked for, but a branch of nothing else is not carried.
func (s *Store) carriedBy(ctx context.Context, dir, sha, ref string) (bool, string, error) {
	tip, err := s.resolveTip(ctx, dir, ref)
	if err != nil || tip == "" {
		return false, "", err
	}
	v, err := s.memo(ctx, "carried\x00"+dir+"\x00"+sha+"\x00"+tip, true, func() (any, error) {
		ok, c, err := s.carriedAt(ctx, dir, sha, tip)
		return carried{ok, c}, err
	})
	if err != nil {
		return false, "", err
	}
	c := v.(carried)
	return c.ok, c.by, nil
}

type carried struct {
	ok bool
	by string
}

func (s *Store) carriedAt(ctx context.Context, dir, sha, ref string) (bool, string, error) {
	if up, err := s.isAncestor(ctx, dir, sha, ref); err != nil || up {
		return up, sha, err
	}
	base, err := s.git(ctx, dir, "merge-base", sha, ref)
	if err != nil {
		if exitCode(err) == 1 {
			return false, "", nil
		}
		return false, "", err
	}
	base = strings.TrimSpace(base)
	mine, err := s.patchIDs(ctx, dir, base+".."+sha)
	if err != nil || len(mine) == 0 {
		return false, "", err
	}
	theirs, err := s.patchIDs(ctx, dir, base+".."+ref)
	if err != nil {
		return false, "", err
	}
	byID := map[string]string{}
	for _, t := range theirs {
		byID[t.id] = t.commit
	}
	carrier := ""
	for i, m := range mine {
		c, ok := byID[m.id]
		if !ok {
			return false, "", nil
		}
		if i == 0 {
			carrier = c
		}
	}
	return true, carrier, nil
}

type idCommit struct{ id, commit string }

// patchIDs are the patch ids of the non-merge commits of a range, newest first, commits with no change left out.
func (s *Store) patchIDs(ctx context.Context, dir, spec string) ([]idCommit, error) {
	v, err := s.memo(ctx, "pid\x00"+dir+"\x00"+spec, true, func() (any, error) { return s.patchIDsOf(ctx, dir, spec) })
	if err != nil {
		return nil, err
	}
	return v.([]idCommit), nil
}

func (s *Store) patchIDsOf(ctx context.Context, dir, spec string) ([]idCommit, error) {
	out, err := s.h.runner().GitCapped(ctx, dir, s.env(), 16<<20, "log", "-p", "--no-merges", "--no-renames",
		"--max-count=500", "--format=commit %H", spec)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(out) == "" {
		return nil, nil
	}
	ids, err := s.h.runner().GitInput(ctx, dir, []byte(out), "patch-id", "--stable")
	if err != nil {
		return nil, err
	}
	var res []idCommit
	for _, l := range strings.Split(ids, "\n") {
		if f := strings.Fields(l); len(f) == 2 {
			res = append(res, idCommit{id: f[0], commit: f[1]})
		}
	}
	return res, nil
}

// Contains says whether the commit `build` holds sha, and known false when the mirror does not have `build`, which is a
// room on a build the hub has never seen: older than anything it holds, as far as anyone can tell.
func (s *Store) Contains(ctx context.Context, name, build, sha string) (contains, known bool, err error) {
	dir, err := s.bareFor(name)
	if err != nil || dir == "" || !ValidSHA(build) || !ValidSHA(sha) {
		return false, false, err
	}
	ctx, cancel := context.WithTimeout(ctx, readBound)
	defer cancel()
	if known, err = s.knownCommit(ctx, dir, build); err != nil || !known {
		return false, false, err
	}
	v, err := s.memo(ctx, "anc\x00"+dir+"\x00"+sha+"\x00"+build, true, func() (any, error) {
		return s.isAncestor(ctx, dir, sha, build)
	})
	if err != nil {
		return false, true, err
	}
	return v.(bool), true, nil
}
