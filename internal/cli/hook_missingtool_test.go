package cli

import (
	"encoding/json"
	"testing"
)

func TestMissingToolReadsAShellsCommandNotFound(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, tool, resp, err, want string
	}{
		{"bash stderr", "Bash", `{"stdout":"","stderr":"/usr/bin/bash: line 1: go: command not found"}`, "", "go"},
		{"bash failed", "Bash", "", "Exit code 127\nbash: make: command not found", "make"},
		{"powershell", "PowerShell", "", "The term 'node' is not recognized as a name of a cmdlet", "node"},
		{"gofmt is not go", "Bash", `{"stderr":"bash: gofmt: command not found"}`, "", "gofmt"},
		{"unknown tool", "Bash", `{"stderr":"bash: frob: command not found"}`, "", ""},
		{"not a shell", "Read", `{"content":"bash: go: command not found"}`, "", ""},
	}
	for _, c := range cases {
		in := hookInput{ToolName: c.tool, Error: c.err}
		if c.resp != "" {
			in.ToolResponse = json.RawMessage(c.resp)
		}
		if got := missingTool(in); got != c.want {
			t.Errorf("%s: got %q, wanted %q", c.name, got, c.want)
		}
	}
}
