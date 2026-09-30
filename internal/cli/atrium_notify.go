package cli

import (
	"encoding/json"

	"github.com/dovholuknf/atrium/internal/hubstore"
	"github.com/dovholuknf/atrium/internal/link"
)

// notifyStore adapts the hub's database to what the notifier asks of it, by
// room name, so internal/link never learns there are room ids.
type notifyStore struct{ s *hubstore.Store }

func (n notifyStore) Setting(name string) (string, error) { return n.s.Setting(name) }

func (n notifyStore) SetSetting(name, value string) error { return n.s.SetSetting(name, value) }

func (n notifyStore) Record(room string, ids map[string]string, present []string, silent bool) ([]string, error) {
	r, err := n.s.ByName(room)
	if err != nil {
		return nil, err
	}
	return n.s.NotifyRecord(r.ID, ids, present, silent)
}

func (n notifyStore) Rooms() ([]string, error) {
	rooms, err := n.s.Rooms()
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(rooms))
	for _, r := range rooms {
		out = append(out, r.Name)
	}
	return out, nil
}

func (n notifyStore) Cards(room string) ([]link.CardState, error) {
	r, err := n.s.ByName(room)
	if err != nil {
		return nil, err
	}
	cards, err := n.s.Cards(r.ID)
	if err != nil {
		return nil, err
	}
	out := make([]link.CardState, 0, len(cards))
	for _, c := range cards {
		out = append(out, link.CardState{ID: c.ID, Status: c.Status, Payload: json.RawMessage(c.Payload)})
	}
	return out, nil
}

func (n notifyStore) Prune() error {
	_, err := n.s.NotifyPrune()
	return err
}
