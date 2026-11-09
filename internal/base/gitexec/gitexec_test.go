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
		"empty remote":    Fetch{},
		"group":           ListCmds{Groups: []string{"-x"}},
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
