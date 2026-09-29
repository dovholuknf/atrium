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

// The order of a pinned strip, saved from a view that holds more than one room.
//
// A card has one rank and the strip sorts by it, so the order the operator
// dropped is only kept if EVERY room writes its cards at their place in the
// WHOLE list. The hub therefore passes the same full list, untagged, to every
// attached room, whether or not a room is named: a scoped view's list only holds
// that room's cards, so the others match nothing.
//
// A room that is not answering never fails or stalls the reply. Each post is
// bounded and they run in parallel, so the reply waits for the slowest bound at
// most. The answer is 200 while at least one room took the list, with the ones
// that did not named in `unreached`, and 502 only when none did.

const pinOrderBound = 3 * time.Second

type pinUnreached struct {
	Room  string `json:"room"`
	Error string `json:"error"`
}

// pinOrderIn reports whether this is the strip's order being saved, and
// puts the body back.
func pinOrderIn(r *http.Request) (ids []string, found bool) {
	if r.URL.Path != "/v1/tasks/pin-order" || r.Method != http.MethodPost {
		return nil, false
	}
	payload, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	r.Body.Close()
	r.Body = io.NopCloser(bytes.NewReader(payload))
	if err != nil {
		return nil, false
	}
	var in struct {
		IDs []string `json:"ids"`
	}
	if json.Unmarshal(payload, &in) != nil || in.IDs == nil {
		return nil, false
	}
	return in.IDs, true
}

func (p *Proxy) fanPinOrder(w http.ResponseWriter, r *http.Request, ids []string) {
	bare := make([]string, len(ids))
	for i, id := range ids {
		_, bare[i] = splitTag(id)
	}
	payload, _ := json.Marshal(map[string][]string{"ids": bare})

	rooms := p.hub.Rooms()
	var (
		mu        sync.Mutex
		wg        sync.WaitGroup
		took      = []string{}
		unreached = []pinUnreached{}
	)
	for _, room := range rooms {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			code, body, err := p.postRoomPinOrder(r.Context(), name, payload)
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
				took = append(took, name)
			}
		}(room.Name)
	}
	wg.Wait()

	if len(took) == 0 {
		msg := "no attached room took the pinned order"
		if len(unreached) > 0 {
			msg += ": " + unreached[0].Room + ": " + unreached[0].Error
		}
		writeErrBody(w, http.StatusBadGateway, msg)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"rooms": took, "unreached": unreached})
}

func (p *Proxy) postRoomPinOrder(ctx context.Context, room string, payload []byte) (int, []byte, error) {
	ctx, cancel := context.WithTimeout(ctx, pinOrderBound)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"http://"+hostFor(room)+"/v1/tasks/pin-order", bytes.NewReader(payload))
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
