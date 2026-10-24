package shepherd

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/gitsync"
)

// Check is one verification, as the packet shows it. The exit code and the last lines are the evidence.
type Check struct {
	Name  string `json:"name"`
	Code  int    `json:"code"`
	Tail  string `json:"tail,omitempty"`
	Rerun bool   `json:"rerun,omitempty"`
	Note  string `json:"note,omitempty"`
}

// OK is whether the check passed.
func (c Check) OK() bool { return c.Code == 0 }

// testTimeout bounds scripts/test-touched.sh, which the brief gives 600000ms.
const testTimeout = 590 * time.Second

// stageStep is the recipe's step 4 with exit codes: cherry-pick on claude/land-<id>, strip BOMs, gofmt, globals,
// test-touched. Each result goes to the packet. A conflict or an unknown red goes to the exception phase and the
// staging branch is left as it is for whoever reads it.
type stageStep struct{}

func (stageStep) Phase() Phase { return PhaseStage }

func (stageStep) Run(ctx context.Context, e *Env, st *State) (Outcome, error) {
	main := e.Cfg.mainBranch()
	stage := path.Join(e.Cfg.Worktrees, "land-"+st.ItemID)
	st.Stage = stage
	have, err := hasWorktree(ctx, e, stage)
	if err != nil {
		return Outcome{}, err
	}
	// Resuming resets the branch to main, which is safe: nothing has landed, and the picks are redone.
	if have {
		_, err = git(ctx, e, stage, "switch", "-C", st.Branch, main)
	} else {
		_, err = git(ctx, e, e.Cfg.Repo, "worktree", "add", "-B", st.Branch, stage, main)
	}
	if err != nil {
		return Outcome{}, err
	}
	if err := configureWorktree(ctx, e, stage); err != nil {
		return Outcome{}, err
	}

	var checks []Check
	finish := func(p Phase, why string) (Outcome, error) {
		raw, _ := json.Marshal(checks)
		// The hold phase reads these into the packet. An exception keeps them as the evidence it stopped on.
		st.Packet = string(raw)
		if p == PhaseHold {
			return Outcome{Next: PhaseHold}, nil
		}
		return stop(st, p, why), nil
	}

	r, err := e.Runner.Run(ctx, stage, "git", "cherry-pick", main+".."+st.Ref)
	if err != nil {
		return Outcome{}, fmt.Errorf("git cherry-pick: %w", err)
	}
	checks = append(checks, Check{Name: "cherry-pick", Code: r.Code, Tail: tail(r.Out(), 8)})
	if r.Code != 0 {
		// Leave nothing half-picked behind for the next phase to trip on.
		_, _ = e.Runner.Run(ctx, stage, "git", "cherry-pick", "--abort")
		return finish(PhaseException, "the cherry-pick did not apply: a conflict a model or a person resolves")
	}

	stripped, err := stripBOMs(ctx, e, stage, main)
	if err != nil {
		return Outcome{}, err
	}
	if len(stripped) > 0 {
		if _, err := git(ctx, e, stage, "commit", "-a", "--amend", "--no-edit"); err != nil {
			return Outcome{}, err
		}
	}
	checks = append(checks, Check{Name: "bom", Note: fmt.Sprintf("%d stripped", len(stripped))})

	r, err = e.Runner.Run(ctx, stage, "gofmt", "-l", "internal/", "cmd/")
	if err != nil {
		return Outcome{}, fmt.Errorf("gofmt: %w", err)
	}
	fmtCheck := Check{Name: "gofmt", Code: r.Code, Tail: tail(r.Out(), 8)}
	if r.Code == 0 && strings.TrimSpace(r.Stdout) != "" {
		fmtCheck.Code = 1
		fmtCheck.Note = "gofmt -l must print nothing"
	}
	checks = append(checks, fmtCheck)

	r, err = e.Runner.Run(ctx, stage, "go", "run", "scripts/check-test-globals.go")
	if err != nil {
		return Outcome{}, fmt.Errorf("check-test-globals: %w", err)
	}
	checks = append(checks, Check{Name: "globals", Code: r.Code, Tail: tail(r.Out(), 8)})

	tctx, cancel := context.WithTimeout(ctx, testTimeout)
	defer cancel()
	tt, err := runTests(tctx, e, stage)
	if err != nil {
		return Outcome{}, err
	}
	checks = append(checks, tt)

	for _, c := range checks {
		if !c.OK() {
			return finish(PhaseException, c.Name+" failed: "+c.Tail)
		}
	}
	return finish(PhaseHold, "")
}

// runTests runs test-touched under the merge queue's red rules: a 20ms budget miss gets one rerun, recorded as one, and
// a red whose every failing test is in the repo's known-failing list passes, and says so.
func runTests(ctx context.Context, e *Env, stage string) (Check, error) {
	run := func() (Result, error) {
		return e.Runner.Run(ctx, stage, "bash", "scripts/test-touched.sh")
	}
	r, err := run()
	if err != nil {
		return Check{}, fmt.Errorf("test-touched: %w", err)
	}
	c := Check{Name: "test-touched", Code: r.Code, Tail: tail(r.Out(), 12)}
	if r.Code != 0 && gitsync.RedRerun(r.Out()) {
		r, err = run()
		if err != nil {
			return Check{}, fmt.Errorf("test-touched rerun: %w", err)
		}
		c = Check{Name: "test-touched", Code: r.Code, Tail: tail(r.Out(), 12), Rerun: true}
	}
	if c.Code != 0 {
		known, err := e.Files.Read(path.Join(e.Cfg.Repo, gitsync.KnownFailingFile))
		if err == nil && gitsync.RedKnown(r.Out(), string(known)) {
			c.Code = 0
			c.Note = "every failing test is in " + gitsync.KnownFailingFile
		}
	}
	return c, nil
}

// stripBOMs removes a leading EF BB BF from every .md the branch added or changed. A BOM-prefixed changelog entry
// breaks the build's changelog parse.
func stripBOMs(ctx context.Context, e *Env, stage, main string) ([]string, error) {
	out, err := git(ctx, e, stage, "diff", "--name-only", "--diff-filter=AM", main, "HEAD")
	if err != nil {
		return nil, err
	}
	var done []string
	for _, f := range strings.Split(out, "\n") {
		f = strings.TrimSpace(f)
		if !strings.HasSuffix(f, ".md") {
			continue
		}
		p := path.Join(stage, f)
		b, err := e.Files.Read(p)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", f, err)
		}
		if len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF {
			if err := e.Files.Write(p, b[3:]); err != nil {
				return nil, fmt.Errorf("write %s: %w", f, err)
			}
			done = append(done, f)
		}
	}
	return done, nil
}
