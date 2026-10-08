package roomspec

import (
	"encoding/json"
	"strings"
	"testing"
)

const winSpec = `version: 1
name: sg3
os: windows
account: SG3\localai
work_root: V:/localai
`

const linSpec = `version: 1
name: pi
os: linux
account: localai
work_root: /srv/localai
caches: [npm, go]
packs:
  - runner: claude
    repo: dovholuknf/agents
`

func TestParseValid(t *testing.T) {
	s, err := ParseFor([]byte(winSpec), Windows)
	if err != nil {
		t.Fatal(err)
	}
	p := s.Resolve()
	if p.Root != "V:/localai" || p.Git != "V:/localai/git" || p.Cache != "V:/localai/cache" || p.Handoff != "V:/localai/handoff" || p.Reviews != "V:/localai/reviews" {
		t.Fatalf("paths: %+v", p)
	}
	if len(s.Hash) != 64 {
		t.Fatalf("hash %q", s.Hash)
	}
	l, err := ParseFor([]byte(linSpec), Linux)
	if err != nil {
		t.Fatal(err)
	}
	pk, ok := l.PackFor("claude")
	if !ok || pk.Branch != "main" || pk.From != "claude" {
		t.Fatalf("pack defaults: %+v", pk)
	}
	if got := l.CacheSet(); len(got) != 2 {
		t.Fatalf("caches %v", got)
	}
	if got := s.CacheSet(); len(got) != len(CacheNames()) {
		t.Fatalf("an empty caches means all: %v", got)
	}
}

func TestParseHashIsOfTheBytes(t *testing.T) {
	a, _ := ParseFor([]byte(winSpec), Windows)
	b, _ := ParseFor([]byte(winSpec+"\n"), Windows)
	if a.Hash == b.Hash {
		t.Fatal("a different file must have a different hash")
	}
}

func TestParseRefuses(t *testing.T) {
	cases := []struct{ name, goos, spec, want string }{
		{"wrong os", Linux, winSpec, "this machine is linux"},
		{"unknown key", Windows, winSpec + "bogus: 1\n", "bogus"},
		{"two documents", Windows, winSpec + "---\nversion: 1\n", "more than one document"},
		{"version", Windows, strings.Replace(winSpec, "version: 1", "version: 2", 1), "version"},
		{"relative work root", Windows, strings.Replace(winSpec, "V:/localai", "localai", 1), "work_root"},
		{"unc work root", Windows, strings.Replace(winSpec, "V:/localai", `\\\\srv\\share`, 1), "network path"},
		{"drive root", Windows, strings.Replace(winSpec, "V:/localai", "V:/", 1), "filesystem root"},
		{"dotdot", Windows, strings.Replace(winSpec, "V:/localai", "V:/a/../b", 1), ".."},
		{"unix root", Linux, strings.Replace(linSpec, "/srv/localai", "/", 1), "filesystem root"},
		{"drive on linux", Linux, strings.Replace(linSpec, "/srv/localai", "V:/x", 1), "drive path"},
		{"semicolon", Linux, strings.Replace(linSpec, "/srv/localai", `"/srv/a;b"`, 1), "semicolon"},
		{"unknown cache", Linux, strings.Replace(linSpec, "[npm, go]", "[npm, ruby]", 1), "not a known cache"},
		{"option as branch", Linux, linSpec + "    branch: -x\n", "branch"},
		{"dotdot branch", Linux, linSpec + "    branch: a..b\n", "branch"},
		{"bad repo", Linux, strings.Replace(linSpec, "dovholuknf/agents", "--upload-pack=x", 1), "repo"},
		{"absolute from", Linux, linSpec + "    from: /etc\n", "from"},
		{"token key", Linux, linSpec + "token: abc\n", "credential"},
		{"deep password key", Linux, linSpec + "    password: abc\n", "credential"},
		{"secret in a list", Linux, strings.Replace(linSpec, "caches: [npm, go]", "caches: [npm]\nextra:\n  - client_secret: x", 1), "credential"},
		{"unknown runner", Linux, linSpec + "runners: [vim]\n", "runner"},
		{"bad name", Linux, strings.Replace(linSpec, "name: pi", "name: -pi", 1), "name"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ParseFor([]byte(c.spec), c.goos)
			if err == nil {
				t.Fatalf("accepted:\n%s", c.spec)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error %q does not say %q", err, c.want)
			}
		})
	}
}

func TestCheckPackArgIsTheScriptsRule(t *testing.T) {
	for _, ok := range [][2]string{{"o/r", "main"}, {"o/r.x", "feature/a-b_c"}} {
		if why := CheckPackArg(ok[0], ok[1]); why != "" {
			t.Errorf("%v refused: %s", ok, why)
		}
	}
	for _, bad := range [][2]string{{"o/r", "-x"}, {"o/r", "a..b"}, {"o", "main"}, {"o/r/x", "main"}, {"-o/r", "main"}, {"o/r", ""}} {
		if CheckPackArg(bad[0], bad[1]) == "" {
			t.Errorf("%v accepted", bad)
		}
	}
}

func TestLockKeysAreTheContract(t *testing.T) {
	b, err := MarshalLock(Lock{Version: 1, SpecHash: "h", OS: "linux", Account: "a", WorkRoot: "/w"})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"version", "spec_hash", "os", "account", "work_root", "steps", "admin_lines", "packs"} {
		if _, ok := m[k]; !ok {
			t.Errorf("lock has no %q: %s", k, b)
		}
	}
	if _, ok := m["applied_at"]; ok {
		t.Error("a lock that was not applied has no applied_at")
	}
	for _, k := range []string{"steps", "admin_lines", "packs"} {
		if string(m[k]) != "[]" {
			t.Errorf("%s is %s, never null", k, m[k])
		}
	}
	if b[len(b)-1] != '\n' {
		t.Error("no final newline")
	}
	full := Lock{Version: 1, AppliedAt: "t", Steps: []Step{{"s", "ok", "d"}}, Packs: []PackLock{{"claude", "o/r", "c", 3}}}
	b, _ = MarshalLock(full)
	for _, k := range []string{`"step"`, `"status"`, `"detail"`, `"runner"`, `"repo"`, `"commit"`, `"files"`, `"applied_at"`} {
		if !strings.Contains(string(b), k) {
			t.Errorf("no %s in %s", k, b)
		}
	}
	if _, err := ParseLock(b); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseLock([]byte(`{"version":9}`)); err == nil {
		t.Fatal("another version accepted")
	}
}

func TestCode(t *testing.T) {
	st := func(s ...string) []Step {
		var o []Step
		for _, x := range s {
			o = append(o, Step{Status: x})
		}
		return o
	}
	for _, c := range []struct {
		in   []Step
		want int
	}{{st("ok", "done", "warn", "todo"), 0}, {st("ok", "fail"), 3}, {st("fail", "human"), 13}, {nil, 0}} {
		if got := Code(c.in); got != c.want {
			t.Errorf("%v: %d want %d", c.in, got, c.want)
		}
	}
}

func TestEditors(t *testing.T) {
	// kv: replaces the first, drops a repeat, keeps the rest, appends when missing
	got := EditKV([]string{"a=1", "cache=x", "b=2", "cache = y"}, "cache", "Z")
	if strings.Join(got, "|") != "a=1|cache=Z|b=2" {
		t.Fatalf("kv: %v", got)
	}
	if got := EditKV(nil, "k", "v"); len(got) != 1 || got[0] != "k=v" {
		t.Fatalf("kv new: %v", got)
	}
	// ini
	got = EditINI([]string{"[other]", "cache-dir = no", "[global]", "x = 1", "cache-dir = old"}, "global", "cache-dir", "/c")
	if strings.Join(got, "|") != "[other]|cache-dir = no|[global]|cache-dir = /c|x = 1" {
		t.Fatalf("ini: %v", got)
	}
	if got := EditINI(nil, "global", "cache-dir", "/c"); strings.Join(got, "|") != "[global]|cache-dir = /c" {
		t.Fatalf("ini new: %v", got)
	}
	// profile: replaces its own line, not another key's, and quotes
	p := EditProfile([]string{"x", "export CARGO_HOME='/old'  " + ProfileMarker, "export OTHER='1'  " + ProfileMarker}, "CARGO_HOME", "/it's")
	if len(p) != 3 || p[2] != `export CARGO_HOME='/it'\''s'  `+ProfileMarker || p[1] != "export OTHER='1'  "+ProfileMarker {
		t.Fatalf("profile: %q", p)
	}
	// idempotent
	if !EqualLines(EditKV(got, "k", "v"), EditKV(EditKV(got, "k", "v"), "k", "v")) {
		t.Fatal("kv not idempotent")
	}
	// lines
	if l := SplitLines([]byte("\ufeffa\r\nb\r\n")); len(l) != 2 || l[0] != "a" {
		t.Fatalf("split: %q", l)
	}
	if string(JoinLines([]string{"a", "b"}, "\r\n")) != "a\r\nb\r\n" {
		t.Fatal("join")
	}
	if JoinLines(nil, "\n") != nil {
		t.Fatal("empty file")
	}
}
