package link

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/zitictx"
)

type fakeZrok struct {
	shares  map[string]bool
	created int
	deleted []string
	dialed  []string
	askErr  error
}

func (f *fakeZrok) Exists(t string) (bool, error) { return f.shares[t], f.askErr }

func (f *fakeZrok) Create() (string, error) {
	f.created++
	t := "share" + string(rune('A'+f.created))
	f.shares[t] = true
	return t, nil
}

func (f *fakeZrok) Delete(t string) error {
	f.deleted = append(f.deleted, t)
	delete(f.shares, t)
	return nil
}

func (f *fakeZrok) Listen(string) (net.Listener, error) { return nil, errors.New("not here") }

func (f *fakeZrok) Dial(t string) (net.Conn, error) {
	f.dialed = append(f.dialed, t)
	c, _ := net.Pipe()
	return c, nil
}

func TestShareIsReusedWhenItIsStillThere(t *testing.T) {
	api := &fakeZrok{shares: map[string]bool{}}
	first, err := (&Zrok{API: api}).Share("")
	if err != nil {
		t.Fatal(err)
	}
	again, err := (&Zrok{API: api}).Share(first)
	if err != nil || again != first || api.created != 1 {
		t.Fatalf("a restart gave %q (%v) after %q with %d created, want the same share and one reservation",
			again, err, first, api.created)
	}
}

func TestShareIsReplacedWhenItIsGone(t *testing.T) {
	api := &fakeZrok{shares: map[string]bool{}}
	first, _ := (&Zrok{API: api}).Share("")
	delete(api.shares, first)
	z := &Zrok{API: api}
	next, err := z.Share(first)
	if err != nil || next == first || api.created != 2 || z.Reserved() != next {
		t.Fatalf("got %q (%v) with %d created, want a new share", next, err, api.created)
	}
}

func TestShareNotKnownToBeGoneIsNotReplaced(t *testing.T) {
	api := &fakeZrok{shares: map[string]bool{"old": true}, askErr: errors.New("controller down")}
	if _, err := (&Zrok{API: api}).Share("old"); err == nil || api.created != 0 {
		t.Fatalf("got %v with %d created, want an error and no new share", err, api.created)
	}
}

func TestRetireDeletesTheShareAndNothingElseDoes(t *testing.T) {
	api := &fakeZrok{shares: map[string]bool{}}
	z := &Zrok{API: api}
	tok, _ := z.Share("")
	z.Close()
	if len(api.deleted) != 0 {
		t.Fatalf("Close deleted %v", api.deleted)
	}
	if err := z.Retire(tok); err != nil || len(api.deleted) != 1 || api.deleted[0] != tok {
		t.Fatalf("Retire gave %v and deleted %v", err, api.deleted)
	}
}

func TestARoomDialsTheTokenItWasGiven(t *testing.T) {
	api := &fakeZrok{shares: map[string]bool{}}
	hubTok, _ := (&Zrok{API: api}).Share("")
	room := &Zrok{Token: hubTok, API: api}
	for i := 0; i < 2; i++ {
		restarted := &Zrok{API: api}
		if tok, _ := restarted.Share(hubTok); tok != hubTok {
			t.Fatalf("restart %d moved the share to %q", i, tok)
		}
		c, err := room.Dial(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		c.Close()
	}
	if len(api.dialed) != 2 || api.dialed[0] != hubTok || api.dialed[1] != hubTok {
		t.Fatalf("the room dialled %v, want %s twice", api.dialed, hubTok)
	}
}

func TestZrokDialsAndResetsShareOneProcessContext(t *testing.T) {
	reg := fakeSDK(t)
	var hour atomic.Int64
	api := sdkZrok{identity: "zrok-env.json"}
	for i := 0; i < 20; i++ {
		z := &Zrok{Token: "tok", API: api}
		z.gate.now = func() time.Time { return time.Unix(hour.Add(3600), 0) }
		l := NewLadder([]Rung{{Path: Path{Transport: "zrok"}, Dial: z}})
		for j := 0; j < 10; j++ {
			_, _ = l.Dial(context.Background())
			l.Reset()
		}
		l.Close()
	}
	deadline := time.Now().Add(2 * time.Second)
	for reg.relogins.Load() < 200 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if reg.built.Load() != 1 || zitictx.Live() != 1 || reg.closed.Load() != 0 {
		t.Fatalf("built %d, live %d, closed %d, want 1, 1 and 0", reg.built.Load(), zitictx.Live(), reg.closed.Load())
	}
	if reg.dialed.Load() != 200 || reg.relogins.Load() != 200 {
		t.Fatalf("dialed %d, relogins %d, want 200 and 200", reg.dialed.Load(), reg.relogins.Load())
	}
}
