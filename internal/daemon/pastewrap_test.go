package daemon

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

// hostile holds both markers and Shift+Tab, and ends in a lone ESC.
const hostile = "before \x1b[201~ \x1b[Z middle \x1b[200~ after\x1b"

// onlyBracketsAtTheEnds fails unless the bytes hold exactly one opener at the start and one closer at the end
// (ignoring a trailing Enter), and no Shift+Tab slipped out of the paste.
func onlyBracketsAtTheEnds(t *testing.T, what, got string) {
	t.Helper()
	body := strings.TrimSuffix(got, "\r")
	if strings.Count(got, pasteOpen) != 1 || strings.Count(got, pasteClose) != 1 {
		t.Fatalf("%s: want one opener and one closer, got %q", what, got)
	}
	if !strings.HasPrefix(body, pasteOpen) || !strings.HasSuffix(body, pasteClose) {
		t.Fatalf("%s: markers are not at the ends: %q", what, got)
	}
	inside := body[strings.Index(body, pasteOpen)+len(pasteOpen) : len(body)-len(pasteClose)]
	if strings.Contains(inside, "\x1b[20") || strings.HasSuffix(inside, "\x1b") {
		t.Fatalf("%s: a marker or ESC survives inside the paste: %q", what, got)
	}
}

func TestStripPasteMarkersSplicedMarker(t *testing.T) {
	in := "\x1b[20" + pasteClose + "1~x"
	if got := stripPasteMarkers(in); strings.Contains(got, "\x1b[20") {
		t.Fatalf("a marker re-formed after stripping: %q", got)
	}
}

func TestSayPastedStripsMarkers(t *testing.T) {
	p := &stubbornPty{most: 4096}
	r := &runner{pty: p}
	if err := r.SayPasted(hostile); err != nil {
		t.Fatal(err)
	}
	onlyBracketsAtTheEnds(t, "SayPasted", string(p.got))
}

func TestAMessageCannotEndItsOwnPaste(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()
	task := pasteTask(t, d, "paste-strip", "claude")
	p := &stubbornPty{most: 4096}
	d.sup.add(&runner{taskID: task.ID, pty: p})
	defer d.sup.remove(task.ID)

	body, err := json.Marshal(map[string]string{"text": hostile})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/v1/tasks/"+task.ID+"/message", bytes.NewReader(body))
	req.SetPathValue("id", task.ID)
	rec := httptest.NewRecorder()
	d.handleMessage(rec, req)
	if rec.Code != 200 {
		t.Fatalf("handleMessage returned %d: %s", rec.Code, rec.Body.String())
	}
	onlyBracketsAtTheEnds(t, "typeLabelledGuarded", string(p.got))
}

func TestAPeerMessageCannotEndItsOwnPaste(t *testing.T) {
	d := testDaemon(t)
	target := pasteTask(t, d, "peer-strip", "claude")
	_, f := typedRunner(t, d, target.ID)
	fresh, err := d.st.Get(target.ID)
	if err != nil {
		t.Fatal(err)
	}
	if typed, _ := d.tellByTyping(fresh, "sg4/builder", hostile, false); !typed {
		t.Fatal("the message was not typed")
	}
	got := f.written()
	// The banner sits outside the markers, so cut it off before looking.
	got = got[strings.Index(got, pasteOpen):]
	onlyBracketsAtTheEnds(t, "tellByTyping", got)
}

func TestAHeldMessageCannotEndItsOwnPaste(t *testing.T) {
	d := testDaemon(t)
	target := pasteTask(t, d, "held-strip", "claude")
	typedRunner(t, d, target.ID)
	t.Cleanup(func() { d.pending.drop(target.ID) })

	d.deferPeerInjection(target.ID, "m1", "", hostile, false)

	d.pending.mu.Lock()
	ht := d.pending.by[target.ID]
	d.pending.mu.Unlock()
	if ht == nil || len(ht.entries) == 0 {
		t.Fatal("nothing was held")
	}
	onlyBracketsAtTheEnds(t, "deferPeerInjection", ht.entries[0].body)
}
