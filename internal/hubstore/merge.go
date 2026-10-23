package hubstore

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/dovholuknf/atrium/internal/gitsync"
)

// The merge queue's rows. See migration 0034 and docs/rnd/merge-queue.md.
//
//   - A REQUEST IS NEVER DELETED, so the table is the history the board and `atrium merge --list` read.
//   - A STATE ONLY MOVES ALONG mergeMoves, and an end state is never left.
//   - ONE OPEN REQUEST PER REPOSITORY AND BRANCH. Asking again answers the open one.

// The states of a request.
const (
	MergeQueued    = "queued"
	MergeMerging   = "merging"
	MergeTesting   = "testing"
	MergeConflict  = "conflict"
	MergeLanded    = "landed"
	MergeRed       = "red"
	MergeRefused   = "refused"
	MergeCancelled = "cancelled"
)

// SettingMergeRoom names the room that runs the queue's tests. Unset, the hub picks the room on its own machine.
const SettingMergeRoom = "merge_room"

// Bounds, in bytes unless said.
const (
	MergeReasonMax  = 500
	MergeFailingMax = 50
	MergeOutputMax  = 64 << 10
	mergeOutputHead = 8 << 10
	mergeListMax    = 500
)

var (
	// ErrMergeNotFound is a request number the queue never had.
	ErrMergeNotFound = errors.New("the merge queue has no such request")
	// ErrMergeMove is a move the state table does not have.
	ErrMergeMove = errors.New("a merge request cannot move that way")
)

var mergeMoves = map[string][]string{
	MergeQueued:   {MergeMerging, MergeRefused, MergeCancelled},
	MergeMerging:  {MergeTesting, MergeConflict, MergeRefused, MergeQueued},
	MergeTesting:  {MergeLanded, MergeRed, MergeRefused, MergeQueued},
	MergeConflict: {MergeLanded, MergeRefused},
}

// MergeOpen reports whether a state is still waiting on the queue or a merger.
func MergeOpen(state string) bool {
	_, ok := mergeMoves[state]
	return ok
}

func mergeMayMove(from, to string) bool {
	for _, s := range mergeMoves[from] {
		if s == to {
			return true
		}
	}
	return false
}

// MergeRequest is one row.
type MergeRequest struct {
	N             int64    `json:"n"`
	Repo          string   `json:"repo"`
	Branch        string   `json:"branch"`
	SHA           string   `json:"sha"`
	Requester     string   `json:"requester,omitempty"`
	RequesterRoom string   `json:"requester_room,omitempty"`
	Launcher      string   `json:"launcher,omitempty"`
	LauncherRoom  string   `json:"launcher_room,omitempty"`
	State         string   `json:"state"`
	Head          bool     `json:"head,omitempty"`
	Continues     int64    `json:"continues,omitempty"`
	Retries       int      `json:"retries,omitempty"`
	Base          string   `json:"base,omitempty"`
	Candidate     string   `json:"candidate,omitempty"`
	Landed        string   `json:"landed,omitempty"`
	Room          string   `json:"room,omitempty"`
	Merger        string   `json:"merger,omitempty"`
	Reason        string   `json:"reason,omitempty"`
	Failing       []string `json:"failing,omitempty"`
	Output        string   `json:"output,omitempty"`
	LandN         int64    `json:"land_n,omitempty"`
	AskedAt       string   `json:"asked_at"`
	StartedAt     string   `json:"started_at,omitempty"`
	TestedAt      string   `json:"tested_at,omitempty"`
	EndedAt       string   `json:"ended_at,omitempty"`
}

// MergeAsk is a request to merge Branch at SHA onto the integration branch of Repo. Continues is the request a
// merger's own request carries on, which puts it at the head of the queue.
type MergeAsk struct {
	Repo, Branch, SHA        string
	Requester, RequesterRoom string
	Launcher, LauncherRoom   string
	Continues                int64
}

func (a MergeAsk) check() string {
	switch {
	case strings.Count(a.Repo, "/") != 2 || !gitsync.ValidName(a.Repo):
		return "that is not <host>/<owner>/<repo>, like github.com/dovholuknf/atrium"
	case !strings.HasPrefix(a.Branch, "claude/") || a.Branch == gitsync.IntegrationBranch:
		return "the queue merges a claude/* branch, and never claude/main itself"
	case !flowShaRe.MatchString(a.SHA):
		return "a sha is 7 to 64 lower case hex digits"
	case a.Continues < 0:
		return "that is not a request number"
	}
	if why := gitsync.CheckPushBranch(a.Branch); why != "" {
		return why
	}
	for name, v := range map[string]string{"requester": a.Requester, "requester's room": a.RequesterRoom,
		"launcher": a.Launcher, "launcher's room": a.LauncherRoom} {
		if why := CRText(name, v, backlogByMax, false); why != "" {
			return why
		}
	}
	return ""
}

const mergeCols = `n, repo, branch, sha, requester, requester_room, launcher, launcher_room, state, head, continues,
	retries, base, candidate, landed, room, merger, reason, failing, output, land_n, asked_at, started_at, tested_at,
	ended_at`

func scanMerge(sc rowScanner) (MergeRequest, error) {
	var m MergeRequest
	var head int
	var failing string
	err := sc.Scan(&m.N, &m.Repo, &m.Branch, &m.SHA, &m.Requester, &m.RequesterRoom, &m.Launcher, &m.LauncherRoom,
		&m.State, &head, &m.Continues, &m.Retries, &m.Base, &m.Candidate, &m.Landed, &m.Room, &m.Merger, &m.Reason,
		&failing, &m.Output, &m.LandN, &m.AskedAt, &m.StartedAt, &m.TestedAt, &m.EndedAt)
	m.Head = head != 0
	if failing != "" {
		m.Failing = strings.Split(failing, "\n")
	}
	return m, err
}

func mergeOpenStates() string {
	return `('` + MergeQueued + `','` + MergeMerging + `','` + MergeTesting + `','` + MergeConflict + `')`
}

// MergeAsked queues a request, or answers the open request for that branch with existing true. An open request at
// another sha is refused, so a push after asking is not silently merged at the old tip.
func (s *Store) MergeAsked(a MergeAsk) (MergeRequest, bool, error) {
	a.Repo, a.Branch, a.SHA = strings.TrimSpace(a.Repo), strings.TrimSpace(a.Branch), strings.ToLower(strings.TrimSpace(a.SHA))
	a.RequesterRoom, a.LauncherRoom = fold(a.RequesterRoom), fold(a.LauncherRoom)
	if strings.Count(a.Repo, "/") == 2 {
		a.Repo = MergeRepoName(a.Repo)
	}
	if why := a.check(); why != "" {
		return MergeRequest{}, false, refuse(errors.New(why))
	}
	var out MergeRequest
	existing := false
	err := s.tx(func(t *sql.Tx) error {
		existing = false
		cur, err := scanMerge(t.QueryRow(`SELECT `+mergeCols+` FROM merge_request WHERE repo = ? AND branch = ? AND state IN `+
			mergeOpenStates()+` ORDER BY n DESC LIMIT 1`, a.Repo, a.Branch))
		switch {
		case err == nil:
			if cur.SHA != a.SHA && !strings.HasPrefix(cur.SHA, a.SHA) && !strings.HasPrefix(a.SHA, cur.SHA) {
				return refuse(fmt.Errorf("request %d already merges %s at %s and is %s. cancel it first", cur.N, cur.Branch,
					shortSHA(cur.SHA), cur.State))
			}
			out, existing = cur, true
			return nil
		case !errors.Is(err, sql.ErrNoRows):
			return err
		}
		if a.Continues > 0 {
			var one int
			if err := t.QueryRow(`SELECT 1 FROM merge_request WHERE n = ? AND repo = ?`, a.Continues, a.Repo).Scan(&one); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return refuse(fmt.Errorf("request %d is not in the queue of %s", a.Continues, a.Repo))
				}
				return err
			}
		}
		var top int64
		if err := t.QueryRow(`SELECT COALESCE(MAX(n), 0) FROM merge_request`).Scan(&top); err != nil {
			return err
		}
		head := 0
		if a.Continues > 0 {
			head = 1
		}
		if _, err := t.Exec(`INSERT INTO merge_request (n, repo, branch, sha, requester, requester_room, launcher,
			launcher_room, state, head, continues, asked_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			top+1, a.Repo, a.Branch, a.SHA, a.Requester, a.RequesterRoom, a.Launcher, a.LauncherRoom, MergeQueued, head,
			a.Continues, ts(s.now())); err != nil {
			return err
		}
		out, err = scanMerge(t.QueryRow(`SELECT `+mergeCols+` FROM merge_request WHERE n = ?`, top+1))
		return err
	})
	return out, existing, err
}

func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// MergeGet is one request, or ErrMergeNotFound.
func (s *Store) MergeGet(n int64) (MergeRequest, error) {
	var m MergeRequest
	err := s.guard(func() error {
		var err error
		m, err = scanMerge(s.db.QueryRow(`SELECT `+mergeCols+` FROM merge_request WHERE n = ?`, n))
		return err
	})
	if errors.Is(err, sql.ErrNoRows) {
		return m, ErrMergeNotFound
	}
	return m, err
}

// MergeFilter narrows MergeList. Open keeps the requests still waiting. Limit is at most 500, 100 when unset.
type MergeFilter struct {
	Repo   string
	Branch string
	Open   bool
	Limit  int
	Output bool
}

// MergeList lists requests, the open ones first and then newest first. The test output is left out unless asked for.
func (s *Store) MergeList(f MergeFilter) ([]MergeRequest, error) {
	if f.Limit <= 0 || f.Limit > mergeListMax {
		f.Limit = 100
	}
	out := []MergeRequest{}
	err := s.guard(func() error {
		out = []MergeRequest{}
		where, args := []string{"1 = 1"}, []any{}
		if f.Repo != "" {
			where, args = append(where, "repo = ?"), append(args, MergeRepoName(f.Repo))
		}
		if f.Branch != "" {
			where, args = append(where, "branch = ?"), append(args, f.Branch)
		}
		if f.Open {
			where = append(where, "state IN "+mergeOpenStates())
		}
		rows, err := s.db.Query(`SELECT `+mergeCols+` FROM merge_request WHERE `+strings.Join(where, " AND ")+
			` ORDER BY CASE WHEN state IN `+mergeOpenStates()+` THEN 0 ELSE 1 END, n DESC LIMIT ?`, append(args, f.Limit)...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			m, err := scanMerge(rows)
			if err != nil {
				return err
			}
			if !f.Output {
				m.Output = ""
			}
			out = append(out, m)
		}
		return rows.Err()
	})
	return out, err
}

// MergeNext is the request a repository's queue runs next, with ok false when one is already merging or testing there
// or nothing is queued. A merger's request goes first, then the oldest.
func (s *Store) MergeNext(repo string) (MergeRequest, bool, error) {
	repo = MergeRepoName(repo)
	var m MergeRequest
	ok := false
	err := s.guard(func() error {
		ok = false
		var busy int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM merge_request WHERE repo = ? AND state IN (?, ?)`,
			repo, MergeMerging, MergeTesting).Scan(&busy); err != nil {
			return err
		}
		if busy > 0 {
			return nil
		}
		var err error
		m, err = scanMerge(s.db.QueryRow(`SELECT `+mergeCols+` FROM merge_request WHERE repo = ? AND state = ?
			ORDER BY head DESC, n LIMIT 1`, repo, MergeQueued))
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		ok = err == nil
		return err
	})
	return m, ok, err
}

// MergeRepos is every repository with a request that is queued, merging or testing.
func (s *Store) MergeRepos() ([]string, error) {
	out := []string{}
	err := s.guard(func() error {
		out = []string{}
		rows, err := s.db.Query(`SELECT DISTINCT repo FROM merge_request WHERE state IN (?, ?, ?) ORDER BY repo`,
			MergeQueued, MergeMerging, MergeTesting)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var r string
			if err := rows.Scan(&r); err != nil {
				return err
			}
			out = append(out, r)
		}
		return rows.Err()
	})
	return out, err
}

// MergeStep moves request N to To. Every other field is kept when empty. Base, Candidate, Landed, Room, Merger and
// Reason are written as given, Failing replaces the failing tests and Output replaces the output, cut to its cap.
// From, when set, is the state the caller believes the request is in, and a request in another state is refused.
type MergeStep struct {
	N                                             int64
	From, To                                      string
	Base, Candidate, Landed, Room, Merger, Reason string
	Failing                                       []string
	Output                                        string
}

func (m MergeStep) check() string {
	switch {
	case m.Base != "" && !flowShaRe.MatchString(m.Base),
		m.Candidate != "" && !flowShaRe.MatchString(m.Candidate),
		m.Landed != "" && !flowShaRe.MatchString(m.Landed):
		return "a sha is 7 to 64 lower case hex digits"
	case len(m.Failing) > MergeFailingMax:
		return fmt.Sprintf("name at most %d failing tests", MergeFailingMax)
	}
	if why := CRText("reason", m.Reason, MergeReasonMax, true); why != "" {
		return why
	}
	for name, v := range map[string]string{"room": m.Room, "merger": m.Merger} {
		if why := CRText(name, v, backlogByMax, false); why != "" {
			return why
		}
	}
	for _, f := range m.Failing {
		if why := CRText("failing test", f, TaskOutputMax, false); why != "" {
			return why
		}
	}
	return ""
}

// MergeCut keeps the head and the tail of an output that is over MergeOutputMax, at line ends where it can.
func MergeCut(out string) string {
	out = strings.ToValidUTF8(out, "?")
	if len(out) <= MergeOutputMax {
		return out
	}
	head := out[:mergeOutputHead]
	if i := strings.LastIndexByte(head, '\n'); i > 0 {
		head = head[:i+1]
	}
	tail := out[len(out)-(MergeOutputMax-mergeOutputHead-64):]
	if i := strings.IndexByte(tail, '\n'); i >= 0 && i < len(tail)-1 {
		tail = tail[i+1:]
	}
	tail = strings.ToValidUTF8(tail, "")
	return head + fmt.Sprintf("\n[... %d bytes cut ...]\n\n", len(out)-len(head)-len(tail)) + tail
}

// MergeMove applies a step and answers the row it made.
func (s *Store) MergeMove(m MergeStep) (MergeRequest, error) {
	if why := m.check(); why != "" {
		return MergeRequest{}, refuse(errors.New(why))
	}
	m.Room = fold(m.Room)
	var out MergeRequest
	err := s.tx(func(t *sql.Tx) error {
		cur, err := scanMerge(t.QueryRow(`SELECT `+mergeCols+` FROM merge_request WHERE n = ?`, m.N))
		if errors.Is(err, sql.ErrNoRows) {
			return refuse(ErrMergeNotFound)
		}
		if err != nil {
			return err
		}
		if m.From != "" && cur.State != m.From {
			return refuse(fmt.Errorf("%w: request %d is %s, not %s", ErrMergeMove, m.N, cur.State, m.From))
		}
		if !mergeMayMove(cur.State, m.To) {
			return refuse(fmt.Errorf("%w: request %d is %s and cannot become %s", ErrMergeMove, m.N, cur.State, m.To))
		}
		at := ts(s.now())
		sets, args := []string{"state = ?"}, []any{m.To}
		set := func(col string, v any) { sets, args = append(sets, col+" = ?"), append(args, v) }
		for col, v := range map[string]string{"base": m.Base, "candidate": m.Candidate, "landed": m.Landed,
			"room": m.Room, "merger": m.Merger, "reason": m.Reason} {
			if v != "" {
				set(col, v)
			}
		}
		if m.Failing != nil {
			set("failing", strings.Join(m.Failing, "\n"))
		}
		if m.Output != "" {
			set("output", MergeCut(m.Output))
		}
		switch {
		case m.To == MergeMerging:
			set("started_at", at)
		case m.To == MergeQueued:
			set("retries", cur.Retries+1)
			set("candidate", "")
			set("started_at", "")
		}
		if cur.State == MergeTesting && m.To != MergeRefused {
			set("tested_at", at)
		}
		if !MergeOpen(m.To) {
			set("ended_at", at)
		}
		if _, err := t.Exec(`UPDATE merge_request SET `+strings.Join(sets, ", ")+` WHERE n = ?`, append(args, m.N)...); err != nil {
			return err
		}
		out, err = scanMerge(t.QueryRow(`SELECT `+mergeCols+` FROM merge_request WHERE n = ?`, m.N))
		return err
	})
	return out, err
}

// MergeRoom reads the configured merge room, "" when unset.
func (s *Store) MergeRoom() (string, error) { return s.HubSetting(SettingMergeRoom) }

// SetMergeRoom sets the merge room, and "" unsets it. The room has to be one the hub has.
func (s *Store) SetMergeRoom(name string) error {
	name = fold(name)
	if name != "" {
		if _, err := s.ByName(name); errors.Is(err, ErrNoSuchRoom) {
			return refuse(fmt.Errorf("the hub has no room %s", name))
		} else if err != nil {
			return err
		}
	}
	return s.SetHubSetting(SettingMergeRoom, name)
}

// MergeRepoName is a repository's name as the hub's store spells it, so github.com and github are one queue.
func MergeRepoName(name string) string {
	name = strings.TrimSpace(name)
	if ref, err := gitsync.ParseName(name); err == nil {
		return ref.Name()
	}
	return name
}
