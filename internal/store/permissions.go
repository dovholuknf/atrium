package store

import (
	"database/sql"
	"errors"
)

// RecordPermission stores a pending permission request. dedupKey makes the
// call idempotent: if the daemon dies between a decision and its write, the
// agent reconnects and re-posts the same request, and the operator must not be
// asked the same question twice. A repeated key returns the existing row, along
// with whether it has already been decided.
func (s *Store) RecordPermission(taskID, tool, command, dedupKey, details string) (*Permission, bool, error) {
	var (
		p       *Permission
		decided bool
	)
	err := s.guard(func() error {
		p, decided = nil, false
		if dedupKey != "" {
			// Scoped to the task: the key means "this agent's same request",
			// so an identical key from another agent is a different question.
			existing, err := s.permissionBy(`task_id = ? AND dedup_key = ?`, taskID, dedupKey)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			if existing != nil {
				p, decided = existing, existing.DecidedAt != nil
				return nil
			}
		}
		n := now()
		rec := &Permission{
			ID: newID(), TaskID: taskID, Tool: tool, Command: command,
			RequestedAt: n, DedupKey: dedupKey, Details: details,
		}
		// The key is stored as NULL when absent. UNIQUE permits many NULLs but
		// only one empty string, so defaulting to '' would make the second
		// un-keyed request collide with the first.
		if _, err := s.db.Exec(
			`INSERT INTO permission (id, task_id, tool, command, requested_at, dedup_key, details)
			 VALUES (?,?,?,?,?,?,?)`,
			rec.ID, rec.TaskID, rec.Tool, rec.Command, ts(rec.RequestedAt),
			nullable(rec.DedupKey), rec.Details); err != nil {
			return err
		}
		if err := s.appendEvent(taskID, EventPermRequested, map[string]any{
			"id": rec.ID, "tool": tool, "command": command,
		}); err != nil {
			return err
		}
		p = rec
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return p, decided, nil
}

const permColumns = `id, task_id, tool, command, requested_at, decided_at,
	decision, reason, dedup_key, decided_by, rule_created, details`

func scanPermission(sc interface{ Scan(...any) error }) (*Permission, error) {
	var (
		p         Permission
		req       string
		decidedAt sql.NullString
		decision  sql.NullString
		dedup     sql.NullString
	)
	if err := sc.Scan(&p.ID, &p.TaskID, &p.Tool, &p.Command, &req, &decidedAt,
		&decision, &p.Reason, &dedup, &p.DecidedBy, &p.RuleCreated, &p.Details); err != nil {
		return nil, err
	}
	p.DedupKey = dedup.String
	var err error
	if p.RequestedAt, err = parseTS(req); err != nil {
		return nil, err
	}
	if decidedAt.Valid && decidedAt.String != "" {
		d, err := parseTS(decidedAt.String)
		if err != nil {
			return nil, err
		}
		p.DecidedAt = &d
	}
	p.Decision = decision.String
	return &p, nil
}

func (s *Store) permissionBy(where string, args ...any) (*Permission, error) {
	row := s.db.QueryRow(`SELECT `+permColumns+` FROM permission WHERE `+where+` LIMIT 1`, args...)
	return scanPermission(row)
}

// DecidedBySelf marks a decision the operator made by hand, as opposed to one
// a standing rule answered.
const DecidedBySelf = "you"

// History returns recently decided requests, newest first. This is the answer
// to "what have I been approving," and it is the only place an auto-approved
// request is ever visible, since those never reach the pending queue.
func (s *Store) History(limit int) ([]*Permission, error) {
	if limit <= 0 {
		limit = 100
	}
	var out []*Permission
	err := s.guard(func() error {
		out = nil
		rows, err := s.db.Query(`SELECT `+permColumns+` FROM permission
			WHERE decided_at IS NOT NULL ORDER BY decided_at DESC LIMIT ?`, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			p, err := scanPermission(rows)
			if err != nil {
				return err
			}
			out = append(out, p)
		}
		return rows.Err()
	})
	return out, err
}

// DecidePermission resolves a request the operator answered by hand.
func (s *Store) DecidePermission(id, decision, reason string) (*Permission, error) {
	return s.DecidePermissionBy(id, decision, reason, DecidedBySelf)
}

// DecidePermissionBy resolves a request and records what answered it. Deciding
// an already-decided request is a no-op that returns the original decision, so
// a replayed request cannot flip an answer.
func (s *Store) DecidePermissionBy(id, decision, reason, by string) (*Permission, error) {
	var p *Permission
	err := s.guard(func() error {
		existing, err := s.permissionBy(`id = ?`, id)
		if err != nil {
			return err
		}
		if existing.DecidedAt != nil {
			p = existing
			return nil
		}
		n := now()
		if _, err := s.db.Exec(
			`UPDATE permission SET decided_at = ?, decision = ?, reason = ?, decided_by = ?
			 WHERE id = ?`,
			ts(n), decision, reason, by, id); err != nil {
			return err
		}
		existing.DecidedAt, existing.Decision, existing.Reason = &n, decision, reason
		existing.DecidedBy = by
		if err := s.appendEvent(existing.TaskID, EventPermDecided, map[string]any{
			"id": id, "decision": decision, "reason": reason,
		}); err != nil {
			return err
		}
		p = existing
		return nil
	})
	return p, err
}

// RewriteCommand replaces the command on a pending request, recording that the
// human changed it. Only pending requests can be rewritten: editing something
// already answered would make the audit log lie.
func (s *Store) RewriteCommand(id, command string) error {
	return s.guard(func() error {
		existing, err := s.permissionBy(`id = ?`, id)
		if err != nil {
			return err
		}
		if existing.DecidedAt != nil {
			return errors.New("cannot rewrite a request that is already decided")
		}
		if existing.Command == command {
			return nil
		}
		if _, err := s.db.Exec(`UPDATE permission SET command = ? WHERE id = ?`, command, id); err != nil {
			return err
		}
		return s.appendEvent(existing.TaskID, EventPermRequested, map[string]any{
			"id": id, "rewritten": true, "from": existing.Command, "to": command,
		})
	})
}

// NoteRuleCreated records that answering this request also established a
// standing rule, so the log can distinguish "approve once" from "always".
func (s *Store) NoteRuleCreated(id, pattern string) error {
	return s.guard(func() error {
		_, err := s.db.Exec(`UPDATE permission SET rule_created = ? WHERE id = ?`, pattern, id)
		return err
	})
}

// GetPermission returns one permission by id.
func (s *Store) GetPermission(id string) (*Permission, error) {
	var p *Permission
	err := s.guard(func() error {
		got, err := s.permissionBy(`id = ?`, id)
		if err != nil {
			return err
		}
		p = got
		return nil
	})
	return p, err
}

// PendingForTask returns the undecided requests belonging to one task.
func (s *Store) PendingForTask(taskID string) ([]*Permission, error) {
	var out []*Permission
	err := s.guard(func() error {
		out = nil
		rows, err := s.db.Query(
			`SELECT id FROM permission WHERE task_id = ? AND decided_at IS NULL
			 ORDER BY requested_at ASC`, taskID)
		if err != nil {
			return err
		}
		var ids []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		for _, id := range ids {
			p, err := s.permissionBy(`id = ?`, id)
			if err != nil {
				return err
			}
			out = append(out, p)
		}
		return nil
	})
	return out, err
}

// PendingPermissions returns undecided requests, oldest first.
func (s *Store) PendingPermissions() ([]*Permission, error) {
	var out []*Permission
	err := s.guard(func() error {
		out = nil
		rows, err := s.db.Query(`SELECT id FROM permission
			WHERE decided_at IS NULL ORDER BY requested_at ASC`)
		if err != nil {
			return err
		}
		var ids []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		for _, id := range ids {
			p, err := s.permissionBy(`id = ?`, id)
			if err != nil {
				return err
			}
			out = append(out, p)
		}
		return nil
	})
	return out, err
}
