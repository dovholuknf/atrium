//go:build integration

package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// forceSayVia posts a say to the message endpoint and answers the code and the body.
func forceSayVia(t *testing.T, d *Daemon, id, body string) (int, map[string]any) {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/v1/tasks/"+id+"/message", strings.NewReader(body))
	r.SetPathValue("id", id)
	w := httptest.NewRecorder()
	d.handleMessage(w, r)
	out := map[string]any{}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

// loseTheLine is what a room restart does to the typing model: it cannot say what is on the prompt.
func loseTheLine(r *runner) {
	r.typeMu.Lock()
	r.line.unsure = reattachLineKey
	r.typeMu.Unlock()
}

// A gate that cannot read the line holds the say, and force types it, once, and puts it on the card's history under
// its own name.
func TestForceTypesWhenTheGateCannotReadTheLine(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	target, r, f := peerPair(t, d)
	loseTheLine(r)
	t.Cleanup(func() { d.pending.stopAll() })

	code, out := forceSayVia(t, d, target.ID, `{"text":"continue"}`)
	if code != http.StatusOK || out["delivered"] != "queued" || strings.Contains(f.written(), "continue") {
		t.Fatalf("without force the say should be held: %d %v %q", code, out, f.written())
	}
	if heldCount(d.pending, target.ID) != 1 {
		t.Fatal("the unforced say is not held")
	}

	code, out = forceSayVia(t, d, target.ID, `{"text":"FORCEWORDS","force":true}`)
	if code != http.StatusOK || out["delivered"] != "terminal" || out["forced"] != true {
		t.Fatalf("force answered %d %v", code, out)
	}
	got := f.written()
	if !strings.Contains(got, "FORCEWORDS") || !strings.HasSuffix(got, "\r") {
		t.Fatalf("force did not type and submit: %q", got)
	}
	evs, err := d.st.Events(target.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range evs {
		if p := string(e.Payload); strings.Contains(p, `"forced":true`) && strings.Contains(p, forcedHow) {
			found = true
		}
	}
	if !found {
		t.Fatal("the history does not record the forced say under its own name")
	}
}

// A dialog owns the terminal and force does not take it.
func TestForceIsRefusedWhileADialogIsOpen(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	target, r, f := peerPair(t, d)
	loseTheLine(r)
	d.act.dialogRaised(target.ID)

	code, out := forceSayVia(t, d, target.ID, `{"text":"FORCEWORDS","force":true}`)
	msg, _ := out["error"].(string)
	if code != http.StatusConflict || !strings.Contains(msg, "dialog") {
		t.Fatalf("force with a dialog answered %d %v", code, out)
	}
	if strings.Contains(f.written(), "FORCEWORDS") {
		t.Fatalf("typed into a dialog: %q", f.written())
	}
}

// A new-context cycle owns the terminal and force does not take it.
func TestForceIsRefusedDuringANewContextCycle(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	d := testDaemon(t)
	fastNewContext(t, d)
	task, f, _ := ncCard(t, d)
	if err := d.StartNewContext(task.ID); err != nil {
		t.Fatal(err)
	}
	until(t, "the limit prompt", func() bool { return strings.Contains(f.written(), limitPrompt) })
	before := f.written()

	code, out := forceSayVia(t, d, task.ID, `{"text":"FORCEWORDS","force":true}`)
	msg, _ := out["error"].(string)
	if code != http.StatusConflict || !strings.Contains(msg, "new-context") {
		t.Fatalf("force during a cycle answered %d %v", code, out)
	}
	if strings.Contains(f.written()[len(before):], "FORCEWORDS") {
		t.Fatal("typed into a cycle")
	}
}

// The chip's Type anyway types every held message and leaves the line alone.
func TestTypeAnywayTypesWhatIsHeld(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	target, r, f := peerPair(t, d)
	loseTheLine(r)
	holdTwo(t, d, target.ID)

	req := httptest.NewRequest(http.MethodPost, "/v1/tasks/"+target.ID+"/type-anyway", strings.NewReader("{}"))
	req.SetPathValue("id", target.ID)
	w := httptest.NewRecorder()
	d.handleTypeAnyway(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("answered %d: %s", w.Code, w.Body.String())
	}
	got := f.written()
	if strings.Index(got, "first words") < 0 || strings.Index(got, "second words") < strings.Index(got, "first words") {
		t.Fatalf("held messages not typed in order: %q", got)
	}
	if strings.Contains(got, "\x03") {
		t.Fatalf("the line was cleared: %q", got)
	}
	if heldCount(d.pending, target.ID) != 0 {
		t.Fatal("the messages are still held")
	}
}
