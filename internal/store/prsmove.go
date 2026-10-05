package store

import (
	"database/sql"
	"errors"
	"strings"
)

// A review moves with its PR. These are the store's parts of that: the row's fields as they travel, the lookup by the
// canonical key, the insert on the room that receives it, and the archive on the room that gave it up. The run folder
// travels as files and is the api package's part. See docs/rnd/scm-forge-design.md Built.

// PRTransfer is a row's fields as they cross from one room to another. The id, the run folder, the walker and the
// claim are left out: the receiving room makes its own, and the walker is the new room's card.
type PRTransfer struct {
	URL            string  `json:"url"`
	Why            string  `json:"why"`
	Host           string  `json:"host"`
	Org            string  `json:"org"`
	Repo           string  `json:"repo"`
	Number         int     `json:"number"`
	Head           string  `json:"reviewed_head"`
	ObservedHead   string  `json:"observed_head"`
	ObservedState  string  `json:"observed_state"`
	ObservedTitle  string  `json:"observed_title"`
	ObservedAuthor string  `json:"observed_author"`
	CheckedAt      string  `json:"checked_at"`
	CheckError     string  `json:"check_error"`
	State          string  `json:"state"`
	RunState       string  `json:"run_state"`
	RunError       string  `json:"run_error"`
	CostUSD        float64 `json:"cost_usd"`
	StartedAt      string  `json:"started_at"`
	ReadyAt        string  `json:"ready_at"`
	SecondState    string  `json:"second_state"`
	SecondSummary  string  `json:"second_summary"`
	SecondError    string  `json:"second_error"`
	CreatedAt      string  `json:"created_at"`
}

// PRByKey is the live row for a pull request, the newest that is not archived, in any state. Nil when there is none.
// The key is PRKey's, so the case of host, org and repo does not matter.
func (st *Store) PRByKey(host, org, repo string, number int) (*PRReview, error) {
	var out *PRReview
	err := st.guard(func() error {
		out = nil
		got, err := scanPR(st.db.QueryRow(`SELECT `+prColumns+` FROM pr_review
			WHERE lower(host) = ? AND lower(org) = ? AND lower(repo) = ? AND number = ? AND archived_at = ''
			ORDER BY created_at DESC, id DESC LIMIT 1`,
			strings.ToLower(host), strings.ToLower(org), strings.ToLower(repo), number))
		if err == sql.ErrNoRows {
			return nil
		}
		if err != nil {
			return err
		}
		out = got
		return nil
	})
	return out, err
}

// PRTransferOf reads the fields of a row that travel.
func (st *Store) PRTransferOf(id string) (PRTransfer, error) {
	var t PRTransfer
	err := st.guard(func() error {
		return st.db.QueryRow(`SELECT url, why, host, org, repo, number, reviewed_head, observed_head, observed_state,
			observed_title, observed_author, checked_at, check_error, state, run_state, run_error, cost_usd,
			started_at, ready_at, second_state, second_summary, second_error, created_at
			FROM pr_review WHERE id = ?`, id).Scan(&t.URL, &t.Why, &t.Host, &t.Org, &t.Repo, &t.Number, &t.Head,
			&t.ObservedHead, &t.ObservedState, &t.ObservedTitle, &t.ObservedAuthor, &t.CheckedAt, &t.CheckError,
			&t.State, &t.RunState, &t.RunError, &t.CostUSD, &t.StartedAt, &t.ReadyAt, &t.SecondState,
			&t.SecondSummary, &t.SecondError, &t.CreatedAt)
	})
	return t, err
}

// PRRunDirTaken says whether any row, archived or not, already has this run folder.
func (st *Store) PRRunDirTaken(runDir string) (bool, error) {
	var n int
	err := st.guard(func() error {
		return st.db.QueryRow(`SELECT COUNT(*) FROM pr_review WHERE run_dir = ?`, runDir).Scan(&n)
	})
	return n > 0, err
}

// ImportPR makes a row from another room's, with a fresh id, this room's run folder, no walker and a claim of
// 'claimed'. A row that was in flight arrives queued with nothing observed of its run, for the caller to start: the
// runner resumes from the folder's steps. Any other state arrives as it was.
func (st *Store) ImportPR(t PRTransfer, runDir string) (*PRReview, error) {
	if strings.TrimSpace(t.Org) == "" || strings.TrimSpace(t.Repo) == "" || t.Number <= 0 {
		return nil, errors.New("a review needs an org, a repo and a number")
	}
	if strings.TrimSpace(runDir) == "" {
		return nil, errors.New("a review needs a run folder")
	}
	if !ValidPRHead(t.Head) || !ValidPRHead(t.ObservedHead) {
		return nil, errors.New("head is not a commit hash")
	}
	if !validPRState(t.State) {
		return nil, errors.New("that is not a review state")
	}
	switch t.ObservedState {
	case "", "open", "merged", "closed":
	default:
		return nil, errors.New("that is not an observed state")
	}
	switch t.SecondState {
	case "none", "pending", "done", "failed":
	case "":
		t.SecondState = "none"
	default:
		return nil, errors.New("that is not a second opinion state")
	}
	if len(t.Why) > MaxPRWhy {
		return nil, errors.New("why is too long")
	}
	switch t.State {
	case PRQueued, PRFetching, PRRunning:
		t.State, t.RunState, t.RunError, t.StartedAt, t.ReadyAt = PRQueued, "", "", "", ""
	}
	if t.State != PRFailed {
		t.RunError = ""
	}
	if t.CreatedAt == "" {
		t.CreatedAt = ts(now())
	}
	id := newID()
	err := st.guard(func() error {
		_, err := st.db.Exec(`INSERT INTO pr_review
			(id, run_dir, url, why, host, org, repo, number, reviewed_head, observed_head, observed_state,
			 observed_title, observed_author, checked_at, check_error, state, run_state, run_error, cost_usd,
			 started_at, ready_at, second_state, second_summary, second_error, created_at, claim)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,'claimed')`,
			id, runDir, strings.TrimSpace(t.URL), t.Why, t.Host, t.Org, t.Repo, t.Number,
			strings.ToLower(t.Head), strings.ToLower(t.ObservedHead), t.ObservedState, t.ObservedTitle,
			t.ObservedAuthor, t.CheckedAt, t.CheckError, t.State, t.RunState, t.RunError, t.CostUSD, t.StartedAt,
			t.ReadyAt, t.SecondState, t.SecondSummary, t.SecondError, t.CreatedAt)
		return err
	})
	if err != nil {
		return nil, err
	}
	return st.PRByID(id)
}

// ArchivePR hides a row from the index. The run folder is not touched. Archiving an archived row keeps its first
// time.
func (st *Store) ArchivePR(id string) (*PRReview, error) {
	err := st.guard(func() error {
		res, err := st.db.Exec(`UPDATE pr_review SET archived_at = CASE WHEN archived_at = '' THEN ? ELSE archived_at END
			WHERE id = ?`, ts(now()), id)
		if err != nil {
			return err
		}
		if k, _ := res.RowsAffected(); k == 0 {
			return sql.ErrNoRows
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return st.PRByID(id)
}
