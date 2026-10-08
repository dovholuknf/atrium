package runnersetup

import "testing"

func TestPackLayoutIsWhereEachRunnerReads(t *testing.T) {
	none := func(string) string { return "" }
	for _, c := range []struct {
		id, dir        string
		agents, skills bool
	}{
		{"claude", "/home/al/.claude", true, true},
		{"gemini", "/home/al/.gemini", true, true},
		{"codex", "/home/al/.codex", false, true},
	} {
		l, ok := PackLayoutFor(c.id, Env{Home: "/home/al", Getenv: none})
		if !ok || l.Dir != c.dir || l.Agents != c.agents || l.Skills != c.skills {
			t.Errorf("%s: %+v %v", c.id, l, ok)
		}
	}
	if _, ok := PackLayoutFor("aider", Env{Home: "/home/al", Getenv: none}); ok {
		t.Error("a runner with no pack adapter has no layout")
	}
	moved := func(k string) string {
		return map[string]string{"GEMINI_CLI_HOME": "/data/g", "CODEX_HOME": "/data/c", "CLAUDE_CONFIG_DIR": "/data/cl"}[k]
	}
	for id, want := range map[string]string{"gemini": "/data/g/.gemini", "codex": "/data/c", "claude": "/data/cl"} {
		if l, _ := PackLayoutFor(id, Env{Home: "/home/al", Getenv: moved}); l.Dir != want {
			t.Errorf("%s moved: %q, want %q", id, l.Dir, want)
		}
	}
}
