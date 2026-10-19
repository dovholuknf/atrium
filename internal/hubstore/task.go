package hubstore

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Task kinds. `code` is the column's other value, refused until stage T2.
const (
	TaskScratch = "scratch"
	TaskCode    = "code"
)

// Task states.
const (
	TaskPending       = "pending"
	TaskHeld          = "held"
	TaskRunning       = "running"
	TaskCapturing     = "capturing"
	TaskCaptured      = "captured"
	TaskCaptureFailed = "capture-failed"
	TaskFailed        = "failed"
	TaskUnconfirmed   = "unconfirmed"
	TaskCancelled     = "cancelled"
)

// Fact kinds of a task.
const (
	FactTaskStarted       = "task-started"
	FactTaskDone          = "task-done"
	FactTaskCaptured      = "task-captured"
	FactTaskCaptureFailed = "task-capture-failed"
	FactTaskReaped        = "task-reaped"
	FactTaskFailed        = "task-failed"
	FactTaskCancelled     = "task-cancelled"
	FactLearning          = "learning"
	FactLearningCapped    = "task-learning-capped"
)

// Bounds, in bytes unless said.
const (
	TaskTitleMax    = 200
	TaskBriefMax    = 8 << 10
	TaskContextMax  = 2 << 10
	TaskOutputsMax  = 20
	TaskOutputMax   = 200
	TaskLineMax     = 400
	TaskWhyMax      = 1000
	TaskLearnedMax  = 1500
	TaskRecapMax    = 4 << 10
	TaskReasonMax   = 500
	TaskFolderMax   = 500
	LearningEntries = 50
	LearningBytes   = 20 << 10
	// LearningBriefBytes is how much of the newest learnings a task's brief carries.
	LearningBriefBytes = 4 << 10
)

// ErrNoTask is a task the item does not have.
var ErrNoTask = errors.New("no such task")

// TaskFile is one captured file: its name in the folder, the hub file that holds it, its size and its hash.
type TaskFile struct {
	Path string `json:"path"`
	Name string `json:"name"`
	Size int64  `json:"size"`
	SHA  string `json:"sha256"`
}

// Task is one row.
type Task struct {
	BacklogID string     `json:"backlog_id"`
	TaskID    string     `json:"task_id"`
	Title     string     `json:"title"`
	Brief     string     `json:"brief,omitempty"`
	Kind      string     `json:"kind"`
	Repo      string     `json:"repo,omitempty"`
	Base      string     `json:"base,omitempty"`
	After     []string   `json:"after"`
	Outputs   []string   `json:"outputs"`
	Room      string     `json:"room,omitempty"`
	Model     string     `json:"model,omitempty"`
	State     string     `json:"state"`
	Attempt   int        `json:"attempt"`
	Card      string     `json:"card,omitempty"`
	Folder    string     `json:"folder,omitempty"`
	Branch    string     `json:"branch,omitempty"`
	SHA       string     `json:"sha,omitempty"`
	Diffstat  string     `json:"diffstat,omitempty"`
	Line      string     `json:"line,omitempty"`
	Why       string     `json:"why,omitempty"`
	Learned   string     `json:"learned,omitempty"`
	Recap     string     `json:"recap,omitempty"`
	Files     []TaskFile `json:"files"`
	Reason    string     `json:"reason,omitempty"`
	CreatedBy string     `json:"created_by,omitempty"`
	CreatedAt string     `json:"created_at"`
	StartedAt string     `json:"started_at,omitempty"`
	EndedAt   string     `json:"ended_at,omitempty"`
}

// TaskInput is what an owner gives to add a task.
type TaskInput struct {
	BacklogID string
	Title     string
	Brief     string
	Kind      string
	Repo      string
	Base      string
	After     []string
	Outputs   []string
	Room      string
	Model     string
	CreatedBy string
}

const taskCols = `backlog_id, task_id, title, brief, kind, repo, base, after, outputs, room, model, state, attempt, card,
	folder, branch, sha, diffstat, line, why, learned, recap, files, reason, created_by, created_at, started_at, ended_at`

func scanTask(sc scanner) (Task, error) {
	var t Task
	var after, outputs, files string
	err := sc.Scan(&t.BacklogID, &t.TaskID, &t.Title, &t.Brief, &t.Kind, &t.Repo, &t.Base, &after, &outputs, &t.Room,
		&t.Model, &t.State, &t.Attempt, &t.Card, &t.Folder, &t.Branch, &t.SHA, &t.Diffstat, &t.Line, &t.Why, &t.Learned,
		&t.Recap, &files, &t.Reason, &t.CreatedBy, &t.CreatedAt, &t.StartedAt, &t.EndedAt)
	if err != nil {
		return t, err
	}
	_ = json.Unmarshal([]byte(after), &t.After)
	_ = json.Unmarshal([]byte(outputs), &t.Outputs)
	_ = json.Unmarshal([]byte(files), &t.Files)
	if t.After == nil {
		t.After = []string{}
	}
	if t.Outputs == nil {
		t.Outputs = []string{}
	}
	if t.Files == nil {
		t.Files = []TaskFile{}
	}
	return t, nil
}

func jsonList(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "[]"
	}
	return string(b)
}

func clipBytes(s string, max int) string {
	if len(s) <= max {
		return s
	}
	s = s[:max]
	for len(s) > 0 && s[len(s)-1]&0xC0 == 0x80 {
		s = s[:len(s)-1]
	}
	return s
}

func CheckOutput(o string) string {
	o = strings.TrimSpace(o)
	switch {
	case o == "":
		return "an outputs entry is empty"
	case len(o) > TaskOutputMax:
		return fmt.Sprintf("an outputs entry is over %d bytes", TaskOutputMax)
	case strings.ContainsAny(o, "\x00\r\n"):
		return "an outputs entry has a control character"
	case strings.HasPrefix(o, "/") || strings.HasPrefix(o, "\\") || strings.Contains(o, ":"):
		return "outputs are relative to the task folder: " + o + " is not"
	}
	for _, seg := range strings.FieldsFunc(o, func(r rune) bool { return r == '/' || r == '\\' }) {
		if seg == ".." {
			return "outputs stay inside the task folder: " + o + " goes up"
		}
	}
	return ""
}

func checkTaskInput(in TaskInput) error {
	switch {
	case strings.TrimSpace(in.BacklogID) == "":
		return errors.New("a task belongs to an item")
	case strings.TrimSpace(in.Title) == "" || len(in.Title) > TaskTitleMax || strings.ContainsAny(in.Title, "\r\n"):
		return fmt.Errorf("a task has a one line title of up to %d bytes", TaskTitleMax)
	case strings.TrimSpace(in.Brief) == "":
		return errors.New("a task has a brief: the whole of what its worker is told")
	case len(in.Brief) > TaskBriefMax:
		return fmt.Errorf("the brief is %d bytes, over %d", len(in.Brief), TaskBriefMax)
	case in.Kind != TaskScratch && in.Kind != TaskCode:
		return errors.New("a task's kind is scratch or code")
	case len(in.Outputs) > TaskOutputsMax:
		return fmt.Errorf("a task keeps at most %d outputs", TaskOutputsMax)
	}
	for _, o := range in.Outputs {
		if why := CheckOutput(o); why != "" {
			return errors.New(why)
		}
	}
	return nil
}

func taskNumber(id string) int {
	n, _ := strconv.Atoi(strings.TrimPrefix(id, "t"))
	return n
}

// TaskAdd adds a task to an item and numbers it t1, t2, ... An item the hub has no work row for is ErrNoWork.
func (s *Store) TaskAdd(in TaskInput) (Task, error) {
	in.BacklogID = strings.TrimSpace(in.BacklogID)
	in.Title = strings.TrimSpace(in.Title)
	in.Brief = strings.TrimSpace(in.Brief)
	if in.Kind == "" {
		in.Kind = TaskScratch
	}
	outs := make([]string, 0, len(in.Outputs))
	for _, o := range in.Outputs {
		outs = append(outs, strings.TrimSpace(o))
	}
	in.Outputs = outs
	if err := checkTaskInput(in); err != nil {
		return Task{}, refuse(err)
	}
	after := in.After
	if after == nil {
		after = []string{}
	}
	var out Task
	err := s.tx(func(t *sql.Tx) error {
		var have int
		if err := t.QueryRow(`SELECT COUNT(*) FROM work WHERE backlog_id = ?`, in.BacklogID).Scan(&have); err != nil {
			return err
		}
		if have == 0 {
			return refuse(ErrNoWork)
		}
		rows, err := t.Query(`SELECT task_id, after FROM work_task WHERE backlog_id = ?`, in.BacklogID)
		if err != nil {
			return err
		}
		max := 0
		known := map[string]bool{}
		for rows.Next() {
			var id, a string
			if err := rows.Scan(&id, &a); err != nil {
				_ = rows.Close()
				return err
			}
			known[id] = true
			if n := taskNumber(id); n > max {
				max = n
			}
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return err
		}
		_ = rows.Close()
		for _, a := range after {
			if !known[a] {
				return refuse(fmt.Errorf("after names %q, which is not a task of this item", a))
			}
		}
		id := "t" + strconv.Itoa(max+1)
		_, err = t.Exec(`INSERT INTO work_task (backlog_id, task_id, title, brief, kind, repo, base, after, outputs, room, model,
			state, created_by, created_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			in.BacklogID, id, in.Title, in.Brief, in.Kind, in.Repo, in.Base, jsonList(after), jsonList(in.Outputs),
			fold(in.Room), strings.TrimSpace(in.Model), TaskPending, in.CreatedBy, ts(s.now()))
		if err != nil {
			return err
		}
		out, err = scanTask(t.QueryRow(`SELECT `+taskCols+` FROM work_task WHERE backlog_id = ? AND task_id = ?`,
			in.BacklogID, id))
		return err
	})
	return out, err
}

// TaskGet reads one task, or ErrNoTask.
func (s *Store) TaskGet(id, taskID string) (Task, error) {
	var t Task
	err := s.guard(func() error {
		var err error
		t, err = scanTask(s.db.QueryRow(`SELECT `+taskCols+` FROM work_task WHERE backlog_id = ? AND task_id = ?`,
			strings.TrimSpace(id), strings.TrimSpace(taskID)))
		return err
	})
	if errors.Is(err, sql.ErrNoRows) {
		return Task{}, ErrNoTask
	}
	return t, err
}

// TaskList is every task of an item, by number.
func (s *Store) TaskList(id string) ([]Task, error) {
	var out []Task
	err := s.guard(func() error {
		out = nil
		rows, err := s.db.Query(`SELECT `+taskCols+` FROM work_task WHERE backlog_id = ?`, strings.TrimSpace(id))
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
	sort.Slice(out, func(i, j int) bool { return taskNumber(out[i].TaskID) < taskNumber(out[j].TaskID) })
	return out, err
}

// TaskItemsWithPending is the items that have a task waiting for its turn.
func (s *Store) TaskItemsWithPending() ([]string, error) {
	var out []string
	err := s.guard(func() error {
		out = nil
		rows, err := s.db.Query(`SELECT DISTINCT backlog_id FROM work_task WHERE state = ? ORDER BY backlog_id`, TaskPending)
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

// TaskBusy is whether the item has a task a worker is on, or may be on: one at a time per item.
func (s *Store) TaskBusy(id string) (bool, error) {
	var n int
	err := s.guard(func() error {
		return s.db.QueryRow(`SELECT COUNT(*) FROM work_task WHERE backlog_id = ? AND state IN (?,?,?)`,
			strings.TrimSpace(id), TaskRunning, TaskCapturing, TaskUnconfirmed).Scan(&n)
	})
	return n > 0, err
}

// TaskContext is the owner's words every task's brief of this item carries.
func (s *Store) TaskContext(id string) (string, error) {
	var c string
	err := s.guard(func() error {
		return s.db.QueryRow(`SELECT task_context FROM work WHERE backlog_id = ?`, strings.TrimSpace(id)).Scan(&c)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNoWork
	}
	return c, err
}

// SetTaskContext sets it, up to TaskContextMax bytes.
func (s *Store) SetTaskContext(id, text string) error {
	text = strings.TrimSpace(text)
	if len(text) > TaskContextMax {
		return refuse(fmt.Errorf("the task context is %d bytes, over %d", len(text), TaskContextMax))
	}
	return s.tx(func(t *sql.Tx) error {
		res, err := t.Exec(`UPDATE work SET task_context = ? WHERE backlog_id = ?`, text, strings.TrimSpace(id))
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return refuse(ErrNoWork)
		}
		return nil
	})
}

func taskFact(t *sql.Tx, now, id, kind, ref, card, payload string) error {
	_, err := t.Exec(`INSERT OR IGNORE INTO work_fact (backlog_id, kind, source, card, ref, at_sha, dirty, at, payload)
		VALUES (?,?,?,?,?,'',0,?,?)`, id, kind, SourceSeen, card, ref, now, payload)
	return err
}

func taskRef(taskID string, attempt int) string { return taskID + "-a" + strconv.Itoa(attempt) }

func taskPayload(m map[string]any) string {
	b, _ := json.Marshal(m)
	return string(b)
}

// moveTask runs a guarded UPDATE of one task, and says whether it changed a row.
func moveTask(t *sql.Tx, id, taskID string, from []string, set string, args ...any) (bool, error) {
	ph := strings.TrimSuffix(strings.Repeat("?,", len(from)), ",")
	all := append(append([]any{}, args...), id, taskID)
	for _, f := range from {
		all = append(all, f)
	}
	res, err := t.Exec(`UPDATE work_task SET `+set+` WHERE backlog_id = ? AND task_id = ? AND state IN (`+ph+`)`, all...)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (s *Store) TaskBegin(id, taskID, room string) (Task, bool, error) {
	var out Task
	moved := false
	err := s.tx(func(t *sql.Tx) error {
		var err error
		moved, err = moveTask(t, id, taskID, []string{TaskPending},
			`state = ?, attempt = attempt + 1, card = '', folder = '', reason = '', started_at = ?, ended_at = '',
			room = CASE WHEN room = '' THEN ? ELSE room END`,
			TaskRunning, ts(s.now()), fold(room))
		if err != nil || !moved {
			return err
		}
		out, err = scanTask(t.QueryRow(`SELECT `+taskCols+` FROM work_task WHERE backlog_id = ? AND task_id = ?`, id, taskID))
		return err
	})
	return out, moved, err
}

func (s *Store) TaskWait(id, taskID, reason string) error {
	return s.tx(func(t *sql.Tx) error {
		_, err := moveTask(t, id, taskID, []string{TaskRunning},
			`state = ?, attempt = attempt - 1, reason = ?, started_at = ''`, TaskPending, clipBytes(reason, TaskReasonMax))
		return err
	})
}

// TaskNote says why a pending task waits, without moving it.
func (s *Store) TaskNote(id, taskID, reason string) error {
	return s.tx(func(t *sql.Tx) error {
		_, err := moveTask(t, id, taskID, []string{TaskPending}, `reason = ?`, clipBytes(reason, TaskReasonMax))
		return err
	})
}

// TaskLaunched records the card a launch made. unconfirmed is a launch whose answer was lost: it is never sent again.
func (s *Store) TaskLaunched(id, taskID, card string, unconfirmed bool, reason string) error {
	return s.tx(func(t *sql.Tx) error {
		state := TaskRunning
		if unconfirmed {
			state = TaskUnconfirmed
		}
		ok, err := moveTask(t, id, taskID, []string{TaskRunning}, `state = ?, card = ?, reason = ?`, state, card,
			clipBytes(reason, TaskReasonMax))
		if err != nil || !ok {
			return err
		}
		var attempt int
		if err := t.QueryRow(`SELECT attempt FROM work_task WHERE backlog_id = ? AND task_id = ?`, id, taskID).Scan(&attempt); err != nil {
			return err
		}
		return taskFact(t, ts(s.now()), id, FactTaskStarted, taskRef(taskID, attempt), card,
			taskPayload(map[string]any{"task": taskID, "attempt": attempt, "unconfirmed": unconfirmed}))
	})
}

// TaskLaunchFailed ends a launch the room refused: the task is failed with the sentence.
func (s *Store) TaskLaunchFailed(id, taskID, reason string) error {
	return s.tx(func(t *sql.Tx) error {
		ok, err := moveTask(t, id, taskID, []string{TaskRunning}, `state = ?, reason = ?, ended_at = ?`, TaskFailed,
			clipBytes(reason, TaskReasonMax), ts(s.now()))
		if err != nil || !ok {
			return err
		}
		var attempt int
		if err := t.QueryRow(`SELECT attempt FROM work_task WHERE backlog_id = ? AND task_id = ?`, id, taskID).Scan(&attempt); err != nil {
			return err
		}
		return taskFact(t, ts(s.now()), id, FactTaskFailed, taskRef(taskID, attempt), "",
			taskPayload(map[string]any{"task": taskID, "attempt": attempt, "reason": reason}))
	})
}

// TaskCardSeen is a card of this item's room reporting in: a task whose launch was lost is running on it.
func (s *Store) TaskCardSeen(id, room, card, since string) (bool, error) {
	moved := false
	err := s.tx(func(t *sql.Tx) error {
		moved = false
		rows, err := t.Query(`SELECT task_id, room, started_at FROM work_task WHERE backlog_id = ? AND state = ? AND card = ''`,
			id, TaskUnconfirmed)
		if err != nil {
			return err
		}
		var ids []string
		for rows.Next() {
			var tid, r, started string
			if err := rows.Scan(&tid, &r, &started); err != nil {
				_ = rows.Close()
				return err
			}
			if (r == "" || r == fold(room)) && since >= started {
				ids = append(ids, tid)
			}
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return err
		}
		_ = rows.Close()
		if len(ids) != 1 {
			return nil
		}
		moved, err = moveTask(t, id, ids[0], []string{TaskUnconfirmed}, `state = ?, card = ?, reason = ''`, TaskRunning,
			fold(room)+"~"+card)
		return err
	})
	return moved, err
}

// TaskSetHeld holds a pending task or releases a held one. False when the task is in neither state.
func (s *Store) TaskSetHeld(id, taskID string, hold bool) (bool, error) {
	moved := false
	err := s.tx(func(t *sql.Tx) error {
		var err error
		if hold {
			moved, err = moveTask(t, id, taskID, []string{TaskPending}, `state = ?, reason = ''`, TaskHeld)
		} else {
			moved, err = moveTask(t, id, taskID, []string{TaskHeld}, `state = ?, reason = ''`, TaskPending)
		}
		return err
	})
	return moved, err
}

// TaskCancel stops a task.
func (s *Store) TaskCancel(id, taskID, by string) (bool, error) {
	moved := false
	err := s.tx(func(t *sql.Tx) error {
		var err error
		moved, err = moveTask(t, id, taskID, []string{TaskPending, TaskHeld, TaskRunning, TaskUnconfirmed},
			`state = ?, reason = ?, ended_at = ?`, TaskCancelled, "cancelled by "+clipBytes(by, 80), ts(s.now()))
		if err != nil || !moved {
			return err
		}
		var attempt int
		if err := t.QueryRow(`SELECT attempt FROM work_task WHERE backlog_id = ? AND task_id = ?`, id, taskID).Scan(&attempt); err != nil {
			return err
		}
		return taskFact(t, ts(s.now()), id, FactTaskCancelled, taskRef(taskID, attempt), "",
			taskPayload(map[string]any{"task": taskID, "attempt": attempt, "by": by}))
	})
	return moved, err
}

// TaskRetry puts a failed task back to pending: its next launch is a new attempt.
func (s *Store) TaskRetry(id, taskID string) (bool, error) {
	moved := false
	err := s.tx(func(t *sql.Tx) error {
		var err error
		moved, err = moveTask(t, id, taskID, []string{TaskFailed}, `state = ?, reason = '', ended_at = ''`, TaskPending)
		return err
	})
	return moved, err
}

// TaskEvent is what a room reports about a task of its own.
type TaskEvent struct {
	BacklogID string
	TaskID    string
	Attempt   int
	Card      string
	Folder    string
	Reason    string
	Line      string
	Why       string
	Learned   string
	Recap     string
	Files     []TaskFile
	// By is who the room says: the card's handle, for the fact.
	By string
}

func (e TaskEvent) clean() TaskEvent {
	e.Folder = clipBytes(strings.TrimSpace(e.Folder), TaskFolderMax)
	e.Reason = clipBytes(strings.TrimSpace(e.Reason), TaskReasonMax)
	e.Line = clipBytes(strings.Join(strings.Fields(e.Line), " "), TaskLineMax)
	e.Why = clipBytes(strings.Join(strings.Fields(e.Why), " "), TaskWhyMax)
	e.Learned = clipBytes(strings.Join(strings.Fields(e.Learned), " "), TaskLearnedMax)
	e.Recap = clipBytes(strings.TrimSpace(e.Recap), TaskRecapMax)
	return e
}

// current is the attempt a report is about, and whether it is the task's.
func currentAttempt(t *sql.Tx, e TaskEvent) (Task, error) {
	task, err := scanTask(t.QueryRow(`SELECT `+taskCols+` FROM work_task WHERE backlog_id = ? AND task_id = ?`,
		e.BacklogID, e.TaskID))
	if errors.Is(err, sql.ErrNoRows) {
		return Task{}, refuse(ErrNoTask)
	}
	return task, err
}

// TaskCapturingNow is the room saying the worker called done and capture has begun.
func (s *Store) TaskCapturingNow(e TaskEvent) (bool, error) {
	e = e.clean()
	moved := false
	err := s.tx(func(t *sql.Tx) error {
		moved = false
		task, err := currentAttempt(t, e)
		if err != nil {
			return err
		}
		if task.Attempt != e.Attempt {
			return nil
		}
		moved, err = moveTask(t, e.BacklogID, e.TaskID, []string{TaskRunning, TaskUnconfirmed},
			`state = ?, folder = ?, card = CASE WHEN card = '' THEN ? ELSE card END, reason = ''`,
			TaskCapturing, e.Folder, e.Card)
		if err != nil || !moved {
			return err
		}
		return taskFact(t, ts(s.now()), e.BacklogID, FactTaskDone, taskRef(e.TaskID, e.Attempt), e.Card,
			taskPayload(map[string]any{"task": e.TaskID, "attempt": e.Attempt, "line": e.Line, "why": e.Why}))
	})
	return moved, err
}

// TaskCapturedAs is the room's captured result, with the files already on the hub.
func (s *Store) TaskCapturedAs(e TaskEvent) (Task, bool, error) {
	e = e.clean()
	var out Task
	applied := false
	err := s.tx(func(t *sql.Tx) error {
		applied = false
		task, err := currentAttempt(t, e)
		if err != nil {
			return err
		}
		out = task
		if task.Attempt != e.Attempt {
			return nil
		}
		now := ts(s.now())
		state := TaskCaptured
		switch task.State {
		case TaskRunning, TaskUnconfirmed, TaskCapturing, TaskCaptureFailed:
		case TaskCancelled:
			if task.Line != "" || len(task.Files) > 0 {
				return nil
			}
			state = TaskCancelled
		default:
			return nil
		}
		reason := ""
		if state == TaskCancelled {
			reason = task.Reason
		}
		res, err := t.Exec(`UPDATE work_task SET state = ?, line = ?, why = ?, learned = ?, recap = ?, files = ?, folder = '',
			card = CASE WHEN card = '' THEN ? ELSE card END, reason = ?, ended_at = ? WHERE backlog_id = ? AND task_id = ?`,
			state, e.Line, e.Why, e.Learned, e.Recap, jsonList(e.Files), e.Card, reason, now, e.BacklogID, e.TaskID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return nil
		}
		applied = true
		ref := taskRef(e.TaskID, e.Attempt)
		if err := taskFact(t, now, e.BacklogID, FactTaskDone, ref, e.Card,
			taskPayload(map[string]any{"task": e.TaskID, "attempt": e.Attempt, "line": e.Line, "why": e.Why})); err != nil {
			return err
		}
		if err := taskFact(t, now, e.BacklogID, FactTaskCaptured, ref, e.Card,
			taskPayload(map[string]any{"task": e.TaskID, "attempt": e.Attempt, "files": e.Files})); err != nil {
			return err
		}
		if e.Learned != "" {
			if err := addLearning(t, now, e.BacklogID, e.TaskID, e.Attempt, e.Learned); err != nil {
				return err
			}
		}
		out, err = scanTask(t.QueryRow(`SELECT `+taskCols+` FROM work_task WHERE backlog_id = ? AND task_id = ?`,
			e.BacklogID, e.TaskID))
		return err
	})
	return out, applied, err
}

func (s *Store) TaskCaptureFailedAs(e TaskEvent) (Task, bool, error) {
	e = e.clean()
	var out Task
	applied := false
	err := s.tx(func(t *sql.Tx) error {
		applied = false
		task, err := currentAttempt(t, e)
		if err != nil {
			return err
		}
		out = task
		if task.Attempt != e.Attempt {
			return nil
		}
		now := ts(s.now())
		switch task.State {
		case TaskRunning, TaskUnconfirmed, TaskCapturing:
			applied, err = moveTask(t, e.BacklogID, e.TaskID, []string{task.State},
				`state = ?, folder = ?, reason = ?, ended_at = ?, card = CASE WHEN card = '' THEN ? ELSE card END`,
				TaskCaptureFailed, e.Folder, e.Reason, now, e.Card)
		case TaskCancelled:
			applied, err = moveTask(t, e.BacklogID, e.TaskID, []string{TaskCancelled}, `folder = ?, reason = ?`, e.Folder,
				"cancelled, and the capture failed: "+e.Reason)
		}
		if err != nil || !applied {
			return err
		}
		if err := taskFact(t, now, e.BacklogID, FactTaskCaptureFailed, taskRef(e.TaskID, e.Attempt), e.Card,
			taskPayload(map[string]any{"task": e.TaskID, "attempt": e.Attempt, "reason": e.Reason})); err != nil {
			return err
		}
		out, err = scanTask(t.QueryRow(`SELECT `+taskCols+` FROM work_task WHERE backlog_id = ? AND task_id = ?`,
			e.BacklogID, e.TaskID))
		return err
	})
	return out, applied, err
}

// TaskFailedAs is the room saying a worker ended with no done. The folder stays.
func (s *Store) TaskFailedAs(e TaskEvent) (Task, bool, error) {
	e = e.clean()
	var out Task
	applied := false
	err := s.tx(func(t *sql.Tx) error {
		applied = false
		task, err := currentAttempt(t, e)
		if err != nil {
			return err
		}
		out = task
		if task.Attempt != e.Attempt {
			return nil
		}
		now := ts(s.now())
		switch task.State {
		case TaskRunning, TaskUnconfirmed:
			applied, err = moveTask(t, e.BacklogID, e.TaskID, []string{task.State},
				`state = ?, folder = ?, reason = ?, ended_at = ?, card = CASE WHEN card = '' THEN ? ELSE card END`,
				TaskFailed, e.Folder, e.Reason, now, e.Card)
		case TaskCancelled:
			_, err = moveTask(t, e.BacklogID, e.TaskID, []string{TaskCancelled}, `folder = ?`, e.Folder)
		}
		if err != nil || !applied {
			return err
		}
		if err := taskFact(t, now, e.BacklogID, FactTaskFailed, taskRef(e.TaskID, e.Attempt), e.Card,
			taskPayload(map[string]any{"task": e.TaskID, "attempt": e.Attempt, "reason": e.Reason})); err != nil {
			return err
		}
		out, err = scanTask(t.QueryRow(`SELECT `+taskCols+` FROM work_task WHERE backlog_id = ? AND task_id = ?`,
			e.BacklogID, e.TaskID))
		return err
	})
	return out, applied, err
}

// TaskReapedAs is the room saying the folder is gone, after the hub took the result.
func (s *Store) TaskReapedAs(e TaskEvent) (bool, error) {
	e = e.clean()
	wrote := false
	err := s.tx(func(t *sql.Tx) error {
		wrote = false
		if _, err := currentAttempt(t, e); err != nil {
			return err
		}
		res, err := t.Exec(`INSERT OR IGNORE INTO work_fact (backlog_id, kind, source, card, ref, at_sha, dirty, at, payload)
			VALUES (?,?,?,?,?,'',0,?,?)`, e.BacklogID, FactTaskReaped, SourceSeen, e.Card, taskRef(e.TaskID, e.Attempt),
			ts(s.now()), taskPayload(map[string]any{"task": e.TaskID, "attempt": e.Attempt, "reason": e.Reason}))
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		wrote = n > 0
		return nil
	})
	return wrote, err
}

// Learning is one entry on an item.
type Learning struct {
	Task    string `json:"task"`
	Attempt int    `json:"attempt"`
	Text    string `json:"text"`
	At      string `json:"at"`
}

// addLearning appends what a task's worker learned, under the cap of 50 entries or 20 KB on the item.
func addLearning(t *sql.Tx, now, id, taskID string, attempt int, text string) error {
	var n, bytes int
	if err := t.QueryRow(`SELECT COUNT(*), COALESCE(SUM(LENGTH(payload)), 0) FROM work_fact WHERE backlog_id = ? AND kind = ?`,
		id, FactLearning).Scan(&n, &bytes); err != nil {
		return err
	}
	if n >= LearningEntries || bytes+len(text) > LearningBytes {
		return taskFact(t, now, id, FactLearningCapped, taskRef(taskID, attempt), "",
			taskPayload(map[string]any{"task": taskID, "attempt": attempt, "entries": n, "bytes": bytes}))
	}
	return taskFact(t, now, id, FactLearning, taskRef(taskID, attempt), "",
		taskPayload(map[string]any{"task": taskID, "attempt": attempt, "text": text}))
}

// Learnings is the item's entries, newest first, as many as fit in max bytes (all when max is not positive).
func (s *Store) Learnings(id string, max int) ([]Learning, error) {
	facts, err := s.WorkFacts(id)
	if err != nil {
		return nil, err
	}
	var out []Learning
	used := 0
	for i := len(facts) - 1; i >= 0; i-- {
		f := facts[i]
		if f.Kind != FactLearning {
			continue
		}
		text := f.Note("text")
		if text == "" {
			continue
		}
		if max > 0 && used+len(text) > max && len(out) > 0 {
			break
		}
		if max > 0 && len(text) > max {
			text = clipBytes(text, max)
		}
		used += len(text)
		var m struct {
			Task    string `json:"task"`
			Attempt int    `json:"attempt"`
		}
		_ = json.Unmarshal([]byte(f.Payload), &m)
		out = append(out, Learning{Task: m.Task, Attempt: m.Attempt, Text: text, At: f.At})
	}
	return out, nil
}

// TaskFileFind is the drop file already held under this name with these bytes, so a replayed capture adds nothing.
func (s *Store) TaskFileFind(name, sha string) (FileRow, bool, error) {
	var r FileRow
	err := s.guard(func() error {
		var err error
		r, err = scanFile(s.db.QueryRow(`SELECT `+fileCols+` FROM hub_files WHERE name = ? AND sha = ? AND expires > ?`,
			name, sha, ts(s.now())))
		return err
	})
	if errors.Is(err, sql.ErrNoRows) {
		return FileRow{}, false, nil
	}
	return r, err == nil, err
}
