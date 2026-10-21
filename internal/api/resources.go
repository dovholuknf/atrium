package api

import (
	"context"
	"log"
	"net/http"
	"path/filepath"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
	"github.com/dovholuknf/atrium/internal/wtfree"
)

// A CARD'S INVENTORY, READ AND MEASURED. Design: docs/rnd/card-lifecycle-design.md section 6. Item
// r-card-inventory. The table is store/resources.go. The open verb writes it (open.go), and finish frees it.
//
//	GET  /v1/tasks/{id}/resources          the rows, and the card's disk
//	POST /v1/tasks/{id}/resources/measure  measure the sizes now, then the same answer
//
// SIZES ARE MEASURED ON ASK, and after an open, never on a timer. A worktree and a dir are their directory, and a
// review is its run folder. The card's disk is the sum over its live rows.

// measureWait bounds one measure of a card. A huge tree is reported as far as the walk got.
const measureWait = 30 * time.Second

func (s *Server) resourcesAnswer(w http.ResponseWriter, id string) {
	rows, err := s.st.Resources(id)
	if err != nil {
		s.fail(w, err)
		return
	}
	if rows == nil {
		rows = []*store.CardResource{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"resources": rows, "disk_bytes": diskOf(rows)})
}

// GET /v1/tasks/{id}/resources
func (s *Server) getResources(w http.ResponseWriter, r *http.Request) {
	s.resourcesAnswer(w, r.PathValue("id"))
}

// POST /v1/tasks/{id}/resources/measure
func (s *Server) measureResourcesNow(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.measureResources(r.Context(), id)
	if t, err := s.st.Get(id); err == nil {
		s.PublishTask(t)
	}
	s.resourcesAnswer(w, id)
}

// cardDisk is one card's measured bytes, 0 when nothing was measured.
func (s *Server) cardDisk(id string) int64 {
	rows, err := s.st.Resources(id)
	if err != nil {
		return 0
	}
	return diskOf(rows)
}

func diskOf(rows []*store.CardResource) int64 {
	var n int64
	for _, r := range rows {
		if r.Live() && r.Bytes > 0 {
			n += r.Bytes
		}
	}
	return n
}

// measureResources sizes every live row that has a directory.
func (s *Server) measureResources(ctx context.Context, id string) {
	rows, err := s.st.Resources(id)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, measureWait)
	defer cancel()
	for _, r := range rows {
		if !r.Live() {
			continue
		}
		dir := ""
		switch r.Kind {
		case store.ResWorktree, store.ResDir, store.ResClone:
			dir = r.Ref
		case store.ResReview:
			if p, err := s.st.PRByID(r.Ref); err == nil && p.RunDir != "" {
				dir = p.RunDir
			}
		}
		if dir == "" {
			continue
		}
		n := dirSize(ctx, filepath.FromSlash(dir))
		// A close is freeing this tree: a count taken while it goes would overwrite the size the close recorded.
		if _, busy := closing.Load(id); busy {
			return
		}
		if err := s.st.SetResourceBytes(r.Card, r.Seq, n); err != nil {
			log.Printf("[atrium api] measure %s %s: %v", id, dir, err)
		}
	}
}

// dirSize is the bytes of every regular file under dir, 0 for a directory that is not there. Symlinks are not
// followed, so a link out of the tree is not counted.
func dirSize(ctx context.Context, dir string) int64 { return wtfree.DirSize(ctx, dir) }
