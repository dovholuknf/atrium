//go:build integration

package conn

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// A ROOM WHOSE RECORD GOES WHILE IT IS ATTACHED IS LET GO.
//
// The hub's store is a file, and `atrium2 hub room rm --force` is another
// process writing to it. Checking only at attach would leave the hub proxying
// to a room it has no record of: not on the list, not nameable, and not
// something anything else in the design knows how to describe.
func TestARoomWhoseRecordGoesIsLetGo(t *testing.T) {
	h := NewHub(Timings{Beat: 20 * time.Millisecond, Silence: time.Hour})

	var gone bool
	var mu sync.Mutex
	h.Attaching = func(string, string, string) error {
		mu.Lock()
		defer mu.Unlock()
		if gone {
			return errors.New("this hub has no room called sparta")
		}
		return nil
	}

	hub, room := net.Pipe()
	defer room.Close()
	// The room's side has to be read continuously or the pipe blocks the hub.
	go func() {
		buf := make([]byte, 256)
		for {
			if _, err := room.Read(buf); err != nil {
				return
			}
		}
	}()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go h.control(ctx, "sparta", hello{}, hub, bufio.NewReader(hub), "")

	waitFor(t, 5*time.Second, func() bool { return h.Has("sparta") })

	mu.Lock()
	gone = true
	mu.Unlock()

	waitFor(t, 5*time.Second, func() bool { return !h.Has("sparta") })
}

// A ROOM THE HUB HAS NO RECORD OF DOES NOT ATTACH, even holding papers this hub
// signed. A room that was forced out keeps its certificate, and a hub that let
// it back in would be proxying to something it cannot name, settle, or mark for
// deletion.
func TestARoomWithNoRecordIsTurnedAway(t *testing.T) {
	h := NewHub(Timings{})
	h.Attaching = func(name, _, _ string) error {
		if name == "sparta" {
			return nil
		}
		return errors.New("this hub has no room called " + name)
	}

	hub, room := net.Pipe()
	defer hub.Close()
	defer room.Close()

	go h.control(t.Context(), "athens", hello{}, hub, bufio.NewReader(hub), "")

	_ = room.SetDeadline(time.Now().Add(10 * time.Second))
	var w welcome
	if err := readJSON(bufio.NewReader(room), &w); err != nil {
		t.Fatal(err)
	}
	if w.OK {
		t.Fatal("a room the hub has no record of was adopted")
	}
	if !strings.Contains(w.Error, "no room called athens") {
		t.Fatalf("the room was told %q", w.Error)
	}
	if h.Has("athens") {
		t.Fatal("the refused room is in the hub's list anyway")
	}
}

// A KEYLESS ATTACH CANNOT REPLACE A KEYED ROOM, on any transport. Before f-026 an
// empty key slipped past the comparison in `Hub.control`. Over a pipe, with the
// key read from a table, so only that comparison is under test.
func TestAKeylessAttachCannotReplaceAKeyedRoom(t *testing.T) {
	var mu sync.Mutex
	keys := map[net.Conn]string{}

	h := NewHub(Timings{Beat: time.Hour, Silence: time.Hour, Warm: 1})
	h.peerKey = func(c net.Conn) string {
		mu.Lock()
		defer mu.Unlock()
		if k, ok := keys[c]; ok {
			return k
		}
		return peerKey(c)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	dial := func(key string) welcome {
		t.Helper()
		srv, cli := net.Pipe()
		t.Cleanup(func() { _ = cli.Close() })
		mu.Lock()
		keys[srv] = key
		mu.Unlock()
		go h.control(ctx, "alpha", hello{V: Version, Kind: "control", Room: "alpha"}, srv, bufio.NewReader(srv), "")
		var w welcome
		_ = cli.SetDeadline(time.Now().Add(5 * time.Second))
		br := bufio.NewReader(cli)
		if err := readJSON(br, &w); err != nil {
			t.Fatal(err)
		}
		// A PIPE HAS NO BUFFER, so the room keeps reading, as a socket would let
		// the hub write its goodbye when this link is replaced.
		_ = cli.SetDeadline(time.Time{})
		go func() { _, _ = io.Copy(io.Discard, br) }()
		return w
	}
	if w := dial("key-one"); !w.OK {
		t.Fatalf("the keyed room was refused: %+v", w)
	}
	if w := dial(""); w.OK || !strings.Contains(w.Error, "different room") {
		t.Fatalf("a keyless attach over a keyed room was answered %+v", w)
	}
	if w := dial("key-one"); !w.OK {
		t.Fatalf("the same key could not reconnect: %+v", w)
	}
}
