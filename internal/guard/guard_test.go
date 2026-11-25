package guard

import (
	"errors"
	"runtime"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/base/gitexec"
)

// fakeEnv answers from maps, for the rules that need no repository.
type fakeEnv struct {
	dirs  map[string]bool // existing paths; true for a directory
	vars  map[string]string
	agent string
}

func (e fakeEnv) Git(string, gitexec.Op) (string, error) { return "", errors.New("no git here") }
func (e fakeEnv) Stat(p string) (bool, bool) {
	d, ok := e.dirs[strings.ReplaceAll(p, "/", `\`)]
	return d, ok
}
func (e fakeEnv) HasPrefix(dir, prefix string) bool {
	for p := range e.dirs {
		if strings.HasPrefix(strings.ToLower(p), strings.ToLower(dir+prefix)) {
			return true
		}
	}
	return false
}
func (e fakeEnv) ReadFile(string) ([]byte, error) { return nil, errors.New("none") }
func (e fakeEnv) Getenv(k string) string          { return e.vars[k] }
func (e fakeEnv) Agent() string                   { return e.agent }

type call struct {
	tool    string
	cmd     string
	path    string
	content string
	newStr  string
	subtype string
	cwd     string
	// want is the rule that answers, or "" for none.
	want string
	note string
}

func (c call) input() Input {
	in := Input{ToolName: c.tool, CWD: c.cwd}
	in.ToolInput.Command = c.cmd
	in.ToolInput.FilePath = c.path
	in.ToolInput.Content = c.content
	in.ToolInput.NewString = c.newStr
	in.ToolInput.SubagentType = c.subtype
	return in
}

var env0 = fakeEnv{
	dirs: map[string]bool{
		`C:\Users`: true, `D:\tmp`: true, `D:\worktrees`: true, `C:\Program Files`: true,
		`D:\proj`: true, `D:\proj\CMakePresets.json`: false, `D:\plain`: true,
	},
}

// The rules the old hook had no tests for, case by case from its source, and
// the misfires that involve no repository.
var calls = []call{
	// drive roots
	{tool: "Bash", cmd: `mkdir D:/zz-new`, want: "drive-root"},
	{tool: "Bash", cmd: `mkdir D:\zz-new`, want: "", note: "bash makes this D:zz-new, relative"},
	{tool: "Bash", cmd: `mkdir /d/zz-new`, want: "drive-root"},
	{tool: "PowerShell", cmd: `New-Item -ItemType Directory C:\zz-new`, want: "drive-root"},
	{tool: "Bash", cmd: `ls D:/tmp/x`, want: ""},
	{tool: "Bash", cmd: `ls "C:/Program Files/x"`, want: ""},
	{tool: "PowerShell", cmd: `ls C:\Program Files\x`, want: ""},
	{tool: "Bash", cmd: `ls /usr/bin`, want: ""},
	{tool: "Write", path: `E:\zz-new\a.txt`, content: "x", want: "drive-root"},
	{tool: "Write", path: `D:\tmp\a.txt`, content: "x", want: ""},
	{tool: "Bash", cmd: `grep -r "D:\zz-nowhere" .`, want: "", note: "a phrase inside a grep"},
	// subagents
	{tool: "Agent", subtype: "general-purpose", want: "only-atrium-subagents"},
	{tool: "Task", subtype: "", want: "only-atrium-subagents"},
	{tool: "Agent", subtype: "Explore", want: ""},
	{tool: "Agent", subtype: "plan", want: ""},
	{tool: "Agent", subtype: "web-fetch", want: ""},
	{tool: "Agent", subtype: "my-reviewer", want: "only-atrium-subagents", note: "no agent file"},
	{tool: "Agent", subtype: "../x", want: "only-atrium-subagents", note: "not a name"},
	// co-author trailer
	{tool: "Bash", cmd: `git commit -m "x

Co-Authored-By: someone"`, want: "no-coauthor"},
	{tool: "Bash", cmd: "git commit -F - <<'EOF'\nsubject\n\nCo-authored-by: x\nEOF", want: "no-coauthor"},
	{tool: "Bash", cmd: `gh pr create --body "co-authored-by: x"`, want: "no-coauthor"},
	{tool: "Bash", cmd: `grep -ri co-authored-by .`, want: "", note: "a phrase inside a grep"},
	{tool: "Bash", cmd: `git log --grep=Co-Authored-By`, want: "", note: "a phrase inside a grep"},
	{tool: "Bash", cmd: `rg "Co-Authored-By" docs | head`, want: "", note: "a phrase inside a grep"},
	{tool: "PowerShell", cmd: `Select-String -Pattern co-authored-by -Path *.md`, want: "", note: "a phrase inside a grep"},
	{tool: "Bash", cmd: `git grep -n co-authored-by`, want: "", note: "a phrase inside a grep"},
	// go build
	{tool: "Bash", cmd: `go build ./...`, want: "go-build-output"},
	{tool: "Bash", cmd: `go build -o bin/x ./cmd/x`, want: "go-build-output"},
	{tool: "Bash", cmd: `go build -o build.claude/ ./...`, want: ""},
	{tool: "PowerShell", cmd: `go build -o build.claude/atrium.exe ./cmd/atrium`, want: ""},
	{tool: "Bash", cmd: `go vet ./...`, want: ""},
	{tool: "Bash", cmd: `grep -n "go build" Makefile`, want: "", note: "a phrase inside a grep"},
	{tool: "PowerShell", cmd: `explorer.exe shell:AppsFolder\Claude_x!Claude`, want: "no-gui-app-launch"},
	{tool: "Bash", cmd: `grep appsfolder notes.txt`, want: ""},
	// commands
	{tool: "Bash", cmd: `find . -name x`, want: "no-find"},
	{tool: "Bash", cmd: `ls | xargs find`, want: "no-find"},
	{tool: "Bash", cmd: `echo find me`, want: ""},
	{tool: "Bash", cmd: `perl -pe s/a/b/ f`, want: "no-perl"},
	{tool: "Bash", cmd: `cat f | perl -ne print`, want: "no-perl"},
	{tool: "Bash", cmd: `perldoc x`, want: ""},
	{tool: "Bash", cmd: `python3 x.py`, want: "no-python"},
	{tool: "PowerShell", cmd: `python.exe x.py`, want: "no-python"},
	{tool: "Bash", cmd: `ls /usr/lib/python3`, want: ""},
	{tool: "Bash", cmd: `grep python setup.cfg`, want: ""},
	{tool: "Bash", cmd: `bash -c "python3 x.py"`, want: "no-python", note: "one level down is still checked"},
	{tool: "Bash", cmd: `env FOO=1 perl x`, want: "no-perl"},
	{tool: "PowerShell", cmd: `pwsh -NoProfile -Command "git push origin main"`, cwd: `D:\plain`, want: "git-remote"},
	// git config naming a program
	{tool: "Bash", cmd: `git config core.fsmonitor ./x.sh`, want: "git-program-config"},
	{tool: "Bash", cmd: `git config --local core.hooksPath .h`, want: "git-program-config"},
	{tool: "Bash", cmd: `git config --global core.sshCommand evil`, want: "git-program-config"},
	{tool: "Bash", cmd: `git config --worktree core.pager evil`, want: "git-program-config"},
	{tool: "Bash", cmd: `git config -f .git/config core.editor evil`, want: "git-program-config"},
	{tool: "Bash", cmd: `git config --file=.git/config core.askPass evil`, want: "git-program-config"},
	{tool: "Bash", cmd: `git config set core.FSMonitor evil`, want: "git-program-config"},
	{tool: "Bash", cmd: `git config --add diff.external evil`, want: "git-program-config"},
	{tool: "Bash", cmd: `git config diff.bin.textconv evil`, want: "git-program-config"},
	{tool: "Bash", cmd: `git config diff.x.command evil`, want: "git-program-config"},
	{tool: "Bash", cmd: `git config merge.ours.driver evil`, want: "git-program-config"},
	{tool: "Bash", cmd: `git config filter.lfs.clean evil`, want: "git-program-config"},
	{tool: "Bash", cmd: `git config filter.lfs.smudge evil`, want: "git-program-config"},
	{tool: "Bash", cmd: `git config filter.lfs.process evil`, want: "git-program-config"},
	{tool: "Bash", cmd: `git config credential.helper evil`, want: "git-program-config"},
	{tool: "Bash", cmd: `git config credential.https://x.helper evil`, want: "git-program-config"},
	{tool: "Bash", cmd: `git config gpg.program evil`, want: "git-program-config"},
	{tool: "Bash", cmd: `git config gpg.ssh.program evil`, want: "git-program-config"},
	{tool: "Bash", cmd: `git config sequence.editor evil`, want: "git-program-config"},
	{tool: "Bash", cmd: `git config pager.log evil`, want: "git-program-config"},
	{tool: "Bash", cmd: `git config uploadpack.packObjectsHook evil`, want: "git-program-config"},
	{tool: "Bash", cmd: `git config include.path ../evil`, want: "git-program-config"},
	{tool: "Bash", cmd: `git config includeIf.onbranch:claude.path ../evil`, want: "git-program-config"},
	{tool: "Bash", cmd: `git config hook.x.command evil`, want: "git-program-config"},
	{tool: "Bash", cmd: `git config --unset core.hooksPath`, want: "git-program-config"},
	{tool: "Bash", cmd: `git config unset core.pager`, want: "git-program-config"},
	{tool: "Bash", cmd: `git config --edit`, want: "git-program-config"},
	{tool: "Bash", cmd: `git config edit --local`, want: "git-program-config"},
	{tool: "Bash", cmd: `git config --rename-section x diff.x`, want: "git-program-config"},
	{tool: "Bash", cmd: `git config "$K" evil`, want: "git-program-config"},
	{tool: "Bash", cmd: `git config set "$K" evil`, want: "git-program-config"},
	{tool: "PowerShell", cmd: `git config core.fsmonitor .\x.ps1`, want: "git-program-config"},
	{tool: "Bash", cmd: `git config core.pager list`, want: "git-program-config", note: "list is a value here"},
	{tool: "Bash", cmd: `git config --get core.hooksPath`, cwd: `D:\plain`, want: ""},
	{tool: "Bash", cmd: `git config get core.pager`, cwd: `D:\plain`, want: ""},
	{tool: "Bash", cmd: `git config --list --show-origin`, cwd: `D:\plain`, want: ""},
	{tool: "Bash", cmd: `git config user.email a@b`, cwd: `D:\plain`, want: ""},
	{tool: "Bash", cmd: `git config user.name "$NAME"`, cwd: `D:\plain`, want: ""},
	{tool: "Edit", path: `D:/proj/.git/config`, newStr: "x", want: "git-config-file"},
	{tool: "Write", path: `D:\proj\.git\worktrees\w\config.worktree`, content: "x", want: "git-config-file"},
	{tool: "Write", path: `D:/proj/.git/hooks/pre-commit`, content: "x", want: "git-config-file"},
	{tool: "Write", path: `C:/Users/u/.gitconfig`, content: "x", want: "git-config-file"},
	{tool: "Write", path: `D:/proj/config`, content: "x", want: ""},
	// docker
	{tool: "Bash", cmd: `FOO=bar docker run x`, want: "docker-env-prefix"},
	{tool: "Bash", cmd: `docker run -e FOO=bar x`, want: ""},
	// semicolons and redirection, Bash tool only
	{tool: "Bash", cmd: `git -C D:/proj status`, want: "git-dir-option"},
	{tool: "Bash", cmd: `git --git-dir=x/.git status`, want: "git-dir-option"},
	{tool: "Bash", cmd: `git --git-dir x/.git status`, want: "git-dir-option"},
	{tool: "PowerShell", cmd: `git -C D:\proj log`, want: "git-dir-option"},
	{tool: "Bash", cmd: `git status`, cwd: `D:\plain`, want: ""},
	{tool: "Bash", cmd: `echo a; echo b`, want: ""},
	{tool: "Bash", cmd: `sed -e 's/a/b/;s/c/d/' f.txt`, want: "", note: "; inside a sed expression"},
	{tool: "Bash", cmd: `sed 's/x/y/;s/p/q/g' f.txt | head`, want: "", note: "; inside a sed expression"},
	{tool: "Bash", cmd: `echo "a; b"`, want: "", note: "; inside quotes"},
	{tool: "Bash", cmd: `ls > out.txt`, want: ""},
	{tool: "Bash", cmd: `ls >> out.txt`, want: ""},
	{tool: "Bash", cmd: `ls 2> err.txt`, want: ""},
	{tool: "Bash", cmd: `ls nope 2>/dev/null`, want: "", note: "> redirection that writes no file"},
	{tool: "Bash", cmd: `go vet ./... 2>&1 | tee out.txt`, want: "", note: "> redirection that writes no file"},
	{tool: "Bash", cmd: `echo oops >&2`, want: "", note: "> redirection that writes no file"},
	{tool: "Bash", cmd: `echo "a > b"`, want: "", note: "> inside quotes"},
	{tool: "Bash", cmd: `grep -E 'x->y' f`, want: "", note: "> inside quotes"},
	{tool: "PowerShell", cmd: `ls > out.txt; echo a`, want: ""},
	// cmake
	{tool: "Bash", cmd: `cmake --build build`, cwd: `D:\proj`, want: "cmake-preset"},
	{tool: "Bash", cmd: `cmake -S . -B build`, cwd: `D:\proj`, want: "cmake-preset"},
	{tool: "Bash", cmd: `cmake --build --preset dev`, cwd: `D:\proj`, want: ""},
	{tool: "Bash", cmd: `cmake --preset dev`, cwd: `D:\proj`, want: ""},
	{tool: "Bash", cmd: `cmake -E copy a b`, cwd: `D:\proj`, want: ""},
	{tool: "Bash", cmd: `cmake --build build`, cwd: `D:\plain`, want: ""},
	// gh api
	{tool: "Bash", cmd: `gh api repos/o/r/pulls/1`, want: "gh-api-get"},
	{tool: "Bash", cmd: `gh api -X POST repos/o/r/issues`, want: "gh-api-get"},
	{tool: "Bash", cmd: `gh api -X GET repos/o/r/pulls/1`, want: ""},
	{tool: "Bash", cmd: `gh api -X GET repos/o/r/pulls/12/comments`, want: "gh-pr-comments"},
	{tool: "Bash", cmd: `gh api -X GET repos/o/r/pulls/12/comments | jq .`, want: ""},
	{tool: "Bash", cmd: `echo x | gh api repos/o/r/issues --input -`, want: "gh-api-get", note: "gh api anywhere, not only first"},
	// file rules
	{tool: "Edit", path: `D:\proj\CMakePresets.json`, newStr: "x", want: "vcpkg-files"},
	{tool: "Write", path: `D:/proj/vcpkg.json`, content: "{}", want: "vcpkg-files"},
	{tool: "Write", path: `D:/proj/triplets/x64.cmake`, content: "x", want: "vcpkg-files"},
	{tool: "Write", path: `D:/proj/ports/zlib/portfile.cmake`, content: "x", want: "vcpkg-files"},
	{tool: "Write", path: `D:/proj/a.md`, content: "a \u2014 b", want: "no-em-dash"},
	{tool: "Edit", path: `D:/proj/a.md`, newStr: "a \u2014 b", want: "no-em-dash"},
	{tool: "Write", path: `D:/proj/a.md`, content: "a - b", want: ""},
	{tool: "Write", path: `D:/proj/a.css`, content: "a{color:red !important}", want: "no-css-important"},
	{tool: "Write", path: `D:/proj/a.md`, content: "!important", want: ""},
	{tool: "Write", path: `D:/proj/default.env`, content: "A=1", want: "default-env"},
	{tool: "Write", path: `D:/proj/.env`, content: "A=1", want: "no-env-files"},
	{tool: "Write", path: `D:/proj/.env.local`, content: "A=1", want: "no-env-files"},
	{tool: "Edit", path: `D:/proj/prod.env`, newStr: "A=1", want: "no-env-files"},
	{tool: "Write", path: `D:/proj/default.env`, content: "a \u2014 b", want: "no-em-dash"},
	// tools nothing applies to
	{tool: "Read", path: `D:/proj/.env`, want: ""},
	{tool: "Grep", want: ""},
}

func TestRules(t *testing.T) {
	rules, err := Builtin()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range calls {
		d := rules.Evaluate(c.input(), env0)
		if d.Rule != c.want {
			t.Errorf("%s %q%s: want rule %q, got %q (%s)", c.tool, c.cmd, c.path, c.want, d.Rule, d.Reason)
		}
	}
}

func TestSubagentSwitch(t *testing.T) {
	rules, _ := Builtin()
	in := call{tool: "Agent", subtype: "general-purpose"}.input()
	for _, v := range []string{"0", "off", "False", "no"} {
		e := env0
		e.vars = map[string]string{"ATRIUM_ONLY_SUBAGENTS": v}
		if d := rules.Evaluate(in, e); d.Action != "" {
			t.Errorf("ATRIUM_ONLY_SUBAGENTS=%s still refuses: %s", v, d.Rule)
		}
	}
}

func TestReasonNamesWhatMatched(t *testing.T) {
	rules, _ := Builtin()
	d := rules.Evaluate(call{tool: "PowerShell", cmd: `mkdir D:\zz-new`}.input(), env0)
	if !strings.Contains(d.Reason, `'D:\zz-new'`) {
		t.Fatalf("reason does not name the path: %s", d.Reason)
	}
}

func TestBuiltinRulesLoad(t *testing.T) {
	rs, err := Builtin()
	if err != nil {
		t.Fatal(err)
	}
	if len(rs.List) < 20 {
		t.Fatalf("only %d rules", len(rs.List))
	}
}

func TestBrokenRulesRefuseToLoad(t *testing.T) {
	for name, raw := range map[string]string{
		"not json":      `{`,
		"empty":         `{"rules":[]}`,
		"unknown check": `{"rules":[{"id":"a","check":"nope","tools":["shell"],"reason":"r"}]}`,
		"unknown field": `{"rules":[{"id":"a","check":"command","tools":["shell"],"reason":"r","typo":1}]}`,
		"no reason":     `{"rules":[{"id":"a","check":"command","tools":["shell"]}]}`,
		"no tools":      `{"rules":[{"id":"a","check":"command","reason":"r"}]}`,
		"bad regexp":    `{"rules":[{"id":"a","check":"path","tools":["Write"],"reason":"r","params":{"patterns":["("]}}]}`,
		"bad decision":  `{"rules":[{"id":"a","check":"command","tools":["shell"],"reason":"r","decision":"maybe"}]}`,
		"duplicate id":  `{"rules":[{"id":"a","check":"command","tools":["shell"],"reason":"r"},{"id":"a","check":"command","tools":["shell"],"reason":"r"}]}`,
	} {
		if _, err := Load([]byte(raw)); err == nil {
			t.Errorf("%s: loaded", name)
		}
	}
}

// A check that panics denies the call rather than letting it through.
func TestPanicDenies(t *testing.T) {
	table := map[string]checkFunc{"boom": func(*evalCtx, *Rule) (bool, string) { panic("kaboom") }}
	rs, err := loadWith([]byte(`{"rules":[{"id":"b","check":"boom","tools":["shell"],"reason":"r"}]}`), table)
	if err != nil {
		t.Fatal(err)
	}
	d := rs.Evaluate(call{tool: "Bash", cmd: "ls"}.input(), env0)
	if d.Action != "deny" || !strings.Contains(d.Reason, "kaboom") {
		t.Fatalf("got %+v", d)
	}
}

// Anything the shell parsers are handed comes back as a script, never a panic.
func TestParsersSurviveJunk(t *testing.T) {
	for _, src := range []string{
		"", "'", `"`, "$(", "((", "}", "@'\nx", "`", "<#", "a |", "&& b", "cat <<EOF\nx",
		"if then fi", "x=$((1+))", "echo ${", "Write-Output (git push", "{ git push",
	} {
		for _, sh := range []string{"bash", "pwsh"} {
			_ = Parse(src, sh, "", env0)
		}
	}
}

func TestParseSeesNestedCommands(t *testing.T) {
	for _, tc := range []struct{ sh, src string }{
		{"bash", "echo $(git push)"},
		{"bash", "echo `git push`"},
		{"bash", "x=$(git push) true"},
		{"bash", "(git push)"},
		{"bash", "if true; then git push; fi"},
		{"bash", "bash -lc 'git push'"},
		{"bash", "sh <<EOF\ngit push\nEOF"},
		{"bash", "timeout 5 git push"},
		{"bash", "eval git push"},
		{"pwsh", "Write-Output $(git push)"},
		{"pwsh", "& git push"},
		{"pwsh", "1..2 | % { git push }"},
		{"pwsh", "\"$(git push)\""},
		{"pwsh", "Invoke-Expression 'git push'"},
		{"pwsh", "cmd /c git push"},
		{"pwsh", "pwsh -EncodedCommand ZwBpAHQAIABwAHUAcwBoAA=="},
	} {
		s := Parse(tc.src, tc.sh, "", env0)
		found := false
		for _, c := range s.Cmds {
			if c.Name() == "git" && len(c.Args()) > 0 && c.Args()[0].Lit == "push" {
				found = true
			}
		}
		if !found {
			t.Errorf("%s %q: git push not seen", tc.sh, tc.src)
		}
	}
}

func TestUserAgentsDirExempt(t *testing.T) {
	rules, _ := Builtin()
	e := env0
	e.dirs = map[string]bool{`C:\Users\claude\.claude\agents\my-reviewer.md`: false, `C:\Users\claude\.claude\agents\adir.md`: true}
	e.vars = map[string]string{"USERPROFILE": "C:/Users/claude"}
	cases := []struct{ sub, want string }{
		{"my-reviewer", ""},
		{"other", "only-atrium-subagents"},
		{"adir", "only-atrium-subagents"},
		{"general-purpose", "only-atrium-subagents"},
	}
	for _, c := range cases {
		if d := rules.Evaluate(call{tool: "Agent", subtype: c.sub}.input(), e); d.Rule != c.want {
			t.Errorf("subagent %q: want %q, got %q", c.sub, c.want, d.Rule)
		}
	}
}

func TestCheckoutStaysInHome(t *testing.T) {
	rules, _ := Builtin()
	e := env0
	e.vars = map[string]string{"ATRIUM_HOME": "D:/tmp/cards/c1"}
	cwd := `D:\tmp\cards\c1\work`
	cases := []struct{ tool, cmd, want string }{
		{"Bash", `git worktree add D:/worktrees/claude/reviews/x claude/x`, "checkout-home"},
		{"Bash", `git worktree add -b claude/x ../../other`, "checkout-home"},
		{"Bash", `git worktree add $DIR claude/x`, "checkout-home"},
		{"Bash", `git worktree move ../tmp/a D:/worktrees/a`, "checkout-home"},
		{"Bash", `git clone https://github.com/o/r.git D:/worktrees/r`, "checkout-home"},
		{"Bash", `cd D:/worktrees && git clone https://github.com/o/r.git`, "checkout-home"},
		{"PowerShell", `git clone --depth 1 https://github.com/o/r D:\tmp\r`, "checkout-home"},
		{"PowerShell", `gwt new some.work`, "checkout-home"},
		{"Bash", `gwt pr 4397`, "checkout-home"},
		{"Bash", `git worktree add -b claude/x ../tmp/x`, ""},
		{"Bash", `git worktree add D:/tmp/cards/c1/tmp/x claude/x`, ""},
		{"PowerShell", `git worktree add d:\TMP\Cards\c1\tmp\x claude/x`, ""},
		{"Bash", `git clone https://github.com/o/r.git ../tmp/r`, ""},
		{"Bash", `git clone https://github.com/o/r.git`, ""},
		{"Bash", `git worktree list`, ""},
		{"Bash", `git worktree remove D:/worktrees/x`, ""},
		{"Bash", `gwt sessions`, ""},
		{"Bash", `gh repo clone o/r D:/tmp/other/r`, "checkout-home"},
		{"Bash", `gh repo clone o/r $DIR -- --depth 1`, "checkout-home"},
		{"Bash", `gh repo clone o/r ../tmp/r`, ""},
		{"Bash", `gh repo clone o/r`, ""},
		{"Bash", `gh repo clone o/r -- --depth 1`, ""},
	}
	for _, c := range cases {
		cmd, cwd := c.cmd, cwd
		if runtime.GOOS != "windows" {
			// A drive path and a backslash mean nothing to a posix host, so only the forward-slash cases run there.
			if strings.Contains(cmd, `\`) {
				continue
			}
			cmd, cwd = strings.ReplaceAll(cmd, "D:/", "/"), "/tmp/cards/c1/work"
			e.vars = map[string]string{"ATRIUM_HOME": "/tmp/cards/c1"}
		}
		if d := rules.Evaluate(call{tool: c.tool, cmd: cmd, cwd: cwd}.input(), e); d.Rule != c.want {
			t.Errorf("%s %q: want %q, got %q (%s)", c.tool, c.cmd, c.want, d.Rule, d.Reason)
		}
	}
	if d := rules.Evaluate(call{tool: "Bash", cmd: `git worktree add D:/worktrees/x`, cwd: cwd}.input(), env0); d.Rule != "" {
		t.Errorf("a session with no home was checked: %s", d.Rule)
	}
}

func TestWritesStayInTheCardFolder(t *testing.T) {
	rules, err := Load([]byte(outsideOnly))
	if err != nil {
		t.Fatal(err)
	}
	e := env0
	e.vars = map[string]string{"ATRIUM_CARD_ROOT": "V:/cards", "ATRIUM_HOME": "V:/cards/scratch/c1"}
	cwd := "V:/cards/scratch/c1/work"
	cases := []struct{ tool, cmd, path, want string }{
		{"Bash", `git clone https://x/r.git V:/cards/lb`, "", "outside-card-folder"},
		{"Bash", `mkdir -p V:/cards/lb/tools`, "", "outside-card-folder"},
		{"Bash", `touch V:/cards/lb/a`, "", "outside-card-folder"},
		{"Bash", `mkdir ../../lb`, "", "outside-card-folder"},
		{"Bash", `echo hi > V:/cards/lb/a`, "", "outside-card-folder"},
		{"Bash", `echo hi | tee V:/cards/other/a`, "", "outside-card-folder"},
		{"Bash", `cp a.txt V:/cards/lb/`, "", "outside-card-folder"},
		{"Bash", `tar xf a.tar -C V:/cards/lb && mv V:/cards/scratch/c1/tmp/a V:/cards/lb/a`, "", "outside-card-folder"},
		{"PowerShell", `New-Item -ItemType Directory V:/cards/lb`, "", "outside-card-folder"},
		{"Write", "", "V:/cards/lb/a.txt", "outside-card-folder"},
		{"Edit", "", "V:/cards/scratch/c2/work/a.txt", "outside-card-folder"},
		{"Write", "", "../../lb/a.txt", "outside-card-folder"},
		{"Bash", `mkdir -p V:/cards/scratch/c1/tmp/x`, "", ""},
		{"Bash", `mkdir tools`, "", ""},
		{"Bash", `echo hi > ../tmp/a`, "", ""},
		{"Bash", `cp V:/cards/lb/a.txt .`, "", ""},
		{"Bash", `ls V:/cards/lb`, "", ""},
		{"Bash", `mkdir V:/elsewhere/x`, "", ""},
		{"Bash", `mkdir $DIR`, "", ""},
		{"Write", "", "V:/cards/scratch/c1/work/a.txt", ""},
		{"Write", "", "V:/elsewhere/a.txt", ""},
	}
	for _, c := range cases {
		if d := rules.Evaluate(call{tool: c.tool, cmd: c.cmd, path: c.path, cwd: cwd}.input(), e); d.Rule != c.want {
			t.Errorf("%s %q %q: want %q, got %q (%s)", c.tool, c.cmd, c.path, c.want, d.Rule, d.Reason)
		}
	}
	for _, vars := range []map[string]string{nil, {"ATRIUM_HOME": "V:/cards/scratch/c1"}} {
		e.vars = vars
		if d := rules.Evaluate(call{tool: "Bash", cmd: `mkdir V:/cards/lb`, cwd: cwd}.input(), e); d.Rule == "outside-card-folder" {
			t.Errorf("a session with no card root was checked: %v", vars)
		}
	}
}

// outsideOnly is the one rule, so a drive path in a case does not meet the drive-root rule first.
const outsideOnly = `{"rules":[{"id":"outside-card-folder","check":"outside-home","tools":["shell","Write","Edit"],
"params":{"env":"ATRIUM_CARD_ROOT","dir":"ATRIUM_HOME"},"reason":"no {name}"}]}`

func TestTargetPathsOfWritingCommands(t *testing.T) {
	rules, err := Load([]byte(outsideOnly))
	if err != nil {
		t.Fatal(err)
	}
	e := env0
	e.vars = map[string]string{"ATRIUM_CARD_ROOT": "V:/localai/cards", "ATRIUM_HOME": "V:/localai/cards/scratch/c1"}
	const cwd = "V:/localai/cards/scratch/c1/work"
	const hit = "outside-card-folder"
	cases := []struct{ tool, cmd, cwd, want string }{
		{"Bash", `git clone https://github.com/o/r.git V:/localai/cards/lb`, cwd, hit},
		{"Bash", `git clone https://github.com/o/r.git 'v:\LocalAI\Cards\LB'`, cwd, hit},
		{"Bash", `git clone https://github.com/o/r.git ../../lb`, cwd, hit},
		{"Bash", `git clone https://github.com/o/r.git`, "V:/localai/cards/lb", hit},
		{"Bash", `git -C V:/localai/cards/lb worktree add V:/LOCALAI/cards/lb/w claude/x`, cwd, hit},
		{"Bash", `git clone https://github.com/o/r.git V:/localai/cards/scratch/c1/tmp/r`, cwd, ""},
		{"Bash", `git clone https://github.com/o/r.git`, cwd, ""},

		{"Bash", `tar -xf a.tar -C V:/localai/cards/lb`, cwd, hit},
		{"Bash", `tar xzf a.tgz -C 'v:\localai\CARDS\lb'`, cwd, hit},
		{"Bash", `tar -xzf a.tgz -CV:/localai/cards/lb`, cwd, hit},
		{"Bash", `tar --extract --file=a.tar --directory=V:/localai/cards/lb`, cwd, hit},
		{"Bash", `tar -xf a.tar --directory V:/localai/cards/lb`, cwd, hit},
		{"Bash", `tar -xf a.tar`, "V:/localai/cards/lb", hit},
		{"Bash", `tar -czf V:/localai/cards/lb/out.tgz src`, cwd, hit},
		{"Bash", `tar -xf a.tar -C V:/localai/cards/scratch/c1/tmp/x`, cwd, ""},
		{"Bash", `tar -xf a.tar`, cwd, ""},
		{"Bash", `tar -tf V:/localai/cards/lb/a.tar`, cwd, ""},
		{"Bash", `tar -czf ../tmp/out.tgz src`, cwd, ""},

		{"Bash", `unzip a.zip -d V:/localai/cards/lb`, cwd, hit},
		{"Bash", `unzip -o a.zip -dV:/localai/cards/lb`, cwd, hit},
		{"Bash", `unzip a.zip -d 'v:\LOCALAI\cards\lb'`, cwd, hit},
		{"Bash", `unzip a.zip`, "V:/localai/cards/lb", hit},
		{"Bash", `unzip a.zip -d V:/localai/cards/scratch/c1/tmp/x`, cwd, ""},
		{"Bash", `unzip -l V:/localai/cards/lb/a.zip`, "V:/localai/cards/lb", ""},
		{"Bash", `unzip a.zip`, cwd, ""},

		{"Bash", `curl -o V:/localai/cards/lb/a.bin https://x/a`, cwd, hit},
		{"Bash", `curl -sSLo V:/localai/cards/lb/a.bin https://x/a`, cwd, hit},
		{"Bash", `curl --output='v:\localai\CARDS\lb\a.bin' https://x/a`, cwd, hit},
		{"Bash", `curl --output V:/localai/cards/lb/a.bin https://x/a`, cwd, hit},
		{"Bash", `curl -O https://x/a.bin`, "V:/localai/cards/lb", hit},
		{"Bash", `curl -O --output-dir V:/localai/cards/lb https://x/a.bin`, cwd, hit},
		{"Bash", `curl -o V:/localai/cards/scratch/c1/tmp/a.bin https://x/a`, cwd, ""},
		{"Bash", `curl -O https://x/a.bin`, cwd, ""},
		{"Bash", `curl https://x/a`, "V:/localai/cards/lb", ""},

		{"PowerShell", `Expand-Archive -Path a.zip -DestinationPath V:\localai\cards\lb`, cwd, hit},
		{"PowerShell", `expand-archive a.zip v:\LOCALAI\cards\lb -Force`, cwd, hit},
		{"PowerShell", `Expand-Archive -Path a.zip -Dest V:/localai/cards/lb`, cwd, hit},
		{"PowerShell", `Expand-Archive -DestinationPath:V:/localai/cards/lb a.zip`, cwd, hit},
		{"PowerShell", `Expand-Archive -Path a.zip -DestinationPath V:\localai\cards\scratch\c1\tmp\x`, cwd, ""},

		{"PowerShell", `Invoke-WebRequest https://x/a -OutFile V:\localai\cards\lb\a.zip`, cwd, hit},
		{"PowerShell", `iwr -uri https://x/a -outfile v:\LOCALAI\Cards\lb\a.zip`, cwd, hit},
		{"PowerShell", `Invoke-RestMethod https://x/a -OutFile:V:/localai/cards/lb/a.zip`, cwd, hit},
		{"PowerShell", `curl.exe -o V:\localai\cards\lb\a.zip https://x/a`, cwd, hit},
		{"PowerShell", `Invoke-WebRequest https://x/a -OutFile V:\localai\cards\scratch\c1\tmp\a.zip`, cwd, ""},
		{"PowerShell", `Invoke-WebRequest https://x/a -OutVariable r`, cwd, ""},

		{"Bash", `tar -xf a.tar -C $DIR`, cwd, ""},
		{"Bash", `tar -xf a.tar -C V:/elsewhere/x`, cwd, ""},
	}
	for _, c := range cases {
		if d := rules.Evaluate(call{tool: c.tool, cmd: c.cmd, cwd: c.cwd}.input(), e); d.Rule != c.want {
			t.Errorf("%s %q in %s: want %q, got %q (%s)", c.tool, c.cmd, c.cwd, c.want, d.Rule, d.Reason)
		}
	}
}
