package transport

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/zitictx"
	"github.com/openziti/sdk-golang/ziti"
	"github.com/openziti/sdk-golang/ziti/edge"
	"github.com/openziti/zrok/v2/rest_client_zrok/share"
)

type fakeZrok struct {
	shares  map[string]bool
	created int
	deleted []string
	dialed  []string
	access  []string
	accErr  error
	dialErr error
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
	if f.dialErr != nil {
		return nil, f.dialErr
	}
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

func (c *sdkCtx) DialWithOptions(string, *ziti.DialOptions) (edge.Conn, error) {
	c.reg.dialed.Add(1)
	return nil, errors.New("fake: no route")
}

func TestZrokRedialsHoldOneContext(t *testing.T) {
	reg := fakeSDK(t)
	z := &Zrok{Token: "tok", API: sdkZrok{identity: "zrok.json"}, accessed: "tok"}
	var hour atomic.Int64
	z.gate.now = func() time.Time { return time.Unix(hour.Add(3600), 0) }
	for i := 0; i < 200; i++ {
		if c, err := z.Dial(context.Background()); err == nil {
			c.Close()
		}
	}
	if reg.dialed.Load() != 200 || reg.built.Load() != 1 || zitictx.Live() != 1 {
		t.Fatalf("dialled %d, built %d, live %d, want 200, 1 and 1", reg.dialed.Load(), reg.built.Load(), zitictx.Live())
	}
}

func (f *fakeZrok) Access(t string) error {
	if f.accErr != nil {
		return f.accErr
	}
	f.access = append(f.access, t)
	return nil
}

func TestARoomAsksForAccessOnceBeforeItDials(t *testing.T) {
	api := &fakeZrok{shares: map[string]bool{}}
	room := &Zrok{Token: "tok", API: api}
	for i := 0; i < 3; i++ {
		c, err := room.Dial(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		c.Close()
	}
	if len(api.access) != 1 || api.access[0] != "tok" || len(api.dialed) != 3 {
		t.Fatalf("access %v, dialled %v, want tok once and three dials", api.access, api.dialed)
	}
}

func TestARoomWithNoAccessDoesNotDial(t *testing.T) {
	api := &fakeZrok{shares: map[string]bool{}, accErr: errors.New("refused")}
	if _, err := (&Zrok{Token: "tok", API: api}).Dial(context.Background()); err == nil || len(api.dialed) != 0 {
		t.Fatalf("got %v and dialled %v, want an error and no dial", err, api.dialed)
	}
}

func TestTheZrokBackoffSaysWhatFailed(t *testing.T) {
	cases := []struct {
		name    string
		api     *fakeZrok
		want    string
		accessd bool
	}{
		{"share gone", &fakeZrok{accErr: fmt.Errorf("unable to create access: %w", share.NewAccessNotFound())}, causeShareGone, false},
		{"no access", &fakeZrok{accErr: fmt.Errorf("unable to create access: %w", share.NewAccessUnauthorized())}, causeNoAccess, false},
		{"controller down", &fakeZrok{accErr: errors.New("dial tcp: connection refused")}, causeController, false},
		{"hub down", &fakeZrok{dialErr: errors.New("service tok has no terminators")}, causeHubDown, true},
		{"not found", &fakeZrok{dialErr: errors.New("service 'tok' not found")}, causeNotFound, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.api.shares = map[string]bool{}
			z := &Zrok{Token: "tok", API: c.api}
			if _, err := z.Dial(context.Background()); err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("the dial said %v, want %q", err, c.want)
			}
			_, err := z.Dial(context.Background())
			if err == nil || !strings.Contains(err.Error(), "not dialling") || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("the backoff said %v, want %q", err, c.want)
			}
			if got := z.accessed == "tok"; got != c.accessd {
				t.Fatalf("access kept %v, want %v", got, c.accessd)
			}
		})
	}
}
