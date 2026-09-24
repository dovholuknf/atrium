package runnerprofile

import (
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

// A row is recognised by id, then by the program it runs, so a second codex
// row with its own id is still codex.
func TestForFindsARunnerByIDThenByProgram(t *testing.T) {
	cases := []struct {
		h    store.Harness
		want string
	}{
		{store.Harness{ID: "codex"}, "codex"},
		{store.Harness{ID: "codex-fast", Cmd: `C:\Users\x\AppData\Roaming\npm\codex.cmd`}, "codex"},
		{store.Harness{ID: "mine", BinPath: "/usr/local/bin/gemini"}, "gemini"},
		{store.Harness{ID: "Claude"}, "claude"},
		{store.Harness{ID: "bash", Cmd: "bash"}, ""},
	}
	for _, c := range cases {
		if got := For(&c.h).ID; got != c.want {
			t.Errorf("%+v is %q, wanted %q", c.h, got, c.want)
		}
	}
	if For(nil).ID != "" {
		t.Error("a nil row found a profile")
	}
}

// Every default is claude's, so claude's own row has to be the zero terminal
// behaviour or the board would start treating claude like codex.
func TestClaudeNeedsNoTerminalHelp(t *testing.T) {
	if got := For(&store.Harness{ID: "claude"}).CursorSettle; got != 0 {
		t.Fatalf("claude holds the cursor for %v", got)
	}
	if got := For(&store.Harness{ID: "codex"}).CursorSettle; got <= 0 {
		t.Fatal("codex writes its cursor straight through, so it jumps across the input box")
	}
}
