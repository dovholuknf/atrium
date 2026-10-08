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

const specSG3 = "version: 1\nname: sg3\nos: windows\n"
const specSG4 = "version: 1\nname: sg4\nos: linux\n"

func lockFor(os string) []byte {
	return []byte(`{"version":1,"os":"` + os + `","steps":[]}`)
}

// A ROOM READS ITS OWN SPEC AND POSTS ITS OWN LOCK, and the hash comes with the spec.
func TestRoomFetchesItsSpecAndPostsItsLock(t *testing.T) {
	h := newSpecHub(t, "sg3", "sg4")
	ctx := context.Background()
	if _, _, err := FetchRoomSpec(ctx, plain{h.addr}, "sg3"); !errors.Is(err, ErrNoRoomSpec) {
		t.Fatalf("before a spec: %v", err)
	}
	sp, err := h.st.SetRoomSpec("sg3", []byte(specSG3), "t")
	if err != nil {
		t.Fatal(err)
	}
	raw, sum, err := FetchRoomSpec(ctx, plain{h.addr}, "sg3")
	if err != nil || string(raw) != specSG3 || sum != sp.SHA256 {
		t.Fatalf("fetch = %q %q %v", raw, sum, err)
	}
	if err := PostRoomLock(ctx, plain{h.addr}, "sg3", lockFor("windows")); err != nil {
		t.Fatal(err)
	}
	lk, err := h.st.RoomLockOf("sg3")
	if err != nil || !strings.Contains(string(lk.Lock), `"windows"`) {
		t.Fatalf("lock = %+v %v", lk, err)
	}
	if got, _ := h.st.RoomSpecOf("sg3"); got.YAML != specSG3 {
		t.Fatal("posting a lock changed the spec")
	}
	// A lock with a credential is refused by the hub, and the last lock stays.
	if err := PostRoomLock(ctx, plain{h.addr}, "sg3", []byte(`{"version":1,"api_token":"x"}`)); err == nil {
		t.Fatal("a credential was accepted")
	}
	if lk2, _ := h.st.RoomLockOf("sg3"); string(lk2.Lock) != string(lk.Lock) {
		t.Fatal("a refused lock replaced the last")
	}
}

// ROOM X CANNOT READ OR WRITE ROOM Y'S SPEC. The path names no room, and what a room puts in a header or the query to
// say it is another is never read.
func TestRoomCannotReachAnotherRoomsSpecOrLock(t *testing.T) {
	h := newSpecHub(t, "sg3", "sg4")
	ctx := context.Background()
	if _, err := h.st.SetRoomSpec("sg3", []byte(specSG3), "t"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.st.SetRoomSpec("sg4", []byte(specSG4), "t"); err != nil {
		t.Fatal(err)
	}
	// Each room gets its own.
	if raw, _, err := FetchRoomSpec(ctx, plain{h.addr}, "sg4"); err != nil || string(raw) != specSG4 {
		t.Fatalf("sg4 fetched %q %v", raw, err)
	}
	// sg4 posts a lock: only sg4 has one.
	if err := PostRoomLock(ctx, plain{h.addr}, "sg4", lockFor("linux")); err != nil {
		t.Fatal(err)
	}
	if _, err := h.st.RoomLockOf("sg3"); !errors.Is(err, hubstore.ErrNoRoomLock) {
		t.Fatalf("sg4's lock landed on sg3: %v", err)
	}

	// sg4 now tries to be sg3, by header, by query and by path, on the raw connection.
	conn, err := plain{h.addr}.Dial(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	br := bufio.NewReader(conn)
	if _, err := sayHello(conn, br, hello{Kind: roomSpecKind, Room: "sg4"}); err != nil {
		t.Fatal(err)
	}
	io.WriteString(conn, "GET /_room/spec?room=sg3 HTTP/1.1\r\nHost: hub\r\n"+RoomSpecRoomHeader+": sg3\r\n\r\n")
	res, err := http.ReadResponse(br, nil)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	if string(body) != specSG4 {
		t.Fatalf("a spoofed header read %q", body)
	}
	io.WriteString(conn, "POST /_room/lock?room=sg3 HTTP/1.1\r\nHost: hub\r\n"+RoomSpecRoomHeader+
		": sg3\r\nContent-Length: 38\r\n\r\n"+`{"version":1,"os":"forged","steps":[]}`)
	res2, err := http.ReadResponse(br, nil)
	if err != nil {
		t.Fatal(err)
	}
	res2.Body.Close()
	if _, err := h.st.RoomLockOf("sg3"); !errors.Is(err, hubstore.ErrNoRoomLock) {
		t.Fatalf("a spoofed header wrote sg3's lock: %v", err)
	}
	if lk, _ := h.st.RoomLockOf("sg4"); !strings.Contains(string(lk.Lock), "forged") {
		t.Fatalf("the forged lock should have landed on sg4 itself: %s", lk.Lock)
	}
	// A path with a room in it is nothing.
	io.WriteString(conn, "GET /_room/sg3/spec HTTP/1.1\r\nHost: hub\r\n\r\n")
	res3, err := http.ReadResponse(br, nil)
	if err != nil || res3.StatusCode != http.StatusNotFound {
		t.Fatalf("a path naming a room = %v %v", res3, err)
	}
}

// THE BOARD READS A ROOM'S SPEC AND LOCK, and cannot write them.
func TestBoardReadsSpecAndLockReadOnly(t *testing.T) {
	h := newSpecHub(t, "sg3")
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
	if code, _ := get("GET", "/_hub/rooms/sg3/spec"); code != 404 {
		t.Fatalf("no spec yet = %d", code)
	}
	if _, err := h.st.SetRoomSpec("sg3", []byte(specSG3), "t"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.st.PutRoomLock("sg3", lockFor("windows")); err != nil {
		t.Fatal(err)
	}
	code, body := get("GET", "/_hub/rooms/sg3/spec")
	var sp hubstore.RoomSpec
	if code != 200 || json.Unmarshal([]byte(body), &sp) != nil || sp.YAML != specSG3 || sp.SHA256 == "" {
		t.Fatalf("spec = %d %s", code, body)
	}
	code, body = get("GET", "/_hub/rooms/sg3/lock")
	var lk hubstore.RoomLock
	if code != 200 || json.Unmarshal([]byte(body), &lk) != nil || !strings.Contains(string(lk.Lock), "windows") {
		t.Fatalf("lock = %d %s", code, body)
	}
	for _, m := range []string{"POST", "PUT", "DELETE"} {
		if code, _ := get(m, "/_hub/rooms/sg3/spec"); code != 405 {
			t.Errorf("%s spec = %d", m, code)
		}
	}
	if code, _ := get("GET", "/_hub/rooms/nobody/spec"); code != 404 {
		t.Errorf("unknown room = %d", code)
	}
	if got, _ := h.st.RoomSpecOf("sg3"); got.YAML != specSG3 {
		t.Fatal("the board changed the spec")
	}
}
