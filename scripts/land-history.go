//go:build ignore

// Prepares a branch for pushing to GitHub: every commit in <base>..<branch> gets a one-line, scrubbed message and the
// operator as author and committer, and the branch is moved to the result. The old tip is kept as
// <branch>-before-land-<yyyyMMdd-HHmmss>.
//
//	go run scripts/land-history.go -base origin/main -branch claude/main [-no-sign]
//
// It does what `git filter-branch` with an env filter and msg filter did, in one pass: one rev-list reads the range,
// the new commit objects are written as loose files from Go, and one signing process runs per commit. It needs no
// work tree. It checks its own result and puts the branch back if the check fails. It prints the push command and
// never pushes or fetches.
package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/base/gitexec"
	"github.com/dovholuknf/atrium/internal/base/land"
)

var (
	base   = flag.String("base", "origin/main", "the commits after this are rewritten")
	branch = flag.String("branch", "claude/main", "the local branch to rewrite")
	noSign = flag.Bool("no-sign", false, "do not sign the commits")
)

func main() {
	flag.Parse()
	id := land.Identity{Name: gitOut(gitexec.ConfigGet{Key: "user.name"}), Email: gitOut(gitexec.ConfigGet{Key: "user.email"})}
	if id.Name == "" || id.Email == "" {
		die("set git config user.name and user.email")
	}
	if err := land.CheckGitEmail(id.Email); err != nil {
		die(err.Error())
	}
	oldTip := gitOut(gitexec.RevParseVerify{Quiet: true, Rev: "refs/heads/" + *branch})
	if oldTip == "" {
		die("no local branch " + *branch)
	}
	ref := "refs/heads/" + *branch
	baseSHA := gitOut(gitexec.RevParseVerify{Quiet: true, Rev: *base + "^{commit}"})
	if baseSHA == "" {
		die("cannot resolve " + *base)
	}

	if dups, err := land.Duplicates(".", baseSHA, oldTip); err != nil {
		die(err.Error())
	} else if len(dups) > 0 {
		fmt.Fprintln(os.Stderr, "  "+strings.Join(dups, "\n  "))
		die(fmt.Sprintf("%d commits on %s have a patch already on %s. Landing them again adds copies. "+
			"Run git rebase %s first", len(dups), *branch, *base, *base))
	}

	start := time.Now()
	out, err := gitexec.Run(context.Background(), gitexec.Opts{Visible: true}, revListHeader(baseSHA+".."+oldTip))
	if err != nil {
		die("rev-list: " + err.Error())
	}
	cs, err := land.ParseRevList(out)
	if err != nil {
		die(err.Error())
	}
	if len(cs) == 0 {
		fmt.Printf("nothing to land: %s has no commits over %s\n", *branch, *base)
		return
	}
	var sign land.Signer
	if !*noSign {
		if sign, err = land.GitSigner(func(k string) string { return gitOut(gitexec.ConfigGet{Key: k}) }); err != nil {
			die(err.Error())
		}
	}
	fmt.Printf("%d commits on %s over %s, as %s <%s>, signed: %v\n", len(cs), *branch, *base, id.Name, id.Email, sign != nil)

	common := gitOut(gitexec.RevParseGitDirs{Absolute: true, Common: true})
	if common == "" {
		die("cannot find the git directory")
	}
	newSHA, err := land.Rewrite(cs, id, sign, land.LooseWriter(filepath.Join(common, "objects")))
	if err != nil {
		die(err.Error())
	}
	newTip := newSHA[cs[len(cs)-1].SHA]
	backup := *branch + "-before-land-" + time.Now().Format("20060102-150405")
	git(gitexec.UpdateRef{Ref: "refs/heads/" + backup, New: oldTip, Create: true})
	git(gitexec.UpdateRef{Ref: ref, New: newTip, Old: oldTip})

	// Read the result back and check it.
	out, err = gitexec.Run(context.Background(), gitexec.Opts{Visible: true}, revListHeader(baseSHA+".."+newTip))
	var bad []string
	if err != nil {
		bad = append(bad, "rev-list: "+err.Error())
	} else if got, perr := land.ParseRevList(out); perr != nil {
		bad = append(bad, perr.Error())
	} else {
		bad = land.Verify(got, id, sign != nil)
		if len(got) != len(cs) {
			bad = append(bad, fmt.Sprintf("%d commits after the rewrite, %d before", len(got), len(cs)))
		}
	}
	if len(bad) > 0 {
		git(gitexec.UpdateRef{Ref: ref, New: oldTip, Old: newTip})
		for _, b := range bad {
			fmt.Fprintln(os.Stderr, "  "+b)
		}
		die(fmt.Sprintf("%d problems: %s is back at %s", len(bad), *branch, oldTip[:10]))
	}
	remote, _, found := strings.Cut(*base, "/")
	if !found {
		remote = "origin"
	}
	fmt.Printf("ok: %s is %s, %d commits in %s. The old tip is %s\n", *branch, newTip[:10], len(cs),
		time.Since(start).Round(time.Millisecond), backup)
	fmt.Printf("push: git push %s %s:main\n", remote, *branch)
}

func revListHeader(rng string) gitexec.Op {
	return gitexec.RevList{Reverse: true, TopoOrder: true, Header: true, Revs: []string{rng}}
}

func git(op gitexec.Op) {
	var errb bytes.Buffer
	if _, err := gitexec.Run(context.Background(), gitexec.Opts{Stderr: &errb, Visible: true}, op); err != nil {
		args, _ := gitexec.Argv(gitexec.Opts{}, op)
		die(fmt.Sprintf("git %v: %v: %s", args, err, errb.String()))
	}
}

func gitOut(op gitexec.Op) string {
	out, err := gitexec.Run(context.Background(), gitexec.Opts{Visible: true}, op)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func die(why string) {
	fmt.Fprintln(os.Stderr, "STOP:", why)
	os.Exit(1)
}
