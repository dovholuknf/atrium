package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

// `atrium worktree new|rm|list`: scripts/new-worktree.ps1 and the `git worktree add -b claude/<name>` loop as one verb.
// It runs git in this checkout and never asks the room, so it says nothing about which card uses a worktree.

var worktreeName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

type worktreeEntry struct {
	path, branch string
}

func newWorktree() *cobra.Command {
	var base, root string
	c := &cobra.Command{
		Use:   "worktree",
		Short: "Make, remove and list the claude/<name> worktrees of this repo.",
	}
	add := &cobra.Command{
		Use:   "new <name>",
		Short: "Make branch claude/<name> off the base and a worktree for it, with the CLAUDE.md links.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return worktreeNew(cmd.OutOrStdout(), args[0], base, root)
		},
	}
	add.Flags().StringVar(&base, "base", "claude/main", "the branch to start from")
	add.Flags().StringVar(&root, "root", "", "where worktrees live (default: worktrees/<repo> beside the main checkout)")
	rm := &cobra.Command{
		Use:   "rm <name>",
		Short: "Remove the worktree of claude/<name>, prune, and delete the branch when it is merged.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return worktreeRm(cmd.OutOrStdout(), args[0], base)
		},
	}
	rm.Flags().StringVar(&base, "base", "claude/main", "the branch a merged branch is merged into")
	list := &cobra.Command{
		Use:   "list",
		Short: "List the claude/ worktrees with their branch and ahead/behind of the base.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return worktreeList(cmd.OutOrStdout(), base)
		},
	}
	list.Flags().StringVar(&base, "base", "claude/main", "the branch to count ahead and behind")
	c.AddCommand(add, rm, list)
	return c
}

func worktreeGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	s := strings.TrimSpace(string(out))
	if err != nil {
		return s, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, s)
	}
	return s, nil
}

// listWorktrees returns every worktree, the main checkout first.
func listWorktrees() ([]worktreeEntry, error) {
	out, err := worktreeGit("", "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	var all []worktreeEntry
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			all = append(all, worktreeEntry{path: strings.TrimPrefix(line, "worktree ")})
		case strings.HasPrefix(line, "branch ") && len(all) > 0:
			all[len(all)-1].branch = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
		}
	}
	if len(all) == 0 {
		return nil, errors.New("not in a git checkout")
	}
	return all, nil
}

func worktreeNew(w io.Writer, name, base, root string) error {
	if !worktreeName.MatchString(name) {
		return fmt.Errorf("bad name %q", name)
	}
	all, err := listWorktrees()
	if err != nil {
		return err
	}
	main := all[0].path
	if root == "" {
		root = filepath.Join(filepath.Dir(main), "worktrees", filepath.Base(main))
	}
	branch, wt := "claude/"+name, filepath.Join(root, name)
	if _, err := os.Lstat(wt); err == nil {
		return fmt.Errorf("worktree path already exists: %s", wt)
	}
	if _, err := worktreeGit(main, "show-ref", "--verify", "--quiet", "refs/heads/"+branch); err == nil {
		return fmt.Errorf("branch already exists: %s", branch)
	}
	if _, err := worktreeGit(main, "show-ref", "--verify", "--quiet", "refs/heads/"+base); err != nil {
		return fmt.Errorf("base branch not found: %s", base)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	if _, err := worktreeGit(main, "worktree", "add", "-b", branch, wt, base); err != nil {
		return err
	}
	fmt.Fprintf(w, "worktree %s (branch %s, base %s)\n", wt, branch, base)
	n, err := linkClaudeMD(w, main, wt)
	if err != nil {
		return fmt.Errorf("%w (worktree left at %s)", err, wt)
	}
	if err := excludeClaudeMD(wt); err != nil {
		return err
	}
	fmt.Fprintf(w, "linked %d CLAUDE.md file(s) into %s\n", n, wt)
	return nil
}

// linkClaudeMD mirrors each CLAUDE.md symlink of the main checkout, because git worktree add gives a worker none.
func linkClaudeMD(w io.Writer, main, wt string) (int, error) {
	n := 0
	err := filepath.WalkDir(main, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == "node_modules" || d.Name() == ".mercurius" {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() != "CLAUDE.md" || d.Type()&os.ModeSymlink == 0 {
			return nil
		}
		target, err := os.Readlink(p)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(main, p)
		if err != nil {
			return err
		}
		dest := filepath.Join(wt, rel)
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		_ = os.Remove(dest)
		if err := os.Symlink(target, dest); err != nil {
			return fmt.Errorf("could not link %s -> %s: %w", filepath.ToSlash(rel), target, err)
		}
		fmt.Fprintf(w, "linked %s -> %s\n", filepath.ToSlash(rel), target)
		n++
		return nil
	})
	return n, err
}

// excludeClaudeMD keeps the untracked links out of `git add -A`. info/exclude is in the common dir, so it covers every worktree.
func excludeClaudeMD(wt string) error {
	common, err := worktreeGit(wt, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return err
	}
	exclude := filepath.Join(common, "info", "exclude")
	have, _ := os.ReadFile(exclude)
	for _, l := range strings.Split(string(have), "\n") {
		if strings.TrimSpace(l) == "CLAUDE.md" {
			return nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(exclude), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(exclude, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	prefix := ""
	if len(have) > 0 && !strings.HasSuffix(string(have), "\n") {
		prefix = "\n"
	}
	_, err = f.WriteString(prefix + "CLAUDE.md\n")
	return err
}

func worktreeRm(w io.Writer, name, base string) error {
	if !worktreeName.MatchString(name) {
		return fmt.Errorf("bad name %q", name)
	}
	all, err := listWorktrees()
	if err != nil {
		return err
	}
	main, branch := all[0].path, "claude/"+name
	for _, e := range all[1:] {
		if e.branch != branch {
			continue
		}
		// No --force: a worktree with changes is refused by git and left alone.
		if _, err := worktreeGit(main, "worktree", "remove", e.path); err != nil {
			return err
		}
		fmt.Fprintf(w, "removed worktree %s\n", e.path)
	}
	if _, err := worktreeGit(main, "worktree", "prune"); err != nil {
		return err
	}
	if _, err := worktreeGit(main, "show-ref", "--verify", "--quiet", "refs/heads/"+branch); err != nil {
		return nil
	}
	if _, err := worktreeGit(main, "merge-base", "--is-ancestor", branch, base); err != nil {
		fmt.Fprintf(w, "kept branch %s: not merged into %s\n", branch, base)
		return nil
	}
	if _, err := worktreeGit(main, "branch", "-d", branch); err != nil {
		return err
	}
	fmt.Fprintf(w, "deleted branch %s\n", branch)
	return nil
}

func worktreeList(w io.Writer, base string) error {
	all, err := listWorktrees()
	if err != nil {
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "BRANCH\tAHEAD\tBEHIND\tPATH")
	for _, e := range all[1:] {
		if !strings.HasPrefix(e.branch, "claude/") {
			continue
		}
		ahead, behind := "?", "?"
		if out, err := worktreeGit(all[0].path, "rev-list", "--left-right", "--count", base+"..."+e.branch); err == nil {
			if f := strings.Fields(out); len(f) == 2 {
				behind, ahead = f[0], f[1]
			}
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", e.branch, ahead, behind, filepath.ToSlash(e.path))
	}
	return tw.Flush()
}
