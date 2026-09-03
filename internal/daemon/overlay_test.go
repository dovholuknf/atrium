package daemon

import (
	"strings"
	"testing"
)

func joined(args []string) string { return strings.Join(args, " ") }

// A share atrium runs has no terminal to draw in. Without --headless zrok
// paints an interface into a pipe and nothing readable comes back, so the
// address never appears and the share looks broken.
func TestZrokShareIsHeadless(t *testing.T) {
	for _, c := range []ZrokConfig{
		{Mode: "private", Backend: "localhost:7778"},
		{Mode: "public", Backend: "localhost:7778"},
		{Reserved: "abc123", Backend: "localhost:7778"},
	} {
		args, err := c.zrokArgs()
		if err != nil {
			t.Fatalf("%+v: %v", c, err)
		}
		if !strings.Contains(joined(args), "--headless") {
			t.Fatalf("%+v produced %q, which would draw an interface into a pipe", c, joined(args))
		}
	}
}

// The mode is the difference between a link only you can open and a link
// anyone can. A typo in it must not quietly become the more open one.
func TestZrokRefusesAModeItDoesNotKnow(t *testing.T) {
	_, err := ZrokConfig{Mode: "publik", Backend: "localhost:7778"}.zrokArgs()
	if err == nil {
		t.Fatal("a misspelled mode was accepted")
	}
	if !strings.Contains(err.Error(), "public or private") {
		t.Fatalf("the error does not say what is allowed: %v", err)
	}
}

// A reserved token already carries its own mode and address, so the token is
// the whole instruction and the mode field must not be sent alongside it.
func TestZrokReservedShareUsesTheToken(t *testing.T) {
	args, err := ZrokConfig{
		Mode: "public", Reserved: "tok123", Backend: "localhost:7778",
	}.zrokArgs()
	if err != nil {
		t.Fatal(err)
	}
	got := joined(args)
	if !strings.Contains(got, "reserved tok123") {
		t.Fatalf("the token is not what it shares: %q", got)
	}
	if strings.Contains(got, "share public") {
		t.Fatalf("a reserved share was also told a mode: %q", got)
	}
}

// Nothing to publish is a misconfiguration with a name, not a command that
// fails somewhere downstream.
func TestZrokRefusesWithNothingToPublish(t *testing.T) {
	if _, err := (ZrokConfig{Mode: "private"}).zrokArgs(); err == nil {
		t.Fatal("a share with no backend was accepted")
	}
}

// The identity is the tunneler's to open, and it is the one thing hosting
// cannot proceed without.
func TestZitiRefusesWithoutAnIdentity(t *testing.T) {
	_, err := ZitiConfig{}.zitiArgs()
	if err == nil {
		t.Fatal("hosting was accepted with no identity")
	}
	if !strings.Contains(err.Error(), "identity") {
		t.Fatalf("the error does not name the missing field: %v", err)
	}
}

// The identity reaches the tunneler as a path and nothing else. Atrium never
// opens it, so it must not be transformed on the way through either.
func TestZitiPassesTheIdentityThrough(t *testing.T) {
	path := "C:/Users/someone/.ziti/atrium.json"
	args, err := ZitiConfig{Identity: path}.zitiArgs()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(joined(args), "--identity "+path) {
		t.Fatalf("the identity was not passed through as given: %q", joined(args))
	}
}

// Every field on the form has to reach the command. A field that is collected,
// stored and then dropped is a control that does nothing, and one of those
// teaches you to distrust the rest.
func TestZitiSendsEveryFieldItAsksFor(t *testing.T) {
	args, err := ZitiConfig{Identity: "C:/id.json", Extra: "--verbose"}.zitiArgs()
	if err != nil {
		t.Fatal(err)
	}
	got := joined(args)
	for _, want := range []string{"C:/id.json", "--verbose"} {
		if !strings.Contains(got, want) {
			t.Fatalf("%q never reached the command: %q", want, got)
		}
	}
}

// Whatever a share prints, the address has to come back out of it. The format
// is not a contract, so this only asks that a URL in a line is found and that
// the punctuation around it is not part of it.
func TestFindURL(t *testing.T) {
	cases := map[string]string{
		"https://x.share.zrok.io":                      "https://x.share.zrok.io",
		"  access your share at https://a.b.zrok.io  ": "https://a.b.zrok.io",
		`{"url":"https://c.d.zrok.io","token":"abc"}`:  "https://c.d.zrok.io",
		"| https://e.f.zrok.io |":                      "https://e.f.zrok.io",
		"see https://g.h.zrok.io.":                     "https://g.h.zrok.io",
		"nothing here":                                 "",
		"http://insecure.example.com":                  "",
	}
	for line, want := range cases {
		if got := findURL(line); got != want {
			t.Errorf("findURL(%q) = %q, wanted %q", line, got, want)
		}
	}
}

// Stopping something that is not running is a state the board can ask for,
// since it can be a click behind. It must not panic on the nil it holds.
func TestStoppingAnOverlayThatIsNotRunning(t *testing.T) {
	o := newOverlays()
	o.get(OverlayZrok).stop()
	if st := o.get(OverlayZrok).state(""); st.Running {
		t.Fatal("an overlay that never started reports as running")
	}
}

// The default is what gets shared when nobody says otherwise, and it has to be
// the board rather than an empty string that fails at the command line.
func TestOverlayBackendDefaultsToTheBoard(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	if got := d.zrokConfig().Backend; !strings.Contains(got, d.opts.HumanAddr) {
		t.Fatalf("zrok would publish %q, not the board at %q", got, d.opts.HumanAddr)
	}
}

// Two clicks, or two tabs, must not each launch a share. The second used to
// overwrite the first, leaving one publishing the board with nothing tracking
// it and no way to stop it from here.
func TestOnlyOneShareStartsAtATime(t *testing.T) {
	v := newOverlays().get(OverlayZrok)
	v.mu.Lock()
	v.starting = true
	v.mu.Unlock()

	if err := v.start("does-not-matter", nil, nil); err == nil {
		t.Fatal("a second share started while the first was still starting")
	}
	if !v.state("").Running {
		t.Fatal("a share that is starting does not report as running, so the board would offer start again")
	}
}

// Private, because this board has no login and the safe default for something
// with no login is the one that needs an account on the other end.
func TestZrokDefaultsToPrivate(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	if got := d.zrokConfig().Mode; got != "private" {
		t.Fatalf("the default share mode is %q, wanted private", got)
	}
}

// Configuration outlives a restart, like everything else the daemon keeps.
func TestOverlayConfigIsRemembered(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	if err := d.saveOverlay("ziti", []byte(
		`{"identity":"C:/id.json","extra":"--verbose"}`)); err != nil {
		t.Fatal(err)
	}
	got := d.zitiConfig()
	if got.Identity != "C:/id.json" || got.Extra != "--verbose" {
		t.Fatalf("what came back is not what was saved: %+v", got)
	}
}

// An overlay nobody has heard of is a typo in a URL, not a reason to start
// something.
func TestUnknownOverlayIsRefused(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	if err := d.saveOverlay("tailscale", []byte(`{}`)); err == nil {
		t.Fatal("an overlay atrium does not have was saved")
	}
	if err := d.startOverlay("tailscale"); err == nil {
		t.Fatal("an overlay atrium does not have was started")
	}
}
