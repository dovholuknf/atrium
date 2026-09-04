package store

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// A source is a command on a timer whose stdout is intake items.
//
// The same shape as a harness, and the same reason. A harness row says how to
// start a runner without atrium knowing what claude is. A source row says how
// to find work without atrium knowing what GitHub is.
//
// Atrium holds an argv and an interval. `gh` holds the token, `mcp-gateway`
// holds four backends' worth of secrets, `zrok` holds an environment. There is
// nowhere in this struct to put a credential and that is the point, not an
// omission. See docs/intake-design.md, "How this must not be built".

// Source is one configured way of finding work.
type Source struct {
	ID      string   `json:"id"`
	Label   string   `json:"label"`
	Enabled bool     `json:"enabled"`
	Cmd     string   `json:"cmd"`
	Args    []string `json:"args"`
	Cwd     string   `json:"cwd"`
	// IntervalSecs is how often to run it.
	//
	// There is no good default and the document says so: a review request is
	// stale in an hour and a support queue somebody is paid to watch does not
	// want a second watcher at all. Fifteen minutes is a starting point, not
	// an answer.
	IntervalSecs int `json:"interval_secs"`
	// LastRunAt is when it last finished, successfully or not.
	LastRunAt *time.Time `json:"last_run_at,omitempty"`
	// LastError is what it said when it last broke, and empty when it did not.
	LastError string `json:"last_error"`
	// LastCount is how many items the last successful run produced. Zero is a
	// perfectly good answer and is not a failure.
	LastCount int `json:"last_count"`
	// Failures is how many times in a row it has broken. Reset by a run that
	// works.
	Failures  int       `json:"failures"`
	Notes     string    `json:"notes"`
	CreatedAt time.Time `json:"created_at"`
}

// MaxSourceFailures is how many consecutive failures switch a source off.
//
// A source retrying forever against a script somebody deleted is a daemon
// spawning a process every fifteen minutes to produce the same error nobody is
// reading. Three, because one is a blip and two is a coincidence.
const MaxSourceFailures = 3

// MinSourceInterval is the floor on how often a source may run.
//
// Not a safety limit, a sanity one: a source is a child process, and one every
// few seconds is a fork bomb with a settings screen.
const MinSourceInterval = 30

const sourceColumns = `id, label, enabled, cmd, args, cwd, interval_secs,
	last_run_at, last_error, last_count, failures, notes, created_at`

func scanSource(sc interface{ Scan(...any) error }) (*Source, error) {
	var (
		s                Source
		args             string
		enabled          int
		lastRun, created string
	)
	if err := sc.Scan(&s.ID, &s.Label, &enabled, &s.Cmd, &args, &s.Cwd,
		&s.IntervalSecs, &lastRun, &s.LastError, &s.LastCount, &s.Failures,
		&s.Notes, &created); err != nil {
		return nil, err
	}
	s.Enabled = enabled != 0
	if err := json.Unmarshal([]byte(orDefault(args, "[]")), &s.Args); err != nil {
		return nil, err
	}
	if s.Args == nil {
		s.Args = []string{}
	}
	if lastRun != "" {
		t, err := parseTS(lastRun)
		if err != nil {
			return nil, err
		}
		s.LastRunAt = &t
	}
	var err error
	if s.CreatedAt, err = parseTS(created); err != nil {
		return nil, err
	}
	return &s, nil
}

// Sources lists every configured source.
func (st *Store) Sources() ([]*Source, error) {
	var out []*Source
	err := st.guard(func() error {
		out = nil
		rows, err := st.db.Query(`SELECT ` + sourceColumns + ` FROM source ORDER BY id ASC`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			s, err := scanSource(rows)
			if err != nil {
				return err
			}
			out = append(out, s)
		}
		return rows.Err()
	})
	return out, err
}

// SourceByID returns one source.
func (st *Store) SourceByID(id string) (*Source, error) {
	var s *Source
	err := st.guard(func() error {
		row := st.db.QueryRow(`SELECT `+sourceColumns+` FROM source WHERE id = ?`, id)
		got, err := scanSource(row)
		if err != nil {
			return err
		}
		s = got
		return nil
	})
	return s, err
}

// SaveSource creates or replaces a source.
//
// The failure bookkeeping is not writable here. It is what atrium observed and
// the operator does not get to type it, which is the same observed-versus-
// overrides rule the cards follow. Enabling a source that had been switched
// off for failing clears the count, because that is what enabling it means.
func (st *Store) SaveSource(s Source) (*Source, error) {
	s.ID = strings.TrimSpace(s.ID)
	if s.ID == "" {
		return nil, errors.New("a source needs an id")
	}
	if strings.TrimSpace(s.Cmd) == "" {
		return nil, errors.New("a source needs a command to run")
	}
	if s.Label == "" {
		s.Label = s.ID
	}
	if s.IntervalSecs < MinSourceInterval {
		s.IntervalSecs = MinSourceInterval
	}
	args, err := json.Marshal(orEmptySlice(s.Args))
	if err != nil {
		return nil, err
	}

	err = st.guard(func() error {
		created := ts(now())
		wasEnabled := false
		row := st.db.QueryRow(`SELECT created_at, enabled FROM source WHERE id = ?`, s.ID)
		var enabledNow int
		if err := row.Scan(&created, &enabledNow); err == nil {
			wasEnabled = enabledNow != 0
		}

		enabled := 0
		if s.Enabled {
			enabled = 1
		}
		// Turning a source back on is the operator saying they fixed it.
		clear := s.Enabled && !wasEnabled
		_, err := st.db.Exec(`INSERT INTO source
			(id, label, enabled, cmd, args, cwd, interval_secs, notes, created_at)
			VALUES (?,?,?,?,?,?,?,?,?)
			ON CONFLICT(id) DO UPDATE SET
				label = excluded.label, enabled = excluded.enabled, cmd = excluded.cmd,
				args = excluded.args, cwd = excluded.cwd,
				interval_secs = excluded.interval_secs, notes = excluded.notes,
				failures   = CASE WHEN ? THEN 0  ELSE source.failures   END,
				last_error = CASE WHEN ? THEN '' ELSE source.last_error END`,
			s.ID, s.Label, enabled, s.Cmd, string(args), s.Cwd,
			s.IntervalSecs, s.Notes, created, clear, clear)
		return err
	})
	if err != nil {
		return nil, err
	}
	return st.SourceByID(s.ID)
}

// DeleteSource removes a source. The cards it raised stay: they are work, and
// deleting the thing that found it does not make it not work.
func (st *Store) DeleteSource(id string) error {
	return st.guard(func() error {
		_, err := st.db.Exec(`DELETE FROM source WHERE id = ?`, id)
		return err
	})
}

// SourceRan records the outcome of one run.
//
// A run that worked clears the error and the failure count. A run that broke
// increments, and the one that reaches MaxSourceFailures switches the source
// off with the reason still attached.
//
// Returns whether this call is what switched it off, so the caller can say so
// once rather than on every tick afterwards.
func (st *Store) SourceRan(id string, count int, runErr error) (disabled bool, err error) {
	n := ts(now())
	if runErr == nil {
		return false, st.guard(func() error {
			_, err := st.db.Exec(`UPDATE source SET
				last_run_at = ?, last_error = '', last_count = ?, failures = 0
				WHERE id = ?`, n, count, id)
			return err
		})
	}

	reason := firstLineOf(runErr.Error())
	err = st.guard(func() error {
		disabled = false
		res := st.db.QueryRow(`SELECT failures FROM source WHERE id = ?`, id)
		var failures int
		if err := res.Scan(&failures); err != nil {
			return err
		}
		failures++
		off := failures >= MaxSourceFailures
		enabled := 1
		if off {
			enabled = 0
		}
		if _, err := st.db.Exec(`UPDATE source SET
			last_run_at = ?, last_error = ?, failures = ?, enabled = ?
			WHERE id = ?`, n, reason, failures, enabled, id); err != nil {
			return err
		}
		disabled = off
		return nil
	})
	return disabled, err
}

// DueSources returns the enabled sources whose interval has elapsed.
func (st *Store) DueSources(at time.Time) ([]*Source, error) {
	all, err := st.Sources()
	if err != nil {
		return nil, err
	}
	var out []*Source
	for _, s := range all {
		if !s.Enabled {
			continue
		}
		if s.LastRunAt == nil {
			out = append(out, s)
			continue
		}
		if at.Sub(*s.LastRunAt) >= time.Duration(s.IntervalSecs)*time.Second {
			out = append(out, s)
		}
	}
	return out, nil
}

// firstLineOf keeps a source's failure to one readable line on a row.
func firstLineOf(s string) string {
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimSpace(s)
	if len(s) > 300 {
		s = s[:300] + "..."
	}
	return s
}
