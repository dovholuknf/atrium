//go:build windows

package daemon

import (
	"os/exec"
	"slices"
	"testing"
)

func TestNoWTWindowKeepsCardTextOffCmd(t *testing.T) {
	if _, err := exec.LookPath("wt.exe"); err == nil {
		t.Skip("wt.exe is installed, defaultTerminal picks it")
	}
	title, prompt := `a & echo PWNED`, `x" & echo PWNED & "`
	argv := expandTemplate(defaultTerminal(), `C:/w`, title, `C:/n/node.exe`, []string{`C:/n/cli.js`, prompt})
	if !slices.Equal(argv, []string{`C:/n/node.exe`, `C:/n/cli.js`, prompt}) {
		t.Errorf("want the runner alone, got %q", argv)
	}
}

func TestWTSeparatorFallsBackToOwnConsole(t *testing.T) {
	wt := []string{"wt.exe", "-w", "atrium", "new-tab", "--title", "{title}", "-d", "{cwd}", "{cmd}"}
	for _, c := range []struct{ title, prompt string }{{"t; new-tab calc", "p"}, {"t", "p ; new-tab calc"}, {"t", `p" x`}} {
		if got := windowTemplate(wt, `C:/w`, c.title, `C:/n/node.exe`, []string{c.prompt}); got[0] == "wt.exe" {
			t.Errorf("%q / %q reached wt.exe", c.title, c.prompt)
		}
	}
	if got := windowTemplate(wt, `C:/w`, "title", `C:/n/node.exe`, []string{"plain prompt"}); got[0] != "wt.exe" {
		t.Errorf("a plain card left wt.exe: %q", got)
	}
}
