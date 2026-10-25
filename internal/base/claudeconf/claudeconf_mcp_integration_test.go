//go:build integration

package claudeconf

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConvertMCPForms(t *testing.T) {
	for _, decision := range []string{"approve", "block"} {
		for raw, want := range map[string]string{
			"mcp__mercurius__discourse_discourse_search": "mcp__mercurius__discourse_discourse_search",
			"mcp__mercurius__discourse_discourse_get_*":  "mcp__mercurius__discourse_discourse_get_*",
			"mcp__mercurius__*":                          "mcp__mercurius__*",
			"mcp__mercurius":                             "mcp__mercurius__*",
		} {
			var entries []Entry
			var skipped []Skipped
			convert(raw, decision, "test", &entries, &skipped)
			if len(skipped) != 0 || len(entries) != 1 {
				t.Fatalf("%s as %s: %+v %+v", raw, decision, entries, skipped)
			}
			e := entries[0]
			if e.Tool != want || e.Pattern != "*" || e.Decision != decision || e.Broad {
				t.Errorf("%s as %s became %+v, want tool %s", raw, decision, e, want)
			}
		}
	}
}

func TestConvertMCPArgumentsAreSkipped(t *testing.T) {
	for _, decision := range []string{"approve", "block"} {
		var entries []Entry
		var skipped []Skipped
		convert("mcp__a__t(x)", decision, "test", &entries, &skipped)
		if len(entries) != 0 || len(skipped) != 1 || !strings.Contains(skipped[0].Reason, "not its arguments") {
			t.Errorf("%s: an MCP rule with arguments must be skipped and say why: %+v %+v",
				decision, entries, skipped)
		}
	}
}

func TestConvertMCPUnanchoredGlobs(t *testing.T) {
	var entries []Entry
	var skipped []Skipped
	convert("mcp__*", "approve", "test", &entries, &skipped)
	if len(entries) != 0 || len(skipped) != 1 {
		t.Fatalf("an unanchored allow glob must be skipped: %+v %+v", entries, skipped)
	}

	entries, skipped = nil, nil
	convert("mcp__*", "block", "test", &entries, &skipped)
	if len(entries) != 1 || !entries[0].Broad || entries[0].Tool != "mcp__*" {
		t.Fatalf("a bare deny should be an entry marked Broad: %+v %+v", entries, skipped)
	}

	for _, raw := range []string{"mcp__*__t", "mcp__s*", "mcp__", "mcp____t"} {
		for _, decision := range []string{"approve", "block"} {
			entries, skipped = nil, nil
			convert(raw, decision, "test", &entries, &skipped)
			if len(entries) != 0 || len(skipped) != 1 {
				t.Errorf("%s as %s should be skipped: %+v %+v", raw, decision, entries, skipped)
			}
		}
	}
}

// isolateHome points the user-level settings at an empty directory, because Load
// reads the real ~/.claude/settings.json and a test must not depend on it.
func isolateHome(t *testing.T) {
	t.Helper()
	h := t.TempDir()
	t.Setenv("USERPROFILE", h)
	t.Setenv("HOME", h)
}

func writeSettings(t *testing.T, path string, allow, deny, ask []string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	blob, err := json.Marshal(map[string]any{
		"permissions": map[string]any{"allow": allow, "deny": deny, "ask": ask},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, blob, 0o644); err != nil {
		t.Fatal(err)
	}
}

func toolsOf(entries []Entry, decision string) map[string]bool {
	out := map[string]bool{}
	for _, e := range entries {
		if e.Decision == decision {
			out[e.Tool] = true
		}
	}
	return out
}

func TestLoadDropsAnMCPAllowThatADenyCovers(t *testing.T) {
	// Same file, then two different files: Claude Code merges them and a deny
	// still wins.
	isolateHome(t)
	dir := t.TempDir()
	c := filepath.Join(dir, ".claude")
	writeSettings(t, filepath.Join(c, "settings.json"),
		[]string{"mcp__s__t", "mcp__other__t"}, []string{"mcp__s__*"}, nil)
	entries, skipped, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if toolsOf(entries, "approve")["mcp__s__t"] {
		t.Error("an allow covered by a deny in the same file was imported")
	}
	if !toolsOf(entries, "approve")["mcp__other__t"] {
		t.Error("an allow on another server was dropped")
	}
	found := false
	for _, s := range skipped {
		found = found || (s.Raw == "mcp__s__t" && strings.Contains(s.Reason, "shadowed by deny mcp__s__*"))
	}
	if !found {
		t.Errorf("the dropped allow was not reported: %+v", skipped)
	}

	dir = t.TempDir()
	c = filepath.Join(dir, ".claude")
	writeSettings(t, filepath.Join(c, "settings.json"), []string{"mcp__s__t"}, nil, nil)
	writeSettings(t, filepath.Join(c, "settings.local.json"), nil, []string{"mcp__s__*"}, nil)
	entries, _, err = Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if toolsOf(entries, "approve")["mcp__s__t"] {
		t.Error("an allow covered by a deny in another file was imported")
	}
}

func TestLoadKeepsABroadAllowWithANarrowDeny(t *testing.T) {
	isolateHome(t)
	dir := t.TempDir()
	writeSettings(t, filepath.Join(dir, ".claude", "settings.json"),
		[]string{"mcp__s__*"}, []string{"mcp__s__t"}, nil)
	entries, _, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !toolsOf(entries, "approve")["mcp__s__*"] || !toolsOf(entries, "block")["mcp__s__t"] {
		t.Errorf("both should import, the store answers the pair by specificity: %+v", entries)
	}
}

func TestLoadAnAskShadowsLikeADeny(t *testing.T) {
	isolateHome(t)
	dir := t.TempDir()
	writeSettings(t, filepath.Join(dir, ".claude", "settings.json"),
		[]string{"mcp__mercurius__*", "mcp__mercurius__discourse_discourse_create_topic"}, nil,
		[]string{"mcp__mercurius__discourse_discourse_create_topic"})
	entries, skipped, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	approves := toolsOf(entries, "approve")
	if approves["mcp__mercurius__discourse_discourse_create_topic"] {
		t.Error("an allow covered by an ask was imported")
	}
	if !approves["mcp__mercurius__*"] {
		t.Error("the broad allow is not covered by a narrower ask and should import")
	}
	if len(toolsOf(entries, "block")) != 0 {
		t.Errorf("an ask must never become a rule: %+v", entries)
	}
	found := false
	for _, s := range skipped {
		found = found || strings.Contains(s.Reason, "shadowed by ask")
	}
	if !found {
		t.Errorf("the dropped allow was not reported: %+v", skipped)
	}
}
