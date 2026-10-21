package cli

import (
	"strings"
	"testing"
)

func TestControlSkewNoteNamesAnOlderControlExe(t *testing.T) {
	note := controlSkewNote(buildID{Code: "aaa", Commit: "1111111111111"}, buildID{Code: "bbb", Commit: "2222222222222"})
	for _, want := range []string{"atrium-control.exe", "older", "111111111111", "222222222222"} {
		if !strings.Contains(note, want) {
			t.Errorf("note %q lacks %q", note, want)
		}
	}
}

func TestControlSkewNoteIsSilentWhenTheBuildsMatchOrAreUnknown(t *testing.T) {
	for name, c := range map[string][2]buildID{
		"same code":      {{Code: "a", Commit: "1"}, {Code: "a", Commit: "2"}},
		"same commit":    {{Commit: "1"}, {Commit: "1"}},
		"dev control":    {{}, {Code: "b", Commit: "2"}},
		"room unstamped": {{Code: "a", Commit: "1"}, {}},
	} {
		if note := controlSkewNote(c[0], c[1]); note != "" {
			t.Errorf("%s: want no note, got %q", name, note)
		}
	}
}

func TestMissingLaunchArgsReportsTheScratchDrift(t *testing.T) {
	old := map[string]any{"properties": map[string]any{"cwd": 1, "title": 1, "prompt": 1, "tags": 1}}
	got := missingLaunchArgs(old, []string{"cwd", "scratch", "tags", "item"})
	if len(got) != 2 || got[0] != "scratch" || got[1] != "item" {
		t.Fatalf("missing = %v, want [scratch item]", got)
	}
	if got := missingLaunchArgs(map[string]any{"properties": map[string]any{"scratch": 1}}, []string{"scratch"}); len(got) != 0 {
		t.Fatalf("missing = %v, want none", got)
	}
}

func missingLaunchArgs(schema any, args []string) []string {
	m, _ := schema.(map[string]any)
	props, _ := m["properties"].(map[string]any)
	var out []string
	for _, a := range args {
		if _, ok := props[a]; !ok {
			out = append(out, a)
		}
	}
	return out
}
