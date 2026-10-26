package hubstore

import (
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Backlog items and director reports, the rows half. See migration 0011 and internal/link/backlog.go.
//
//   - AN ITEM'S ID IS GIVEN BY THE FILER and is never reused. Filing an id that exists answers ErrItemExists with the
//     row as it is, so two rooms filing the same slug cannot overwrite each other.
//   - A REPORT IS APPEND-ONLY. It is added, listed and marked read. Nothing edits its words.

// Item statuses, as the CHECK constraint spells them.
const (
	ItemOpen       = "open"
	ItemHeld       = "held"
	ItemInProgress = "in-progress"
	ItemBuilt      = "built"
	ItemBlocked    = "blocked"
	ItemIncomplete = "incomplete"
	ItemDone       = "done"
	ItemDropped    = "dropped"
)

// The bounds of an item's and a report's words.
const (
	ItemTitleMax   = 200
	ItemBodyMax    = 64000
	ItemMaxPrio    = 20
	ReportSubjMax  = 200
	ReportBodyMax  = 16000
	backlogByMax   = 200
	backlogListMax = 1000
)

var (
	// ErrItemNotFound is an id the hub has never held.
	ErrItemNotFound = errors.New("no backlog item has that id")
	// ErrItemExists is a filing under an id that is taken. The row comes back with it.
	ErrItemExists = errors.New("a backlog item with that id already exists")
	// ErrReportNotFound is a report id the hub has never held.
	ErrReportNotFound = errors.New("no report has that id")

	itemIDRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,79}$`)
	deptRe   = regexp.MustCompile(`^[a-z][a-z0-9-]{0,39}$`)
)

// ValidItemStatus reports whether s is a status an item can have.
func ValidItemStatus(s string) bool {
	switch s {
	case ItemOpen, ItemHeld, ItemInProgress, ItemBuilt, ItemBlocked, ItemIncomplete, ItemDone, ItemDropped:
		return true
	}
	return false
}

// The priorities, as the CHECK constraint spells them. Unset is the empty string.
const (
	PrioHigh   = "high"
	PrioMedium = "medium"
	PrioLow    = "low"
)

// PriorityWords is what a refusal lists.
const PriorityWords = "high, medium, low, or unset (empty)"

// NormPriority folds what a caller typed to a priority, and says whether it is one. Case and surrounding space are
// forgiven (HIGH is high), and "unset" and "none" are the empty priority.
func NormPriority(s string) (string, bool) {
	switch p := strings.ToLower(strings.TrimSpace(s)); p {
	case PrioHigh, PrioMedium, PrioLow, "":
		return p, true
	case "unset", "none":
		return "", true
	}
	return "", false
}

func priorityRefusal(s string) error {
	return refuse(fmt.Errorf("priority %q is not allowed: it is %s", s, PriorityWords))
}

// BacklogItem is one row, as the API carries it.
type BacklogItem struct {
	ID        string `json:"id"`
	Dept      string `json:"dept"`
	Title     string `json:"title"`
	Body      string `json:"body,omitempty"`
	Priority  string `json:"priority,omitempty"`
	Status    string `json:"status"`
	Card      string `json:"card,omitempty"`
	FiledBy   string `json:"filed_by"`
	FiledRoom string `json:"filed_room"`
	CreatedAt string `json:"created_at"`
	ChangedAt string `json:"changed_at"`
	ChangedBy string `json:"changed_by"`
}

// ItemNew is an item to file.
type ItemNew struct {
	ID, Dept, Title, Body, Priority string
	FiledBy, FiledRoom              string
}

// Check says why an item cannot be filed, or "".
func (n ItemNew) Check() string {
	switch {
	case n.ID != "" && !itemIDRe.MatchString(n.ID):
		return "an item id is lower case letters, digits, dot, dash and underscore, like f-new-thing or r-037"
	case !deptRe.MatchString(n.Dept):
		return "a department is lower case letters and dashes, like fabric"
	case strings.TrimSpace(n.Title) == "":
		return "an item needs a title"
	}
	if _, ok := NormPriority(n.Priority); !ok {
		return priorityRefusal(n.Priority).Error()
	}
	if why := CRText("title", n.Title, ItemTitleMax, false); why != "" {
		return why
	}
	if why := CRText("body", n.Body, ItemBodyMax, true); why != "" {
		return why
	}
	if why := CRText("filer", n.FiledBy, backlogByMax, false); why != "" {
		return why
	}
	return ""
}

const itemCols = `id, dept, title, body, priority, status, card, filed_by, filed_room, created_at, changed_at, changed_by`

type rowScanner interface{ Scan(dest ...any) error }

func scanItem(sc rowScanner) (BacklogItem, error) {
	var b BacklogItem
	err := sc.Scan(&b.ID, &b.Dept, &b.Title, &b.Body, &b.Priority, &b.Status, &b.Card, &b.FiledBy, &b.FiledRoom,
		&b.CreatedAt, &b.ChangedAt, &b.ChangedBy)
	return b, err
}

// ItemFile files an item. An id that is taken answers ErrItemExists with the row that holds it.
func (s *Store) ItemFile(in ItemNew) (BacklogItem, error) {
	if why := in.Check(); why != "" {
		return BacklogItem{}, refuse(errors.New(why))
	}
	at := ts(s.now())
	in.Priority, _ = NormPriority(in.Priority)
	var out BacklogItem
	err := s.tx(func(t *sql.Tx) error {
		out = BacklogItem{}
		id := in.ID
		if id == "" {
			var err error
			if id, err = nextItemID(t, in.Dept); err != nil {
				return err
			}
		}
		prior, err := scanItem(t.QueryRow(`SELECT `+itemCols+` FROM backlog_item WHERE id = ?`, id))
		if err == nil {
			out = prior
			return refuse(ErrItemExists)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if _, err := t.Exec(`INSERT INTO backlog_item (`+itemCols+`) VALUES (?, ?, ?, ?, ?, 'open', '', ?, ?, ?, ?, ?)`,
			id, in.Dept, in.Title, in.Body, in.Priority, in.FiledBy, fold(in.FiledRoom), at, at, in.FiledBy); err != nil {
			return err
		}
		out, err = scanItem(t.QueryRow(`SELECT `+itemCols+` FROM backlog_item WHERE id = ?`, id))
		return err
	})
	if errors.Is(err, ErrItemExists) {
		return out, ErrItemExists
	}
	return out, err
}

// ItemGet is one item, or ErrItemNotFound.
func (s *Store) ItemGet(id string) (BacklogItem, error) {
	var b BacklogItem
	err := s.guard(func() error {
		var err error
		b, err = scanItem(s.db.QueryRow(`SELECT `+itemCols+` FROM backlog_item WHERE id = ?`, id))
		return err
	})
	if errors.Is(err, sql.ErrNoRows) {
		return b, ErrItemNotFound
	}
	return b, err
}

// ItemFilter narrows a list. Empty fields match everything. Open is every status but done and dropped, and takes the
// place of Status.
type ItemFilter struct {
	Dept, Status string
	// Priority is one of high, medium, low, or "unset" for the items with none.
	Priority string
	Open     bool
}

// ItemList is the items that match, grouped by department and in the order they were filed.
func (s *Store) ItemList(f ItemFilter) ([]BacklogItem, error) {
	out := []BacklogItem{}
	err := s.guard(func() error {
		out = []BacklogItem{}
		where, args := []string{"1 = 1"}, []any{}
		if f.Dept != "" {
			where, args = append(where, "dept = ?"), append(args, f.Dept)
		}
		if f.Priority != "" {
			p, _ := NormPriority(f.Priority)
			where, args = append(where, "priority = ?"), append(args, p)
		}
		switch {
		case f.Open:
			where = append(where, "status NOT IN ('done','dropped')")
		case f.Status != "":
			where, args = append(where, "status = ?"), append(args, f.Status)
		}
		rows, err := s.db.Query(`SELECT `+itemCols+` FROM backlog_item WHERE `+strings.Join(where, " AND ")+
			fmt.Sprintf(` ORDER BY dept, created_at, id LIMIT %d`, backlogListMax), args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			b, err := scanItem(rows)
			if err != nil {
				return err
			}
			out = append(out, b)
		}
		return rows.Err()
	})
	return out, err
}

// ItemSetStatus changes an item's status and says who did.
func (s *Store) ItemSetStatus(id, status, by string) (BacklogItem, error) {
	if !ValidItemStatus(status) {
		return BacklogItem{}, refuse(errors.New("a status is open, held, in-progress, done or dropped"))
	}
	if why := CRText("changer", by, backlogByMax, false); why != "" {
		return BacklogItem{}, refuse(errors.New(why))
	}
	var out BacklogItem
	err := s.tx(func(t *sql.Tx) error {
		res, err := t.Exec(`UPDATE backlog_item SET status = ?, changed_at = ?, changed_by = ? WHERE id = ?`,
			status, ts(s.now()), by, id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return refuse(ErrItemNotFound)
		}
		out, err = scanItem(t.QueryRow(`SELECT `+itemCols+` FROM backlog_item WHERE id = ?`, id))
		return err
	})
	if errors.Is(err, ErrItemNotFound) {
		return BacklogItem{}, ErrItemNotFound
	}
	return out, err
}

// ItemSetPriority changes an item's priority and says who did. An empty priority makes it unset.
func (s *Store) ItemSetPriority(id, priority, by string) (BacklogItem, error) {
	p, ok := NormPriority(priority)
	if !ok {
		return BacklogItem{}, priorityRefusal(priority)
	}
	if why := CRText("changer", by, backlogByMax, false); why != "" {
		return BacklogItem{}, refuse(errors.New(why))
	}
	var out BacklogItem
	err := s.tx(func(t *sql.Tx) error {
		res, err := t.Exec(`UPDATE backlog_item SET priority = ?, changed_at = ?, changed_by = ? WHERE id = ?`,
			p, ts(s.now()), by, id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return refuse(ErrItemNotFound)
		}
		out, err = scanItem(t.QueryRow(`SELECT `+itemCols+` FROM backlog_item WHERE id = ?`, id))
		return err
	})
	if errors.Is(err, ErrItemNotFound) {
		return BacklogItem{}, ErrItemNotFound
	}
	return out, err
}

// ItemEdit replaces an item's title, its body, or both, and says who did. A nil field is left as it is.
func (s *Store) ItemEdit(id string, title, body *string, by string) (BacklogItem, error) {
	switch {
	case title == nil && body == nil:
		return BacklogItem{}, refuse(errors.New("an edit needs a title or a body"))
	case title != nil && strings.TrimSpace(*title) == "":
		return BacklogItem{}, refuse(errors.New("an item needs a title"))
	}
	if title != nil {
		if why := CRText("title", *title, ItemTitleMax, false); why != "" {
			return BacklogItem{}, refuse(errors.New(why))
		}
	}
	if body != nil {
		if why := CRText("body", *body, ItemBodyMax, true); why != "" {
			return BacklogItem{}, refuse(errors.New(why))
		}
	}
	if why := CRText("changer", by, backlogByMax, false); why != "" {
		return BacklogItem{}, refuse(errors.New(why))
	}
	var out BacklogItem
	err := s.tx(func(t *sql.Tx) error {
		res, err := t.Exec(`UPDATE backlog_item SET title = COALESCE(?, title), body = COALESCE(?, body),
			changed_at = ?, changed_by = ? WHERE id = ?`, title, body, ts(s.now()), by, id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return refuse(ErrItemNotFound)
		}
		out, err = scanItem(t.QueryRow(`SELECT `+itemCols+` FROM backlog_item WHERE id = ?`, id))
		return err
	})
	if errors.Is(err, ErrItemNotFound) {
		return BacklogItem{}, ErrItemNotFound
	}
	return out, err
}

// ── reports ─────────────────────────────────────────────

// DirectorReport is one report, as the API carries it.
type DirectorReport struct {
	ID       string  `json:"id"`
	ToDept   string  `json:"to_dept"`
	FromBy   string  `json:"from_by"`
	FromRoom string  `json:"from_room"`
	Subject  string  `json:"subject"`
	Body     string  `json:"body,omitempty"`
	At       string  `json:"at"`
	ReadAt   *string `json:"read_at"`
	ReadBy   *string `json:"read_by"`
}

// ReportNew is a report to leave. ToDept is the director it is for, or empty for the orchestrator.
type ReportNew struct {
	ToDept, FromBy, FromRoom, Subject, Body string
}

// Check says why a report cannot be left, or "".
func (n ReportNew) Check() string {
	if n.ToDept != "" && !deptRe.MatchString(n.ToDept) {
		return "a report goes to a department like fabric, or to nobody for the orchestrator"
	}
	if strings.TrimSpace(n.Subject) == "" {
		return "a report needs a subject"
	}
	if why := CRText("subject", n.Subject, ReportSubjMax, false); why != "" {
		return why
	}
	if why := CRText("body", n.Body, ReportBodyMax, true); why != "" {
		return why
	}
	if why := CRText("sender", n.FromBy, backlogByMax, false); why != "" {
		return why
	}
	return ""
}

const reportCols = `id, to_dept, from_by, from_room, subject, body, at, read_at, read_by`

func scanReport(sc rowScanner) (DirectorReport, error) {
	var r DirectorReport
	var readAt, readBy string
	if err := sc.Scan(&r.ID, &r.ToDept, &r.FromBy, &r.FromRoom, &r.Subject, &r.Body, &r.At, &readAt, &readBy); err != nil {
		return r, err
	}
	if readAt != "" {
		r.ReadAt, r.ReadBy = &readAt, &readBy
	}
	return r, nil
}

// ReportAdd leaves a report. The id is `rp_<n>`, the table's highest n plus one, taken in the same transaction.
func (s *Store) ReportAdd(in ReportNew) (DirectorReport, error) {
	if why := in.Check(); why != "" {
		return DirectorReport{}, refuse(errors.New(why))
	}
	var out DirectorReport
	err := s.tx(func(t *sql.Tx) error {
		var top int64
		if err := t.QueryRow(`SELECT COALESCE(MAX(n), 0) FROM director_report`).Scan(&top); err != nil {
			return err
		}
		id := fmt.Sprintf("rp_%d", top+1)
		if _, err := t.Exec(`INSERT INTO director_report (n, id, to_dept, from_by, from_room, subject, body, at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			top+1, id, in.ToDept, in.FromBy, fold(in.FromRoom), in.Subject, in.Body, ts(s.now())); err != nil {
			return err
		}
		var err error
		out, err = scanReport(t.QueryRow(`SELECT `+reportCols+` FROM director_report WHERE id = ?`, id))
		return err
	})
	return out, err
}

// ReportFilter narrows a list. Unread is only the reports not yet marked read. ToDept matches the director it is for.
type ReportFilter struct {
	ToDept string
	Unread bool
	Limit  int
}

// ReportList is the reports that match, newest first.
func (s *Store) ReportList(f ReportFilter) ([]DirectorReport, error) {
	out := []DirectorReport{}
	if f.Limit <= 0 || f.Limit > backlogListMax {
		f.Limit = 100
	}
	err := s.guard(func() error {
		out = []DirectorReport{}
		where, args := []string{"1 = 1"}, []any{}
		if f.ToDept != "" {
			where, args = append(where, "to_dept = ?"), append(args, f.ToDept)
		}
		if f.Unread {
			where = append(where, "read_at = ''")
		}
		rows, err := s.db.Query(`SELECT `+reportCols+` FROM director_report WHERE `+strings.Join(where, " AND ")+
			fmt.Sprintf(` ORDER BY n DESC LIMIT %d`, f.Limit), args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			r, err := scanReport(rows)
			if err != nil {
				return err
			}
			out = append(out, r)
		}
		return rows.Err()
	})
	return out, err
}

// ReportGet is one report, or ErrReportNotFound.
func (s *Store) ReportGet(id string) (DirectorReport, error) {
	var r DirectorReport
	err := s.guard(func() error {
		var err error
		r, err = scanReport(s.db.QueryRow(`SELECT `+reportCols+` FROM director_report WHERE id = ?`, id))
		return err
	})
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrReportNotFound
	}
	return r, err
}

// ReportRead marks a report read and says by whom. A report already read keeps its first reader.
func (s *Store) ReportRead(id, by string) (DirectorReport, error) {
	if why := CRText("reader", by, backlogByMax, false); why != "" {
		return DirectorReport{}, refuse(errors.New(why))
	}
	var out DirectorReport
	err := s.tx(func(t *sql.Tx) error {
		if _, err := t.Exec(`UPDATE director_report SET read_at = ?, read_by = ? WHERE id = ? AND read_at = ''`,
			ts(s.now()), by, id); err != nil {
			return err
		}
		var err error
		out, err = scanReport(t.QueryRow(`SELECT `+reportCols+` FROM director_report WHERE id = ?`, id))
		if errors.Is(err, sql.ErrNoRows) {
			return refuse(ErrReportNotFound)
		}
		return err
	})
	if errors.Is(err, ErrReportNotFound) {
		return DirectorReport{}, ErrReportNotFound
	}
	return out, err
}
