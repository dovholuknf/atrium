package link

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/dovholuknf/atrium/internal/edge"
)

// The names the hub answers besides its own, as a setting in the gear.
//
//	GET /_hub/hosts   {"hosts": ["*.shares.zrok.io"], "ignored": [...], "env": [...]}
//	PUT /_hub/hosts   {"hosts": [...]}, replacing what was there
//
// Sharing the board over zrok failed with "this listener does not answer to that
// name" until somebody set $ATRIUM_HOSTS for the hub's user and restarted it by
// hand. Now the list lives in the hub's store and edge.SetExtra applies it to
// every listener from the next request, no restart. The variable stays, for a
// headless install, and both are answered.
//
// SET FROM THE HUB'S MACHINE ONLY, like the launch caps. This list IS the DNS
// rebinding guard (docs/rnd/security-design.md stage 0): a name here is a name a
// page could use if it controlled that name's DNS. A board reached over a share
// must not be able to widen it. Reading stays open.

// SettingExtraHosts is the hub setting holding the list, as a JSON array.
const SettingExtraHosts = "extra_hosts"

const (
	hostsMax    = 100
	hostNameMax = 253
)

type hostsView struct {
	Hosts   []string       `json:"hosts"`
	Ignored []edge.Ignored `json:"ignored"`
	// Env is $ATRIUM_HOSTS, answered as well and not editable here.
	Env []string `json:"env"`
}

// LoadExtraHosts applies the stored list to every listener. Called once as the
// hub starts. A store that fails or a value that will not parse applies nothing,
// which is the names the listeners already had.
func LoadExtraHosts(st HubSettings) {
	if st == nil {
		return
	}
	hosts := storedHosts(st)
	for _, ig := range edge.SetExtra(hosts) {
		log.Printf("[atrium] hosts setting: ignoring %q: %s", ig.Name, ig.Why)
	}
}

func storedHosts(st HubSettings) []string {
	v, err := st.HubSetting(SettingExtraHosts)
	if err != nil || strings.TrimSpace(v) == "" {
		return nil
	}
	var hosts []string
	if json.Unmarshal([]byte(v), &hosts) != nil {
		return nil
	}
	return hosts
}

// serveHosts answers GET and PUT /_hub/hosts.
func (p *Proxy) serveHosts(w http.ResponseWriter, r *http.Request) {
	fail := func(code int, msg string) {
		w.WriteHeader(code)
		fmt.Fprintf(w, `{"error":%q}`, msg)
	}
	p.mu.Lock()
	st := p.capStore
	p.mu.Unlock()
	if st == nil {
		http.NotFound(w, r)
		return
	}
	view := func(hosts []string) hostsView {
		if hosts == nil {
			hosts = []string{}
		}
		ignored := edge.CheckNames(hosts)
		if ignored == nil {
			ignored = []edge.Ignored{}
		}
		env := edge.EnvNames()
		if env == nil {
			env = []string{}
		}
		return hostsView{Hosts: hosts, Ignored: ignored, Env: env}
	}
	switch r.Method {
	case http.MethodGet:
		_ = json.NewEncoder(w).Encode(view(storedHosts(st)))
	case http.MethodPut:
		if !edge.LocalOperator(r) {
			fail(http.StatusForbidden, "the hosts the hub answers are set only from the machine the hub runs on"+edge.ProxyNote(r))
			return
		}
		var body struct {
			Hosts []string `json:"hosts"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body); err != nil {
			fail(http.StatusBadRequest, "could not read that: "+err.Error())
			return
		}
		hosts := make([]string, 0, len(body.Hosts))
		for _, h := range body.Hosts {
			if h = strings.TrimSpace(h); h == "" {
				continue
			}
			if len(h) > hostNameMax {
				fail(http.StatusBadRequest, "a host name is at most 253 characters")
				return
			}
			hosts = append(hosts, h)
		}
		if len(hosts) > hostsMax {
			fail(http.StatusBadRequest, fmt.Sprintf("at most %d hosts", hostsMax))
			return
		}
		b, _ := json.Marshal(hosts)
		if err := st.SetHubSetting(SettingExtraHosts, string(b)); err != nil {
			fail(http.StatusInternalServerError, "could not save the hosts: "+err.Error())
			return
		}
		// An ignored entry is saved as typed, so the row can show it with why,
		// and is answered by nothing.
		edge.SetExtra(hosts)
		p.RecordAudit("", "hosts-set", string(b))
		_ = json.NewEncoder(w).Encode(view(hosts))
	default:
		fail(http.StatusMethodNotAllowed, "that has to be a GET or a PUT")
	}
}
