package store

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/dovholuknf/atrium/internal/base/rules"
)

// The recogniser rows. The value, its matching and its filling are in internal/rules, which the hub shares.

const recogniserColumns = `id, label, enabled, rank, pattern, kind, title, tags, cwd,
	prompt, branch, window_name, theme, fetch_cmd, fetch_args, fetch_cwd, notes,
	last_error, failures, last_used_at, created_at, default_repo`

func scanRecogniser(sc interface{ Scan(...any) error }) (*rules.Recogniser, error) {
	var (
		r                 rules.Recogniser
		enabled           int
		args              string
		lastUsed, created string
	)
	if err := sc.Scan(&r.ID, &r.Label, &enabled, &r.Rank, &r.Pattern, &r.Kind, &r.Title,
		&r.Tags, &r.Cwd, &r.Prompt, &r.Branch, &r.Window, &r.Theme, &r.Fetch, &args,
		&r.FetchCwd, &r.Notes, &r.LastError, &r.Failures, &lastUsed, &created, &r.DefaultRepo); err != nil {
		return nil, err
	}
	r.Enabled = enabled != 0
	if err := json.Unmarshal([]byte(orDefault(args, "[]")), &r.FetchArgs); err != nil {
		return nil, err
	}
	if r.FetchArgs == nil {
		r.FetchArgs = []string{}
	}
	if lastUsed != "" {
		t, err := parseTS(lastUsed)
		if err != nil {
			return nil, err
		}
		r.LastUsedAt = &t
	}
	var err error
	if r.CreatedAt, err = parseTS(created); err != nil {
		return nil, err
	}
	return &r, nil
}

// Recognisers lists every row, in the order they are asked: most specific
// first, which is what rank means.
func (st *Store) Recognisers() ([]*rules.Recogniser, error) {
	var out []*rules.Recogniser
	err := st.guard(func() error {
		out = nil
		rows, err := st.db.Query(`SELECT ` + recogniserColumns +
			` FROM recogniser ORDER BY rank ASC, id ASC`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			r, err := scanRecogniser(rows)
			if err != nil {
				return err
			}
			out = append(out, r)
		}
		return rows.Err()
	})
	return out, err
}

// RecogniserByID returns one row.
func (st *Store) RecogniserByID(id string) (*rules.Recogniser, error) {
	var r *rules.Recogniser
	err := st.guard(func() error {
		row := st.db.QueryRow(`SELECT `+recogniserColumns+` FROM recogniser WHERE id = ?`, id)
		got, err := scanRecogniser(row)
		if err != nil {
			return err
		}
		r = got
		return nil
	})
	return r, err
}

// SaveRecogniser creates or replaces a row.
//
// THE PATTERN IS COMPILED HERE AND THE SAVE IS REFUSED IF IT DOES NOT. A bad
// regular expression that is only discovered when somebody pastes a URL is a
// feature that appears broken at the moment it is being relied on, and the
// person who typed the pattern is long gone. Refusing at save time puts the
// error in front of the person who can fix it.
//
// The failure bookkeeping is not writable, the same as a source: it is what
// atrium observed, and the operator does not get to type it.
func (st *Store) SaveRecogniser(r rules.Recogniser) (*rules.Recogniser, error) {
	r, err := rules.CheckRecogniser(r)
	if err != nil {
		return nil, err
	}
	args, err := json.Marshal(orEmptySlice(r.FetchArgs))
	if err != nil {
		return nil, err
	}

	err = st.guard(func() error {
		created := ts(st.now())
		wasEnabled := false
		row := st.db.QueryRow(`SELECT created_at, enabled FROM recogniser WHERE id = ?`, r.ID)
		var enabledNow int
		if err := row.Scan(&created, &enabledNow); err == nil {
			wasEnabled = enabledNow != 0
		}
		enabled := 0
		if r.Enabled {
			enabled = 1
		}
		// Turning one back on is the operator saying they fixed it.
		clear := r.Enabled && !wasEnabled
		_, err := st.db.Exec(`INSERT INTO recogniser
			(id, label, enabled, rank, pattern, kind, title, tags, cwd, prompt, branch,
			 window_name, theme, fetch_cmd, fetch_args, fetch_cwd, notes, created_at, default_repo)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
			ON CONFLICT(id) DO UPDATE SET
				label = excluded.label, enabled = excluded.enabled, rank = excluded.rank,
				pattern = excluded.pattern, kind = excluded.kind, title = excluded.title,
				tags = excluded.tags, cwd = excluded.cwd, prompt = excluded.prompt,
				branch = excluded.branch, window_name = excluded.window_name,
				theme = excluded.theme, fetch_cmd = excluded.fetch_cmd,
				fetch_args = excluded.fetch_args, fetch_cwd = excluded.fetch_cwd,
				notes = excluded.notes, default_repo = excluded.default_repo,
				failures   = CASE WHEN ? THEN 0  ELSE recogniser.failures   END,
				last_error = CASE WHEN ? THEN '' ELSE recogniser.last_error END`,
			r.ID, r.Label, enabled, r.Rank, r.Pattern, r.Kind, r.Title, r.Tags, r.Cwd,
			r.Prompt, r.Branch, r.Window, r.Theme, r.Fetch, string(args), r.FetchCwd,
			r.Notes, created, r.DefaultRepo, clear, clear)
		return err
	})
	if err != nil {
		return nil, err
	}
	return st.RecogniserByID(r.ID)
}

// DeleteRecogniser removes a row. The cards it filled in stay: they are work,
// and deleting the thing that recognised the URL does not make it not work.
func (st *Store) DeleteRecogniser(id string) error {
	return st.guard(func() error {
		_, err := st.db.Exec(`DELETE FROM recogniser WHERE id = ?`, id)
		return err
	})
}

// RecogniserFetched records what a fetch command did.
//
// A FETCH THAT FAILS NEVER SWITCHES THE ROW OFF, and this is deliberately
// unlike a source. A source is a timer nobody is watching, so a broken one
// retrying forever is a process spawned every fifteen minutes to produce an
// error into an empty room, and switching it off is a kindness. A recogniser is
// a verb somebody just typed, with the board open in front of them. Switching
// the row off would mean the NEXT paste silently matches nothing while they
// watch, which turns "gh was not logged in" into "atrium is broken".
//
// So the reason goes on the row where the settings screen shows it, the count
// says how long it has been going on, and the URL still resolves from its
// captures. Losing the fetched title is a worse card. Losing the card is worse
// than that.
func (st *Store) RecogniserFetched(id string, fetchErr error) error {
	n := ts(st.now())
	if fetchErr == nil {
		return st.guard(func() error {
			_, err := st.db.Exec(`UPDATE recogniser SET
				last_used_at = ?, last_error = '', failures = 0 WHERE id = ?`, n, id)
			return err
		})
	}
	reason := rules.FirstLine(fetchErr.Error())
	return st.guard(func() error {
		_, err := st.db.Exec(`UPDATE recogniser SET
			last_used_at = ?, last_error = ?, failures = failures + 1
			WHERE id = ?`, n, reason, id)
		return err
	})
}

// MatchRecogniser finds the first enabled row whose pattern matches, and
// returns it with the variables its captures produced.
//
// FIRST MATCH WINS, in rank order. That is the whole dispatch: the specific
// rows are asked before the generic ones, and the generic one at the bottom is
// a shrug rather than an error, because a URL on a known host that matches
// nothing is still worth opening a dialog for.
//
// The url is trimmed and nothing else. No scheme is added, no trailing slash is
// removed, nothing is lowercased. Every one of those would be atrium deciding
// what a URL means, which is the one thing it must not do: a pattern author who
// wants to accept a bare host writes `(?:https?://)?` and has said so.
func (st *Store) MatchRecogniser(url string) (*rules.Recogniser, map[string]string, error) {
	if strings.TrimSpace(url) == "" {
		return nil, nil, errors.New("no url to recognise")
	}
	rows, err := st.Recognisers()
	if err != nil {
		return nil, nil, err
	}
	return rules.MatchRecogniserIn(rows, url)
}
