package daemon

import (
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/dovholuknf/atrium/internal/scratch"
	"github.com/dovholuknf/atrium/internal/store"
)

// THE ROOM'S SCRATCH ROOT. One setting names where throwaway files go, and every card on the room is told: launched
// ones through ATRIUM_SCRATCH, joined ones through the session-start hook. Unset means the card's own folder. Atrium
// removes a scratch folder only when a ledger row says it made it. See internal/scratch.

var scratchMu sync.Mutex

// scratchFor is the folder a card is told to put throwaway files in, made on first use. It is `home` when the room has no
// scratch root, or when the folder cannot be made.
func (d *Daemon) scratchFor(t *store.Task, home string) string {
	home = filepath.ToSlash(strings.TrimSpace(home))
	root := d.st.ScratchRoot()
	if root == "" {
		return home
	}
	if _, err := scratch.Check(root); err != nil {
		log.Printf("[atrium] scratch_root is not usable, so %s is told its own folder: %v", t.ID, err)
		return home
	}
	scratchMu.Lock()
	defer scratchMu.Unlock()
	if row, ok, err := d.st.CardScratchOf(t.ID); err == nil && ok {
		_ = os.MkdirAll(filepath.FromSlash(row.Path), 0o755)
		return row.Path
	}
	dir := scratch.Dir(root, t.DisplayTitle(), t.WireName, t.ID)
	path := filepath.FromSlash(dir)
	if _, err := os.Lstat(path); err == nil {
		return dir
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		log.Printf("[atrium] could not make the scratch root for %s: %v", t.ID, err)
		return home
	}
	if err := d.st.CardScratchAdd(dir, t.ID); err != nil {
		log.Printf("[atrium] could not record the scratch folder of %s: %v", t.ID, err)
		return home
	}
	if err := os.Mkdir(path, 0o755); err != nil && !os.IsExist(err) {
		_ = d.st.CardScratchRemoved(dir)
		log.Printf("[atrium] could not make the scratch folder of %s: %v", t.ID, err)
		return home
	}
	return dir
}

// scratchLine is the sentence the session-start hook carries for a card, empty when there is no folder to name.
func (d *Daemon) scratchLine(t *store.Task) string {
	dir := d.scratchFor(t, t.Worktree)
	if dir == "" {
		return ""
	}
	return scratch.Line(dir)
}

// removeScratch removes the scratch folder atrium made for a card, by its ledger row. A folder with no row, a link, or a
// path that is a drive root is left alone.
func (d *Daemon) removeScratch(t *store.Task) bool {
	row, ok, err := d.st.CardScratchOf(t.ID)
	if err != nil || !ok || row.Card != t.ID {
		return false
	}
	path := filepath.FromSlash(row.Path)
	if !filepath.IsAbs(path) || filepath.Dir(path) == path {
		return false
	}
	if fi, err := os.Lstat(path); err == nil && fi.Mode()&(fs.ModeSymlink|fs.ModeIrregular) != 0 {
		log.Printf("[atrium] the scratch folder %s is a link, so it was kept", row.Path)
		return false
	}
	if err := removeTree(path); err != nil {
		log.Printf("[atrium] could not remove the scratch folder %s: %v", row.Path, err)
		return false
	}
	_ = d.st.CardScratchRemoved(row.Path)
	log.Printf("[atrium] removed the scratch folder %s", row.Path)
	return true
}
