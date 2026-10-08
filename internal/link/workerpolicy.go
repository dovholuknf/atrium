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

// The default worker model and the per-worker budget, on the hub's side. The same shape as contextlimits.go: the hub
// owns it, a write in any scope is read ahead of routing and kept in the hub's settings, the hub passes it to every
// attached room, and a room that attaches later is sent it.

// hubWorkerPolicy is the name the hub stores the policy under, the room's setting name.
const hubWorkerPolicy = store.SettingWorkerPolicy

// workerPolicyOff is what the hub keeps for a policy that was set back to off, so a room that attaches later is still
// told.
const workerPolicyOff = "off"

// workerPolicyIn reads `worker_policy` off a settings write and puts the body back for whoever reads it next. stored
// is the policy as the rooms store it ("" for off). Found is false for any other request. err is a bad policy.
func workerPolicyIn(r *http.Request) (stored string, found bool, err error) {
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
		Policy *store.WorkerPolicy `json:"worker_policy"`
	}
	if json.Unmarshal(payload, &body) != nil || body.Policy == nil {
		return "", false, nil
	}
	stored, err = store.CheckWorkerPolicy(*body.Policy)
	return stored, true, err
}

// policyOf is a stored policy as the board reads it: off when unset.
func policyOf(stored string) store.WorkerPolicy {
	var out store.WorkerPolicy
	if strings.TrimSpace(stored) != "" {
		_ = json.Unmarshal([]byte(stored), &out)
	}
	return out
}

// noteWorkerPolicy stores the policy when a settings write names it. A bad policy is not stored, and the room it goes
// to refuses it the same way.
func (p *Proxy) noteWorkerPolicy(r *http.Request) (stored string, found bool, err error) {
	stored, found, err = workerPolicyIn(r)
	if !found || err != nil {
		return stored, found, err
	}
	if s := p.settingStore(); s != nil {
		v := stored
		if v == "" {
			v = workerPolicyOff
		}
		if serr := s.SetHubSetting(hubWorkerPolicy, v); serr != nil {
			log.Printf("[hub] could not store the worker policy: %v", serr)
		}
	}
	return stored, found, nil
}

// hubWorkerPolicyValue is what the hub was told, and whether it was told at all.
func (p *Proxy) hubWorkerPolicyValue() (stored string, set bool) {
	s := p.settingStore()
	if s == nil {
		return "", false
	}
	v, err := s.HubSetting(hubWorkerPolicy)
	if err != nil || strings.TrimSpace(v) == "" {
		return "", false
	}
	if v == workerPolicyOff {
		return "", true
	}
	return v, true
}

// PushWorkerPolicy tells one room the hub's policy, for a room that just attached. Nothing is sent when the hub has
// never been told, so a room keeps its own.
func (p *Proxy) PushWorkerPolicy(room string) {
	stored, set := p.hubWorkerPolicyValue()
	if !set {
		return
	}
	if err := p.postWorkerPolicy(context.Background(), room, stored); err != nil {
		log.Printf("[hub] could not pass the worker policy to %s: %v", room, err)
	}
}

// fanWorkerPolicy passes the policy to every attached room but skip, which got the write itself. Best effort: a room
// that misses it is sent it again when it attaches.
func (p *Proxy) fanWorkerPolicy(ctx context.Context, stored, skip string) {
	for _, room := range p.hub.Rooms() {
		if room.Name == skip {
			continue
		}
		if err := p.postWorkerPolicy(ctx, room.Name, stored); err != nil {
			log.Printf("[hub] could not pass the worker policy to %s: %v", room.Name, err)
		}
	}
}

// answerWorkerPolicy is the ALL view's answer to a write naming the policy.
func (p *Proxy) answerWorkerPolicy(w http.ResponseWriter, r *http.Request, stored string) {
	p.fanWorkerPolicy(r.Context(), stored, "")
	writeJSONBody(w, http.StatusOK, map[string]any{"worker_policy": policyOf(stored)})
}

// applyWorkerPolicy writes the hub's policy into a borrowed settings payload.
func (p *Proxy) applyWorkerPolicy(body map[string]any) {
	if stored, set := p.hubWorkerPolicyValue(); set {
		body["worker_policy"] = policyOf(stored)
	}
}

func (p *Proxy) postWorkerPolicy(ctx context.Context, room, stored string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	body, _ := json.Marshal(map[string]any{"worker_policy": policyOf(stored)})
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
