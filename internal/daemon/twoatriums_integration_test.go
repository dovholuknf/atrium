//go:build integration

package daemon

import (
	"net"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/dovholuknf/atrium/internal/daemon/shared"
)

// Two atriums side by side: the machine's room and an isolated one, each opened fresh under one home.
func TestAnIsolatedRoomWiresItsOwnMCPFileAndAimsItsHooksAtItself(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	open := func(name, location string) *Daemon {
		dir := filepath.Join(t.TempDir(), name)
		d, err := New(Options{DBPath: filepath.ToSlash(filepath.Join(dir, "atrium.db")), LocationFile: location})
		if err != nil {
			t.Fatal(err)
		}
		closeAtCleanup(t, d)
		return d
	}
	first := open("first", "")
	second := open("second", filepath.Join(t.TempDir(), "second.json"))

	mcpOf := func(d *Daemon) string {
		h, err := d.st.Harness("claude")
		if err != nil {
			t.Fatal(err)
		}
		i := slices.Index(h.Args, "--mcp-config")
		if i < 0 {
			t.Fatalf("no --mcp-config in %q", h.Args)
		}
		return h.Args[i+1]
	}
	a, b := mcpOf(first), mcpOf(second)
	if want := filepath.ToSlash(filepath.Join(home, ".atrium", "mcp.json")); a != want {
		t.Fatalf("the machine's room uses %q, want %q", a, want)
	}
	if b == a {
		t.Fatalf("the isolated room uses the other room's mcp.json %q", b)
	}
	if want := filepath.ToSlash(filepath.Dir(second.opts.DBPath)); filepath.ToSlash(filepath.Dir(filepath.Dir(b))) != want {
		t.Fatalf("the isolated room's mcp.json %q is not beside its database %q", b, want)
	}
	if _, err := os.Stat(b); err != nil {
		t.Fatalf("the isolated room's mcp.json was not written: %v", err)
	}

	second.SetAgentAddr(&net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 7887})
	t.Setenv("ATRIUM_LOCATION", "")
	t.Setenv("ATRIUM_HUB_URL", "http://127.0.0.1:7777")
	got := launchDump(t, second, shared.LaunchRequest{})
	if got["ATRIUM_HUB_URL"] != "http://127.0.0.1:7887" {
		t.Fatalf("the isolated room's runner is aimed at %q", got["ATRIUM_HUB_URL"])
	}
	if got["ATRIUM_LOCATION"] != second.opts.LocationFile {
		t.Fatalf("the isolated room's runner reads its address from %q, want %q", got["ATRIUM_LOCATION"], second.opts.LocationFile)
	}
}
