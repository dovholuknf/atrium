package api

import (
	"reflect"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

func TestDefaultRunnerPrefersClaudeThenTheFirstFound(t *testing.T) {
	hs := []*store.Harness{
		{ID: "codex", Enabled: true}, {ID: "claude", Enabled: true}, {ID: "gemini", Enabled: true}, {ID: "off", Enabled: false},
	}
	on := func(ids ...string) func(*store.Harness) string {
		return func(h *store.Harness) string {
			for _, id := range ids {
				if h.ID == id {
					return "/bin/" + id
				}
			}
			return ""
		}
	}
	cases := []struct {
		name   string
		found  func(*store.Harness) string
		want   string
		looked []string
	}{
		{"claude present", on("codex", "claude", "gemini", "off"), "claude", nil},
		{"only codex present", on("codex"), "codex", nil},
		{"only gemini present", on("gemini"), "gemini", []string{"codex", "claude"}},
		{"a disabled runner is never picked", on("off"), "", []string{"codex", "claude", "gemini"}},
		{"none present", on(), "", []string{"codex", "claude", "gemini"}},
	}
	for _, c := range cases {
		id, looked := DefaultRunner(hs, c.found)
		if id != c.want {
			t.Errorf("%s: picked %q, want %q", c.name, id, c.want)
		}
		if id == "" && !reflect.DeepEqual(looked, c.looked) {
			t.Errorf("%s: looked for %v, want %v", c.name, looked, c.looked)
		}
	}
	if msg := NoRunnerFound([]string{"claude", "codex"}, "m1"); !strings.Contains(msg, "claude, codex") {
		t.Errorf("the refusal names no runner: %q", msg)
	}
	if off := RunnersOff(hs, on("off", "codex")); !reflect.DeepEqual(off, []string{"off"}) {
		t.Errorf("runners off but found = %v, want [off]", off)
	}
	if msg := NoRunnerFound([]string{"claude"}, "m1", "codex"); !strings.Contains(msg, "codex is on PATH but turned off") {
		t.Errorf("the refusal does not name the runner that is off: %q", msg)
	}
}

func TestNewWorkHarnessExplicitWins(t *testing.T) {
	s := &Server{}
	if got := s.newWorkHarness(" codex ", "o", "r"); got != "codex" {
		t.Errorf("an explicit runner became %q", got)
	}
}
