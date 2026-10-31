//go:build integration

package link

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/hubstore"
	"github.com/dovholuknf/atrium/internal/link/conn"
)

// testGrowlStore is internal/cli's growlStore, again, because link cannot
// import cli. It maps and nothing else.
type testGrowlStore struct{ s *hubstore.Store }

func (g testGrowlStore) id(room string) (string, error) {
	r, err := g.s.ByName(room)
	if err != nil {
		return "", err
	}
	return r.ID, nil
}

func toHub(r GrowlRow) hubstore.Growl {
	return hubstore.Growl{ID: r.ID, CardID: r.CardID, Reason: r.Reason, Title: r.Title, Body: r.Body, Subject: r.Subject}
}

func fromHub(g hubstore.Growl) GrowlRow {
	r := GrowlRow{ID: g.ID, Room: g.RoomName, CardID: g.CardID, Reason: g.Reason, Title: g.Title, Body: g.Body,
		Subject: g.Subject, RaisedAt: g.RaisedAt, State: g.State, Reminders: g.Reminders, ChangedAt: g.ChangedAt,
		ChangedVia: g.ChangedVia, ChangedTab: g.ChangedTab}
	if g.Until != nil {
		r.Until = g.Until.Format(time.RFC3339Nano)
	}
	return r
}

func (g testGrowlStore) Sync(room string, reasons []string, want []GrowlRow, present map[string]bool) ([]string, bool, error) {
	id, err := g.id(room)
	if err != nil {
		return nil, false, err
	}
	var out []hubstore.Growl
	for _, w := range want {
		out = append(out, toHub(w))
	}
	return g.s.GrowlSync(id, reasons, out, present)
}

func (g testGrowlStore) Room(room, reason string, on bool, row GrowlRow) (bool, bool, error) {
	id, err := g.id(room)
	if err != nil {
		return false, false, err
	}
	return g.s.GrowlRoom(id, reason, on, toHub(row))
}

func (g testGrowlStore) Raise(room string, row GrowlRow) (bool, error) {
	id, err := g.id(room)
	if err != nil {
		return false, err
	}
	return g.s.GrowlRaise(id, toHub(row))
}

func (g testGrowlStore) End(id string) (bool, error) { return g.s.GrowlEnd(id) }

func (g testGrowlStore) Fill(id, subject, body string) (bool, error) {
	return g.s.GrowlFill(id, subject, body)
}

func (g testGrowlStore) Live() ([]GrowlRow, error) {
	rows, err := g.s.GrowlLive()
	var out []GrowlRow
	for _, r := range rows {
		out = append(out, fromHub(r))
	}
	return out, err
}

func (g testGrowlStore) Act(id, state string, until time.Time, via, tab string) (GrowlRow, error) {
	row, err := g.s.GrowlAct(id, state, until, via, tab)
	switch {
	case errors.Is(err, hubstore.ErrGrowlNotFound):
		return GrowlRow{}, ErrGrowlNotFound
	case errors.Is(err, hubstore.ErrGrowlStale):
		return fromHub(row), ErrGrowlStale
	}
	return fromHub(row), err
}

func (g testGrowlStore) Wake() ([]string, error) { return g.s.GrowlWake() }

func (g testGrowlStore) Reminded(id string, n int) error { return g.s.GrowlReminded(id, n) }

func (g testGrowlStore) Prune() error { _, err := g.s.GrowlPrune(); return err }

func (g testGrowlStore) Setting(n string) (string, error) { return g.s.Setting(n) }

func (g testGrowlStore) SetSetting(n, v string) error { return g.s.SetSetting(n, v) }

func (g testGrowlStore) Rooms() ([]string, error) {
	rooms, err := g.s.Rooms()
	var out []string
	for _, r := range rooms {
		out = append(out, r.Name)
	}
	return out, err
}

func (g testGrowlStore) Cards(room string) ([]conn.CardState, error) {
	id, err := g.id(room)
	if err != nil {
		return nil, err
	}
	cards, err := g.s.Cards(id)
	var out []conn.CardState
	for _, c := range cards {
		out = append(out, conn.CardState{ID: c.ID, Status: c.Status, Payload: json.RawMessage(c.Payload)})
	}
	return out, err
}
func do(t *testing.T, method, url, ctype, body string) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(method, url, strings.NewReader(body))
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out map[string]any
	raw, _ := io.ReadAll(res.Body)
	_ = json.Unmarshal(raw, &out)
	return res.StatusCode, out
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
