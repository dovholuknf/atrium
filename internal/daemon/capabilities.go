package daemon

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/dovholuknf/atrium/internal/capability"
)

// What this room can do: the declared list, probed here, read by the hub over the link. See
// docs/design/room-capabilities.md. The probes only read, and a run names ids from the declared list.

const capTick = time.Minute

const capHubFile = "capabilities.hub.yaml"

var capOnce sync.Once
var capAud *capability.Auditor

func (d *Daemon) capabilities() *capability.Auditor {
	capOnce.Do(func() {
		dir := StateDir()
		l, err := capability.Load(filepath.Join(dir, "capabilities.yaml"))
		if err != nil {
			log.Printf("[atrium] capabilities: %v. using the shipped list", err)
		}
		if b, err := os.ReadFile(filepath.Join(dir, capHubFile)); err == nil {
			if hl, err := capability.WithHub(l, b); err == nil {
				l = hl
			}
		}
		// Results are the room's own: a second room on the machine must not show the first one's.
		own := d.hostStateDir()
		capAud = capability.NewAuditor(l, filepath.Join(own, "capability-scratch"), filepath.Join(own, "capability-results.json"))
		capAud.Repos = filepath.ToSlash(filepath.Dir(filepath.Dir(dir)))
	})
	return capAud
}

// handleCapabilities is `GET /v1/capabilities`: the declared list and the newest result of each.
func (d *Daemon) handleCapabilities(w http.ResponseWriter, r *http.Request) {
	writeJSONBody(w, d.capabilities().Snapshot())
}

// handleCapabilitiesRun is `POST /v1/capabilities/run` with `{caps: [ids]}`, or none for all. It answers 202 when the
// run is started and the results are read from the snapshot as they land.
func (d *Daemon) handleCapabilitiesRun(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Caps []string `json:"caps"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&in)
	a := d.capabilities()
	snap := a.Snapshot()
	known := map[string]bool{}
	for _, c := range snap.Caps {
		known[c.ID] = true
	}
	for _, id := range in.Caps {
		if !known[id] {
			writeJSONError(w, http.StatusBadRequest, id+" is not a declared capability")
			return
		}
	}
	if len(snap.Running) > 0 {
		writeJSONError(w, http.StatusConflict, "a capability run is already going")
		return
	}
	go func() {
		if _, err := a.Run(context.Background(), in.Caps); err != nil {
			log.Printf("[atrium] capabilities run: %v", err)
		}
	}()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_, _ = w.Write([]byte(`{"accepted":true}`))
}

// capabilityLoop probes what is due, one capability at a time and never beside a run someone asked for.
func (d *Daemon) capabilityLoop(ctx context.Context) {
	t := time.NewTicker(capTick)
	defer t.Stop()
	for {
		a := d.capabilities()
		d.capabilityHubList(ctx, a)
		if ids := a.Due(); len(ids) > 0 {
			_, _ = a.Run(ctx, ids)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// capabilityHubList keeps a copy of the hub's list on disk so a restart with no hub still has it.
func (d *Daemon) capabilityHubList(ctx context.Context, a *capability.Auditor) {
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	code, data, _ := d.HubCall(cctx, http.MethodGet, "/_hub/capability-list", nil)
	if code != http.StatusOK {
		return
	}
	var got struct {
		YAML    string `json:"yaml"`
		Version int    `json:"version"`
	}
	if json.Unmarshal(data, &got) != nil || got.Version == 0 || got.Version == a.Snapshot().Version {
		return
	}
	dir := StateDir()
	local, err := capability.Load(filepath.Join(dir, "capabilities.yaml"))
	if err != nil {
		log.Printf("[atrium] capabilities: %v", err)
	}
	l, err := capability.WithHub(local, []byte(got.YAML))
	if err != nil {
		log.Printf("[atrium] capabilities: %v. keeping the list in use", err)
		return
	}
	a.SetList(l)
	if err := os.WriteFile(filepath.Join(dir, capHubFile), []byte(got.YAML), 0o600); err != nil {
		log.Printf("[atrium] capabilities: the hub list was not kept: %v", err)
	}
}
