package link

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// The scoped board's list of everywhere cards. See docs/rnd/everywhere-card-design.md.
//
// A HUB ENDPOINT AND NOT A CHANGE TO THE PIPE. A scoped `/v1/tasks` stays the
// room's own answer, byte for byte, and a board that wants the cards tagged
// atrium:everywhere on other rooms asks here beside it.

// everywhereWait bounds one card's fetch, and everywhereCap the number in
// flight, since the list is one per card and usually one in all.
const (
	everywhereWait = 10 * time.Second
	everywhereCap  = 8
)

// serveEverywhere is `GET /_hub/everywhere?room=<scope>`: every indexed card not
// on `<scope>`, as `{"tasks": [...]}`.
func (p *Proxy) serveEverywhere(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		_, _ = io.WriteString(w, `{"error":"that has to be a GET"}`)
		return
	}
	cards := p.hub.every.all(r.URL.Query().Get("room"))
	rows := make([]map[string]any, len(cards))
	sem := make(chan struct{}, everywhereCap)
	var wg sync.WaitGroup
	for i, c := range cards {
		wg.Add(1)
		go func(i int, c everyCard) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			rows[i] = p.everywhereRow(r.Context(), c)
		}(i, c)
	}
	wg.Wait()
	out := []map[string]any{}
	for _, row := range rows {
		if row != nil {
			out = append(out, row)
		}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"tasks": out})
}

// everywhereRow is one card as a scoped board draws it: live from its room when
// the room is attached, from the cache with `offline` when it is not. Nil for a
// card its room no longer has.
func (p *Proxy) everywhereRow(ctx context.Context, c everyCard) map[string]any {
	var obj map[string]any
	offline := !p.hub.Has(c.Room)
	if !offline {
		var gone bool
		if obj, gone = p.fetchCard(ctx, c); gone {
			return nil
		}
		// A room that would not answer is drawn from the cache, which says so.
		offline = obj == nil
	}
	if obj == nil {
		if err := json.Unmarshal(c.Payload, &obj); err != nil || obj == nil {
			return nil
		}
		// What a room would have computed on the way out. See `remembered`.
		derive(obj)
	}
	obj["room"] = c.Room
	obj["id"] = tagFor(c.Room, c.ID)
	obj["everywhere"] = true
	if offline {
		obj["offline"] = true
	}
	return obj
}

// fetchCard reads one card from its room. `gone` is the room saying it has no
// such card, and a nil map with gone false is a room that did not answer.
func (p *Proxy) fetchCard(ctx context.Context, c everyCard) (obj map[string]any, gone bool) {
	ctx, cancel := context.WithTimeout(ctx, everywhereWait)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"http://"+hostFor(c.Room)+"/v1/tasks/"+url.PathEscape(c.ID), nil)
	if err != nil {
		return nil, false
	}
	res, err := p.roomClient(c.Room).Do(req)
	if err != nil {
		return nil, false
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return nil, true
	}
	if res.StatusCode != http.StatusOK {
		return nil, false
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(&obj); err != nil {
		return nil, false
	}
	return obj, false
}
