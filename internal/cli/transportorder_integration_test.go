//go:build integration

package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/hubstore"
	"github.com/dovholuknf/atrium/internal/link"
	"github.com/dovholuknf/atrium/internal/link/wire"
)

// See docs/rnd/room-transport-order.md.

func runHubTransports(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	c := roomTransportsCmd("")
	var out bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&out)
	c.SetArgs(append(args, "--dir", dir, "--board-addr", "127.0.0.1:1"))
	err := c.Execute()
	return out.String(), err
}

func TestRoomsTransportsSetsTheHubOrder(t *testing.T) {
	dir := t.TempDir()
	out, err := runHubTransports(t, dir)
	if err != nil || !strings.HasPrefix(out, "ziti,direct,zrok") {
		t.Fatalf("default: %q, %v, want ziti,direct,zrok", out, err)
	}
	if out, err = runHubTransports(t, dir, "openziti,direct"); err != nil || !strings.HasPrefix(out, "ziti,direct") {
		t.Fatalf("set: %q, %v", out, err)
	}
	store, err := hubstore.Open(filepath.Join(dir, "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	v, err := store.RoomTransportOrder()
	store.Close()
	if err != nil || v != "ziti,direct" {
		t.Fatalf("the store holds %q, %v", v, err)
	}
	if _, err = runHubTransports(t, dir, "direct,smoke-signals"); err == nil {
		t.Fatal("an unknown transport was accepted")
	}
	if out, _ = runHubTransports(t, dir); !strings.HasPrefix(out, "ziti,direct") {
		t.Fatalf("junk changed the order: %q", out)
	}
}

// The hub tells rooms only what it listens on, in its order, and never a credential.
func TestHubPathsFollowTheOrderAndSayNoSecret(t *testing.T) {
	set := hubSet{sides: []*hubSide{
		{kind: "direct", path: wire.Path{Transport: "direct", Addr: "sg4:7779"}},
		{kind: "ziti", path: wire.Path{Transport: "ziti", Service: "atrium-hub"}},
		{kind: "zrok", path: wire.Path{Transport: "zrok", Share: "secret-share"}},
	}}
	ps := set.paths([]string{"ziti", "direct"})
	if len(ps) != 2 || ps[0].Transport != "ziti" || ps[1].Transport != "direct" {
		t.Fatalf("got %+v, want ziti then direct and no zrok", ps)
	}
	for _, p := range ps {
		if p.Share != "" || p.Identity != "" {
			t.Fatalf("a credential went to rooms: %+v", p)
		}
	}
}

// A room that joined over direct, with a ziti identity configured by hand, gets
// a ziti rung wrapped in its certificate, so the hub still knows its name. Ziti
// comes first, the default order.
func TestRoomLadderWrapsTheZitiFallbackInTheRoomCertificate(t *testing.T) {
	keys := link.Keys{Dir: t.TempDir()}
	writeStrayRoomCert(t, keys)
	saved := link.Saved{Transport: "direct", Hub: "sg4:7779", Room: "m1mini"}
	tr := link.RoomTransports{Paths: []wire.Path{{Transport: "ziti", Identity: "m1mini.json"}}}
	l, skipped, err := roomLadder(keys, saved, tr)
	if err != nil || len(skipped) != 0 {
		t.Fatalf("%v %v", err, skipped)
	}
	rs := l.Rungs()
	if len(rs) != 2 || rs[0].Path.Transport != "ziti" || rs[1].Path.Transport != "direct" {
		t.Fatalf("rungs %+v, want ziti then direct, the default", rs)
	}
	if _, ok := rs[1].Dial.(link.Direct); !ok {
		t.Fatalf("direct rung is %T", rs[1].Dial)
	}
	if _, ok := rs[0].Dial.(link.Proven); !ok {
		t.Fatalf("the ziti rung is %T, want it inside the room's certificate", rs[0].Dial)
	}
}

// An old-form overlay join keeps its old path on its own rung.
func TestRoomLadderLeavesAnOldFormOverlayJoinUnwrapped(t *testing.T) {
	keys := link.Keys{Dir: t.TempDir()}
	writeStrayRoomCert(t, keys)
	saved := link.Saved{Transport: "ziti", Service: "atrium-hub", Identity: "vm1.json", Room: "vm1"}
	l, _, err := roomLadder(keys, saved, link.RoomTransports{})
	if err != nil {
		t.Fatal(err)
	}
	rs := l.Rungs()
	if len(rs) != 1 {
		t.Fatalf("rungs %+v", rs)
	}
	if _, ok := rs[0].Dial.(link.Proven); ok {
		t.Fatal("an old-form ziti join was wrapped")
	}
	saved.Proven = true
	l, _, _ = roomLadder(keys, saved, link.RoomTransports{})
	if _, ok := l.Rungs()[0].Dial.(link.Proven); !ok {
		t.Fatal("a proven ziti join was not wrapped when the room started from what it saved")
	}
}

func TestSavedOverlayJoinRemembersItWasProven(t *testing.T) {
	keys := link.Keys{Dir: t.TempDir()}
	j := provenZitiJoin(t)
	if err := keys.SaveOverlayRoom(j, "vm1", "vm1.json"); err != nil {
		t.Fatal(err)
	}
	s, err := keys.Joined()
	if err != nil || !s.Proven {
		t.Fatalf("saved %+v, %v, want proven", s, err)
	}
}

func runRoomTransports(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	c := roomTransportsLocalCmd()
	var out bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&out)
	c.SetArgs(append(args, "--dir", dir))
	err := c.Execute()
	return out.String(), err
}

func TestRoomTransportsVerbWritesTheLocalSettings(t *testing.T) {
	dir := t.TempDir()
	keys := link.Keys{Dir: dir}
	if err := os.WriteFile(filepath.Join(dir, "room.json"),
		[]byte(`{"hub":"sg4:7779","room":"m1mini"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	id := filepath.Join(dir, "m1mini.json")
	if err := os.WriteFile(id, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := runRoomTransports(t, dir, "ziti", "--identity", id, "--service", "atrium-hub"); err != nil {
		t.Fatal(err)
	}
	if _, err := runRoomTransports(t, dir, "order", "ziti,direct"); err != nil {
		t.Fatal(err)
	}
	tr, err := keys.Transports()
	if err != nil {
		t.Fatal(err)
	}
	if p := tr.Local("ziti"); p.Identity != filepath.ToSlash(id) || p.Service != "atrium-hub" {
		t.Fatalf("ziti saved as %+v", p)
	}
	if strings.Join(tr.Order, ",") != "ziti,direct" {
		t.Fatalf("order saved as %v", tr.Order)
	}
	out, err := runRoomTransports(t, dir)
	if err != nil {
		t.Fatal(err)
	}
	zi, di := strings.Index(out, "ziti"), strings.Index(out, "direct")
	if zi < 0 || di < 0 || zi > di || !strings.Contains(out, "sg4:7779") {
		t.Fatalf("the ladder should list ziti then direct at sg4:7779:\n%s", out)
	}
	if _, err := runRoomTransports(t, dir, "order", "--clear"); err != nil {
		t.Fatal(err)
	}
	if tr, _ = keys.Transports(); len(tr.Order) != 0 {
		t.Fatalf("--clear left %v", tr.Order)
	}
	if _, err := runRoomTransports(t, dir, "ziti", "--identity", filepath.Join(dir, "missing.json")); err == nil {
		t.Fatal("an identity file that is not there was accepted")
	}
}
