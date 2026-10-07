package link

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/dovholuknf/atrium/internal/store"
)

// OPEN A LINK, ON THE HUB. Design: docs/rnd/card-lifecycle-design.md, section 3. Item r-open-verb-pr.
//
// `POST /v1/open {url}` is one path on a hub and on a room, so every door (the launch dialog, ctrl-alt-r, the pulls
// tab, `atrium open`) calls the same thing wherever it is pointed. The room does the work (internal/api/open.go). The
// hub only decides which room:
//
//   - a room the caller named, by header or query, is where it goes.
//   - otherwise the link is matched against the hub's own recogniser table, and a pull request some room already
//     claimed goes to that room, which answers its live card rather than making a second one.
//   - otherwise the least busy room, as a paste on the pulls tab is placed. That room claims the key for itself.
//
// The answer comes back with `room` set and `card` and `pr` tagged, so the board can attach the card wherever it is.

// openRoomKey carries the room an open went to, for retagOpen.
type openRoomKey struct{}

// placeOpen is the hub's placement of `POST /v1/open`.
func (p *Proxy) placeOpen(w http.ResponseWriter, r *http.Request) (*http.Request, bool) {
	if r.Method != http.MethodPost || r.URL.Path != "/v1/open" {
		return r, true
	}
	if room, named := p.roomFor(r); named || room != "" {
		return r.WithContext(context.WithValue(r.Context(), openRoomKey{}, room)), true
	}
	room := ""
	if key := p.openKey(r); key != "" {
		if st := p.prClaims(); st != nil {
			if c, err := st.PRClaimOf(key); err == nil {
				room = c.Room
			}
		}
	}
	if room == "" {
		room = p.placePRRoom(r.Context(), "")
	}
	if room == "" {
		return r, true
	}
	r = p.placedOn(w, r, room)
	return r.WithContext(context.WithValue(r.Context(), openRoomKey{}, room)), true
}

// openKey is the claim key of a pasted pull request, read from the hub's recogniser table, or "" when the hub has no
// table, nothing matches, or the match is not a pull request. The body is put back for the room.
func (p *Proxy) openKey(r *http.Request) string {
	if r.Body == nil {
		return ""
	}
	raw, _ := io.ReadAll(io.LimitReader(r.Body, 64<<10))
	r.Body = io.NopCloser(io.MultiReader(bytes.NewReader(raw), r.Body))
	var in struct {
		URL string `json:"url"`
	}
	if json.Unmarshal(raw, &in) != nil || strings.TrimSpace(in.URL) == "" {
		return ""
	}
	f := p.forgeSide()
	if f == nil {
		return ""
	}
	rows, err := hubRecogniserRows(f.st)
	if err != nil {
		return ""
	}
	_, vars, err := store.MatchRecogniserIn(rows, in.URL)
	if err != nil {
		return ""
	}
	num, _ := strconv.Atoi(strings.TrimSpace(vars["num"]))
	if vars["host"] == "" || vars["org"] == "" || vars["repo"] == "" || num <= 0 {
		return ""
	}
	return store.PRKey(vars["host"], vars["org"], vars["repo"], num)
}

// retagOpen puts the room on an open's answer, and on its card and review ids, in the merged view. A scoped board
// asked one room and wants that room's own ids.
func (p *Proxy) retagOpen(res *http.Response) error {
	if res.Request == nil || res.StatusCode >= 300 {
		return nil
	}
	room, _ := res.Request.Context().Value(openRoomKey{}).(string)
	if room == "" {
		return nil
	}
	if _, merged := p.merged(); !merged {
		return nil
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	res.Body.Close()
	if err != nil {
		return err
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		res.Body = io.NopCloser(bytes.NewReader(raw))
		return nil
	}
	obj["room"] = room
	for _, field := range []string{"card", "pr"} {
		if id, ok := obj[field].(string); ok && id != "" {
			obj[field] = tagFor(room, id)
		}
	}
	out, err := json.Marshal(obj)
	if err != nil {
		res.Body = io.NopCloser(bytes.NewReader(raw))
		return nil
	}
	res.Body = io.NopCloser(bytes.NewReader(out))
	res.ContentLength = int64(len(out))
	res.Header.Set("Content-Length", fmt.Sprint(len(out)))
	return nil
}
