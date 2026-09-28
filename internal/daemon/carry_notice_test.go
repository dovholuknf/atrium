package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/dovholuknf/atrium/internal/store"
)

// THE NOT-REPLAYED NOTICE AND ITS TWO ACTIONS, over the real attach socket.
//
// The board asks for the links with a nonce (`?link=`), and for the whole
// history with `?carry=all`. See `carryLinkNotice`.

// cutSession is a supervised card whose saved history is bigger than one
// attach replays.
func cutSession(t *testing.T, d *Daemon, id string) []byte {
	t.Helper()
	// Raw, so megabytes of replay land inside the reader's quiet window. The
	// screen model is not what is under test.
	if err := d.st.SetSetting(store.SettingReplayMode, "raw"); err != nil {
		t.Fatal(err)
	}
	narrowSession(t, d, id, "● live line from the new process, long enough\r\n")
	old := savedLines(carryReplayMax/80 + 2000)
	r := d.sup.get(id)
	r.mu.Lock()
	r.carried = &carryover{cols: 80, bytes: old}
	r.mu.Unlock()
	return old
}

const firstSaved = "saved line 0000000 "

// attachUntil is attachVia that reads until `want` has arrived, or ten seconds.
// A replay of megabytes under a loaded `go test ./...` can outlast attachVia's
// quiet window, and the test then reads a short replay as a missing line.
func attachUntil(t *testing.T, h http.Handler, path, want string) string {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+path, nil)
	if err != nil {
		t.Fatalf("could not attach: %v", err)
	}
	defer c.CloseNow()
	c.SetReadLimit(64 << 20)
	frame, _ := json.Marshal(attachIn{T: "resize", Cols: 120, Rows: 40})
	if err := c.Write(ctx, websocket.MessageText, frame); err != nil {
		t.Fatalf("could not send a size: %v", err)
	}
	var got strings.Builder
	for !strings.Contains(got.String(), want) {
		_, data, err := c.Read(ctx)
		if err != nil {
			break
		}
		got.Write(data)
	}
	return got.String()
}

func TestTheNoticeCarriesTheBoardsNonceInItsLinks(t *testing.T) {
	d := testDaemon(t)
	old := cutSession(t, d, "cut")
	got := attachPath(t, d, "/v1/tasks/cut/attach?link=n0nce", 120, 40)
	for _, want := range []string{
		"\x1b]8;;atrium:carry/open?n=n0nce\x1b\\",
		"\x1b]8;;atrium:carry/load?n=n0nce&b=" + strconv.Itoa(len(old)) + "\x1b\\",
		"load all " + humanBytes(int64(len(old))) + " in here",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("the notice has no %q", want)
		}
	}
	if strings.Contains(got, firstSaved) {
		t.Fatal("replayed the oldest saved line, so nothing was held back")
	}
}

// No nonce is an older board, and a nonce that is not plain letters and
// digits is not one this board made. Both get the line with no links.
func TestWithoutAUsableNonceTheNoticeHasNoLinks(t *testing.T) {
	d := testDaemon(t)
	cutSession(t, d, "cut")
	for _, q := range []string{"", "?link=", "?link=a%1b%5D8%3B%3Bx", "?link=" + strings.Repeat("a", 65)} {
		got := attachPath(t, d, "/v1/tasks/cut/attach"+q, 120, 40)
		if strings.Contains(got, "atrium:") {
			t.Fatalf("%q: a link without a usable nonce", q)
		}
		if !strings.Contains(got, "not replayed here") {
			t.Fatalf("%q: the notice is missing", q)
		}
	}
}

func TestCarryAllReplaysEverySavedLine(t *testing.T) {
	d := testDaemon(t)
	cutSession(t, d, "cut")
	got := attachUntil(t, d.ap.Handler(), "/v1/tasks/cut/attach?carry=all&link=n0nce", firstSaved)
	if !strings.Contains(got, firstSaved) {
		t.Fatal("carry=all left the oldest saved line out")
	}
	if strings.Contains(got, "not replayed here") {
		t.Fatal("carry=all still says something was held back")
	}
}

// A GUEST GETS NEITHER. The whole history is `/scrollback/older` by another
// route, and a guest is refused that.
func TestAGuestCannotAskForTheWholeHistory(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()
	task := sharedCard(t, d, "lent")
	rec := guestGet(d.guestHandler(task.ID), "/v1/tasks/"+task.ID+"/attach?carry=all")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("a guest asking for carry=all got %d, wanted 403", rec.Code)
	}
}

func TestAGuestGetsTheNoticeWithoutLinks(t *testing.T) {
	d := testDaemon(t)
	cutSession(t, d, "cut")
	got := attachUntil(t, d.guestHandler("cut"), "/v1/tasks/cut/attach?link=n0nce", "not replayed here")
	if !strings.Contains(got, "not replayed here") {
		t.Fatal("the guest's attach has no notice")
	}
	if strings.Contains(got, "atrium:") {
		t.Fatal("a guest got the notice's links")
	}
}
