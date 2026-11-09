//go:build integration

package daemon

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/base/cardproc"
	"github.com/dovholuknf/atrium/internal/store"
)

// fakeProc stands in for the joined session's process: alive with one start time until stopped.
type fakeProc struct {
	mu      sync.Mutex
	pid     int
	started string
	stopped []int
	resumed int
	reused  bool
}

func (f *fakeProc) startTime(pid int) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if pid != f.pid || len(f.stopped) > 0 {
		return "", cardproc.ErrGone
	}
	if f.reused {
		return "someone-else", nil
	}
	return f.started, nil
}

func (f *fakeProc) stop(pid int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopped = append(f.stopped, pid)
	return nil
}

func joinedHeld(t *testing.T, d *Daemon, pid int) (*store.Task, *store.Permission, *fakeProc) {
	t.Helper()
	if err := d.onSession(SessionEvent{Agent: "outside", Event: "join", Cwd: t.TempDir(), PID: pid, Runner: "claude"}); err != nil {
		t.Fatal(err)
	}
	card, err := d.st.GetByWireName("outside")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetStatus(card.ID, store.StatusNeedsPermission); err != nil {
		t.Fatal(err)
	}
	p, _, err := d.st.RecordPermission(card.ID, "Bash", "rm -rf build/cache", "", "")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeProc{pid: pid, started: "t0"}
	d.mv = moveIn{startTime: f.startTime, stop: f.stop, poll: time.Millisecond, quiet: time.Millisecond,
		limit: 200 * time.Millisecond, resume: func(w http.ResponseWriter, r *http.Request) {
			f.mu.Lock()
			f.resumed++
			f.mu.Unlock()
			w.WriteHeader(http.StatusOK)
		}}
	card, _ = d.st.Get(card.ID)
	return card, p, f
}

func moveInCall(d *Daemon, id string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	d.ap.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/tasks/"+id+"/move-in", nil))
	return w
}

func TestMoveInDeniesEndsWaitsAndResumes(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	card, p, f := joinedHeld(t, d, 4242)
	if w := moveInCall(d, card.ID); w.Code != http.StatusOK {
		t.Fatalf("move in answered %d: %s", w.Code, w.Body)
	}
	got, err := d.st.GetPermission(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Decision != "block" || got.Reason != MoveInReason {
		t.Fatalf("the held request was answered %q %q, want block with the move-in reason", got.Decision, got.Reason)
	}
	if len(f.stopped) != 1 || f.stopped[0] != 4242 {
		t.Fatalf("stopped %v, want only the card's pid 4242", f.stopped)
	}
	if f.resumed != 1 {
		t.Fatalf("resumed %d times, want once", f.resumed)
	}
}

func TestMoveInRefusesWhatIsNotAJoinedRunner(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		pid   int
		spoil func(*testing.T, *Daemon, *store.Task)
	}{
		"atrium holds its terminal": {4243, func(t *testing.T, d *Daemon, c *store.Task) { typedRunner(t, d, c.ID) }},
		"not running": {4243, func(t *testing.T, d *Daemon, c *store.Task) {
			if err := d.st.SetStatus(c.ID, store.StatusDone); err != nil {
				t.Fatal(err)
			}
		}},
		"no pid": {0, func(*testing.T, *Daemon, *store.Task) {}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			d := testDaemon(t)
			card, p, f := joinedHeld(t, d, c.pid)
			c.spoil(t, d, card)
			if w := moveInCall(d, card.ID); w.Code != http.StatusConflict {
				t.Fatalf("answered %d, want 409: %s", w.Code, w.Body)
			}
			if got, _ := d.st.GetPermission(p.ID); got.Decision != "" {
				t.Fatalf("a refused move in answered the request: %q", got.Decision)
			}
			if len(f.stopped) != 0 || f.resumed != 0 {
				t.Fatalf("a refused move in stopped %v and resumed %d", f.stopped, f.resumed)
			}
		})
	}
}

func TestMoveInNeverEndsAReusedPid(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	card, _, f := joinedHeld(t, d, 4244)
	seams := d.mv
	calls := 0
	d.mv.startTime = func(pid int) (string, error) {
		calls++
		if calls > 1 {
			f.reused = true
		}
		return seams.startTime(pid)
	}
	if w := moveInCall(d, card.ID); w.Code != http.StatusOK {
		t.Fatalf("move in answered %d: %s", w.Code, w.Body)
	}
	if len(f.stopped) != 0 {
		t.Fatalf("stopped %v, a pid that now names another process", f.stopped)
	}
}

func TestMoveInDoesNotResumeWhileTheProcessLives(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	card, _, f := joinedHeld(t, d, 4245)
	d.mv.stop = func(int) error { return nil }
	w := moveInCall(d, card.ID)
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "still running") {
		t.Fatalf("answered %d %s, want 409 still running", w.Code, w.Body)
	}
	if f.resumed != 0 {
		t.Fatal("resumed while the outside process still ran")
	}
}

func TestMoveInWaitsForTheHooksToGoQuiet(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	card, _, f := joinedHeld(t, d, 4246)
	d.mv.quiet = time.Hour
	d.act.set(card.ID, "Bash", "Bash")
	w := moveInCall(d, card.ID)
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "hooks still reach") {
		t.Fatalf("answered %d %s, want 409 hooks still reach", w.Code, w.Body)
	}
	if f.resumed != 0 {
		t.Fatal("resumed while its hooks were still talking")
	}
}

func TestMoveInHelperProcess(t *testing.T) {
	if os.Getenv("ATRIUM_MOVEIN_HELPER") != "1" {
		return
	}
	time.Sleep(time.Minute) // wait: the stand-in session, ended by the test that started it
}

func TestMoveInEndsARealProcess(t *testing.T) {
	t.Parallel()
	cmd := exec.Command(os.Args[0], "-test.run=^TestMoveInHelperProcess$")
	cmd.Env = append(os.Environ(), "ATRIUM_MOVEIN_HELPER=1")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan struct{})
	go func() { _ = cmd.Wait(); close(exited) }()
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	d := testDaemon(t)
	card, _, f := joinedHeld(t, d, cmd.Process.Pid)
	d.mv.startTime, d.mv.stop, d.mv.limit = nil, nil, 20*time.Second
	if w := moveInCall(d, card.ID); w.Code != http.StatusOK {
		t.Fatalf("move in answered %d: %s", w.Code, w.Body)
	}
	select {
	case <-exited:
	case <-time.After(10 * time.Second):
		t.Fatal("the joined process still runs after move in")
	}
	if f.resumed != 1 {
		t.Fatalf("resumed %d times, want once", f.resumed)
	}
}
