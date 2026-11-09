package gitexec

import (
	"strconv"
	"strings"
)

// Status is `git status --porcelain`. V1 spells it --porcelain=v1. Untracked is "", "all", "normal" or "no".
// Exclude limits it to "." without each of those paths.
type Status struct {
	V1        bool
	Untracked string
	Exclude   []string
}

func (o Status) build(b *builder) {
	b.sub("status")
	if o.V1 {
		b.add("--porcelain=v1")
	} else {
		b.add("--porcelain")
	}
	if o.Untracked != "" {
		b.add("--untracked-files=" + b.oneOf("untracked", o.Untracked, "all", "normal", "no"))
	}
	if len(o.Exclude) > 0 {
		b.add("--", ".")
		for _, p := range b.paths("exclude", o.Exclude) {
			b.add(":(exclude)" + p)
		}
	}
}

// RevListCount is `git rev-list --count`: Revs (after --exclude and --branches), then --not with the remotes, one
// remote's branches, or a glob.
type RevListCount struct {
	Exclude    []string
	Branches   bool
	Revs       []string
	NotRemotes bool
	NotRemote  string
	NotGlob    string
}

func (o RevListCount) build(b *builder) {
	b.sub("rev-list", "--count")
	for _, e := range b.refs("exclude", o.Exclude) {
		b.add("--exclude=" + e)
	}
	b.flag(o.Branches, "--branches")
	b.add(b.refs("rev", o.Revs)...)
	if !o.NotRemotes && o.NotRemote == "" && o.NotGlob == "" {
		return
	}
	b.add("--not")
	b.flag(o.NotRemotes, "--remotes")
	if o.NotRemote != "" {
		b.add("--remotes=" + b.ref("remote", o.NotRemote))
	}
	if o.NotGlob != "" {
		b.add("--glob=" + b.ref("glob", o.NotGlob))
	}
}

// SymbolicRef reads what the symbolic ref Name points at, quietly. Short answers the short name.
type SymbolicRef struct {
	Name  string
	Short bool
}

func (o SymbolicRef) build(b *builder) {
	b.sub("symbolic-ref", "--quiet")
	b.flag(o.Short, "--short")
	b.add(eoo, b.ref("name", o.Name))
}

// BranchDelete is `git branch -D` of one branch.
type BranchDelete struct {
	Branch string
}

func (o BranchDelete) build(b *builder) {
	b.sub("branch", "-D")
	b.add("--", b.ref("branch", o.Branch))
}

// DeleteRef is `git update-ref -d`.
type DeleteRef struct {
	Ref string
}

func (o DeleteRef) build(b *builder) {
	b.sub("update-ref", "-d")
	b.add(eoo, b.ref("ref", o.Ref))
}

// RemoteAdd is `git remote add`.
type RemoteAdd struct {
	Name string
	URL  string
}

func (o RemoteAdd) build(b *builder) {
	b.sub("remote", "add")
	b.add(eoo, b.ref("remote", o.Name), b.ref("url", o.URL))
}

// RemoteRemove is `git remote remove`.
type RemoteRemove struct {
	Name string
}

func (o RemoteRemove) build(b *builder) {
	b.sub("remote", "remove")
	b.add(eoo, b.ref("remote", o.Name))
}

// RemoteGetURL is `git remote get-url`, the push URL when Push, every one when All.
type RemoteGetURL struct {
	Name string
	Push bool
	All  bool
}

func (o RemoteGetURL) build(b *builder) {
	b.sub("remote", "get-url")
	b.flag(o.Push, "--push")
	b.flag(o.All, "--all")
	b.add(eoo, b.ref("remote", o.Name))
}

// RemoteList is `git remote`, one name a line.
type RemoteList struct{}

func (RemoteList) build(b *builder) { b.sub("remote") }

// RevParse is `git rev-parse` of one revision. The rev-parse ops pass no --end-of-options: before git 2.44 rev-parse
// echoes it back as output, and ref already refuses a leading '-'.
type RevParse struct {
	Rev string
}

func (o RevParse) build(b *builder) {
	b.sub("rev-parse")
	b.add(b.ref("rev", o.Rev))
}

// RevParseVerify is `git rev-parse --verify`, silent on a missing one when Quiet.
type RevParseVerify struct {
	Rev   string
	Quiet bool
}

func (o RevParseVerify) build(b *builder) {
	b.sub("rev-parse", "--verify")
	b.flag(o.Quiet, "--quiet")
	b.add(b.ref("rev", o.Rev))
}

// RevParseAbbrevRef is `git rev-parse --abbrev-ref`.
type RevParseAbbrevRef struct {
	Rev string
}

func (o RevParseAbbrevRef) build(b *builder) {
	b.sub("rev-parse", "--abbrev-ref")
	b.add(b.ref("rev", o.Rev))
}

// RevParseShowToplevel is `git rev-parse --show-toplevel`.
type RevParseShowToplevel struct{}

func (RevParseShowToplevel) build(b *builder) { b.sub("rev-parse", "--show-toplevel") }

// RevParseGitPath is `git rev-parse --git-path`.
type RevParseGitPath struct {
	Path string
}

func (o RevParseGitPath) build(b *builder) {
	b.sub("rev-parse", "--git-path")
	b.add(b.ref("path", o.Path))
}

// RevParseGitDirs is `git rev-parse` of the git directory, the common one, or both in that order, absolute when
// Absolute.
type RevParseGitDirs struct {
	Absolute bool
	GitDir   bool
	Common   bool
}

func (o RevParseGitDirs) build(b *builder) {
	b.sub("rev-parse")
	b.flag(o.Absolute, "--path-format=absolute")
	b.flag(o.GitDir, "--git-dir")
	b.flag(o.Common, "--git-common-dir")
	if !o.GitDir && !o.Common {
		b.refuse("dirs", "", "it asks for neither directory")
	}
}

// WorktreeAdd is `git worktree add`: of Commitish, or on a new branch NewBranch (reset to Commitish when it exists
// and Reset), not tracking when NoTrack.
type WorktreeAdd struct {
	Path      string
	Commitish string
	NewBranch string
	Reset     bool
	NoTrack   bool
}

func (o WorktreeAdd) build(b *builder) {
	b.sub("worktree", "add")
	b.flag(o.NoTrack, "--no-track")
	if o.NewBranch != "" {
		flag := "-b"
		if o.Reset {
			flag = "-B"
		}
		b.add(flag, b.ref("branch", o.NewBranch))
	} else if o.Reset {
		b.refuse("branch", "", "a reset needs a branch")
	}
	b.add(eoo, b.path("path", o.Path))
	if o.Commitish != "" {
		b.add(b.ref("commitish", o.Commitish))
	}
}

// WorktreeRemove is `git worktree remove`, with --force Force times.
type WorktreeRemove struct {
	Path  string
	Force int
}

func (o WorktreeRemove) build(b *builder) {
	b.sub("worktree", "remove")
	for range o.Force {
		b.add("--force")
	}
	b.add(eoo, b.path("path", o.Path))
}

// WorktreePrune is `git worktree prune`.
type WorktreePrune struct{}

func (WorktreePrune) build(b *builder) { b.sub("worktree", "prune") }

// WorktreeList is `git worktree list --porcelain`.
type WorktreeList struct{}

func (WorktreeList) build(b *builder) { b.sub("worktree", "list", "--porcelain") }

// ConfigGet is `git config --get`, of the repository's own file alone when Local.
type ConfigGet struct {
	Key   string
	Local bool
}

func (o ConfigGet) build(b *builder) {
	b.sub("config")
	b.flag(o.Local, "--local")
	b.add("--get", eoo, b.ref("key", o.Key))
}

// ConfigSet is `git config <key> <value>`, in the worktree's own file when Worktree.
type ConfigSet struct {
	Key      string
	Value    string
	Worktree bool
}

func (o ConfigSet) build(b *builder) {
	b.sub("config")
	b.flag(o.Worktree, "--worktree")
	b.add(eoo, b.ref("key", o.Key), b.value("value", o.Value))
}

// MergeBase is `git merge-base` of two commits.
type MergeBase struct {
	A string
	B string
}

func (o MergeBase) build(b *builder) {
	b.sub("merge-base")
	b.add(eoo, b.ref("a", o.A), b.ref("b", o.B))
}

// IsAncestor is `git merge-base --is-ancestor`: exit 0 when Commit is an ancestor of Of, 1 when not.
type IsAncestor struct {
	Commit string
	Of     string
}

func (o IsAncestor) build(b *builder) {
	b.sub("merge-base", "--is-ancestor")
	b.add(eoo, b.ref("commit", o.Commit), b.ref("of", o.Of))
}

// ForEachRef is `git for-each-ref` of Patterns.
type ForEachRef struct {
	Format   string
	Count    int
	Sort     string
	Contains string
	PointsAt string
	Patterns []string
}

func (o ForEachRef) build(b *builder) {
	b.sub("for-each-ref")
	if o.Count > 0 {
		b.add("--count=" + strconv.Itoa(o.Count))
	}
	if o.Sort != "" {
		b.add("--sort=" + b.value("sort", o.Sort))
	}
	if o.Format != "" {
		b.add("--format=" + b.value("format", o.Format))
	}
	if o.Contains != "" {
		b.add("--contains=" + b.ref("contains", o.Contains))
	}
	if o.PointsAt != "" {
		b.add("--points-at=" + b.ref("points-at", o.PointsAt))
	}
	b.add(eoo)
	b.add(b.refs("pattern", o.Patterns)...)
}

// Fetch is `git fetch` of Refspecs from Remote, a name, URL or path.
type Fetch struct {
	Remote   string
	Refspecs []string
	NoTags   bool
	Prune    bool
	Quiet    bool
}

func (o Fetch) build(b *builder) {
	b.sub("fetch")
	b.flag(o.Quiet, "--quiet")
	b.flag(o.NoTags, "--no-tags")
	b.flag(o.Prune, "--prune")
	b.add("--", b.ref("remote", o.Remote))
	b.add(b.refs("refspec", o.Refspecs)...)
}

// CheckRefFormat is `git check-ref-format`, of a branch name when Branch.
type CheckRefFormat struct {
	Name   string
	Branch bool
}

func (o CheckRefFormat) build(b *builder) {
	b.sub("check-ref-format")
	b.flag(o.Branch, "--branch")
	b.add(b.ref("name", o.Name))
}

// Diff is `git diff` of Revs: the patch (binary when Binary), or --numstat, --stat or --name-only, limited to
// DiffFilter.
type Diff struct {
	Revs       []string
	Binary     bool
	Numstat    bool
	Stat       bool
	NameOnly   bool
	DiffFilter string
}

func (o Diff) build(b *builder) {
	b.sub("diff")
	b.flag(o.Binary, "--binary")
	b.flag(o.Numstat, "--numstat")
	b.flag(o.Stat, "--stat")
	b.flag(o.NameOnly, "--name-only")
	if o.DiffFilter != "" {
		b.add("--diff-filter=" + b.value("diff-filter", o.DiffFilter))
	}
	b.add(eoo)
	b.add(b.refs("rev", o.Revs)...)
}

// DiffNoIndex is `git diff --no-index --no-color` of two paths. Git exits 1 when they differ.
type DiffNoIndex struct {
	A string
	B string
}

func (o DiffNoIndex) build(b *builder) {
	b.sub("diff", "--no-index", "--no-color")
	b.add("--", b.path("a", o.A), b.path("b", o.B))
}

// Apply is `git apply` of the patch on stdin.
type Apply struct {
	Binary     bool
	Whitespace string
}

func (o Apply) build(b *builder) {
	b.sub("apply")
	b.flag(o.Binary, "--binary")
	if o.Whitespace != "" {
		b.add("--whitespace=" + b.oneOf("whitespace", o.Whitespace, "nowarn", "warn", "fix", "error", "error-all"))
	}
}

// LsFiles is `git ls-files`: the untracked files (not ignored when ExcludeStandard), NUL separated when Z, failing on
// a path git does not track when ErrorUnmatch.
type LsFiles struct {
	Others          bool
	ExcludeStandard bool
	Z               bool
	ErrorUnmatch    bool
	Paths           []string
}

func (o LsFiles) build(b *builder) {
	b.sub("ls-files")
	b.flag(o.Others, "--others")
	b.flag(o.ExcludeStandard, "--exclude-standard")
	b.flag(o.Z, "-z")
	b.flag(o.ErrorUnmatch, "--error-unmatch")
	if len(o.Paths) > 0 {
		b.add("--")
		b.add(b.paths("path", o.Paths)...)
	}
}

// ShowFile is `git show <rev>:<path>`, a file as it is in a commit.
type ShowFile struct {
	Rev  string
	Path string
}

func (o ShowFile) build(b *builder) {
	b.sub("show")
	b.add(eoo, b.ref("rev", o.Rev)+":"+b.path("path", o.Path))
}

// CatFileExists is `git cat-file -e`: exit 0 when Object is in the repository.
type CatFileExists struct {
	Object string
}

func (o CatFileExists) build(b *builder) {
	b.sub("cat-file", "-e")
	b.add(eoo, b.ref("object", o.Object))
}

// LsRemote is `git ls-remote` of Refs on Remote.
type LsRemote struct {
	Remote string
	Refs   []string
}

func (o LsRemote) build(b *builder) {
	b.sub("ls-remote")
	b.add("--", b.ref("remote", o.Remote))
	b.add(b.refs("ref", o.Refs)...)
}

// Clone is `git clone` of URL into Dir, of Branch alone and Depth commits deep when they are set.
type Clone struct {
	URL    string
	Dir    string
	Branch string
	Depth  int
}

func (o Clone) build(b *builder) {
	b.sub("clone")
	if o.Depth > 0 {
		b.add("--depth", strconv.Itoa(o.Depth))
	}
	if o.Branch != "" {
		b.add("--branch", b.ref("branch", o.Branch))
	}
	b.add("--", b.ref("url", o.URL), b.path("dir", o.Dir))
}

// Log is `git log` of Revs, at most MaxCount, one line each when Oneline, else in Format.
type Log struct {
	Revs     []string
	MaxCount int
	Oneline  bool
	Format   string
}

func (o Log) build(b *builder) {
	b.sub("log")
	if o.MaxCount > 0 {
		b.add("-n", strconv.Itoa(o.MaxCount))
	}
	b.flag(o.Oneline, "--oneline")
	if o.Format != "" {
		b.add("--format=" + b.value("format", o.Format))
	}
	b.add(eoo)
	b.add(b.refs("rev", o.Revs)...)
}

// Commit is `git commit -a --amend --no-edit` as far as each is set.
type Commit struct {
	All    bool
	Amend  bool
	NoEdit bool
}

func (o Commit) build(b *builder) {
	b.sub("commit")
	b.flag(o.All, "-a")
	b.flag(o.Amend, "--amend")
	b.flag(o.NoEdit, "--no-edit")
}

// SwitchReset is `git switch -C`: Branch made or reset at Start, and checked out.
type SwitchReset struct {
	Branch string
	Start  string
}

func (o SwitchReset) build(b *builder) {
	b.sub("switch")
	b.add("-C", b.ref("branch", o.Branch), eoo, b.ref("start", o.Start))
}

// GrepCount is `git grep -c` of Pattern, a fixed string when Fixed, in Rev under Paths.
type GrepCount struct {
	Pattern string
	Fixed   bool
	Rev     string
	Paths   []string
}

func (o GrepCount) build(b *builder) {
	b.sub("grep", "-c")
	b.flag(o.Fixed, "-F")
	if o.Pattern == "" {
		b.refuse("pattern", "", "it is empty")
	}
	b.add("-e", b.value("pattern", o.Pattern), eoo, b.ref("rev", o.Rev))
	if len(o.Paths) > 0 {
		b.add("--")
		b.add(b.paths("path", o.Paths)...)
	}
}

// CherryPick is `git cherry-pick` of Revs.
type CherryPick struct {
	Revs []string
}

func (o CherryPick) build(b *builder) {
	b.sub("cherry-pick")
	if len(o.Revs) == 0 {
		b.refuse("rev", "", "it is empty")
	}
	b.add(eoo)
	b.add(b.refs("rev", o.Revs)...)
}

// CherryPickAbort is `git cherry-pick --abort`.
type CherryPickAbort struct{}

func (CherryPickAbort) build(b *builder) { b.sub("cherry-pick", "--abort") }

// ListCmds is `git --list-cmds=<groups>`.
type ListCmds struct {
	Groups []string
}

func (o ListCmds) build(b *builder) {
	for _, g := range o.Groups {
		b.oneOf("group", g, "builtins", "main", "others", "alias", "nohelpers", "config", "list-mainporcelain")
	}
	b.sub("--list-cmds=" + strings.Join(o.Groups, ","))
}
