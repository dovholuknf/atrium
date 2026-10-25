package link

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/dovholuknf/atrium/internal/base/edge"
	"github.com/dovholuknf/atrium/internal/hubstore"
)

// The shepherd's row over /_hub/shepherds. See docs/rnd/script-minion.md, recommendation 5.
//
//	GET  /_hub/shepherds/<item>   the row, or 404
//	POST /_hub/shepherds/<item>   the row whole, written before the phase it names runs
//
// WRITES ARE THE MACHINE THE HUB RUNS ON, like the backlog: a shepherd is the operator's own process, and a row says
// which sha a human was shown, so nothing else may write it. It rides the items store, as items do.

func (p *Proxy) serveShepherds(w http.ResponseWriter, r *http.Request, sub string) {
	st := p.itemStore()
	if st == nil {
		http.NotFound(w, r)
		return
	}
	id := strings.Trim(strings.TrimPrefix(sub, "shepherds"), "/")
	if id == "" || strings.Contains(id, "/") {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		row, err := st.ShepherdGet(id)
		if err != nil {
			shepherdFail(w, err)
			return
		}
		crJSON(w, http.StatusOK, row)
	case http.MethodPost:
		switch {
		case docsCrossOrigin.Check(r) != nil:
			crFail(w, http.StatusForbidden, "a page on another origin cannot write here")
			return
		case !edge.LocalOperator(r):
			crFail(w, http.StatusForbidden, "a shepherd is written only from the machine the hub runs on"+edge.ProxyNote(r))
			return
		}
		var row hubstore.Shepherd
		raw, err := io.ReadAll(io.LimitReader(r.Body, hubstore.ShepherdMax*3))
		if err == nil {
			err = json.Unmarshal(raw, &row)
		}
		if err != nil {
			crFail(w, http.StatusBadRequest, "that is not a shepherd row: "+err.Error())
			return
		}
		// The path names the row, so a body cannot write another's.
		row.ItemID = id
		out, err := st.ShepherdPut(row)
		if err != nil {
			shepherdFail(w, err)
			return
		}
		crJSON(w, http.StatusOK, out)
	default:
		crFail(w, http.StatusMethodNotAllowed, "that has to be a GET or a POST")
	}
}

func shepherdFail(w http.ResponseWriter, err error) {
	code := http.StatusServiceUnavailable
	switch {
	case errors.Is(err, hubstore.ErrNoShepherd):
		code = http.StatusNotFound
	case hubstore.IsRefusal(err):
		code = http.StatusBadRequest
	}
	crFail(w, code, err.Error())
}
