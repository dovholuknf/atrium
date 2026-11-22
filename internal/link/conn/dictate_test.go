package conn

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"testing"
)

// fakeDictateHub answers one clip the way serveDictate does, or refuses the hello with the given welcome.
func fakeDictateHub(b net.Conn, refuse string, ans DictateAnswer, got chan<- []byte) {
	defer b.Close()
	br := bufio.NewReader(b)
	var hi hello
	_ = readJSON(br, &hi)
	if refuse != "" {
		_ = writeJSON(b, welcome{OK: false, Error: refuse})
		return
	}
	_ = writeJSON(b, welcome{OK: true})
	var req DictateRequest
	_ = readJSONLimit(br, &req, 1<<16)
	wav := make([]byte, req.Size)
	_, _ = io.ReadFull(br, wav)
	got <- wav
	_ = writeJSON(b, ans)
}

func dictateRoom(a net.Conn) *Room {
	return &Room{Name: "room-c", up: true, Dial: pipeDialer{a}, T: DefaultTimings()}
}

// A room's clip reaches the hub whole and the text comes back.
func TestRoomDictateCarriesTheClipUp(t *testing.T) {
	a, b := net.Pipe()
	got := make(chan []byte, 1)
	go fakeDictateHub(b, "", DictateAnswer{OK: true, Text: "hello"}, got)
	text, err := dictateRoom(a).Dictate(context.Background(), []byte("RIFFxxxxWAVE-audio"), []string{"alpha"})
	if err != nil || text != "hello" || string(<-got) != "RIFFxxxxWAVE-audio" {
		t.Fatalf("text %q, err %v", text, err)
	}
}

// The room tells apart a hub with speech to text off, a hub that predates the kind, and a hub it cannot reach.
func TestRoomDictateSaysWhyNot(t *testing.T) {
	a, b := net.Pipe()
	go fakeDictateHub(b, "", DictateAnswer{Off: true}, make(chan []byte, 1))
	if _, err := dictateRoom(a).Dictate(context.Background(), []byte("x"), nil); !errors.Is(err, ErrDictateOff) {
		t.Fatalf("off: %v", err)
	}
	a, b = net.Pipe()
	go fakeDictateHub(b, "a connection is control, data, enrol, upgrade, announce, relay, git, roomspec or stream", DictateAnswer{}, nil)
	if _, err := dictateRoom(a).Dictate(context.Background(), []byte("x"), nil); !errors.Is(err, ErrDictateOld) {
		t.Fatalf("old: %v", err)
	}
	r := dictateRoom(nil)
	r.up = false
	if _, err := r.Dictate(context.Background(), []byte("x"), nil); !errors.Is(err, ErrDictateDown) {
		t.Fatalf("down: %v", err)
	}
}

// A hub with no speech to text refuses the kind in its welcome, which the room reads as off.
func TestHubWithoutDictateRefuses(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	go (&Hub{}).serveDictate(context.Background(), "room-c", b, bufio.NewReader(b))
	var w welcome
	_ = readJSON(bufio.NewReader(a), &w)
	if w.OK || w.Error != ErrDictateOff.Error() {
		t.Fatalf("welcome %+v", w)
	}
}

// The hub's accept path hands a dictate hello to serveDictate, so a room is answered and never left to time out.
func TestHubTakeAnswersADictateHello(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	go (&Hub{}).take(context.Background(), b, "test")
	if err := writeJSON(a, hello{V: Version, Kind: dictateKind, Room: "room-c"}); err != nil {
		t.Fatal(err)
	}
	var w welcome
	if err := readJSON(bufio.NewReader(a), &w); err != nil || w.OK || w.Error != ErrDictateOff.Error() {
		t.Fatalf("welcome %+v, err %v", w, err)
	}
}

// A room asks before it records: the welcome says yes, or says the hub has speech to text off.
func TestRoomDictateReady(t *testing.T) {
	a, b := net.Pipe()
	go fakeDictateHub(b, "", DictateAnswer{}, make(chan []byte, 1))
	if err := dictateRoom(a).DictateReady(context.Background()); err != nil {
		t.Fatalf("ready: %v", err)
	}
	a, b = net.Pipe()
	go fakeDictateHub(b, ErrDictateOff.Error(), DictateAnswer{}, nil)
	if err := dictateRoom(a).DictateReady(context.Background()); !errors.Is(err, ErrDictateOff) {
		t.Fatalf("off: %v", err)
	}
}
