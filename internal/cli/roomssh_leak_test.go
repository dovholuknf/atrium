package cli

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"

	"github.com/dovholuknf/atrium/internal/daemon"
	"github.com/dovholuknf/atrium/internal/link"
	"github.com/dovholuknf/atrium/internal/sshwizard"
	"github.com/dovholuknf/atrium/internal/zitictx"
)

type countedRung struct {
	built, closed, dials *atomic.Int64
}

func (c countedRung) Dial(context.Context) (net.Conn, error) {
	c.dials.Add(1)
	return nil, errors.New("hub unreachable")
}

func (countedRung) Describe() string { return "counted" }

func (c countedRung) Close() { c.closed.Add(1) }

type rungCounts struct{ built, closed, dials atomic.Int64 }

func (r *rungCounts) live() int64 { return r.built.Load() - r.closed.Load() }

func (r *rungCounts) rung() countedRung {
	return countedRung{built: &r.built, closed: &r.closed, dials: &r.dials}
}

func countZitiRungs(t *testing.T) *rungCounts {
	t.Helper()
	rc := &rungCounts{}
	old := newZitiRung
	newZitiRung = func(string, string) link.Dialer {
		rc.built.Add(1)
		return rc.rung()
	}
	t.Cleanup(func() { newZitiRung = old })
	return rc
}

func joinedOverZiti(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	j := link.Join{Transport: "ziti", Service: "atrium-hub"}
	if err := (link.Keys{Dir: dir}).SaveOverlayRoom(j, "vm1", "vm1.json"); err != nil {
		t.Fatal(err)
	}
	return dir
}

func hubCalls(m sshwizard.LocalMachine) {
	ctx := context.Background()
	_, _ = m.PublishFn(ctx)
	_, _ = m.PeersFn(ctx)
	_ = m.SendFn(ctx, sshwizard.PublishRecord{User: "claude", Key: "ssh-ed25519 AAAA", Port: 22})
	_, _ = m.PublishFn(ctx)
}

func TestSSHSyncOnTheRoomsLadderBuildsNoDialersOfItsOwn(t *testing.T) {
	rc := countZitiRungs(t)
	dir := joinedOverZiti(t)
	m := localSSHMachine(dir, rc.rung())
	for i := 0; i < 5; i++ {
		hubCalls(m)
	}
	if got := rc.built.Load(); got != 0 {
		t.Fatalf("five passes built %d ziti dialers, want 0: the room's own ladder is the one to dial", got)
	}
	if rc.dials.Load() == 0 {
		t.Fatal("the room's ladder was never dialled")
	}
	if got := rc.closed.Load(); got != 0 {
		t.Fatalf("the room's shared ladder was closed %d times", got)
	}
}

func TestSSHCallsWithNoLadderAndNoRoomBuildNoDialer(t *testing.T) {
	rc := countZitiRungs(t)
	dir := joinedOverZiti(t)
	prev := localRoomLocation
	localRoomLocation = func() (daemon.Location, bool) { return daemon.Location{}, false }
	t.Cleanup(func() { localRoomLocation = prev })
	m := localSSHMachine(dir, nil)
	for i := 0; i < 5; i++ {
		hubCalls(m)
	}
	if rc.built.Load() != 0 || zitictx.Live() != 0 {
		t.Fatalf("built %d dialers, %d ziti contexts live, want 0 and 0", rc.built.Load(), zitictx.Live())
	}
}
