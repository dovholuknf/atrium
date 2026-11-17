package link

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/dovholuknf/atrium/internal/base/edge"
	"github.com/dovholuknf/atrium/internal/capability"
)

// SettingCapabilityList holds the YAML a person edited.
const SettingCapabilityList = "capability_list"

// CapabilityList is the hub's declared list, and Version is the one its YAML names.
type CapabilityList struct {
	YAML    string `json:"yaml"`
	Version int    `json:"version"`
	Hash    string `json:"hash,omitempty"`
}

func (p *Proxy) capabilityList() (CapabilityList, error) {
	p.mu.Lock()
	st := p.capStore
	p.mu.Unlock()
	var out CapabilityList
	if st == nil {
		return out, nil
	}
	v, err := st.HubSetting(SettingCapabilityList)
	if err != nil || v == "" {
		return out, err
	}
	l, err := capability.Lint([]byte(v))
	if err != nil {
		return out, err
	}
	sum := sha256.Sum256([]byte(v))
	return CapabilityList{YAML: v, Version: l.Version, Hash: hex.EncodeToString(sum[:8])}, nil
}

// serveCapabilityList answers GET and PUT /_hub/capability-list. A save is local only, since every room runs what it says.
func (p *Proxy) serveCapabilityList(w http.ResponseWriter, r *http.Request) {
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
	switch r.Method {
	case http.MethodGet:
		cur, err := p.capabilityList()
		if err != nil {
			fail(http.StatusInternalServerError, "the stored capability list is unusable: "+err.Error())
			return
		}
		_ = json.NewEncoder(w).Encode(cur)
	case http.MethodPut:
		if !edge.LocalOperator(r) {
			fail(http.StatusForbidden, "the capability list is saved only from the machine the hub runs on"+edge.ProxyNote(r))
			return
		}
		var in struct {
			YAML string `json:"yaml"`
		}
		dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&in); err != nil {
			fail(http.StatusBadRequest, "could not read that: "+err.Error())
			return
		}
		l, err := capability.Lint([]byte(in.YAML))
		if err != nil {
			fail(http.StatusBadRequest, err.Error())
			return
		}
		if cur, _ := p.capabilityList(); l.Version <= cur.Version {
			fail(http.StatusConflict, fmt.Sprintf("version is %d, and a save has to raise it above %d", l.Version, cur.Version))
			return
		}
		if err := st.SetHubSetting(SettingCapabilityList, in.YAML); err != nil {
			fail(http.StatusInternalServerError, "could not save the list: "+err.Error())
			return
		}
		p.RecordAudit("", "capability-list-set", fmt.Sprintf("version %d", l.Version))
		cur, _ := p.capabilityList()
		_ = json.NewEncoder(w).Encode(cur)
	default:
		w.Header().Set("Allow", "GET, PUT")
		fail(http.StatusMethodNotAllowed, "that has to be a GET or a PUT")
	}
}
