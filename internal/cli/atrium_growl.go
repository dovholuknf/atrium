package cli

import (
	"errors"
	"time"

	"github.com/dovholuknf/atrium/internal/hubstore"
	"github.com/dovholuknf/atrium/internal/link"
)

// growlStore adapts the hub's database to what the growler asks of it, by room
// name, the way notifyStore does for the notifier.
type growlStore struct {
	notifyStore
}

func (g growlStore) roomID(room string) (string, error) {
	r, err := g.s.ByName(room)
	if err != nil {
		return "", err
	}
	return r.ID, nil
}

func toHubGrowl(r link.GrowlRow) hubstore.Growl {
	return hubstore.Growl{ID: r.ID, CardID: r.CardID, Reason: r.Reason, Title: r.Title, Body: r.Body,
		Subject: r.Subject}
}

func fromHubGrowl(g hubstore.Growl) link.GrowlRow {
	r := link.GrowlRow{
		ID: g.ID, Room: g.RoomName, CardID: g.CardID, Reason: g.Reason, Title: g.Title, Body: g.Body,
		Subject: g.Subject, RaisedAt: g.RaisedAt, State: g.State, Reminders: g.Reminders,
		ChangedAt: g.ChangedAt, ChangedVia: g.ChangedVia, ChangedTab: g.ChangedTab,
	}
	if g.Until != nil {
		r.Until = g.Until.UTC().Format(time.RFC3339Nano)
	}
	return r
}

func (g growlStore) Sync(room string, reasons []string, want []link.GrowlRow, present map[string]bool) (
	[]string, bool, error) {
	id, err := g.roomID(room)
	if err != nil {
		return nil, false, err
	}
	out := make([]hubstore.Growl, 0, len(want))
	for _, w := range want {
		out = append(out, toHubGrowl(w))
	}
	return g.s.GrowlSync(id, reasons, out, present)
}

func (g growlStore) Room(room, reason string, on bool, row link.GrowlRow) (bool, bool, error) {
	id, err := g.roomID(room)
	if err != nil {
		return false, false, err
	}
	return g.s.GrowlRoom(id, reason, on, toHubGrowl(row))
}

// Raise and End are the question a change request into main is, under an id of its own. See link/changerequest.go.
func (g growlStore) Raise(room string, row link.GrowlRow) (bool, error) {
	id, err := g.roomID(room)
	if err != nil {
		return false, err
	}
	return g.s.GrowlRaise(id, toHubGrowl(row))
}

func (g growlStore) End(id string) (bool, error) { return g.s.GrowlEnd(id) }

func (g growlStore) Fill(id, subject, body string) (bool, error) {
	return g.s.GrowlFill(id, subject, body)
}

func (g growlStore) Live() ([]link.GrowlRow, error) {
	rows, err := g.s.GrowlLive()
	if err != nil {
		return nil, err
	}
	out := make([]link.GrowlRow, 0, len(rows))
	for _, r := range rows {
		// A ROOM REMOVED takes its growlers off every screen at once. The prune
		// deletes the rows within the hour.
		if r.RoomName == "" {
			continue
		}
		out = append(out, fromHubGrowl(r))
	}
	return out, nil
}

func (g growlStore) Act(id, state string, until time.Time, via, tab string) (link.GrowlRow, error) {
	row, err := g.s.GrowlAct(id, state, until, via, tab)
	switch {
	case errors.Is(err, hubstore.ErrGrowlNotFound):
		return link.GrowlRow{}, link.ErrGrowlNotFound
	case errors.Is(err, hubstore.ErrGrowlStale):
		return fromHubGrowl(row), link.ErrGrowlStale
	case err != nil:
		return link.GrowlRow{}, err
	}
	return fromHubGrowl(row), nil
}

func (g growlStore) Wake() ([]string, error) { return g.s.GrowlWake() }

func (g growlStore) Reminded(id string, n int) error { return g.s.GrowlReminded(id, n) }

func (g growlStore) Prune() error {
	_, err := g.s.GrowlPrune()
	return err
}
