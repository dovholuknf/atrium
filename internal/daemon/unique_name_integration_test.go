//go:build integration

package daemon

import (
	"errors"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

// TWO LAUNCHES IN ONE DIRECTORY MUST NOT SHARE A NAME. The wire name is the key
// registration matches on, so a second session wearing the first's name is
// handed the first's card, history and permission rules rather than getting its
// own. The orchestrator relaunches doers into the same worktree, so this is the
// normal case, not an unlucky one.
func TestLaunchedNameDisambiguatesACollision(t *testing.T) {
	live := map[string]bool{}
	taken := func(n string) (bool, error) { return live[n], nil }

	first, _ := launchedName("", "/work/atriumx", taken)
	if first != "atriumx" {
		t.Fatalf("first launch got %q, want the directory leaf %q", first, "atriumx")
	}
	live[first] = true

	second, _ := launchedName("", "/work/atriumx", taken)
	if second == first {
		t.Fatalf("a second launch in the same dir reused %q", second)
	}
	if second != "atriumx-2" {
		t.Fatalf("the disambiguated name was %q, want %q", second, "atriumx-2")
	}
}

// A run of collisions keeps counting up rather than sticking at -2 forever.
func TestLaunchedNameCountsPastTheFirstCollision(t *testing.T) {
	live := map[string]bool{"atriumx": true, "atriumx-2": true, "atriumx-3": true}
	taken := func(n string) (bool, error) { return live[n], nil }
	got, _ := launchedName("", "/work/atriumx", taken)
	if got != "atriumx-4" {
		t.Fatalf("with -2 and -3 taken the next name was %q, want %q", got, "atriumx-4")
	}
}

// A LAUNCH TITLE IS PREFERRED OVER THE DIRECTORY LEAF, because the board shows
// the title and a name slugged from it reads as the same work.
func TestLaunchedNamePrefersTheTitle(t *testing.T) {
	free := func(string) (bool, error) { return false, nil }
	got, _ := launchedName("Fix the flaky attach test", "/work/atriumx", free)
	if got != "fix-the-flaky-attach-test" {
		t.Fatalf("title-derived name was %q", got)
	}
}

// A title still has to be unique. Two sessions launched with the same title in
// the same directory get distinct names.
func TestLaunchedNameDisambiguatesADuplicateTitle(t *testing.T) {
	live := map[string]bool{"review-the-pr": true}
	taken := func(n string) (bool, error) { return live[n], nil }
	got, _ := launchedName("review the PR", "/work/atriumx", taken)
	if got != "review-the-pr-2" {
		t.Fatalf("duplicate title was named %q, want %q", got, "review-the-pr-2")
	}
}

func TestNameSlug(t *testing.T) {
	cases := map[string]string{
		"Fix the flaky attach test": "fix-the-flaky-attach-test",
		"  spaced  out  ":           "spaced-out",
		"symbols!!! & such":         "symbols-such",
		"---edges---":               "edges",
		"":                          "",
		"!!!":                       "",
	}
	for in, want := range cases {
		if got := nameSlug(in); got != want {
			t.Errorf("nameSlug(%q) = %q, want %q", in, got, want)
		}
	}
	// A long title is capped and not left with a trailing dash.
	long := nameSlug("this is a very long launch title that goes well past the limit we set")
	if len(long) > 40 {
		t.Errorf("slug %q is longer than the cap", long)
	}
	if long[len(long)-1] == '-' {
		t.Errorf("slug %q ends on a dash", long)
	}
}

// A DEAD OR DONE CARD STILL HOLDS ITS NAME. Register matches the wire name at any
// status, so a launch titled the same as a finished card was handed that card and
// re-prompted its old recap. r-028.
func TestLaunchedNameSkipsADeadCardsName(t *testing.T) {
	d := testDaemon(t)
	old, _, err := d.st.Register(store.Observed{WireName: "smoke", Worktree: "/work/smoke"})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetStatus(old.ID, "done"); err != nil {
		t.Fatal(err)
	}
	if got, _ := d.launchedName("smoke", "/work/smoke"); got != "smoke-2" {
		t.Fatalf("a title matching a done card got %q, want smoke-2", got)
	}
}

// An ARCHIVED card holds its name too: List skips it, Register does not.
func TestLaunchedNameSkipsAnArchivedCardsName(t *testing.T) {
	d := testDaemon(t)
	old, _, err := d.st.Register(store.Observed{WireName: "smoke", Worktree: "/work/smoke"})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetStatus(old.ID, "done"); err != nil {
		t.Fatal(err)
	}
	if err := d.st.ArchiveCulled(old.ID, "test"); err != nil {
		t.Fatal(err)
	}
	if got, _ := d.launchedName("smoke", "/work/smoke"); got != "smoke-2" {
		t.Fatalf("a title matching an archived card got %q, want smoke-2", got)
	}
}

// A HALTED STORE FAILS THE LAUNCH, it does not loop. WireNameHeld used to answer
// true on any error, so a store that halted mid-launch spun launchedName forever
// while holding the launch path. r-044.
func TestLaunchedNameFailsOnAHaltedStore(t *testing.T) {
	d := testDaemon(t)
	d.st.Close()
	got, err := d.launchedName("smoke", "/work/smoke")
	if err == nil {
		t.Fatalf("a closed store named the launch %q instead of failing", got)
	}
}

// The cap is the backstop: a taken func that says true forever fails at it.
func TestLaunchedNameGivesUpAtTheCap(t *testing.T) {
	calls := 0
	always := func(string) (bool, error) { calls++; return true, nil }
	got, err := launchedName("smoke", "/work/smoke", always)
	if err == nil {
		t.Fatalf("an always-held name produced %q", got)
	}
	if !strings.Contains(err.Error(), "smoke") {
		t.Fatalf("error %q does not name the base", err)
	}
	if calls > maxNameSuffix+1 {
		t.Fatalf("asked %d times, cap is %d", calls, maxNameSuffix)
	}
}

// A store error from taken is returned, wrapped, and stops the search.
func TestLaunchedNameReturnsTheStoreError(t *testing.T) {
	boom := errors.New("disk gone")
	calls := 0
	failing := func(string) (bool, error) { calls++; return false, boom }
	if _, err := launchedName("smoke", "/work/smoke", failing); !errors.Is(err, boom) || calls != 1 {
		t.Fatalf("err=%v calls=%d, want the store error after one call", err, calls)
	}
}
