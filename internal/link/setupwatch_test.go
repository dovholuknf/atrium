package link

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

// What the hub reads of a room's setup, and what it says about it on the `rooms` row.

// setupRoom answers the three reads a room is asked. A zero `missing` and `build` equal to the hub's is a room set up
// like the hub.
func setupRoom(missing, stale int, build string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/hooks", func(w http.ResponseWriter, r *http.Request) {
		hooks := ""
		for i := 0; i < stale; i++ {
			if hooks != "" {
				hooks += ","
			}
			hooks += `{"stale":true}`
		}
		_, _ = w.Write([]byte(`{"missing":` + itoa(missing) + `,"hooks":[` + hooks + `]}`))
	})
	mux.HandleFunc("/v1/health", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"build":"` + build + `","go":"go1.99"}`))
	})
	mux.HandleFunc("/v1/harnesses", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"harnesses":[{"id":"claude","enabled":true,"found":"/bin/claude"},{"id":"codex","enabled":true,"found":""}]}`))
	})
	return mux
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for ; n > 0; n /= 10 {
		s = string(rune('0'+n%10)) + s
	}
	return s
}

func setupOf(t *testing.T, b *bench, room string) *RoomSetup {
	t.Helper()
	for _, a := range b.proxy.attachedView() {
		if a.Name == room {
			return a.Setup
		}
	}
	t.Fatalf("%s is not attached", room)
	return nil
}

func sweep(b *bench) { b.proxy.setupSweep(context.Background(), time.Now()) }

func TestARoomMissingHooksIsNamedOnItsRow(t *testing.T) {
	b := newBench(t)
	b.proxy.boardID = "aaaaaaaaaaaa"
	b.attach("m1mini", setupRoom(5, 0, "aaaaaaaaaaaa"))
	b.attach("sg4", setupRoom(0, 0, "aaaaaaaaaaaa"))
	if setupOf(t, b, "m1mini") != nil {
		t.Fatal("a room has facts before anything read it")
	}
	sweep(b)

	m := setupOf(t, b, "m1mini")
	if m == nil || !m.Answering || m.HooksMissing != 5 || len(m.Issues) != 1 || m.Issues[0].Kind != "hooks" {
		t.Fatalf("m1mini's setup = %+v", m)
	}
	if !strings.Contains(m.Issues[0].Text, "5 missing") {
		t.Fatalf("the issue does not say how many: %q", m.Issues[0].Text)
	}
	if got := strings.Join(m.Runners, ","); got != "claude" {
		t.Fatalf("runners = %q, want only the one that was found", got)
	}
	if m.Go != "go1.99" {
		t.Fatalf("go = %q", m.Go)
	}
	if s := setupOf(t, b, "sg4"); s == nil || len(s.Issues) != 0 {
		t.Fatalf("a room set up like the hub has issues: %+v", s)
	}
}

func TestAStaleHookIsCountedAndSaid(t *testing.T) {
	b := newBench(t)
	b.attach("alpha", setupRoom(2, 2, ""))
	sweep(b)
	s := setupOf(t, b, "alpha")
	if s.HooksStale != 2 || !strings.Contains(s.Issues[0].Text, "2 stale") {
		t.Fatalf("setup = %+v", s)
	}
}

func TestADifferentBuildIsNamedAndHasNoHooksIssue(t *testing.T) {
	b := newBench(t)
	b.proxy.boardID = "aaaaaaaaaaaa"
	b.attach("alpha", setupRoom(0, 0, "bbbbbbbbbbbb"))
	sweep(b)
	s := setupOf(t, b, "alpha")
	if len(s.Issues) != 1 || s.Issues[0].Kind != "build" {
		t.Fatalf("setup = %+v", s)
	}
}

// A hub that does not know its own build, as in a test, compares nothing.
func TestNoHubBuildMeansNoBuildMismatch(t *testing.T) {
	b := newBench(t)
	b.attach("alpha", setupRoom(0, 0, "bbbbbbbbbbbb"))
	sweep(b)
	if s := setupOf(t, b, "alpha"); len(s.Issues) != 0 {
		t.Fatalf("setup = %+v", s)
	}
}

// A room that does not answer is not answering, and is not told off for its hooks.
func TestARoomThatDoesNotAnswerIsNotASetupProblem(t *testing.T) {
	b := newBench(t)
	b.attach("alpha", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "busy", http.StatusServiceUnavailable)
	}))
	sweep(b)
	s := setupOf(t, b, "alpha")
	if s == nil || s.Answering || len(s.Issues) != 0 {
		t.Fatalf("setup = %+v", s)
	}
}

// The facts ride the same builder as the attached list, so the event and the endpoint carry them.
func TestTheSetupRidesTheRoomsEndpoint(t *testing.T) {
	b := newBench(t)
	b.attach("alpha", setupRoom(3, 0, ""))
	sweep(b)
	got := fetchFields(t, b.front.URL+"/_hub/rooms")
	rooms, _ := got["rooms"].([]any)
	if len(rooms) != 1 {
		t.Fatalf("rooms = %v", got)
	}
	setup, _ := rooms[0].(map[string]any)["setup"].(map[string]any)
	if setup == nil || setup["hooks_missing"].(float64) != 3 {
		t.Fatalf("the endpoint did not carry the setup: %v", rooms[0])
	}
}

// A fresh read is not repeated before it is due, and is repeated once it is.
func TestASetupIsReadAgainOnlyWhenDue(t *testing.T) {
	b := newBench(t)
	reads := 0
	inner := setupRoom(0, 0, "")
	b.attach("alpha", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/hooks" {
			reads++
		}
		inner.ServeHTTP(w, r)
	}))
	now := time.Now()
	b.proxy.setupSweep(context.Background(), now)
	b.proxy.setupSweep(context.Background(), now.Add(time.Minute))
	if reads != 1 {
		t.Fatalf("read %d times inside the interval", reads)
	}
	b.proxy.setupSweep(context.Background(), now.Add(setupEvery+time.Second))
	if reads != 2 {
		t.Fatalf("read %d times after the interval", reads)
	}
}

// The board's fix clears the pill at once through the check route.
func TestACheckRouteReadsOneRoomNow(t *testing.T) {
	b := newBench(t)
	missing := 4
	b.attach("alpha", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setupRoom(missing, 0, "").ServeHTTP(w, r)
	}))
	sweep(b)
	if setupOf(t, b, "alpha").HooksMissing != 4 {
		t.Fatal("the first read missed the hooks")
	}
	missing = 0
	res, err := http.Post(b.front.URL+"/_hub/setup-check?room=alpha", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("check answered %d", res.StatusCode)
	}
	if s := setupOf(t, b, "alpha"); s.HooksMissing != 0 || len(s.Issues) != 0 {
		t.Fatalf("setup after the fix = %+v", s)
	}
}
