package daemon

import (
	"encoding/json"
	"testing"
)

func TestUnquoteStatusLine(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]struct{ in, want string }{
		"quoted program without a space": {`"V:/work/tools/git/bin/bash.exe" "C:/Users/claude/.claude/statusline-command.sh"`,
			`V:/work/tools/git/bin/bash.exe "C:/Users/claude/.claude/statusline-command.sh"`},
		"quoted program with a space": {`"C:/Program Files/Git/usr/bin/bash.exe" "C:/Users/claude/s.sh"`,
			`"C:/Program Files/Git/usr/bin/bash.exe" "C:/Users/claude/s.sh"`},
		"unquoted":          {`bash /c/Users/claude/.claude/statusline-command.sh`, `bash /c/Users/claude/.claude/statusline-command.sh`},
		"unterminated":      {`"bash`, `"bash`},
		"empty quoted name": {`"" x`, `"" x`},
	} {
		raw, _ := json.Marshal(map[string]string{"type": "command", "command": c.in})
		var got struct{ Command string }
		if err := json.Unmarshal(unquoteStatusLine(raw), &got); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got.Command != c.want {
			t.Errorf("%s: got %q want %q", name, got.Command, c.want)
		}
	}
}
