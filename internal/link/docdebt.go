package link

import (
	"encoding/json"
	"net/http"
)

// GET /_hub/doc-debt: how far behind the docs are, from the open `docs` backlog items. See internal/hubstore/docdebtmeter.go
// and docs/rnd/doc-debt-from-features.md section 2.5. Open like the backlog list it reads, and it names no path on the hub's
// disk, only the pages the items name. Without a backlog wired it answers 404. Nothing draws it yet.
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
	_ = json.NewEncoder(w).Encode(m)
}
