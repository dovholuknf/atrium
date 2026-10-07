package link

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dovholuknf/atrium/internal/forge"
	"github.com/dovholuknf/atrium/internal/store"
	"github.com/dovholuknf/atrium/scripts/recognisers"
)

// THE HUB OWNS THE RECOGNISER TABLE. Design: docs/rnd/card-lifecycle-design.md, section 2.
//
//	POST   /_forge/recognisers     (a room, on the git kind)  the hub's rows, for the room to match a pasted link against
//	GET    /v1/recognisers         the board, any view        the hub's rows
//	PUT    /v1/recognisers/{id}    the board, any view        save one
//	DELETE /v1/recognisers/{id}    the board, any view        remove one
//
// A link is recognised before anyone knows which room it goes to, so the rows live in the one place every room asks.
// They used to be per room and loaded by hand, and a paste placed on a room nobody had loaded failed with "no
// recogniser matches this" (u-pulls-no-recogniser-on-hub). The room still does the matching, the filling and the look
// at its own disk, from the hub's rows. See daemon/recognise.go.
//
// SEEDED ONCE PER ROW. The rows in scripts/recognisers are built in. A seed row the hub has never held is added the
// first time the table is read, and its id is remembered, so a row the operator edited is never overwritten and one
// they deleted does not come back.
//
// The rows are two hub settings, the rows and the seeded ids, both JSON. The table is small and edited by hand.

const (
	SettingRecognisers       = "recognisers.rows"
	SettingRecognisersSeeded = "recognisers.seeded"
	// SettingRecognisersBackfill is set once the seeded rows got the default repo field. See backfillDefaultRepo.
	SettingRecognisersBackfill = "recognisers.backfilled"
)

// HubRecognisers is the answer to a room's question, and the board's list.
type HubRecognisers struct {
	Recognisers []*store.Recogniser `json:"recognisers"`
}

// recogniserSeed is the built-in rows. A seam for tests.
var recogniserSeed = recognisers.Seed

// recognisersMu serialises the read, seed and write of the two settings.
var recognisersMu sync.Mutex

// hubRecogniserRows reads the hub's rows, seeding any built-in row it has never held.
func hubRecogniserRows(st ForgeSettings) ([]*store.Recogniser, error) {
	recognisersMu.Lock()
	defer recognisersMu.Unlock()
	return hubRecogniserRowsLocked(st)
}

func hubRecogniserRowsLocked(st ForgeSettings) ([]*store.Recogniser, error) {
	rows, err := readRecogniserRows(st)
	if err != nil {
		return nil, err
	}
	seeded := map[string]bool{}
	if raw, err := st.HubSetting(SettingRecognisersSeeded); err == nil && strings.TrimSpace(raw) != "" {
		var ids []string
		if err := json.Unmarshal([]byte(raw), &ids); err != nil {
			log.Printf("[hub] %s does not read (%v), so every built-in row is offered again", SettingRecognisersSeeded, err)
		}
		for _, id := range ids {
			seeded[id] = true
		}
	}
	seed, err := recogniserSeed()
	if err != nil {
		// A broken seed is a broken build, and the operator's rows still work.
		log.Printf("[hub] the built-in recognisers do not read: %v", err)
		return rows, nil
	}
	held := map[string]bool{}
	for _, r := range rows {
		held[r.ID] = true
	}
	changed := backfillDefaultRepo(st, rows, seed)
	for _, s := range seed {
		if seeded[s.ID] {
			continue
		}
		seeded[s.ID] = true
		changed = true
		if held[s.ID] {
			continue
		}
		s := s
		s.CreatedAt = time.Now().UTC()
		rows = append(rows, &s)
	}
	if !changed {
		return rows, nil
	}
	if err := writeRecogniserRows(st, rows); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(seeded))
	for id := range seeded {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	raw, _ := json.Marshal(ids)
	if err := st.SetHubSetting(SettingRecognisersSeeded, string(raw)); err != nil {
		return nil, err
	}
	return rows, nil
}

// backfillDefaultRepo gives a seeded row held from before the field existed the seed's default repo, once per hub, so
// an operator who later empties it on purpose keeps it empty. True when a row changed.
func backfillDefaultRepo(st ForgeSettings, rows []*store.Recogniser, seed []store.Recogniser) bool {
	if raw, err := st.HubSetting(SettingRecognisersBackfill); err == nil && strings.TrimSpace(raw) != "" {
		return false
	}
	byID := map[string]string{}
	for _, s := range seed {
		byID[s.ID] = s.DefaultRepo
	}
	changed := false
	for _, r := range rows {
		if def := byID[r.ID]; def != "" && r.DefaultRepo == "" {
			r.DefaultRepo, changed = def, true
		}
	}
	if err := st.SetHubSetting(SettingRecognisersBackfill, "default_repo"); err != nil {
		log.Printf("[hub] %s did not save: %v", SettingRecognisersBackfill, err)
	}
	return changed
}

func readRecogniserRows(st ForgeSettings) ([]*store.Recogniser, error) {
	raw, err := st.HubSetting(SettingRecognisers)
	if err != nil || strings.TrimSpace(raw) == "" {
		return []*store.Recogniser{}, nil
	}
	var rows []*store.Recogniser
	if err := json.Unmarshal([]byte(raw), &rows); err != nil {
		return nil, err
	}
	return sortedRecognisers(rows), nil
}

func writeRecogniserRows(st ForgeSettings, rows []*store.Recogniser) error {
	raw, err := json.Marshal(sortedRecognisers(rows))
	if err != nil {
		return err
	}
	return st.SetHubSetting(SettingRecognisers, string(raw))
}

// sortedRecognisers is the order the rows are asked in: rank, then id.
func sortedRecognisers(rows []*store.Recogniser) []*store.Recogniser {
	out := make([]*store.Recogniser, 0, len(rows))
	for _, r := range rows {
		if r != nil {
			out = append(out, r)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Rank != out[j].Rank {
			return out[i].Rank < out[j].Rank
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// saveHubRecogniser checks and stores one row. What atrium observed of the row (failures, last use) is kept from
// the row it replaces and never taken from the request, the same rule the room's table follows.
func saveHubRecogniser(st ForgeSettings, in store.Recogniser) (*store.Recogniser, error) {
	in, err := store.CheckRecogniser(in)
	if err != nil {
		return nil, err
	}
	recognisersMu.Lock()
	defer recognisersMu.Unlock()
	rows, err := hubRecogniserRowsLocked(st)
	if err != nil {
		return nil, err
	}
	in.LastError, in.Failures, in.LastUsedAt, in.CreatedAt = "", 0, nil, time.Now().UTC()
	saved := &in
	replaced := false
	for i, r := range rows {
		if r.ID != in.ID {
			continue
		}
		in.CreatedAt, in.LastUsedAt = r.CreatedAt, r.LastUsedAt
		// Turning one back on is the operator saying they fixed it.
		if !(in.Enabled && !r.Enabled) {
			in.LastError, in.Failures = r.LastError, r.Failures
		}
		rows[i], replaced = saved, true
	}
	if !replaced {
		rows = append(rows, saved)
	}
	if err := writeRecogniserRows(st, rows); err != nil {
		return nil, err
	}
	return saved, nil
}

// deleteHubRecogniser removes one row. A built-in row stays deleted: its id is in the seeded list.
func deleteHubRecogniser(st ForgeSettings, id string) error {
	recognisersMu.Lock()
	defer recognisersMu.Unlock()
	rows, err := hubRecogniserRowsLocked(st)
	if err != nil {
		return err
	}
	kept := rows[:0]
	for _, r := range rows {
		if r.ID != id {
			kept = append(kept, r)
		}
	}
	return writeRecogniserRows(st, kept)
}

// serveForgeRecognisers answers a room's question for the rows.
func (p *Proxy) serveForgeRecognisers(w http.ResponseWriter, f *hubForge) {
	rows, err := hubRecogniserRows(f.st)
	if err != nil {
		forgeFail(w, http.StatusInternalServerError, &forge.HubError{Code: forge.CodeOther,
			Message: "the hub's recognisers do not read: " + err.Error()})
		return
	}
	crJSON(w, http.StatusOK, HubRecognisers{Recognisers: rows})
}

// hubRecognisersRoute answers the board's recogniser routes from the hub's table, in every view, since the table is
// the hub's and no room's. False leaves the request to the rest of the proxy: a hub with no forge wired keeps the old
// per-room behaviour.
func (p *Proxy) hubRecognisersRoute(w http.ResponseWriter, r *http.Request) bool {
	const pre = "/v1/recognisers"
	if r.URL.Path != pre && !strings.HasPrefix(r.URL.Path, pre+"/") {
		return false
	}
	f := p.forgeSide()
	if f == nil {
		return false
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	id := strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, pre), "/")
	switch {
	case id == "" && (r.Method == http.MethodGet || r.Method == http.MethodHead):
		rows, err := hubRecogniserRows(f.st)
		if err != nil {
			crFail(w, http.StatusInternalServerError, err.Error())
			return true
		}
		crJSON(w, http.StatusOK, HubRecognisers{Recognisers: rows})
	case id != "" && !strings.Contains(id, "/") && r.Method == http.MethodPut:
		if docsCrossOrigin.Check(r) != nil {
			crFail(w, http.StatusForbidden, "a page on another origin cannot write here")
			return true
		}
		var rec store.Recogniser
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&rec); err != nil {
			crFail(w, http.StatusBadRequest, err.Error())
			return true
		}
		rec.ID = id
		saved, err := saveHubRecogniser(f.st, rec)
		if err != nil {
			crFail(w, http.StatusBadRequest, err.Error())
			return true
		}
		p.recognisersChanged(saved)
		crJSON(w, http.StatusOK, saved)
	case id != "" && !strings.Contains(id, "/") && r.Method == http.MethodDelete:
		if docsCrossOrigin.Check(r) != nil {
			crFail(w, http.StatusForbidden, "a page on another origin cannot write here")
			return true
		}
		if err := deleteHubRecogniser(f.st, id); err != nil {
			crFail(w, http.StatusInternalServerError, err.Error())
			return true
		}
		p.recognisersChanged(map[string]string{"removed": id})
		w.WriteHeader(http.StatusNoContent)
	default:
		crFail(w, http.StatusMethodNotAllowed, "recognisers are listed with GET, saved with PUT and removed with DELETE")
	}
	return true
}

// recognisersChanged tells every open board, as a room's save does.
func (p *Proxy) recognisersChanged(v any) {
	if data, err := json.Marshal(v); err == nil {
		p.feeds.broadcast(Event{Kind: "recognisers", Data: data})
	}
}
