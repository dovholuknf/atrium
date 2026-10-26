package launch

import (
	"strings"
	"testing"
)

func fakeExists(files ...string) func(string) bool {
	return func(p string) bool {
		for _, f := range files {
			if f == p {
				return true
			}
		}
		return false
	}
}

const toolsBash = `C:\Users\u\.local\share\atrium-tools\git\bin\bash.exe`

func TestGitBashSkippedOffWindowsAndForOtherRunners(t *testing.T) {
	if add, err := gitBashFor("linux", "claude", nil, "", fakeExists()); add != "" || err != nil {
		t.Fatalf("linux: %q %v", add, err)
	}
	if add, err := gitBashFor("windows", "codex", nil, "", fakeExists()); add != "" || err != nil {
		t.Fatalf("codex: %q %v", add, err)
	}
}

func TestGitBashEnvVarHonoured(t *testing.T) {
	env := []string{`CLAUDE_CODE_GIT_BASH_PATH=D:\g\bin\bash.exe`}
	add, err := gitBashFor("windows", "claude", env, "", fakeExists(`D:\g\bin\bash.exe`))
	if add != "" || err != nil {
		t.Fatalf("%q %v", add, err)
	}
}

func TestGitBashMinGitOnlyIsRefused(t *testing.T) {
	_, err := gitBashFor("windows", "claude", []string{`PATH=C:\MinGit\cmd`}, `C:\Users\u`, fakeExists())
	if err == nil || !strings.Contains(err.Error(), "CLAUDE_CODE_GIT_BASH_PATH") {
		t.Fatalf("want a refusal naming the variable, got %v", err)
	}
}

func TestGitBashFullGitOnPathUnchanged(t *testing.T) {
	add, err := gitBashFor("windows", "claude", []string{`PATH=C:\Git\cmd`}, "", fakeExists(`C:\Git\cmd\git.exe`, `C:\Git\bin\bash.exe`))
	if add != "" || err != nil {
		t.Fatalf("%q %v", add, err)
	}
}

func TestGitBashAtriumToolsGitIsSet(t *testing.T) {
	add, err := gitBashFor("windows", "claude", nil, `C:\Users\u`, fakeExists(toolsBash))
	if err != nil || add != "CLAUDE_CODE_GIT_BASH_PATH="+toolsBash {
		t.Fatalf("%q %v", add, err)
	}
}

func TestGitBashGitInSecondPathEntry(t *testing.T) {
	env := []string{`PATH=C:\Windows;C:\Git\cmd;C:\x`}
	add, err := gitBashFor("windows", "claude", env, "", fakeExists(`C:\Git\cmd\git.exe`, `C:\Git\bin\bash.exe`))
	if add != "" || err != nil {
		t.Fatalf("%q %v", add, err)
	}
}
