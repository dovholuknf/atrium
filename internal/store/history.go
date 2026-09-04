package store

import "strings"

// Everything that has ever run here.
//
// The board answers "what wants my attention now". This answers "what have I
// had running", which is a different question and was unanswerable: archived
// cards were read by nothing, and every list query excludes them by design.
//
// The useful cut through it is which ones left an account of themselves. A
// session that ended with a recap said what it did. One that did not is either
// still worth writing up or was never worth starting, and both of those are
// worth being able to see. That cut is only meaningful because an agent can
// now say it finished; before that, no card had a recap and the column would
// have been empty.

// Recap filters for History.
const (
	// RecapAny is everything.
	RecapAny = ""
	// RecapWith is cards that left an account of themselves.
	RecapWith = "with"
	// RecapWithout is the ones that did not, which is the interesting half.
	RecapWithout = "without"
)

// HistoryQuery is what to show.
type HistoryQuery struct {
	// Recap is RecapAny, RecapWith or RecapWithout.
	Recap string
	// Search matches the title, the reason, the directory, the tags and the
	// recap. One box, because a person looking for "that thing about DNS"
	// does not know which field they are remembering.
	Search string
	// Limit and Offset page through it. This grows forever until somebody
	// turns pruning on, so it is paged from the start rather than after the
	// first machine that has been running for a year.
	Limit  int
	Offset int
}

// EverRun lists every card ever created, newest first, archived or not.
//
// Not called History, which is taken by the permission log and answers a
// different question: that one is "what have I been approving", this one is
// "what have I had running".
func (s *Store) EverRun(q HistoryQuery) ([]*Task, int, error) {
	where := []string{"1=1"}
	var args []any

	switch q.Recap {
	case RecapWith:
		where = append(where, "TRIM(recap) <> ''")
	case RecapWithout:
		where = append(where, "TRIM(recap) = ''")
	}

	if term := strings.TrimSpace(q.Search); term != "" {
		// LIKE rather than a full text index. The board is one person's and
		// this table is thousands of rows, not millions, and an FTS table is a
		// second thing to keep in step with the first.
		like := "%" + strings.ToLower(term) + "%"
		where = append(where, `(LOWER(title) LIKE ? OR LOWER(why) LIKE ?
			OR LOWER(worktree) LIKE ? OR LOWER(tags) LIKE ? OR LOWER(recap) LIKE ?
			OR LOWER(external_id) LIKE ?)`)
		args = append(args, like, like, like, like, like, like)
	}

	limit := q.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	clause := strings.Join(where, " AND ")

	var (
		out   []*Task
		total int
	)
	err := s.guard(func() error {
		out, total = nil, 0
		// The count first, so the view can say "showing 100 of 4,312" rather
		// than leaving somebody to work out whether there is more.
		if err := s.db.QueryRow(
			`SELECT COUNT(*) FROM task WHERE `+clause, args...).Scan(&total); err != nil {
			return err
		}
		rows, err := s.db.Query(`SELECT `+taskColumns+` FROM task WHERE `+clause+`
			ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?`,
			append(append([]any{}, args...), limit, q.Offset)...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			t, err := scanTask(rows)
			if err != nil {
				return err
			}
			out = append(out, t)
		}
		return rows.Err()
	})
	return out, total, err
}
