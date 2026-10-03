package cli

import (
	"reflect"
	"testing"

	"github.com/dovholuknf/atrium/internal/daemon"
)

// A restart must hand the new daemon the bind the old one had, and never a
// wider one.
func TestRestartKeepsTheDaemonsBind(t *testing.T) {
	cases := []struct {
		name string
		loc  daemon.Location
		have bool
		want []string
	}{
		{"loopback stays loopback",
			daemon.Location{AgentListen: "127.0.0.1:7777", BoardListen: "127.0.0.1:7778"}, true,
			[]string{"daemon", "--db", "x.db", "--addr", "127.0.0.1:7777", "--http", "127.0.0.1:7778"}},
		{"an explicit wide bind is kept",
			daemon.Location{AgentListen: "127.0.0.1:7777", BoardListen: "0.0.0.0:7778"}, true,
			[]string{"daemon", "--db", "x.db", "--addr", "127.0.0.1:7777", "--http", "0.0.0.0:7778"}},
		{"an older file with no bind falls to loopback on its port",
			daemon.Location{Agent: "http://localhost:7777", Board: "http://localhost:7778"}, true,
			[]string{"daemon", "--db", "x.db", "--addr", "127.0.0.1:7777", "--http", "127.0.0.1:7778"}},
		{"no file leaves the flag defaults, which are loopback",
			daemon.Location{}, false, []string{"daemon", "--db", "x.db"}},
	}
	for _, c := range cases {
		if got := restartDaemonArgs("x.db", c.loc, c.have); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

// With no flag the daemon listens on loopback only.
func TestDaemonDefaultsAreLoopback(t *testing.T) {
	c := newDaemon()
	for _, f := range []string{"addr", "http"} {
		v := c.Flags().Lookup(f).DefValue
		if len(v) < 10 || v[:10] != "127.0.0.1:" {
			t.Errorf("--%s defaults to %q, want a 127.0.0.1 address", f, v)
		}
	}
}
