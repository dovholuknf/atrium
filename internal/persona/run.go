package persona

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/safepath"
	"go.yaml.in/yaml/v3"
)

// Target is what a review looks at: a card's worktree and the diff range of
// its branch against the default branch.
type Target struct {
	Worktree      string `json:"worktree"`
	Branch        string `json:"branch"`
	DefaultBranch string `json:"default_branch"`
	MergeBase     string `json:"merge_base"`
	// Range is `<merge-base>..<branch>`, the committed work on the branch.
	Range string `json:"range"`
	// Dirty says the worktree also has uncommitted changes, which are part of
	// what the author is asking about and are named separately in TARGET.md.
	Dirty bool `json:"dirty"`
	// RepoKey is `<host>/<org>/<repo>`, the key knowledge and memory are
	// scoped by. Empty when the worktree has no origin atrium can read.
	RepoKey string `json:"repo_key"`
}

// RepoHint is what the card already knows about its repository. Used when all
// three are set, since the launcher that resolved them knew better than a
// remote url does.
type RepoHint struct{ Host, Org, Repo string }

// ResolveTarget reads the worktree's branch, default branch and merge-base.
// All of it is `git` reading, and none of it changes the worktree.
func ResolveTarget(worktree string, hint RepoHint) (Target, error) {
	wt := filepath.FromSlash(strings.TrimSpace(worktree))
	if wt == "" {
		return Target{}, errors.New("this card has no worktree to review")
	}
	if fi, err := os.Stat(wt); err != nil || !fi.IsDir() {
		return Target{}, fmt.Errorf("%s is not a directory", worktree)
	}
	if _, err := git(wt, "rev-parse", "--git-dir"); err != nil {
		return Target{}, fmt.Errorf("%s is not in a git repository, so there is no diff to review", worktree)
	}
	t := Target{Worktree: filepath.ToSlash(wt)}

	branch, err := git(wt, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return Target{}, err
	}
	if branch == "HEAD" {
		// Detached. The commit is the only name the work has.
		if branch, err = git(wt, "rev-parse", "HEAD"); err != nil {
			return Target{}, err
		}
	}
	t.Branch = branch

	t.DefaultBranch = defaultBranch(wt)
	if t.DefaultBranch == "" {
		return Target{}, fmt.Errorf("%s has no main, master or origin/HEAD to diff against", worktree)
	}
	if t.MergeBase, err = git(wt, "merge-base", t.DefaultBranch, "HEAD"); err != nil {
		return Target{}, fmt.Errorf("%s and %s share no history: %w", t.Branch, t.DefaultBranch, err)
	}
	t.Range = t.MergeBase + ".." + t.Branch
	if st, err := git(wt, "status", "--porcelain"); err == nil && strings.TrimSpace(st) != "" {
		t.Dirty = true
	}

	if hint.Host != "" && hint.Org != "" && hint.Repo != "" {
		t.RepoKey = hostKey(hint.Host) + "/" + hint.Org + "/" + hint.Repo
	} else if origin, err := git(wt, "remote", "get-url", "origin"); err == nil {
		t.RepoKey = RepoKeyFromRemote(origin)
	}
	return t, nil
}

// defaultBranch is what origin says HEAD is, or the first of the usual names
// that exists.
func defaultBranch(wt string) string {
	if ref, err := git(wt, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil && ref != "" {
		return ref
	}
	for _, name := range []string{"main", "master", "origin/main", "origin/master"} {
		if _, err := git(wt, "rev-parse", "--verify", "--quiet", name+"^{commit}"); err == nil {
			return name
		}
	}
	return ""
}

// scpLike is `git@github.com:org/repo.git`.
var scpLike = regexp.MustCompile(`^(?:[^@/]+@)?([^:/]+):(.+)$`)

// RepoKeyFromRemote turns an origin url into `<host>/<org>/<repo>`, the layout
// dotagents already uses. `github.com` becomes `github`. Empty when the url is
// not one it can read.
func RepoKeyFromRemote(remote string) string {
	remote = strings.TrimSpace(remote)
	var host, path string
	if u, err := url.Parse(remote); err == nil && u.Scheme != "" && u.Host != "" {
		host, path = u.Hostname(), u.Path
	} else if m := scpLike.FindStringSubmatch(remote); m != nil {
		host, path = m[1], m[2]
	} else {
		return ""
	}
	path = strings.TrimSuffix(strings.Trim(path, "/"), ".git")
	parts := strings.Split(path, "/")
	if len(parts) < 2 || host == "" {
		return ""
	}
	return hostKey(host) + "/" + parts[len(parts)-2] + "/" + parts[len(parts)-1]
}

func hostKey(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	if i := strings.IndexByte(host, '.'); i > 0 {
		host = host[:i]
	}
	return host
}

// repoKeyPattern is a repo key safe to turn into a knowledge file path.
var repoKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+(/[A-Za-z0-9_.-]+){0,3}$`)

// ValidRepoKey reports whether key may name a knowledge file. `general` is the
// one key that is not a path.
func ValidRepoKey(key string) bool {
	if key == "general" {
		return true
	}
	if !repoKeyPattern.MatchString(key) {
		return false
	}
	for _, seg := range strings.Split(key, "/") {
		if seg == "." || seg == ".." || strings.HasPrefix(seg, ".") {
			return false
		}
	}
	return true
}

// KnowledgeFile is where a repo key's reviewed facts live, relative to the
// persona folder.
func KnowledgeFile(key string) string {
	if key == "general" || key == "" {
		return "knowledge/_general.md"
	}
	return "knowledge/" + key + ".md"
}

// Run is one persona review, set up and ready to launch.
type Run struct {
	ID      string `json:"id"`
	Dir     string `json:"dir"`
	Persona string `json:"persona"`
	Runner  string `json:"runner"`
	// Agent is the agent name the runner is started as, and Args the
	// arguments that say so.
	Agent  string   `json:"agent"`
	Args   []string `json:"args"`
	Prompt string   `json:"prompt"`
	Target Target   `json:"target"`
}

// NewRun makes `<runsRoot>/<id>/<run>/`, writes TARGET.md, puts the persona's
// rendered file where the runner loads it, and says how to start it.
//
// A PERSONA SESSION NEVER STARTS IN ITS OWN PACK FOLDER. The run directory is
// scratch space in atrium's data, so the session cannot edit its own
// definition by accident, and a runner that reads instructions from its
// working directory loads exactly the rendered file.
func NewRun(runsRoot, pack string, p Persona, runner string, t Target, now time.Time) (*Run, error) {
	if !ValidID(p.ID) {
		return nil, fmt.Errorf("%q is not a persona id", p.ID)
	}
	runner = strings.ToLower(strings.TrimSpace(runner))
	if runner != "claude" {
		// The render step only measures claude today (design stage 5).
		return nil, fmt.Errorf("atrium launches personas on claude only. %s is not measured yet", runner)
	}
	if !p.RendersFor(runner) {
		return nil, fmt.Errorf("%s does not render for %s. run render-personas.ps1 in dotagents", p.ID, runner)
	}
	src := renderFile(p.dir, p.ID, runner)
	body, err := os.ReadFile(src)
	if err != nil {
		return nil, err
	}

	var suffix [3]byte
	_, _ = rand.Read(suffix[:])
	runID := now.UTC().Format("20060102-150405") + "-" + hex.EncodeToString(suffix[:])
	dir, err := safepath.Contained(runsRoot, filepath.Join(p.ID, runID))
	if err != nil {
		return nil, err
	}
	agentsDir := filepath.Join(dir, ".claude", "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		return nil, err
	}
	// A project agent in the run directory, which `claude --agent` resolves
	// ahead of the user's own. It is the same file ~/.claude/agents links to,
	// copied so the run records which version of the persona reviewed.
	if err := os.WriteFile(filepath.Join(agentsDir, p.ID+".md"), body, 0o644); err != nil {
		return nil, err
	}
	agent := frontmatterName(body)
	if agent == "" {
		agent = p.ID
	}
	target := TargetMarkdown(pack, p, runner, t)
	if err := os.WriteFile(filepath.Join(dir, "TARGET.md"), []byte(target), 0o644); err != nil {
		return nil, err
	}
	return &Run{
		ID: runID, Dir: filepath.ToSlash(dir), Persona: p.ID, Runner: runner,
		Agent: agent, Args: []string{"--agent", agent},
		Prompt: launchPrompt(p), Target: t,
	}, nil
}

// frontmatterName is the `name:` in a rendered agent file, which is what
// `claude --agent` matches on.
func frontmatterName(body []byte) string {
	s := strings.ReplaceAll(string(body), "\r\n", "\n")
	if !strings.HasPrefix(s, "---\n") {
		return ""
	}
	end := strings.Index(s[4:], "\n---")
	if end < 0 {
		return ""
	}
	var fm struct {
		Name string `yaml:"name"`
	}
	if yaml.Unmarshal([]byte(s[4:4+end]), &fm) != nil {
		return ""
	}
	return strings.TrimSpace(fm.Name)
}

func launchPrompt(p Persona) string {
	return "You are the " + p.Name + " persona, started by atrium to review one card's diff. " +
		"Read TARGET.md in this directory first and do what it says: review only, change no files, " +
		"read only the knowledge and memory it lists for this repo, and return your findings as the " +
		"single fenced json array in the review-panel finding schema it quotes. When you finish, call " +
		"atrium_report with status done, no_commit \"review only\", and the json array as the summary."
}
