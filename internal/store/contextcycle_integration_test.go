//go:build integration

package store

import (
	"path/filepath"
	"testing"
)

// The per-harness limits read the default when unset or unreadable, drop an entry out of range, and the check
// refuses one.
func TestContextLimitsReadAndCheck(t *testing.T) {
	s := openTestStore(t)
	if got := s.ContextLimits(); len(got) != 1 || got["claude"] != 200 {
		t.Fatalf("unset reads %v, want claude 200", got)
	}
	if err := s.SetSetting(SettingContextLimits, `{"claude":300,"codex":5,"ollama":150}`); err != nil {
		t.Fatal(err)
	}
	if got := s.ContextLimits(); got["claude"] != 300 || got["ollama"] != 150 || got["codex"] != 0 {
		t.Fatalf("reads %v", got)
	}
	if err := s.SetSetting(SettingContextLimits, `junk`); err != nil {
		t.Fatal(err)
	}
	if got := s.ContextLimits(); got["claude"] != 200 {
		t.Fatalf("junk reads %v, want the default", got)
	}
	if v, err := CheckContextLimits(map[string]int{" claude ": 250}); err != nil || v != `{"claude":250}` {
		t.Fatalf("check gave %q %v", v, err)
	}
	for _, bad := range []map[string]int{{"claude": 9}, {"claude": 2001}, {" ": 200}} {
		if _, err := CheckContextLimits(bad); err == nil {
			t.Fatalf("%v was accepted", bad)
		}
	}
	if v, err := CheckContextLimits(nil); err != nil || v != "" {
		t.Fatalf("empty gave %q %v", v, err)
	}
}

func TestContextCycleChecks(t *testing.T) {
	for v, want := range map[string]string{"": "", "on": "", " OFF ": "off"} {
		if got, err := CheckContextCycleText(v); err != nil || got != want {
			t.Fatalf("%q gave %q %v", v, got, err)
		}
	}
	if _, err := CheckContextCycleText("sometimes"); err == nil {
		t.Fatal("a junk switch was accepted")
	}
	if _, err := CheckContextHandoffDir("rel/dir"); err == nil {
		t.Fatal("a relative handoff directory was accepted")
	}
	abs := filepath.Join(t.TempDir(), "h")
	if got, err := CheckContextHandoffDir(abs); err != nil || got != abs {
		t.Fatalf("%q gave %q %v", abs, got, err)
	}
}

// The migration drops the old automatic new context settings, and running it again is harmless.
func TestContextCycleMigrationDropsTheOldSettings(t *testing.T) {
	s := openTestStore(t)
	for _, k := range []string{"auto_new_context", "auto_new_context_k", "context_ceiling_k", "context_threshold_k"} {
		if err := s.SetSetting(k, "1"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.db.Exec(`DELETE FROM schema_migration WHERE name = '0086_context_cycle'`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := s.migrate(); err != nil {
			t.Fatal(err)
		}
	}
	for _, k := range []string{"auto_new_context", "auto_new_context_k", "context_ceiling_k", "context_threshold_k"} {
		if v, _ := s.Setting(k); v != "" {
			t.Fatalf("%s survived the migration as %q", k, v)
		}
	}
}
