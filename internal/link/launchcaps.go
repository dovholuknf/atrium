package link

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// The launch cap, PER ROOM.
//
//	GET /_hub/launch-caps   {"default": 10, "rooms": {"claude-sg4": 10, "sg3": 5}}
//	PUT /_hub/launch-caps   the same shape, replacing what was there
//
// A ROOM IS A MACHINE, AND THE CAP IS ABOUT THE MACHINE. It used to be one
// number for every room the hub could see, so five workers on sg3 and five on
// sg4 refused a launch onto either, though neither machine was busy. Now a
// launch counts only the workers already on the room it is going to, against
// that room's own cap. `default` covers a room that is not listed, and when it
// is not set either, the old cap (ATRIUM_LAUNCH_CAP, else 10) does.
//
// Kept in the hub's settings table under one name, as JSON, like the other hub
// settings: a column per setting is a migration per setting.

// SettingLaunchCaps names the per-room caps in the hub's setting table.
const SettingLaunchCaps = "launch_caps"

// maxRoomCap bounds one room's cap, so a typo cannot quietly switch the
// backstop off.
const maxRoomCap = 100

// HubSettings is what the caps need from the hub's database. hubstore.Store
// has both methods.
type HubSettings interface {
	HubSetting(name string) (string, error)
	SetHubSetting(name, value string) error
}

// LaunchCaps is how many live workers each room may run.
type LaunchCaps struct {
	// Default is the cap for a room not in Rooms. Nil means launchCap().
	Default *int `json:"default,omitempty"`
	// Rooms is keyed by room name, matched the way every room name is.
	Rooms map[string]int `json:"rooms,omitempty"`
}

// For is the cap for one room.
func (lc LaunchCaps) For(room string) int {
	for name, n := range lc.Rooms {
		if room != "" && equalFold(name, room) {
			return n
		}
	}
	if lc.Default != nil {
		return *lc.Default
	}
	return launchCap()
}

func (lc LaunchCaps) check() error {
	if lc.Default != nil && (*lc.Default < 0 || *lc.Default > maxRoomCap) {
		return fmt.Errorf("the default cap has to be 0 to %d", maxRoomCap)
	}
	for name, n := range lc.Rooms {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("a room cap needs the room's name")
		}
		if n < 0 || n > maxRoomCap {
			return fmt.Errorf("the cap for %s has to be 0 to %d", name, maxRoomCap)
		}
	}
	return nil
}

// SetLaunchCaps wires the store the caps are kept in. Optional: without it
// every room gets launchCap().
func (p *Proxy) SetLaunchCaps(st HubSettings) {
	p.mu.Lock()
	p.capStore = st
	p.mu.Unlock()
}

// launchCaps reads the caps. A store that fails, or a value that will not
// parse, reads as no per-room caps at all, which is the old single cap: the
// same fail-sane rule the count follows, so a bad setting cannot stop every
// launch.
func (p *Proxy) launchCaps() LaunchCaps {
	p.mu.Lock()
	st := p.capStore
	p.mu.Unlock()
	if st == nil {
		return LaunchCaps{}
	}
	v, err := st.HubSetting(SettingLaunchCaps)
	if err != nil || strings.TrimSpace(v) == "" {
		return LaunchCaps{}
	}
	var lc LaunchCaps
	if json.Unmarshal([]byte(v), &lc) != nil || lc.check() != nil {
		return LaunchCaps{}
	}
	return lc
}

// serveLaunchCaps answers GET and PUT /_hub/launch-caps. Trusted the way the
// notify routes are: see the note at the top of notifyapi.go.
func (p *Proxy) serveLaunchCaps(w http.ResponseWriter, r *http.Request) {
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
		_ = json.NewEncoder(w).Encode(p.launchCapsView())
	case http.MethodPut:
		// SET FROM THE HUB'S MACHINE ONLY, like the notify command (f-024). A cap
		// is the backstop on how many workers a room may run, so raising it is
		// not something a board reached over an overlay decides. Reading stays open.
		if !loopbackRemote(r.RemoteAddr) {
			fail(http.StatusForbidden, "launch caps are set only from the machine the hub runs on")
			return
		}
		var lc LaunchCaps
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&lc); err != nil {
			fail(http.StatusBadRequest, "could not read that: "+err.Error())
			return
		}
		if err := lc.check(); err != nil {
			fail(http.StatusBadRequest, err.Error())
			return
		}
		b, _ := json.Marshal(lc)
		if err := st.SetHubSetting(SettingLaunchCaps, string(b)); err != nil {
			fail(http.StatusInternalServerError, "could not save the caps: "+err.Error())
			return
		}
		p.RecordAudit("", "launch-caps-set", string(b))
		_ = json.NewEncoder(w).Encode(p.launchCapsView())
	default:
		fail(http.StatusMethodNotAllowed, "that has to be a GET or a PUT")
	}
}

// launchCapsView is the caps as stored, with the default filled in, so a reader
// sees the number an unlisted room actually gets.
func (p *Proxy) launchCapsView() LaunchCaps {
	lc := p.launchCaps()
	d := lc.For("")
	lc.Default = &d
	if lc.Rooms == nil {
		lc.Rooms = map[string]int{}
	}
	return lc
}
