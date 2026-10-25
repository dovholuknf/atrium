//go:build integration

package cli

import (
	"net"
	"os"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/link"
)

type fakeShares struct {
	live    map[string]bool
	created int
	deleted []string
}

func (f *fakeShares) Exists(t string) (bool, error) { return f.live[t], nil }

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

func (f *fakeShares) Listen(string) (net.Listener, error) { return net.Listen("tcp", "127.0.0.1:0") }

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
