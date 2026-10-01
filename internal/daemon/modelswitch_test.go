package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// Switching a live card's model. See modelswitch.go.

// switchModel posts one model switch at the handler and reads the answer.
func switchModel(t *testing.T, d *Daemon, id, body string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/tasks/"+id+"/model", strings.NewReader(body))
	req.SetPathValue("id", id)
	rec := httptest.NewRecorder()
	d.handleModel(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

// modelEvents are the card's model-switch events, oldest first (as Events returns them).
func modelEvents(t *testing.T, d *Daemon, id string) []map[string]any {
	t.Helper()
	evs, err := d.st.Events(id, 50)
	if err != nil {
		t.Fatal(err)
	}
	var out []map[string]any
	for i := range evs {
		if evs[i].Kind != store.EventNotified {
			continue
		}
		var p map[string]any
		if json.Unmarshal(evs[i].Payload, &p) == nil && p["by"] == modelSwitchBy {
			out = append(out, p)
		}
	}
	return out
}

func TestValidModel(t *testing.T) {
	for in, want := range map[string]string{
		"sonnet": "sonnet", " Opus ": "opus", "HAIKU": "haiku", "fable": "fable",
		"claude-opus-4-7": "claude-opus-4-7", "claude-sonnet-5-5[1m]": "claude-sonnet-5-5[1m]",
	} {
		if got, ok := validModel(in); !ok || got != want {
			t.Errorf("validModel(%q) = %q, %v, want %q", in, got, ok, want)
		}
	}
	for _, in := range []string{"", "  ", "gpt-5", "opus 4", "claude-opus\r/exit", "claude-a b", "claude-\"x\"",
		"claude-" + strings.Repeat("x", 80)} {
		if got, ok := validModel(in); ok {
			t.Errorf("validModel(%q) = %q, want refused", in, got)
		}
	}
}

// TYPED NOW. A free line takes `/model opus` and Enter, the card remembers the
// model, and the answer and the timeline say who asked, from what and to what.
func TestAModelSwitchIsTypedAndRecorded(t *testing.T) {
	d := testDaemon(t)
	target, _, f := peerPair(t, d)
	if err := d.st.SetModel(target.ID, "sonnet"); err != nil {
		t.Fatal(err)
	}

	code, out := switchModel(t, d, target.ID, `{"model":"Opus","from":"sg4/orch"}`)
	if code != http.StatusOK || out["typed"] != true || out["delivered"] != "terminal" {
		t.Fatalf("answered %d %v, want typed", code, out)
	}
	if got := f.written(); got != "/model opus\r" {
		t.Fatalf("the terminal got %q, want /model opus then Enter, with no banner", got)
	}
	if out["model"] != "opus" || out["from"] != "sonnet" {
		t.Fatalf("answer = %v, want opus from sonnet", out)
	}
	if got, _ := d.st.Get(target.ID); got.Model != "opus" {
		t.Fatalf("the card's model is %q, want opus, so a resume keeps it", got.Model)
	}
	evs := modelEvents(t, d, target.ID)
	if len(evs) != 1 || evs[0]["asked_by"] != "sg4/orch" || evs[0]["from"] != "sonnet" ||
		evs[0]["to"] != "opus" || evs[0]["state"] != "typed" {
		t.Fatalf("events = %v, want one typed switch by sg4/orch", evs)
	}
}

// A full id is taken as given, and the operator is the default asker.
func TestAModelSwitchTakesAFullIDAndDefaultsToTheOperator(t *testing.T) {
	d := testDaemon(t)
	target, _, f := peerPair(t, d)

	if code, out := switchModel(t, d, target.ID, `{"model":"claude-opus-4-7"}`); code != http.StatusOK {
		t.Fatalf("answered %d %v", code, out)
	}
	if got := f.written(); got != "/model claude-opus-4-7\r" {
		t.Fatalf("the terminal got %q", got)
	}
	if evs := modelEvents(t, d, target.ID); len(evs) != 1 || evs[0]["asked_by"] != "the operator" {
		t.Fatalf("events = %v, want the operator as the asker", evs)
	}
}

func TestAModelSwitchRefusesWhatItCannotType(t *testing.T) {
	d := testDaemon(t)
	target, _, f := peerPair(t, d)

	for _, body := range []string{``, `not json`, `{}`, `{"model":""}`, `{"model":"gpt-5"}`,
		`{"model":"claude-x\r/exit"}`, `{"model":"opus 4"}`} {
		if code, _ := switchModel(t, d, target.ID, body); code != http.StatusBadRequest {
			t.Errorf("body %q answered %d, want 400", body, code)
		}
	}
	if f.written() != "" {
		t.Fatalf("a refused switch still wrote %q", f.written())
	}
	if got, _ := d.st.Get(target.ID); got.Model != "" {
		t.Fatalf("a refused switch recorded %q", got.Model)
	}
}

func TestAModelSwitchOnAnUnknownCardIs404(t *testing.T) {
	d := testDaemon(t)
	if code, _ := switchModel(t, d, "nosuchcard", `{"model":"opus"}`); code != http.StatusNotFound {
		t.Fatalf("answered %d, want 404", code)
	}
}

// ONLY A CLAUDE CARD ATRIUM OWNS THE TERMINAL OF. Anything else is a 409 that
// says why, and records nothing.
func TestAModelSwitchIsRefusedWithoutAClaudeTerminal(t *testing.T) {
	d := testDaemon(t)

	// A claude card with no runner under atrium.
	bare := cardFor(t, d, "elsewhere")
	code, out := switchModel(t, d, bare.ID, `{"model":"opus"}`)
	if code != http.StatusConflict || !strings.Contains(out["error"].(string), "terminal") {
		t.Fatalf("no runner answered %d %v, want 409 naming the terminal", code, out)
	}

	// A runner that is not claude.
	other, _, err := d.st.Register(store.Observed{
		WireName: "codex-one", Worktree: "d:/git/atrium", Runner: "codex", PID: impossiblePID,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, f := typedRunner(t, d, other.ID)
	code, out = switchModel(t, d, other.ID, `{"model":"opus"}`)
	if code != http.StatusConflict || !strings.Contains(out["error"].(string), "claude") {
		t.Fatalf("a codex card answered %d %v, want 409 naming claude", code, out)
	}
	if f.written() != "" {
		t.Fatalf("a refused switch wrote %q", f.written())
	}
	for _, id := range []string{bare.ID, other.ID} {
		if got, _ := d.st.Get(id); got.Model != "" {
			t.Fatalf("a refused switch recorded %q on %s", got.Model, id)
		}
	}
}

// NOT INTO A PART WRITTEN LINE. The answer says waiting, the model is already on
// the card, and it is typed once the line clears.
func TestAModelSwitchWaitsForTheLineAndThenTypes(t *testing.T) {
	oldEvery := modelRetryEvery
	modelRetryEvery = 20 * time.Millisecond
	t.Cleanup(func() { modelRetryEvery = oldEvery })

	d := testDaemon(t)
	target, r, f := peerPair(t, d)
	r.noteOperatorTyped([]byte("git comm"))

	code, out := switchModel(t, d, target.ID, `{"model":"haiku","from":"sg4/orch"}`)
	if code != http.StatusOK || out["typed"] != false || out["delivered"] != "waiting" || out["note"] == nil {
		t.Fatalf("answered %d %v, want waiting with a note", code, out)
	}
	if f.written() != "" {
		t.Fatalf("typed into a part written line: %q", f.written())
	}
	if got, _ := d.st.Get(target.ID); got.Model != "haiku" {
		t.Fatalf("a waiting switch left the card on %q, want haiku recorded now", got.Model)
	}

	// The operator finishes the line and goes quiet.
	r.noteOperatorTyped([]byte("\r"))
	r.typeMu.Lock()
	r.lastTyped = time.Now().Add(-peerGateIdle - time.Second)
	r.typeMu.Unlock()

	deadline := time.Now().Add(5 * time.Second)
	for !strings.HasSuffix(f.written(), "") && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if got := f.written(); got != "/model haiku\r" {
		t.Fatalf("after the line cleared the terminal got %q", got)
	}
	for len(modelEvents(t, d, target.ID)) < 2 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	evs := modelEvents(t, d, target.ID)
	if len(evs) != 2 || evs[0]["state"] != "waiting" || evs[1]["state"] != "typed after waiting" {
		t.Fatalf("events = %v, want waiting then typed after waiting", evs)
	}
	if _, still := d.modelWaits.Load(target.ID); still {
		t.Fatal("the wait outlived the typing")
	}
}

// THE NEWEST REQUEST WINS. A switch that is still waiting is replaced, so the
// terminal never gets both.
func TestANewerModelSwitchReplacesAWaitingOne(t *testing.T) {
	oldEvery := modelRetryEvery
	modelRetryEvery = 20 * time.Millisecond
	t.Cleanup(func() { modelRetryEvery = oldEvery })

	d := testDaemon(t)
	target, r, f := peerPair(t, d)
	r.noteOperatorTyped([]byte("git comm"))

	switchModel(t, d, target.ID, `{"model":"haiku"}`)
	switchModel(t, d, target.ID, `{"model":"opus"}`)

	r.noteOperatorTyped([]byte("\r"))
	r.typeMu.Lock()
	r.lastTyped = time.Now().Add(-peerGateIdle - time.Second)
	r.typeMu.Unlock()

	deadline := time.Now().Add(5 * time.Second)
	for !strings.HasSuffix(f.written(), "") && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond)
	if got := f.written(); got != "/model opus\r" {
		t.Fatalf("the terminal got %q, want only the newer /model opus", got)
	}
	if got, _ := d.st.Get(target.ID); got.Model != "opus" {
		t.Fatalf("the card is on %q, want opus", got.Model)
	}
}

// A DIALOG ON SCREEN is answered by Enter, so nothing is typed.
func TestAModelSwitchIsNotTypedOverADialog(t *testing.T) {
	d := testDaemon(t)
	target, _, f := peerPair(t, d)
	d.act.dialogRaised(target.ID)

	code, out := switchModel(t, d, target.ID, `{"model":"opus"}`)
	if code != http.StatusOK || out["typed"] != false || out["delivered"] != "waiting" {
		t.Fatalf("answered %d %v, want waiting", code, out)
	}
	if f.written() != "" {
		t.Fatalf("typed over a dialog: %q", f.written())
	}
	d.modelWaits.Delete(target.ID)
}

// MID-TURN follows the runner's own setting, as an immediate say does: typed now
// when the runner takes input mid-turn, and waiting for the turn when it does not.
func TestAModelSwitchFollowsTheMidTurnRule(t *testing.T) {
	d := testDaemon(t)
	target, _, f := peerPair(t, d)
	d.act.set(target.ID, ActivityThinking, "")

	code, out := switchModel(t, d, target.ID, `{"model":"opus"}`)
	if code != http.StatusOK {
		t.Fatalf("answered %d %v", code, out)
	}
	if d.midTurnInputFor(target.ID) {
		if out["typed"] != true || out["when"] != "immediate" || f.written() != "/model opus\r" {
			t.Fatalf("mid-turn input is on, answer = %v written %q, want typed now", out, f.written())
		}
		return
	}
	if out["typed"] != false || out["when"] != "done" || f.written() != "" {
		t.Fatalf("mid-turn input is off, answer = %v written %q, want waiting for the turn", out, f.written())
	}
	d.modelWaits.Delete(target.ID)
}

// A `/model <id>` TYPED BY HAND is a switch the card remembers, so the room
// restart's resume does not put it back. This is what happened on @fabric.
func TestAHandTypedModelSwitchIsRecorded(t *testing.T) {
	d := testDaemon(t)
	target, r, _ := peerPair(t, d)

	r.noteOperatorTyped([]byte("/model claude-sonnet-5-5\r"))
	line := r.takeSubmitted()
	if line != "/model claude-sonnet-5-5" {
		t.Fatalf("the submitted line is %q", line)
	}
	if again := r.takeSubmitted(); again != "" {
		t.Fatalf("a line was handed over twice: %q", again)
	}
	d.noteTypedModel(target.ID, line)
	if got, _ := d.st.Get(target.ID); got.Model != "claude-sonnet-5-5" {
		t.Fatalf("the card's model is %q after a hand typed switch", got.Model)
	}
	evs := modelEvents(t, d, target.ID)
	if len(evs) != 1 || evs[0]["state"] != "typed by hand" || evs[0]["to"] != "claude-sonnet-5-5" {
		t.Fatalf("events = %v", evs)
	}
}

// Only an exact `/model <one value>` counts. The picker, other commands, a bad
// value and a line atrium could not follow record nothing.
func TestOnlyAnExactHandTypedModelLineIsRecorded(t *testing.T) {
	d := testDaemon(t)
	target, r, _ := peerPair(t, d)

	for _, line := range []string{"/model", "/model opus extra", "/modelx opus", "hello /model opus",
		"/model gpt-5", "/clear"} {
		d.noteTypedModel(target.ID, line)
	}
	if got, _ := d.st.Get(target.ID); got.Model != "" {
		t.Fatalf("a line that is not a switch recorded %q", got.Model)
	}
	// A line edited in a way atrium cannot follow is not trusted.
	r.noteOperatorTyped([]byte("/model opus\x1b[D"))
	r.noteOperatorTyped([]byte("\r"))
	if got := r.takeSubmitted(); got != "" {
		t.Fatalf("an unfollowed line was handed over: %q", got)
	}
}

// THE ROUTE IS ON THE BOARD'S HANDLER, and a card by its handle reaches it too.
func TestTheModelRouteIsServedAndTakesAHandle(t *testing.T) {
	d := testDaemon(t)
	target, _, f := peerPair(t, d)

	req := httptest.NewRequest(http.MethodPost, "/v1/tasks/"+target.WireName+"/model",
		strings.NewReader(`{"model":"fable"}`))
	rec := httptest.NewRecorder()
	d.BoardHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("answered %d %s", rec.Code, rec.Body.String())
	}
	if got := f.written(); got != "/model fable\r" {
		t.Fatalf("the terminal got %q", got)
	}
}

// THE STATUSLINE DOES NOT FIGHT THE CARD. Telemetry names the model the runner
// reports, in its own words, and never writes to the card's model.
func TestTelemetryDoesNotOverwriteTheCardsModel(t *testing.T) {
	d := testDaemon(t)
	target, _, _ := peerPair(t, d)
	if code, out := switchModel(t, d, target.ID, `{"model":"opus"}`); code != http.StatusOK {
		t.Fatalf("answered %d %v", code, out)
	}
	d.onTelemetry(TelemetryEvent{TaskID: target.ID, ContextUsed: 1000, ContextWindow: 200000, Model: "Sonnet 5"})
	if got, _ := d.st.Get(target.ID); got.Model != "opus" {
		t.Fatalf("telemetry moved the card's model to %q", got.Model)
	}
}
