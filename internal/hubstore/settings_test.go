package hubstore

import "testing"

// A FRESH HUB HAS NO SHARE LOGIN, which reads as an empty scheme rather than an
// error. The share-creation side turns an empty scheme into a refusal for a
// public share, so a hub that was never configured cannot open one silently.
func TestShareAuthIsEmptyUntilSet(t *testing.T) {
	s := open(t)
	got, err := s.ShareAuth()
	if err != nil {
		t.Fatalf("reading an unset share auth errored: %v", err)
	}
	if got.Scheme != "" || got.User != "" || got.Pass != "" || got.OIDCProvider != "" {
		t.Fatalf("a fresh hub reported share auth %+v, wanted all empty", got)
	}
}

// SET THEN READ the updb credential, all fields together.
func TestShareAuthUpdbRoundTrips(t *testing.T) {
	s := open(t)
	want := ShareAuth{Scheme: "updb", User: "clint", Pass: "hunter2"}
	if err := s.SetShareAuth(want); err != nil {
		t.Fatalf("set: %v", err)
	}
	got, _ := s.ShareAuth()
	if got != want {
		t.Fatalf("share auth came back %+v, wanted %+v", got, want)
	}
}

// SAVING WITHOUT A PASSWORD LEAVES THE STORED ONE ALONE, so a settings save
// that only touches the username does not blank the password. A blank password
// box is "do not change", not "clear it".
func TestShareAuthKeepsPasswordWhenOmitted(t *testing.T) {
	s := open(t)
	if err := s.SetShareAuth(ShareAuth{Scheme: "updb", User: "clint", Pass: "hunter2"}); err != nil {
		t.Fatalf("first set: %v", err)
	}
	if err := s.SetShareAuth(ShareAuth{Scheme: "updb", User: "clint2"}); err != nil {
		t.Fatalf("second set: %v", err)
	}
	got, _ := s.ShareAuth()
	if got.User != "clint2" {
		t.Fatalf("username did not update: %q", got.User)
	}
	if got.Pass != "hunter2" {
		t.Fatalf("password was blanked by a save that omitted it: %q", got.Pass)
	}
}

// CLEARING THE PASSWORD is explicit, through SetSharePass, and it works.
func TestSetSharePassClears(t *testing.T) {
	s := open(t)
	if err := s.SetShareAuth(ShareAuth{Scheme: "updb", User: "clint", Pass: "hunter2"}); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := s.SetSharePass(""); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if got, _ := s.ShareAuth(); got.Pass != "" {
		t.Fatalf("password survived an explicit clear: %q", got.Pass)
	}
}

// OIDC ROUND TRIPS, and switching schemes keeps the other scheme's fields so a
// switch back does not lose what was typed.
func TestShareAuthOIDCAndSchemeSwitchKeepsFields(t *testing.T) {
	s := open(t)
	if err := s.SetShareAuth(ShareAuth{Scheme: "updb", User: "clint", Pass: "hunter2"}); err != nil {
		t.Fatalf("updb set: %v", err)
	}
	if err := s.SetShareAuth(ShareAuth{Scheme: "oidc", User: "clint", OIDCProvider: "google"}); err != nil {
		t.Fatalf("oidc set: %v", err)
	}
	got, _ := s.ShareAuth()
	if got.Scheme != "oidc" || got.OIDCProvider != "google" {
		t.Fatalf("oidc did not take: %+v", got)
	}
	if got.Pass != "hunter2" {
		t.Fatalf("switching to oidc lost the updb password: %q", got.Pass)
	}
}

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
