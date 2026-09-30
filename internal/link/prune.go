package link

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"time"
)

// Clearing a finished column from a view that holds more than one room (item 92).
//
// `POST /v1/tasks/prune` names no card, so it has no room of its own. From a
// view scoped to one room it goes to that room, like any other write. From the
// ALL view, which names none, it goes to EVERY attached room, because the board
// counted the column across all of them when it asked "clear N done cards?".
// Before this it was refused as a write that needs a room, or, with a stale room
// header from the board, reached one room and left the others' cards behind.
//
// UNLIKE THE PINNED ORDER, A NAMED ROOM IS NOT FANNED OUT. A pinned order only
// means something across the whole strip, so it goes everywhere. A prune deletes
// cards, and a view scoped to one room confirmed a count from that room alone.
//
// A room that is not answering never fails or stalls the reply: each post is
// bounded and they run in parallel. The answer is 200 with the total `removed`
// while at least one room took it, the rooms that did not named in `unreached`,
// and 502 only when none did.

const pruneBound = 5 * time.Second

// pruneIn reports whether this is a prune, and puts the body back.
func pruneIn(r *http.Request) ([]byte, bool) {
	if r.URL.Path != "/v1/tasks/prune" || r.Method != http.MethodPost {
		return nil, false
	}
	payload, err := io.ReadAll(io.LimitReader(r.Body, 1<<16))
	r.Body.Close()
	r.Body = io.NopCloser(bytes.NewReader(payload))
	if err != nil {
		return nil, false
	}
	return payload, true
}

func (p *Proxy) fanPrune(w http.ResponseWriter, r *http.Request, payload []byte) {
	var (
		mu        sync.Mutex
		wg        sync.WaitGroup
		removed   int
		took      = []string{}
		unreached = []pinUnreached{}
	)
	for _, room := range p.hub.Rooms() {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			code, body, err := p.postRoomPrune(r.Context(), name, payload)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err != nil:
				unreached = append(unreached, pinUnreached{name, err.Error()})
			case code/100 != 2:
				msg := string(bytes.TrimSpace(body))
				if msg == "" {
					msg = http.StatusText(code)
				}
				unreached = append(unreached, pinUnreached{name, msg})
			default:
				var out struct {
					Removed int `json:"removed"`
				}
				_ = json.Unmarshal(body, &out)
				removed += out.Removed
				took = append(took, name)
			}
		}(room.Name)
	}
	wg.Wait()

	if len(took) == 0 {
		msg := "no attached room took the prune"
		if len(unreached) > 0 {
			msg += ": " + unreached[0].Room + ": " + unreached[0].Error
		}
		writeErrBody(w, http.StatusBadGateway, msg)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"removed": removed, "rooms": took, "unreached": unreached})
}

func (p *Proxy) postRoomPrune(ctx context.Context, room string, payload []byte) (int, []byte, error) {
	ctx, cancel := context.WithTimeout(ctx, pruneBound)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"http://"+hostFor(room)+"/v1/tasks/prune", bytes.NewReader(payload))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := p.roomClient(room).Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<16))
	return res.StatusCode, body, err
}
