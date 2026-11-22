//go:build integration

package link

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/hubstore"
	"github.com/dovholuknf/atrium/internal/link/transport"
)

type specHub struct {
	addr  string
	st    *hubstore.Store
	front *httptest.Server
}

func newSpecHub(t *testing.T, rooms ...string) *specHub {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	st, err := hubstore.Open(filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rooms {
		if _, err := st.Add(r, hubstore.TransportDirect); err != nil {
			t.Fatal(err)
		}
	}
	hub := NewHub(Timings{Beat: 200 * time.Millisecond, Silence: 2 * time.Second, Warm: 2})
	ctx, stop := context.WithCancel(context.Background())
	go func() { _ = hub.Serve(ctx, ln) }()
	p := NewProxy(hub, nil, "", nil)
	p.SetRoomSpecs(st)
	front := httptest.NewServer(p)
	t.Cleanup(func() { front.Close(); stop(); ln.Close(); st.Close() })
	return &specHub{addr: ln.Addr().String(), st: st, front: front}
}

const specRoomD = "version: 1\nname: room-d\nos: windows\n"

const specRoomE = "version: 1\nname: room-e\nos: linux\n"

func lockFor(os string) []byte {
	return []byte(`{"version":1,"os":"` + os + `","steps":[]}`)
}

// A ROOM READS ITS OWN SPEC AND POSTS ITS OWN LOCK, and the hash comes with the spec.
func TestRoomFetchesItsSpecAndPostsItsLock(t *testing.T) {
	h := newSpecHub(t, "room-d", "room-e")
	ctx := context.Background()
	if _, _, err := FetchRoomSpec(ctx, plain{h.addr}, "room-d"); !errors.Is(err, ErrNoRoomSpec) {
		t.Fatalf("before a spec: %v", err)
	}
	sp, err := h.st.SetRoomSpec("room-d", []byte(specRoomD), "t")
	if err != nil {
		t.Fatal(err)
	}
	raw, sum, err := FetchRoomSpec(ctx, plain{h.addr}, "room-d")
	if err != nil || string(raw) != specRoomD || sum != sp.SHA256 {
		t.Fatalf("fetch = %q %q %v", raw, sum, err)
	}
	if err := PostRoomLock(ctx, plain{h.addr}, "room-d", lockFor("windows")); err != nil {
		t.Fatal(err)
	}
	lk, err := h.st.RoomLockOf("room-d")
	if err != nil || !strings.Contains(string(lk.Lock), `"windows"`) {
		t.Fatalf("lock = %+v %v", lk, err)
	}
	if got, _ := h.st.RoomSpecOf("room-d"); got.YAML != specRoomD {
		t.Fatal("posting a lock changed the spec")
	}
	// A lock with a credential is refused by the hub, and the last lock stays.
	if err := PostRoomLock(ctx, plain{h.addr}, "room-d", []byte(`{"version":1,"api_token":"x"}`)); err == nil {
		t.Fatal("a credential was accepted")
	}
	if lk2, _ := h.st.RoomLockOf("room-d"); string(lk2.Lock) != string(lk.Lock) {
		t.Fatal("a refused lock replaced the last")
	}
}

// ROOM X CANNOT READ OR WRITE ROOM Y'S SPEC. The path names no room, and what a room puts in a header or the query to
// say it is another is never read.
func TestRoomCannotReachAnotherRoomsSpecOrLock(t *testing.T) {
	h := newSpecHub(t, "room-d", "room-e")
	ctx := context.Background()
	if _, err := h.st.SetRoomSpec("room-d", []byte(specRoomD), "t"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.st.SetRoomSpec("room-e", []byte(specRoomE), "t"); err != nil {
		t.Fatal(err)
	}
	// Each room gets its own.
	if raw, _, err := FetchRoomSpec(ctx, plain{h.addr}, "room-e"); err != nil || string(raw) != specRoomE {
		t.Fatalf("room-e fetched %q %v", raw, err)
	}
	// room-e posts a lock: only room-e has one.
	if err := PostRoomLock(ctx, plain{h.addr}, "room-e", lockFor("linux")); err != nil {
		t.Fatal(err)
	}
	if _, err := h.st.RoomLockOf("room-d"); !errors.Is(err, hubstore.ErrNoRoomLock) {
		t.Fatalf("room-e's lock landed on room-d: %v", err)
	}

	// room-e now tries to be room-d, by header, by query and by path, on the raw connection.
	conn, err := plain{h.addr}.Dial(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	br := bufio.NewReader(conn)
	if _, err := sayHello(conn, br, hello{Kind: roomSpecKind, Room: "room-e"}); err != nil {
		t.Fatal(err)
	}
	io.WriteString(conn, "GET /_room/spec?room=room-d HTTP/1.1\r\nHost: hub\r\n"+RoomSpecRoomHeader+": room-d\r\n\r\n")
	res, err := http.ReadResponse(br, nil)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	if string(body) != specRoomE {
		t.Fatalf("a spoofed header read %q", body)
	}
	io.WriteString(conn, "POST /_room/lock?room=room-d HTTP/1.1\r\nHost: hub\r\n"+RoomSpecRoomHeader+
		": room-d\r\nContent-Length: 38\r\n\r\n"+`{"version":1,"os":"forged","steps":[]}`)
	res2, err := http.ReadResponse(br, nil)
	if err != nil {
		t.Fatal(err)
	}
	res2.Body.Close()
	if _, err := h.st.RoomLockOf("room-d"); !errors.Is(err, hubstore.ErrNoRoomLock) {
		t.Fatalf("a spoofed header wrote room-d's lock: %v", err)
	}
	if lk, _ := h.st.RoomLockOf("room-e"); !strings.Contains(string(lk.Lock), "forged") {
		t.Fatalf("the forged lock should have landed on room-e itself: %s", lk.Lock)
	}
	// A path with a room in it is nothing.
	io.WriteString(conn, "GET /_room/room-d/spec HTTP/1.1\r\nHost: hub\r\n\r\n")
	res3, err := http.ReadResponse(br, nil)
	if err != nil || res3.StatusCode != http.StatusNotFound {
		t.Fatalf("a path naming a room = %v %v", res3, err)
	}
}

// THE BOARD READS A ROOM'S SPEC AND LOCK, and cannot write them.
func TestBoardReadsSpecAndLockReadOnly(t *testing.T) {
	h := newSpecHub(t, "room-d")
	get := func(method, path string) (int, string) {
		req, _ := http.NewRequest(method, h.front.URL+path, strings.NewReader("x"))
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(b)
	}
	if code, _ := get("GET", "/_hub/rooms/room-d/spec"); code != 404 {
		t.Fatalf("no spec yet = %d", code)
	}
	if _, err := h.st.SetRoomSpec("room-d", []byte(specRoomD), "t"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.st.PutRoomLock("room-d", lockFor("windows")); err != nil {
		t.Fatal(err)
	}
	code, body := get("GET", "/_hub/rooms/room-d/spec")
	var sp hubstore.RoomSpec
	if code != 200 || json.Unmarshal([]byte(body), &sp) != nil || sp.YAML != specRoomD || sp.SHA256 == "" {
		t.Fatalf("spec = %d %s", code, body)
	}
	code, body = get("GET", "/_hub/rooms/room-d/lock")
	var lk hubstore.RoomLock
	if code != 200 || json.Unmarshal([]byte(body), &lk) != nil || !strings.Contains(string(lk.Lock), "windows") {
		t.Fatalf("lock = %d %s", code, body)
	}
	for _, m := range []string{"POST", "PUT", "DELETE"} {
		if code, _ := get(m, "/_hub/rooms/room-d/spec"); code != 405 {
			t.Errorf("%s spec = %d", m, code)
		}
	}
	// AN UNKNOWN ROOM AND A ROOM WITH NO SPEC SAY THE SAME WORDS.
	h.st.Add("bare", hubstore.TransportDirect)
	c1, b1 := get("GET", "/_hub/rooms/nobody/spec")
	c2, b2 := get("GET", "/_hub/rooms/bare/spec")
	if c1 != 404 || c2 != 404 || b1 != b2 {
		t.Errorf("unknown %d %q, no spec %d %q", c1, b1, c2, b2)
	}
	c1, b1 = get("GET", "/_hub/rooms/nobody/lock")
	c2, b2 = get("GET", "/_hub/rooms/bare/lock")
	if c1 != 404 || c2 != 404 || b1 != b2 {
		t.Errorf("lock: unknown %d %q, none %d %q", c1, b1, c2, b2)
	}
	if got, _ := h.st.RoomSpecOf("room-d"); got.YAML != specRoomD {
		t.Fatal("the board changed the spec")
	}
}

// A CONNECTION WITH NO CERTIFICATE CANNOT READ OR POST ANOTHER ROOM'S SPEC OR LOCK by naming it in its hello. The hub
// here asks for authentication as an overlay hub does, and the old path (a legacyConn) proves nothing but the overlay's
// word, so the kind is refused for it whatever name it claims, enrolled or not.
func TestLegacyConnectionCannotNameARoomForItsSpec(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	st, err := hubstore.Open(filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range []string{"room-d", "never"} {
		if _, err := st.Add(r, hubstore.TransportDirect); err != nil {
			t.Fatal(err)
		}
		if _, err := st.SetRoomSpec(r, []byte("version: 1\nname: "+r+"\n"), "t"); err != nil {
			t.Fatal(err)
		}
	}
	hub := NewHub(Timings{Beat: 200 * time.Millisecond, Silence: 2 * time.Second, Warm: 2})
	hub.Authenticated = OverlayAuthenticated
	hub.Enrolled = func(name string) bool { return name == "room-d" }
	p := NewProxy(hub, nil, "", nil)
	p.SetRoomSpecs(st)
	ctx, stop := context.WithCancel(context.Background())
	go func() { _ = hub.Serve(ctx, legacyListener{ln}) }()
	t.Cleanup(func() { stop(); ln.Close(); st.Close() })

	for _, claim := range []string{"room-d", "never"} {
		if _, _, err := FetchRoomSpec(ctx, plain{ln.Addr().String()}, claim); err == nil {
			t.Errorf("a legacy connection read the spec of %s", claim)
		}
		if err := PostRoomLock(ctx, plain{ln.Addr().String()}, claim, lockFor("forged")); err == nil {
			t.Errorf("a legacy connection posted the lock of %s", claim)
		}
		if _, err := st.RoomLockOf(claim); !errors.Is(err, hubstore.ErrNoRoomLock) {
			t.Errorf("a lock landed on %s: %v", claim, err)
		}
	}
}

// legacyListener hands the hub connections that look like the old overlay path.
type legacyListener struct{ net.Listener }

func (l legacyListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return transport.NewLegacyConn(c, "ziti"), nil
}
