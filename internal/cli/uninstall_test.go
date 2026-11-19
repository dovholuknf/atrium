package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fakeUninstallDeps(t *testing.T, held int, calls *[]string) uninstallDeps {
	rec := func(s string) { *calls = append(*calls, s) }
	return uninstallDeps{
		heldTerminals: func() int { return held },
		stop:          func(bool) error { rec("stop"); return nil },
		hasHooks:      func() bool { return true },
		removeHooks:   func() error { rec("hooks"); return nil },
		removeService: func(n string) error { rec("service " + n); return nil },
	}
}

func TestUninstallDryRunPrintsThePlanAndChangesNothing(t *testing.T) {
	state := filepath.Join(t.TempDir(), ".atrium")
	if err := os.MkdirAll(state, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "atrium.db"), []byte("db"), 0o644); err != nil {
		t.Fatal(err)
	}
	var calls []string
	var out bytes.Buffer
	o := uninstallOpts{purge: true, dryRun: true, services: []string{"room1"}, stateDir: state, exe: filepath.Join(t.TempDir(), "atrium")}
	if err := runUninstall(&out, o, fakeUninstallDeps(t, 0, &calls)); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 0 {
		t.Fatalf("dry-run did something: %v", calls)
	}
	if _, err := os.Stat(filepath.Join(state, "atrium.db")); err != nil {
		t.Fatalf("dry-run deleted state: %v", err)
	}
	for _, want := range []string{"would stop", "would remove atrium's hooks", "would remove the service room1", "would delete"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("plan lacks %q:\n%s", want, out.String())
		}
	}
}

func TestUninstallRefusesWhileTerminalsAreHeldUnlessForced(t *testing.T) {
	var calls []string
	var out bytes.Buffer
	o := uninstallOpts{stateDir: t.TempDir()}
	if err := runUninstall(&out, o, fakeUninstallDeps(t, 2, &calls)); err != errAlreadySaid {
		t.Fatalf("err = %v", err)
	}
	if len(calls) != 0 {
		t.Fatalf("refusal still acted: %v", calls)
	}
	o.force = true
	if err := runUninstall(&out, o, fakeUninstallDeps(t, 2, &calls)); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || calls[0] != "stop" {
		t.Fatalf("forced run did %v", calls)
	}
}

func TestUninstallPurgeDeletesTheFilesBesideTheAddressFile(t *testing.T) {
	state := filepath.Join(t.TempDir(), ".atrium")
	if err := os.MkdirAll(state, 0o755); err != nil {
		t.Fatal(err)
	}
	loc := filepath.Join(t.TempDir(), "atrium")
	if err := os.MkdirAll(loc, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := []string{filepath.Join(loc, "lastdb.json"), filepath.Join(loc, "daemon.json")}
	if err := os.WriteFile(outside[0], []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	var calls []string
	var out bytes.Buffer
	o := uninstallOpts{purge: true, stateDir: state, outside: outside}
	if err := runUninstall(&out, o, fakeUninstallDeps(t, 0, &calls)); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{state, outside[0], loc} {
		if _, err := os.Stat(p); err == nil {
			t.Errorf("%s survived the purge", p)
		}
	}
}
