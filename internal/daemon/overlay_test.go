package daemon

import (
	"net/http"
	"strings"
	"testing"
)

// The board is served on an overlay listener now rather than proxied to by a
// child process, so what used to be tested here, the command line and the
// scraping of an address out of its output, no longer exists. What is left is
// the configuration and the state machine around the listener.

// The default is what gets served when nobody says otherwise, and it has to be
// the board rather than an empty string.
func TestOverlayBackendDefaultsToTheBoard(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	if got := d.zrokConfig().Backend; !strings.Contains(got, d.opts.HumanAddr) {
		t.Fatalf("zrok would name %q, not the board at %q", got, d.opts.HumanAddr)
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
		`{"identity":"C:/id.json","service":"atrium-board","extra":"--verbose"}`)); err != nil {
		t.Fatal(err)
	}
	got := d.zitiConfig()
	if got.Identity != "C:/id.json" || got.Service != "atrium-board" {
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

// Each refusal names the field that is missing. Serving on an overlay fails
// for reasons the operator can fix, and "could not bind" is not one of them.
func TestZitiRefusesWithoutWhatItNeeds(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	err := d.startZitiNative(ZitiConfig{Service: "atrium-board"})
	if err == nil || !strings.Contains(err.Error(), "identity") {
		t.Fatalf("a missing identity gave %v", err)
	}
	err = d.startZitiNative(ZitiConfig{Identity: "C:/id.json"})
	if err == nil || !strings.Contains(err.Error(), "service") {
		t.Fatalf("a missing service gave %v", err)
	}
}

// The mode decides whether a link needs an account on the other end, so a
// typo in it must not quietly become the more open one.
func TestZrokRefusesAModeItDoesNotKnow(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	err := d.startZrokNative(ZrokConfig{Mode: "publik", Backend: "localhost:7778"})
	if err == nil {
		t.Fatal("a misspelled mode was accepted")
	}
	// Either answer is correct and which one depends on whether this machine
	// has zrok set up, so the test asserts it refused rather than why.
	if !strings.Contains(err.Error(), "public or private") &&
		!strings.Contains(err.Error(), "zrok environment") {
		t.Fatalf("the refusal explains neither problem: %v", err)
	}
}

// A public share with no login is the whole internet with an extra step, so the
// panel refuses to save the public toggle rather than letting it be discovered
// when the phone cannot get in. This is the design's save-time refusal.
func TestZrokPublicRefusedWithoutLogin(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	err := d.saveOverlay("zrok", []byte(`{"public":true}`))
	if err == nil || !strings.Contains(err.Error(), "needs a login") {
		t.Fatalf("a public share saved with no login gave %v", err)
	}
	// And starting one, in case a config predates the rule, is refused for the
	// same reason before it ever reaches the share command.
	if err := d.st.SetSetting(SettingOverlayZrok, `{"mode":"public","public":true}`); err != nil {
		t.Fatal(err)
	}
	if err := d.startOverlay("zrok"); err == nil || !strings.Contains(err.Error(), "needs a login") {
		t.Fatalf("a public share started with no login gave %v", err)
	}
}

// A private share is gated by an account holding its token, so it needs no login
// of its own and saving it must not be refused for lacking one.
func TestZrokPrivateSavesWithoutLogin(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	if err := d.saveOverlay("zrok", []byte(`{"private":true}`)); err != nil {
		t.Fatalf("a private share was refused for lacking a login it does not need: %v", err)
	}
}

// With a working login configured, the public toggle saves: the login is the
// authorisation a public URL cannot get from reachability.
func TestZrokPublicSavesWithALogin(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	if err := d.SaveAuth(AuthConfig{
		Enabled: true, Issuer: "https://idp.example/realms/x", ClientID: "atrium",
		Redirect: "https://atrium-abc.share.zrok.io/auth/callback", Allow: []string{"me@example.com"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := d.saveOverlay("zrok", []byte(`{"public":true}`)); err != nil {
		t.Fatalf("a public share with a login configured was still refused: %v", err)
	}
}

// Stopping something that is not running is a state the board can ask for,
// since it can be a click behind. It must not panic on the nil it holds.
func TestStoppingAnOverlayThatIsNotRunning(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	d.nat(OverlayZrok).stop(nil)
	if d.nat(OverlayZrok).running() {
		t.Fatal("an overlay that never started reports as running")
	}
	if st := d.nat(OverlayZrok).state(""); st.Running || st.Address != "" {
		t.Fatalf("a stopped overlay still offers %+v", st)
	}
}

// Two clicks must not each bind a listener. The second would take the service
// or the share and leave the first answering nothing.
func TestOnlyOneListenerPerOverlay(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	// Held directly, since binding one needs a network this test does not have.
	n := d.nat(OverlayZiti)
	n.mu.Lock()
	n.srv = &http.Server{}
	n.mu.Unlock()

	if err := d.startOverlay("ziti"); err == nil {
		t.Fatal("a second listener was started while one was already up")
	}
}
