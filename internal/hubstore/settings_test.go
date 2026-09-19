package hubstore

import "testing"

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
