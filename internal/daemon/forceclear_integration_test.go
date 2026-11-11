//go:build integration

package daemon

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

func forceClearCard(t *testing.T, frame string) (*Daemon, *store.Task, *runner, *fakePTY) {
	t.Helper()
	d := testDaemon(t)
	task, _, err := d.st.Register(store.Observed{WireName: "fc", Worktree: t.TempDir(), Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	f := newFakePTY()
	t.Cleanup(func() { f.Close() })
	r := &runner{taskID: task.ID, pty: f, started: time.Now(), buf: newRingSized(1<<16, 80, 24),
		watchers: map[chan []byte]struct{}{}, done: make(chan struct{})}
	_, _ = r.buf.Write([]byte(frame))
	d.sup.add(r)
	return d, task, r, f
}

func postForceClear(d *Daemon, id string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/v1/tasks/"+id+"/force-clear", nil)
	req.SetPathValue("id", id)
	rec := httptest.NewRecorder()
	d.handleForceClear(rec, req)
	return rec
}

func TestForceClearThrowsTheLineAwayAndEmptiesTheModel(t *testing.T) {
	t.Parallel()
	d, task, r, f := forceClearCard(t, string(idleFrameWithText("stop sgg both processes")))
	r.noteOperatorTyped([]byte("stop sgg both processes"))
	r.typeMu.Lock()
	r.lastTyped = time.Now().Add(-letThroughQuiet - time.Second)
	r.typeMu.Unlock()

	rec := postForceClear(d, task.ID)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if got := f.written(); got != "\x03" {
		t.Fatalf("wrote %q, want one control-c", got)
	}
	if s := r.typing(); s.Count != 0 {
		t.Fatalf("the model still holds %q", s.Line)
	}
}

func TestForceClearRefusesWhileAKeyIsLanding(t *testing.T) {
	t.Parallel()
	d, task, r, f := forceClearCard(t, string(idleFrameWithText("half")))
	r.noteOperatorTyped([]byte("half"))

	rec := postForceClear(d, task.ID)

	if rec.Code != http.StatusConflict || f.written() != "" {
		t.Fatalf("status %d, wrote %q", rec.Code, f.written())
	}
}

func TestForceClearRefusesMidTurnWhereControlCWouldInterrupt(t *testing.T) {
	t.Parallel()
	d, task, _, f := forceClearCard(t, string(idleFrameWithText("")))
	d.act.set(task.ID, ActivityTool, "Bash")

	rec := postForceClear(d, task.ID)

	if rec.Code != http.StatusConflict || f.written() != "" {
		t.Fatalf("status %d, wrote %q", rec.Code, f.written())
	}
}

func TestForceClearOnACardWithNoTerminalIs404(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	if rec := postForceClear(d, "nobody"); rec.Code != http.StatusNotFound {
		t.Fatalf("status %d", rec.Code)
	}
}

// The text to show is the model's when it is exact, else the prompt row on screen, else nothing.
func TestTheLineShownComesFromTheModelThenTheScreenThenNothing(t *testing.T) {
	t.Parallel()
	_, _, r, _ := forceClearCard(t, string(idleFrameWithText("on the screen")))

	r.noteOperatorTyped([]byte("typed by hand"))
	if text, from := r.lineShown(r.typing()); text != "typed by hand" || from != lineFromModel {
		t.Fatalf("exact model: %q from %q", text, from)
	}

	r.typeMu.Lock()
	r.line.lost("a recall")
	r.typeMu.Unlock()
	if text, from := r.lineShown(r.typing()); text != "on the screen" || from != lineFromScreen {
		t.Fatalf("unsure model: %q from %q", text, from)
	}

	_, _ = r.buf.Write([]byte("\x1b[2J\x1b[Hnothing drawn here"))
	if text, from := r.lineShown(r.typing()); text != "" || from != "" {
		t.Fatalf("no readable line: %q from %q", text, from)
	}
}

func TestTheTypingAnswerCarriesTheLineShown(t *testing.T) {
	t.Parallel()
	d, task, r, _ := forceClearCard(t, string(idleFrameWithText("x")))
	r.noteOperatorTyped([]byte("draft one"))
	req := httptest.NewRequest(http.MethodGet, "/v1/tasks/"+task.ID+"/typing", nil)
	req.SetPathValue("id", task.ID)
	rec := httptest.NewRecorder()
	d.handleTypingState(rec, req)
	if b := rec.Body.String(); !strings.Contains(b, `"shown":"draft one"`) || !strings.Contains(b, `"shown_from":"model"`) {
		t.Fatalf("answer: %s", b)
	}
}
