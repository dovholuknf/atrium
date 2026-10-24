package hubstore

import (
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/dovholuknf/atrium/internal/gitsync"
)

// The merge flow's rows. See migration 0015 and docs/design/merge-flow.md.
//
//   - A ROLE HAS ONE HOLDER, except the tester. Setting it again replaces the holder, and the caller is told who was
//     replaced. Atrium never elects one: a role is set by a person or by the holder handing it on.
//   - A STAGE ONLY MOVES FORWARD through a report. Git can raise it later, and only the operator's reset lowers it.
//   - A CHECK AND A DEPLOY ARE KEYED BY SHA, not by item: an item is tested when a passing check exists on a sha that
//     contains it.

// SettingFlowRepo names the hub repository, as `github.com/owner/repo`, that the flow reads an item's stage from. It is a
// hub setting, so it needs no migration. Unset, a stage is what was reported.
const SettingFlowRepo = "flow_repo"

// FlowRepo reads the repository the flow reads, "" when unset.
func (s *Store) FlowRepo() (string, error) { return s.HubSetting(SettingFlowRepo) }

// SetFlowRepo sets it, and "" unsets it. A name is <host>/<owner>/<repo>, as the store spells it.
func (s *Store) SetFlowRepo(name string) error {
	if name = strings.TrimSpace(name); name != "" && !gitsync.ValidName(name) {
		return refuse(errors.New("that is not <host>/<owner>/<repo>, like github.com/dovholuknf/atrium"))
	}
	return s.SetHubSetting(SettingFlowRepo, name)
}

// The roles, as the CHECK constraint spells them.
const (
	RoleOrchestrator = "orchestrator"
	RoleMerger       = "merger"
	RoleTester       = "tester"
	RoleDirector     = "director"
)

// Stages are the stages of an item, in order.
var Stages = []string{"idea", "designed", "implementing", "implemented", "merged", "tested", "deployed", "pushed-by-clint"}

var (
	// ErrFlowItemNotFound is an item that is not in the flow.
	ErrFlowItemNotFound = errors.New("that item is not in the merge flow")
	// ErrStageBackwards is a report that would move an item back.
	ErrStageBackwards = errors.New("a stage only moves forward")
	// ErrMergerRetired is naming a merger, which the merge queue replaced. Rows already there are left alone.
	ErrMergerRetired = errors.New("the merge queue is the merger now: atrium merge")

	flowShaRe  = regexp.MustCompile(`^[0-9a-f]{7,64}$`)
	flowNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,39}$`)
)

// ValidRole reports whether r is a role.
func ValidRole(r string) bool {
	switch r {
	case RoleOrchestrator, RoleMerger, RoleTester, RoleDirector:
		return true
	}
	return false
}

// StageIndex is a stage's place in the order, or -1.
func StageIndex(stage string) int {
	for i, s := range Stages {
		if s == stage {
			return i
		}
	}
	return -1
}

// RoleHolder is one holder of a role.
type RoleHolder struct {
	Role  string `json:"role"`
	Room  string `json:"room"`
	Card  string `json:"card"`
	Since string `json:"since"`
	SetBy string `json:"set_by"`
}

// RoleSet makes (room, card) the holder of role. For every role but the tester it replaces the holder, and the
// replaced holders come back. The tester is added to.
func (s *Store) RoleSet(role, room, card, by string) (RoleHolder, []RoleHolder, error) {
	room = fold(room)
	switch {
	case role == RoleMerger:
		return RoleHolder{}, nil, refuse(ErrMergerRetired)
	case !ValidRole(role):
		return RoleHolder{}, nil, refuse(errors.New("a role is orchestrator, tester or director"))
	case room == "" || strings.TrimSpace(card) == "":
		return RoleHolder{}, nil, refuse(errors.New("a role is held by a card, so say its room and its card"))
	}
	for name, v := range map[string]string{"card": card, "setter": by} {
		if why := CRText(name, v, backlogByMax, false); why != "" {
			return RoleHolder{}, nil, refuse(errors.New(why))
		}
	}
	var out RoleHolder
	var replaced []RoleHolder
	err := s.tx(func(t *sql.Tx) error {
		replaced = nil
		prior, err := roleRows(t, role)
		if err != nil {
			return err
		}
		if role != RoleTester {
			for _, p := range prior {
				if p.Room != room || p.Card != card {
					replaced = append(replaced, p)
				}
			}
			if _, err := t.Exec(`DELETE FROM flow_role WHERE role = ? AND NOT (room = ? AND card = ?)`, role, room, card); err != nil {
				return err
			}
		}
		if _, err := t.Exec(`INSERT INTO flow_role (role, room, card, since, set_by) VALUES (?, ?, ?, ?, ?)
			ON CONFLICT (role, room, card) DO UPDATE SET set_by = excluded.set_by`, role, room, card, ts(s.now()), by); err != nil {
			return err
		}
		return t.QueryRow(`SELECT role, room, card, since, set_by FROM flow_role WHERE role = ? AND room = ? AND card = ?`,
			role, room, card).Scan(&out.Role, &out.Room, &out.Card, &out.Since, &out.SetBy)
	})
	return out, replaced, err
}

// RoleClear lets (room, card) go from role. Clearing a holder that is not one is not an error.
func (s *Store) RoleClear(role, room, card string) error {
	if !ValidRole(role) {
		return refuse(errors.New("a role is orchestrator, merger, tester or director"))
	}
	return s.tx(func(t *sql.Tx) error {
		_, err := t.Exec(`DELETE FROM flow_role WHERE role = ? AND room = ? AND card = ?`, role, fold(room), card)
		return err
	})
}

type querier interface {
	Query(string, ...any) (*sql.Rows, error)
}

func roleRows(q querier, role string) ([]RoleHolder, error) {
	where, args := "1 = 1", []any{}
	if role != "" {
		where, args = "role = ?", []any{role}
	}
	rows, err := q.Query(`SELECT role, room, card, since, set_by FROM flow_role WHERE `+where+` ORDER BY role, since, room, card`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RoleHolder{}
	for rows.Next() {
		var h RoleHolder
		if err := rows.Scan(&h.Role, &h.Room, &h.Card, &h.Since, &h.SetBy); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// Roles lists the holders of role, or of every role when it is empty.
func (s *Store) Roles(role string) ([]RoleHolder, error) {
	if role != "" && !ValidRole(role) {
		return nil, refuse(errors.New("a role is orchestrator, merger, tester or director"))
	}
	var out []RoleHolder
	err := s.guard(func() error {
		var err error
		out, err = roleRows(s.db, role)
		return err
	})
	return out, err
}

// IsHolder says whether (room, card) holds role. THE MERGE GATE ASKS THIS on every push to claude/main.
func (s *Store) IsHolder(role, room, card string) (bool, error) {
	var n int
	err := s.guard(func() error {
		return s.db.QueryRow(`SELECT COUNT(*) FROM flow_role WHERE role = ? AND room = ? AND card = ?`,
			role, fold(room), card).Scan(&n)
	})
	return n > 0, err
}

// FlowItem is an item's place in the flow.
type FlowItem struct {
	ItemID    string `json:"item_id"`
	Stage     string `json:"stage"`
	Branch    string `json:"branch,omitempty"`
	SHA       string `json:"sha,omitempty"`
	Note      string `json:"note,omitempty"`
	ChangedAt string `json:"changed_at"`
	ChangedBy string `json:"changed_by"`
}

// FlowSet is a report of an item's stage. Branch, SHA and Note are kept when empty. The item has to be a backlog
// item. A report that would move the stage back is refused with ErrStageBackwards; the same stage again is allowed and
// updates the branch, sha and note.
type FlowSet struct {
	ItemID, Stage, Branch, SHA, Note, By string
}

func (f FlowSet) check() string {
	switch {
	case StageIndex(f.Stage) < 0:
		return "a stage is " + strings.Join(Stages, ", ")
	case f.SHA != "" && !flowShaRe.MatchString(f.SHA):
		return "a sha is 7 to 64 lower case hex digits"
	case f.Branch != "" && (len(f.Branch) > 200 || strings.ContainsAny(f.Branch, " \t\n:\\")):
		return "that is not a branch name"
	}
	if why := CRText("note", f.Note, 2000, true); why != "" {
		return why
	}
	return CRText("reporter", f.By, backlogByMax, false)
}

// FlowSetStage records a stage for an item and says who did.
func (s *Store) FlowSetStage(f FlowSet) (FlowItem, error) {
	if why := f.check(); why != "" {
		return FlowItem{}, refuse(errors.New(why))
	}
	var out FlowItem
	err := s.tx(func(t *sql.Tx) error {
		var one int
		if err := t.QueryRow(`SELECT 1 FROM backlog_item WHERE id = ?`, f.ItemID).Scan(&one); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return refuse(ErrItemNotFound)
			}
			return err
		}
		cur, err := scanFlow(t.QueryRow(`SELECT `+flowCols+` FROM flow_item WHERE item_id = ?`, f.ItemID))
		at := ts(s.now())
		switch {
		case errors.Is(err, sql.ErrNoRows):
			if _, err := t.Exec(`INSERT INTO flow_item (`+flowCols+`) VALUES (?, ?, ?, ?, ?, ?, ?)`,
				f.ItemID, f.Stage, f.Branch, f.SHA, f.Note, at, f.By); err != nil {
				return err
			}
		case err != nil:
			return err
		default:
			if StageIndex(f.Stage) < StageIndex(cur.Stage) {
				return refuse(fmt.Errorf("%w: %s is already %s", ErrStageBackwards, f.ItemID, cur.Stage))
			}
			if f.Branch == "" {
				f.Branch = cur.Branch
			}
			if f.SHA == "" {
				f.SHA = cur.SHA
			}
			if f.Note == "" {
				f.Note = cur.Note
			}
			if _, err := t.Exec(`UPDATE flow_item SET stage = ?, branch = ?, sha = ?, note = ?, changed_at = ?, changed_by = ? WHERE item_id = ?`,
				f.Stage, f.Branch, f.SHA, f.Note, at, f.By, f.ItemID); err != nil {
				return err
			}
		}
		out, err = scanFlow(t.QueryRow(`SELECT `+flowCols+` FROM flow_item WHERE item_id = ?`, f.ItemID))
		return err
	})
	return out, err
}

const flowCols = `item_id, stage, branch, sha, note, changed_at, changed_by`

func scanFlow(sc rowScanner) (FlowItem, error) {
	var f FlowItem
	err := sc.Scan(&f.ItemID, &f.Stage, &f.Branch, &f.SHA, &f.Note, &f.ChangedAt, &f.ChangedBy)
	return f, err
}

// FlowGet is an item's place in the flow, or ErrFlowItemNotFound.
func (s *Store) FlowGet(id string) (FlowItem, error) {
	var f FlowItem
	err := s.guard(func() error {
		var err error
		f, err = scanFlow(s.db.QueryRow(`SELECT `+flowCols+` FROM flow_item WHERE item_id = ?`, id))
		return err
	})
	if errors.Is(err, sql.ErrNoRows) {
		return f, ErrFlowItemNotFound
	}
	return f, err
}

// FlowList lists the items at a stage, or every item in the flow when stage is empty.
func (s *Store) FlowList(stage string) ([]FlowItem, error) {
	if stage != "" && StageIndex(stage) < 0 {
		return nil, refuse(errors.New("a stage is " + strings.Join(Stages, ", ")))
	}
	out := []FlowItem{}
	err := s.guard(func() error {
		out = []FlowItem{}
		q, args := `SELECT `+flowCols+` FROM flow_item`, []any{}
		if stage != "" {
			q, args = q+` WHERE stage = ?`, []any{stage}
		}
		rows, err := s.db.Query(q+` ORDER BY changed_at, item_id`, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			f, err := scanFlow(rows)
			if err != nil {
				return err
			}
			out = append(out, f)
		}
		return rows.Err()
	})
	return out, err
}

// FlowCheck is a verdict on one sha of main.
type FlowCheck struct {
	N      int64  `json:"n"`
	SHA    string `json:"sha"`
	Name   string `json:"name"`
	Result string `json:"result"`
	By     string `json:"by"`
	At     string `json:"at"`
}

// CheckAdd records that the check `name` came out `result` on sha. Checks are append-only.
func (s *Store) CheckAdd(sha, name, result, by string) (FlowCheck, error) {
	switch {
	case !flowShaRe.MatchString(sha):
		return FlowCheck{}, refuse(errors.New("a sha is 7 to 64 lower case hex digits"))
	case !flowNameRe.MatchString(name):
		return FlowCheck{}, refuse(errors.New("a check is named in lower case letters and digits, like sanity or go-tests"))
	case result != "pass" && result != "fail" && result != "timeout":
		return FlowCheck{}, refuse(errors.New("a result is pass, fail or timeout"))
	}
	if why := CRText("reporter", by, backlogByMax, false); why != "" {
		return FlowCheck{}, refuse(errors.New(why))
	}
	var out FlowCheck
	err := s.tx(func(t *sql.Tx) error {
		var top int64
		if err := t.QueryRow(`SELECT COALESCE(MAX(n), 0) FROM flow_check`).Scan(&top); err != nil {
			return err
		}
		if _, err := t.Exec(`INSERT INTO flow_check (n, sha, name, result, by, at) VALUES (?, ?, ?, ?, ?, ?)`,
			top+1, sha, name, result, by, ts(s.now())); err != nil {
			return err
		}
		return t.QueryRow(`SELECT n, sha, name, result, by, at FROM flow_check WHERE n = ?`, top+1).
			Scan(&out.N, &out.SHA, &out.Name, &out.Result, &out.By, &out.At)
	})
	return out, err
}

// Checks lists the checks recorded for sha, newest first. An empty sha lists the latest `limit`.
func (s *Store) Checks(sha string, limit int) ([]FlowCheck, error) {
	if limit <= 0 || limit > backlogListMax {
		limit = 100
	}
	out := []FlowCheck{}
	err := s.guard(func() error {
		out = []FlowCheck{}
		q, args := `SELECT n, sha, name, result, by, at FROM flow_check`, []any{}
		if sha != "" {
			q, args = q+` WHERE sha = ?`, []any{sha}
		}
		rows, err := s.db.Query(q+fmt.Sprintf(` ORDER BY n DESC LIMIT %d`, limit), args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var c FlowCheck
			if err := rows.Scan(&c.N, &c.SHA, &c.Name, &c.Result, &c.By, &c.At); err != nil {
				return err
			}
			out = append(out, c)
		}
		return rows.Err()
	})
	return out, err
}

// FlowDeploy is the sha a room runs.
type FlowDeploy struct {
	Room string `json:"room"`
	SHA  string `json:"sha"`
	At   string `json:"at"`
	By   string `json:"by"`
}

// DeploySet records that room runs sha. A room has one row: the latest word replaces the last.
func (s *Store) DeploySet(room, sha, by string) (FlowDeploy, error) {
	room = fold(room)
	switch {
	case room == "":
		return FlowDeploy{}, refuse(errors.New("say which room"))
	case !flowShaRe.MatchString(sha):
		return FlowDeploy{}, refuse(errors.New("a sha is 7 to 64 lower case hex digits"))
	}
	if why := CRText("reporter", by, backlogByMax, false); why != "" {
		return FlowDeploy{}, refuse(errors.New(why))
	}
	var out FlowDeploy
	err := s.tx(func(t *sql.Tx) error {
		if _, err := t.Exec(`INSERT INTO flow_deploy (room, sha, at, by) VALUES (?, ?, ?, ?)
			ON CONFLICT (room) DO UPDATE SET sha = excluded.sha, at = excluded.at, by = excluded.by`,
			room, sha, ts(s.now()), by); err != nil {
			return err
		}
		return t.QueryRow(`SELECT room, sha, at, by FROM flow_deploy WHERE room = ?`, room).
			Scan(&out.Room, &out.SHA, &out.At, &out.By)
	})
	return out, err
}

// Deploys lists what every room runs.
func (s *Store) Deploys() ([]FlowDeploy, error) {
	out := []FlowDeploy{}
	err := s.guard(func() error {
		out = []FlowDeploy{}
		rows, err := s.db.Query(`SELECT room, sha, at, by FROM flow_deploy ORDER BY room`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var d FlowDeploy
			if err := rows.Scan(&d.Room, &d.SHA, &d.At, &d.By); err != nil {
				return err
			}
			out = append(out, d)
		}
		return rows.Err()
	})
	return out, err
}
