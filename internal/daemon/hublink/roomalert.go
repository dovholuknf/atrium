package hublink

import (
	"errors"
	"fmt"
	"github.com/dovholuknf/atrium/internal/daemon/shared"
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
func (d *Service) AgentsRunning() int {
	ts, err := d.St.List(store.StatusRunning)
	if err != nil {
		return -1
	}
	n := 0
	for _, t := range ts {
		if d.Liveness(t) == store.Live {
			n++
		}
	}
	return n
}

// cardPlace is a directory that belongs to a card.
type CardPlace struct {
	Path, Card string
	Live       bool
}

func (d *Service) CardPlaces() []CardPlace {
	var out []CardPlace
	live := map[string]bool{}
	if ts, err := d.St.List(); err == nil {
		for _, t := range ts {
			live[t.ID] = d.Liveness(t) == store.Live
			if t.Worktree != "" {
				out = append(out, CardPlace{t.Worktree, t.ID, live[t.ID]})
			}
		}
	}
	if hs, err := d.St.CardHomesOpen(); err == nil {
		for _, h := range hs {
			out = append(out, CardPlace{h.Path, h.TaskID, live[h.TaskID]})
		}
	}
	return out
}

// classifyProc marks p. A live card's place wins over a closed one's, since a
// worktree can sit inside a closed card's home only if something reused it.
func ClassifyProc(p *roomstats.Proc, places []CardPlace, self int) {
	p.Kind, p.Card = roomstats.ProcForeign, ""
	cmd := strings.ToLower(filepath.ToSlash(p.Command))
	for _, c := range places {
		if c.Path == "" {
			continue
		}
		in := false
		if p.Cwd != "" {
			in = SameOrInside(c.Path, p.Cwd)
		} else {
			in = strings.Contains(cmd, strings.ToLower(filepath.ToSlash(filepath.Clean(c.Path)))+"/")
		}
		if !in || p.Kind == roomstats.ProcCardLive {
			continue
		}
		p.Kind, p.Card = roomstats.ProcCardClosed, c.Card
		if c.Live {
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

func SameOrInside(root, path string) bool {
	if root = filepath.Clean(filepath.FromSlash(root)); strings.EqualFold(root, filepath.Clean(filepath.FromSlash(path))) {
		return true
	}
	return shared.InsideRoot(root, path)
}

// topProcs is the alert's process list.
func (d *Service) TopProcs() ([]roomstats.Proc, error) {
	ps, err := roomstats.ListTop(roomstats.TopProcs)
	if err != nil {
		return nil, err
	}
	places := d.CardPlaces()
	for i := range ps {
		ClassifyProc(&ps[i], places, os.Getpid())
	}
	return ps, nil
}

// KillOrphan is the alert's kill. It reads the process again and kills it only
// if it is still under a closed card's place, so a stale alert or a reused pid
// never ends anything else.
func (d *Service) KillOrphan(pid int) error {
	p, ok, err := roomstats.ProcByPID(pid)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("process %d is gone", pid)
	}
	ClassifyProc(&p, d.CardPlaces(), os.Getpid())
	if p.Kind != roomstats.ProcCardClosed {
		return fmt.Errorf("process %d is %s: %w", pid, p.Kind, ErrNotOrphan)
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return proc.Kill()
}
