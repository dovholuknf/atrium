//go:build integration

package roomspec

import (
	"strings"
	"testing"
)

// A variable that moves a runner's folder is followed only to an absolute place under the account's home or the work root, with
// a %NAME% of the Windows registry expanded first. Anything else is a fail row naming the variable, and nothing is installed.
func TestMovedPackFolderIsCheckedBeforeAnythingIsWritten(t *testing.T) {
	cases := []struct {
		goos, runner, key, val string
		want                   string // the folder the pack lands in, or "" for a fail row
	}{
		{Linux, "codex", "CODEX_HOME", "/home/localai/.codex-x", "/home/localai/.codex-x"},
		{Linux, "codex", "CODEX_HOME", "/srv/localai/codex", "/srv/localai/codex"},
		{Linux, "codex", "CODEX_HOME", "/home/x/.claude", ""},
		{Linux, "codex", "CODEX_HOME", "/home/localai/.claude", ""},
		{Linux, "codex", "CODEX_HOME", "/home/localai/.claude/sub", ""},
		{Linux, "codex", "CODEX_HOME", "/home/localai", ""},
		{Linux, "codex", "CODEX_HOME", "/home/localai/./.claude", ""},
		{Linux, "codex", "CODEX_HOME", "/home/localai//.claude", ""},
		{Linux, "codex", "CODEX_HOME", "//.claude", ""},
		{Linux, "codex", "CODEX_HOME", "/home/localai/./.codex-x/", "/home/localai/.codex-x"},
		{Linux, "codex", "CODEX_HOME", "/etc", ""},
		{Linux, "codex", "CODEX_HOME", "../../x", ""},
		{Linux, "codex", "CODEX_HOME", "/home/localai/../x", ""},
		{Linux, "codex", "CODEX_HOME", "/home/localai/$X", ""},
		{Linux, "codex", "CODEX_HOME", "%USERPROFILE%/.codex-x", ""},
		{Linux, "claude", "CLAUDE_CONFIG_DIR", "/home/localai/.codex", ""},
		{Linux, "gemini", "GEMINI_CLI_HOME", "/data/g", ""},
		{Windows, "codex", "CODEX_HOME", `%USERPROFILE%\.codex-x`, "C:/Users/localai/.codex-x"},
		{Windows, "codex", "CODEX_HOME", `C:\Users\localai\.codex-x`, "C:/Users/localai/.codex-x"},
		{Windows, "codex", "CODEX_HOME", `V:\localai\codex`, "V:/localai/codex"},
		{Windows, "codex", "CODEX_HOME", `%NOPE%\x`, ""},
		{Windows, "codex", "CODEX_HOME", `D:\other`, ""},
		{Windows, "codex", "CODEX_HOME", `.codex`, ""},
		{Windows, "codex", "CODEX_HOME", `C:\Users\localai\.\.claude`, ""},
		{Windows, "codex", "CODEX_HOME", `C:\Users\localai\\.claude`, ""},
		{Windows, "codex", "CODEX_HOME", `C:\Users\localai\.codex-x:stream`, ""},
		{Windows, "codex", "CODEX_HOME", `C:\Users\localai\CODEX~1`, ""},
		{Windows, "codex", "CODEX_HOME", `C:\Users\localai\.\.codex-x`, "C:/Users/localai/.codex-x"},
		{Windows, "codex", "CODEX_HOME", `\Users\localai\.codex-x`, ""},
		{Windows, "codex", "CODEX_HOME", `C:\Users\localai\..\other`, ""},
		{Windows, "codex", "CODEX_HOME", `%USERPROFILE%\.claude`, ""},
		{Windows, "codex", "CODEX_HOME", `c:\users\LOCALAI\.CLAUDE`, ""},
	}
	for _, c := range cases {
		var spec *Spec
		var m *MemFS
		if c.goos == Linux {
			spec = mustSpec(t, Linux, linHead+"packs:\n  - runner: "+c.runner+"\n    repo: o/a\n")
			m = NewMemFS(linHome, "localai")
			m.Dir("/srv/localai")
		} else {
			spec = mustSpec(t, Windows, winSpec+"packs:\n  - runner: "+c.runner+"\n    repo: o/a\n")
			m = NewMemFS(winHome, `SG3\localai`)
			m.Dir("V:/")
			m.Dir("V:/localai")
		}
		m.Env = map[string]string{c.key: c.val}
		name := "agent-pack"
		if c.runner != "claude" {
			name += "-" + c.runner
		}
		ff := &fakeFetcher{src: packFiles(), latest: "abcdef1234567890"}
		before := m.Writes
		lk := Apply(spec, adapterFor(t, c.goos), Host{FS: m, Env: m, Settings: &fakeSettings{vals: map[string]string{}}, Fetch: ff})
		s := step(lk, name)
		if c.want == "" {
			if s.Status != StatusFail || !strings.Contains(s.Detail, c.key) || !strings.Contains(s.Detail, c.val) {
				t.Errorf("%s=%s: %+v", c.key, c.val, s)
			}
			if m.Has("/home/localai/.claude/atrium-agent-pack.json") || m.Has("C:/Users/localai/.claude/atrium-agent-pack.json") {
				t.Errorf("%s=%s: another runner's record was written", c.key, c.val)
			}
			continue
		}
		if s.Status != StatusDone {
			t.Errorf("%s=%s: %+v", c.key, c.val, s)
			continue
		}
		if !m.Has(c.want+"/atrium-agent-pack.json") && !m.Has(c.want+"/.gemini/atrium-agent-pack.json") {
			t.Errorf("%s=%s: nothing in %s (writes %d, was %d)", c.key, c.val, c.want, m.Writes, before)
		}
	}
}
