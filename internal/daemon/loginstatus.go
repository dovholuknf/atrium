package daemon

import (
	"net/http"
)

// handleLoginStatus is `GET /v1/login-status`: preflight's claude check, as in, out or unknown.
func (d *Daemon) handleLoginStatus(w http.ResponseWriter, r *http.Request) {
	exe := "claude"
	if h, err := d.st.Harness(exe); err == nil && h != nil {
		exe = h.Exe()
	}
	it := d.preflightOne(r.Context(), "claude", append([]string{exe}, preflightRunners["claude"]...), exe)
	state := "out"
	switch {
	case it.OK:
		state = "in"
	case it.Error == "not found" || it.Error == "timeout":
		state = "unknown"
	}
	writeJSONBody(w, map[string]string{"state": state})
}
