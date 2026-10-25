//go:build integration

package cli

import (
	"context"
	"errors"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/link"
)

type fakeShares struct {
	live    map[string]bool
	created int
	deleted []string
	askErr  error
	bound   chan net.Listener
}

func (f *fakeShares) Exists(t string) (bool, error) { return f.live[t], f.askErr }

func (f *fakeShares) Create() (string, error) {
	f.created++
	t := "share" + string(rune('0'+f.created))
	f.live[t] = true
	return t, nil
}

func (f *fakeShares) Delete(t string) error {
	f.deleted = append(f.deleted, t)
	delete(f.live, t)
	return nil
}

func (f *fakeShares) Listen(string) (net.Listener, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err == nil && f.bound != nil {
		f.bound <- ln
	}
	return ln, err
}

func (f *fakeShares) Dial(string) (net.Conn, error) { return nil, os.ErrInvalid }

func (f *fakeShares) Access(string) error { return nil }

func hubStart(t *testing.T, keys link.Keys, api link.ZrokAPI) (token string, stop func()) {
	t.Helper()
	side, err := newZrokHub(keys, nil, api)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := side.listen()
	if err != nil {
		t.Fatal(err)
	}
	line, err := side.joinString("vm1", "secret")
	if err != nil {
		t.Fatal(err)
	}
	j, err := link.ParseToken(line)
	if err != nil {
		t.Fatal(err)
	}
	return j.ShareToken, func() { ln.Close(); side.release() }
}

func TestAHubRestartKeepsItsZrokShareAndItsRoomsToken(t *testing.T) {
	keys := link.Keys{Dir: t.TempDir()}
	api := &fakeShares{live: map[string]bool{}}
	first, stop := hubStart(t, keys, api)
	stop()
	if len(api.deleted) != 0 {
		t.Fatalf("a clean stop deleted %v", api.deleted)
	}
	second, stop := hubStart(t, keys, api)
	defer stop()
	if second != first || api.created != 1 {
		t.Fatalf("a restart minted %q after %q with %d reserved, want the same share and one", second, first, api.created)
	}
	if saved, _ := readZrokShare(keys); saved != first {
		t.Fatalf("the share file says %q, want %q", saved, first)
	}
}

func TestAHubReservesANewZrokShareOnlyWhenTheOldIsGone(t *testing.T) {
	keys := link.Keys{Dir: t.TempDir()}
	api := &fakeShares{live: map[string]bool{}}
	first, stop := hubStart(t, keys, api)
	stop()
	delete(api.live, first)
	second, stop := hubStart(t, keys, api)
	defer stop()
	if second == first || api.created != 2 {
		t.Fatalf("got %q after %q with %d reserved, want a replacement", second, first, api.created)
	}
	if saved, _ := readZrokShare(keys); saved != second {
		t.Fatalf("the share file says %q, want %q", saved, second)
	}
}

func TestRetireShareDeletesItAndForgetsIt(t *testing.T) {
	keys := link.Keys{Dir: t.TempDir()}
	api := &fakeShares{live: map[string]bool{}}
	first, stop := hubStart(t, keys, api)
	stop()
	got, err := retireZrokShare(keys, api)
	if err != nil || got != first || len(api.deleted) != 1 || api.live[first] {
		t.Fatalf("retire gave %q (%v), deleted %v", got, err, api.deleted)
	}
	if _, err := readZrokShare(keys); err == nil || !strings.Contains(err.Error(), "--transport zrok") {
		t.Fatalf("the share is still written down: %v", err)
	}
	next, stop := hubStart(t, keys, api)
	defer stop()
	if next == first {
		t.Fatalf("a retired share came back as %q", next)
	}
}

func TestRoomZrokSeedIsOnOnlyWhenTransportNamesZrok(t *testing.T) {
	if s := roomZrokSeed("direct,zrok"); s.On == nil || !*s.On {
		t.Fatalf("direct,zrok seeds %+v, want on", s)
	}
	if s := roomZrokSeed("direct,ziti"); s.On != nil {
		t.Fatalf("direct,ziti seeds %+v, want nothing", s)
	}
}

func TestAZrokFirstHubStartsWithTheControllerDownAndRebindsWhenItsShareGoes(t *testing.T) {
	keys := link.Keys{Dir: t.TempDir()}
	if err := writeZrokShare(keys, "old"); err != nil {
		t.Fatal(err)
	}
	api := &fakeShares{live: map[string]bool{"old": true}, askErr: errors.New("controller down"),
		bound: make(chan net.Listener, 1)}
	side, err := newZrokHub(keys, nil, api)
	if err != nil {
		t.Fatal(err)
	}
	h := link.NewHub(link.Timings{})
	waited, goOn := make(chan struct{}), make(chan struct{})
	b := zrokBinder(side, h, nil)
	b.Wait = func(ctx context.Context, _ time.Duration) bool {
		waited <- struct{}{}
		<-goOn
		return ctx.Err() == nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { b.Run(ctx); close(done) }()
	defer func() { cancel(); close(goOn); <-done }()

	<-waited
	if ls := h.Listeners(); len(ls) != 1 || ls[0].Up || !strings.Contains(ls[0].LastError, "could not ask zrok") {
		t.Fatalf("with the controller down the listeners are %+v, want zrok down and why", ls)
	}
	if saved, _ := readZrokShare(keys); saved != "old" || api.created != 0 {
		t.Fatalf("the share file says %q with %d reserved, want old and none", saved, api.created)
	}
	api.askErr = nil
	goOn <- struct{}{}
	first := <-api.bound

	delete(api.live, "old")
	first.Close()
	<-waited
	goOn <- struct{}{}
	<-api.bound
	if saved, _ := readZrokShare(keys); saved != "share1" || api.created != 1 {
		t.Fatalf("after the share went the file says %q with %d reserved, want share1 and one", saved, api.created)
	}
	if line, err := side.joinString("vm1", "secret"); err != nil || shareOf(t, line) != "share1" {
		t.Fatalf("the join string after the rebind: %v", err)
	}
}

func shareOf(t *testing.T, line string) string {
	t.Helper()
	j, err := link.ParseToken(line)
	if err != nil {
		t.Fatal(err)
	}
	return j.ShareToken
}
