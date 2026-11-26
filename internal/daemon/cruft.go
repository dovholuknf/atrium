package daemon

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/api"
	"github.com/dovholuknf/atrium/internal/base/gitexec"
	"github.com/dovholuknf/atrium/internal/daemon/home"
	"github.com/dovholuknf/atrium/internal/daemon/hublink"
	"github.com/dovholuknf/atrium/internal/daemon/shared"
	"github.com/dovholuknf/atrium/internal/store"
)

// THE NIGHTLY CRUFT PASS: once a day, after CruftHour, the room scans its own card root, the worktrees under it, the
// claude/* and stash/* branches of the clones its cards worked in and their stash refs for what closed or gone cards left.
// It frees what holds no unique work, keeps the rest with the reason, and reports both to the hub, which holds the latest
// report per room (internal/link/cruft.go). A kept item the operator has to decide about is Needs, and only those show on
// the board.
//
// The folder sweep (endedfolders.go) is the part that frees folders, so this pass runs it and reads what it kept.

// CruftHour is the local hour a day's pass first becomes due, and CruftEvery how often the loop looks.
var (
	CruftHour  = 3
	CruftEvery = 10 * time.Minute
)

const (
	settingCruftDay = "cruft.last_day"
	cruftPath       = "/_hub/cruft"
	cruftPostWait   = 60 * time.Second
	cruftListMax    = 50
)

// CruftItem is one thing the pass freed or kept.
type CruftItem struct {
	Kind  string `json:"kind"`
	Ref   string `json:"ref"`
	Why   string `json:"why,omitempty"`
	Bytes int64  `json:"bytes,omitempty"`
	// Needs is a kept item only the operator can decide about.
	Needs bool `json:"needs,omitempty"`
}

// CruftReport is what one pass did.
type CruftReport struct {
	At         string      `json:"at"`
	FreedCount int         `json:"freed_count"`
	FreedBytes int64       `json:"freed_bytes"`
	Freed      []CruftItem `json:"freed"`
	Kept       []CruftItem `json:"kept"`
}

func (r *CruftReport) free(it CruftItem) {
	r.FreedCount++
	r.FreedBytes += it.Bytes
	if len(r.Freed) < cruftListMax {
		r.Freed = append(r.Freed, it)
	}
}

func (d *Daemon) cruftLoop(ctx context.Context) {
	if d.reattached != nil {
		select {
		case <-ctx.Done():
			return
		case <-d.reattached:
		}
	}
	for {
		if d.cruftOn() && d.cruftDue(time.Now()) {
			d.cruftNightly(ctx)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(CruftEvery):
		}
	}
}

// cruftOn is whether the room runs the nightly pass. Unset is off.
func (d *Daemon) cruftOn() bool {
	v, err := d.st.Setting(api.SettingCruftNightly)
	return err == nil && strings.TrimSpace(v) == "on"
}

// cruftDue is whether today's pass has not run and its hour has come.
func (d *Daemon) cruftDue(now time.Time) bool {
	last, err := d.st.Setting(settingCruftDay)
	return err == nil && now.Hour() >= CruftHour && last != now.Format(time.DateOnly)
}

// cruftNightly runs the pass and tells the hub. The day is marked only once the hub took the report, so a hub that was
// away is told on the next look.
func (d *Daemon) cruftNightly(ctx context.Context) {
	rep := d.cruftPass(ctx)
	if ctx.Err() != nil {
		return
	}
	if err := d.cruftPost(ctx, rep); err != nil {
		log.Printf("[atrium] the cruft report was not taken by the hub, it is sent again: %v", err)
		return
	}
	_ = d.st.SetSetting(settingCruftDay, time.Now().Format(time.DateOnly))
}

func (d *Daemon) cruftPost(ctx context.Context, rep *CruftReport) error {
	ha, _ := d.Relay().(hublink.HubAsker)
	if ha == nil {
		return os.ErrNotExist
	}
	raw, _ := json.Marshal(rep)
	ctx, cancel := context.WithTimeout(ctx, cruftPostWait)
	defer cancel()
	res, err := ha.Hub(ctx, "", http.MethodPost, cruftPath, raw)
	if err == nil && !res.OK {
		err = &taskPostError{msg: res.Error}
	}
	return err
}

// cruftPass frees what holds no unique work and lists what it kept.
func (d *Daemon) cruftPass(ctx context.Context) *CruftReport {
	rep := &CruftReport{At: time.Now().UTC().Format(time.RFC3339), Freed: []CruftItem{}, Kept: []CruftItem{}}
	for _, s := range d.sweepEndedFolders(ctx) {
		rep.free(CruftItem{Kind: "folder", Ref: s.Path, Bytes: s.Bytes})
	}
	sweepKept.Range(func(k, v any) bool {
		path, why := k.(string), v.(string)
		if _, err := os.Lstat(filepath.FromSlash(path)); err == nil {
			rep.Kept = append(rep.Kept, CruftItem{Kind: "folder", Ref: filepath.ToSlash(path), Why: why,
				Needs: !strings.Contains(why, "holds its card")})
		}
		return true
	})
	d.cruftUnrecorded(rep)
	d.cruftBranches(ctx, rep)
	slices.SortFunc(rep.Kept, func(a, b CruftItem) int { return strings.Compare(a.Kind+a.Ref, b.Kind+b.Ref) })
	return rep
}

// cruftUnrecorded lists the folders of the card root the ledger has no row for. Atrium never removes a folder it did not
// make, so each is the operator's to look at.
func (d *Daemon) cruftUnrecorded(rep *CruftReport) {
	root, err := d.CardRoot()
	if err != nil {
		return
	}
	ents, _ := os.ReadDir(root)
	open, _ := d.st.CardHomesOpen()
	for _, e := range ents {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		path := filepath.ToSlash(filepath.Join(root, e.Name()))
		if _, ok, _ := d.st.CardHomeByPath(path); !ok && !holdsHome(open, path) {
			rep.Kept = append(rep.Kept, CruftItem{Kind: "folder", Ref: path, Needs: true,
				Why: "no card home in the ledger names it, so atrium did not make it and does not remove it"})
		}
	}
}

// cruftClones are the clones the room's cards worked in, from their homes and their inventories.
func (d *Daemon) cruftClones() []string {
	seen := map[string]bool{}
	var out []string
	add := func(c string) {
		c = filepath.FromSlash(strings.TrimSpace(c))
		if c != "" && !seen[strings.ToLower(c)] && shared.HasDotGit(c) {
			seen[strings.ToLower(c)] = true
			out = append(out, c)
		}
	}
	if rows, err := d.st.CardHomesOpen(); err == nil {
		for _, r := range rows {
			add(r.Clone)
		}
	}
	live, _ := d.st.List()
	archived, _ := d.st.ListArchived(1 << 20)
	for _, t := range append(live, archived...) {
		if rs, err := d.st.Resources(t.ID); err == nil {
			for _, r := range rs {
				if r.Kind == store.ResBranch || r.Kind == store.ResWorktree {
					add(r.Detail)
				}
			}
		}
	}
	return out
}

// cruftBranches frees each claude/* and stash/* branch of those clones that no worktree has checked out, no card that is
// not ended names, and whose commits are on the hub or in claude/main. One that holds work nowhere else is kept, and its
// card's say is the only call there is, so it is Needs.
func (d *Daemon) cruftBranches(ctx context.Context, rep *CruftReport) {
	live := map[string]bool{}
	tasks, _ := d.st.List()
	now := time.Now()
	for _, t := range tasks {
		if b := strings.TrimSpace(t.Branch); b != "" && !d.cardEnded(t, nil, now) {
			live[b] = true
		}
	}
	for _, clone := range d.cruftClones() {
		if ctx.Err() != nil {
			return
		}
		held := map[string]bool{}
		if list, err := shared.GitIn(clone, gitexec.WorktreeList{}); err == nil {
			for _, l := range strings.Split(list, "\n") {
				if b, ok := strings.CutPrefix(strings.TrimSpace(l), "branch refs/heads/"); ok {
					held[b] = true
				}
			}
		}
		refs, err := shared.GitIn(clone, gitexec.ForEachRef{Format: "%(refname:short) %(objectname)",
			Patterns: []string{"refs/heads/claude", "refs/heads/stash"}})
		if err != nil {
			continue
		}
		for _, line := range strings.Split(refs, "\n") {
			name, tip, ok := strings.Cut(strings.TrimSpace(line), " ")
			if !ok || held[name] || live[name] || shared.ProtectedBranch(name, shared.DefaultCullInto) {
				continue
			}
			ref := filepath.ToSlash(clone) + " " + name
			if home.HubHas(clone, tip) || isAncestorOf(clone, tip, shared.DefaultCullInto) {
				if _, err := shared.GitIn(clone, gitexec.BranchDelete{Branch: name}); err == nil {
					rep.free(CruftItem{Kind: "branch", Ref: ref})
				}
				continue
			}
			ahead, _ := shared.GitIn(clone, gitexec.RevListCount{Revs: []string{tip}, NotRemotes: true})
			rep.Kept = append(rep.Kept, CruftItem{Kind: "branch", Ref: ref, Needs: true,
				Why: shared.OrDefault(ahead, "some") + " commits that are on no remote and not in " + shared.DefaultCullInto})
		}
		if out, err := shared.GitIn(clone, gitexec.ForEachRef{Format: "%(refname)", Patterns: []string{"refs/stash"}}); err == nil && out != "" {
			rep.Kept = append(rep.Kept, CruftItem{Kind: "stash", Ref: filepath.ToSlash(clone),
				Why: "a git stash holds work nowhere else"})
		}
	}
}

func isAncestorOf(clone, tip, into string) bool {
	_, err := shared.GitIn(clone, gitexec.IsAncestor{Commit: tip, Of: into})
	return err == nil
}
