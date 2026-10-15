package store

import "time"

// A COSTED DELIVERY AS THE HUB READS IT, to put what the orchestrator spent answering an item's cards on the item
// (item lifecycle L5, docs/rnd/item-lifecycle-design.md section 6.5). Read only.

// DeliveryExport is one costed delivery. A turn that answered several deliveries costs the first and marks the rest as
// sharing it, so Group is the id of the one that carries the tokens ("" on the carrier itself, whose Input and the
// rest are the turn's). The hub divides a turn equally among the items its group touches.
type DeliveryExport struct {
	ID string `json:"id"`
	// TaskID is who the text went to, FromTask the card that sent it ("" when the sender is not a card here: the
	// operator, atrium, or a card another room holds).
	TaskID       string    `json:"task_id"`
	FromTask     string    `json:"from_task,omitempty"`
	From         string    `json:"from,omitempty"`
	Kind         string    `json:"kind"`
	At           time.Time `json:"at"`
	CostedAt     time.Time `json:"costed_at"`
	Group        string    `json:"group,omitempty"`
	Model        string    `json:"model,omitempty"`
	Input        int64     `json:"input"`
	Output       int64     `json:"output"`
	CacheWrite5m int64     `json:"cache_write_5m"`
	CacheWrite1h int64     `json:"cache_write_1h"`
	CacheRead    int64     `json:"cache_read"`
}

// DeliveriesSince calls each with every costed delivery made at or after since, oldest first. The dollar figure is left
// out on purpose: the hub prices from tokens.
func (s *Store) DeliveriesSince(since time.Time, each func(*DeliveryExport) error) error {
	var stop error
	err := s.guard(func() error {
		stop = nil
		rows, err := s.db.Query(`SELECT d.id, d.task_id, COALESCE((SELECT t.id FROM task t
				WHERE d.from_name <> '' AND (t.wire_name = d.from_name OR t.alias = d.from_name) LIMIT 1), ''),
				d.from_name, d.kind, d.at, d.costed_at, d.group_id, d.model, d.input, d.output, d.cache_write_5m,
				d.cache_write_1h, d.cache_read
			FROM delivery d WHERE d.at >= ? AND d.costed_at != '' ORDER BY d.at, d.id`, ts(since))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var d DeliveryExport
			var at, costed string
			if err := rows.Scan(&d.ID, &d.TaskID, &d.FromTask, &d.From, &d.Kind, &at, &costed, &d.Group, &d.Model, &d.Input,
				&d.Output, &d.CacheWrite5m, &d.CacheWrite1h, &d.CacheRead); err != nil {
				return err
			}
			if d.At, err = parseTS(at); err != nil {
				return err
			}
			if d.CostedAt, err = parseTS(costed); err != nil {
				return err
			}
			if stop = each(&d); stop != nil {
				return nil
			}
		}
		return rows.Err()
	})
	if err != nil {
		return err
	}
	return stop
}
