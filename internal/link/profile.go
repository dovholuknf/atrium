package link

import (
	"net/http"

	"github.com/dovholuknf/atrium/internal/base/edge"
	"github.com/dovholuknf/atrium/internal/hubstore"
	"github.com/dovholuknf/atrium/internal/link/wire"
)

// The operator's profile over /_hub/profile.
//
//	GET  /_hub/profile   the profile, atrium's own operator until one is written
//	POST /_hub/profile   the profile whole
//
// WRITES ARE THE MACHINE THE HUB RUNS ON, like the shepherd's row: the profile names who every land is by.

func (p *Proxy) serveProfile(w http.ResponseWriter, r *http.Request) {
	st := p.itemStore()
	if st == nil {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		pr, err := st.OperatorProfile()
		if err != nil {
			shepherdFail(w, err)
			return
		}
		crJSON(w, http.StatusOK, pr)
	case http.MethodPost:
		switch {
		case wire.DocsCrossOrigin.Check(r) != nil:
			crFail(w, http.StatusForbidden, "a page on another origin cannot write here")
			return
		case !edge.LocalOperator(r):
			crFail(w, http.StatusForbidden, "the profile is written only from the machine the hub runs on"+edge.ProxyNote(r))
			return
		}
		var pr hubstore.OperatorProfile
		if err := crDecode(r, &pr); err != nil {
			crFail(w, http.StatusBadRequest, "that is not a profile: "+err.Error())
			return
		}
		if err := st.SetOperatorProfile(pr); err != nil {
			shepherdFail(w, err)
			return
		}
		crJSON(w, http.StatusOK, pr)
	default:
		crFail(w, http.StatusMethodNotAllowed, "that has to be a GET or a POST")
	}
}
