package hubstore

import (
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// The three things an item does besides being filed and set by hand: take an id from the hub, be refreshed from a
// file, and follow the card that works on it. See docs/rnd/reports-channel-design.md.

// deptPrefix is the prefix of the ids a department's items carry. An item filed with no id takes `<prefix>-<n>`.
var deptPrefix = map[string]string{
	"fabric": "f", "runtime": "r", "ui": "u", "release": "m", "rnd": "rnd", "terminal": "t", "review": "review",
}

// DeptPrefix is a department's id prefix, and whether it has one.
func DeptPrefix(dept string) (string, bool) {
	p, ok := deptPrefix[dept]
	return p, ok
}

// nextItemID is `<prefix>-<n>`, n one above the highest numeric id of that prefix the table holds. An id like
// `f-new-thing` is not numeric and does not count. Taken inside the filing's transaction, so two filings cannot
// take the same n.
func nextItemID(t *sql.Tx, dept string) (string, error) {
	prefix, ok := DeptPrefix(dept)
	if !ok {
		return "", refuse(fmt.Errorf("an item with no id needs a department that has an id prefix (fabric, runtime, " +
			"ui, release, rnd, terminal or review), or an id of your own"))
	}
	num := regexp.MustCompile(`^` + regexp.QuoteMeta(prefix) + `-([0-9]+)$`)
	rows, err := t.Query(`SELECT id FROM backlog_item WHERE id LIKE ?`, prefix+"-%")
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var top int64
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return "", err
		}
		if m := num.FindStringSubmatch(id); m != nil {
			if n, err := strconv.ParseInt(m[1], 10, 64); err == nil && n > top {
				top = n
			}
		}
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s-%03d", prefix, top+1), nil
}

// ItemUpsert makes the row for an id match a file: dept, title, body, priority and status. A new id is inserted. An
// existing one is changed only where it differs, so a refresh of what has not moved changes nothing. The second
// answer is `added`, `updated` or `unchanged`. The card an item is linked to is left as it is.
func (s *Store) ItemUpsert(in ItemNew, status string) (BacklogItem, string, error) {
	if status == "" {
		status = ItemOpen
	}
	if in.ID == "" {
		return BacklogItem{}, "", refuse(errors.New("an import needs an id"))
	}
	if !ValidItemStatus(status) {
		return BacklogItem{}, "", refuse(errors.New("a status is open, held, in-progress, built, blocked, incomplete, done or dropped"))
	}
	if why := in.Check(); why != "" {
		return BacklogItem{}, "", refuse(errors.New(why))
	}
	var out BacklogItem
	how := "unchanged"
	err := s.tx(func(t *sql.Tx) error {
		at := ts(now())
		prior, err := scanItem(t.QueryRow(`SELECT `+itemCols+` FROM backlog_item WHERE id = ?`, in.ID))
		switch {
		case errors.Is(err, sql.ErrNoRows):
			if _, err := t.Exec(`INSERT INTO backlog_item (`+itemCols+`) VALUES (?, ?, ?, ?, ?, ?, '', ?, ?, ?, ?, ?)`,
				in.ID, in.Dept, in.Title, in.Body, in.Priority, status, in.FiledBy, fold(in.FiledRoom), at, at, in.FiledBy); err != nil {
				return err
			}
			how = "added"
		case err != nil:
			return err
		case prior.Dept != in.Dept || prior.Title != in.Title || prior.Body != in.Body || prior.Priority != in.Priority ||
			prior.Status != status:
			if _, err := t.Exec(`UPDATE backlog_item SET dept = ?, title = ?, body = ?, priority = ?, status = ?,
				changed_at = ?, changed_by = ? WHERE id = ?`,
				in.Dept, in.Title, in.Body, in.Priority, status, at, in.FiledBy, in.ID); err != nil {
				return err
			}
			how = "updated"
		}
		out, err = scanItem(t.QueryRow(`SELECT `+itemCols+` FROM backlog_item WHERE id = ?`, in.ID))
		return err
	})
	return out, how, err
}

// ItemLink puts an item in progress with the card that was launched for it.
func (s *Store) ItemLink(id, card, by string) (BacklogItem, error) {
	if why := CRText("card", card, backlogByMax, false); why != "" || card == "" {
		return BacklogItem{}, refuse(errors.New("a link needs a card"))
	}
	if why := CRText("changer", by, backlogByMax, false); why != "" {
		return BacklogItem{}, refuse(errors.New(why))
	}
	var out BacklogItem
	err := s.tx(func(t *sql.Tx) error {
		res, err := t.Exec(`UPDATE backlog_item SET status = ?, card = ?, changed_at = ?, changed_by = ? WHERE id = ?`,
			ItemInProgress, card, ts(now()), by, id)
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

// FollowStatus is the item status a card's report brings: done is built, and blocked and incomplete are themselves.
// Any other report status (a question, progress) moves nothing, and the second answer is false.
func FollowStatus(report string) (string, bool) {
	switch strings.TrimSpace(report) {
	case "done":
		return ItemBuilt, true
	case "blocked":
		return ItemBlocked, true
	case "incomplete":
		return ItemIncomplete, true
	}
	return "", false
}

// ItemFollow moves the item linked to a card by that card's report. No item linked, or a report that moves nothing,
// answers false and changes nothing.
func (s *Store) ItemFollow(card, report, by string) (BacklogItem, bool, error) {
	status, ok := FollowStatus(report)
	if !ok || card == "" {
		return BacklogItem{}, false, nil
	}
	var out BacklogItem
	found := false
	err := s.tx(func(t *sql.Tx) error {
		prior, err := scanItem(t.QueryRow(`SELECT `+itemCols+` FROM backlog_item WHERE card = ? ORDER BY changed_at DESC, id LIMIT 1`, card))
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		found = true
		if _, err := t.Exec(`UPDATE backlog_item SET status = ?, changed_at = ?, changed_by = ? WHERE id = ?`,
			status, ts(now()), by, prior.ID); err != nil {
			return err
		}
		out, err = scanItem(t.QueryRow(`SELECT `+itemCols+` FROM backlog_item WHERE id = ?`, prior.ID))
		return err
	})
	return out, found, err
}
