package shepherd

import (
	"context"
	"fmt"
	"path"
	"strings"
	"time"
)

func (c Config) mainBranch() string {
	if c.Main != "" {
		return c.Main
	}
	return "claude/main"
}

// git runs git in a directory and turns a non-zero exit into an error carrying the last of its output.
func git(ctx context.Context, e *Env, dir string, args ...string) (string, error) {
	r, err := e.Runner.Run(ctx, dir, "git", args...)
	if err != nil {
		return "", fmt.Errorf("git %s: %w", args[0], err)
	}
	if r.Code != 0 {
		return r.Out(), fmt.Errorf("git %s exited %d: %s", strings.Join(args, " "), r.Code, tail(r.Out(), 6))
	}
	return strings.TrimSpace(r.Stdout), nil
}

// tail is the last n lines, which is where a failure says what it was.
func tail(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

func stop(st *State, p Phase, why string) Outcome {
	st.Phase, st.LastError = p, why
	return Outcome{Stop: true}
}

// ---- check ----

// checkStep reads the item, refuses one that is gone or already done, and fixes the sha the worker starts from.
// Whether the work is ALREADY on main is a reading task, so the grep evidence goes in the packet for the person.
type checkStep struct{}

func (checkStep) Phase() Phase { return PhaseCheck }

func (checkStep) Run(ctx context.Context, e *Env, st *State) (Outcome, error) {
	it, err := e.Hub.Backlog(ctx, st.ItemID)
	if err != nil {
		return Outcome{}, fmt.Errorf("read the backlog item: %w", err)
	}
	switch it.Status {
	case "done", "dropped":
		return stop(st, PhaseRefused, "the item is already "+it.Status), nil
	}
	if st.BaseSHA == "" {
		sha, err := git(ctx, e, e.Cfg.Repo, "rev-parse", e.Cfg.mainBranch())
		if err != nil {
			return Outcome{}, err
		}
		st.BaseSHA = sha
	}
	st.Branch = "claude/land-" + st.ItemID
	if st.Room == "" {
		st.Room = "local"
	}
	return Outcome{Next: PhaseWorktree}, nil
}

// ---- worktree ----

// worktreeStep makes the worker's worktree at the base sha and asserts the HEAD it got, because a failed add that
// goes on in the wrong directory is the failure the recipe was bitten by.
type worktreeStep struct{}

func (worktreeStep) Phase() Phase { return PhaseWorktree }

func (worktreeStep) Run(ctx context.Context, e *Env, st *State) (Outcome, error) {
	wt := path.Join(e.Cfg.Worktrees, st.ItemID)
	st.Worktree = wt
	branch := "claude/" + st.ItemID
	have, err := hasWorktree(ctx, e, wt)
	if err != nil {
		return Outcome{}, err
	}
	if !have {
		// The branch may be there from a run that died after making it.
		if _, err := git(ctx, e, e.Cfg.Repo, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch); err == nil {
			_, err = git(ctx, e, e.Cfg.Repo, "worktree", "add", wt, branch)
		} else {
			_, err = git(ctx, e, e.Cfg.Repo, "worktree", "add", "-b", branch, wt, st.BaseSHA)
		}
		if err != nil {
			return Outcome{}, err
		}
	}
	if err := configureWorktree(ctx, e, wt); err != nil {
		return Outcome{}, err
	}
	head, err := git(ctx, e, wt, "rev-parse", "HEAD")
	if err != nil {
		return Outcome{}, err
	}
	// A worker already committed is a worktree past the base, which is a resume and not a failure.
	if !have && head != st.BaseSHA {
		return Outcome{}, fmt.Errorf("the worktree is at %s, not the base %s: it was not made where it was meant to be", head, st.BaseSHA)
	}
	it, err := e.Hub.Backlog(ctx, st.ItemID)
	if err != nil {
		return Outcome{}, fmt.Errorf("read the backlog item for the brief: %w", err)
	}
	prof, err := operator(ctx, e)
	if err != nil {
		return Outcome{}, err
	}
	brief := fmt.Sprintf("# %s\n\n%s\n\n%s\n", it.ID+": "+it.Title, strings.TrimSpace(it.Body), strings.TrimSpace(strings.ReplaceAll(e.Cfg.Rules, "{operator}", prof.Display)))
	if err := e.Files.Write(path.Join(wt, "BRIEF.md"), []byte(brief)); err != nil {
		return Outcome{}, fmt.Errorf("write the brief: %w", err)
	}
	return Outcome{Next: PhaseLaunch}, nil
}

func hasWorktree(ctx context.Context, e *Env, wt string) (bool, error) {
	out, err := git(ctx, e, e.Cfg.Repo, "worktree", "list", "--porcelain")
	if err != nil {
		return false, err
	}
	for _, l := range strings.Split(out, "\n") {
		if strings.TrimSpace(strings.TrimPrefix(l, "worktree ")) == wt && strings.HasPrefix(l, "worktree ") {
			return true, nil
		}
	}
	return false, nil
}

// operator is the hub's operator profile, shown by name when it has no display name.
func operator(ctx context.Context, e *Env) (Profile, error) {
	p, err := e.Hub.Profile(ctx)
	if err != nil {
		return p, fmt.Errorf("read the operator profile: %w", err)
	}
	if p.Display == "" {
		p.Display = p.Name
	}
	return p, nil
}

// configureWorktree gives a worktree the operator as its author, in the worktree's own config so no other checkout changes.
func configureWorktree(ctx context.Context, e *Env, wt string) error {
	prof, err := operator(ctx, e)
	if err != nil {
		return err
	}
	for _, kv := range [][]string{
		{"extensions.worktreeConfig", "true"},
	} {
		if _, err := git(ctx, e, wt, "config", kv[0], kv[1]); err != nil {
			return err
		}
	}
	for _, kv := range [][]string{
		{"user.name", prof.Name},
		{"user.email", prof.Email},
	} {
		if _, err := git(ctx, e, wt, "config", "--worktree", kv[0], kv[1]); err != nil {
			return err
		}
	}
	return nil
}

// ---- launch ----

// launchStep starts the worker as an isolated item owned by the human, so its end is one keyed notice and a state on
// the hub, and the shepherd, which is not a session, is paged by nothing.
//
// THE ONE WINDOW: a launch that started a card and died before the row was saved starts a second one on resume. The
// card id is the only proof, and it is printed after the card exists. A card the first run made is visible on the
// board by the item's tag, and `atrium close` removes it.
type launchStep struct{}

func (launchStep) Phase() Phase { return PhaseLaunch }

func (launchStep) Run(ctx context.Context, e *Env, st *State) (Outcome, error) {
	if st.Card != "" {
		return Outcome{Next: PhaseWait}, nil
	}
	r, err := e.Runner.Run(ctx, e.Cfg.Repo, e.Cfg.Atrium, "launch", "--isolated", "--item", st.ItemID,
		"--item-owner", "human", "--runner", "claude", "--model", "sonnet", "--cwd", st.Worktree,
		"--title", st.ItemID, "--tags", "atrium:item:"+st.ItemID, "--prompt", "Read BRIEF.md and do the task.", "--quiet")
	if err != nil {
		return Outcome{}, fmt.Errorf("atrium launch: %w", err)
	}
	if r.Code != 0 {
		return Outcome{}, fmt.Errorf("atrium launch exited %d: %s", r.Code, tail(r.Out(), 6))
	}
	lines := strings.Split(strings.TrimSpace(r.Stdout), "\n")
	card := strings.TrimSpace(lines[len(lines)-1])
	if card == "" || strings.ContainsAny(card, " \t") {
		return Outcome{}, fmt.Errorf("atrium launch printed no card id: %q", tail(r.Out(), 3))
	}
	st.Card = card
	return Outcome{Next: PhaseWait}, nil
}

// ---- wait ----

// waitStep polls the hub's item, never subscribes: a poll cannot miss a message and a stream can.
type waitStep struct{}

func (waitStep) Phase() Phase { return PhaseWait }

func (waitStep) Run(ctx context.Context, e *Env, st *State) (Outcome, error) {
	delay := e.Cfg.PollStart
	if delay <= 0 {
		delay = 3 * time.Second
	}
	max := e.Cfg.PollMax
	if max <= 0 {
		max = time.Minute
	}
	for {
		it, ok, err := e.Hub.ItemForCard(ctx, st.Card)
		if err != nil {
			// The hub being away is a wait, not an end: the row is on it, and the worker is unaffected.
			e.say("hub not answering: %v", err)
		} else if ok {
			st.WorkItem = it.ID
			switch it.State {
			case "finished":
				return Outcome{Next: PhaseCollect}, nil
			case "blocked":
				st.Packet = it.Note
				return stop(st, PhaseHeldBlocked, ""), nil
			}
		} else if status, err := e.Hub.CardStatus(ctx, st.Card); err == nil && status == "dead" {
			// A card that is dead with no item ever pushed ended before it began. With an item still running it is
			// caught below.
			return stop(st, PhaseFailed, "the worker's card is dead and its item never reported"), nil
		}
		if ok && it.State == "running" {
			if status, err := e.Hub.CardStatus(ctx, st.Card); err == nil && status == "dead" {
				return stop(st, PhaseFailed, "the worker's card is dead with its item still running"), nil
			}
		}
		if err := e.Sleep(ctx, delay); err != nil {
			return Outcome{}, err
		}
		if delay *= 2; delay > max {
			delay = max
		}
	}
}

// ---- collect ----

// collectStep reads the worker's branch. The worker's worktree is a worktree of the main checkout, so its branch is
// already a ref here and there is nothing to fetch. A remote room would collect first, and is not built.
type collectStep struct{}

func (collectStep) Phase() Phase { return PhaseCollect }

func (collectStep) Run(ctx context.Context, e *Env, st *State) (Outcome, error) {
	ref := "refs/heads/claude/" + st.ItemID
	if _, err := git(ctx, e, e.Cfg.Repo, "rev-parse", "--verify", "--quiet", ref); err != nil {
		return Outcome{}, fmt.Errorf("the worker's branch %s is not there: %w", ref, err)
	}
	st.Ref = ref
	// An empty range is the worker saying done on work that was already on main, or on nothing. A script cannot read
	// which, so it refuses to land a no-op and hands the case on.
	commits, err := git(ctx, e, e.Cfg.Repo, "log", "--oneline", e.Cfg.mainBranch()+".."+ref)
	if err != nil {
		return Outcome{}, err
	}
	if strings.TrimSpace(commits) == "" {
		return stop(st, PhaseException, "the worker's branch holds no commit that main lacks: verify against main, do not land a no-op"), nil
	}
	return Outcome{Next: PhaseStage}, nil
}
