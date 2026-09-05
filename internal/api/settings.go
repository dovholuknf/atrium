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
		// Not a timer, but read the same way and for the same reason: empty is
		// a value here, and it means the open button is off.
		SettingEditor: "editor_command",
		// Where a pasted file lands, and what gets typed in front of its path.
		// Read as stored, because empty means something for each: the default
		// in one case, a bare path in the other.
		SettingPasteKeep:     "paste_keep",
		SettingPastePreamble: "paste_preamble",
		// How far back a terminal remembers, at both ends. Read as stored so
		// an empty box shows as empty and means the default, rather than
		// showing today's default as though somebody had chosen it.
		SettingScrollbackMB:    "scrollback_mb",
		SettingScrollbackLines: "scrollback_lines",
		// Where the directory picker may look. Empty means the default set,
		// which is the home directory plus every directory a card names.
		SettingBrowseRoots: "browse_roots",
	} {
		v, err := s.st.Setting(key)
		if err != nil {
			continue
		}
		out[field] = v
	}
	// Sent rather than written into the board, so the wording lives in one
	// place and an old tab cannot show a different default than the daemon
	// would use.
	out["paste_preamble_default"] = DefaultPastePreamble
	out["paste_preamble_none"] = PastePreambleNone
	// What is actually in force, whatever the two boxes say. An empty box
	// means the default, and the person reading it wants the number.
	//
	// `scrollback_lines_now` is also how the board sizes xterm: it asks the
	// daemon rather than carrying a number of its own, so the two buffers
	// cannot be configured to disagree by editing only one of them.
	out["scrollback_mb_now"] = scrollbackMB(s.st)
	out["scrollback_lines_now"] = scrollbackLines(s.st)
	out["scrollback_mb_max"] = maxScrollbackMB
	out["scrollback_lines_max"] = maxScrollbackLines
	// What the picker will actually use, resolved. An empty box means the
	// default set, and the person reading it wants to know what that came out
	// as rather than being told there is a default.
	out["browse_roots_now"] = s.browseRootsFor()
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
		// The command that opens a file, on the machine atrium is on. A
		// pointer for the same reason as the timers: not mentioning it and
		// clearing it are different requests, and clearing it is how the open
		// button gets turned back off.
		Editor *string `json:"editor_command"`
		// Whether a pasted file is kept in the card, and the words that go in
		// front of the path. Pointers, again because clearing one is a request.
		PasteKeep     *string `json:"paste_keep"`
		PastePreamble *string `json:"paste_preamble"`
		// How far back a terminal remembers. Strings rather than numbers,
		// because empty is a value and means the default, and a JSON number
		// has no way to say it.
		ScrollbackMB    *string `json:"scrollback_mb"`
		ScrollbackLines *string `json:"scrollback_lines"`
		// Where the picker may look. A pointer, because clearing it back to
		// the default set is a request.
		BrowseRoots *string `json:"browse_roots"`
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
	if body.Editor != nil {
		// Stored as typed, minus the surrounding space. Not validated against
		// anything on disk: a command that is not there fails at the moment
		// somebody presses open and says which program it could not run, which
		// is more use than refusing to save a line that would work tomorrow
		// when the tool is installed.
		if err := s.st.SetSetting(SettingEditor, strings.TrimSpace(*body.Editor)); err != nil {
			s.fail(w, err)
			return
		}
	}

	if body.PasteKeep != nil {
		// Two values and nothing else. Anything unrecognised would read as
		// `keep` at the one place that asks, which is a setting that looks
		// changed and is not.
		v := strings.ToLower(strings.TrimSpace(*body.PasteKeep))
		if v == "" {
			v = "keep"
		}
		if v != "keep" && v != "scrap" {
			writeErr(w, http.StatusBadRequest, fmt.Errorf(
				"a pasted file is either `keep` or `scrap`, not %q", v))
			return
		}
		if err := s.st.SetSetting(SettingPasteKeep, v); err != nil {
			s.fail(w, err)
			return
		}
	}
	if body.PastePreamble != nil {
		// Kept exactly as typed, trailing space and all. The space between the
		// words and the path is part of what somebody wrote, and trimming it
		// would join the two.
		if err := s.st.SetSetting(SettingPastePreamble, *body.PastePreamble); err != nil {
			s.fail(w, err)
			return
		}
	}

	// Checked before either is written, so a bad number in the second box does
	// not leave the first one changed.
	if body.ScrollbackMB != nil || body.ScrollbackLines != nil {
		var mb, lines string
		var err error
		if body.ScrollbackMB != nil {
			mb, err = checkNum("the daemon's scrollback, in megabytes", *body.ScrollbackMB, maxScrollbackMB)
			if err != nil {
				writeErr(w, http.StatusBadRequest, err)
				return
			}
		}
		if body.ScrollbackLines != nil {
			lines, err = checkNum("the terminal's scrollback, in lines", *body.ScrollbackLines, maxScrollbackLines)
			if err != nil {
				writeErr(w, http.StatusBadRequest, err)
				return
			}
		}
		if body.ScrollbackMB != nil {
			if err := s.st.SetSetting(SettingScrollbackMB, mb); err != nil {
				s.fail(w, err)
				return
			}
		}
		if body.ScrollbackLines != nil {
			if err := s.st.SetSetting(SettingScrollbackLines, lines); err != nil {
				s.fail(w, err)
				return
			}
		}
	}

	if body.BrowseRoots != nil {
		// Stored as typed. Nothing is validated here on purpose: a root that
		// does not exist is dropped when the set is resolved, and refusing one
		// at save time would stop somebody preparing a list for a drive that
		// is not plugged in.
		if err := s.st.SetSetting(SettingBrowseRoots, *body.BrowseRoots); err != nil {
			s.fail(w, err)
			return
		}
	}

	out := globalAutoView(s)
	out["drained"] = drained
	writeJSON(w, http.StatusOK, out)
}
