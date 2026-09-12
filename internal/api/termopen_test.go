package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Opening a card's directory in a terminal window on the daemon's desktop.
//
// The tests are the three fences, because everything else about this endpoint
// is one `exec.Command`. What is worth asserting is that it does nothing until
// somebody configures it, and that a directory is an argument rather than a
// command whatever is in its name.

func termReq(id string) *http.Request {
	r := httptest.NewRequest("POST", "/v1/tasks/"+id+"/open-terminal", nil)
	r.SetPathValue("id", id)
	return r
}

func TestTerminalIsOffUntilConfigured(t *testing.T) {
	s, st, work := fileServer(t)
	task := cardIn(t, st, work)

	w := httptest.NewRecorder()
	s.openTerminal(w, termReq(task.ID))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("an unconfigured terminal answered %d, want 400", w.Code)
	}
	// The refusal has to say what to set and where the window would appear,
	// since neither is guessable from a menu entry that is not there.
	body := w.Body.String()
	for _, want := range []string{"terminal_command", "wt.exe -d {path}", "machine atrium"} {
		if !strings.Contains(body, want) {
			t.Errorf("the refusal never mentions %q: %s", want, body)
		}
	}
}

func TestTerminalNeedsADirectory(t *testing.T) {
	s, st, _ := fileServer(t)
	// A card with no worktree at all, which is every card atrium heard about
	// before it learned where it was running.
	task := cardIn(t, st, "")
	if err := st.SetSetting(SettingTerminal, "wt.exe -d {path}"); err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	s.openTerminal(w, termReq(task.ID))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("a card with no directory answered %d, want 400", w.Code)
	}
}

func TestTerminalRefusesADirectoryThatIsGone(t *testing.T) {
	s, st, work := fileServer(t)
	task := cardIn(t, st, filepath.Join(work, "went-away"))
	if err := st.SetSetting(SettingTerminal, "wt.exe -d {path}"); err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	s.openTerminal(w, termReq(task.ID))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("a missing directory answered %d, want 400", w.Code)
	}
}

// A DIRECTORY IS ONE ARGUMENT, whatever is in its name.
//
// This is the fence that matters, and the reason the configured string is split
// here rather than handed to a shell. A directory called `x; shutdown` on the
// other side of `cmd /c` is two commands.
func TestTerminalPathIsNeverASecondCommand(t *testing.T) {
	name, args := editorCommand(`wt.exe -d {path}`, `C:\work\x; shutdown -s`)
	if name != "wt.exe" {
		t.Fatalf("program is %q, want wt.exe", name)
	}
	if len(args) != 2 || args[0] != "-d" || args[1] != `C:\work\x; shutdown -s` {
		t.Fatalf("arguments are %q, want the flag and one whole path", args)
	}
}

// A command with no `{path}` still gets the directory, which is the obvious
// spelling for a terminal that takes one positionally.
func TestTerminalWithoutThePlaceholderStillGetsTheDirectory(t *testing.T) {
	name, args := editorCommand(`"C:\Program Files\wt\wt.exe"`, `C:\work`)
	if name != `C:\Program Files\wt\wt.exe` {
		t.Fatalf("program is %q, want the quoted path unquoted", name)
	}
	if len(args) != 1 || args[0] != `C:\work` {
		t.Fatalf("arguments are %q, want the directory appended", args)
	}
}

func TestTerminalRunsTheConfiguredCommand(t *testing.T) {
	s, st, work := fileServer(t)
	task := cardIn(t, st, work)

	// The test binary itself, with a filter that matches nothing, so something
	// real starts and exits at once. The endpoint does not wait on it, which is
	// the point of the whole feature: a terminal emulator returns immediately.
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetSetting(SettingTerminal,
		`"`+self+`" -test.run=TestNothingMatchesThisOnPurpose {path}`); err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	s.openTerminal(w, termReq(task.ID))
	if w.Code != http.StatusOK {
		t.Fatalf("a configured terminal answered %d: %s", w.Code, w.Body.String())
	}
}

func TestTerminalSaysWhichProgramItCouldNotRun(t *testing.T) {
	s, st, work := fileServer(t)
	task := cardIn(t, st, work)
	if err := st.SetSetting(SettingTerminal, "atrium-no-such-terminal {path}"); err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	s.openTerminal(w, termReq(task.ID))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("a missing program answered %d, want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "atrium-no-such-terminal") {
		t.Errorf("the failure never names the program: %s", w.Body.String())
	}
}
