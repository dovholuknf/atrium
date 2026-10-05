package requirements

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// Section 2.1 of the design, verbatim.
func TestTheDesignsOwnFileParsesToTheExpectedJSON(t *testing.T) {
	data, err := os.ReadFile("testdata/atrium.requirements.yaml")
	if err != nil {
		t.Fatal(err)
	}
	f, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := json.Marshal(f)
	const want = `{"version":1,"atrium":{"min":"7eb1555"},` +
		`"git":{"base":"claude/main","mirror":"hub-main","clone":"{home}/git/github/{owner}/{repo}",` +
		`"worktrees":"{clone}-worktrees","fresh":true},` +
		`"toolchain":{"git":{"min":"2.39","windows":"git-for-windows"},"go":{"from":"go.mod"},` +
		`"node":{"min":"24"},"pwsh":{"min":"7","os":["windows"]}},` +
		`"runners":{"claude":{"hooks":"atrium","gate":"required","mcp":["atrium-control"],"smoke":true},` +
		`"codex":{"helpers":["codex-code-mode-host"],"smoke":true}},` +
		`"room":{"survives":"none","runner_auth":["claude","codex"]},"env":{},"services":[],"forges":{}}`
	if string(got) != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

func TestStatuslineRequirementParsesAndRefusesOtherValues(t *testing.T) {
	f, err := Parse([]byte("version: 1\nrunners:\n  claude:\n    statusline: required\n"))
	if err != nil {
		t.Fatal(err)
	}
	if f.Runners["claude"].Statusline != "required" {
		t.Errorf("runner: %+v", f.Runners["claude"])
	}
	_, err = Parse([]byte("version: 1\nrunners:\n  claude:\n    statusline: ./x.sh\n"))
	if err == nil || !strings.Contains(err.Error(), `runners.claude.statusline: "./x.sh" is not one of: required`) {
		t.Errorf("a command was accepted or misreported: %v", err)
	}
}

func TestDefaultsAndPlainValuesKeepTheirText(t *testing.T) {
	f, err := Parse([]byte("version: 1\ngit: { base: main }\ntoolchain:\n  go: { min: 1.30 }\nenv:\n  TZ: UTC\n  KEY_NAME: { required: true }\n"))
	if err != nil {
		t.Fatal(err)
	}
	if f.Git.Mirror != "hub-main" || f.Git.Clone != "{home}/git/github/{owner}/{repo}" || f.Git.Worktrees != "{clone}-worktrees" {
		t.Errorf("defaults: %+v", f.Git)
	}
	if f.Toolchain["go"].Min != "1.30" {
		t.Errorf("an unquoted 1.30 became %q", f.Toolchain["go"].Min)
	}
	if f.Env["TZ"].Value != "UTC" || !f.Env["KEY_NAME"].Required {
		t.Errorf("env: %+v", f.Env)
	}
}

func TestRefusals(t *testing.T) {
	cases := []struct {
		name, yaml, want string
	}{
		{"an unknown top level key", "version: 1\ntoolchian: {}\n", `:2: toolchian: unknown key`},
		{"an unknown nested key", "version: 1\nrunners:\n  claude:\n    smok: true\n", `:4: runners.claude.smok: unknown key`},
		{"no version", "git: { base: main }\n", `version: required`},
		{"another version", "version: 2\n", `version: "2" is not a version`},
		{"an absolute clone path", "version: 1\ngit: { base: main, clone: /home/me/x }\n", `:2: git.clone: "/home/me/x" is an absolute path`},
		{"a windows drive path", "version: 1\ngit: { base: main, clone: 'C:\\x' }\n", `git.clone: "C:\\x" is an absolute path`},
		{"a home relative path", "version: 1\ngit: { base: main, clone: '~/x' }\n", `is an absolute path`},
		{"an absolute path anywhere", "version: 1\nenv:\n  WHERE: /opt/x\n", `:3: env.WHERE: "/opt/x" is an absolute path`},
		{"an unknown template", "version: 1\ngit: { base: main, clone: '{root}/x' }\n", `unknown template {root}`},
		{"clone template outside worktrees", "version: 1\ngit: { base: main, clone: '{clone}/x' }\n", `{clone} is only meaningful in worktrees`},
		{"a parent directory", "version: 1\ngit: { base: main, clone: '{home}/../x' }\n", `parent directory`},
		{"git without base", "version: 1\ngit: { fresh: true }\n", `git.base: required`},
		{"a github token", "version: 1\nenv:\n  X: ghp_abcdefghijklmnopqrstuvwxyz0123456789\n", `env.X: looks like a secret`},
		{"a url with a password", "version: 1\nenv:\n  X: https://u:pw@example.com/x\n", `looks like a secret`},
		{"a private key", "version: 1\nenv:\n  X: '-----BEGIN PRIVATE KEY-----'\n", `looks like a secret`},
		{"a long token-like run", "version: 1\nenv:\n  X: Zm9vYmFyMTIzNDU2Nzg5MGFiY2RlZjEyMzQ1Ng\n", `looks like a secret`},
		{"a secret-named variable with a value", "version: 1\nenv:\n  API_TOKEN: hunter2\n", `env.API_TOKEN: API_TOKEN reads as a secret`},
		{"a hook that names a command", "version: 1\nrunners:\n  claude:\n    hooks: ./evil.sh\n", `runners.claude.hooks: "./evil.sh" is not one of: atrium`},
		{"a helper that is a path", "version: 1\nrunners:\n  codex:\n    helpers: [../bin/x]\n", `runners.codex.helpers[0]: "../bin/x" is not a plain name`},
		{"a service that is a url", "version: 1\nservices: [https://internal.example]\n", `services[0]`},
		{"a bad survives", "version: 1\nroom: { survives: forever }\n", `room.survives: "forever" is not one of: none, logoff, reboot`},
		{"a bad sha", "version: 1\natrium: { min: main }\n", `atrium.min: "main" is not a commit sha`},
		{"an alias", "version: 1\nenv:\n  A: &a { required: true }\n  B: *a\n", `anchors and aliases are not allowed`},
		{"a duplicate key", "version: 1\nversion: 1\n", `version: given twice`},
		{"two documents", "version: 1\n---\nversion: 1\n", `more than one YAML document`},
		{"broken yaml", "version: [1\n", `yaml:`},
		{"nothing at all", "", `empty`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Parse([]byte(c.yaml))
			if err == nil {
				t.Fatalf("accepted:\n%s", c.yaml)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error %q does not contain %q", err, c.want)
			}
		})
	}
}

func TestEveryProblemIsReportedInFileOrder(t *testing.T) {
	_, err := Parse([]byte("version: 1\nzzz: 1\ngit: { base: main, clone: /abs }\naaa: 2\n"))
	var bad *Error
	if err == nil {
		t.Fatal("accepted")
	}
	bad = err.(*Error)
	if len(bad.Problems) != 3 || !strings.Contains(bad.Problems[0], ":2:") ||
		!strings.Contains(bad.Problems[1], ":3:") || !strings.Contains(bad.Problems[2], ":4:") {
		t.Fatalf("problems: %q", bad.Problems)
	}
}

func TestATooLargeFileIsRefused(t *testing.T) {
	if _, err := Parse([]byte("version: 1\n# " + strings.Repeat("x", MaxSize))); err == nil {
		t.Fatal("accepted a file over the limit")
	}
}

func TestForgesParseAndRefuse(t *testing.T) {
	f, err := Parse([]byte("version: 1\nforges:\n  gh: { host: github.com, scopes: [repo, read:org] }\n  bb: { host: bitbucket.org }\n"))
	if err != nil {
		t.Fatal(err)
	}
	if g := f.Forges["gh"]; g.Host != "github.com" || len(g.Scopes) != 2 || g.Scopes[1] != "read:org" {
		t.Fatalf("gh: %+v", g)
	}
	if g := f.Forges["bb"]; g.Host != "bitbucket.org" || len(g.Scopes) != 0 {
		t.Fatalf("bb: %+v", g)
	}
	for name, in := range map[string]string{
		"unknown cli":    "forges:\n  svn: { host: x.org }\n",
		"no host":        "forges:\n  gh: { scopes: [repo] }\n",
		"path as host":   "forges:\n  gh: { host: x.org/y }\n",
		"token in scope": "forges:\n  gh: { host: x.org, scopes: [ghp_abcdefghijklmnopqrstuvwxyz0123456789] }\n",
		"unknown key":    "forges:\n  gh: { host: x.org, token: abc }\n",
	} {
		if _, err := Parse([]byte("version: 1\n" + in)); err == nil {
			t.Errorf("%s: parsed, want a refusal", name)
		}
	}
}
