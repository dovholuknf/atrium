package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"
)

// The work ledger: who was handed what, where it is, and who said it was done.
// See docs/runtime/work-ledger-design.md for the whole argument.
//
// A card another session launched gets a WORK ITEM. Its state is a second axis
// beside the card's column. The column says what the session is doing, the
// work state says where the work is, and the two are not the same claim: a
// card reaches `done` when a worker reports, when its session ends, and when a
// human drags it, and none of those says the work is finished.
//
// This file is stage 1: the ledger RECORDS. Items are created at launch,
// reports and messages are logged, a `done` report moves work to `reported`,
// and a session that ends before one moves it to `ended-without-report` in the
// same transaction as the exit. Nothing here closes work. The verdicts that do
// (accept, reject, abandon, reopen) are stage 2, and the states they reach are
// in the schema already so that stage adds verbs, not a migration.

// Work states.
const (
	WorkOpen       = "open"
	WorkReported   = "reported"
	WorkReopened   = "reopened"
	WorkEnded      = "ended-without-report"
	WorkAccepted   = "accepted"
	WorkAbandoned  = "abandoned"
	WorkSuperseded = "superseded"
)

// openStatesSQL is every state that is not closed, for an IN clause.
const openStatesSQL = `('open','reported','reopened','ended-without-report')`

// WorkClosed reports whether a state is closed. Only a verdict reaches one.
func WorkClosed(state string) bool {
	switch state {
	case WorkAccepted, WorkAbandoned, WorkSuperseded:
		return true
	}
	return false
}

// Work log kinds.
const (
	LogReport      = "report"
	LogSay         = "say"
	LogInstruction = "instruction"
	LogVerdict     = "verdict"
	LogAtrium      = "atrium"
)

// Who moved an item, besides an arbiter's handle.
const (
	ByWorker = "worker"
	ByAtrium = "atrium"
)

// The bounds. Operational history, not an audit trail.
const (
	MaxWorkBrief    = 4000
	MaxWorkText     = 8000
	MaxWorkOutput   = 400
	MaxWorkOutputs  = 16 << 10
	MaxWorkLogRows  = 300
	MaxWorkLogBytes = 1 << 20
)

// WorkItem is one piece of delegated work.
type WorkItem struct {
	TaskID         string `json:"task_id"`
	Handle         string `json:"handle"`
	Title          string `json:"title"`
	Worktree       string `json:"worktree"`
	LauncherID     string `json:"launcher_id"`
	LauncherHandle string `json:"launcher_handle"`
	// ArbiterID is who rules on the work now. It starts as the launcher, and
	// the launcher never changes: it is provenance. Every notice goes here.
	ArbiterID     string    `json:"arbiter_id"`
	ArbiterHandle string    `json:"arbiter_handle"`
	Brief         string    `json:"brief,omitempty"`
	BriefPath     string    `json:"brief_path,omitempty"`
	State         string    `json:"state"`
	StateAt       time.Time `json:"state_at"`
	StateBy       string    `json:"state_by"`
	// Revision goes up when the state moves or a newer report replaces the
	// one a verdict would rule on. A verdict names the revision it read. A
	// session ending after its `done` report does not move it, so an arbiter
	// who read that report can still rule on it.
	Revision int `json:"revision"`
	// Generation is which run of the card's session this is, and
	// EndedGeneration the last one atrium recorded as over. Equal means no
	// session is running, and a second exit for the same death is a no-op.
	Generation       int         `json:"generation"`
	EndedGeneration  int         `json:"ended_generation"`
	LatestReportID   string      `json:"latest_report_id,omitempty"`
	AcceptedReportID string      `json:"accepted_report_id,omitempty"`
	Outputs          WorkOutputs `json:"outputs"`
	ContinuedIn      string      `json:"continued_in,omitempty"`
	Continues        string      `json:"continues,omitempty"`
	// Inferred marks an item the backfill made from thin records.
	Inferred  bool      `json:"inferred,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	// The merged-cull mark. See merged.go.
	MergedAt     *time.Time `json:"merged_at,omitempty"`
	MergedInto   string     `json:"merged_into,omitempty"`
	MergedSHA    string     `json:"merged_sha,omitempty"`
	MergedBranch string     `json:"merged_branch,omitempty"`
	CullAt       *time.Time `json:"cull_at,omitempty"`
	HeldBy       string     `json:"held_by,omitempty"`
	// LastReport is the newest report in the log, filled in by the readers
	// that list items. Not a column.
	LastReport *WorkLogEntry `json:"last_report,omitempty"`
}

// Running reports whether the item's current generation has not ended.
func (w *WorkItem) Running() bool { return w.EndedGeneration < w.Generation }

// WorkOutputs is where a `done` report says the work is. Stage 1 fills in the
// commit `atrium_report` already takes. Branch and paths, and the checks on
// each, are stage 3, and the shape is ready for them.
type WorkOutputs struct {
	Repo     string   `json:"repo,omitempty"`
	Branch   string   `json:"branch,omitempty"`
	Commits  []string `json:"commits,omitempty"`
	Paths    []string `json:"paths,omitempty"`
	NoCommit string   `json:"no_commit,omitempty"`
	// Unverified names each output that could not be found. Recorded, never
	// a reason to refuse the report.
	Unverified []string `json:"unverified,omitempty"`
}

func (o WorkOutputs) empty() bool {
	return o.Repo == "" && o.Branch == "" && len(o.Commits) == 0 && len(o.Paths) == 0 && o.NoCommit == ""
}

// Summary is the outputs in one line, for a notice or the snapshot file.
func (o WorkOutputs) Summary() string {
	var parts []string
	if o.Branch != "" {
		parts = append(parts, "branch "+o.Branch)
	}
	if len(o.Commits) > 0 {
		parts = append(parts, "commits "+strings.Join(o.Commits, ", "))
	}
	if len(o.Paths) > 0 {
		parts = append(parts, "paths "+strings.Join(o.Paths, ", "))
	}
	if o.NoCommit != "" {
		parts = append(parts, "no commit: "+o.NoCommit)
	}
	if len(o.Unverified) > 0 {
		parts = append(parts, "unverified: "+strings.Join(o.Unverified, ", "))
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, "; ")
}

// bounded cuts every string to its limit and the whole to MaxWorkOutputs.
func (o WorkOutputs) bounded() WorkOutputs {
	cut := func(s string) string { return cutRunes(strings.TrimSpace(s), MaxWorkOutput) }
	out := WorkOutputs{Repo: cut(o.Repo), Branch: cut(o.Branch), NoCommit: cut(o.NoCommit)}
	for i, c := range o.Commits {
		if i == 20 {
			break
		}
		out.Commits = append(out.Commits, cut(c))
	}
	for i, p := range o.Paths {
		if i == 50 {
			break
		}
		out.Paths = append(out.Paths, cut(p))
	}
	for _, u := range o.Unverified {
		out.Unverified = append(out.Unverified, cut(u))
	}
	for len(out.json()) > MaxWorkOutputs && len(out.Paths) > 0 {
		out.Paths = out.Paths[:len(out.Paths)-1]
	}
	return out
}

func (o WorkOutputs) json() string {
	if o.empty() && len(o.Unverified) == 0 {
		return "{}"
	}
	raw, _ := json.Marshal(o)
	return string(raw)
}

func parseOutputs(raw string) WorkOutputs {
	var o WorkOutputs
	if strings.TrimSpace(raw) != "" {
		_ = json.Unmarshal([]byte(raw), &o)
	}
	return o
}

// WorkLogEntry is one thing said or decided about the work.
type WorkLogEntry struct {
	ID       string       `json:"id"`
	TaskID   string       `json:"task_id"`
	At       time.Time    `json:"at"`
	Kind     string       `json:"kind"`
	By       string       `json:"by"`
	Status   string       `json:"status,omitempty"`
	Text     string       `json:"text,omitempty"`
	Outputs  *WorkOutputs `json:"outputs,omitempty"`
	ReportID string       `json:"report_id,omitempty"`
	// Note is what atrium adds about the entry: "after the session ended",
	// "after close", "recap at backfill".
	Note string `json:"note,omitempty"`
}

// LedgerNotice is a notice the ledger queued inside its transaction, handed to
// the daemon after the commit so it can type it when the arbiter's terminal is
// free. The queue row is already durable by then: this is delivery, not record.
type LedgerNotice struct {
	TaskID    string
	ArbiterID string
	MessageID string
	From      string
	Text      string
	Source    string
	// Held is a notice recorded on the arbiter's card and not queued: MessageID is
	// empty and there is nothing to type.
	Held bool
}

// Notice sources the ledger writes, for the a2a_notice dedupe.
const NoticeEnded = "ended"

// HeldNoticePayload is the `notified` event a held notice leaves on the card it
// was for. `held` is what a reader filters on. The one shape, so the ledger's
// notices and the daemon's read alike.
func HeldNoticePayload(source, about, aboutID, text string) map[string]any {
	return map[string]any{
		"by": ByAtrium, "held": true, "source": source, "about": about, "about_card": aboutID, "text": text,
	}
}

// cutRunes bounds a string to n bytes on a rune boundary.
func cutRunes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && !utf8Start(s[cut]) {
		cut--
	}
	return s[:cut]
}

// ledgerChanged tells the daemon, after the commit, that an item moved.
func (s *Store) ledgerChanged(tx *Tx, taskID string) {
	tx.afterCommit(func() {
		if cb := s.OnLedgerChange; cb != nil {
			cb(taskID)
		}
	})
}

// ── creating ────────────────────────────────────────────────────────────────────

// NewWorkItem is what a launch knows about the work it is handing out.
type NewWorkItem struct {
	Brief     string
	BriefPath string
}

// CreateWorkItem gives a launched card its item, and reports whether it made
// one. A card nobody launched, or one the board's own dialog launched, gets
// nothing: the human did not ask for a queue of verdicts. A card that already
// has one keeps it, so a second launch onto the same card cannot reset it.
func (s *Store) CreateWorkItem(t *Task, in NewWorkItem) (bool, error) {
	if t == nil || !t.Launched() {
		return false, nil
	}
	made := false
	err := s.inTx(func(tx *Tx) error {
		made = false
		n := ts(now())
		launcherHandle := t.SpawnedBy
		res, err := tx.Exec(`INSERT INTO work_item (task_id, handle, title, worktree, launcher_id,
				launcher_handle, arbiter_id, arbiter_handle, brief, brief_path, state, state_at, state_by,
				created_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)
			ON CONFLICT (task_id) DO NOTHING`,
			t.ID, t.WireName, t.DisplayTitle(), t.Worktree, t.SpawnedByID, launcherHandle,
			t.SpawnedByID, launcherHandle, cutRunes(strings.TrimSpace(in.Brief), MaxWorkBrief),
			strings.TrimSpace(in.BriefPath), WorkOpen, n, launcherHandle, ts(t.CreatedAt))
		if err != nil {
			return err
		}
		got, err := res.RowsAffected()
		if err != nil {
			return err
		}
		made = got > 0
		if made {
			s.ledgerChanged(tx, t.ID)
		}
		return nil
	})
	return made, err
}

// ── reading ─────────────────────────────────────────────────────────────────────

const workItemColumns = `task_id, handle, title, worktree, launcher_id, launcher_handle, arbiter_id,
	arbiter_handle, brief, brief_path, state, state_at, state_by, revision, generation, ended_generation,
	latest_report_id, accepted_report_id, outputs, continued_in, continues, inferred, created_at,
	merged_at, merged_into, merged_sha, merged_branch, cull_at, held_by`

func scanWorkItem(sc interface{ Scan(...any) error }) (*WorkItem, error) {
	var (
		w                WorkItem
		stateAt, created string
		outputs          string
		inferred         int
		mergedAt, cullAt string
	)
	if err := sc.Scan(&w.TaskID, &w.Handle, &w.Title, &w.Worktree, &w.LauncherID, &w.LauncherHandle,
		&w.ArbiterID, &w.ArbiterHandle, &w.Brief, &w.BriefPath, &w.State, &stateAt, &w.StateBy,
		&w.Revision, &w.Generation, &w.EndedGeneration, &w.LatestReportID, &w.AcceptedReportID,
		&outputs, &w.ContinuedIn, &w.Continues, &inferred, &created,
		&mergedAt, &w.MergedInto, &w.MergedSHA, &w.MergedBranch, &cullAt, &w.HeldBy); err != nil {
		return nil, err
	}
	if t, err := parseTS(mergedAt); err == nil {
		w.MergedAt = &t
	}
	if t, err := parseTS(cullAt); err == nil {
		w.CullAt = &t
	}
	w.StateAt, _ = parseTS(stateAt)
	w.CreatedAt, _ = parseTS(created)
	w.Outputs = parseOutputs(outputs)
	w.Inferred = inferred != 0
	return &w, nil
}

func workItemOn(q querier, taskID string) (*WorkItem, error) {
	return scanWorkItem(q.QueryRow(`SELECT `+workItemColumns+` FROM work_item WHERE task_id = ?`, taskID))
}

// WorkItem returns one card's item, or sql.ErrNoRows.
func (s *Store) WorkItem(taskID string) (*WorkItem, error) {
	var out *WorkItem
	err := s.guard(func() error {
		w, err := workItemOn(s.db, taskID)
		if err != nil {
			return err
		}
		if w.LastReport, err = lastReportOn(s.db, taskID); err != nil {
			return err
		}
		out = w
		return nil
	})
	return out, err
}

// LedgerView is what the snapshot file and `atrium ledger` list: every item
// that is not closed, then those closed since the cutoff.
type LedgerView struct {
	At     time.Time   `json:"at"`
	Open   []*WorkItem `json:"open"`
	Closed []*WorkItem `json:"closed"`
}

// LedgerClosedFor is how long a closed item stays in the listing.
const LedgerClosedFor = 7 * 24 * time.Hour

// Ledger lists the ledger for the snapshot file.
func (s *Store) Ledger() (*LedgerView, error) {
	var out *LedgerView
	err := s.guard(func() error {
		v, err := ledgerOn(s.db, now())
		out = v
		return err
	})
	return out, err
}

func ledgerOn(q querier, at time.Time) (*LedgerView, error) {
	v := &LedgerView{At: at}
	var err error
	if v.Open, err = listWorkItemsOn(q, `state IN `+openStatesSQL); err != nil {
		return nil, err
	}
	if v.Closed, err = listWorkItemsOn(q, `state NOT IN `+openStatesSQL+` AND state_at >= ?`,
		ts(at.Add(-LedgerClosedFor))); err != nil {
		return nil, err
	}
	for _, w := range append(append([]*WorkItem{}, v.Open...), v.Closed...) {
		if w.LastReport, err = lastReportOn(q, w.TaskID); err != nil {
			return nil, err
		}
	}
	sortLedger(v.Open)
	return v, nil
}

// listWorkItemsOn reads items, oldest state change first.
func listWorkItemsOn(q querier, where string, args ...any) ([]*WorkItem, error) {
	rows, err := q.Query(`SELECT `+workItemColumns+` FROM work_item WHERE `+where+
		` ORDER BY state_at ASC, task_id ASC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*WorkItem
	for rows.Next() {
		w, err := scanWorkItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// ledgerRank is the order the open list reads in: the crash case first, then
// work waiting on somebody, then work sent back, then work in hand.
func ledgerRank(state string) int {
	switch state {
	case WorkEnded:
		return 0
	case WorkReported:
		return 1
	case WorkReopened:
		return 2
	}
	return 3
}

func sortLedger(items []*WorkItem) {
	// Insertion sort, stable, on a list already ordered by age.
	for i := 1; i < len(items); i++ {
		for j := i; j > 0 && ledgerRank(items[j].State) < ledgerRank(items[j-1].State); j-- {
			items[j], items[j-1] = items[j-1], items[j]
		}
	}
}

const workLogColumns = `id, task_id, at, kind, by, status, text, outputs, report_id, note`

func scanWorkLog(sc interface{ Scan(...any) error }) (*WorkLogEntry, error) {
	var (
		e           WorkLogEntry
		at, outputs string
	)
	if err := sc.Scan(&e.ID, &e.TaskID, &at, &e.Kind, &e.By, &e.Status, &e.Text, &outputs,
		&e.ReportID, &e.Note); err != nil {
		return nil, err
	}
	e.At, _ = parseTS(at)
	if strings.TrimSpace(outputs) != "" {
		o := parseOutputs(outputs)
		e.Outputs = &o
	}
	return &e, nil
}

func lastReportOn(q querier, taskID string) (*WorkLogEntry, error) {
	e, err := scanWorkLog(q.QueryRow(`SELECT `+workLogColumns+` FROM work_log
		WHERE task_id = ? AND kind = 'report' ORDER BY at DESC, id DESC LIMIT 1`, taskID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return e, err
}

// WorkLog returns an item's newest `limit` log entries, oldest first.
func (s *Store) WorkLog(taskID string, limit int) ([]*WorkLogEntry, error) {
	if limit <= 0 {
		limit = MaxWorkLogRows
	}
	var out []*WorkLogEntry
	err := s.guard(func() error {
		out = nil
		rows, err := s.db.Query(`SELECT `+workLogColumns+` FROM work_log
			WHERE task_id = ? ORDER BY at DESC, id DESC LIMIT ?`, taskID, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			e, err := scanWorkLog(rows)
			if err != nil {
				return err
			}
			out = append(out, e)
		}
		return rows.Err()
	})
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, err
}

// OpenWorkItems lists items the liveness sweep has to look at: open or sent
// back, with no end recorded for the current generation.
func (s *Store) OpenWorkItems() ([]*WorkItem, error) {
	var out []*WorkItem
	err := s.guard(func() error {
		got, err := listWorkItemsOn(s.db, `state IN ('open','reopened') AND ended_generation < generation`)
		out = got
		return err
	})
	return out, err
}

// WorkerIDs lists the cards whose work item names this card as its launcher,
// whatever state the item is in. Whether a worker is still around is the
// caller's question, since only the daemon knows about runners.
func (s *Store) WorkerIDs(launcherID string) ([]string, error) {
	var out []string
	err := s.guard(func() error {
		rows, err := s.db.Query(`SELECT task_id FROM work_item WHERE launcher_id = ? ORDER BY task_id`,
			launcherID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				return err
			}
			out = append(out, id)
		}
		return rows.Err()
	})
	return out, err
}

// ── the log ─────────────────────────────────────────────────────────────────────

// logRow is one row to write.
type logRow struct {
	kind, by, status, text, note, reportID, clientID string
	outputs                                          *WorkOutputs
	// at is when it happened, when that is not now: the backfill writes a
	// report at the time its event says.
	at time.Time
}

// insertLog writes one row and trims the item's log back under its caps, in
// the caller's transaction. It reports the row id, or "" when a row with the
// same client id is already there, which is a retry.
func (s *Store) insertLog(tx *Tx, taskID string, r logRow) (string, error) {
	id := newID()
	outputs := ""
	if r.outputs != nil {
		outputs = r.outputs.bounded().json()
	}
	var client any
	if r.clientID != "" {
		client = r.clientID
	}
	at := now()
	if !r.at.IsZero() {
		at = r.at
	}
	res, err := tx.Exec(`INSERT INTO work_log (id, task_id, at, kind, by, status, text, outputs, report_id,
			note, client_id)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT (task_id, client_id) DO NOTHING`,
		id, taskID, ts(at), r.kind, r.by, r.status,
		cutRunes(strings.TrimSpace(r.text), MaxWorkText), outputs, r.reportID, r.note, client)
	if err != nil {
		return "", err
	}
	if n, err := res.RowsAffected(); err != nil {
		return "", err
	} else if n == 0 {
		return "", nil
	}
	return id, s.trimLog(tx, taskID)
}

// trimClass is the order rows go in once an item is over its cap: chatter
// first, then reports that did not claim the work was done, then atrium's own
// rows, and done reports and verdicts last.
const trimClass = `CASE
	WHEN kind IN ('say','instruction') THEN 0
	WHEN kind = 'report' AND status != 'done' THEN 1
	WHEN kind = 'atrium' THEN 2
	ELSE 3 END`

// trimLog holds an item to MaxWorkLogRows rows and MaxWorkLogBytes bytes,
// oldest first within each class, never taking the latest or the accepted
// report.
//
// A verdict that outlives the report it ruled on keeps a copy of it in its own
// row. Stage 2 writes verdicts and that copy, and this is where it will read
// from.
func (s *Store) trimLog(tx *Tx, taskID string) error {
	var rows, bytes int
	if err := tx.QueryRow(`SELECT COUNT(*),
			COALESCE(SUM(LENGTH(CAST(text AS BLOB)) + LENGTH(CAST(outputs AS BLOB))), 0)
		FROM work_log WHERE task_id = ?`, taskID).Scan(&rows, &bytes); err != nil {
		return err
	}
	if rows <= MaxWorkLogRows && bytes <= MaxWorkLogBytes {
		return nil
	}
	var keepLatest, keepAccepted string
	err := tx.QueryRow(`SELECT latest_report_id, accepted_report_id FROM work_item WHERE task_id = ?`,
		taskID).Scan(&keepLatest, &keepAccepted)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	cands, err := tx.Query(`SELECT id, LENGTH(CAST(text AS BLOB)) + LENGTH(CAST(outputs AS BLOB))
		FROM work_log WHERE task_id = ? AND id != ? AND id != ?
		ORDER BY `+trimClass+`, at ASC, id ASC`, taskID, keepLatest, keepAccepted)
	if err != nil {
		return err
	}
	var drop []string
	for cands.Next() && (rows > MaxWorkLogRows || bytes > MaxWorkLogBytes) {
		var (
			id   string
			size int
		)
		if err := cands.Scan(&id, &size); err != nil {
			cands.Close()
			return err
		}
		drop = append(drop, id)
		rows--
		bytes -= size
	}
	cands.Close()
	if err := cands.Err(); err != nil {
		return err
	}
	for _, id := range drop {
		if _, err := tx.Exec(`DELETE FROM work_log WHERE id = ?`, id); err != nil {
			return err
		}
	}
	return nil
}

// ── the session starting and ending ─────────────────────────────────────────────

// ledgerOnSession moves a card's item for a `launched` or `exited` event, in
// the transaction that wrote the event. See AppendEvent.
//
// Keyed on generations, not on events, because several parts of atrium notice
// one death: the reaper, the session hook, the supervisor, the orphan reaper.
// The first to record it ends the generation. The rest find it already ended
// and do nothing, so one death is one log row and one notice.
func (s *Store) ledgerOnSession(tx *Tx, taskID string, e *Event) error {
	w, err := workItemOn(tx, taskID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var payload map[string]any
	_ = json.Unmarshal(e.Payload, &payload)

	// Copied while the card exists, so a pruned card's item still reads.
	if t, err := getByOn(tx, `id = ?`, taskID); err == nil {
		if _, err := tx.Exec(`UPDATE work_item SET handle = ?, title = ?, worktree = ? WHERE task_id = ?`,
			orKeep(t.WireName, w.Handle), t.DisplayTitle(), orKeep(t.Worktree, w.Worktree), taskID); err != nil {
			return err
		}
		w.Handle = orKeep(t.WireName, w.Handle)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	switch e.Kind {
	case EventLaunched:
		return s.ledgerRunning(tx, w, payload)
	case EventExited:
		return s.ledgerEnded(tx, w, payload, e.At)
	}
	return nil
}

// ledgerRunning is the card's session running again. Atrium never resumes
// anything: a human or a launcher did, and this follows that fact. A launch
// while the current generation is still running is the same run announcing
// itself twice (the launch and then its session hook), and changes nothing.
func (s *Store) ledgerRunning(tx *Tx, w *WorkItem, payload map[string]any) error {
	if w.Running() {
		return nil
	}
	gen := w.Generation + 1
	by := payloadWord(payload, "by", "a launch")
	// A RESTART OR A RESUME IS NOT NEW WORK. Only a launch that carried a prompt reopens an item that ended
	// without a report; the room reopening a card after a deploy, or an idle card waking, leaves it as it was.
	if prompted, _ := payload["prompted"].(bool); w.State == WorkEnded && prompted {
		if _, err := tx.Exec(`UPDATE work_item SET generation = ?, state = ?, state_at = ?, state_by = ?,
				revision = revision + 1 WHERE task_id = ?`,
			gen, WorkOpen, ts(now()), ByAtrium, w.TaskID); err != nil {
			return err
		}
		if _, err := s.insertLog(tx, w.TaskID, logRow{kind: LogAtrium, by: ByAtrium,
			text: fmt.Sprintf("running again (generation %d, started by %s with a new prompt), back to open", gen, by)}); err != nil {
			return err
		}
	} else {
		if _, err := tx.Exec(`UPDATE work_item SET generation = ? WHERE task_id = ?`, gen, w.TaskID); err != nil {
			return err
		}
		if _, err := s.insertLog(tx, w.TaskID, logRow{kind: LogAtrium, by: ByAtrium,
			text: fmt.Sprintf("running again (generation %d, started by %s)", gen, by)}); err != nil {
			return err
		}
	}
	s.ledgerChanged(tx, w.TaskID)
	return nil
}

// ledgerEnded is the card's session ending. It moves `open` and `reopened` to
// `ended-without-report` and nothing else: a session that ends after a `done`
// report leaves the item `reported`, because the report is on record and the
// arbiter can still judge it. The process never closes work.
func (s *Store) ledgerEnded(tx *Tx, w *WorkItem, payload map[string]any, at time.Time) error {
	// An exit that names a generation that is not the current one is late:
	// the card is running again since. Logged, and it moves nothing.
	if g, ok := payload["generation"].(float64); ok && int(g) != w.Generation {
		_, err := s.insertLog(tx, w.TaskID, logRow{kind: LogAtrium, by: ByAtrium,
			text: fmt.Sprintf("a late exit from generation %d arrived during generation %d, ignored",
				int(g), w.Generation)})
		if err == nil {
			s.ledgerChanged(tx, w.TaskID)
		}
		return err
	}
	if !w.Running() {
		return nil // this death is already recorded
	}
	how := exitHow(payload)
	// AN END ATRIUM CAUSED IS NOT A SURPRISE. An exit the launcher or operator asked for, a room shutdown and an
	// idle park are logged and move nothing: the work is exactly as unreported as it was, and it stays open so
	// a later death of the resumed session is still noticed.
	_, expected := expectedEnd(payload)
	moved := (w.State == WorkOpen || w.State == WorkReopened) && !expected
	if moved {
		if _, err := tx.Exec(`UPDATE work_item SET ended_generation = generation, state = ?, state_at = ?,
				state_by = ?, revision = revision + 1 WHERE task_id = ?`,
			WorkEnded, ts(now()), ByAtrium, w.TaskID); err != nil {
			return err
		}
	} else if _, err := tx.Exec(`UPDATE work_item SET ended_generation = generation WHERE task_id = ?`,
		w.TaskID); err != nil {
		return err
	}
	text := fmt.Sprintf("the session ended (%s), generation %d", how, w.Generation)
	if moved {
		text = fmt.Sprintf("ended without a final report (%s), generation %d", how, w.Generation)
	}
	if err := failpoint("ended-item"); err != nil {
		return err
	}
	if _, err := s.insertLog(tx, w.TaskID, logRow{kind: LogAtrium, by: ByAtrium, text: text}); err != nil {
		return err
	}
	if moved {
		last, err := lastReportOn(tx, w.TaskID)
		if err != nil {
			return err
		}
		body := endedNotice(w, how, at, last)
		if err := failpoint("ended-notice"); err != nil {
			return err
		}
		if _, err := s.queueNotice(tx, w, NoticeEnded, strconv.Itoa(w.Generation), body); err != nil {
			return err
		}
	}
	s.ledgerChanged(tx, w.TaskID)
	return nil
}

// Why atrium ended a session, in an exit payload's `cause`.
const (
	CauseAsked    = "asked"
	CauseShutdown = "shutdown"
	CauseIdlePark = "idle-park"
	CauseShelved  = "shelved"
	CauseKilled   = "terminated"
)

// expectedEnd says whether an exit's payload names a cause atrium itself brought about, and the words for it.
func expectedEnd(payload map[string]any) (string, bool) {
	by := payloadWord(payload, "cause_by", "")
	switch payloadWord(payload, "cause", "") {
	case CauseAsked:
		return "asked to exit by " + orKeep(by, "the operator"), true
	case CauseShutdown:
		return "the room shut down", true
	case CauseIdlePark:
		return "parked after sitting idle", true
	case CauseShelved:
		return "shelved by " + orKeep(by, "the operator"), true
	case CauseKilled:
		return "terminated by " + orKeep(by, "the operator"), true
	}
	return "", false
}

// exitHow is how a session ended, in the words its exit path used.
func exitHow(payload map[string]any) string {
	if how, ok := expectedEnd(payload); ok {
		return how
	}
	if d := payloadWord(payload, "detected", ""); d != "" {
		return d
	}
	if code, ok := payload["exit_code"].(float64); ok {
		return fmt.Sprintf("exited with code %d", int(code))
	}
	return payloadWord(payload, "by", "it exited")
}

func payloadWord(payload map[string]any, key, def string) string {
	if v, ok := payload[key].(string); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return def
}

// endedNotice is what the arbiter reads when work ends without a report.
func endedNotice(w *WorkItem, how string, at time.Time, last *WorkLogEntry) string {
	body := fmt.Sprintf("%s ended without a final report (%s at %s). nobody asked it to exit and it never said "+
		"done or blocked, so this end is unexpected. last report: ",
		orKeep(w.Handle, w.TaskID), how, at.Local().Format("15:04"))
	if last == nil {
		body += "none"
	} else {
		body += fmt.Sprintf("%s %s %s", orKeep(last.Status, "done"), last.At.Local().Format("15:04"),
			reportWords(last))
	}
	return body + ". outputs: " + w.Outputs.Summary() + ". card " + w.TaskID
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return cutRunes(s, 200)
}

// queueNotice puts a notice on the arbiter's message queue inside the
// transaction that caused it, once per (item, source, key). Both commit or
// neither does, so a crash can lose neither half. Delivery is the queue's own
// job, after the commit. An arbiter whose card is gone gets nothing, and the
// item stays where the board shows it.
//
// AN ARBITER ON ANOTHER ROOM is not a card here: the lineage records it as
// `room~id`. Its notice is held in the relay outbox in the same transaction,
// the way a report's is, and sent from there (item 62). Before, it was logged
// as having nowhere to go, and a worker whose launcher was on another room
// ended with nothing reaching the launcher.
func (s *Store) queueNotice(tx *Tx, w *WorkItem, source, key, body string) (*LedgerNotice, error) {
	if w.ArbiterID == "" {
		return nil, nil
	}
	arbiter, err := getByOn(tx, `id = ?`, w.ArbiterID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, s.relayNotice(tx, w, source, key, body)
	} else if err != nil {
		return nil, err
	}
	fresh, err := claimNotice(tx, w.TaskID, source, key)
	if err != nil || !fresh {
		return nil, err
	}
	from := orKeep(w.Handle, "atrium")
	// A HELD NOTICE is recorded on the arbiter's card and never queued, so nothing
	// types it and no hook carries it. The arbiter reads it when it asks.
	if s.HoldNotice != nil && s.HoldNotice(arbiter, source) {
		ev := HeldNoticePayload(source, from, w.TaskID, body)
		if _, err := s.appendEventOn(tx, w.ArbiterID, EventNotified, ev); err != nil {
			return nil, err
		}
		n := &LedgerNotice{TaskID: w.TaskID, ArbiterID: w.ArbiterID, From: from, Text: body, Source: source,
			Held: true}
		tx.afterCommit(func() {
			if cb := s.OnLedgerNotice; cb != nil {
				cb(*n)
			}
		})
		return n, nil
	}
	m := &Message{ID: newID(), TaskID: w.ArbiterID, Text: body, CreatedAt: now(), FromPeer: from}
	// A frozen arbiter keeps the notice in its freeze queue. See enqueueFrozenOn.
	held, err := enqueueFrozenOn(tx, m)
	if err != nil {
		return nil, err
	}
	if !held {
		if _, err := tx.Exec(`INSERT INTO message (id, task_id, text, created_at, from_peer) VALUES (?,?,?,?,?)`,
			m.ID, m.TaskID, m.Text, ts(m.CreatedAt), m.FromPeer); err != nil {
			return nil, err
		}
		if _, err := s.appendEventOn(tx, w.ArbiterID, EventPrompted, map[string]any{
			"queued": true, "text": body, "from_peer": from,
		}); err != nil {
			return nil, err
		}
	}
	n := &LedgerNotice{TaskID: w.TaskID, ArbiterID: w.ArbiterID, MessageID: m.ID, From: from, Text: body,
		Source: source}
	tx.afterCommit(func() {
		if cb := s.OnLedgerNotice; cb != nil {
			cb(*n)
		}
	})
	return n, nil
}

// claimNotice is the once-per-(worker, source, key) claim, and whether this
// is the first.
func claimNotice(tx *Tx, workerID, source, key string) (bool, error) {
	res, err := tx.Exec(`INSERT INTO a2a_notice (worker_id, source, key, created_at) VALUES (?,?,?,?)
		ON CONFLICT (worker_id, source, key) DO NOTHING`, workerID, source, key, ts(now()))
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// relayNotice holds a ledger notice for an arbiter on another room. See
// queueNotice.
func (s *Store) relayNotice(tx *Tx, w *WorkItem, source, key, body string) error {
	worker, err := getByOn(tx, `id = ?`, w.TaskID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	} else if err != nil {
		return err
	}
	var spec *RelaySpec
	if s.RemoteArbiter != nil {
		spec = s.RemoteArbiter(worker, body)
	}
	if spec == nil {
		log.Printf("store: %s notice for %s has no arbiter card to go to", source, w.TaskID)
		return nil
	}
	fresh, err := claimNotice(tx, w.TaskID, source, key)
	if err != nil || !fresh {
		return err
	}
	if err := s.holdRelayOn(tx, *spec); err != nil {
		return err
	}
	tx.afterCommit(func() {
		if cb := s.OnRelayHeld; cb != nil {
			cb()
		}
	})
	return nil
}

// failpoint lets a test fail a change between two of its writes. Nil in a
// running daemon.
var failpointHook func(step string) error

func failpoint(step string) error {
	if failpointHook == nil {
		return nil
	}
	return failpointHook(step)
}

// ── reports ─────────────────────────────────────────────────────────────────────

// ReportWrite is everything one worker report records, written in one
// transaction. It is `finish`'s writes, which used to be five separate
// statements, and the ledger's.
type ReportWrite struct {
	TaskID string
	// UnheardIsUnsent is set for an agent-launched worker: its report, if nobody is queued to hear
	// it, is not stamped as sent. A card the operator launched is stamped either way, which is what
	// the board's "report waiting" and "Last report" read.
	UnheardIsUnsent bool
	// Recap is the composed recap, bounded like SetRecap.
	Recap string
	// SetSHA writes SHA and Unverified onto the card, for a `done`.
	SetSHA     bool
	SHA        string
	Unverified bool
	// Event is the `submitted` event's payload.
	Event map[string]any
	// Status and Reason move the card when MoveStatus is set.
	MoveStatus     bool
	Status, Reason string
	// ReportStatus is what the worker said: done, progress, blocked,
	// question, or needs-input.
	ReportStatus string
	Outputs      *WorkOutputs
	// SayReport is a `done` or `blocked` say to the launcher standing as the report. Both move the item.
	SayReport bool
	// Notice goes to the launcher inside the same transaction, once per
	// (Source, Key). Nil for a card nobody launched.
	Notice *NoticeSpec
	// Relay is the same notice for a launcher on ANOTHER room, held in the
	// relay outbox in the same transaction, so a crash cannot record the report
	// and lose the notice either. See relay.go.
	Relay *RelaySpec
}

// NoticeSpec is a notice a report queues to whoever hears about the card.
type NoticeSpec struct {
	ToID, From, Source, Key, Text string
}

// ReportResult is what RecordReport did.
type ReportResult struct {
	// Item is the card's work item after the report, nil when it has none.
	Item *WorkItem
	// Duplicate is a retried report, recorded once.
	Duplicate bool
	// Notice is the notice queued, nil when none was.
	Notice *LedgerNotice
	// Relayed is a notice held for a launcher on another room.
	Relayed bool
}

// RecordReport writes a report, the card's own fields and the ledger's, in
// one transaction. A failure between any two of them rolls all of them back.
func (s *Store) RecordReport(r ReportWrite) (*ReportResult, error) {
	recap, recapAt := boundRecap(r.Recap)
	var out *ReportResult
	err := s.inTx(func(tx *Tx) error {
		out = &ReportResult{}
		if _, err := tx.Exec(`UPDATE task SET recap = ?, recap_at = ? WHERE id = ?`,
			recap, recapAt, r.TaskID); err != nil {
			return err
		}
		if err := failpoint("recap"); err != nil {
			return err
		}
		if r.SetSHA {
			sha := strings.TrimSpace(r.SHA)
			flag := 0
			if sha != "" && r.Unverified {
				flag = 1
			}
			if _, err := tx.Exec(`UPDATE task SET report_sha = ?, report_unverified = ? WHERE id = ?`,
				sha, flag, r.TaskID); err != nil {
				return err
			}
		}
		if err := failpoint("sha"); err != nil {
			return err
		}
		if _, err := s.appendEventOn(tx, r.TaskID, EventSubmitted, r.Event); err != nil {
			return err
		}
		if err := failpoint("event"); err != nil {
			return err
		}
		if r.MoveStatus {
			if err := s.setStatusOn(tx, r.TaskID, r.Status, r.Reason); err != nil {
				return err
			}
		}
		if err := failpoint("status"); err != nil {
			return err
		}
		// STAMPED ONLY WHEN SOMEBODY IS TOLD. A report nobody was queued to hear is not a report
		// the board may call sent: "report waiting" with a launcher that heard nothing was the
		// r-scm-clone failure. See docs/backlog/runtime/r-new-report-no-launcher.md.
		if r.Notice != nil || r.Relay != nil || !r.UnheardIsUnsent {
			if _, err := tx.Exec(`UPDATE task SET reported_at = ? WHERE id = ?`, ts(now()), r.TaskID); err != nil {
				return err
			}
		}
		if err := failpoint("reported"); err != nil {
			return err
		}
		// The log keeps the report to its own bound, MaxWorkText, rather than
		// the card's shorter recap.
		if err := s.ledgerReport(tx, r, strings.TrimSpace(r.Recap), out); err != nil {
			return err
		}
		if err := failpoint("ledger"); err != nil {
			return err
		}
		if r.Notice != nil && !out.Duplicate {
			n, err := s.queueReportNotice(tx, r.TaskID, *r.Notice)
			if err != nil {
				return err
			}
			out.Notice = n
		}
		if r.Relay != nil && !out.Duplicate {
			if err := s.holdRelayOn(tx, *r.Relay); err != nil {
				return err
			}
			out.Relayed = true
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ledgerReport logs a report on the card's item and moves it. Only a `done`
// report moves anything. See the transition table in the design.
func (s *Store) ledgerReport(tx *Tx, r ReportWrite, summary string, out *ReportResult) error {
	w, err := workItemOn(tx, r.TaskID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	status := strings.ToLower(strings.TrimSpace(r.ReportStatus))
	if status == "" {
		status = "done"
	}
	done := status == "done" || (r.SayReport && status == "blocked")
	note, next := "", w.State
	if done {
		switch w.State {
		case WorkOpen, WorkReopened, WorkReported:
			next = WorkReported
		case WorkEnded:
			next, note = WorkReported, "after the session ended"
		default:
			note = "after close"
		}
	} else if WorkClosed(w.State) {
		note = "after close"
	}
	var outputs *WorkOutputs
	if r.Outputs != nil && !r.Outputs.empty() {
		o := r.Outputs.bounded()
		outputs = &o
	}
	id, err := s.insertLog(tx, r.TaskID, logRow{kind: LogReport, by: ByWorker, status: status, text: summary,
		note: note, outputs: outputs, clientID: reportClientID(w.Generation, status, summary, outputs)})
	if err != nil {
		return err
	}
	if id == "" {
		out.Duplicate = true
		out.Item = w
		return nil
	}
	if done && next == WorkReported {
		o := "{}"
		if outputs != nil {
			o = outputs.json()
		}
		if _, err := tx.Exec(`UPDATE work_item SET state = ?, state_at = ?, state_by = ?, revision = revision + 1,
				latest_report_id = ?, outputs = ? WHERE task_id = ?`,
			WorkReported, ts(now()), ByWorker, id, o, r.TaskID); err != nil {
			return err
		}
	}
	s.ledgerChanged(tx, r.TaskID)
	if out.Item, err = workItemOn(tx, r.TaskID); err != nil {
		return err
	}
	return nil
}

// reportClientID makes a retried report one row: the same words in the same
// generation are the same report.
func reportClientID(gen int, status, summary string, outputs *WorkOutputs) string {
	h := sha256.New()
	fmt.Fprintf(h, "%d\x00%s\x00%s\x00", gen, status, summary)
	if outputs != nil {
		h.Write([]byte(outputs.json()))
	}
	return hex.EncodeToString(h.Sum(nil))[:32]
}

// queueReportNotice queues a report's notice to the card's arbiter, or to the
// launcher the caller named when the card has no item.
func (s *Store) queueReportNotice(tx *Tx, taskID string, n NoticeSpec) (*LedgerNotice, error) {
	w, err := workItemOn(tx, taskID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		w = &WorkItem{TaskID: taskID, ArbiterID: n.ToID, Handle: n.From}
	case err != nil:
		return nil, err
	default:
		if w.ArbiterID == "" {
			w.ArbiterID = n.ToID
		}
		w.Handle = orKeep(n.From, w.Handle)
	}
	return s.queueNotice(tx, w, n.Source, n.Key, n.Text)
}

// boundRecap is SetRecap's bound and timestamp, shared with RecordReport.
func boundRecap(recap string) (string, string) {
	recap = strings.TrimSpace(recap)
	if len(recap) > MaxRecap {
		recap = strings.TrimSpace(cutRunes(recap, MaxRecap)) + "..."
	}
	at := ""
	if recap != "" {
		at = ts(now())
	}
	return recap, at
}

// ── messages between a worker and its launcher ──────────────────────────────────

// LogWorkMessage records a peer message on the work it is about: a worker
// saying something to its arbiter or launcher is a `say` on its own item, and
// the arbiter or launcher saying something to a worker is an `instruction` on
// the worker's. Anything else is not about delegated work and is not logged.
// Verbatim, so a changed brief is on record. Returns whether it logged.
func (s *Store) LogWorkMessage(fromID, toID, byHandle, text string) (bool, error) {
	if fromID == "" || toID == "" || fromID == toID || strings.TrimSpace(text) == "" {
		return false, nil
	}
	logged := false
	err := s.inTx(func(tx *Tx) error {
		logged = false
		pairs := []struct{ item, other, kind string }{
			{fromID, toID, LogSay},
			{toID, fromID, LogInstruction},
		}
		for _, p := range pairs {
			w, err := workItemOn(tx, p.item)
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			if err != nil {
				return err
			}
			if p.other != w.ArbiterID && p.other != w.LauncherID {
				continue
			}
			id, err := s.insertLog(tx, p.item, logRow{kind: p.kind, by: byHandle, text: text})
			if err != nil {
				return err
			}
			if id != "" {
				logged = true
				if p.kind == LogSay {
					if status := sayReportStatus(text); status != "" {
						if err := s.ledgerReport(tx, ReportWrite{TaskID: p.item, ReportStatus: status,
							Recap: text, SayReport: true}, text, &ReportResult{}); err != nil {
							return err
						}
					}
				} else if w.State == WorkEnded {
					// New work from the launcher reopens an item that ended without a report.
					if _, err := tx.Exec(`UPDATE work_item SET state = ?, state_at = ?, state_by = ?,
							revision = revision + 1 WHERE task_id = ?`,
						WorkOpen, ts(now()), ByAtrium, p.item); err != nil {
						return err
					}
				}
				s.ledgerChanged(tx, p.item)
			}
		}
		return nil
	})
	return logged, err
}

// sayReportStatus is `done` or `blocked` when a say is the worker's final word to its launcher: the first
// word of the text, at its start. Anything else is free text and reports nothing.
func sayReportStatus(text string) string {
	t := strings.ToLower(strings.TrimSpace(text))
	for _, status := range []string{"done", "blocked"} {
		if !strings.HasPrefix(t, status) {
			continue
		}
		rest := t[len(status):]
		if rest == "" || strings.ContainsAny(rest[:1], " \t\n:,.;-") {
			return status
		}
	}
	return ""
}

// ReportedSince says whether the card has a report on its item at or after `since`.
func (s *Store) ReportedSince(taskID string, since time.Time) bool {
	e, err := lastReportOn(s.db, taskID)
	return err == nil && e != nil && !e.At.Before(since)
}

// LogWorkAtrium puts one of atrium's own lines on a card's work item, and does
// nothing for a card with none.
func (s *Store) LogWorkAtrium(taskID, text string) error {
	return s.inTx(func(tx *Tx) error {
		if _, err := workItemOn(tx, taskID); errors.Is(err, sql.ErrNoRows) {
			return nil
		} else if err != nil {
			return err
		}
		id, err := s.insertLog(tx, taskID, logRow{kind: LogAtrium, by: ByAtrium, text: text})
		if err == nil && id != "" {
			s.ledgerChanged(tx, taskID)
		}
		return err
	})
}

// ── ending a generation the sweep found gone ────────────────────────────────────

// Liveness is whether a card's session is there. Only gone ends work.
type Liveness string

const (
	Live        Liveness = "live"
	Gone        Liveness = "gone"
	LiveUnknown Liveness = "unknown"
)
