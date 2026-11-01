package daemon

import (
	"errors"
	"fmt"
	"github.com/dovholuknf/atrium/internal/daemon/home"
	"github.com/dovholuknf/atrium/internal/daemon/shared"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/dovholuknf/atrium/internal/store"
)

// THE DISK VIEW: every home with its size, and what the room cannot say is its own. See card-home-design.md section 7.

type HomeInfo struct {
	store.CardHome
	Title    string `json:"title"`
	Status   string `json:"status"`
	Orphaned bool   `json:"orphaned"`
	Running  bool   `json:"running"`
}

type OutsideCard struct {
	Card  string `json:"card"`
	Title string `json:"title"`
	Path  string `json:"path"`
}

type DiskView struct {
	Root       string        `json:"root"`
	FreeBytes  uint64        `json:"free_bytes"`
	TotalBytes uint64        `json:"total_bytes"`
	Bytes      int64         `json:"bytes"`
	Error      string        `json:"error,omitempty"`
	Homes      []HomeInfo    `json:"homes"`
	NotAtriums []string      `json:"not_atriums"`
	Outside    []OutsideCard `json:"outside"`
}

const outsideCap = 200

// DiskView is the homes sorted by size, the folders under the root that have no row, and the cards working outside it.
func (d *Daemon) DiskView() (any, error) {
	v := DiskView{Homes: []HomeInfo{}, NotAtriums: []string{}, Outside: []OutsideCard{}}
	root, err := d.CardRoot()
	if err != nil {
		v.Error = err.Error()
	}
	if err == nil {
		v.Root = filepath.ToSlash(root)
		_, v.FreeBytes, v.TotalBytes = d.RootDisk()
	}
	rows, err := d.st.CardHomesOpen()
	if err != nil {
		return nil, err
	}
	known := map[string]bool{}
	for _, r := range rows {
		known[strings.ToLower(filepath.ToSlash(r.Path))] = true
		hi := HomeInfo{CardHome: r}
		if r.Card == "" {
			hi.Orphaned = true
		} else if t, err := d.st.Get(r.Card); err != nil {
			hi.Orphaned = true
		} else {
			hi.Title, hi.Status = t.DisplayTitle(), t.Status
			hi.Running = d.sup.Get(t.ID) != nil
		}
		v.Bytes += r.Bytes
		v.Homes = append(v.Homes, hi)
	}
	sort.SliceStable(v.Homes, func(i, j int) bool { return v.Homes[i].Bytes > v.Homes[j].Bytes })
	if v.Root != "" {
		if ents, err := os.ReadDir(root); err == nil {
			for _, e := range ents {
				p := strings.ToLower(filepath.ToSlash(filepath.Join(root, e.Name())))
				if !known[p] {
					v.NotAtriums = append(v.NotAtriums, filepath.ToSlash(filepath.Join(root, e.Name())))
				}
			}
		}
	}
	if ts, err := d.st.List(); err == nil {
		homed := map[string]bool{}
		for _, r := range rows {
			if r.Card != "" {
				homed[r.Card] = true
			}
		}
		for _, t := range ts {
			if homed[t.ID] || t.Worktree == "" || t.ArchivedAt != nil {
				continue
			}
			if v.Root != "" && insideRoot(root, t.Worktree) {
				continue
			}
			if len(v.Outside) < outsideCap {
				v.Outside = append(v.Outside, OutsideCard{Card: t.ID, Title: t.DisplayTitle(), Path: t.Worktree})
			}
		}
	}
	return v, nil
}

var insideRoot = shared.InsideRoot

type HomeRemoved struct {
	Removed bool   `json:"removed"`
	Kept    string `json:"kept,omitempty"`
	Freed   int64  `json:"freed,omitempty"`
}

// RemoveHome is the Disk view's remove button: the ledger row's id, never a path, and the removal of reclaimHome with
// the checks on the work skipped. It is the one caller that may skip them, and only for a home that is kept or orphaned.
func (d *Daemon) RemoveHome(id string) (any, error) {
	row, ok, err := d.st.CardHomeByID(id)
	if err != nil {
		return nil, err
	}
	if !ok || row.State == store.CardHomeRemoved {
		return nil, errors.New("that home is not on the ledger")
	}
	var t *store.Task
	if row.Card != "" {
		if got, err := d.st.Get(row.Card); err == nil {
			t = got
		}
	}
	if t != nil {
		if d.sup.Get(t.ID) != nil {
			return nil, errors.New("its runner is still up. exit it first")
		}
		if row.State != store.CardHomeKept {
			return nil, fmt.Errorf("%s is not kept. cull the card and atrium removes its home when the checks pass", t.DisplayTitle())
		}
	}
	freed := home.HomeBytes(row.Path)
	removed, kept, _ := d.ReclaimHome(t, row, true)
	res := HomeRemoved{Removed: removed, Kept: kept}
	if removed {
		res.Freed = freed
	}
	return res, nil
}

// OpenHome shows a home's folder in the file manager of the machine the room runs on, by the ledger row's id. Argv and
// never a shell, and only a folder homeGuard accepts.
func (d *Daemon) OpenHome(id string) (any, error) {
	row, ok, err := d.st.CardHomeByID(id)
	if err != nil {
		return nil, err
	}
	if !ok || row.State == store.CardHomeRemoved {
		return nil, errors.New("that home is not on the ledger")
	}
	path, err := d.HomeGuard(row)
	if err != nil {
		return nil, err
	}
	if fi, err := os.Stat(path); err != nil || !fi.IsDir() {
		return nil, errors.New("that folder is not there")
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("explorer.exe", path)
	case "darwin":
		cmd = exec.Command("open", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("could not open the folder: %v", err)
	}
	go func() { _ = cmd.Wait() }()
	return map[string]bool{"ok": true}, nil
}
