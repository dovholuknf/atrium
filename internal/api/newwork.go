package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/base/nowindow"
	"github.com/dovholuknf/atrium/internal/base/rules"
	"github.com/dovholuknf/atrium/internal/base/userenv"
	"github.com/dovholuknf/atrium/internal/gitsync"
	"github.com/dovholuknf/atrium/internal/store"
)

// NEW WORK: `gwt new some.new.work`, from the board and from `atrium new`. One call that updates the repo's origin,
// makes a worktree on a branch named for the work, runs the repo's setup hook and starts a card there.
//
//	POST /v1/new {repo: "host/org/repo", name, from?, prompt?, harness?, title?, why?, model?, effort?}
//
// Where the branch starts, in order:
//
//  1. `from`, when the request names one (origin/<from> first, since the fetch just moved it, then the ref as given)
//  2. origin/<name>, when the remote already has that branch: the work is tracked and picked up where it stands
//  3. the remote's default branch
//
// The operator's own clone is never checked out, pulled or fast-forwarded. Only its remote-tracking refs move.
//
// A name that already has a worktree answers that worktree and the live card in it, and starts nothing. A worktree with
// no live card gets one, and its setup hook is not run again.
//
// It reuses branchWorktree (openkinds.go), which is also the issue and branch links' path, so a third way of making a
// worktree is not added. The room runs the fetch against its own origin over git, which is not a forge API.

// newWorkHookDeadline bounds the setup hook, so a hook that waits on a prompt cannot hold the request.
const newWorkHookDeadline = 2 * time.Minute

// SettingWorktreeHooks is the folder of per-repo setup hooks. Unset, it is ~/.atrium/hooks.
const SettingWorktreeHooks = "worktree_hooks_dir"

type newWorkRequest struct {
	Repo    string `json:"repo"`
	Name    string `json:"name"`
	From    string `json:"from"`
	Prompt  string `json:"prompt"`
	Harness string `json:"harness"`
	Title   string `json:"title"`
	Why     string `json:"why"`
	Model   string `json:"model"`
	Effort  string `json:"effort"`
	// Progress is an id the board made up. Each step is broadcast as an `open-progress` event carrying it, the same as
	// an open of a link, so the card the board drew for the paste can name the step.
	Progress string `json:"progress"`
	// Prepare finds or clones the repo and answers, with no worktree, no hook and no card. A pasted repo link that no
	// hub or room here has seen stops after this for a confirm, since a worktree's own settings and hooks run once a
	// runner starts in it. Name is not needed.
	Prepare bool `json:"prepare"`
}

type newWorkAnswer struct {
	Card       string `json:"card"`
	Worktree   string `json:"worktree"`
	Repo       string `json:"repo"`
	Cloned     bool   `json:"cloned,omitempty"`
	Checkout   string `json:"checkout,omitempty"`
	Branch     string `json:"branch"`
	Base       string `json:"base,omitempty"`
	Created    bool   `json:"created"`
	Existed    bool   `json:"existed"`
	Hook       string `json:"hook,omitempty"`
	HookError  string `json:"hook_error,omitempty"`
	FetchError string `json:"fetch_error,omitempty"`

	// HookWarning says the hooks folder is not there, so no setup hook could run.
	HookWarning string `json:"hook_warning,omitempty"`
}

func (s *Server) newWork(w http.ResponseWriter, r *http.Request) {
	if s.Launch == nil {
		openFail(w, http.StatusNotImplemented, "card", "not_wired", "no daemon wired")
		return
	}
	var in newWorkRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&in); err != nil {
		openFail(w, http.StatusBadRequest, "request", "bad_request", "the body is not json: "+err.Error())
		return
	}
	pick := strings.Trim(strings.TrimSpace(in.Repo), "/")
	name := strings.TrimSpace(in.Name)
	if !rules.RepoSlug(pick) {
		openFail(w, http.StatusBadRequest, "request", "bad_request",
			"repo is host/org/repo, like github.com/openziti/ziti")
		return
	}
	if name == "" && !in.Prepare {
		openFail(w, http.StatusBadRequest, "request", "bad_request", "name is required: it is the branch and the folder")
		return
	}
	parts := strings.Split(pick, "/")
	host, org, repo := strings.ToLower(parts[0]), parts[1], parts[2]

	ctx, cancel := context.WithTimeout(r.Context(), makeWorktreeDeadline)
	defer cancel()
	steps := s.newOpSteps(strings.TrimSpace(in.Progress), pick)
	defer steps.end()
	ctx = withOpSteps(ctx, steps)
	if in.Prepare {
		s.prepareRepo(ctx, w, host, org, repo, pick)
		return
	}
	wt, status, err := s.branchWorktreeFrom(ctx, s.providerByHost(host), host, org, repo, name, true,
		branchOpts{fetch: true, from: strings.TrimSpace(in.From)})
	if err != nil {
		worktreeFail(w, status, err)
		return
	}
	ans := newWorkAnswer{Worktree: wt.Path, Repo: pick, Branch: wt.Branch, Base: wt.Base, Existed: wt.Existed,
		FetchError: wt.FetchError}

	steps.begin("card", "starting the card")
	// A WORKTREE THAT IS ALREADY THERE, with its live card, is the answer. Nothing starts a second one.
	if wt.Existed {
		if t := s.liveCardIn(wt.Path); t != nil {
			// the card's own spelling of the folder, which is what the first answer gave: git says it by its real path
			ans.Card, ans.Worktree = t.ID, t.Worktree
			writeJSON(w, http.StatusOK, ans)
			return
		}
	}

	if !wt.Existed {
		if ans.HookWarning = s.missingHooksDir(); ans.HookWarning != "" {
			steps.note(ans.HookWarning)
		}
		ans.Hook, ans.HookError = s.runWorktreeHook(ctx, host, org, repo, wt.Path)
	}

	pending := "pending:new:" + host + "/" + org + "/" + repo + "/" + name + ":" + strconv.FormatInt(time.Now().UnixNano(), 36)
	if !wt.Existed {
		own := func(kind, ref, detail string) {
			if _, err := s.st.AddResource(pending, kind, ref, detail); err != nil {
				log.Printf("[atrium api] new %s: the inventory did not take %s %s: %v", pick, kind, ref, err)
			}
		}
		own(store.ResWorktree, wt.Path, wt.Repo)
		if wt.CreatedBranch {
			own(store.ResBranch, wt.Branch, wt.Repo)
		}
		if wt.Cloned {
			own(store.ResClone, wt.Repo, "")
		}
	}
	undo := func() {
		undoPRWorktree(wt)
		rows, _ := s.st.Resources(pending)
		for _, r := range rows {
			_ = s.st.FreeResource(pending, r.Seq, "")
		}
	}

	title := strings.TrimSpace(in.Title)
	if title == "" {
		title = name
	}
	body, _ := json.Marshal(map[string]any{
		"harness": strings.TrimSpace(in.Harness), "cwd": wt.Path, "title": title, "why": in.Why,
		"prompt": strings.TrimSpace(in.Prompt), "model": strings.TrimSpace(in.Model),
		"effort": strings.TrimSpace(in.Effort), "repo": repo, "org": org, "host": host, "branch": wt.Branch,
	})
	task, err := s.Launch(body)
	if err != nil {
		undo()
		openFail(w, http.StatusBadRequest, "card", "launch_failed", "the card did not start: "+err.Error())
		return
	}
	s.PublishTask(task)
	ans.Card, ans.Created = task.ID, true
	if err := s.st.MoveResources(pending, task.ID); err != nil {
		log.Printf("[atrium api] new %s: the inventory was not handed to %s: %v", pick, task.ID, err)
	}
	s.measureAfterOpen(task)
	writeJSON(w, http.StatusCreated, ans)
}

// prepareRepo is the `prepare` of POST /v1/new: the checkout is found, or cloned the way a worktree would clone it, and
// nothing else is made. It never makes a card, so a repo is only ever started after somebody said so.
func (s *Server) prepareRepo(ctx context.Context, w http.ResponseWriter, host, org, repo, pick string) {
	p := s.providerByHost(host)
	if p != nil && p.Host != "" {
		host = p.Host
	}
	if !s.haveCheckout(p, host, org, repo) {
		stepsOf(ctx).begin("clone", "cloning "+pick)
	}
	path, _, cloned, status, err := s.repoCheckout(ctx, p, host, org, repo)
	if err != nil {
		worktreeFail(w, status, err)
		return
	}
	writeJSON(w, http.StatusOK, newWorkAnswer{Repo: pick, Worktree: "", Cloned: cloned, Checkout: path})
}

// liveCardIn is a card that is not done or dead and runs in dir, or nil.
func (s *Server) liveCardIn(dir string) *store.Task {
	all, err := s.st.List()
	if err != nil {
		return nil
	}
	want := dirKey(dir)
	for _, t := range all {
		if t.Status == store.StatusDone || t.Status == store.StatusDead {
			continue
		}
		if t.Worktree != "" && dirKey(t.Worktree) == want {
			return t
		}
	}
	return nil
}

// dirKey is a folder as two spellings of it compare equal. git answers a worktree by its real path, while a card keeps
// the path its runner was launched in, so /var and /private/var on macOS and a short and a long name on Windows are
// the same folder. A folder that is not there is compared as written.
func dirKey(p string) string {
	p = filepath.Clean(filepath.FromSlash(p))
	if real, err := filepath.EvalSymlinks(p); err == nil {
		p = real
	}
	return strings.ToLower(strings.TrimRight(filepath.ToSlash(p), "/"))
}

// newWorkBase is the ref a branch called name that exists nowhere yet starts from. See the header.
func newWorkBase(ctx context.Context, repo, name, from string) (string, error) {
	has := func(ref string) bool {
		_, err := gitIn(ctx, repo, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
		return err == nil
	}
	if from = strings.TrimSpace(from); from != "" {
		if strings.HasPrefix(from, "-") {
			return "", fmt.Errorf("%q is not a ref", from)
		}
		for _, ref := range []string{"refs/remotes/origin/" + from, from} {
			if has(ref) {
				return ref, nil
			}
		}
		return "", fmt.Errorf("%s is not a branch, tag or commit in %s, here or on origin", from, repo)
	}
	if ref := "refs/remotes/origin/" + name; has(ref) {
		return ref, nil
	}
	for _, ref := range []string{"refs/remotes/origin/HEAD", "refs/remotes/origin/main", "refs/remotes/origin/master"} {
		if has(ref) {
			return ref, nil
		}
	}
	return defaultBase(ctx, repo), nil
}

// worktreeHookFile is the setup hook of a repo, or "": `<hooks>/<host>/<org>/<repo>/worktree.ps1` or `worktree.sh`,
// where host is the full host name or its first label (github.com or github), as gwt's hooks are laid out.
func (s *Server) worktreeHookFile(host, org, repo string) string {
	dir := s.worktreeHooksDir()
	if dir == "" {
		return ""
	}
	hosts := []string{host}
	if i := strings.Index(host, "."); i > 0 {
		hosts = append(hosts, host[:i])
	}
	for _, h := range hosts {
		for _, f := range []string{"worktree.ps1", "worktree.sh"} {
			p := filepath.Join(dir, h, org, repo, f)
			if st, err := os.Stat(p); err == nil && !st.IsDir() {
				return p
			}
		}
	}
	return ""
}

// worktreeHooksDir is the folder of setup hooks, the setting or ~/.atrium/hooks, or "" with no home.
func (s *Server) worktreeHooksDir() string {
	dir, _ := s.st.Setting(SettingWorktreeHooks)
	dir = gitsync.ExpandHome(strings.TrimSpace(dir))
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		dir = filepath.Join(home, ".atrium", "hooks")
	}
	return dir
}

// missingHooksDir is a warning when the hooks folder is not a folder, or "".
func (s *Server) missingHooksDir() string {
	dir := s.worktreeHooksDir()
	if dir == "" {
		return ""
	}
	if st, err := os.Stat(dir); err == nil && st.IsDir() {
		return ""
	}
	return "the hooks folder " + filepath.ToSlash(dir) + " does not exist, so no setup hook ran (setting " +
		SettingWorktreeHooks + ")"
}

// runWorktreeHook runs the repo's setup hook in the new worktree, and answers which file ran and why it failed. A hook
// that fails never fails the new work: the worktree is made, and the card says so.
func (s *Server) runWorktreeHook(ctx context.Context, host, org, repo, path string) (ran, failed string) {
	file := s.worktreeHookFile(host, org, repo)
	if file == "" {
		return "", ""
	}
	ctx, cancel := context.WithTimeout(ctx, newWorkHookDeadline)
	defer cancel()
	var cmd *exec.Cmd
	if strings.HasSuffix(file, ".ps1") {
		shell := "pwsh"
		if _, err := exec.LookPath(shell); err != nil {
			shell = "powershell"
		}
		cmd = exec.CommandContext(ctx, shell, "-NoProfile", "-File", file,
			"-WorktreePath", filepath.FromSlash(path), "-Org", org, "-Repo", repo, "-RemoteHost", host)
	} else {
		cmd = exec.CommandContext(ctx, "sh", file, filepath.FromSlash(path), org, repo, host)
	}
	cmd.Dir = filepath.FromSlash(path)
	cmd.Env = append(userenv.Refresh(os.Environ()), "ATRIUM_WORKTREE="+filepath.FromSlash(path), "ATRIUM_ORG="+org,
		"ATRIUM_REPO="+repo, "ATRIUM_HOST="+host)
	nowindow.Hide(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(tailLines(string(out)))
		if msg == "" {
			msg = err.Error()
		}
		log.Printf("[atrium api] new %s/%s/%s: hook %s failed: %v", host, org, repo, file, err)
		return file, msg
	}
	return file, ""
}
