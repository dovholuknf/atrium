package guard

import (
	"errors"
	"strings"
	"testing"
)

// fakeEnv answers from maps, for the rules that need no repository.
type fakeEnv struct {
	dirs  map[string]bool // existing paths; true for a directory
	vars  map[string]string
	agent string
}

func (e fakeEnv) Git(string, ...string) (string, error) { return "", errors.New("no git here") }
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
func (e fakeEnv) Getenv(k string) string         { return e.vars[k] }
func (e fakeEnv) Agent() string                  { return e.agent }

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
	// docker
	{tool: "Bash", cmd: `FOO=bar docker run x`, want: "docker-env-prefix"},
	{tool: "Bash", cmd: `docker run -e FOO=bar x`, want: ""},
	// semicolons and redirection, Bash tool only
	{tool: "Bash", cmd: `echo a; echo b`, want: "bash-semicolon"},
	{tool: "Bash", cmd: `sed -e 's/a/b/;s/c/d/' f.txt`, want: "", note: "; inside a sed expression"},
	{tool: "Bash", cmd: `sed 's/x/y/;s/p/q/g' f.txt | head`, want: "", note: "; inside a sed expression"},
	{tool: "Bash", cmd: `echo "a; b"`, want: "", note: "; inside quotes"},
	{tool: "Bash", cmd: `ls > out.txt`, want: "bash-redirect"},
	{tool: "Bash", cmd: `ls >> out.txt`, want: "bash-redirect"},
	{tool: "Bash", cmd: `ls 2> err.txt`, want: "bash-redirect"},
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
	checks["boom"] = func(*evalCtx, *Rule) (bool, string) { panic("kaboom") }
	defer delete(checks, "boom")
	rs, err := Load([]byte(`{"rules":[{"id":"b","check":"boom","tools":["shell"],"reason":"r"}]}`))
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
