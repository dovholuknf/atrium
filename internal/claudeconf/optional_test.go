package claudeconf

import (
	"strings"
	"testing"
)

// wantedCount is how many hooks atrium asks for unprompted. An optional hook
// is offered rather than wanted, so it never counts toward `Missing`.
func wantedCount() int {
	n := 0
	for _, w := range WantedHooks {
		if !w.Optional {
			n++
		}
	}
	return n
}

// The Stop hook is the only hook whose answer changes what a session does, so
// nobody may end up with one by pressing a button that said "install the
// hooks". It has to be asked for by name.
func TestInstallAllSkipsTheStopHook(t *testing.T) {
	withHome(t, "")

	rep, _, err := Install("C:/tools/atrium.exe")
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range rep.Hooks {
		if h.Hook == "Stop" && h.Installed {
			t.Fatal("install all wrote a Stop hook")
		}
		if !h.Optional && !h.Installed {
			t.Fatalf("install all skipped %s, which is not optional", h.Hook)
		}
	}
}

// Asked for by name it is written, and it points at the subcommand rather than
// at the activity hook, because it is the one hook that has to read the reply.
func TestStopHookInstallsWhenNamed(t *testing.T) {
	withHome(t, "")

	rep, _, err := InstallOnly("C:/tools/atrium.exe", []string{"turn-end"})
	if err != nil {
		t.Fatal(err)
	}
	var found string
	for _, h := range rep.Hooks {
		if h.Hook == "Stop" {
			if !h.Installed {
				t.Fatal("a Stop hook asked for by name was not written")
			}
			found = h.Found
		}
	}
	if !strings.Contains(found, "turn --event end") {
		t.Fatalf("the Stop hook runs %q, wanted the turn subcommand", found)
	}
}

// An optional hook that is off is not a problem to be fixed. Counting it would
// leave the board permanently offering to install something switched off on
// purpose.
func TestAnUninstalledOptionalHookIsNotMissing(t *testing.T) {
	withHome(t, "")

	rep, err := Inspect("C:/tools/atrium.exe")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Missing != wantedCount() {
		t.Fatalf("missing is %d, wanted %d", rep.Missing, wantedCount())
	}
	if wantedCount() == len(WantedHooks) {
		t.Fatal("no hook is optional, so this test proves nothing")
	}
}

// A Stop hook someone installed and that now points at an old binary IS a
// problem: they asked for it, and it is broken.
func TestAStaleOptionalHookStillCounts(t *testing.T) {
	withHome(t, `{"hooks":{"Stop":[{"hooks":[{"type":"command",`+
		`"command":"C:/old/atrium.exe turn --event end"}]}]}}`)

	rep, err := Inspect("C:/tools/atrium.exe")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Missing != wantedCount()+1 {
		t.Fatalf("missing is %d, wanted %d with a stale optional hook", rep.Missing, wantedCount()+1)
	}
}
