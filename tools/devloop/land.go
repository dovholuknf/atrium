package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/dovholuknf/atrium/internal/base/gitexec"
)

// git is what land needs from git, so the plan can be tested without a repository.
type git interface {
	run(dir string, op gitexec.Op) (string, error)
}

type realGit struct{}

func (realGit) run(dir string, op gitexec.Op) (string, error) {
	b, err := gitexec.Run(context.Background(), gitexec.Opts{Dir: dir, Combined: true, Visible: true}, op)
	s := strings.TrimSpace(string(b))
	if err != nil {
		args, _ := gitexec.Argv(gitexec.Opts{}, op)
		return s, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, s)
	}
	return s, nil
}

// landPlan is the git commands that put ref (a local branch, else hub/<branch>) on claude/main: a fast-forward when claude/main is an ancestor,
// otherwise a cherry-pick of what the branch has that claude/main lacks.
func landPlan(ref string, fastForward bool) []gitexec.Op {
	if fastForward {
		return []gitexec.Op{gitexec.MergeFFOnly{Commit: ref}}
	}
	return []gitexec.Op{gitexec.CherryPick{Revs: []string{"claude/main.." + ref}}}
}

// deployShell runs atrium-deploy.ps1 on every OS: the script needs PowerShell 7, which is pwsh, not Windows PowerShell.
const deployShell = "pwsh"

// landRef is what to land: the local branch when there is one (a landing branch is made locally and never reaches the
// hub), else the hub's copy.
func landRef(g git, repo, branch string) string {
	if _, err := g.run(repo, gitexec.RevParseVerify{Quiet: true, Rev: "refs/heads/" + branch}); err == nil {
		return branch
	}
	return "hub/" + branch
}

// landRollback is how claude/main is put back as it was.
func landRollback(old string, fastForward bool) []gitexec.Op {
	var cmds []gitexec.Op
	if !fastForward {
		cmds = append(cmds, gitexec.CherryPickAbort{})
	}
	return append(cmds, gitexec.ResetHard{Commit: old})
}

func cmdLand(args []string, out io.Writer, g git) error {
	pos, flags := splitArgs(args, "item", "repo", "deploy", "hub")
	fs := flag.NewFlagSet("land", flag.ContinueOnError)
	item := fs.String("item", "", "backlog id to mark done")
	repo := fs.String("repo", ".", "the main checkout")
	deploy := fs.String("deploy", `C:\Users\claude\.atrium2\scripts\atrium-deploy.ps1`, "atrium-deploy.ps1 path")
	hub := fs.String("hub", defaultHub, "hub base URL")
	noDeploy := fs.Bool("no-deploy", false, "skip the deploy step")
	if err := fs.Parse(flags); err != nil {
		return err
	}
	if len(pos) != 1 {
		return fmt.Errorf("usage: land <branch> [--item <id>]")
	}
	branch := pos[0]

	if st, err := g.run(*repo, gitexec.Status{Untracked: "no"}); err != nil {
		return err
	} else if st != "" {
		return fmt.Errorf("the tree is dirty, refusing to land")
	}
	if cur, err := g.run(*repo, gitexec.RevParseAbbrevRef{Rev: "HEAD"}); err != nil {
		return err
	} else if cur != "claude/main" {
		return fmt.Errorf("the checkout is on %q, not claude/main", cur)
	}
	old, err := g.run(*repo, gitexec.RevParse{Rev: "HEAD"})
	if err != nil {
		return err
	}
	if _, err := g.run(*repo, gitexec.Fetch{Remote: "hub"}); err != nil {
		return err
	}
	step(out, "fetched hub")
	ref := landRef(g, *repo, branch)
	step(out, "landing %s", ref)
	_, ancErr := g.run(*repo, gitexec.IsAncestor{Commit: "claude/main", Of: ref})
	ff := ancErr == nil
	for _, c := range landPlan(ref, ff) {
		if _, err := g.run(*repo, c); err != nil {
			for _, r := range landRollback(old, ff) {
				_, _ = g.run(*repo, r)
			}
			return fmt.Errorf("%w (claude/main left at %s)", err, short(old))
		}
	}
	sha, err := g.run(*repo, gitexec.RevParse{Rev: "HEAD"})
	if err != nil {
		return err
	}
	step(out, "claude/main is now %s", short(sha))
	if !*noDeploy {
		c := exec.Command(deployShell, "-NoProfile", "-File", *deploy, "-Rooms", "hub")
		c.Stdout, c.Stderr = os.Stderr, os.Stderr
		if err := c.Run(); err != nil {
			return fmt.Errorf("deploy failed: %w", err)
		}
		step(out, "deployed hub")
	}
	if *item != "" {
		if err := backlogDone(*hub, *item); err != nil {
			return err
		}
		step(out, "%s marked done", *item)
	}
	return nil
}

func short(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}
