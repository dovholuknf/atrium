package store

import (
	"time"
)

// Token use on record: what every card's runner spent, turn by turn, kept for
// history. See 0063_session_usage. The daemon reads the transcripts and decides
// what caused a turn. This file only keeps the rows.

// Usage causes: what started a turn, where atrium can tell.
const (
	// UsageOperator is a prompt the operator typed or sent from the board.
	UsageOperator = "operator"
	// UsageSay is another session's message, typed in or carried by a hook.
	UsageSay = "say"
	// UsageRestartWake is the after-restart or after-exit wake atrium types.
	UsageRestartWake = "restart-wake"
	// UsageKeepalive is a keep-alive refresh fork. Not in the card's
	// transcript: the row comes from the fork's receipt.
	UsageKeepalive = "keepalive"
	// UsageResume is the first turn of a resumed runner that nothing else
	// claims.
	UsageResume = "resume"
	// UsageUnknown is a turn with no prompt atrium saw: a subagent's report
	// waking the session, or a hook older than the prompt event.
	UsageUnknown = "unknown"
	// UsageSubagent is what the card's Claude Code subagents (the Task tool)
	// spent. Never folded into the turn that started them. An atrium worker is
	// a card of its own and is not this.
	UsageSubagent = "subagent"
)

// SessionUsage is one turn's spend, or one refresh's.
type SessionUsage struct {
	ID       string    `json:"id"`
	TaskID   string    `json:"task_id"`
	ResumeID string    `json:"resume_id"`
	Started  time.Time `json:"started_at"`
	Ended    time.Time `json:"ended_at"`
	Cause    string    `json:"cause"`
	// AfterResume is the first turn of a runner started on a resume, whatever
	// started it. The one to read to learn whether a resume missed the cache.
	AfterResume  bool    `json:"after_resume,omitempty"`
	Model        string  `json:"model"`
	Replies      int     `json:"replies"`
	Input        int64   `json:"input"`
	Output       int64   `json:"output"`
	CacheWrite5m int64   `json:"cache_write_5m"`
	CacheWrite1h int64   `json:"cache_write_1h"`
	CacheRead    int64   `json:"cache_read"`
	Context      int64   `json:"context"`
	LastMessage  string  `json:"last_message,omitempty"`
	Cost         float64 `json:"cost"`
	Prices       string  `json:"prices,omitempty"`
}

// UsageTotals is a card's spend summed.
type UsageTotals struct {
	Rows         int     `json:"rows"`
	Replies      int     `json:"replies"`
	Input        int64   `json:"input"`
	Output       int64   `json:"output"`
	CacheWrite5m int64   `json:"cache_write_5m"`
	CacheWrite1h int64   `json:"cache_write_1h"`
	CacheRead    int64   `json:"cache_read"`
	Cost         float64 `json:"cost"`
}

// AddSessionUsage writes one row. The id is filled in when empty.
func (s *Store) AddSessionUsage(u *SessionUsage) error {
	if u.ID == "" {
		u.ID = newID()
	}
	if u.Ended.IsZero() {
		u.Ended = now()
	}
	if u.Started.IsZero() {
		u.Started = u.Ended
	}
	if u.Cause == "" {
		u.Cause = UsageUnknown
	}
	return s.guard(func() error {
		_, err := s.db.Exec(`INSERT INTO session_usage (id, task_id, resume_id, started_at, ended_at, cause,
			after_resume, model, replies, input, output, cache_write_5m, cache_write_1h, cache_read, context,
			last_message, cost, prices) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			u.ID, u.TaskID, u.ResumeID, ts(u.Started), ts(u.Ended), u.Cause, u.AfterResume, u.Model, u.Replies,
			u.Input, u.Output, u.CacheWrite5m, u.CacheWrite1h, u.CacheRead, u.Context, u.LastMessage, u.Cost,
			u.Prices)
		return err
	})
}

const usageColumns = `id, task_id, resume_id, started_at, ended_at, cause, after_resume, model, replies,
	input, output, cache_write_5m, cache_write_1h, cache_read, context, last_message, cost, prices`

func scanUsage(r rowScanner) (*SessionUsage, error) {
	var (
		u              SessionUsage
		started, ended string
	)
	if err := r.Scan(&u.ID, &u.TaskID, &u.ResumeID, &started, &ended, &u.Cause, &u.AfterResume, &u.Model,
		&u.Replies, &u.Input, &u.Output, &u.CacheWrite5m, &u.CacheWrite1h, &u.CacheRead, &u.Context,
		&u.LastMessage, &u.Cost, &u.Prices); err != nil {
		return nil, err
	}
	var err error
	if u.Started, err = parseTS(started); err != nil {
		return nil, err
	}
	if u.Ended, err = parseTS(ended); err != nil {
		return nil, err
	}
	return &u, nil
}

// SessionUsageOf lists a card's newest rows, newest first, at most limit.
func (s *Store) SessionUsageOf(taskID string, limit int) ([]*SessionUsage, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	var out []*SessionUsage
	err := s.guard(func() error {
		out = nil
		rows, err := s.db.Query(`SELECT `+usageColumns+` FROM session_usage WHERE task_id = ?
			ORDER BY ended_at DESC, id DESC LIMIT ?`, taskID, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			u, err := scanUsage(rows)
			if err != nil {
				return err
			}
			out = append(out, u)
		}
		return rows.Err()
	})
	return out, err
}

// SessionUsageTotals sums a card's rows, all of them and by cause.
func (s *Store) SessionUsageTotals(taskID string) (*UsageTotals, map[string]*UsageTotals, error) {
	var (
		all     *UsageTotals
		byCause map[string]*UsageTotals
	)
	err := s.guard(func() error {
		all, byCause = &UsageTotals{}, map[string]*UsageTotals{}
		rows, err := s.db.Query(`SELECT cause, COUNT(*), COALESCE(SUM(replies), 0), COALESCE(SUM(input), 0),
			COALESCE(SUM(output), 0), COALESCE(SUM(cache_write_5m), 0), COALESCE(SUM(cache_write_1h), 0),
			COALESCE(SUM(cache_read), 0), COALESCE(SUM(cost), 0)
			FROM session_usage WHERE task_id = ? GROUP BY cause`, taskID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var (
				cause string
				t     UsageTotals
			)
			if err := rows.Scan(&cause, &t.Rows, &t.Replies, &t.Input, &t.Output, &t.CacheWrite5m,
				&t.CacheWrite1h, &t.CacheRead, &t.Cost); err != nil {
				return err
			}
			byCause[cause] = &t
			all.Rows += t.Rows
			all.Replies += t.Replies
			all.Input += t.Input
			all.Output += t.Output
			all.CacheWrite5m += t.CacheWrite5m
			all.CacheWrite1h += t.CacheWrite1h
			all.CacheRead += t.CacheRead
			all.Cost += t.Cost
		}
		return rows.Err()
	})
	return all, byCause, err
}

// LastTranscriptUsage is a session's newest row read from its main transcript,
// or nil. Keep-alive rows are not from the transcript and subagent rows are
// from their own files, so both are left out. The daemon starts its first read
// after a restart from here, so nothing is counted twice.
func (s *Store) LastTranscriptUsage(taskID, resumeID string) (*SessionUsage, error) {
	return s.lastUsage(`cause NOT IN (?, ?)`, taskID, resumeID, UsageKeepalive, UsageSubagent)
}

// LastSubagentUsage is a session's newest subagent row, or nil. The subagent
// read after a restart starts from here.
func (s *Store) LastSubagentUsage(taskID, resumeID string) (*SessionUsage, error) {
	return s.lastUsage(`cause = ?`, taskID, resumeID, UsageSubagent)
}

func (s *Store) lastUsage(where, taskID, resumeID string, causes ...any) (*SessionUsage, error) {
	var out *SessionUsage
	args := append([]any{taskID, resumeID}, causes...)
	err := s.guard(func() error {
		out = nil
		rows, err := s.db.Query(`SELECT `+usageColumns+` FROM session_usage
			WHERE task_id = ? AND resume_id = ? AND `+where+`
			ORDER BY ended_at DESC, id DESC LIMIT 1`, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		if rows.Next() {
			if out, err = scanUsage(rows); err != nil {
				return err
			}
		}
		return rows.Err()
	})
	return out, err
}
