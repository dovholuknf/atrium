package daemon

import "testing"

// Public and private stopped being one choice and became two, and the upgrade
// from one to the other is the part that can go wrong silently.
//
// Every install that exists was written before the flags. Reading that as
// "neither is on" would switch sharing off on every configured machine, and
// the operator would find out by pressing start and being refused.

func TestALegacyModeBecomesTheMatchingFlag(t *testing.T) {
	for _, tc := range []struct {
		mode           string
		public, privat bool
	}{
		{"public", true, false},
		{"private", false, true},
	} {
		c := ZrokConfig{Mode: tc.mode}
		c.normalise()
		if c.Public != tc.public || c.Private != tc.privat {
			t.Fatalf("mode %q became public=%v private=%v, wanted %v and %v",
				tc.mode, c.Public, c.Private, tc.public, tc.privat)
		}
		if c.Mode != tc.mode {
			t.Fatalf("mode %q was rewritten to %q", tc.mode, c.Mode)
		}
	}
}

// A configuration nobody has touched shares privately, because the board has no
// login in front of it. That was the default before and it stays the default.
func TestAnEmptyConfigIsPrivate(t *testing.T) {
	var c ZrokConfig
	c.normalise()
	if !c.Private || c.Public {
		t.Fatalf("an untouched config came out public=%v private=%v", c.Public, c.Private)
	}
	if c.Mode != "private" {
		t.Fatalf("mode is %q", c.Mode)
	}
}

// Both at once is the whole reason this stopped being a select. It must not be
// reduced back to one on the way through, and the board's own share picks
// public, because that is what somebody ticking both is reaching for.
func TestBothAtOnceSurvivesAndTheBoardGoesPublic(t *testing.T) {
	c := ZrokConfig{Public: true, Private: true}
	c.normalise()
	if !c.Public || !c.Private {
		t.Fatalf("ticking both came back public=%v private=%v", c.Public, c.Private)
	}
	if c.Mode != "public" {
		t.Fatalf("with both on, the board's share starts %q", c.Mode)
	}
}

// A stale mode arriving beside fresh flags is what an older browser tab sends.
// The flags are what the operator just ticked, so they win.
func TestTheFlagsBeatAStaleMode(t *testing.T) {
	c := ZrokConfig{Mode: "private", Public: true}
	c.normalise()
	if c.Mode != "public" {
		t.Fatalf("a stale mode survived the flags: %q", c.Mode)
	}
	if c.Private {
		t.Fatal("a stale mode turned private back on")
	}
}

// Normalising twice changes nothing. It runs on both the read and the write
// path, so a value that drifts each time it passes through would drift on every
// save.
func TestNormaliseIsIdempotent(t *testing.T) {
	for _, start := range []ZrokConfig{
		{},
		{Mode: "public"},
		{Mode: "private"},
		{Public: true},
		{Private: true},
		{Public: true, Private: true},
	} {
		once := start
		once.normalise()
		twice := once
		twice.normalise()
		if once != twice {
			t.Fatalf("%+v normalised to %+v and then to %+v", start, once, twice)
		}
	}
}
