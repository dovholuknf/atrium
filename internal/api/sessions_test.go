package api

import "testing"

// Claude Code names a directory's transcript folder by turning every character
// that is not a letter or a digit into a dash. A dot missed here meant a card in
// `build.claude` looked for a folder that was never written and never resumed.
func TestProjectDirEncodingMatchesClaudeCode(t *testing.T) {
	cases := map[string]string{
		`D:\git\github\dovholuknf\atrium`:                           "D--git-github-dovholuknf-atrium",
		`D:\worktrees\claude\atrium\terminal-suite\build.claude\tw`: "D--worktrees-claude-atrium-terminal-suite-build-claude-tw",
		`C:/Users/claude/AppData/Local/Temp/atrium-ts-tw/work`:      "C--Users-claude-AppData-Local-Temp-atrium-ts-tw-work",
		`D:\worktrees\my_repo\v1.2 beta`:                            "D--worktrees-my-repo-v1-2-beta",
		"  D:\\padded  ":                                            "D--padded",
	}
	for in, want := range cases {
		if got := encodeProjectDir(in); got != want {
			t.Errorf("encodeProjectDir(%q) = %q, want %q", in, got, want)
		}
	}
}
