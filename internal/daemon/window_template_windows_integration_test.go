//go:build windows && integration

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
