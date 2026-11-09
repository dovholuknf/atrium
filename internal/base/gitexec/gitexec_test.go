package gitexec

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

const nul = "\x00"

type opCase struct {
	name string
	op   Op
	want string
	bad  map[string]func(v string) Op
}

func opCases() []opCase {
	return []opCase{
		{"status", Status{}, "status --porcelain", nil},
		{"status v1", Status{V1: true, Untracked: "all", Exclude: []string{"node_modules"}},
			"status --porcelain=v1 --untracked-files=all -- . :(exclude)node_modules", map[string]func(string) Op{
				"exclude": func(v string) Op { return Status{Exclude: []string{"a" + nul}} },
			}},
		{"rev-list", RevListCount{Exclude: []string{"refs/heads/x"}, Branches: true, Revs: []string{"a..b"}, NotRemote: "hub"},
			"rev-list --count --exclude=refs/heads/x --branches a..b --not --remotes=hub", map[string]func(string) Op{
				"exclude": func(v string) Op { return RevListCount{Exclude: []string{v}} },
				"rev":     func(v string) Op { return RevListCount{Revs: []string{v}} },
				"remote":  func(v string) Op { return RevListCount{Revs: []string{"a"}, NotRemote: v} },
				"glob":    func(v string) Op { return RevListCount{Revs: []string{"a"}, NotGlob: v} },
			}},
		{"rev-list remotes", RevListCount{Revs: []string{"a"}, NotRemotes: true, NotGlob: "refs/x/*"},
			"rev-list --count a --not --remotes --glob=refs/x/*", nil},
		{"symbolic-ref", SymbolicRef{Name: "HEAD", Short: true}, "symbolic-ref --quiet --short --end-of-options HEAD",
			map[string]func(string) Op{"name": func(v string) Op { return SymbolicRef{Name: v} }}},
		{"branch -D", BranchDelete{Branch: "claude/x"}, "branch -D -- claude/x",
			map[string]func(string) Op{"branch": func(v string) Op { return BranchDelete{Branch: v} }}},
		{"update-ref -d", DeleteRef{Ref: "refs/heads/x"}, "update-ref -d --end-of-options refs/heads/x",
			map[string]func(string) Op{"ref": func(v string) Op { return DeleteRef{Ref: v} }}},
		{"remote add", RemoteAdd{Name: "hub", URL: "https://h/r.git"}, "remote add --end-of-options hub https://h/r.git",
			map[string]func(string) Op{
				"remote": func(v string) Op { return RemoteAdd{Name: v, URL: "u"} },
				"url":    func(v string) Op { return RemoteAdd{Name: "hub", URL: v} },
			}},
		{"remote remove", RemoteRemove{Name: "hub"}, "remote remove --end-of-options hub",
			map[string]func(string) Op{"remote": func(v string) Op { return RemoteRemove{Name: v} }}},
		{"remote get-url", RemoteGetURL{Name: "origin", Push: true, All: true},
			"remote get-url --push --all --end-of-options origin",
			map[string]func(string) Op{"remote": func(v string) Op { return RemoteGetURL{Name: v} }}},
		{"remote", RemoteList{}, "remote", nil},
		{"rev-parse", RevParse{Rev: "HEAD"}, "rev-parse HEAD",
			map[string]func(string) Op{"rev": func(v string) Op { return RevParse{Rev: v} }}},
		{"rev-parse --verify", RevParseVerify{Rev: "HEAD^{commit}", Quiet: true},
			"rev-parse --verify --quiet HEAD^{commit}",
			map[string]func(string) Op{"rev": func(v string) Op { return RevParseVerify{Rev: v} }}},
		{"rev-parse --abbrev-ref", RevParseAbbrevRef{Rev: "HEAD"}, "rev-parse --abbrev-ref HEAD",
			map[string]func(string) Op{"rev": func(v string) Op { return RevParseAbbrevRef{Rev: v} }}},
		{"rev-parse --show-toplevel", RevParseShowToplevel{}, "rev-parse --show-toplevel", nil},
		{"rev-parse --git-path", RevParseGitPath{Path: "hooks"}, "rev-parse --git-path hooks",
			map[string]func(string) Op{"path": func(v string) Op { return RevParseGitPath{Path: v} }}},
		{"rev-parse dirs", RevParseGitDirs{Absolute: true, GitDir: true, Common: true},
			"rev-parse --path-format=absolute --git-dir --git-common-dir", nil},
		{"worktree add -b", WorktreeAdd{Path: "w", NewBranch: "claude/x", Commitish: "claude/main", NoTrack: true},
			"worktree add --no-track -b claude/x --end-of-options w claude/main", map[string]func(string) Op{
				"branch":    func(v string) Op { return WorktreeAdd{Path: "w", NewBranch: v} },
				"commitish": func(v string) Op { return WorktreeAdd{Path: "w", Commitish: v} },
				"path":      func(v string) Op { return WorktreeAdd{Path: "a\x00b"} },
			}},
		{"worktree add -B", WorktreeAdd{Path: "-w", NewBranch: "b", Reset: true, Commitish: "c"},
			"worktree add -B b --end-of-options -w c", nil},
		{"worktree remove", WorktreeRemove{Path: "w", Force: 2}, "worktree remove --force --force --end-of-options w",
			map[string]func(string) Op{"path": func(v string) Op { return WorktreeRemove{Path: "a\x00b"} }}},
		{"worktree prune", WorktreePrune{}, "worktree prune", nil},
		{"worktree list", WorktreeList{}, "worktree list --porcelain", nil},
		{"config --get", ConfigGet{Key: "remote.hub.url", Local: true}, "config --local --get --end-of-options remote.hub.url",
			map[string]func(string) Op{"key": func(v string) Op { return ConfigGet{Key: v} }}},
		{"config set", ConfigSet{Key: "core.x", Value: "-v", Worktree: true}, "config --worktree --end-of-options core.x -v",
			map[string]func(string) Op{"key": func(v string) Op { return ConfigSet{Key: v, Value: "v"} }}},
		{"merge-base", MergeBase{A: "HEAD", B: "claude/main"}, "merge-base --end-of-options HEAD claude/main",
			map[string]func(string) Op{
				"a": func(v string) Op { return MergeBase{A: v, B: "b"} },
				"b": func(v string) Op { return MergeBase{A: "a", B: v} },
			}},
		{"is-ancestor", IsAncestor{Commit: "abc", Of: "main"}, "merge-base --is-ancestor --end-of-options abc main",
			map[string]func(string) Op{
				"commit": func(v string) Op { return IsAncestor{Commit: v, Of: "b"} },
				"of":     func(v string) Op { return IsAncestor{Commit: "a", Of: v} },
			}},
		{"for-each-ref", ForEachRef{Format: "%(refname)", Count: 3, Sort: "-committerdate", Contains: "abc", PointsAt: "def",
			Patterns: []string{"refs/heads"}},
			"for-each-ref --count=3 --sort=-committerdate --format=%(refname) --contains=abc --points-at=def --end-of-options refs/heads",
			map[string]func(string) Op{
				"contains":  func(v string) Op { return ForEachRef{Contains: v} },
				"points-at": func(v string) Op { return ForEachRef{PointsAt: v} },
				"pattern":   func(v string) Op { return ForEachRef{Patterns: []string{v}} },
			}},
		{"fetch", Fetch{Remote: "hub", Refspecs: []string{"+refs/heads/*:refs/remotes/hub/*"}, NoTags: true, Prune: true, Quiet: true},
			"fetch --quiet --no-tags --prune -- hub +refs/heads/*:refs/remotes/hub/*", map[string]func(string) Op{
				"remote":  func(v string) Op { return Fetch{Remote: v} },
				"refspec": func(v string) Op { return Fetch{Remote: "hub", Refspecs: []string{v}} },
			}},
		{"check-ref-format", CheckRefFormat{Name: "claude/x", Branch: true}, "check-ref-format --branch claude/x",
			map[string]func(string) Op{"name": func(v string) Op { return CheckRefFormat{Name: v} }}},
		{"diff", Diff{Revs: []string{"a", "b"}, Binary: true, NameOnly: true, DiffFilter: "AM"},
			"diff --binary --name-only --diff-filter=AM --end-of-options a b",
			map[string]func(string) Op{"rev": func(v string) Op { return Diff{Revs: []string{v}} }}},
		{"diff --no-index", DiffNoIndex{A: "-a", B: "b"}, "diff --no-index --no-color -- -a b",
			map[string]func(string) Op{"a": func(v string) Op { return DiffNoIndex{A: "x\x00", B: "b"} }}},
		{"apply", Apply{Binary: true, Whitespace: "nowarn"}, "apply --binary --whitespace=nowarn", nil},
		{"apply a file", Apply{Binary: true, Patch: "-p.patch"}, "apply --binary -- -p.patch",
			map[string]func(string) Op{"patch": func(v string) Op { return Apply{Patch: v + nul} }}},
		{"diff to a file", Diff{Revs: []string{"HEAD"}, Binary: true, NoRenames: true, Output: "-o.patch"},
			"diff --binary --no-renames --output=-o.patch --end-of-options HEAD",
			map[string]func(string) Op{"output": func(v string) Op { return Diff{Output: "x" + nul} }}},
		{"ls-files", LsFiles{Others: true, ExcludeStandard: true, Z: true, ErrorUnmatch: true, Paths: []string{"-f"}},
			"ls-files --others --exclude-standard -z --error-unmatch -- -f",
			map[string]func(string) Op{"path": func(v string) Op { return LsFiles{Paths: []string{"a\x00"}} }}},
		{"show", ShowFile{Rev: "HEAD", Path: "a/b.go"}, "show --end-of-options HEAD:a/b.go",
			map[string]func(string) Op{"rev": func(v string) Op { return ShowFile{Rev: v, Path: "p"} }}},
		{"cat-file -e", CatFileExists{Object: "abc^{commit}"}, "cat-file -e --end-of-options abc^{commit}",
			map[string]func(string) Op{"object": func(v string) Op { return CatFileExists{Object: v} }}},
		{"ls-remote", LsRemote{Remote: "origin", Refs: []string{"refs/heads/main"}}, "ls-remote -- origin refs/heads/main",
			map[string]func(string) Op{
				"remote": func(v string) Op { return LsRemote{Remote: v} },
				"ref":    func(v string) Op { return LsRemote{Remote: "o", Refs: []string{v}} },
			}},
		{"clone", Clone{URL: "https://h/r.git", Dir: "-d", Branch: "main", Depth: 1},
			"clone --depth 1 --branch main -- https://h/r.git -d", map[string]func(string) Op{
				"url":    func(v string) Op { return Clone{URL: v, Dir: "d"} },
				"branch": func(v string) Op { return Clone{URL: "u", Dir: "d", Branch: v} },
			}},
		{"log", Log{Revs: []string{"a..b"}, MaxCount: 1, Oneline: true, Format: "%cI"},
			"log -n 1 --oneline --format=%cI --end-of-options a..b",
			map[string]func(string) Op{"rev": func(v string) Op { return Log{Revs: []string{v}} }}},
		{"commit", Commit{All: true, Amend: true, NoEdit: true}, "commit -a --amend --no-edit", nil},
		{"switch -C", SwitchReset{Branch: "b", Start: "origin/b"}, "switch -C b --end-of-options origin/b",
			map[string]func(string) Op{
				"branch": func(v string) Op { return SwitchReset{Branch: v, Start: "s"} },
				"start":  func(v string) Op { return SwitchReset{Branch: "b", Start: v} },
			}},
		{"grep -c", GrepCount{Pattern: "-x", Fixed: true, Rev: "HEAD", Paths: []string{"docs"}},
			"grep -c -F -e -x --end-of-options HEAD -- docs",
			map[string]func(string) Op{"rev": func(v string) Op { return GrepCount{Pattern: "p", Rev: v} }}},
		{"cherry-pick", CherryPick{Revs: []string{"abc"}}, "cherry-pick --end-of-options abc",
			map[string]func(string) Op{"rev": func(v string) Op { return CherryPick{Revs: []string{v}} }}},
		{"cherry-pick --abort", CherryPickAbort{}, "cherry-pick --abort", nil},
		{"list-cmds", ListCmds{Groups: []string{"builtins", "alias"}}, "--list-cmds=builtins,alias", nil},
		{"update-ref -d old", DeleteRef{Ref: "r", Old: "o"}, "update-ref -d --end-of-options r o",
			map[string]func(string) Op{"old": func(v string) Op { return DeleteRef{Ref: "r", Old: v} }}},
		{"config --get scopes", ConfigGet{Key: "k", Global: true, Worktree: true},
			"config --global --worktree --get --end-of-options k", nil},
		{"fetch prune", Fetch{NoWriteFetchHead: true, Prune: true}, "fetch --no-write-fetch-head --prune", nil},
		{"fetch shallow", Fetch{Remote: "origin", Refspecs: []string{"r"}, Quiet: true, Depth: 1, Filter: "blob:none"},
			"fetch --quiet --depth 1 --filter=blob:none -- origin r", nil},
		{"ls-remote heads", LsRemote{Remote: "o", Heads: true, Symref: true}, "ls-remote --heads --symref -- o", nil},
		{"clone no tags", Clone{URL: "u", Dir: "d", NoTags: true}, "clone --no-tags -- u d", nil},
		{"log flags", Log{Revs: []string{"a"}, Paths: []string{"-p"}, EmptyFormat: true, NoMerges: true, Merges: true,
			RemergeDiff: true, NoRenames: true, NameOnly: true, NameStatus: true, FullDiff: true, Raw: true, Patch: true,
			All: true, AncestryPath: true, FirstParent: true, Reverse: true, LeftRight: true, DateOrder: true, NoWalk: true,
			NoWalkUnsorted: true, ExtendedRegexp: true, Grep: "^x", DiffFilter: "A", Stdin: true},
			"log --format= --no-merges --merges --remerge-diff --no-renames --name-only --name-status --full-diff --raw -p " +
				"--all --ancestry-path --first-parent --reverse --left-right --date-order --no-walk --no-walk=unsorted " +
				"--extended-regexp --grep=^x --diff-filter=A --stdin --end-of-options a -- -p",
			map[string]func(string) Op{"path": func(v string) Op { return Log{Paths: []string{"a" + nul}} }}},
		{"diff flags", Diff{Revs: []string{"a"}, Paths: []string{"-p"}, NameStatus: true, Z: true, Relative: true,
			FindRenames: true, NoExtDiff: true, NoTextconv: true, NoColor: true},
			"diff --name-status -z --relative -M --no-ext-diff --no-textconv --no-color --end-of-options a -- -p", nil},
		{"show diff", ShowDiff{Rev: "abc", Paths: []string{"p"}, Numstat: true, NameStatus: true, Z: true, Relative: true,
			FindRenames: true, NoExtDiff: true, NoTextconv: true, NoColor: true},
			"show --format= --numstat --name-status -z --relative -M --no-ext-diff --no-textconv --no-color " +
				"--end-of-options abc -- p",
			map[string]func(string) Op{"rev": func(v string) Op { return ShowDiff{Rev: v} }}},
		{"commit message", Commit{NoVerify: true, Quiet: true, Message: "-m"}, "commit --no-verify --quiet -m -m", nil},
		{"version", Version{}, "--version", nil},
		{"rev-list", RevList{Revs: []string{"a..b"}, Paths: []string{"p"}, NoMerges: true, Reverse: true,
			FirstParent: true, AncestryPath: true, Parents: true, All: true, Count: true, MaxCount: 5, Format: "%at",
			After: 10, Before: 20},
			"rev-list --no-merges --reverse --first-parent --ancestry-path --parents --all --count --max-count=5 " +
				"--format=%at --after=10 --before=20 --end-of-options a..b -- p",
			map[string]func(string) Op{
				"rev":  func(v string) Op { return RevList{Revs: []string{v}} },
				"path": func(v string) Op { return RevList{Revs: []string{"a"}, Paths: []string{"a" + nul}} },
			}},
		{"rev-parse revs", RevParseRevs{Revs: []string{"a", "b"}, Quiet: true}, "rev-parse --quiet a b",
			map[string]func(string) Op{"rev": func(v string) Op { return RevParseRevs{Revs: []string{"a", v}} }}},
		{"rev-parse bare", RevParseIsBare{}, "rev-parse --is-bare-repository", nil},
		{"rev-parse inside", RevParseIsInsideWorkTree{}, "rev-parse --is-inside-work-tree", nil},
		{"ls-tree", LsTree{Recursive: true, NameOnly: true, Tree: "HEAD", Paths: []string{"-p"}},
			"ls-tree -r --name-only --end-of-options HEAD -- -p",
			map[string]func(string) Op{"tree": func(v string) Op { return LsTree{Tree: v} }}},
		{"diff-tree", DiffTree{NoCommitID: true, NameOnly: true, Recursive: true, Root: true, Z: true, Commit: "abc"},
			"diff-tree --no-commit-id --name-only -r --root -z --end-of-options abc",
			map[string]func(string) Op{"commit": func(v string) Op { return DiffTree{Commit: v} }}},
		{"cat-file -p", CatFilePretty{Object: "abc:f"}, "cat-file -p --end-of-options abc:f",
			map[string]func(string) Op{"object": func(v string) Op { return CatFilePretty{Object: v} }}},
		{"cat-file --batch", CatFileBatch{}, "cat-file --batch", nil},
		{"cherry", Cherry{Upstream: "u", Head: "h"}, "cherry --end-of-options u h", map[string]func(string) Op{
			"upstream": func(v string) Op { return Cherry{Upstream: v, Head: "h"} },
			"head":     func(v string) Op { return Cherry{Upstream: "u", Head: v} },
		}},
		{"update-ref", UpdateRef{Message: "-m", Ref: "refs/heads/x", New: "abc", Old: "def"},
			"update-ref -m -m --end-of-options refs/heads/x abc def", map[string]func(string) Op{
				"ref": func(v string) Op { return UpdateRef{Ref: v, New: "n"} },
				"new": func(v string) Op { return UpdateRef{Ref: "r", New: v} },
				"old": func(v string) Op { return UpdateRef{Ref: "r", New: "n", Old: v} },
			}},
		{"update-ref --stdin", UpdateRefStdin{Message: "why"}, "update-ref -m why --stdin", nil},
		{"init", Init{Dir: "-d", Bare: true, Quiet: true, NoTemplate: true, InitialBranch: "main", RefFormat: "files"},
			"init --quiet --bare --template= --initial-branch=main --ref-format=files --end-of-options -d",
			map[string]func(string) Op{
				"initial-branch": func(v string) Op { return Init{InitialBranch: v} },
				"dir":            func(v string) Op { return Init{Dir: "a" + nul} },
			}},
		{"symbolic-ref set", SymbolicRefSet{Name: "HEAD", Target: "refs/heads/main"},
			"symbolic-ref --end-of-options HEAD refs/heads/main", map[string]func(string) Op{
				"name":   func(v string) Op { return SymbolicRefSet{Name: v, Target: "t"} },
				"target": func(v string) Op { return SymbolicRefSet{Name: "n", Target: v} },
			}},
		{"merge-base --octopus", MergeBaseOctopus{Commits: []string{"a", "b"}}, "merge-base --octopus --end-of-options a b",
			map[string]func(string) Op{"commit": func(v string) Op { return MergeBaseOctopus{Commits: []string{"a", v}} }}},
		{"merge-tree", MergeTree{Branch1: "a", Branch2: "b"},
			"merge-tree --write-tree --name-only --no-messages --end-of-options a b", map[string]func(string) Op{
				"branch1": func(v string) Op { return MergeTree{Branch1: v, Branch2: "b"} },
				"branch2": func(v string) Op { return MergeTree{Branch1: "a", Branch2: v} },
			}},
		{"commit-tree", CommitTree{Tree: "t", Parents: []string{"p1", "p2"}, Message: "-m", NoGPGSign: true},
			"commit-tree --no-gpg-sign t -p p1 -p p2 -m -m", map[string]func(string) Op{
				"tree":   func(v string) Op { return CommitTree{Tree: v} },
				"parent": func(v string) Op { return CommitTree{Tree: "t", Parents: []string{v}} },
			}},
		{"config --get-all", ConfigGetAll{Key: "k.x", Local: true}, "config --local --get-all --end-of-options k.x",
			map[string]func(string) Op{"key": func(v string) Op { return ConfigGetAll{Key: v} }}},
		{"config --unset-all", ConfigUnsetAll{Key: "k.x", ValueRegex: "^a$", Local: true},
			"config --local --unset-all --end-of-options k.x ^a$",
			map[string]func(string) Op{"key": func(v string) Op { return ConfigUnsetAll{Key: v} }}},
		{"remote set-url", RemoteSetURL{Name: "hub", URL: "https://h"}, "remote set-url --end-of-options hub https://h",
			map[string]func(string) Op{
				"remote": func(v string) Op { return RemoteSetURL{Name: v, URL: "u"} },
				"url":    func(v string) Op { return RemoteSetURL{Name: "hub", URL: v} },
			}},
		{"merge --ff-only", MergeFFOnly{Commit: "origin/main", Quiet: true}, "merge --quiet --ff-only origin/main",
			map[string]func(string) Op{"commit": func(v string) Op { return MergeFFOnly{Commit: v} }}},
		{"branch -f", BranchForce{Branch: "b", Start: "HEAD"}, "branch -f -- b HEAD", map[string]func(string) Op{
			"branch": func(v string) Op { return BranchForce{Branch: v, Start: "s"} },
			"start":  func(v string) Op { return BranchForce{Branch: "b", Start: v} },
		}},
		{"reset --keep", ResetKeep{Commit: "abc", Quiet: true}, "reset --keep --quiet abc",
			map[string]func(string) Op{"commit": func(v string) Op { return ResetKeep{Commit: v} }}},
		{"push", Push{Remote: "hub", Refspecs: []string{"HEAD:refs/heads/x"}, Porcelain: true, NoVerify: true,
			NoRecurseSubmodules: true},
			"push --porcelain --no-verify --no-recurse-submodules -- hub HEAD:refs/heads/x", map[string]func(string) Op{
				"remote":  func(v string) Op { return Push{Remote: v} },
				"refspec": func(v string) Op { return Push{Remote: "hub", Refspecs: []string{v}} },
			}},
		{"checkout --detach", CheckoutDetach{Commit: "abc", Force: true, Quiet: true},
			"checkout --quiet --detach --force abc",
			map[string]func(string) Op{"commit": func(v string) Op { return CheckoutDetach{Commit: v} }}},
		{"clean", Clean{Force: 2, Dirs: true, Ignored: true, Quiet: true}, "clean -f -f -d -x -q", nil},
		{"patch-id", PatchID{Stable: true}, "patch-id --stable", nil},
		{"add -A", AddAll{}, "add -A", nil},
	}
}

func TestOpsBuildTheirArgv(t *testing.T) {
	for _, c := range opCases() {
		got, err := Argv(Opts{}, c.op)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if strings.Join(got, " ") != c.want {
			t.Errorf("%s:\n got %q\nwant %q", c.name, strings.Join(got, " "), c.want)
		}
	}
}

func TestOpsRefuseALeadingDashAndANUL(t *testing.T) {
	for _, c := range opCases() {
		for field, mk := range c.bad {
			for _, v := range []string{"-x", "--upload-pack=evil", "a" + nul + "b"} {
				_, err := Argv(Opts{}, mk(v))
				var ae *ArgError
				if !errors.As(err, &ae) {
					t.Errorf("%s %s=%q: want an *ArgError, got %v", c.name, field, v, err)
					continue
				}
				if ae.Field != field {
					t.Errorf("%s %s=%q: refused field %q", c.name, field, v, ae.Field)
				}
			}
		}
	}
}

func TestOptsBuildTheirArgvAndRefuseABadKey(t *testing.T) {
	got, err := Argv(Opts{Config: []string{"core.hooksPath=/dev/null"}, GitDir: "/r.git"}, WorktreePrune{})
	want := []string{"-c", "core.hooksPath=/dev/null", "--git-dir=/r.git", "worktree", "prune"}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, %v, want %q", got, err, want)
	}
	for _, kv := range []string{"-x=1", "=1", "a.b=\x00"} {
		var ae *ArgError
		if _, err := Argv(Opts{Config: []string{kv}}, WorktreePrune{}); !errors.As(err, &ae) {
			t.Errorf("config %q: want an *ArgError, got %v", kv, err)
		}
	}
}

func TestOpsRefuseWhatTheyCannotBuild(t *testing.T) {
	for name, op := range map[string]Op{
		"no dirs":         RevParseGitDirs{},
		"reset no branch": WorktreeAdd{Path: "w", Reset: true},
		"untracked":       Status{Untracked: "some"},
		"whitespace":      Apply{Whitespace: "-x"},
		"empty pattern":   GrepCount{Rev: "HEAD"},
		"no revs":         CherryPick{},
		"empty rev":       RevParse{},
		"empty remote":    Fetch{Refspecs: []string{"x"}},
		"group":           ListCmds{Groups: []string{"-x"}},
		"no rev-parse":    RevParseRevs{},
		"no octopus":      MergeBaseOctopus{},
		"ref format":      Init{RefFormat: "-x"},
	} {
		var ae *ArgError
		if _, err := Argv(Opts{}, op); !errors.As(err, &ae) {
			t.Errorf("%s: want an *ArgError, got %v", name, err)
		}
	}
}

func TestNameIsTheSubcommand(t *testing.T) {
	if n := Name(RevParseVerify{Rev: "x"}); n != "rev-parse" {
		t.Fatalf("Name = %q", n)
	}
}
