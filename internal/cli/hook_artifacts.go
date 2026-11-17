package cli

import (
	"encoding/json"
	"strings"
)

// fileTouch is the file a finished Write, Edit or NotebookEdit changed, and how: "new" or "edited", with the
// line counts the edit's own strings give. Empty path when the tool was none of those.
type fileTouch struct {
	Path    string `json:"file,omitempty"`
	Change  string `json:"file_change,omitempty"`
	Added   int    `json:"file_add,omitempty"`
	Removed int    `json:"file_del,omitempty"`
}

func lineCount(s string) int {
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}

func touchedFile(in hookInput) fileTouch {
	if in.ToolName != "Write" && in.ToolName != "Edit" && in.ToolName != "NotebookEdit" {
		return fileTouch{}
	}
	var ti struct {
		FilePath     string `json:"file_path"`
		NotebookPath string `json:"notebook_path"`
		Content      string `json:"content"`
		OldString    string `json:"old_string"`
		NewString    string `json:"new_string"`
	}
	if json.Unmarshal(in.ToolInput, &ti) != nil {
		return fileTouch{}
	}
	t := fileTouch{Path: ti.FilePath, Change: "edited", Added: lineCount(ti.NewString), Removed: lineCount(ti.OldString)}
	if t.Path == "" {
		t.Path = ti.NotebookPath
	}
	if in.ToolName == "Write" {
		var resp struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal(in.ToolResponse, &resp)
		t.Added, t.Removed = lineCount(ti.Content), 0
		if resp.Type == "create" {
			t.Change = "new"
		}
	}
	return t
}
