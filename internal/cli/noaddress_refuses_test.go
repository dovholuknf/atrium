package cli

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTypedCommandsWithNoAddressFileRefuse(t *testing.T) {
	t.Setenv("ATRIUM_HUB_URL", "")
	t.Setenv("ATRIUM_AGENT_NAME", "tester")
	t.Setenv("ATRIUM_TASK_ID", "")
	loc := filepath.Join(t.TempDir(), "none.json")
	t.Setenv("ATRIUM_LOCATION", loc)
	t.Setenv("ATRIUM_SHARED_LOCATION", "-")

	calls := map[string]func() error{
		"join":   func() error { return sessionEvent("", "join", "", "", "") },
		"ready":  func() error { _, err := postReady("", "", nil); return err },
		"finish": func() error { return reportFinished(io.Discard, "", "", finishReport{Recap: "r"}) },
		"ask":    func() error { return reportStuck(io.Discard, "", "", "q", "", false) },
		"peers":  func() error { return listPeers(io.Discard, "", "", false, "") },
		"tell":   func() error { return tellPeer(io.Discard, "", "", "x", "hi", "", "", false) },
	}
	for name, call := range calls {
		err := call()
		if err == nil || !strings.Contains(err.Error(), "--url") || !strings.Contains(err.Error(), filepath.ToSlash(loc)) {
			t.Errorf("%s with no address file: %v, want a refusal naming --url and %s", name, err, loc)
		}
	}
	if _, ok := openStreams(); ok {
		t.Error("remote-access asked the default port with no address file")
	}

	if err := os.WriteFile(loc, []byte(`{"agent":"http://127.0.0.1:1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if addr, err := typedHubAddress("", "--url"); err != nil || addr != "http://127.0.0.1:1" {
		t.Errorf("with an address file: %q %v, want the recorded address", addr, err)
	}
	if addr, err := typedHubAddress("http://127.0.0.1:2/", "--url"); err != nil || addr != "http://127.0.0.1:2" {
		t.Errorf("with --url: %q %v, want the flag", addr, err)
	}
}
