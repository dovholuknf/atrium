package guard

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The git policy, ported case for case from dotfiles
// claude/hooks/tests/test-git-guard.ps1, against real throwaway repositories.
// Where the old hook misfired, the case here holds the right answer and says
// so in `note`.

const fakeAgent = "http://127.0.0.1:7777"

type testEnv struct {
	OSEnv
	agent string
	vars  map[string]string
}

func (e testEnv) Agent() string            { return e.agent }
func (e testEnv) Getenv(key string) string { return e.vars[key] }

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
}

func initRepo(t *testing.T, path, extraBranch string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, path, "-c", "init.defaultBranch=main", "init", "-q", ".")
	if err := os.WriteFile(filepath.Join(path, "README.md"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, path, "add", "README.md")
	git(t, path, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "-m", "init")
	// Aliases, to prove the guard resolves them instead of matching verbs.
	git(t, path, "config", "alias.co", "checkout")
	git(t, path, "config", "alias.ci", "commit")
	git(t, path, "config", "alias.p", "push")
	git(t, path, "config", "alias.evil", "!touch pwned")
	if extraBranch != "" {
		git(t, path, "checkout", "-q", "-b", extraBranch)
	}
}

type repos map[string]string

func buildRepos(t *testing.T) repos {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	root := t.TempDir()
	if r, err := filepath.EvalSymlinks(root); err == nil {
		root = r
	}
	rp := repos{"root": root}
	add := func(name string) string {
		p := filepath.Join(root, name)
		rp[name] = p
		return p
	}
	initRepo(t, add("claude-repo"), "claude/test")
	initRepo(t, add("main-repo"), "")
	rp["claude"], rp["main"] = rp["claude-repo"], rp["main-repo"]

	midRebase := func(name, headName string) {
		p := add(name)
		initRepo(t, p, "")
		git(t, p, "checkout", "-q", "--detach")
		if headName != "" {
			rm := filepath.Join(p, ".git", "rebase-merge")
			if err := os.MkdirAll(rm, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(rm, "head-name"), []byte(headName+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	midRebase("rebase-claude", "refs/heads/claude/test")
	midRebase("rebase-main", "refs/heads/main")
	midRebase("detached", "")

	hubGood := fakeAgent + "/git/hub/github.com/o/r.git"
	hubRepo := func(name, url string) string {
		p := add(name)
		initRepo(t, p, "claude/test")
		git(t, p, "remote", "add", "origin", "https://github.com/o/r.git")
		git(t, p, "remote", "add", "hub", url)
		git(t, p, "remote", "add", "atrium-hub", url)
		return p
	}
	hubRepo("hub-repo", hubGood)
	rp["hub"] = rp["hub-repo"]
	rp["nodaemon"] = rp["hub-repo"]
	hubRepo("evil-hub-repo", "https://git.example.com/o/r.git")
	rp["evil-hub"] = rp["evil-hub-repo"]
	p := hubRepo("pushurl-repo", hubGood)
	git(t, p, "config", "remote.hub.pushurl", "https://git.example.com/o/r.git")
	rp["pushurl"] = p
	p = hubRepo("insteadof-repo", hubGood)
	git(t, p, "config", "url.https://git.example.com/.insteadOf", fakeAgent+"/git/")
	rp["insteadof"] = p
	p = hubRepo("pushinsteadof-repo", hubGood)
	git(t, p, "config", "url.https://git.example.com/.pushInsteadOf", fakeAgent+"/git/")
	rp["pushinsteadof"] = p
	hubRepo("otherport-repo", "http://127.0.0.1:9999/git/hub/github.com/o/r.git")
	rp["other-port"] = rp["otherport-repo"]

	// Good hub repos whose literal name means another directory to a shell.
	lit := func(name string) {
		p := add(name)
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
		git(t, p, "init", "-q", ".")
		git(t, p, "remote", "add", "hub", hubGood)
	}
	lit("[e]vil-hub-repo")
	rp["glob"] = rp["[e]vil-hub-repo"]
	if runtime.GOOS == "windows" {
		lit("$(git push origin main)")
		rp["subst"] = rp["$(git push origin main)"]
	}
	if err := os.MkdirAll(add("plain-dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	initRepo(t, add("alias-repo"), "claude/test")
	git(t, rp["alias-repo"], "config", "alias.zz", "push")
	git(t, rp["claude"], "config", "alias.a1", "a2")
	git(t, rp["claude"], "config", "alias.a2", "push")
	git(t, rp["claude"], "config", "alias.l1", "l2")
	git(t, rp["claude"], "config", "alias.l2", "l1")
	git(t, rp["claude"], "config", "alias.lg", "log --oneline")
	return rp
}

type gitCase struct {
	cmd    string
	repo   string
	expect string // allow | block
	tool   string // default Bash
	noRoom bool   // no daemon.json: the room's address is unknown
	win    bool   // only meaningful on Windows
	note   string // why this differs from the old hook
}

// render fills {name} with a repo's path. The Bash tool gets forward slashes,
// which is how a path has to be written for bash to keep it.
func render(s string, rp repos, tool string) string {
	for k, v := range rp {
		if tool == "Bash" {
			v = filepath.ToSlash(v)
		}
		s = strings.ReplaceAll(s, "{"+k+"}", v)
	}
	return s
}

func bashDrive(p string) string {
	p = filepath.ToSlash(p)
	if len(p) > 2 && p[1] == ':' {
		return "/" + strings.ToLower(p[:1]) + p[2:]
	}
	return p
}

var gitCases = []gitCase{
	// --- current-branch verbs on a claude/* branch: allow ---
	{cmd: `git commit -m "wip"`, repo: "claude", expect: "allow"},
	{cmd: `git commit -am "wip"`, repo: "claude", expect: "allow"},
	{cmd: `git add .`, repo: "claude", expect: "allow"},
	{cmd: `git add src/foo.c`, repo: "claude", expect: "allow"},
	{cmd: `git rebase main`, repo: "claude", expect: "allow"},
	{cmd: `git reset --hard HEAD~1`, repo: "claude", expect: "allow"},
	{cmd: `git restore foo.txt`, repo: "claude", expect: "allow"},
	{cmd: `git clean -fd`, repo: "claude", expect: "allow"},
	{cmd: `git --no-pager commit -m x`, repo: "claude", expect: "allow"},
	{cmd: `git -c core.pager=touch commit -m x`, repo: "claude", expect: "block"},

	// --- same verbs on a non-claude branch: block ---
	{cmd: `git commit -m "wip"`, repo: "main", expect: "block"},
	{cmd: `git add .`, repo: "main", expect: "block"},
	{cmd: `git reset --hard`, repo: "main", expect: "block"},
	{cmd: `git restore foo.txt`, repo: "main", expect: "block"},
	{cmd: `git clean -fd`, repo: "main", expect: "block"},
	{cmd: `git rebase main`, repo: "main", expect: "block"},

	// --- remote ops: always block ---
	{cmd: `git push`, repo: "claude", expect: "block"},
	{cmd: `git push origin claude/test`, repo: "claude", expect: "block"},
	{cmd: `git push -u claude claude/test`, repo: "claude", expect: "block"},
	{cmd: `git push --force`, repo: "claude", expect: "block"},
	{cmd: `git pull`, repo: "claude", expect: "block"},
	{cmd: `git pull origin main`, repo: "claude", expect: "block"},
	{cmd: `git fetch`, repo: "claude", expect: "block"},
	{cmd: `git fetch --all`, repo: "claude", expect: "block"},

	// --- branch naming ---
	{cmd: `git checkout -b claude/new`, repo: "claude", expect: "allow"},
	{cmd: `git checkout -B claude/new`, repo: "claude", expect: "allow"},
	{cmd: `git checkout -b feature/x`, repo: "claude", expect: "block"},
	{cmd: `git checkout -b main`, repo: "claude", expect: "block"},
	{cmd: `git checkout main`, repo: "claude", expect: "block"},
	{cmd: `git checkout claude/test`, repo: "claude", expect: "block"},
	{cmd: `git checkout -- foo.txt`, repo: "claude", expect: "block"},
	{cmd: `git switch -c claude/new`, repo: "claude", expect: "allow"},
	{cmd: `git switch claude/other`, repo: "claude", expect: "allow"},
	{cmd: `git switch -c feature/x`, repo: "claude", expect: "block"},
	{cmd: `git switch main`, repo: "claude", expect: "block"},
	{cmd: `git branch claude/foo`, repo: "claude", expect: "allow"},
	{cmd: `git branch -d claude/foo`, repo: "claude", expect: "allow"},
	{cmd: `git branch -D claude/foo`, repo: "claude", expect: "allow"},
	{cmd: `git branch -m claude/a claude/b`, repo: "claude", expect: "allow"},
	{cmd: `git branch feature/x`, repo: "claude", expect: "block"},
	{cmd: `git branch -D main`, repo: "claude", expect: "block"},
	{cmd: `git branch -m main claude/b`, repo: "claude", expect: "block"},
	{cmd: `git branch claude/foo 7f736df`, repo: "claude", expect: "allow"},
	{cmd: `git branch claude/foo v1.2.3`, repo: "claude", expect: "allow"},
	{cmd: `git branch -f --track claude/foo origin/main`, repo: "claude", expect: "allow"},
	{cmd: `git branch feature/x 7f736df`, repo: "claude", expect: "block"},
	{cmd: `git branch -m claude/a main`, repo: "claude", expect: "block"},
	{cmd: `git branch -D claude/a main`, repo: "claude", expect: "block"},
	{cmd: `git branch claude/foo $(git rev-parse x)`, repo: "claude", expect: "block"},
	{cmd: "git branch claude/foo 7f736df\ngit commit -m x", repo: "main", expect: "block"},
	{cmd: `git checkout -b claude/new 7f736df`, repo: "claude", expect: "allow"},
	{cmd: `git checkout -b claude/new v1.2.3`, repo: "claude", expect: "allow"},
	{cmd: `git checkout -b feature/x 7f736df`, repo: "claude", expect: "block"},
	{cmd: `git switch -c claude/new 7f736df`, repo: "claude", expect: "allow"},
	{cmd: `git switch -c claude/new v1.2.3`, repo: "claude", expect: "allow"},
	{cmd: `git switch -c feature/x 7f736df`, repo: "claude", expect: "block"},

	// --- read-only git: always allow ---
	{cmd: `git status`, repo: "main", expect: "allow"},
	{cmd: `git status --porcelain`, repo: "main", expect: "allow"},
	{cmd: `git log --oneline -20`, repo: "main", expect: "allow"},
	{cmd: `git diff main...HEAD`, repo: "main", expect: "allow"},
	{cmd: `git show HEAD`, repo: "main", expect: "allow"},
	{cmd: `git remote -v`, repo: "main", expect: "allow"},
	{cmd: `git branch`, repo: "main", expect: "allow"},
	{cmd: `git branch -a`, repo: "main", expect: "allow"},
	{cmd: `git branch -vv`, repo: "main", expect: "allow"},

	// --- aliases: resolved, not pattern-matched ---
	{cmd: `git co -b claude/x`, repo: "claude", expect: "allow"},
	{cmd: `git co -b main`, repo: "claude", expect: "block"},
	{cmd: `git ci -m x`, repo: "claude", expect: "allow"},
	{cmd: `git ci -m x`, repo: "main", expect: "block"},
	{cmd: `git p`, repo: "claude", expect: "block"},
	{cmd: `git p origin claude/test`, repo: "claude", expect: "block"},
	{cmd: `git evil`, repo: "claude", expect: "block"},
	{cmd: `git config alias.x checkout`, repo: "claude", expect: "block"},
	{cmd: `git config --global alias.y '!sh'`, repo: "claude", expect: "block"},
	{cmd: `git config --unset alias.co`, repo: "claude", expect: "block"},
	{cmd: `git config --get-regexp alias`, repo: "claude", expect: "allow"},
	{cmd: `git config --get alias.co`, repo: "claude", expect: "allow"},

	// --- mid-rebase: the branch being rebased decides ---
	{cmd: `git add CHANGELOG.md`, repo: "rebase-claude", expect: "allow"},
	{cmd: `git rebase --continue`, repo: "rebase-claude", expect: "allow"},
	{cmd: `git add CHANGELOG.md`, repo: "rebase-main", expect: "block"},
	{cmd: `git rebase --continue`, repo: "rebase-main", expect: "block"},
	{cmd: `git add .`, repo: "detached", expect: "block"},

	// --- the git policy binds every shell ---
	{cmd: `git commit -m "wip"`, repo: "main", expect: "block", tool: "PowerShell"},
	{cmd: `git commit -m "wip"`, repo: "claude", expect: "allow", tool: "PowerShell"},
	{cmd: `git add .`, repo: "main", expect: "block", tool: "PowerShell"},
	{cmd: `git push`, repo: "claude", expect: "block", tool: "PowerShell"},
	{cmd: `git checkout -b feature/x`, repo: "claude", expect: "block", tool: "PowerShell"},
	{cmd: `git ci -m x`, repo: "main", expect: "block", tool: "PowerShell"},
	{cmd: `git commit -m "co-authored-by: x"`, repo: "claude", expect: "block", tool: "PowerShell"},
	{cmd: `git status`, repo: "main", expect: "allow", tool: "PowerShell"},

	// ';' and '>' are Bash-tool ergonomics.
	{cmd: `git status; git status`, repo: "claude", expect: "block", tool: "Bash"},
	{cmd: `git status; git status`, repo: "claude", expect: "allow", tool: "PowerShell"},
	{cmd: `git log > out.txt`, repo: "claude", expect: "block", tool: "Bash"},
	{cmd: `git log > out.txt`, repo: "claude", expect: "allow", tool: "PowerShell"},

	// --- atrium hub remote ---
	{cmd: `git push hub claude/test`, repo: "hub", expect: "allow"},
	{cmd: `git push hub fix/x`, repo: "hub", expect: "allow"},
	{cmd: `git push -u hub fix/x`, repo: "hub", expect: "allow"},
	{cmd: `git fetch hub`, repo: "hub", expect: "allow"},
	{cmd: `git push atrium-hub fix/x`, repo: "hub", expect: "allow"},
	{cmd: `git push -u atrium-hub fix/x`, repo: "hub", expect: "allow"},
	{cmd: `git fetch atrium-hub`, repo: "hub", expect: "allow"},
	{cmd: `git push hub fix/x`, repo: "hub", expect: "allow", tool: "PowerShell"},
	{cmd: `git fetch hub`, repo: "hub", expect: "allow", tool: "PowerShell"},
	{cmd: `git push -f hub fix/x`, repo: "hub", expect: "block"},
	{cmd: `git push --force hub fix/x`, repo: "hub", expect: "block"},
	{cmd: `git push --force-with-lease hub fix/x`, repo: "hub", expect: "block"},
	{cmd: `git push hub fix/x --force`, repo: "hub", expect: "block"},
	{cmd: `git push hub fix/x -f`, repo: "hub", expect: "block"},
	{cmd: `git push hub +fix/x`, repo: "hub", expect: "block"},
	{cmd: `git push hub :fix/x`, repo: "hub", expect: "block"},
	{cmd: `git push hub fix/x:main`, repo: "hub", expect: "block"},
	{cmd: `git push --delete hub fix/x`, repo: "hub", expect: "block"},
	{cmd: `git push hub --delete fix/x`, repo: "hub", expect: "block"},
	{cmd: `git push -d hub fix/x`, repo: "hub", expect: "block"},
	{cmd: `git push --mirror hub`, repo: "hub", expect: "block"},
	{cmd: `git push --all hub`, repo: "hub", expect: "block"},
	{cmd: `git push --tags hub`, repo: "hub", expect: "block"},
	{cmd: `git push hub fix/x other/y`, repo: "hub", expect: "block"},
	{cmd: `git push hub`, repo: "hub", expect: "block"},
	{cmd: `git push origin fix/x`, repo: "hub", expect: "block"},
	{cmd: `git push -u origin fix/x`, repo: "hub", expect: "block"},
	{cmd: `git fetch origin`, repo: "hub", expect: "block"},
	{cmd: `git fetch hub main`, repo: "hub", expect: "block"},
	{cmd: `git fetch --all`, repo: "hub", expect: "block"},
	{cmd: `git pull hub fix/x`, repo: "hub", expect: "block"},
	{cmd: `git -c http.extraHeader=x push hub fix/x`, repo: "hub", expect: "block"},
	{cmd: `git fetch hub && git push origin fix/x`, repo: "hub", expect: "block"},
	{cmd: "git push hub fix/x\ngit push origin fix/x", repo: "hub", expect: "block"},
	{cmd: `git push hub fix/x; git push origin fix/x`, repo: "hub", expect: "block", tool: "PowerShell"},
	{cmd: `git push hub fix/x`, repo: "evil-hub", expect: "block"},
	{cmd: `git fetch hub`, repo: "evil-hub", expect: "block"},
	{cmd: `git push hub fix/x`, repo: "pushurl", expect: "block"},
	{cmd: `git push hub fix/x`, repo: "insteadof", expect: "block"},
	{cmd: `git push hub fix/x`, repo: "pushinsteadof", expect: "block"},
	{cmd: `git push hub fix/x`, repo: "other-port", expect: "block"},
	{cmd: `git push hub fix/x`, repo: "main", expect: "block"},
	{cmd: `git push hub fix/x`, repo: "nodaemon", expect: "block", noRoom: true},
	{cmd: `git fetch hub`, repo: "nodaemon", expect: "block", noRoom: true},

	// --- one leading directory change: the hub remote is checked in the new dir ---
	{cmd: "cd {hub}\ngit fetch hub", repo: "main", expect: "allow"},
	{cmd: "cd {hub}\ngit fetch hub", repo: "main", expect: "allow", tool: "PowerShell"},
	{cmd: `cd "{hub}" && git fetch hub`, repo: "main", expect: "allow"},
	{cmd: `cd "{hub}" && git fetch hub`, repo: "main", expect: "allow", tool: "PowerShell"},
	{cmd: "Set-Location -LiteralPath \"{hub}\"\ngit fetch hub", repo: "main", expect: "allow", tool: "PowerShell"},
	{cmd: "Set-Location -Path '{hub}'\ngit fetch hub", repo: "main", expect: "allow", tool: "PowerShell"},
	{cmd: "sl {hub}\ngit fetch hub", repo: "main", expect: "allow", tool: "PowerShell"},
	{cmd: "pushd {hub}\ngit fetch hub", repo: "main", expect: "allow", tool: "PowerShell"},
	{cmd: "cd {hub}\r\ngit fetch hub\r\n", repo: "main", expect: "allow", tool: "PowerShell"},
	{cmd: "cd {bashhub} && git fetch hub", repo: "main", expect: "allow", win: true},
	{cmd: "cd ../hub-repo\ngit fetch hub", repo: "main", expect: "allow"},
	{cmd: "cd ..\\hub-repo\ngit fetch hub", repo: "main", expect: "allow", tool: "PowerShell", win: true},
	{cmd: "cd ./hub-repo\ngit fetch hub", repo: "root", expect: "allow"},
	{cmd: "cd {hub}\ngit fetch atrium-hub", repo: "main", expect: "allow"},
	{cmd: "cd {hub}\ngit push hub claude/x", repo: "main", expect: "allow"},
	{cmd: "cd {hub}\ngit push -u hub claude/x", repo: "main", expect: "allow", tool: "PowerShell"},
	{cmd: "cd {main}\ngit fetch hub", repo: "hub", expect: "block"},
	{cmd: "cd {evil-hub}\ngit fetch hub", repo: "hub", expect: "block"},
	{cmd: "cd {evil-hub}\ngit push hub claude/x", repo: "hub", expect: "block"},
	{cmd: "cd {pushurl}\ngit push hub claude/x", repo: "hub", expect: "block"},
	{cmd: "cd {insteadof}\ngit push hub claude/x", repo: "hub", expect: "block"},
	{cmd: "cd {pushinsteadof}\ngit push hub claude/x", repo: "hub", expect: "block"},
	{cmd: "cd {other-port}\ngit push hub claude/x", repo: "hub", expect: "block"},
	{cmd: "cd {hub}\ngit push hub claude/x", repo: "main", expect: "block", noRoom: true},
	{cmd: "cd {plain-dir}\ngit fetch hub", repo: "hub", expect: "block"},
	{cmd: "cd {root}/nope\ngit fetch hub", repo: "hub", expect: "block"},
	{cmd: "cd {hub}\ngit fetch origin", repo: "main", expect: "block"},
	{cmd: "cd {hub} && git fetch origin", repo: "main", expect: "block"},
	{cmd: "cd {hub}\ngit pull", repo: "main", expect: "block"},
	{cmd: "cd {hub}\ngit pull hub claude/x", repo: "main", expect: "block"},
	{cmd: "cd {hub}\ngit push origin claude/x", repo: "main", expect: "block"},
	{cmd: "cd {hub}\ngit push", repo: "main", expect: "block"},
	{cmd: "cd {hub}\ngit fetch hub main", repo: "main", expect: "block"},
	{cmd: "cd {hub}\ngit p hub claude/x", repo: "main", expect: "block"},
	{cmd: "cd {hub}\ngit -c http.extraHeader=x push hub claude/x", repo: "main", expect: "block"},
	{cmd: "cd {hub}\ngit push -f hub x", repo: "main", expect: "block"},
	{cmd: "cd {hub}\ngit push --force hub x", repo: "main", expect: "block"},
	{cmd: "cd {hub}\ngit push hub +x", repo: "main", expect: "block"},
	{cmd: "cd {hub}\ngit push hub :x", repo: "main", expect: "block"},
	{cmd: "cd {hub}\ngit push hub x:main", repo: "main", expect: "block"},
	{cmd: "cd {hub}\ngit push hub x --force", repo: "main", expect: "block"},
	{cmd: "cd {hub}\ngit fetch hub && rm x", repo: "main", expect: "block"},
	{cmd: "cd {hub} && git fetch hub && rm x", repo: "main", expect: "block", tool: "PowerShell"},
	{cmd: "cd {hub}\ngit fetch hub; git push origin x", repo: "main", expect: "block", tool: "PowerShell"},
	{cmd: "cd {hub}\ngit fetch hub\ngit push origin x", repo: "main", expect: "block"},
	{cmd: "cd {hub}\ngit fetch hub | git push origin x", repo: "main", expect: "block"},
	{cmd: "cd {hub}\ncd {evil-hub}\ngit push hub x", repo: "main", expect: "block"},
	{cmd: "cd {hub}; git fetch hub", repo: "main", expect: "block", tool: "PowerShell"},
	{cmd: `cd "{hub}" & git fetch hub`, repo: "main", expect: "block", tool: "PowerShell"},
	{cmd: "git push origin x\ncd {hub}\ngit fetch hub", repo: "main", expect: "block"},
	{cmd: "cd {hub} && cd {evil-hub} && git push hub x", repo: "main", expect: "block"},
	{cmd: "cd \"{glob}\"\ngit push hub x", repo: "main", expect: "block", tool: "PowerShell"},
	{cmd: "cd '{subst}'\ngit push hub x", repo: "main", expect: "block", tool: "PowerShell", win: true},
	{cmd: "cd \"{subst}\"\ngit push hub x", repo: "main", expect: "block", tool: "PowerShell", win: true},
	{cmd: "cd hub-repo\ngit fetch hub", repo: "root", expect: "block"},
	{cmd: "cd -\ngit fetch hub", repo: "hub", expect: "block"},
	{cmd: "cd ~\ngit fetch hub", repo: "hub", expect: "block"},
	{cmd: "cd C:hub-repo\ngit fetch hub", repo: "hub", expect: "block"},
	{cmd: "cd \"FileSystem::{hub}\"\ngit fetch hub", repo: "main", expect: "block", tool: "PowerShell"},
	{cmd: "cd (\"{hub}\")\ngit fetch hub", repo: "main", expect: "block", tool: "PowerShell"},
	{cmd: "cd {hub} && git status", repo: "main", expect: "block"},

	// --- aliases anywhere in the command, and in any repo it changes into ---
	{cmd: "echo hi\ngit p origin main", repo: "claude", expect: "block", tool: "PowerShell"},
	{cmd: "git status\ngit p origin main", repo: "claude", expect: "block", tool: "PowerShell"},
	{cmd: "git status && git p origin main", repo: "claude", expect: "block", tool: "PowerShell"},
	{cmd: "git status; git p origin main", repo: "claude", expect: "block", tool: "PowerShell"},
	{cmd: "Write-Output (git p origin main)", repo: "claude", expect: "block", tool: "PowerShell"},
	{cmd: "echo $(git p origin main)", repo: "claude", expect: "block"},
	{cmd: "GIT p origin main", repo: "claude", expect: "block", tool: "PowerShell"},
	{cmd: "cd {alias-repo}\ngit zz origin main", repo: "main", expect: "block", tool: "PowerShell"},
	{cmd: "Set-Location \"{alias-repo}\"; git zz origin main", repo: "main", expect: "block", tool: "PowerShell"},
	{cmd: "pushd ../alias-repo\ngit zz hub claude/x", repo: "main", expect: "block", tool: "PowerShell"},
	{cmd: "git a1 origin main", repo: "claude", expect: "block"},
	{cmd: "git l1", repo: "claude", expect: "block"},
	{cmd: "git lg -5", repo: "claude", expect: "allow"},
	{cmd: "git status\ngit lg -5", repo: "claude", expect: "allow", tool: "PowerShell"},
	{cmd: "git zz origin main", repo: "main", expect: "block"},
	{cmd: "cd $env:TEMP\ngit zz origin main", repo: "main", expect: "block", tool: "PowerShell"},
	{cmd: "git flow feature publish x", repo: "claude", expect: "block"},
	{cmd: "git lfs push origin main", repo: "claude", expect: "block"},
	{cmd: "git lfs fetch", repo: "claude", expect: "block"},
	{cmd: "git lfs pull", repo: "claude", expect: "block"},
	{cmd: "git lfs ls-files", repo: "claude", expect: "allow"},
	{cmd: "echo git is fun", repo: "claude", expect: "allow"},

	// --- misfires of the old hook: these hold the RIGHT answer ---
	// 1. It checked the session's directory, not the one the command changes to.
	{cmd: "cd {claude}\ngit commit -m x", repo: "main", expect: "allow", note: "cwd"},
	{cmd: "cd {main}\ngit commit -m x", repo: "claude", expect: "block", note: "cwd"},
	{cmd: "Set-Location {claude}; git add .", repo: "main", expect: "allow", tool: "PowerShell", note: "cwd"},
	{cmd: "cd {main}; git add .", repo: "claude", expect: "block", tool: "PowerShell", note: "cwd"},
	{cmd: "cd $HOME\ngit commit -m x", repo: "claude", expect: "block", note: "cwd: a directory the guard cannot follow is not the session's"},
	{cmd: "git checkout -b claude/x && git commit -m y", repo: "main", expect: "block", note: "every git command is checked, not the first"},
	// 2. It read 2>&1 as a branch name.
	{cmd: "git branch -a 2>&1", repo: "main", expect: "allow", note: "2>&1"},
	{cmd: "git switch claude/other 2>&1", repo: "claude", expect: "allow", note: "2>&1"},
	{cmd: "git switch claude/other 2>&1", repo: "claude", expect: "allow", tool: "PowerShell", note: "2>&1"},
	{cmd: "git checkout -b claude/n 2>&1", repo: "claude", expect: "allow", note: "2>&1"},
	{cmd: "git branch claude/foo 2>&1", repo: "claude", expect: "allow", tool: "PowerShell", note: "2>&1"},
	{cmd: "git push hub fix/x 2>&1", repo: "hub", expect: "allow", note: "2>&1"},
	// 3. It refused git -C.
	{cmd: "git -C {main} status", repo: "claude", expect: "allow", note: "git -C"},
	{cmd: "git -C {main} log --oneline -5", repo: "claude", expect: "allow", tool: "PowerShell", note: "git -C"},
	{cmd: "git -C {claude} commit -m x", repo: "main", expect: "allow", note: "git -C runs in that repo"},
	{cmd: "git -C {main} commit -m x", repo: "claude", expect: "block", note: "git -C runs in that repo"},
	{cmd: "git -C {hub} fetch hub", repo: "main", expect: "allow", note: "git -C is a directory change"},
	{cmd: "git -C {evil-hub} fetch hub", repo: "hub", expect: "block", note: "git -C is a directory change"},
	{cmd: "git --git-dir=x/.git commit -m y", repo: "claude", expect: "block", note: "--git-dir: repository unknown"},
	// The Bash tool's own reading of a backslash path: bash removes the
	// backslashes, so cd lands nowhere and the check moves with it.
	{cmd: "cd {hub-backslash}\ngit fetch hub", repo: "main", expect: "block", win: true, note: "bash eats backslashes"},
}

func TestGitGuard(t *testing.T) {
	rp := buildRepos(t)
	rp["bashhub"] = bashDrive(rp["hub"])
	rp["hub-backslash"] = rp["hub"]
	rules, err := Builtin()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range gitCases {
		if c.win && runtime.GOOS != "windows" {
			continue
		}
		tool := c.tool
		if tool == "" {
			tool = "Bash"
		}
		cmd := render(c.cmd, rp, tool)
		if strings.Contains(c.cmd, "{hub-backslash}") {
			cmd = strings.ReplaceAll(c.cmd, "{hub-backslash}", rp["hub"])
		}
		in := Input{ToolName: tool, CWD: rp[c.repo]}
		in.ToolInput.Command = cmd
		env := testEnv{agent: fakeAgent}
		if c.noRoom {
			env.agent = ""
		}
		d := rules.Evaluate(in, env)
		got := "allow"
		if d.Action == "deny" {
			got = "block"
		}
		if got != c.expect {
			t.Errorf("[%s/%s] %q: want %s, got %s (%s: %s)", c.repo, tool, cmd, c.expect, got, d.Rule, d.Reason)
		}
	}
}
