package daemon

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dovholuknf/atrium/internal/roomstats"
	"github.com/dovholuknf/atrium/internal/store"
)

// The room's alert, f-room-cpu-alert: how many agents are mid-turn, whose each
// busy process is, and the one kill the alert offers. See internal/roomstats/alert.go.

// ErrNotOrphan is a kill refused because the process is not under a closed card's home.
var ErrNotOrphan = errors.New("not an orphan of a closed card")

// agentsRunning is the cards mid-turn whose session is there.
func (d *Daemon) agentsRunning() int {
	ts, err := d.st.List(store.StatusRunning)
	if err != nil {
		return -1
	}
	n := 0
	for _, t := range ts {
		if d.liveness(t) == store.Live {
			n++
		}
	}
	return n
}

// cardPlace is a directory that belongs to a card.
type cardPlace struct {
	path, card string
	live       bool
}

func (d *Daemon) cardPlaces() []cardPlace {
	var out []cardPlace
	live := map[string]bool{}
	if ts, err := d.st.List(); err == nil {
		for _, t := range ts {
			live[t.ID] = d.liveness(t) == store.Live
			if t.Worktree != "" {
				out = append(out, cardPlace{t.Worktree, t.ID, live[t.ID]})
			}
		}
	}
	if hs, err := d.st.CardHomesOpen(); err == nil {
		for _, h := range hs {
			out = append(out, cardPlace{h.Path, h.TaskID, live[h.TaskID]})
		}
	}
	return out
}

// classifyProc marks p. A live card's place wins over a closed one's, since a
// worktree can sit inside a closed card's home only if something reused it.
func classifyProc(p *roomstats.Proc, places []cardPlace, self int) {
	p.Kind, p.Card = roomstats.ProcForeign, ""
	cmd := strings.ToLower(filepath.ToSlash(p.Command))
	for _, c := range places {
		if c.path == "" {
			continue
		}
		in := false
		if p.Cwd != "" {
			in = sameOrInside(c.path, p.Cwd)
		} else {
			in = strings.Contains(cmd, strings.ToLower(filepath.ToSlash(filepath.Clean(c.path)))+"/")
		}
		if !in || p.Kind == roomstats.ProcCardLive {
			continue
		}
		p.Kind, p.Card = roomstats.ProcCardClosed, c.card
		if c.live {
			p.Kind = roomstats.ProcCardLive
		}
	}
	if p.Kind != roomstats.ProcForeign {
		return
	}
	exe := strings.ToLower(filepath.Base(strings.Fields(cmd + " x")[0]))
	if p.PID == self || p.PPID == self || strings.HasPrefix(exe, "atrium") {
		p.Kind = roomstats.ProcAtrium
	}
}

func sameOrInside(root, path string) bool {
	if root = filepath.Clean(filepath.FromSlash(root)); strings.EqualFold(root, filepath.Clean(filepath.FromSlash(path))) {
		return true
	}
	return insideRoot(root, path)
}

// topProcs is the alert's process list.
func (d *Daemon) topProcs() ([]roomstats.Proc, error) {
	ps, err := roomstats.ListTop(roomstats.TopProcs)
	if err != nil {
		return nil, err
	}
	places := d.cardPlaces()
	for i := range ps {
		classifyProc(&ps[i], places, os.Getpid())
	}
	return ps, nil
}

// KillOrphan is the alert's kill. It reads the process again and kills it only
// if it is still under a closed card's place, so a stale alert or a reused pid
// never ends anything else.
func (d *Daemon) KillOrphan(pid int) error {
	p, ok, err := roomstats.ProcByPID(pid)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("process %d is gone", pid)
	}
	classifyProc(&p, d.cardPlaces(), os.Getpid())
	if p.Kind != roomstats.ProcCardClosed {
		return fmt.Errorf("process %d is %s: %w", pid, p.Kind, ErrNotOrphan)
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return proc.Kill()
}
