// Package state reads (never writes) the gwt session ledger and state log.
//
// Two data sources:
//   - <WORKTREE_ROOT>\sessions\*.json : one JSON per registered session, written by the gwt SessionStart hook
//   - <WORKTREE_ROOT>\watch\state.log : append-only event log written by the set-session-state hook on every
//                                        thinking/idle/needs-input transition
package state

import (
	"bufio"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Session mirrors the relevant fields of a gwt session JSON. Fields the gwt hooks
// may not always set are pointers so we can distinguish "absent" from "zero value".
type Session struct {
	ID              string `json:"id"`
	ClaudeSessionID string `json:"claude_session_id,omitempty"`
	Branch          string `json:"branch"`
	Label           string `json:"label,omitempty"`
	Repo            string `json:"repo,omitempty"`
	WorktreePath    string `json:"worktree_path"`
	WindowName      string `json:"window_name,omitempty"`
	PID             int    `json:"pid"`
	Saved           bool   `json:"saved"`
	State           string `json:"state,omitempty"`
	LastStateChange string `json:"last_state_change,omitempty"`
	LastSpawnedAt   string `json:"last_spawned_at,omitempty"`
	WTSession       string `json:"wt_session,omitempty"`

	// SourceFile is the absolute path of the JSON the entry came from.
	SourceFile string `json:"-"`
}

// rawSession matches the on-disk schema (PascalCase) verbatim. We convert to Session
// in ReadAll so the public type uses snake_case (better for MCP / JSON output).
type rawSession struct {
	ID              string `json:"Id"`
	ClaudeSessionID string `json:"ClaudeSessionId"`
	Branch          string `json:"Branch"`
	Label           string `json:"Label"`
	Repo            string `json:"Repo"`
	WorktreePath    string `json:"WorktreePath"`
	WindowName      string `json:"WindowName"`
	PID             int    `json:"Pid"`
	Saved           bool   `json:"Saved"`
	State           string `json:"State"`
	LastStateChange string `json:"LastStateChange"`
	LastSpawnedAt   string `json:"LastSpawnedAt"`
	WTSession       string `json:"WtSession"`
}

// Root returns the worktree root that Atrium reads from, taken from
// WORKTREE_ROOT. There is no default: one machine's layout is not a sensible
// fallback for another, and an empty root makes a missing setting obvious
// rather than sending reads somewhere that happens to exist.
func Root() string {
	return strings.TrimRight(os.Getenv("WORKTREE_ROOT"), `\/`)
}

// SessionDir returns the directory containing session JSONs.
func SessionDir() string { return filepath.Join(Root(), "sessions") }

// WatchDir returns the directory containing the state log.
func WatchDir() string { return filepath.Join(Root(), "watch") }

// StateLogPath is the path to the append-only state.log written by the hooks.
func StateLogPath() string { return filepath.Join(WatchDir(), "state.log") }

// ReadAll returns every session entry currently on disk. Best-effort: files that
// fail to parse are skipped silently (the writer is async and may catch a half-
// written file). Returns a stable order: alive entries first, then by branch.
func ReadAll() ([]Session, error) {
	dir := SessionDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]Session, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		full := filepath.Join(dir, e.Name())
		s, err := readOne(full)
		if err != nil {
			continue // skip malformed; not Atrium's job to repair
		}
		out = append(out, s)
	}
	sort.SliceStable(out, func(i, j int) bool {
		ai, aj := isAlive(out[i]), isAlive(out[j])
		if ai != aj {
			return ai
		}
		return out[i].Branch < out[j].Branch
	})
	return out, nil
}

func readOne(path string) (Session, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Session{}, err
	}
	var r rawSession
	if err := json.Unmarshal(b, &r); err != nil {
		return Session{}, err
	}
	return Session{
		ID:              r.ID,
		ClaudeSessionID: r.ClaudeSessionID,
		Branch:          r.Branch,
		Label:           r.Label,
		Repo:            r.Repo,
		WorktreePath:    r.WorktreePath,
		WindowName:      r.WindowName,
		PID:             r.PID,
		Saved:           r.Saved,
		State:           defaultState(r.State),
		LastStateChange: r.LastStateChange,
		LastSpawnedAt:   r.LastSpawnedAt,
		WTSession:       r.WTSession,
		SourceFile:      path,
	}, nil
}

func defaultState(s string) string {
	if s == "" {
		return "idle"
	}
	return s
}

// Event is one parsed line from state.log.
type Event struct {
	Timestamp    time.Time
	State        string
	Branch       string
	WorktreePath string
	Raw          string
}

// TailEvents reads the entire state.log and returns parsed Events. Cheap (the log
// is small for typical use, and rotation should keep it that way).
func TailEvents() ([]Event, error) {
	f, err := os.Open(StateLogPath())
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	var out []Event
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		if ev, ok := parseEvent(sc.Text()); ok {
			out = append(out, ev)
		}
	}
	if err := sc.Err(); err != nil {
		return out, err
	}
	return out, nil
}

// parseEvent parses a single line in the form
//   <iso-ts>  <state>  <branch>  @ <path>
// returning (zero, false) on a malformed line.
func parseEvent(line string) (Event, bool) {
	at := strings.LastIndex(line, " @ ")
	if at < 0 {
		return Event{}, false
	}
	left := line[:at]
	path := strings.TrimSpace(line[at+3:])
	// left is "<ts><spaces><state><spaces><branch>". Split on runs of two-or-more spaces.
	fields := splitDouble(left)
	if len(fields) < 3 {
		return Event{}, false
	}
	ts, err := time.Parse(time.RFC3339Nano, fields[0])
	if err != nil {
		return Event{}, false
	}
	return Event{
		Timestamp:    ts,
		State:        fields[1],
		Branch:       strings.Join(fields[2:], " "), // branch may have spaces; join trailing
		WorktreePath: path,
		Raw:          line,
	}, true
}

func splitDouble(s string) []string {
	var out []string
	var cur strings.Builder
	spaceRun := 0
	for _, r := range s {
		if r == ' ' {
			spaceRun++
			if spaceRun >= 2 && cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
			continue
		}
		spaceRun = 0
		cur.WriteRune(r)
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

func isAlive(s Session) bool { return s.PID > 0 }
