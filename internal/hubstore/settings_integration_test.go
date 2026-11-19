//go:build integration

package hubstore

import (
	"testing"

	"github.com/dovholuknf/atrium/internal/base/cardcolors"
)

// A FRESH HUB HAS NO SKIN OF ITS OWN, and that reads as empty rather than as an
// error. The serving side turns empty into the default, so a hub that has never
// been asked looks exactly as the board shipped.
func TestHubSkinIsEmptyUntilSet(t *testing.T) {
	s := open(t)
	got, err := s.HubSkin()
	if err != nil {
		t.Fatalf("reading an unset hub skin errored: %v", err)
	}
	if got != "" {
		t.Fatalf("a fresh hub reported skin %q, wanted empty", got)
	}
}

// SET THEN READ, and the second set replaces the first: there is one hub skin,
// not a drawer of them.
func TestHubSkinRoundTripsAndReplaces(t *testing.T) {
	s := open(t)
	if err := s.SetHubSkin("noir"); err != nil {
		t.Fatalf("setting the hub skin errored: %v", err)
	}
	if got, _ := s.HubSkin(); got != "noir" {
		t.Fatalf("hub skin came back as %q, wanted noir", got)
	}
	// Surrounding space is trimmed on the way in, so a stored name matches what
	// the board compares against.
	if err := s.SetHubSkin("  moss  "); err != nil {
		t.Fatalf("resetting the hub skin errored: %v", err)
	}
	if got, _ := s.HubSkin(); got != "moss" {
		t.Fatalf("hub skin came back as %q, wanted moss", got)
	}
}

// SURVIVES A REOPEN, which is the whole reason it is in the database. A restart
// is not consent to forget which skin the ALL view was wearing.
func TestHubSkinSurvivesReopen(t *testing.T) {
	dir := t.TempDir() + "/hub.db"
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := s.SetHubSkin("ember"); err != nil {
		t.Fatalf("set: %v", err)
	}
	s.Close()

	again, err := Open(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer again.Close()
	if got, _ := again.HubSkin(); got != "ember" {
		t.Fatalf("after a reopen the hub skin was %q, wanted ember", got)
	}
}

func TestHubCardColorsSeededOnceAndEmptyStaysEmpty(t *testing.T) {
	s := open(t)
	c, err := s.HubCardColors()
	if err != nil || len(c.Repos) != 0 {
		t.Fatalf("seed: %d entries, %v", len(c.Repos), err)
	}
	if err := s.SetHubCardColors(cardcolors.Colors{}); err != nil {
		t.Fatal(err)
	}
	if c, _ := s.HubCardColors(); len(c.Repos) != 0 {
		t.Fatalf("an emptied table came back with %d", len(c.Repos))
	}
	if s.SetHubCardColors(cardcolors.Colors{Repos: map[string]string{"Bad": "x"}}) == nil {
		t.Fatal("a bad key was stored")
	}
}

// A HUB THAT WAS NEVER TOLD keeps a stash 30 days. 0 is never, and a negative number is refused.
func TestStashRetentionDefaultsToThirtyAndRoundTrips(t *testing.T) {
	s := open(t)
	if n, err := s.StashRetentionDays(); err != nil || n != 30 {
		t.Fatalf("unset = %d, %v", n, err)
	}
	for _, want := range []int{7, 0, 90} {
		if err := s.SetStashRetentionDays(want); err != nil {
			t.Fatal(err)
		}
		if n, _ := s.StashRetentionDays(); n != want {
			t.Errorf("set %d, read %d", want, n)
		}
	}
	if err := s.SetStashRetentionDays(-1); err == nil {
		t.Error("a negative retention was taken")
	}
}

func TestTheOperatorProfileIsAtriumsOwnUntilWrittenThenWhatWasWritten(t *testing.T) {
	s := open(t)
	if p, err := s.OperatorProfile(); err != nil || p.Email != DefaultOperatorProfile().Email {
		t.Fatalf("default: %+v %v", p, err)
	}
	if err := s.SetOperatorProfile(OperatorProfile{Name: "Ann"}); err == nil {
		t.Fatal("a profile with no email was kept")
	}
	if err := s.SetOperatorProfile(OperatorProfile{Name: "Ann", Email: "a@x", Reviewers: []string{"r1"}}); err != nil {
		t.Fatal(err)
	}
	if p, err := s.OperatorProfile(); err != nil || p.Name != "Ann" || p.Shown() != "Ann" || len(p.Reviewers) != 1 {
		t.Fatalf("written: %+v %v", p, err)
	}
}

func TestTheOldShareLoginIsForgottenPasswordIncluded(t *testing.T) {
	s := open(t)
	for _, k := range []string{"share_auth", "share_user", "share_pass", "share_oidc"} {
		if err := s.SetHubSetting(k, "x"); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.ForgetShareLogin(); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"share_auth", "share_user", "share_pass", "share_oidc"} {
		if v, _ := s.HubSetting(k); v != "" {
			t.Fatalf("%s is still %q", k, v)
		}
	}
}
