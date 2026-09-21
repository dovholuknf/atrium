package daemon

import "testing"

// TWO LAUNCHES IN ONE DIRECTORY MUST NOT SHARE A NAME. The wire name is the key
// registration matches on, so a second session wearing the first's name is
// handed the first's card, history and permission rules rather than getting its
// own. The orchestrator relaunches doers into the same worktree, so this is the
// normal case, not an unlucky one.
func TestLaunchedNameDisambiguatesACollision(t *testing.T) {
	live := map[string]bool{}
	taken := func(n string) bool { return live[n] }

	first := launchedName("", "/work/atrium", taken)
	if first != "atrium" {
		t.Fatalf("first launch got %q, want the directory leaf %q", first, "atrium")
	}
	live[first] = true

	second := launchedName("", "/work/atrium", taken)
	if second == first {
		t.Fatalf("a second launch in the same dir reused %q", second)
	}
	if second != "atrium-2" {
		t.Fatalf("the disambiguated name was %q, want %q", second, "atrium-2")
	}
}

// A run of collisions keeps counting up rather than sticking at -2 forever.
func TestLaunchedNameCountsPastTheFirstCollision(t *testing.T) {
	live := map[string]bool{"atrium": true, "atrium-2": true, "atrium-3": true}
	taken := func(n string) bool { return live[n] }
	got := launchedName("", "/work/atrium", taken)
	if got != "atrium-4" {
		t.Fatalf("with -2 and -3 taken the next name was %q, want %q", got, "atrium-4")
	}
}

// A LAUNCH TITLE IS PREFERRED OVER THE DIRECTORY LEAF, because the board shows
// the title and a name slugged from it reads as the same work.
func TestLaunchedNamePrefersTheTitle(t *testing.T) {
	free := func(string) bool { return false }
	got := launchedName("Fix the flaky attach test", "/work/atrium", free)
	if got != "fix-the-flaky-attach-test" {
		t.Fatalf("title-derived name was %q", got)
	}
}

// A title still has to be unique. Two sessions launched with the same title in
// the same directory get distinct names.
func TestLaunchedNameDisambiguatesADuplicateTitle(t *testing.T) {
	live := map[string]bool{"review-the-pr": true}
	taken := func(n string) bool { return live[n] }
	got := launchedName("review the PR", "/work/atrium", taken)
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
