package link

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// An unscoped launch finds its own room. See launchroute.go.

// fakeRoom is a room on a machine that has some directories. It answers the
// directory check for those, and on a launch says who served it.
type fakeRoom struct {
	name, host string
	dirs       []string
	// slow is how long it sits on the directory check before answering.
	slow time.Duration
	// noRoute answers the check the way a room built before it existed would.
	noRoute bool
	probes  atomic.Int32
}

func (f *fakeRoom) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/launch/cwd" {
			f.probes.Add(1)
			if f.noRoute {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			select {
			case <-time.After(f.slow):
			case <-r.Context().Done():
				return
			}
			dir := r.URL.Query().Get("path")
			has := false
			for _, d := range f.dirs {
				has = has || d == dir
			}
			fmt.Fprintf(w, `{"exists":%v,"dir":%v}`, has, has)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var body struct {
			TaskID string `json:"task_id"`
		}
		_ = json.Unmarshal(raw, &body)
		fmt.Fprintf(w, `{"served_by":%q,"path":%q,"saw_task_id":%q}`, f.name, r.URL.Path, body.TaskID)
	})
}

// rooms attaches the fake rooms to one hub, and says the hub runs on `hubHost`.
func rooms(t *testing.T, hubHost string, fakes ...*fakeRoom) (*httptest.Server, func()) {
	t.Helper()
	oldHost, oldWait := hubHostname, launchProbeFor
	hubHostname = func() (string, error) { return hubHost, nil }
	launchProbeFor = 600 * time.Millisecond

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	hub := NewHub(Timings{Beat: 200 * time.Millisecond, Silence: 2 * time.Second, Warm: 2})
	ctx, stop := context.WithCancel(context.Background())
	go func() { _ = hub.Serve(ctx, ln) }()
	for _, f := range fakes {
		room := &Room{
			Name: f.name, Host: f.host, Dial: plain{addr: ln.Addr().String()}, Handler: f.handler(),
			T: Timings{Beat: 200 * time.Millisecond, Warm: 2, Backoff: 50 * time.Millisecond},
		}
		go func() { _ = room.Run(ctx) }()
	}
	waitFor(t, 5*time.Second, func() bool {
		for _, f := range fakes {
			if !hub.Has(f.name) {
				return false
			}
		}
		return true
	})
	front := httptest.NewServer(NewProxy(hub, nil, "", nil))
	return front, func() {
		front.Close()
		stop()
		ln.Close()
		hubHostname, launchProbeFor = oldHost, oldWait
	}
}

type launched struct {
	Code  int
	By    string `json:"served_by"`
	Error string `json:"error"`
	// Rooms is nil when the answer carried none.
	Rooms []string `json:"rooms"`
}

// launch posts to the front the way gwt does, loopback and no room.
func launch(t *testing.T, front *httptest.Server, body string, headers ...string) launched {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, front.URL+"/v1/launch", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	got := launched{Code: res.StatusCode}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("launch answered %d %s", res.StatusCode, raw)
	}
	return got
}

const dotagents = `/work/dotagents`

func inDir(dir string) string { return fmt.Sprintf(`{"harness":"claude","cwd":%q}`, dir) }

// THE BUG. gwt on sg4 posts a launch with no room. Four rooms, and the directory
// is on one machine only: it goes there, with no question asked.
func TestAnUnscopedLaunchGoesToTheOneRoomWithTheDirectory(t *testing.T) {
	a := &fakeRoom{name: "claude-sg4", host: "sg4"}
	b := &fakeRoom{name: "sg4-control", host: "sg4", dirs: []string{dotagents}}
	c := &fakeRoom{name: "sg3", host: "sg3", dirs: []string{"/elsewhere"}}
	d := &fakeRoom{name: "m1mini", host: "m1mini"}
	front, done := rooms(t, "sg4", a, b, c, d)
	defer done()

	got := launch(t, front, inDir(dotagents))
	if got.Code != http.StatusOK || got.By != "sg4-control" {
		t.Fatalf("the launch landed on %q (%d %s), want sg4-control", got.By, got.Code, got.Error)
	}
	// Only the caller's machine was asked.
	if c.probes.Load() != 0 || d.probes.Load() != 0 {
		t.Fatalf("rooms on other machines were asked: sg3 %d, m1mini %d", c.probes.Load(), d.probes.Load())
	}
}

// A directory that exists only on sg3 is not launched on sg3 from sg4: the
// caller is on sg4, and sg4 has no such directory. No picker to fail in.
func TestAnUnscopedLaunchWithNoRoomHavingTheDirectoryIsNotAPicker(t *testing.T) {
	a := &fakeRoom{name: "claude-sg4", host: "sg4"}
	b := &fakeRoom{name: "sg4-control", host: "sg4"}
	c := &fakeRoom{name: "sg3", host: "sg3", dirs: []string{dotagents}}
	front, done := rooms(t, "sg4", a, b, c)
	defer done()

	got := launch(t, front, inDir(dotagents))
	if got.Code != http.StatusUnprocessableEntity {
		t.Fatalf("answered %d, want 422", got.Code)
	}
	if got.Rooms != nil {
		t.Fatalf("a refusal that carries rooms %v shows a picker whose pick cannot work", got.Rooms)
	}
	if !strings.Contains(got.Error, "no room has the directory "+dotagents) {
		t.Fatalf("the answer does not say so: %s", got.Error)
	}
}

// sg4 runs two rooms on one machine, so two matches is the ordinary case there.
// The caller is asked, and is offered the two that work, not the third that does
// not have the directory and not the room on another machine that does.
func TestAnUnscopedLaunchWithTwoMatchesListsOnlyThoseTwo(t *testing.T) {
	a := &fakeRoom{name: "claude-sg4", host: "sg4", dirs: []string{dotagents}}
	b := &fakeRoom{name: "sg4-control", host: "sg4", dirs: []string{dotagents}}
	c := &fakeRoom{name: "sg4-lacks", host: "sg4"}
	d := &fakeRoom{name: "sg3", host: "sg3", dirs: []string{dotagents}}
	front, done := rooms(t, "sg4", a, b, c, d)
	defer done()

	got := launch(t, front, inDir(dotagents))
	if got.Code != http.StatusConflict {
		t.Fatalf("answered %d, want 409", got.Code)
	}
	if strings.Join(got.Rooms, ",") != "claude-sg4,sg4-control" {
		t.Fatalf("rooms %v, want exactly claude-sg4 and sg4-control", got.Rooms)
	}
}

// There is no default room. Naming one on the retry is how it is chosen.
func TestThePickAfterTwoMatchesIsHonouredAsNamed(t *testing.T) {
	a := &fakeRoom{name: "claude-sg4", host: "sg4", dirs: []string{dotagents}}
	b := &fakeRoom{name: "sg4-control", host: "sg4", dirs: []string{dotagents}}
	front, done := rooms(t, "sg4", a, b)
	defer done()

	got := launch(t, front, inDir(dotagents), RoomHeader, "claude-sg4")
	if got.Code != http.StatusOK || got.By != "claude-sg4" {
		t.Fatalf("the pick landed on %q (%d %s)", got.By, got.Code, got.Error)
	}
}

// A silent room is not a match, and does not hold the launch up past its wait.
func TestARoomThatDoesNotAnswerIsNotAMatch(t *testing.T) {
	a := &fakeRoom{name: "claude-sg4", host: "sg4", dirs: []string{dotagents}, slow: 5 * time.Second}
	b := &fakeRoom{name: "sg4-control", host: "sg4", dirs: []string{dotagents}}
	front, done := rooms(t, "sg4", a, b)
	defer done()

	began := time.Now()
	got := launch(t, front, inDir(dotagents))
	if got.Code != http.StatusOK || got.By != "sg4-control" {
		t.Fatalf("the launch landed on %q (%d %s), want the room that answered", got.By, got.Code, got.Error)
	}
	if took := time.Since(began); took > 3*time.Second {
		t.Fatalf("the launch waited %s on a silent room", took)
	}
}

// A ROOM THAT DID NOT ANSWER MAY HAVE THE DIRECTORY. A room on a build from before
// the check, a slow one, one that is restarting: the picker let the human choose
// it before, so "none matched" is a 409 over EVERY attached room (the other
// machine's too), never a 422 that strands the launch. This is also why the order
// hub and rooms are deployed in does not matter.
func TestARoomThatDidNotAnswerKeepsThePickerOverEveryRoom(t *testing.T) {
	a := &fakeRoom{name: "claude-sg4", host: "sg4", noRoute: true}
	b := &fakeRoom{name: "sg4-control", host: "sg4"}
	c := &fakeRoom{name: "sg3", host: "sg3"}
	front, done := rooms(t, "sg4", a, b, c)
	defer done()

	got := launch(t, front, inDir(dotagents))
	if got.Code != http.StatusConflict || strings.Join(got.Rooms, ",") != "claude-sg4,sg3,sg4-control" {
		t.Fatalf("answered %d rooms %v, want the old 409 over every room", got.Code, got.Rooms)
	}
}

// Slow is the same as old: not an answer.
func TestASlowRoomWithNoMatchKeepsThePicker(t *testing.T) {
	a := &fakeRoom{name: "claude-sg4", host: "sg4", slow: 5 * time.Second}
	b := &fakeRoom{name: "sg4-control", host: "sg4"}
	front, done := rooms(t, "sg4", a, b)
	defer done()

	got := launch(t, front, inDir(dotagents))
	if got.Code != http.StatusConflict || len(got.Rooms) != 2 {
		t.Fatalf("answered %d rooms %v, want the old 409", got.Code, got.Rooms)
	}
}

// A directory that is not absolute means nothing off the machine it was typed
// on, is "not a directory" on every room, and so was a 422 where the picker used
// to be. It is not probed and is asked about as before.
func TestARelativeDirectoryKeepsThePickerAndIsNotProbed(t *testing.T) {
	a := &fakeRoom{name: "claude-sg4", host: "sg4", dirs: []string{".", "~/x", "src"}}
	b := &fakeRoom{name: "sg3", host: "sg3"}
	front, done := rooms(t, "sg4", a, b)
	defer done()

	for _, dir := range []string{".", "~/x", "src", `\work`, "C:", `C:x`} {
		got := launch(t, front, inDir(dir))
		if got.Code != http.StatusConflict || strings.Join(got.Rooms, ",") != "claude-sg4,sg3" {
			t.Fatalf("%q answered %d rooms %v, want the old 409 over every room", dir, got.Code, got.Rooms)
		}
	}
	if a.probes.Load()+b.probes.Load() != 0 {
		t.Fatal("a directory that is not absolute was probed")
	}
}

// The hub may be Windows or not and the caller's path either style, so a drive
// path is absolute on a hub that is not, and is asked about.
func TestAWindowsDriveDirectoryIsAbsoluteWhateverTheHub(t *testing.T) {
	for _, dir := range []string{`D:\git\dotagents`, `d:/git/dotagents`} {
		a := &fakeRoom{name: "claude-sg4", host: "sg4", dirs: []string{dir}}
		b := &fakeRoom{name: "sg3", host: "sg3"}
		front, done := rooms(t, "sg4", a, b)
		got := launch(t, front, inDir(dir))
		done()
		if got.Code != http.StatusOK || got.By != "claude-sg4" {
			t.Fatalf("%q landed on %q (%d %s), want claude-sg4", dir, got.By, got.Code, got.Error)
		}
	}
}

// A NETWORK PATH IS NEVER PROBED: stat on a Windows room would open SMB to the
// host the caller wrote, with the room user's credentials. Every spelling, and the
// answer is the old question.
func TestANetworkPathIsNeverProbed(t *testing.T) {
	dirs := []string{`\\evil\share\x`, `//evil/share/x`, `\\?\UNC\evil\share`, `\\.\pipe\x`, `/\evil/share`, `\\?\C:\x`}
	a := &fakeRoom{name: "claude-sg4", host: "sg4", dirs: dirs}
	b := &fakeRoom{name: "sg3", host: "sg3", dirs: dirs}
	front, done := rooms(t, "sg4", a, b)
	defer done()

	for _, dir := range dirs {
		got := launch(t, front, inDir(dir))
		if got.Code != http.StatusConflict || strings.Join(got.Rooms, ",") != "claude-sg4,sg3" {
			t.Fatalf("%q answered %d rooms %v, want the old 409 over every room", dir, got.Code, got.Rooms)
		}
	}
	if a.probes.Load()+b.probes.Load() != 0 {
		t.Fatal("a network path was probed")
	}
}

// A room marked for deletion starts nothing, so it is not asked and is not a
// match: the other room on the machine takes the launch with no picker.
func TestARoomMarkedForDeletionIsNotACandidate(t *testing.T) {
	a := &fakeRoom{name: "claude-sg4", host: "sg4", dirs: []string{dotagents}}
	b := &fakeRoom{name: "sg4-control", host: "sg4", dirs: []string{dotagents}}
	front, done := rooms(t, "sg4", a, b)
	defer done()
	seen := time.Now()
	front.Config.Handler.(*Proxy).SetInventory(&remembering{
		rooms: []Known{
			{Name: "claude-sg4", Attached: true, State: "marked-for-deletion", FirstSeen: &seen, LastSeen: &seen},
			{Name: "sg4-control", Attached: true, FirstSeen: &seen, LastSeen: &seen},
		},
		cards: map[string][]CardState{},
	})

	got := launch(t, front, inDir(dotagents))
	if got.Code != http.StatusOK || got.By != "sg4-control" {
		t.Fatalf("the launch landed on %q (%d %s), want sg4-control", got.By, got.Code, got.Error)
	}
	if a.probes.Load() != 0 {
		t.Fatal("a room on its way out was asked")
	}
}

// A launch that names a room is placed exactly as before: no directory check,
// whether or not the room has the directory.
func TestALaunchThatNamesARoomIsNotChecked(t *testing.T) {
	a := &fakeRoom{name: "claude-sg4", host: "sg4"}
	b := &fakeRoom{name: "sg3", host: "sg3", dirs: []string{dotagents}}
	front, done := rooms(t, "sg4", a, b)
	defer done()

	got := launch(t, front, inDir(dotagents), RoomHeader, "claude-sg4")
	if got.Code != http.StatusOK || got.By != "claude-sg4" {
		t.Fatalf("a named room was overridden: %q (%d %s)", got.By, got.Code, got.Error)
	}
	if a.probes.Load()+b.probes.Load() != 0 {
		t.Fatal("a launch that named its room was checked anyway")
	}
}

// A launch that names a card is placed by the card.
func TestALaunchThatNamesACardIsNotChecked(t *testing.T) {
	a := &fakeRoom{name: "claude-sg4", host: "sg4"}
	b := &fakeRoom{name: "sg3", host: "sg3"}
	front, done := rooms(t, "sg4", a, b)
	defer done()

	got := launch(t, front, `{"harness":"claude","cwd":"/work/x","task_id":"sg3~abc"}`)
	if got.Code != http.StatusOK || got.By != "sg3" {
		t.Fatalf("a tagged card was overridden: %q (%d %s)", got.By, got.Code, got.Error)
	}
	if a.probes.Load()+b.probes.Load() != 0 {
		t.Fatal("a launch that named a card was checked anyway")
	}
}

// A launch with no directory has nothing to check, so it is asked for a room as
// it always was.
func TestAnUnscopedLaunchWithNoDirectoryStillAsksForARoom(t *testing.T) {
	a := &fakeRoom{name: "claude-sg4", host: "sg4"}
	b := &fakeRoom{name: "sg3", host: "sg3"}
	front, done := rooms(t, "sg4", a, b)
	defer done()

	got := launch(t, front, `{"harness":"claude"}`)
	if got.Code != http.StatusConflict || strings.Join(got.Rooms, ",") != "claude-sg4,sg3" {
		t.Fatalf("answered %d rooms %v, want the old 409 over every room", got.Code, got.Rooms)
	}
}

// Other unscoped writes are still a question.
func TestAnotherUnscopedWriteStillAsksForARoom(t *testing.T) {
	a := &fakeRoom{name: "claude-sg4", host: "sg4", dirs: []string{dotagents}}
	b := &fakeRoom{name: "sg3", host: "sg3"}
	front, done := rooms(t, "sg4", a, b)
	defer done()

	res, err := http.Post(front.URL+"/v1/settings", "application/json", strings.NewReader(`{"x":"y"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusConflict {
		t.Fatalf("an unscoped settings write answered %d, want 409", res.StatusCode)
	}
}

// A caller that did not come over loopback cannot be placed on a machine, so
// every room is asked, and the directory still decides.
func TestARemoteCallerIsCheckedAgainstEveryRoom(t *testing.T) {
	a := &fakeRoom{name: "claude-sg4", host: "sg4"}
	b := &fakeRoom{name: "sg3", host: "sg3", dirs: []string{dotagents}}
	front, done := rooms(t, "sg4", a, b)
	defer done()

	// A forwarding header is what makes a request not a local operator's.
	got := launch(t, front, inDir(dotagents), "X-Forwarded-For", "203.0.113.9")
	if got.Code != http.StatusOK || got.By != "sg3" {
		t.Fatalf("the launch landed on %q (%d %s), want sg3", got.By, got.Code, got.Error)
	}
	if a.probes.Load() == 0 {
		t.Fatal("a remote caller was filtered to the hub's machine")
	}
}

// Rooms that report no host that matches the hub's leave nothing to filter to,
// and a filter that matches nothing is no filter.
func TestAHostFilterThatMatchesNoRoomAsksEveryRoom(t *testing.T) {
	a := &fakeRoom{name: "claude-sg4", host: "somewhere"}
	b := &fakeRoom{name: "sg3", host: "else", dirs: []string{dotagents}}
	front, done := rooms(t, "sg4", a, b)
	defer done()

	got := launch(t, front, inDir(dotagents))
	if got.Code != http.StatusOK || got.By != "sg3" {
		t.Fatalf("the launch landed on %q (%d %s), want sg3", got.By, got.Code, got.Error)
	}
}

// The host is compared the way a person would: sg4 and SG4 are one machine.
func TestTheHubsHostIsComparedIgnoringCase(t *testing.T) {
	a := &fakeRoom{name: "claude-sg4", host: "SG4", dirs: []string{dotagents}}
	b := &fakeRoom{name: "sg3", host: "sg3", dirs: []string{dotagents}}
	front, done := rooms(t, "sg4", a, b)
	defer done()

	got := launch(t, front, inDir(dotagents))
	if got.Code != http.StatusOK || got.By != "claude-sg4" {
		t.Fatalf("the launch landed on %q (%d %s), want claude-sg4", got.By, got.Code, got.Error)
	}
}
