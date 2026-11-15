package link

import (
	"io"
	"net/http"

	"github.com/dovholuknf/atrium/internal/base/edge"
)

// SetHubAuth makes the hub answer `/v1/auth` in the ALL view with the sign-in in front of its own published board,
// rather than borrowing a room's. view never carries the secret.
func (p *Proxy) SetHubAuth(view func() any, save func([]byte) error) {
	p.authView, p.authSave = view, save
}

func (p *Proxy) hubAuth(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/v1/auth" || p.authView == nil {
		return false
	}
	// A hub with only its own room still keeps its own sign-in, so only a named room borrows the room's.
	if _, named := p.roomFor(r); named {
		return false
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		writeJSONBody(w, http.StatusOK, p.authView())
	case http.MethodPut, http.MethodPost:
		// Anybody signed in to the published board reaches this route, so only the operator here may change who that is.
		if !edge.LocalOperator(r) {
			writeErrBody(w, http.StatusForbidden, "the login is changed from the machine the hub runs on"+edge.ProxyNote(r))
			return true
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<16))
		if err != nil {
			writeErrBody(w, http.StatusBadRequest, err.Error())
			return true
		}
		if err := p.authSave(body); err != nil {
			writeErrBody(w, http.StatusBadRequest, err.Error())
			return true
		}
		writeJSONBody(w, http.StatusOK, p.authView())
	default:
		writeErrBody(w, http.StatusMethodNotAllowed, "GET or PUT")
	}
	return true
}
