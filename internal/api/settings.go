package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// Settings that belong to the board rather than to any one card.

// maxAutoMinutes bounds how long auto mode can be turned on for.
//
// A day. Not a safety limit, a sanity one: "for the next hour" is the shape
// this is for, and a deadline in three weeks is a switch left on with extra
// steps. Turning it on with no deadline is still available and still says what
// it is, which is better than a very long deadline pretending otherwise.
const maxAutoMinutes = 24 * 60

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, globalAutoView(s))
}

// globalAutoView is the switch and its deadline, in the one shape every caller
// reads. Built in one place so the GET and the POST cannot answer differently.
func globalAutoView(s *Server) map[string]any {
	on, until := s.st.GlobalAutoUntil()
	out := map[string]any{"global_auto": on}
	if until != nil {
		out["global_auto_until"] = until.Format(time.RFC3339)
		// Seconds left, so the board does not have to agree with the daemon
		// about what time it is. A clock skew of a few minutes between the
		// browser and the machine is ordinary, and it would show as a switch
		// that expired in the future.
		if left := time.Until(*until); left > 0 {
			out["global_auto_seconds"] = int64(left.Seconds())
		}
	}
	// The two housekeeping timers, as whatever is stored. Read as strings
	// rather than parsed, because `off` is a value and so is a number of
	// seconds, and the board has to be able to show which one it is.
	for key, field := range map[string]string{
		store.SettingSweepDead:  "sweep_dead_after",
		store.SettingPruneAfter: "prune_after",
	} {
		v, err := s.st.Setting(key)
		if err != nil {
			continue
		}
		out[field] = v
	}
	return out
}

// housekeeping writes one of the two timers.
//
// Validated here rather than trusted, because one of them deletes things. A
// value that is neither `off`, empty, nor a positive number of seconds is
// refused rather than stored and quietly ignored by the reader, which is how a
// setting ends up looking on and doing nothing.
func housekeeping(s *Server, key, value string) error {
	value = strings.TrimSpace(value)
	switch value {
	case "", "off":
		return s.st.SetSetting(key, "off")
	}
	secs, err := strconv.Atoi(value)
	if err != nil || secs <= 0 {
		return fmt.Errorf("%s takes a number of seconds or `off`, not %q", key, value)
	}
	return s.st.SetSetting(key, value)
}

// setSettings takes whichever settings are present in the body and leaves the
// rest alone, so a caller that knows about one does not have to send them all.
func (s *Server) setSettings(w http.ResponseWriter, r *http.Request) {
	var body struct {
		GlobalAuto *bool `json:"global_auto"`
		// Minutes is how long to leave it on. Absent or zero means no
		// deadline, which is what this switch did before and still the right
		// answer for somebody who means it.
		Minutes int `json:"global_auto_minutes"`
		// The two housekeeping timers, as strings so that `off` and a number
		// of seconds are the same field. Pointers, because not mentioning one
		// and setting it to `off` are different requests.
		SweepDead  *string `json:"sweep_dead_after"`
		PruneAfter *string `json:"prune_after"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if body.Minutes < 0 || body.Minutes > maxAutoMinutes {
		writeErr(w, http.StatusBadRequest, fmt.Errorf(
			"auto mode can be left on for up to %d minutes, or with no deadline at all",
			maxAutoMinutes))
		return
	}
	drained := 0
	if body.GlobalAuto != nil {
		var until *time.Time
		if *body.GlobalAuto && body.Minutes > 0 {
			t := time.Now().UTC().Add(time.Duration(body.Minutes) * time.Minute)
			until = &t
		}
		if err := s.st.SetGlobalAutoUntil(*body.GlobalAuto, until); err != nil {
			s.fail(w, err)
			return
		}
		// Turning it on empties the queue it was turned on because of. The
		// chain only runs when a request arrives, so anything already waiting
		// had asked before the switch existed and would sit there under a
		// header saying nothing will stop to ask.
		if *body.GlobalAuto && s.DrainAuto != nil {
			n, err := s.DrainAuto()
			if err != nil {
				// Not fatal. The setting is written and every later request is
				// approved, so reporting a failure here would undersell what
				// did happen.
				log.Printf("[atrium] could not drain the queue: %v", err)
			}
			drained = n
		}
		// Every open board, so a switch this broad is never on in one tab and
		// off in another.
		s.Broadcast("settings", globalAutoView(s))
	}
	for key, value := range map[string]*string{
		store.SettingSweepDead:  body.SweepDead,
		store.SettingPruneAfter: body.PruneAfter,
	} {
		if value == nil {
			continue
		}
		if err := housekeeping(s, key, *value); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
	}

	out := globalAutoView(s)
	out["drained"] = drained
	writeJSON(w, http.StatusOK, out)
}
