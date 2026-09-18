package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// The routes a provider answers on.
//
// Every refusal in here runs BEFORE any write, and a body carrying a blocked
// change is refused whole. That is the posture the settings boundary already
// takes: applying half a request and then reporting a failure is the worst of
// both, because the caller reads an error and the machine has changed anyway.
// So a request that turns the worktree toggle off AND renames the root leaves
// the root exactly where it was.

// makeWorktreeDeadline bounds `git worktree add`. Generous, because it can
// check out a large tree, and bounded, because a command waiting on a
// credential prompt it will never get would otherwise hold a request forever.
const makeWorktreeDeadline = 3 * time.Minute

func (s *Server) listProviders(w http.ResponseWriter, r *http.Request) {
	rows, err := s.st.Providers()
	if err != nil {
		s.fail(w, err)
		return
	}
	if rows == nil {
		rows = []*store.Provider{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"providers": rows})
}

// saveProvider creates or updates one.
//
// Three refusals, in this order, and none of them writes anything:
//
//  1. the toggle going OFF while the worktree directory holds something
//  2. a root that nests inside another provider's root
//  3. whatever the store itself refuses, which is what is true of the row alone
func (s *Server) saveProvider(w http.ResponseWriter, r *http.Request) {
	var p store.Provider
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&p); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	p.Name = r.PathValue("name")

	all, err := s.st.Providers()
	if err != nil {
		s.fail(w, err)
		return
	}
	var had *store.Provider
	for _, x := range all {
		if strings.EqualFold(x.Name, p.Name) {
			had = x
		}
	}

	// THE TOGGLE GOING OFF IS THE ONLY CASE THAT READS THE FILESYSTEM.
	// Turning it ON never does, because there is nothing to orphan.
	if had != nil && had.Worktrees && !p.Worktrees {
		blockers, err := worktreeRootBlockers(had.WorktreeRoot)
		if err != nil {
			writeErr(w, http.StatusConflict, err)
			return
		}
		if len(blockers) > 0 {
			writeJSON(w, http.StatusConflict, map[string]any{
				"error":    blockerMessage(had.Name, had.WorktreeRoot, blockers),
				"blockers": blockers,
			})
			return
		}
	}
	if err := providerOverlap(all, p); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}

	saved, err := s.st.SaveProvider(p)
	if err != nil {
		if halted, _ := s.st.Halted(); halted {
			s.fail(w, err)
			return
		}
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	s.Broadcast("providers", saved)
	writeJSON(w, http.StatusOK, saved)
}

// deleteProvider removes a provider, and refuses for the same reason the toggle
// does.
//
// Deleting one whose worktree support is on would leave those directories on
// disk with nothing describing them, which is the identical failure. To delete,
// turn the toggle off first, which forces the check.
func (s *Server) deleteProvider(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	p, err := s.st.Provider(name)
	if err != nil {
		writeErr(w, http.StatusNotFound, errNoProvider)
		return
	}
	if p.Worktrees {
		blockers, err := worktreeRootBlockers(p.WorktreeRoot)
		if err != nil {
			writeErr(w, http.StatusConflict, err)
			return
		}
		if len(blockers) > 0 {
			writeJSON(w, http.StatusConflict, map[string]any{
				"error":    blockerMessage(p.Name, p.WorktreeRoot, blockers),
				"blockers": blockers,
			})
			return
		}
	}
	if err := s.st.DeleteProvider(name); err != nil {
		s.fail(w, err)
		return
	}
	// NO CARD IS TOUCHED. A card's directory is a string a human typed and it
	// outlives every configuration row that describes it.
	s.Broadcast("providers", map[string]any{"deleted": p.Name})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// checkWorktrees asks the refusal's question without saving anything, so
// somebody clearing the directory can find out whether they are done yet.
func (s *Server) checkWorktrees(w http.ResponseWriter, r *http.Request) {
	p, err := s.st.Provider(r.PathValue("name"))
	if err != nil {
		writeErr(w, http.StatusNotFound, errNoProvider)
		return
	}
	blockers, err := worktreeRootBlockers(p.WorktreeRoot)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"clear": false, "error": err.Error(), "blockers": []blocker{},
		})
		return
	}
	if blockers == nil {
		blockers = []blocker{}
	}
	out := map[string]any{"clear": len(blockers) == 0, "blockers": blockers}
	if len(blockers) > 0 {
		out["error"] = blockerMessage(p.Name, p.WorktreeRoot, blockers)
	}
	writeJSON(w, http.StatusOK, out)
}

// discoverNow adopts what is under the root.
//
// A run that could not read the root answers 200 with its error in the body
// rather than a failure status, and that is deliberate: nothing went wrong with
// the REQUEST, and the rows are untouched. The same posture a source takes,
// where a failed run is reported on its own row and halts nothing.
func (s *Server) discoverNow(w http.ResponseWriter, r *http.Request) {
	p, err := s.st.Provider(r.PathValue("name"))
	if err != nil {
		writeErr(w, http.StatusNotFound, errNoProvider)
		return
	}
	d := discoverProvider(s.st, p)
	if err := s.st.RecordProviderScan(p.Name, d.Summary, d.Error); err != nil {
		s.fail(w, err)
		return
	}
	s.Broadcast("providers", map[string]any{"scanned": p.Name})
	writeJSON(w, http.StatusOK, d)
}

// listProviderRepos answers with the rows and whether each is on disk right
// now. Presence is computed here on every read and stored nowhere.
func (s *Server) listProviderRepos(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	repos, err := s.st.ProviderRepos(name)
	if err != nil {
		writeErr(w, http.StatusNotFound, errNoProvider)
		return
	}
	fillPresence(repos)
	orgs, _ := s.st.ProviderOrgs(name)
	if repos == nil {
		repos = []*store.ProviderRepo{}
	}
	if orgs == nil {
		orgs = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"repos": repos, "orgs": orgs})
}

// saveProviderRepo adds or updates one row.
//
// An org and a repo naming no directory are allowed. Adding a repository is
// typing two fields, and atrium does not clone, so a row for something not yet
// on disk is a legitimate thing to hold.
func (s *Server) saveProviderRepo(w http.ResponseWriter, r *http.Request) {
	var rec store.ProviderRepo
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&rec); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	rec.Provider = r.PathValue("name")
	saved, err := s.st.SaveProviderRepo(rec)
	if err != nil {
		if halted, _ := s.st.Halted(); halted {
			s.fail(w, err)
			return
		}
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	one := []*store.ProviderRepo{saved}
	fillPresence(one)
	s.Broadcast("providers", map[string]any{"repo": saved})
	writeJSON(w, http.StatusOK, one[0])
}

// forgetProviderRepo is the DELIBERATE removal, and the only thing that ever
// deletes a row. Discovery never does, which is what makes it safe to run
// against a root that is temporarily not there.
func (s *Server) forgetProviderRepo(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	org, repo := r.URL.Query().Get("org"), r.URL.Query().Get("repo")
	if repo == "" {
		writeErr(w, http.StatusBadRequest, errors.New("which repository"))
		return
	}
	if hide := r.URL.Query().Get("hide"); hide != "" {
		if err := s.st.HideProviderRepo(name, org, repo, hide == "1" || hide == "true"); err != nil {
			s.fail(w, err)
			return
		}
	} else if err := s.st.ForgetProviderRepo(name, org, repo); err != nil {
		s.fail(w, err)
		return
	}
	s.Broadcast("providers", map[string]any{"repos": name})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// listRepoWorktrees asks git what worktrees a repository already has.
//
// SEPARATE FROM THE REPOSITORY LIST, and asked only when one repository is
// being looked at. The old scan asked every repository at once, eight at a
// time, because it had to fill a list of all of them: that is a process spawn
// per repository, and on a root with two hundred it is the difference between a
// list that appears and a list that arrives. A provider knows the paths without
// asking git anything, so the only question left is per repository and is asked
// when somebody opens one.
func (s *Server) listRepoWorktrees(w http.ResponseWriter, r *http.Request) {
	p, err := s.st.Provider(r.PathValue("name"))
	if err != nil {
		writeErr(w, http.StatusNotFound, errNoProvider)
		return
	}
	repoPath := store.ProviderPath(p.Root, r.URL.Query().Get("org"), r.URL.Query().Get("repo"))
	out := worktreesOf(repoPath)
	if out == nil {
		// A repository git will not talk about, or one that is not there,
		// contributes no worktrees. Not an error: the checkout is still a
		// directory a card can start in, and the picker still offers it.
		out = []projectWorktree{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"worktrees": out})
}

// ── making a worktree ───────────────────────────────────

type worktreeRequest struct {
	Org    string `json:"org"`
	Repo   string `json:"repo"`
	Branch string `json:"branch"`
}

// makeProviderWorktree runs `git worktree add` AS AN ARGV, with no shell.
//
// The mechanism this replaces ran an operator-supplied command template through
// a profile-loading shell, and every one of its four defects is gone with the
// shell:
//
//   - the default template also STARTED A SESSION, so "make" then "start"
//     started two. `git worktree add` makes a directory and nothing else.
//   - a template had to be quoted correctly under three different shell
//     grammars, which is why a branch name had to pass a regular expression
//     before it was allowed anywhere near one. As argv, a path with a space or
//     an apostrophe is simply a string.
//   - the destination had to be read back out of the command's output, because
//     output is for a person. Atrium chooses the path, so it already knows.
//   - a repository name needed quoting in a custom template.
//
// What is lost: `gwt new` also fetches, writes a ledger entry and seeds a
// prompt. Somebody who wants those keeps using it in a terminal, and because
// the worktree lands under the declared root the next discovery adopts it.
func (s *Server) makeProviderWorktree(w http.ResponseWriter, r *http.Request) {
	p, err := s.st.Provider(r.PathValue("name"))
	if err != nil {
		writeErr(w, http.StatusNotFound, errNoProvider)
		return
	}
	var req worktreeRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if !p.Worktrees {
		writeErr(w, http.StatusBadRequest, errors.New(
			"worktree support is off for "+p.Name+". turn it on and give it a folder first"))
		return
	}
	branch := strings.TrimSpace(req.Branch)
	if branch == "" {
		writeErr(w, http.StatusBadRequest, errors.New("which branch"))
		return
	}

	repoPath := store.ProviderPath(p.Root, req.Org, req.Repo)
	if repoPath == "" {
		writeErr(w, http.StatusBadRequest, errors.New("which repository"))
		return
	}
	// ATRIUM DOES NOT CLONE. A repository that is not there is a sentence, not
	// a network call.
	if !isCheckout(filepath.FromSlash(repoPath)) {
		writeErr(w, http.StatusBadRequest, errors.New(
			repoPath+" is not a git checkout, so there is nothing to make a worktree of. "+
				"atrium does not clone"))
		return
	}

	// ALREADY THERE IS THE ANSWER, NOT AN ERROR. Asked first, so pressing make
	// twice takes you to the worktree rather than failing.
	for _, wt := range worktreesOf(repoPath) {
		if strings.EqualFold(wt.Branch, branch) {
			writeJSON(w, http.StatusOK, map[string]any{
				"path": wt.Path, "existed": true, "branch": wt.Branch,
			})
			return
		}
	}

	dest := worktreeDest(p.WorktreeRoot, req.Org, req.Repo, branch)
	if _, err := os.Stat(filepath.FromSlash(dest)); err == nil {
		// A directory already standing where the worktree would go, which git
		// itself would refuse with a less useful sentence. Reported rather than
		// overwritten: see `flattenBranch` for how two branch names can land
		// here.
		writeErr(w, http.StatusConflict, errors.New(
			dest+" already exists but is not a worktree of that branch. "+
				"move it aside, or use a different branch name"))
		return
	}
	// The one directory atrium creates. A provider's ROOT is never created,
	// because a typo would become a directory and discovery would then report
	// success over an empty tree. This is somewhere atrium is about to put
	// something, which is a different act.
	if err := os.MkdirAll(filepath.Dir(filepath.FromSlash(dest)), 0o755); err != nil {
		s.fail(w, err)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), makeWorktreeDeadline)
	defer cancel()

	// WHERE THE BRANCH DOES NOT EXIST, MAKE IT off the repository's own HEAD.
	// A provider-level default branch is deliberately not a setting: the branch
	// is a fact about a repository, and two repositories under one root
	// routinely disagree about main versus master.
	made := !branchExists(ctx, repoPath, branch)
	args := []string{"worktree", "add", filepath.FromSlash(dest), branch}
	if made {
		args = []string{"worktree", "add", "-b", branch, filepath.FromSlash(dest)}
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = filepath.FromSlash(repoPath)
	// Nothing on stdin, so a command that decides to ask a question fails
	// rather than waiting out the deadline in silence.
	cmd.Stdin = nil
	out, err := cmd.CombinedOutput()
	if err != nil {
		writeErr(w, http.StatusBadRequest, errors.New(
			"git could not make that worktree: "+strings.TrimSpace(tailLines(string(out)))))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"path": dest, "existed": false, "branch": branch, "created_branch": made,
	})
}

// worktreeDest is `<worktree_root>/<org>/<repo>/<branch>`, which is the layout
// already on this machine.
func worktreeDest(root, org, repo, branch string) string {
	base := store.ProviderPath(root, org, repo)
	if base == "" {
		return ""
	}
	return base + "/" + flattenBranch(branch)
}

// flattenBranch turns `feature/thing` into `feature-thing`.
//
// Nesting would round-trip more cleanly and git would not mind, but it leaves
// an empty parent directory behind when a worktree is removed, and an empty
// directory in the worktree root BLOCKS THE TOGGLE. That would make the
// feature's own refusal fire on debris the feature created.
//
// The cost is a collision between `a/b` and `a-b`, which is rare and which the
// caller detects and reports rather than silently overwriting.
func flattenBranch(b string) string {
	b = strings.TrimSpace(b)
	b = strings.ReplaceAll(b, "\\", "/")
	return strings.Trim(strings.ReplaceAll(b, "/", "-"), "-")
}

// branchExists asks git, so a name that is already a branch is checked out
// rather than refused as a duplicate.
func branchExists(ctx context.Context, repo, branch string) bool {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--verify", "--quiet",
		"refs/heads/"+branch)
	cmd.Dir = filepath.FromSlash(repo)
	return cmd.Run() == nil
}

// tailLines keeps the end of a command's output, which is where git puts the
// reason, bounded so an error is a message rather than a transcript.
func tailLines(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\r\n"), "\n")
	if len(lines) > 6 {
		lines = lines[len(lines)-6:]
	}
	return strings.Join(lines, "\n")
}
