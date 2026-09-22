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

	"github.com/dovholuknf/atrium/internal/inputlag"
)

// Input-lag logging as a gear setting, on the hub's side. See internal/inputlag
// for the switch and internal/api/inputlag.go for the room's side.
//
// ONE CHECKBOX, EVERY HOP. The board posts `input_lag_log` to `/v1/settings`.
// The hub reads it off every such write that passes through it, whatever the
// scope, and switches its own logging. In the ALL view there is no room for the
// write to land in, so the hub passes it to every attached room itself. A room
// that attaches later is told the hub's answer when it arrives, so the switch
// covers a machine that was asleep when it was pressed.
//
// The hub keeps the value in its own settings table, so a hub restart comes up
// timing if it was timing. The variable still wins on either side.

// hubInputLag is the name the hub stores the switch under.
const hubInputLag = "input_lag_log"

// hubSettingStore is the part of the hub's store this needs. Asked for by type
// rather than added to Inventory, so every fake inventory in the tests does not
// have to grow two methods it never uses.
type hubSettingStore interface {
	HubSetting(name string) (string, error)
	SetHubSetting(name, value string) error
}

func (p *Proxy) settingStore() hubSettingStore {
	s, _ := p.inventory().(hubSettingStore)
	return s
}

// inputLagIn reads `input_lag_log` off a settings write and puts the body back
// for whoever reads it next. Found is false for any other request.
func inputLagIn(r *http.Request) (on, found bool) {
	if r.URL.Path != "/v1/settings" || (r.Method != http.MethodPost && r.Method != http.MethodPut) {
		return false, false
	}
	payload, err := io.ReadAll(io.LimitReader(r.Body, 1<<16))
	r.Body.Close()
	r.Body = io.NopCloser(bytes.NewReader(payload))
	if err != nil {
		return false, false
	}
	var body struct {
		InputLag *bool `json:"input_lag_log"`
	}
	if json.Unmarshal(payload, &body) != nil || body.InputLag == nil {
		return false, false
	}
	return *body.InputLag, true
}

// noteInputLag switches the hub's own logging when a settings write names it,
// and stores it. Called ahead of routing, so it sees the write in every scope.
func (p *Proxy) noteInputLag(r *http.Request) (on, found bool) {
	on, found = inputLagIn(r)
	if !found {
		return on, found
	}
	inputlag.SetLive(on)
	if s := p.settingStore(); s != nil {
		v := "off"
		if on {
			v = "on"
		}
		if err := s.SetHubSetting(hubInputLag, v); err != nil {
			log.Printf("[hub] could not store the input-lag switch: %v", err)
		}
	}
	return on, found
}

// ApplyInputLag switches the hub's logging to what it stored, for a hub coming
// up. A value never written leaves the start-time default alone.
func (p *Proxy) ApplyInputLag() {
	s := p.settingStore()
	if s == nil {
		return
	}
	if v, err := s.HubSetting(hubInputLag); err == nil && strings.TrimSpace(v) != "" {
		inputlag.SetLive(v == "on")
	}
}

// PushInputLag tells one room the hub's switch, for a room that just attached.
// Nothing is sent when the hub has never been told, so a room keeps its own
// answer until somebody presses the checkbox. Best effort: a room that does not
// take it logs as it did before, which is the same state a missed click leaves.
func (p *Proxy) PushInputLag(room string) {
	s := p.settingStore()
	if s == nil {
		return
	}
	v, err := s.HubSetting(hubInputLag)
	if err != nil || strings.TrimSpace(v) == "" {
		return
	}
	if err := p.postInputLag(context.Background(), room, v == "on"); err != nil {
		log.Printf("[hub] could not pass the input-lag switch to %s: %v", room, err)
	}
}

// fanInputLag passes the switch to every attached room, for a write made in the
// ALL view. Answers with the hub's own state.
func (p *Proxy) fanInputLag(w http.ResponseWriter, r *http.Request, on bool) {
	for _, room := range p.hub.Rooms() {
		if err := p.postInputLag(r.Context(), room.Name, on); err != nil {
			log.Printf("[hub] could not pass the input-lag switch to %s: %v", room.Name, err)
		}
	}
	writeJSONBody(w, http.StatusOK, map[string]any{
		"input_lag_log":    inputlag.On(),
		"input_lag_pinned": inputlag.Pinned(),
	})
}

func (p *Proxy) postInputLag(ctx context.Context, room string, on bool) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	body, _ := json.Marshal(map[string]bool{"input_lag_log": on})
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
