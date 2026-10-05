package cli

import (
	"bytes"
	"log"
	"os"
	"reflect"
	"strings"
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

// A room restarts as a room, with its own flag names and its recorded bind.
func TestRestartKeepsARoomAsARoom(t *testing.T) {
	loc := daemon.Location{Room: "r1", DB: "r.db", AgentListen: "127.0.0.1:7777", BoardListen: "127.0.0.1:7778"}
	want := []string{"room", "--db", "r.db", "--agent", "127.0.0.1:7777", "--http", "127.0.0.1:7778"}
	if got := restartDaemonArgs("x.db", loc, true); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v want %v", got, want)
	}
}

func TestWarnWideBoard(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)
	for addr, wide := range map[string]bool{
		"127.0.0.1:7778": false, "localhost:7778": false, "[::1]:7778": false, "-": false,
		":7778": true, "0.0.0.0:7778": true, "192.168.1.5:7778": true,
	} {
		buf.Reset()
		warnWideBoard(addr)
		if got := strings.Contains(buf.String(), "WARNING"); got != wide {
			t.Errorf("%q: warned=%v want %v", addr, got, wide)
		}
	}
}
