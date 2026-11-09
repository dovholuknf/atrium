package gitexec

import "strconv"

// Version is `git --version`.
type Version struct{}

func (Version) build(b *builder) { b.sub("--version") }

// RevList is `git rev-list` of Revs under Paths, at most MaxCount, in Format, committed after After and before
// Before (Unix seconds) when they are set. The rest are git's flags of the same name.
type RevList struct {
	Revs         []string
	Paths        []string
	NoMerges     bool
	Reverse      bool
	FirstParent  bool
	AncestryPath bool
	Parents      bool
	All          bool
	Count        bool
	MaxCount     int
	Format       string
	After        int64
	Before       int64
}

func (o RevList) build(b *builder) {
	b.sub("rev-list")
	b.flag(o.NoMerges, "--no-merges")
	b.flag(o.Reverse, "--reverse")
	b.flag(o.FirstParent, "--first-parent")
	b.flag(o.AncestryPath, "--ancestry-path")
	b.flag(o.Parents, "--parents")
	b.flag(o.All, "--all")
	b.flag(o.Count, "--count")
	if o.MaxCount > 0 {
		b.add("--max-count=" + strconv.Itoa(o.MaxCount))
	}
	if o.Format != "" {
		b.add("--format=" + b.value("format", o.Format))
	}
	if o.After != 0 {
		b.add("--after=" + strconv.FormatInt(o.After, 10))
	}
	if o.Before != 0 {
		b.add("--before=" + strconv.FormatInt(o.Before, 10))
	}
	b.add(eoo)
	b.add(b.refs("rev", o.Revs)...)
	if len(o.Paths) > 0 {
		b.add("--")
		b.add(b.paths("path", o.Paths)...)
	}
}

// RevParseRevs is `git rev-parse` of several revisions at once, one sha a line.
type RevParseRevs struct {
	Revs  []string
	Quiet bool
}

func (o RevParseRevs) build(b *builder) {
	b.sub("rev-parse")
	b.flag(o.Quiet, "--quiet")
	if len(o.Revs) == 0 {
		b.refuse("rev", "", "it is empty")
	}
	b.add(b.refs("rev", o.Revs)...)
}

// RevParseIsBare is `git rev-parse --is-bare-repository`.
type RevParseIsBare struct{}

func (RevParseIsBare) build(b *builder) { b.sub("rev-parse", "--is-bare-repository") }

// RevParseIsInsideWorkTree is `git rev-parse --is-inside-work-tree`.
type RevParseIsInsideWorkTree struct{}

func (RevParseIsInsideWorkTree) build(b *builder) { b.sub("rev-parse", "--is-inside-work-tree") }

// LsTree is `git ls-tree` of Tree under Paths, recursive when Recursive, names alone when NameOnly.
type LsTree struct {
	Tree      string
	Paths     []string
	Recursive bool
	NameOnly  bool
}

func (o LsTree) build(b *builder) {
	b.sub("ls-tree")
	b.flag(o.Recursive, "-r")
	b.flag(o.NameOnly, "--name-only")
	b.add(eoo, b.ref("tree", o.Tree))
	if len(o.Paths) > 0 {
		b.add("--")
		b.add(b.paths("path", o.Paths)...)
	}
}

// DiffTree is `git diff-tree` of one Commit. The rest are git's flags of the same name, Recursive being -r.
type DiffTree struct {
	Commit     string
	NoCommitID bool
	NameOnly   bool
	Recursive  bool
	Root       bool
	Z          bool
}

func (o DiffTree) build(b *builder) {
	b.sub("diff-tree")
	b.flag(o.NoCommitID, "--no-commit-id")
	b.flag(o.NameOnly, "--name-only")
	b.flag(o.Recursive, "-r")
	b.flag(o.Root, "--root")
	b.flag(o.Z, "-z")
	b.add(eoo, b.ref("commit", o.Commit))
}

// CatFilePretty is `git cat-file -p` of Object.
type CatFilePretty struct {
	Object string
}

func (o CatFilePretty) build(b *builder) {
	b.sub("cat-file", "-p")
	b.add(eoo, b.ref("object", o.Object))
}

// CatFileBatch is `git cat-file --batch`, of the objects named on stdin.
type CatFileBatch struct{}

func (CatFileBatch) build(b *builder) { b.sub("cat-file", "--batch") }

// Cherry is `git cherry` of Head against Upstream.
type Cherry struct {
	Upstream string
	Head     string
}

func (o Cherry) build(b *builder) {
	b.sub("cherry")
	b.add(eoo, b.ref("upstream", o.Upstream), b.ref("head", o.Head))
}

// UpdateRef is `git update-ref` of Ref to New, only while Ref is at Old when Old is set, logged as Message.
type UpdateRef struct {
	Message string
	Ref     string
	New     string
	Old     string
}

func (o UpdateRef) build(b *builder) {
	b.sub("update-ref")
	if o.Message != "" {
		b.add("-m", b.value("message", o.Message))
	}
	b.add(eoo, b.ref("ref", o.Ref), b.ref("new", o.New))
	if o.Old != "" {
		b.add(b.ref("old", o.Old))
	}
}

// UpdateRefStdin is `git update-ref --stdin`, the transaction on stdin, logged as Message.
type UpdateRefStdin struct {
	Message string
}

func (o UpdateRefStdin) build(b *builder) {
	b.sub("update-ref")
	if o.Message != "" {
		b.add("-m", b.value("message", o.Message))
	}
	b.add("--stdin")
}

// Init is `git init` of Dir, or of the directory git runs in when Dir is empty. NoTemplate is --template=.
type Init struct {
	Dir           string
	Bare          bool
	Quiet         bool
	NoTemplate    bool
	InitialBranch string
	RefFormat     string
}

func (o Init) build(b *builder) {
	b.sub("init")
	b.flag(o.Quiet, "--quiet")
	b.flag(o.Bare, "--bare")
	b.flag(o.NoTemplate, "--template=")
	if o.InitialBranch != "" {
		b.add("--initial-branch=" + b.ref("initial-branch", o.InitialBranch))
	}
	if o.RefFormat != "" {
		b.add("--ref-format=" + b.oneOf("ref-format", o.RefFormat, "files", "reftable"))
	}
	if o.Dir != "" {
		b.add(eoo, b.path("dir", o.Dir))
	}
}

// SymbolicRefSet is `git symbolic-ref` that points Name at Target.
type SymbolicRefSet struct {
	Name   string
	Target string
}

func (o SymbolicRefSet) build(b *builder) {
	b.sub("symbolic-ref")
	b.add(eoo, b.ref("name", o.Name), b.ref("target", o.Target))
}

// MergeBaseOctopus is `git merge-base --octopus` of Commits.
type MergeBaseOctopus struct {
	Commits []string
}

func (o MergeBaseOctopus) build(b *builder) {
	b.sub("merge-base", "--octopus")
	if len(o.Commits) == 0 {
		b.refuse("commit", "", "it is empty")
	}
	b.add(eoo)
	b.add(b.refs("commit", o.Commits)...)
}

// MergeTree is `git merge-tree --write-tree --name-only --no-messages` of two commits: the merged tree, then the
// paths that conflict.
type MergeTree struct {
	Branch1 string
	Branch2 string
}

func (o MergeTree) build(b *builder) {
	b.sub("merge-tree", "--write-tree", "--name-only", "--no-messages")
	b.add(eoo, b.ref("branch1", o.Branch1), b.ref("branch2", o.Branch2))
}

// CommitTree is `git commit-tree` of Tree with Parents and Message. It takes no --end-of-options before git 2.25,
// and ref already refuses a leading '-'.
type CommitTree struct {
	Tree      string
	Parents   []string
	Message   string
	NoGPGSign bool
}

func (o CommitTree) build(b *builder) {
	b.sub("commit-tree")
	b.flag(o.NoGPGSign, "--no-gpg-sign")
	b.add(b.ref("tree", o.Tree))
	for _, p := range b.refs("parent", o.Parents) {
		b.add("-p", p)
	}
	b.add("-m", b.value("message", o.Message))
}

// ConfigGetAll is `git config --get-all`, of the repository's own file alone when Local.
type ConfigGetAll struct {
	Key   string
	Local bool
}

func (o ConfigGetAll) build(b *builder) {
	b.sub("config")
	b.flag(o.Local, "--local")
	b.add("--get-all", eoo, b.ref("key", o.Key))
}

// ConfigUnsetAll is `git config --unset-all` of Key, of its values matching ValueRegex alone when that is set.
type ConfigUnsetAll struct {
	Key        string
	ValueRegex string
	Local      bool
}

func (o ConfigUnsetAll) build(b *builder) {
	b.sub("config")
	b.flag(o.Local, "--local")
	b.add("--unset-all", eoo, b.ref("key", o.Key))
	if o.ValueRegex != "" {
		b.add(b.value("value-regex", o.ValueRegex))
	}
}

// RemoteSetURL is `git remote set-url`.
type RemoteSetURL struct {
	Name string
	URL  string
}

func (o RemoteSetURL) build(b *builder) {
	b.sub("remote", "set-url")
	b.add(eoo, b.ref("remote", o.Name), b.ref("url", o.URL))
}

// MergeFFOnly is `git merge --ff-only` of Commit.
type MergeFFOnly struct {
	Commit string
	Quiet  bool
}

func (o MergeFFOnly) build(b *builder) {
	b.sub("merge")
	b.flag(o.Quiet, "--quiet")
	b.add("--ff-only", eoo, b.ref("commit", o.Commit))
}

// BranchForce is `git branch -f`: Branch made or moved to Start.
type BranchForce struct {
	Branch string
	Start  string
}

func (o BranchForce) build(b *builder) {
	b.sub("branch", "-f")
	b.add("--", b.ref("branch", o.Branch), b.ref("start", o.Start))
}

// ResetKeep is `git reset --keep` to Commit.
type ResetKeep struct {
	Commit string
	Quiet  bool
}

func (o ResetKeep) build(b *builder) {
	b.sub("reset", "--keep")
	b.flag(o.Quiet, "--quiet")
	b.add(eoo, b.ref("commit", o.Commit))
}

// Push is `git push` of Refspecs to Remote, a name or URL. The rest are git's flags of the same name.
type Push struct {
	Remote              string
	Refspecs            []string
	Porcelain           bool
	NoVerify            bool
	NoRecurseSubmodules bool
}

func (o Push) build(b *builder) {
	b.sub("push")
	b.flag(o.Porcelain, "--porcelain")
	b.flag(o.NoVerify, "--no-verify")
	b.flag(o.NoRecurseSubmodules, "--no-recurse-submodules")
	b.add("--", b.ref("remote", o.Remote))
	b.add(b.refs("refspec", o.Refspecs)...)
}

// CheckoutDetach is `git checkout --detach` of Commit, throwing away local changes when Force.
type CheckoutDetach struct {
	Commit string
	Force  bool
	Quiet  bool
}

func (o CheckoutDetach) build(b *builder) {
	b.sub("checkout")
	b.flag(o.Quiet, "--quiet")
	b.add("--detach")
	b.flag(o.Force, "--force")
	b.add(eoo, b.ref("commit", o.Commit))
}

// Clean is `git clean`, with -f Force times, directories too when Dirs, ignored files too when Ignored.
type Clean struct {
	Force   int
	Dirs    bool
	Ignored bool
	Quiet   bool
}

func (o Clean) build(b *builder) {
	b.sub("clean")
	for range o.Force {
		b.add("-f")
	}
	b.flag(o.Dirs, "-d")
	b.flag(o.Ignored, "-x")
	b.flag(o.Quiet, "-q")
}

// PatchID is `git patch-id` of the patch on stdin, --stable when Stable.
type PatchID struct {
	Stable bool
}

func (o PatchID) build(b *builder) {
	b.sub("patch-id")
	b.flag(o.Stable, "--stable")
}

// AddAll is `git add -A`.
type AddAll struct{}

func (AddAll) build(b *builder) { b.sub("add", "-A") }
