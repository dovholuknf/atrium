package link

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// A room too old for the merged cull.
//
// ── what a room that predates it looks like ──────────────
//
// The cull, its hold and the merged mark are routes the room grew together. A
// room built before them has none of the three, and Go's mux answers a route it
// does not have with a bare 404 and no sentence. Passed on as it is, that reads as
// "no such card", which is a lie: the card is there and the room cannot do what
// was asked. So the hub, which is the one place that knows the room's build from
// its hello, says so instead, and names the build that is needed.
//
// The mark is KEPT on the machine that sent it. Nothing is lost by the answer,
// the cull simply runs once the room is updated and the mark is sent again.

// cullSince is the commit a room has to include to answer the cull, its hold and
// the mark. It is the claude/main commit r-019 landed as, the merge of
// claude/runtime 9522700, so any build from claude/main at or after it answers.
//
// It is one constant, and not a requirements-file key, because the requirements
// file's `atrium.min` names what a REPOSITORY needs of the binary it is opened
// with. There is no floor of atrium's own that a room is held to.
const cullSince = "965ffe9"

// predatesCull is the sentence for a room that answered 404 to one of the routes.
// `build` is what the room's hello said, which is empty for a room that sent none.
func predatesCull(room, build string) string {
	if strings.TrimSpace(build) == "" {
		build = "unknown"
	}
	return fmt.Sprintf("%s build %s predates cull (needs %s). Update the room.", room, build, cullSince)
}

// isCullRoute is whether a request is one of the three the sentence is about.
// Routes only, because a 404 on `/v1/tasks/{id}` is a card that is not there.
func isCullRoute(r *http.Request) bool {
	if r == nil || r.Method != http.MethodPost {
		return false
	}
	p := r.URL.Path
	if p == "/v1/merged" {
		return true
	}
	if !strings.HasPrefix(p, "/v1/tasks/") {
		return false
	}
	rest := strings.TrimPrefix(p, "/v1/tasks/")
	i := strings.Index(rest, "/")
	if i < 0 {
		return false
	}
	return rest[i:] == "/cull" || rest[i:] == "/cull/hold"
}

// buildOf is the build a room said it was running when it attached.
func (h *Hub) buildOf(room string) string {
	if h == nil {
		return ""
	}
	for _, a := range h.Rooms() {
		if equalFold(a.Name, room) {
			return a.Version
		}
	}
	return ""
}

// rewriteOldRoom turns a room's bare 404 on a cull route into the sentence.
//
// ONLY A BARE ONE. A 404 the room wrote a sentence for is the room's own refusal
// (a card it does not hold) and is passed through untouched. The status stays
// 404, so a caller that reads codes still sees a not-found, and the sentence is
// in the body where every other refusal keeps it.
func (p *Proxy) rewriteOldRoom(res *http.Response) error {
	if res.Request == nil || res.StatusCode != http.StatusNotFound || !isCullRoute(res.Request) {
		return nil
	}
	room, _ := res.Request.Context().Value(roomKey{}).(string)
	if room == "" {
		return nil
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, 1<<16))
	res.Body.Close()
	if err != nil {
		return err
	}
	var e struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(raw, &e) == nil && strings.TrimSpace(e.Error) != "" {
		res.Body = io.NopCloser(bytes.NewReader(raw))
		return nil
	}
	out, _ := json.Marshal(map[string]string{"error": predatesCull(room, p.hub.buildOf(room))})
	res.Body = io.NopCloser(bytes.NewReader(out))
	res.ContentLength = int64(len(out))
	res.Header.Set("Content-Length", fmt.Sprint(len(out)))
	res.Header.Set("Content-Type", "application/json")
	return nil
}
