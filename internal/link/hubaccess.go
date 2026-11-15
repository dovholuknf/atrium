package link

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/dovholuknf/atrium/internal/base/edge"
)

// HubAccess is the hub's list of ways in, as the Accessing Atrium screen reads and writes it. The cli package implements
// it over the hub's store, so this package never learns the hub has a database. Bodies are the screen's JSON.
type HubAccess interface {
	View() any
	SaveAuth(body []byte) error
	Add(ctx context.Context, body []byte) error
	Revoke(id string) error
	OpenUp(id, account string) error
	Approve(room string) error
}

// SetHubAccess makes the hub answer `/v1/access` in the ALL view.
func (p *Proxy) SetHubAccess(a HubAccess) { p.access = a }

func (p *Proxy) hubAccess(w http.ResponseWriter, r *http.Request) bool {
	if p.access == nil || (r.URL.Path != "/v1/access" && !strings.HasPrefix(r.URL.Path, "/v1/access/")) {
		return false
	}
	if _, named := p.roomFor(r); named {
		return false
	}
	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/access"), "/")
	if rest == "" && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
		writeJSONBody(w, http.StatusOK, p.access.View())
		return true
	}
	// Anybody signed in to the published board reaches this route, so only the operator here may change who gets in.
	if !edge.LocalOperator(r) {
		writeErrBody(w, http.StatusForbidden, "access is changed from the machine the hub runs on"+edge.ProxyNote(r))
		return true
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeErrBody(w, http.StatusBadRequest, err.Error())
		return true
	}
	parts := strings.Split(rest, "/")
	var do func() error
	switch {
	case rest == "auth" && (r.Method == http.MethodPut || r.Method == http.MethodPost):
		do = func() error { return p.access.SaveAuth(body) }
	case rest == "entries" && r.Method == http.MethodPost:
		do = func() error { return p.access.Add(r.Context(), body) }
	case len(parts) == 2 && parts[0] == "entries" && r.Method == http.MethodDelete:
		do = func() error { return p.access.Revoke(parts[1]) }
	case len(parts) == 3 && parts[0] == "entries" && parts[2] == "open" && r.Method == http.MethodPost:
		do = func() error {
			var in struct {
				Account string `json:"account"`
			}
			if err := json.Unmarshal(body, &in); err != nil {
				return err
			}
			return p.access.OpenUp(parts[1], in.Account)
		}
	case len(parts) == 3 && parts[0] == "rooms" && parts[2] == "approve" && r.Method == http.MethodPost:
		do = func() error { return p.access.Approve(parts[1]) }
	default:
		writeErrBody(w, http.StatusNotFound, "no such access route")
		return true
	}
	if err := do(); err != nil {
		code := http.StatusBadRequest
		if errors.Is(err, ErrAccessNotFound) {
			code = http.StatusNotFound
		}
		writeErrBody(w, code, err.Error())
		return true
	}
	writeJSONBody(w, http.StatusOK, p.access.View())
	return true
}

// ErrAccessNotFound is what a HubAccess wraps when the entry or room named does not exist.
var ErrAccessNotFound = errors.New("not found")
