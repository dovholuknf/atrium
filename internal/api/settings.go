package api

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/shellpick"
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
		// A second address file, for callers running as another account.
		// Empty means only the per-user one, which is right when the daemon
		// and its callers are the same person.
		SettingSharedLocation: "shared_location",
		// WHICH SHELL A PANE OPENS. Empty means the one `shellpick` found.
		//
		// It was documented, exported by the config export, and settable
		// nowhere: the only routes were editing the database or importing a
		// config. That mattered because it was the WORKAROUND for the shell
		// always being cmd, so the escape hatch for the problem was itself
		// unreachable by anybody hitting the problem.
		SettingShellCommand: "shell_command",
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
	// And which shell an empty box comes out as, for the same reason. This one
	// is a search of PATH on the daemon's machine, so it is not something the
	// person reading the box could work out for themselves.
	out["shell_command_now"] = shellpick.Chosen()
	// What the board is wearing, and everything it could wear. The list is
	// sent rather than written into the page so the picker and the validator
	// cannot disagree: there is one list and the daemon holds it.
	out["board_skin"] = s.SkinOrDefault()
	out["board_skins"] = Skins
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
		// Where to publish the address for other accounts, and clearing it
		// back to nowhere is a request like the rest of these.
		SharedLocation *string `json:"shared_location"`
		// What the board wears. A pointer for the same reason: setting it back
		// to the one it shipped with is a thing somebody asks for.
		BoardSkin *string `json:"board_skin"`
	}
	// Read once and decoded twice: into the struct, which is what the handler
	// works from, and into a map, which is the only way to notice a field that
	// has no struct member. The guard further down needs the second one, and
	// the whole point of that guard is a field nobody has added yet.
	payload, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<16))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := json.Unmarshal(payload, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	raw := map[string]json.RawMessage{}
	// A body that decoded into the struct but not into a map is not possible,
	// so this cannot fail on its own. Ignored rather than reported, which keeps
	// one malformed body from producing two different errors depending on which
	// decode noticed first.
	_ = json.Unmarshal(payload, &raw)

	// AN EXPRESSION MAY BE STORED WHERE IT WAS TYPED, AND NOWHERE ELSE.
	//
	// The board compiles two operator-supplied functions with `new Function` to
	// group cards. That is safe for exactly one reason: `groupingPrefs()` reads
	// `localStorage`, so the code running in a browser was typed into that
	// browser by whoever was sitting at it, and somebody who can write it can
	// already open dev tools.
	//
	// Moving that storage here would change what it is. A daemon-side
	// expression is something one machine typed and another machine runs, which
	// is a stored XSS with a friendly name, and these functions run with full
	// page scope: `fetch` is in hand, and the board can read this machine's
	// filesystem through `/v1/browse`, every card's title, every worktree path
	// and the audit log. It stops being a grouping rule and becomes a
	// general-purpose exfiltration primitive the moment somebody other than the
	// operator can supply one.
	//
	// It is a reasonable thing to WANT: grouping is board-wide and today it is
	// lost when you open a different browser. So this is a refusal with a
	// reason rather than a silence, because the way this goes wrong is somebody
	// adding the obvious field, seeing it work, and shipping it. The answers
	// that do work are in `docs/backlog.md`: a restricted expression language, a
	// worker with no network, or a fixed menu for the shared case.
	//
	// Checked on the RAW body rather than a struct field, because the failure
	// being guarded is a field that does not exist yet.
	//
	// AHEAD OF EVERY WRITE BELOW, and that is not tidiness. A body carrying a
	// grouping expression beside a legitimate setting has to be refused whole:
	// applying half of it and reporting a failure is the worst of both, since
	// the caller reads an error and the machine has changed anyway.
	for _, banned := range []string{"group_by", "group_order"} {
		if _, ok := raw[banned]; ok {
			writeErr(w, http.StatusBadRequest, fmt.Errorf(
				"%s cannot be stored here. a grouping expression is compiled and run by whichever "+
					"browser loads the board, so keeping it daemon-side means one machine typing code "+
					"that another machine runs. it stays in localStorage. see docs/backlog.md, "+
					"\"the grouping expression\"", banned))
			return
		}
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

	if body.SharedLocation != nil {
		// Stored as typed, and it TAKES EFFECT ON THE NEXT START rather than
		// now. Writing the file from here would mean a daemon that publishes
		// its address to wherever the last save named, including a directory
		// somebody typed halfway. Restarting is a thing the operator does
		// anyway and the board says so beside the box.
		if err := s.st.SetSetting(SettingSharedLocation,
			strings.TrimSpace(*body.SharedLocation)); err != nil {
			s.fail(w, err)
			return
		}
	}

	if body.BoardSkin != nil {
		// Refused rather than stored, unlike the browse roots above, and the
		// two differ for a reason. A root that does not exist yet is a list
		// somebody is preparing, and it costs nothing to hold. An unknown skin
		// can never become right: it names a rule that is not in the
		// stylesheet, so the board would save it, wear it, and look exactly as
		// it did before.
		name := strings.TrimSpace(*body.BoardSkin)
		if !KnownSkin(name) {
			writeErr(w, http.StatusBadRequest, fmt.Errorf(
				"no skin called %q. the ones there are: %s", name, strings.Join(Skins, ", ")))
			return
		}
		if err := s.st.SetSetting(SettingBoardSkin, name); err != nil {
			s.fail(w, err)
			return
		}
	}

	out := globalAutoView(s)
	out["drained"] = drained
	writeJSON(w, http.StatusOK, out)
}
