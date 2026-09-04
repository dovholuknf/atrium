package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Where the daemon writes its address, and the rule that a test must never
// write it to the machine's real one.
//
// Run writes this file on start and deletes it on stop. A test that starts a
// daemon and stops it was therefore taking the address of whatever daemon was
// actually running while the suite ran, and putting nothing back. Nothing
// broke, because a caller that cannot find the file falls back to the default
// port and the real daemon was on the default port, which is exactly why it
// went unnoticed for as long as it did. On any other port it would have
// quietly unhooked every live session on the machine.

func TestTheAddressGoesWhereTheOptionsSay(t *testing.T) {
	d, _, cancel, errCh := startDaemon(t)

	want := d.opts.LocationFile
	if want == "" {
		t.Fatal("the test daemon is writing to the machine's real location file")
	}
	raw, err := os.ReadFile(want)
	if err != nil {
		t.Fatalf("the daemon did not record its address: %v", err)
	}
	var loc Location
	if err := json.Unmarshal(raw, &loc); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(loc.Agent, d.opts.AgentAddr) {
		t.Fatalf("recorded agent address %q, wanted one ending %q", loc.Agent, d.opts.AgentAddr)
	}
	if loc.PID != os.Getpid() {
		t.Fatalf("recorded pid %d", loc.PID)
	}

	cancel()
	<-errCh
	if _, err := os.Stat(want); !os.IsNotExist(err) {
		t.Fatal("the address outlived the daemon, so a hook would aim at a port nobody holds")
	}
}

// Not naming a file means the machine's real one. That is what a real daemon
// wants, and the default has to stay the default: an option that has to be set
// to get correct behavior is an option everybody forgets.
func TestNoOptionMeansTheRealPlace(t *testing.T) {
	real, err := LocationPath()
	if err != nil {
		t.Skip("this machine has no answer for where runtime state goes")
	}
	d := &Daemon{}
	got, err := d.locationPath()
	if err != nil {
		t.Fatal(err)
	}
	if got != real {
		t.Fatalf("a daemon with no option chose %q, wanted %q", got, real)
	}

	d.opts.LocationFile = filepath.Join(t.TempDir(), "elsewhere.json")
	got, err = d.locationPath()
	if err != nil {
		t.Fatal(err)
	}
	if got != d.opts.LocationFile {
		t.Fatalf("the option was ignored: %q", got)
	}
}

// Taking the file off a daemon that is still running is announced. The symptom
// otherwise is every hook in every session quietly arriving at the wrong
// daemon, which looks like nothing at all.
//
// Every other case has to stay silent, or the warning becomes noise and stops
// being read. That is most of what this checks.
func TestTakingTheAddressFromALiveDaemonIsAnnounced(t *testing.T) {
	const me = 1000
	living := func(int) bool { return true }
	gone := func(int) bool { return false }

	other, err := json.Marshal(Location{Agent: "http://localhost:9999", PID: 2000})
	if err != nil {
		t.Fatal(err)
	}
	mine, err := json.Marshal(Location{Agent: "http://localhost:9999", PID: me})
	if err != nil {
		t.Fatal(err)
	}
	noPID, err := json.Marshal(Location{Agent: "http://localhost:9999"})
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name string
		raw  []byte
		live func(int) bool
		warn bool
	}{
		{"another daemon, still running", other, living, true},
		{"another daemon, already gone", other, gone, false},
		{"this process restarting in place", mine, living, false},
		{"a file with no pid in it", noPID, living, false},
		{"a file that does not parse", []byte("{not json"), living, false},
	} {
		prev, got := takingOver(tc.raw, me, tc.live)
		if got != tc.warn {
			t.Fatalf("%s: warned=%v, wanted %v", tc.name, got, tc.warn)
		}
		if got && prev.Agent == "" {
			t.Fatalf("%s: warned without saying which address was taken over", tc.name)
		}
	}
}

// And the warning actually reaches the log, rather than only being decided.
func TestTheTakeoverWarningIsPrinted(t *testing.T) {
	d, logs, cancel, errCh := startDaemon(t)
	defer func() { cancel(); <-errCh }()

	raw, err := json.Marshal(Location{Agent: "http://localhost:9999", PID: os.Getpid() + 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(d.opts.LocationFile, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	// processAlive is the real one here, so this only proves the wiring when
	// that pid happens to exist. Checked either way: the point is that
	// writeLocation still writes, whatever it decided about warning.
	d.writeLocation()

	back, err := os.ReadFile(d.opts.LocationFile)
	if err != nil {
		t.Fatal(err)
	}
	var loc Location
	if err := json.Unmarshal(back, &loc); err != nil {
		t.Fatal(err)
	}
	if loc.PID != os.Getpid() {
		t.Fatal("writing the address did not take the file over")
	}
	if strings.Contains(logs.String(), "already listening") &&
		!strings.Contains(logs.String(), "arrive here instead") {
		t.Fatal("the warning does not say what happens next")
	}
}
