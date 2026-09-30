package hubstore

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/dovholuknf/atrium/internal/itemgate"
)

// Item gates: "item A waits on target T", kept on the hub.
//
// ── what this file decides and what it does not ─────────
//
// It stores gates, refuses a loop at write time, follows renames, and records a gate met
// once. It does NOT decide whether a gate is met: that is a git read or the room list, done
// in internal/link, which then calls GateMet. A gate met here is never re-opened, so a
// re-signed branch cannot un-land an item.
//
// Every refusal is wrapped by `refuse`, so a caller's mistake answers as a sentence and
// never halts the hub.

const gateCols = `id, repo, item, kind, target, why, added_by, added_at, met_at, met_by, met_why, told_at`

func scanGate(sc interface{ Scan(...any) error }) (itemgate.Gate, error) {
	var g itemgate.Gate
	var added, met, told string
	if err := sc.Scan(&g.ID, &g.Repo, &g.Item, &g.Kind, &g.Target, &g.Why, &g.AddedBy, &added,
		&met, &g.MetBy, &g.MetWhy, &told); err != nil {
		return g, err
	}
	if p := parseOrNil(added); p != nil {
		g.AddedAt = *p
	}
	g.MetAt, g.ToldAt = parseOrNil(met), parseOrNil(told)
	return g, nil
}

func queryGates(q interface {
	Query(string, ...any) (*sql.Rows, error)
}, where string, args ...any) ([]itemgate.Gate, error) {
	rows, err := q.Query(`SELECT `+gateCols+` FROM item_gate `+where+` ORDER BY added_at, id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []itemgate.Gate
	for rows.Next() {
		g, err := scanGate(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// canonical follows renames from id. Renames are flattened on write, so one hop is the
// answer, and the bound only guards a table somebody edited by hand.
func canonical(t *sql.Tx, repo, id string) (string, error) {
	for i := 0; i < 16; i++ {
		var to string
		err := t.QueryRow(`SELECT to_id FROM item_rename WHERE repo = ? AND from_id = ?`, repo, id).Scan(&to)
		if errors.Is(err, sql.ErrNoRows) {
			return id, nil
		}
		if err != nil {
			return "", err
		}
		id = to
	}
	return id, nil
}

// openEdges is every open item-on-item edge in a repo.
func openEdges(t *sql.Tx, repo string) (map[string][]string, error) {
	rows, err := t.Query(`SELECT item, target FROM item_gate WHERE repo = ? AND kind = 'item' AND met_at = ''`, repo)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	edges := map[string][]string{}
	for rows.Next() {
		var a, b string
		if err := rows.Scan(&a, &b); err != nil {
			return nil, err
		}
		edges[a] = append(edges[a], b)
	}
	return edges, rows.Err()
}

// AddGates records that `item` waits on each of `waits`. A gate already open answers the
// existing row, so adding twice is harmless. A target that would close a loop refuses the
// whole call, naming the loop, and nothing is written.
func (s *Store) AddGates(repo, item string, waits []itemgate.Target, why, addedBy string) ([]itemgate.Gate, error) {
	repo, item = strings.TrimSpace(repo), strings.TrimSpace(item)
	switch {
	case repo == "":
		return nil, errors.New("say which repository")
	case !itemgate.ValidItem(item):
		return nil, fmt.Errorf("%q is not an item id", item)
	case len(waits) == 0:
		return nil, fmt.Errorf("%s has to wait on something", item)
	case len(waits) > itemgate.MaxTargets:
		return nil, fmt.Errorf("at most %d targets at once", itemgate.MaxTargets)
	}
	why = itemgate.Bound(why, itemgate.MaxWhy)
	if strings.TrimSpace(addedBy) == "" {
		addedBy = itemgate.AddedByHuman
	}
	var out []itemgate.Gate
	err := s.tx(func(t *sql.Tx) error {
		out = nil
		item, err := canonical(t, repo, item)
		if err != nil {
			return err
		}
		edges, err := openEdges(t, repo)
		if err != nil {
			return err
		}
		at := ts(now())
		for _, w := range waits {
			target := w.Target
			if w.Kind == itemgate.KindItem {
				if target, err = canonical(t, repo, target); err != nil {
					return err
				}
				if target == item {
					return refuse(fmt.Errorf("%s cannot wait on itself", item))
				}
				if loop := itemgate.FindPath(edges, target, item); loop != nil {
					return refuse(itemgate.LoopError(append([]string{item}, loop...)))
				}
				edges[item] = append(edges[item], target)
			}
			had, err := queryGates(t, `WHERE repo = ? AND item = ? AND kind = ? AND target = ? AND met_at = ''`,
				repo, item, w.Kind, target)
			if err != nil {
				return err
			}
			if len(had) > 0 {
				out = append(out, had[0])
				continue
			}
			id := newID()
			if _, err := t.Exec(`INSERT INTO item_gate (id, repo, item, kind, target, why, added_by, added_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, id, repo, item, w.Kind, target, why, addedBy, at); err != nil {
				return err
			}
			g, err := scanGate(t.QueryRow(`SELECT `+gateCols+` FROM item_gate WHERE id = ?`, id))
			if err != nil {
				return err
			}
			out = append(out, g)
		}
		return nil
	})
	return out, err
}

// Gates lists gates, newest last. Empty repo is every repository and empty item is every
// item. An item is matched under its old names too, so a gate written against a slug is
// found by the number it was given.
func (s *Store) Gates(repo, item string, openOnly bool) ([]itemgate.Gate, error) {
	var conds []string
	var args []any
	if repo = strings.TrimSpace(repo); repo != "" {
		conds, args = append(conds, "repo = ?"), append(args, repo)
	}
	if item = strings.TrimSpace(item); item != "" {
		conds = append(conds, `(item = ? OR item IN (SELECT to_id FROM item_rename r
			WHERE r.repo = item_gate.repo AND r.from_id = ?))`)
		args = append(args, item, item)
	}
	if openOnly {
		conds = append(conds, "met_at = ''")
	}
	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}
	var out []itemgate.Gate
	err := s.guard(func() error {
		var err error
		out, err = queryGates(s.db, where, args...)
		return err
	})
	return out, err
}

// Gate reads one gate by id.
func (s *Store) Gate(id string) (itemgate.Gate, error) {
	var g itemgate.Gate
	err := s.guard(func() error {
		var err error
		g, err = scanGate(s.db.QueryRow(`SELECT `+gateCols+` FROM item_gate WHERE id = ?`, id))
		return err
	})
	return g, err
}

// HasOpenGates is whether any gate is open anywhere, so an idle hub's ticker costs one
// indexed read.
func (s *Store) HasOpenGates() (bool, error) {
	var n int
	err := s.guard(func() error {
		return s.db.QueryRow(`SELECT COUNT(*) FROM (SELECT 1 FROM item_gate WHERE met_at = '' LIMIT 1) x`).Scan(&n)
	})
	return n > 0, err
}

// GateMet records a gate met, once. It answers false when the gate was already met, which
// is how two checks racing each other record one answer. `by` is atrium or human.
func (s *Store) GateMet(id, by, why string) (bool, error) {
	if by != itemgate.MetByAtrium && by != itemgate.MetByHuman {
		return false, fmt.Errorf("a gate is met by atrium or by a human, not %q", by)
	}
	var changed bool
	err := s.guard(func() error {
		res, err := s.db.Exec(`UPDATE item_gate SET met_at = ?, met_by = ?, met_why = ? WHERE id = ? AND met_at = ''`,
			ts(now()), by, itemgate.Bound(why, itemgate.MaxWhy), id)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		changed = n > 0
		return nil
	})
	return changed, err
}

// PendingTells is every met gate an agent added whose waiter has not been told yet. The
// caller tells a waiter only once its item has no open gate left.
func (s *Store) PendingTells() ([]itemgate.Gate, error) {
	var out []itemgate.Gate
	err := s.guard(func() error {
		var err error
		out, err = queryGates(s.db, `WHERE met_at <> '' AND told_at = '' AND added_by <> ?`, itemgate.AddedByHuman)
		return err
	})
	return out, err
}

// GateTold stamps the gates whose waiter has been told the item is clear.
func (s *Store) GateTold(ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	at := ts(now())
	return s.tx(func(t *sql.Tx) error {
		for _, id := range ids {
			if _, err := t.Exec(`UPDATE item_gate SET told_at = ? WHERE id = ? AND told_at = ''`, at, id); err != nil {
				return err
			}
		}
		return nil
	})
}

// RenameItem records that `from` is now called `to`, and moves every OPEN gate written
// against `from`, as the waiting item or as a target, onto `to`. A rename is a fact about
// a name and clears nothing. It answers how many open gates moved.
func (s *Store) RenameItem(repo, from, to, by string) (int, error) {
	repo, from, to = strings.TrimSpace(repo), strings.TrimSpace(from), strings.TrimSpace(to)
	switch {
	case repo == "":
		return 0, errors.New("say which repository")
	case !itemgate.ValidItem(from) || !itemgate.ValidItem(to):
		return 0, fmt.Errorf("%q to %q: both have to be item ids", from, to)
	case from == to:
		return 0, fmt.Errorf("%s is already called that", from)
	}
	moved := 0
	err := s.tx(func(t *sql.Tx) error {
		moved = 0
		end, err := canonical(t, repo, to)
		if err != nil {
			return err
		}
		if end == from {
			return refuse(fmt.Errorf("%s is already renamed to %s, so renaming it back would loop", to, from))
		}
		at := ts(now())
		if _, err := t.Exec(`INSERT INTO item_rename (repo, from_id, to_id, at, by) VALUES (?, ?, ?, ?, ?)
			ON CONFLICT (repo, from_id) DO UPDATE SET to_id = excluded.to_id, at = excluded.at, by = excluded.by`,
			repo, from, end, at, by); err != nil {
			return err
		}
		// FLATTENED, so every old name points straight at the newest one.
		if _, err := t.Exec(`UPDATE item_rename SET to_id = ? WHERE repo = ? AND to_id = ?`, end, repo, from); err != nil {
			return err
		}
		open, err := queryGates(t, `WHERE repo = ? AND met_at = '' AND (item = ? OR (kind = 'item' AND target = ?))`,
			repo, from, from)
		if err != nil {
			return err
		}
		for _, g := range open {
			item, target := g.Item, g.Target
			if item == from {
				item = end
			}
			if g.Kind == itemgate.KindItem && target == from {
				target = end
			}
			if g.Kind == itemgate.KindItem && item == target {
				return refuse(fmt.Errorf("renaming %s to %s would make %s wait on itself", from, end, end))
			}
			dup, err := queryGates(t, `WHERE repo = ? AND item = ? AND kind = ? AND target = ? AND met_at = '' AND id <> ?`,
				repo, item, g.Kind, target, g.ID)
			if err != nil {
				return err
			}
			if len(dup) > 0 {
				// THE SAME GATE under its new name is already there, so this row says
				// nothing the other does not.
				if _, err := t.Exec(`DELETE FROM item_gate WHERE id = ?`, g.ID); err != nil {
					return err
				}
			} else if _, err := t.Exec(`UPDATE item_gate SET item = ?, target = ? WHERE id = ?`, item, target, g.ID); err != nil {
				return err
			}
			moved++
		}
		return nil
	})
	return moved, err
}

// ItemNames is every name an item has had in a repo, its current one first. The landed
// check tries all of them, so a slug that lands before its rename is still seen.
func (s *Store) ItemNames(repo, id string) ([]string, error) {
	var out []string
	err := s.tx(func(t *sql.Tx) error {
		cur, err := canonical(t, repo, id)
		if err != nil {
			return err
		}
		out = []string{cur}
		rows, err := t.Query(`SELECT from_id FROM item_rename WHERE repo = ? AND to_id = ? ORDER BY from_id`, repo, cur)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var n string
			if err := rows.Scan(&n); err != nil {
				return err
			}
			out = append(out, n)
		}
		return rows.Err()
	})
	return out, err
}
