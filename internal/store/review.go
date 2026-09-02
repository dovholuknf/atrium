package store

import (
	"sort"
	"strings"
)

// Reviewing a run after the fact.
//
// Auto mode trades interruption for review: nothing stops to ask, and the audit
// log is read afterwards instead. A flat list of four hundred approvals will
// not be read. What gets read is the subset that would have interrupted you,
// which is the set answered by the mode rather than by a person.
//
// See docs/auto-mode.md.

// ReviewGroup is one tool's worth of a run, with its commands.
type ReviewGroup struct {
	Tool  string `json:"tool"`
	Count int    `json:"count"`
	// Unattended is how many auto mode answered without anyone being asked.
	Unattended int `json:"unattended"`
	// Blocked is how many were refused, by a rule or by shelving. Auto mode
	// never overrides those, so a non-zero count means the session hit a wall
	// and went looking for a way around it.
	Blocked int             `json:"blocked"`
	Entries []*ReviewEntry  `json:"entries"`
	byCmd   map[string]bool `json:"-"`
}

// ReviewEntry is one decision, flattened for reading.
type ReviewEntry struct {
	ID       string `json:"id"`
	Command  string `json:"command"`
	Decision string `json:"decision"`
	// By is who or what answered: a person, a rule pattern, auto, shelved.
	By string `json:"by"`
	At string `json:"at"`
	// Unattended marks a decision no person saw.
	Unattended bool `json:"unattended"`
	// Repeats counts identical commands folded into this entry, so a hundred
	// runs of the same test command is one line.
	Repeats int `json:"repeats"`
}

// Review summarises what a session was allowed to do.
type Review struct {
	TaskID string `json:"task_id"`
	// Total counts every decision, so the summary can give the fraction that
	// went unattended.
	Total      int            `json:"total"`
	Unattended int            `json:"unattended"`
	Blocked    int            `json:"blocked"`
	Groups     []*ReviewGroup `json:"groups"`
}

// unattendedBy are the answers that reached an agent without a person seeing
// them. A rule is absent: it was a decision made once, and surfacing it every
// time it fires buries the requests nobody considered.
var unattendedBy = map[string]bool{"auto": true}

// ReviewTask gathers every decided request for a task, grouped by tool, with
// identical commands folded together.
//
// A session that ran one test command four hundred times produces four hundred
// rows that say nothing and hide the one `rm -rf` that also went through.
func (s *Store) ReviewTask(taskID string) (*Review, error) {
	out := &Review{TaskID: taskID}
	err := s.guard(func() error {
		rows, err := s.db.Query(`SELECT `+permColumns+` FROM permission
			WHERE task_id = ? AND decided_at IS NOT NULL
			ORDER BY decided_at ASC`, taskID)
		if err != nil {
			return err
		}
		defer rows.Close()

		byTool := map[string]*ReviewGroup{}
		// Folding is per tool and per exact command, so the same string under
		// two tools stays two entries.
		folded := map[string]*ReviewEntry{}

		for rows.Next() {
			p, err := scanPermission(rows)
			if err != nil {
				return err
			}
			g := byTool[p.Tool]
			if g == nil {
				g = &ReviewGroup{Tool: p.Tool, byCmd: map[string]bool{}}
				byTool[p.Tool] = g
				out.Groups = append(out.Groups, g)
			}

			unattended := unattendedBy[p.DecidedBy]
			blocked := p.Decision == "block"

			out.Total++
			g.Count++
			if unattended {
				out.Unattended++
				g.Unattended++
			}
			if blocked {
				out.Blocked++
				g.Blocked++
			}

			key := p.Tool + "\x00" + p.Command + "\x00" + p.Decision + "\x00" + p.DecidedBy
			if e := folded[key]; e != nil {
				e.Repeats++
				continue
			}
			at := ""
			if p.DecidedAt != nil {
				at = ts(*p.DecidedAt)
			}
			e := &ReviewEntry{
				ID: p.ID, Command: p.Command, Decision: p.Decision,
				By: p.DecidedBy, At: at, Unattended: unattended, Repeats: 1,
			}
			folded[key] = e
			g.Entries = append(g.Entries, e)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}

	// Ordered by unattended decisions, then by volume. A tool whose every call
	// matched a rule you wrote sorts last.
	sort.SliceStable(out.Groups, func(i, j int) bool {
		a, b := out.Groups[i], out.Groups[j]
		if a.Unattended != b.Unattended {
			return a.Unattended > b.Unattended
		}
		if a.Count != b.Count {
			return a.Count > b.Count
		}
		return strings.Compare(a.Tool, b.Tool) < 0
	})
	// Inside a group, unattended first, then the most repeated.
	for _, g := range out.Groups {
		sort.SliceStable(g.Entries, func(i, j int) bool {
			a, b := g.Entries[i], g.Entries[j]
			if a.Unattended != b.Unattended {
				return a.Unattended
			}
			return a.Repeats > b.Repeats
		})
	}
	return out, nil
}
