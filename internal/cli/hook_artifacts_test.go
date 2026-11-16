package cli

import (
	"encoding/json"
	"testing"
)

func TestTouchedFileReadsWhatAWriteAndAnEditChanged(t *testing.T) {
	t.Parallel()
	w := touchedFile(hookInput{ToolName: "Write", ToolInput: json.RawMessage(`{"file_path":"a/b.go","content":"x\ny"}`),
		ToolResponse: json.RawMessage(`{"type":"create"}`)})
	if w.Path != "a/b.go" || w.Change != "new" || w.Added != 2 {
		t.Fatalf("a created file read as %+v", w)
	}
	e := touchedFile(hookInput{ToolName: "Edit", ToolInput: json.RawMessage(`{"file_path":"a/b.go","old_string":"x","new_string":"y\nz"}`)})
	if e.Change != "edited" || e.Added != 2 || e.Removed != 1 {
		t.Fatalf("an edit read as %+v", e)
	}
	if touchedFile(hookInput{ToolName: "Bash", ToolInput: json.RawMessage(`{"file_path":"a"}`)}).Path != "" {
		t.Fatal("a shell command counted as a file")
	}
}
