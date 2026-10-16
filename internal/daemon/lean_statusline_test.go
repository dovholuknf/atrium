package daemon

import (
	"encoding/json"
	"testing"
)

func TestWrapStatusLine(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]struct{ in, want string }{
		"quoted bash and a script": {`"V:/work/tools/git/bin/bash.exe" "C:/Users/claude/.claude/statusline-command.sh"`,
			`V:/work/tools/git/bin/bash.exe -c 'C:/Users/claude/.claude/statusline-command.sh'`},
		"bare bash and a script": {`bash /c/Users/claude/.claude/statusline-command.sh`,
			`bash -c '/c/Users/claude/.claude/statusline-command.sh'`},
		"bash path with a space keeps its quotes": {`"C:/Program Files/Git/usr/bin/bash.exe" "C:/Users/claude/s.sh"`,
			`"C:/Program Files/Git/usr/bin/bash.exe" -c 'C:/Users/claude/s.sh'`},
		"already -c":            {`bash -c 'x.sh'`, `bash -c 'x.sh'`},
		"not bash":              {`pwsh status.ps1`, `pwsh status.ps1`},
		"script with a space":   {`bash "a b.sh"`, `bash "a b.sh"`},
		"bash alone":            {`bash`, `bash`},
		"unterminated quote":    {`"bash x.sh`, `"bash x.sh`},
		"a pipe is not touched": {`bash x.sh | cat`, `bash x.sh | cat`},
	} {
		raw, _ := json.Marshal(map[string]string{"type": "command", "command": c.in})
		var got struct{ Command string }
		if err := json.Unmarshal(wrapStatusLine(raw), &got); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got.Command != c.want {
			t.Errorf("%s: got %q want %q", name, got.Command, c.want)
		}
	}
}
