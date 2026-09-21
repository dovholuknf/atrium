package claudeconf

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// THE LIST IS THE OPERATOR'S, WHICH IS THE WHOLE POINT. Atrium holds no model
// names and must not: they change every few months and one written into this
// repository ships wrong. It reads theirs.
func TestTheOperatorsOwnPickerIsRead(t *testing.T) {
	got := modelsIn(write(t, `{
      "modelPicker": { "options": [
        { "model": "claude-opus-5",   "label": "Opus 5" },
        { "model": "claude-opus-4-8", "label": "Opus 4.8" }
      ] }
    }`))
	if len(got) != 2 {
		t.Fatalf("read %d models: %v", len(got), got)
	}
	if got[0].ID != "claude-opus-5" || got[0].Label != "Opus 5" {
		t.Errorf("the first entry came back as %+v", got[0])
	}
	// ORDER IS THEIRS TOO. They wrote the list in the order they want to see
	// it, and sorting it would be atrium having an opinion about models.
	if got[1].ID != "claude-opus-4-8" {
		t.Errorf("the order changed: %+v", got)
	}
}

// A picker written without labels still works, because the id is a name.
func TestAModelWithNoLabelKeepsItsID(t *testing.T) {
	got := modelsIn(write(t, `{"modelPicker":{"options":[{"model":"claude-opus-4-6"}]}}`))
	if len(got) != 1 || got[0].ID != "claude-opus-4-6" || got[0].Label != "" {
		t.Fatalf("got %+v", got)
	}
}

// MOST MACHINES HAVE NO PICKER, and that is the normal case rather than a
// fault to report on the board.
func TestNoPickerIsNotAnError(t *testing.T) {
	for _, body := range []string{
		`{}`,
		`{"modelPicker":{}}`,
		`{"modelPicker":{"options":[]}}`,
		// Their file, mid-edit. Claude Code will tell them it is broken, and
		// two programs reporting one syntax error is noise.
		`{"modelPicker": {`,
	} {
		if got := modelsIn(write(t, body)); len(got) != 0 {
			t.Errorf("%s yielded %v", body, got)
		}
	}
	if got := modelsIn(filepath.Join(t.TempDir(), "nothing.json")); got != nil {
		t.Errorf("a missing file yielded %v", got)
	}
}

// Entries with nothing usable are dropped rather than drawn as empty buttons.
func TestBlankAndRepeatedEntriesAreDropped(t *testing.T) {
	got := modelsIn(write(t, `{"modelPicker":{"options":[
      {"model":"  ","label":"blank"},
      {"model":"claude-opus-5","label":"Opus 5"},
      {"model":"claude-opus-5","label":"again"},
      {"label":"no model at all"}
    ]}}`))
	if len(got) != 1 || got[0].ID != "claude-opus-5" || got[0].Label != "Opus 5" {
		t.Fatalf("got %+v", got)
	}
}
