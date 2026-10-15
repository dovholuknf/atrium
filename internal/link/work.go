package link

import (
	"errors"
	"net/http"
	"strings"

	"github.com/dovholuknf/atrium/internal/hubstore"
)

// WORK, ON THE HUB: the item as the unit of work, one per backlog id, with every card attached. READ ONLY. The rows are
// written when a room pushes an assignment that names its backlog id (POST /_hub/items/sync), so there is nothing to
// post here. See internal/hubstore/work.go and docs/changes/item-lifecycle-l1.md.
//
//	GET /_hub/work           the items, newest change first
//	GET /_hub/work/<id>      one: `work`, its `cards` and its `assignments` (the rows of /_hub/items that belong to it)

func (p *Proxy) serveWork(w http.ResponseWriter, r *http.Request, sub string) {
	st := p.itemStore()
	if st == nil {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		crFail(w, http.StatusMethodNotAllowed, "items are written by pushing an assignment, so this is a GET")
		return
	}
	rest := strings.Trim(strings.TrimPrefix(sub, "work"), "/")
	switch {
	case rest == "":
		list, err := st.WorkList(0)
		if err != nil {
			wfail(w, err)
			return
		}
		if list == nil {
			list = []hubstore.Work{}
		}
		crJSON(w, http.StatusOK, map[string]any{"work": list})
	case !strings.Contains(rest, "/"):
		wk, err := st.WorkByID(rest)
		if err != nil {
			wfail(w, err)
			return
		}
		cards, err := st.WorkCards(rest)
		if err != nil {
			wfail(w, err)
			return
		}
		as, err := st.WorkAssignments(rest)
		if err != nil {
			wfail(w, err)
			return
		}
		if cards == nil {
			cards = []hubstore.WorkCard{}
		}
		if as == nil {
			as = []hubstore.Item{}
		}
		crJSON(w, http.StatusOK, map[string]any{"work": wk, "cards": cards, "assignments": as})
	default:
		http.NotFound(w, r)
	}
}

func wfail(w http.ResponseWriter, err error) {
	code := http.StatusServiceUnavailable
	if errors.Is(err, hubstore.ErrNoWork) {
		code = http.StatusNotFound
	}
	crFail(w, code, err.Error())
}
