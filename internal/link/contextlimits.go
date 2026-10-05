package link

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// The context limit per harness, on the hub's side. See docs/context-cycle-design.md,
// "How the hub hands the limit to rooms", and internal/api/contextsize.go for the room's.
//
// THE HUB OWNS IT. One `claude=200, codex=300` list covers every room, so a write in
// any scope is read here ahead of routing and kept in the hub's settings. In the ALL
// view the hub passes it to every attached room and answers itself. In one room's
// view the write goes on to that room as usual, and the hub passes it to the others.
// A room that attaches later is sent the hub's value, so a machine asleep when it
// was changed still cycles at the same limit. The same shape as input_lag_log.

// hubContextLimits is the name the hub stores the list under, the room's setting name.
const hubContextLimits = store.SettingContextLimits

// contextLimitsIn reads `context_limits` off a settings write and puts the body back
// for whoever reads it next. stored is the list as the rooms store it ("" for back
// to the default). Found is false for any other request; err is a bad list.
func contextLimitsIn(r *http.Request) (stored string, found bool, err error) {
	if r.URL.Path != "/v1/settings" || (r.Method != http.MethodPost && r.Method != http.MethodPut) {
		return "", false, nil
	}
	payload, rerr := io.ReadAll(io.LimitReader(r.Body, 1<<16))
	r.Body.Close()
	r.Body = io.NopCloser(bytes.NewReader(payload))
	if rerr != nil {
		return "", false, nil
	}
	var body struct {
		Limits *map[string]int `json:"context_limits"`
	}
	if json.Unmarshal(payload, &body) != nil || body.Limits == nil {
		return "", false, nil
	}
	stored, err = store.CheckContextLimits(*body.Limits)
	return stored, true, err
}

// limitsMap is a stored list as the board reads it: the default when unset.
func limitsMap(stored string) map[string]int {
	out := map[string]int{}
	if strings.TrimSpace(stored) == "" || json.Unmarshal([]byte(stored), &out) != nil || len(out) == 0 {
		return store.DefaultContextLimits()
	}
	return out
}

// noteContextLimits stores the list when a settings write names it. A bad list is
// not stored, and the room it goes to refuses it the same way.
func (p *Proxy) noteContextLimits(r *http.Request) (stored string, found bool, err error) {
	stored, found, err = contextLimitsIn(r)
	if !found || err != nil {
		return stored, found, err
	}
	if s := p.settingStore(); s != nil {
		// Stored as "default" rather than "", so a hub that was set back to the
		// default still tells a room that attaches later.
		v := stored
		if v == "" {
			v = "default"
		}
		if serr := s.SetHubSetting(hubContextLimits, v); serr != nil {
			log.Printf("[hub] could not store the context limits: %v", serr)
		}
	}
	return stored, found, nil
}

// hubContextLimitsValue is what the hub was told, and whether it was told at all.
func (p *Proxy) hubContextLimitsValue() (stored string, set bool) {
	s := p.settingStore()
	if s == nil {
		return "", false
	}
	v, err := s.HubSetting(hubContextLimits)
	if err != nil || strings.TrimSpace(v) == "" {
		return "", false
	}
	if v == "default" {
		return "", true
	}
	return v, true
}

// PushContextLimits tells one room the hub's list, for a room that just attached.
// Nothing is sent when the hub has never been told, so a room keeps its own list.
func (p *Proxy) PushContextLimits(room string) {
	stored, set := p.hubContextLimitsValue()
	if !set {
		return
	}
	if err := p.postContextLimits(context.Background(), room, stored); err != nil {
		log.Printf("[hub] could not pass the context limits to %s: %v", room, err)
	}
}

// fanContextLimits passes the list to every attached room but skip, which got the
// write itself. Best effort: a room that misses it is sent it again when it attaches.
func (p *Proxy) fanContextLimits(ctx context.Context, stored, skip string) {
	for _, room := range p.hub.Rooms() {
		if room.Name == skip {
			continue
		}
		if err := p.postContextLimits(ctx, room.Name, stored); err != nil {
			log.Printf("[hub] could not pass the context limits to %s: %v", room.Name, err)
		}
	}
}

// answerContextLimits is the ALL view's answer to a write naming the list.
func (p *Proxy) answerContextLimits(w http.ResponseWriter, r *http.Request, stored string) {
	p.fanContextLimits(r.Context(), stored, "")
	writeJSONBody(w, http.StatusOK, map[string]any{"context_limits": limitsMap(stored)})
}

// applyContextLimits writes the hub's list into a borrowed settings payload.
func (p *Proxy) applyContextLimits(body map[string]any) {
	if stored, set := p.hubContextLimitsValue(); set {
		body["context_limits"] = limitsMap(stored)
	}
}

func (p *Proxy) postContextLimits(ctx context.Context, room, stored string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	limits := map[string]int{}
	if stored != "" {
		_ = json.Unmarshal([]byte(stored), &limits)
	}
	body, _ := json.Marshal(map[string]any{"context_limits": limits})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"http://"+hostFor(room)+"/v1/settings", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := p.roomClient(room).Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return errStatus(res.StatusCode)
	}
	return nil
}
