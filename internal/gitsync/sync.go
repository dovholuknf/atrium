package gitsync

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// The sync states, exactly as docs/rnd/git-sync-design.md section 6.2 has them.
const (
	StateOK     = "ok"     // fetched, and both branches are at the hub's sha
	StateAbsent = "absent" // no clone and init was false. Nothing was run
	StateBehind = "behind" // fetched, and git refused a move
	StateFailed = "failed" // nothing was fetched
)

// IntegrationBranch is the ONLY branch a room ever syncs from the hub. It is not a
// parameter: nothing in a request or a setting can name another source branch, because a
// room's hub-main that followed a department branch put workers on unmerged work (sg3).
const IntegrationBranch = "claude/main"

// Result is what a sync answers, and what GET /v1/git/status remembers.
type Result struct {
	State  string `json:"state"`
	SHA    string `json:"sha,omitempty"`
	Detail string `json:"detail,omitempty"`
}

// Syncer syncs one room's clones, one at a time, and remembers the last answer for each.
type Syncer struct {
	Runner *Runner
	// Root is where clones live: `<Root>/<name>`.
	Root func() string

	mu   sync.Mutex
	last map[string]Status
}

// Status is a remembered answer.
type Status struct {
	Name string `json:"name"`
	Result
	At string `json:"at"`
}

func (s *Syncer) runner() *Runner {
	if s.Runner != nil {
		return s.Runner
	}
	return Default
}

// Last is every remembered answer, newest first is not promised. Empty before any sync.
func (s *Syncer) Last() []Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Status, 0, len(s.last))
	for _, v := range s.last {
		out = append(out, v)
	}
	return out
}

// Sync fetches the hub's integration branch into the clone of `name` and moves the local
// branches onto it. `hubRT` reaches the hub, and `hubErr` says why it cannot (a hub that did
// not say Git), in which case nothing is run. The clone is `<Root>/<name>`.
//
// It only ever FETCHES. No local branch is forced past git's own refusals: a worktree that
// holds claude/main, or a dirty checkout of hub-main, comes back as `behind`.
func (s *Syncer) Sync(ctx context.Context, name string, init bool, hubRT func() (http.RoundTripper, error)) Result {
	s.mu.Lock()
	defer s.mu.Unlock()
	res := s.sync(ctx, name, init, hubRT)
	if s.last == nil {
		s.last = map[string]Status{}
	}
	s.last[name] = Status{Name: name, Result: res, At: time.Now().UTC().Format(time.RFC3339)}
	return res
}

func failed(detail string) Result { return Result{State: StateFailed, Detail: detail} }

func (s *Syncer) sync(ctx context.Context, name string, init bool, hub func() (http.RoundTripper, error)) Result {
	g := s.runner()
	if !ValidName(name) {
		return failed("that is not a repository name")
	}
	if _, err := g.CheckGit(ctx); err != nil {
		return failed(firstLine(err))
	}
	root := ""
	if s.Root != nil {
		root = s.Root()
	}
	if root == "" {
		return failed("no git root is set on this room")
	}
	clone := filepath.Join(root, filepath.FromSlash(name))
	if _, err := os.Stat(filepath.Join(clone, ".git")); err != nil {
		if !os.IsNotExist(err) {
			return failed(err.Error())
		}
		if !init {
			return Result{State: StateAbsent}
		}
		if err := os.MkdirAll(clone, 0o755); err != nil {
			return failed(err.Error())
		}
		if _, err := g.Git(ctx, clone, "init", "--quiet"); err != nil {
			return failed(firstLine(err))
		}
	}

	if hub == nil {
		return failed("this hub predates git sync")
	}
	rt, err := hub()
	if err != nil {
		return failed(firstLine(err))
	}

	gitDir := filepath.Join(clone, ".git")
	_, left := CleanLocks(gitDir, RoomOwned, 0)

	fwd, err := NewForwarder(rt, "hub", "")
	if err != nil {
		return failed(err.Error())
	}
	defer fwd.Close()
	url := fwd.URL + "/" + name + ".git"
	if _, err := g.Git(ctx, clone, "fetch", "--no-tags", "--no-write-fetch-head", url,
		"+refs/heads/"+IntegrationBranch+":refs/remotes/hub/"+IntegrationBranch); err != nil {
		d := firstLine(err)
		if len(left) > 0 {
			d += " (" + left[0] + ")"
		}
		return failed(d)
	}
	sha, err := g.Git(ctx, clone, "rev-parse", "--verify", "-q", "refs/remotes/hub/"+IntegrationBranch)
	if err != nil {
		return failed(firstLine(err))
	}
	sha = strings.TrimSpace(sha)

	res := Result{State: StateOK, SHA: sha}
	refuse := func(err error) {
		res.State = StateBehind
		if res.Detail == "" {
			res.Detail = firstLine(err)
		}
	}
	at := func(ref string) string {
		out, err := g.Git(ctx, clone, "rev-parse", "--verify", "-q", ref)
		if err != nil {
			return ""
		}
		return strings.TrimSpace(out)
	}

	// claude/main: git itself refuses when a worktree has it checked out.
	if at("refs/heads/claude/main") != sha {
		if _, err := g.Git(ctx, clone, "branch", "-f", "claude/main", sha); err != nil {
			refuse(err)
		}
	}
	// hub-main: kept for worktrees and briefs that name it.
	if at("refs/heads/hub-main") != sha {
		head, _ := g.Git(ctx, clone, "symbolic-ref", "--short", "-q", "HEAD")
		var err error
		if strings.TrimSpace(head) == "hub-main" {
			_, err = g.Git(ctx, clone, "reset", "--keep", "--quiet", sha)
		} else {
			_, err = g.Git(ctx, clone, "branch", "-f", "hub-main", sha)
		}
		if err != nil {
			refuse(err)
		}
	}
	return res
}

func firstLine(err error) string {
	var ge *Error
	if errors.As(err, &ge) {
		if s := ge.First(); s != "" {
			return s
		}
	}
	return strings.TrimSpace(strings.SplitN(err.Error(), "\n", 2)[0])
}
