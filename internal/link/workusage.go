package link

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/base/tokenprice"
	"github.com/dovholuknf/atrium/internal/hubstore"
)

// THE USAGE PULL (item lifecycle L5, docs/rnd/item-lifecycle-design.md section 6.2). On a tick the hub reads each room's
// usage export (a card's own turns) and delivery export (what the orchestrator spent answering) through the proxy it
// already has, and keeps the rows that belong to an item's cards in `work_usage`.
//
//   - ONE CURSOR PER ROOM, with an hour of overlap, and the key makes a re-read safe: a replayed pull adds nothing.
//   - A CARD ATTACHED LATER pulls its earlier rows on the next tick, because the read starts at the earliest attachment
//     made since the last pull when that is before the cursor.
//   - A ROW COUNTS when its card is attached and the row ended at or after the attachment. A row for a card with no
//     attachment is not stored.
//   - A RELAY OR ORCHESTRATOR CARD'S ROWS ARE COORDINATION whatever step they are shown against, and a delivery to a card
//     that is on an item is not counted again: that turn is in the card's own usage row.
//   - IT NEVER FAILS ANYONE. A room that does not answer is skipped and its cursor stays where it was.

// usageOverlap is how far before the cursor a pull reads again, which a delivery costed late needs.
const usageOverlap = time.Hour

// usagePullTick is how often the rooms are read when nothing asked sooner.
const usagePullTick = time.Minute

// usageRowIn is a room's usage export line, the fields the hub keeps.
type usageRowIn struct {
	ID           string    `json:"id"`
	TaskID       string    `json:"task_id"`
	Started      time.Time `json:"started_at"`
	Ended        time.Time `json:"ended_at"`
	Model        string    `json:"model"`
	Input        int64     `json:"input"`
	Output       int64     `json:"output"`
	CacheWrite5m int64     `json:"cache_write_5m"`
	CacheWrite1h int64     `json:"cache_write_1h"`
	CacheRead    int64     `json:"cache_read"`
}

// deliveryRowIn is a room's delivery export line.
type deliveryRowIn struct {
	ID           string    `json:"id"`
	TaskID       string    `json:"task_id"`
	FromTask     string    `json:"from_task"`
	At           time.Time `json:"at"`
	CostedAt     time.Time `json:"costed_at"`
	Group        string    `json:"group"`
	Model        string    `json:"model"`
	Input        int64     `json:"input"`
	Output       int64     `json:"output"`
	CacheWrite5m int64     `json:"cache_write_5m"`
	CacheWrite1h int64     `json:"cache_write_1h"`
	CacheRead    int64     `json:"cache_read"`
}

// roomLinesFunc reads one line per call from a room's ndjson route, a function so a test need not attach a room.
type roomLinesFunc func(ctx context.Context, room, path string, each func(line []byte) error) error

// readRoomLines is roomLinesFunc for a real room.
func (p *Proxy) readRoomLines(ctx context.Context, room, path string, each func([]byte) error) error {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+hostFor(room)+path, nil)
	if err != nil {
		return err
	}
	res, err := p.roomClient(room).Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<16))
		return fmt.Errorf("the room answered %d", res.StatusCode)
	}
	sc := bufio.NewScanner(io.LimitReader(res.Body, 256<<20))
	sc.Buffer(make([]byte, 0, 64<<10), 4<<20)
	for sc.Scan() {
		if b := strings.TrimSpace(sc.Text()); b != "" {
			if err := each([]byte(b)); err != nil {
				return err
			}
		}
	}
	return sc.Err()
}

func (p *Proxy) linesFor() roomLinesFunc {
	p.mu.Lock()
	f := p.roomLines
	p.mu.Unlock()
	if f != nil {
		return f
	}
	return p.readRoomLines
}

// PullStats is what one room's pull did.
type PullStats struct {
	Room  string `json:"room"`
	Read  int    `json:"read"`
	Kept  int    `json:"kept"`
	Added int    `json:"added"`
	Since string `json:"since"`
}

// isCoordCard is a card whose spend is coordination because of what it is for.
func isCoordCard(c hubstore.WorkCard) bool {
	return c.Role == "relay" || c.Role == "orchestrator"
}

// PullRoomUsage reads one room's rows and keeps the ones that belong to an item. It leaves the cursor where it was on any
// failure, so the next tick reads the same span again.
func (p *Proxy) PullRoomUsage(ctx context.Context, room string, now time.Time) (PullStats, error) {
	st := p.itemStore()
	out := PullStats{Room: room}
	if st == nil {
		return out, nil
	}
	cards, err := st.CardsOnRoom(room)
	if err != nil || len(cards) == 0 {
		return out, err
	}
	byCard := map[string][]hubstore.WorkCard{}
	earliest := ""
	for _, c := range cards {
		byCard[c.Card] = append(byCard[c.Card], c)
		if earliest == "" || c.AttachedAt < earliest {
			earliest = c.AttachedAt
		}
	}
	since := rowTime(earliest)
	cur, pulledAt, have, err := st.UsageCursor(room)
	if err != nil {
		return out, err
	}
	if have {
		since = rowTime(cur).Add(-usageOverlap)
		// A card attached since the last pull may have rows from before it was attached to this hub.
		for _, c := range cards {
			if c.AttachedAt >= pulledAt {
				if t := rowTime(c.AttachedAt); !t.IsZero() && t.Before(since) {
					since = t
				}
			}
		}
	}
	if since.IsZero() {
		since = now.Add(-7 * 24 * time.Hour)
	}
	out.Since = since.UTC().Format(time.RFC3339)
	lines := p.linesFor()
	q := "?since=" + url.QueryEscape(out.Since)

	var rows []hubstore.WorkUsageRow
	newest := rowTime(cur)
	err = lines(ctx, room, "/v1/usage/export"+q, func(b []byte) error {
		var u usageRowIn
		if json.Unmarshal(b, &u) != nil || u.ID == "" {
			return nil
		}
		out.Read++
		if u.Ended.After(newest) {
			newest = u.Ended
		}
		for _, c := range byCard[u.TaskID] {
			if u.Ended.Before(rowTime(c.AttachedAt)) {
				continue
			}
			class := hubstore.ClassStep
			if isCoordCard(c) {
				class = hubstore.ClassCoord
			}
			rows = append(rows, hubstore.WorkUsageRow{Room: room, UsageID: u.ID, BacklogID: c.BacklogID, Card: c.Card,
				Class: class, Source: "usage", Model: u.Model, StartedAt: u.Started.UTC().Format(time.RFC3339Nano),
				EndedAt: u.Ended.UTC().Format(time.RFC3339Nano), Tokens: tokenprice.Tokens{Input: u.Input,
					CacheWrite5m: u.CacheWrite5m, CacheWrite1h: u.CacheWrite1h, CacheRead: u.CacheRead, Output: u.Output}})
		}
		return nil
	})
	if err != nil {
		return out, fmt.Errorf("usage of %s: %w", room, err)
	}

	var ds []deliveryRowIn
	err = lines(ctx, room, "/v1/deliveries/export"+q, func(b []byte) error {
		var d deliveryRowIn
		if json.Unmarshal(b, &d) == nil && d.ID != "" {
			ds = append(ds, d)
		}
		return nil
	})
	if err != nil {
		// An older room has no such route, and its cards' own turns are still kept.
		log.Printf("[link] deliveries of %s not read: %v", room, err)
		ds = nil
	}
	rows = append(rows, deliveryRows(room, ds, byCard)...)
	out.Read += len(ds)
	out.Kept = len(rows)
	if out.Added, err = st.AddWorkUsage(rows); err != nil {
		return out, err
	}
	next := cur
	if newest.After(rowTime(cur)) || cur == "" {
		next = newest.UTC().Format(time.RFC3339Nano)
	}
	if next == "" {
		next = out.Since
	}
	return out, st.SetUsageCursor(room, next, now.UTC().Format(time.RFC3339Nano))
}

// deliveryRows puts what the orchestrator spent answering an item's cards on the items. A turn that answered several
// deliveries is one carrier with the tokens and the rest sharing it. The items it touches are the ones the senders are
// on, and it is divided EQUALLY among them with no token lost. A turn for a card that is itself on an item is left out,
// since that turn is in the card's own usage row and counting it here would count it twice.
func deliveryRows(room string, ds []deliveryRowIn, byCard map[string][]hubstore.WorkCard) []hubstore.WorkUsageRow {
	type turn struct {
		carrier deliveryRowIn
		items   map[string]string
		skip    bool
	}
	turns := map[string]*turn{}
	var order []string
	for _, d := range ds {
		if d.Group == "" {
			if turns[d.ID] == nil {
				turns[d.ID] = &turn{items: map[string]string{}}
				order = append(order, d.ID)
			}
			turns[d.ID].carrier = d
		}
	}
	for _, d := range ds {
		id := d.Group
		if id == "" {
			id = d.ID
		}
		t := turns[id]
		if t == nil {
			continue
		}
		if len(byCard[d.TaskID]) > 0 {
			t.skip = true
		}
		for _, c := range byCard[d.FromTask] {
			if !d.At.Before(rowTime(c.AttachedAt)) {
				t.items[c.BacklogID] = c.Card
			}
		}
	}
	var out []hubstore.WorkUsageRow
	for _, id := range order {
		t := turns[id]
		tok := tokenprice.Tokens{Input: t.carrier.Input, CacheWrite5m: t.carrier.CacheWrite5m,
			CacheWrite1h: t.carrier.CacheWrite1h, CacheRead: t.carrier.CacheRead, Output: t.carrier.Output}
		if t.skip || len(t.items) == 0 || tok.Zero() {
			continue
		}
		ids := sortedKeys(t.items)
		at := t.carrier.At.UTC().Format(time.RFC3339Nano)
		for i, part := range tokenprice.Divide(tok, equalWeights(len(ids))) {
			out = append(out, hubstore.WorkUsageRow{Room: room, UsageID: "d:" + id, BacklogID: ids[i], Card: t.items[ids[i]],
				Class: hubstore.ClassCoord, Source: "delivery", Model: t.carrier.Model, StartedAt: at, EndedAt: at, Tokens: part})
		}
	}
	return out
}

// PullWorkUsage reads every attached room that holds a card of an item.
func (p *Proxy) PullWorkUsage(ctx context.Context) []PullStats {
	if p.itemStore() == nil || p.hub == nil {
		return nil
	}
	var all []PullStats
	for _, a := range p.hub.Rooms() {
		if ctx.Err() != nil {
			break
		}
		s, err := p.PullRoomUsage(ctx, a.Name, time.Now())
		if err != nil {
			log.Printf("[link] usage pull of %s: %v", a.Name, err)
		}
		all = append(all, s)
	}
	return all
}

// RunWorkUsage pulls the rooms' usage every usagePullTick until ctx ends.
func (p *Proxy) RunWorkUsage(ctx context.Context) {
	t := time.NewTicker(usagePullTick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			p.PullWorkUsage(ctx)
		}
	}
}
