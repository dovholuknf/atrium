package link

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/dovholuknf/atrium/internal/gitsync"
	"github.com/dovholuknf/atrium/internal/hubstore"
)

// PageChangeGit is what the page-changed hint asks of the hub's repository: the newest commit on the mirror's main after
// `since` that changed `path`, false when none did. The repository the LandGit reads answers it when it can. Without it, or
// on any error, the hint is left out.
type PageChangeGit interface {
	PageChangedSince(ctx context.Context, since, path string) (gitsync.PageChange, bool, error)
}

// docDebtHintBound is how long the hints of one answer may spend on git. A slow mirror costs the hints, never the answer.
const docDebtHintBound = 10 * time.Second

// addPageHints fills Changed on each counted item that has a landed sha, for each page it names. It closes nothing and
// fails nothing: a page it cannot read has no hint, and an item filed by hand (no landed sha) has none at all.
func (p *Proxy) addPageHints(ctx context.Context, m *hubstore.DocDebtMeter) {
	p.mu.Lock()
	g, _ := p.landGit.(PageChangeGit)
	p.mu.Unlock()
	if g == nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, docDebtHintBound)
	defer cancel()
	for i := range m.Counted {
		c := &m.Counted[i]
		if c.LandedSHA == "" {
			continue
		}
		for _, page := range c.Pages {
			if ctx.Err() != nil {
				return
			}
			ch, ok, err := g.PageChangedSince(ctx, c.LandedSHA, page)
			if err != nil || !ok {
				continue
			}
			c.Changed = append(c.Changed, hubstore.DocDebtPageChange{Path: page, SHA: ch.SHA, Date: ch.Date})
		}
	}
}

// GET /_hub/doc-debt: how far behind the docs are, from the open `docs` backlog items. See internal/hubstore/docdebtmeter.go
// and docs/rnd/doc-debt-from-features.md section 2.5. Open like the backlog list it reads, and it names no path on the hub's
// disk, only the pages the items name. Without a backlog wired it answers 404. Nothing draws it yet.
// Each counted item with a landed sha also says which of its pages changed since, see addPageHints.
func (p *Proxy) serveDocDebt(w http.ResponseWriter, r *http.Request) {
	st := p.backlogStore()
	if st == nil {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "that has to be a GET"})
		return
	}
	m, err := st.DocDebtMeter()
	if err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "the hub could not read its store just now"})
		return
	}
	p.addPageHints(r.Context(), &m)
	_ = json.NewEncoder(w).Encode(m)
}
