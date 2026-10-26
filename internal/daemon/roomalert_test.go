package daemon

import (
	"path/filepath"
	"testing"

	"github.com/dovholuknf/atrium/internal/roomstats"
)

// EACH BUSY PROCESS IS NAMED BY WHERE IT RUNS. Only a process under a closed card's place is an orphan, a live
// card's place wins, and with no cwd the command line is read for the place.
func TestClassifyProc(t *testing.T) {
	root := t.TempDir()
	closed, live := filepath.Join(root, "old-card"), filepath.Join(root, "new-card")
	places := []cardPlace{
		{path: closed, card: "c-old"},
		{path: live, card: "c-new", live: true},
		{path: filepath.Join(closed, "work"), card: "c-reused", live: true},
	}
	for _, c := range []struct {
		name string
		p    roomstats.Proc
		kind string
		card string
	}{
		{"cwd under a closed home", roomstats.Proc{PID: 10, Cwd: filepath.Join(closed, "tmp")}, roomstats.ProcCardClosed, "c-old"},
		{"cwd is the closed home", roomstats.Proc{PID: 11, Cwd: closed + string(filepath.Separator)}, roomstats.ProcCardClosed, "c-old"},
		{"cwd under a live card", roomstats.Proc{PID: 12, Cwd: live}, roomstats.ProcCardLive, "c-new"},
		{"live place inside a closed one", roomstats.Proc{PID: 13, Cwd: filepath.Join(closed, "work", "src")}, roomstats.ProcCardLive, "c-reused"},
		{"no cwd, command names the place", roomstats.Proc{PID: 14, Command: "node " + filepath.Join(closed, "x.js")}, roomstats.ProcCardClosed, "c-old"},
		{"a sibling is not inside", roomstats.Proc{PID: 15, Cwd: closed + "-2", Command: "audiomxd"}, roomstats.ProcForeign, ""},
		{"the room itself", roomstats.Proc{PID: 99, Cwd: root}, roomstats.ProcAtrium, ""},
		{"the room's git child", roomstats.Proc{PID: 16, PPID: 99, Command: "git status"}, roomstats.ProcAtrium, ""},
		{"an atrium binary", roomstats.Proc{PID: 17, Command: filepath.Join(root, "atrium.exe") + " ptyhost"}, roomstats.ProcAtrium, ""},
	} {
		p := c.p
		classifyProc(&p, places, 99)
		if p.Kind != c.kind || p.Card != c.card {
			t.Errorf("%s: got %s %q, want %s %q", c.name, p.Kind, p.Card, c.kind, c.card)
		}
	}
}
