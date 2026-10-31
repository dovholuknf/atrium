package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dovholuknf/atrium/internal/daemon/shared"
	"github.com/dovholuknf/atrium/internal/small"
	"github.com/dovholuknf/atrium/internal/store"
)

// The room deploy hold. See docs/rnd/room-deploy-hold-design.md.
//
// A deploy owner sets it, every other card here has its next gated call refused
// with "end your turn and wait", and messages between agents are held. The room
// restarts on a new build and lifts its own hold at startup. Every lift, at startup
// or called off, runs out or lifted on the board, releases the cards SILENTLY:
// nothing is typed or queued into a card, since a message wakes an idle card onto a
// cold prompt cache.
//
// REFUSE, DO NOT PARK. A hook parked across the room's own restart fails open
// when the daemon goes, and runs the call it was holding. A refusal the model
// reads as "end your turn" leaves every agent idle with nothing in flight, the
// one state that survives a restart. Section 2 of the design.
//
// THE STORE IS THE TRUTH. This file keeps a copy for the permission path, written
// only after the store.

const (
	deployHoldMaxDefault = 60 * time.Minute
	// holdTickEvery is how often an expired hold is looked for.
	holdTickEvery = 30 * time.Second
	// maxHoldLine bounds the refusal, reasons cut to fit.
	maxHoldLine = 600
)

// DecidedByDeployHold is what a refusal is recorded as decided by.
const DecidedByDeployHold = "deploy-hold"

// holdState is the copy of the room's holds the hot path reads.
type holdState struct {
	mu    sync.Mutex
	holds []store.RoomHold
}

func newHoldState() *holdState { return &holdState{} }

func (hs *holdState) set(holds []store.RoomHold) {
	hs.mu.Lock()
	defer hs.mu.Unlock()
	hs.holds = holds
}

func (hs *holdState) kind(kind string) *store.RoomHold {
	hs.mu.Lock()
	defer hs.mu.Unlock()
	for i := range hs.holds {
		if hs.holds[i].Kind == kind {
			h := hs.holds[i]
			return &h
		}
	}
	return nil
}

// loadHolds fills the copy at startup. A hold that cannot be read is logged and
// treated as none: the permission path must never halt on it.
func (d *Daemon) loadHolds() {
	holds, err := d.st.RoomHolds()
	if err != nil {
		log.Printf("[atrium] could not read the room hold, treating it as none: %v", err)
		return
	}
	d.holds.set(holds)
}

// deployHold is the room's deploy hold, or nil.
func (d *Daemon) deployHold() *store.RoomHold { return d.holds.kind(store.HoldDeploy) }

// deployHeld reports whether a card is held for a deploy: its gated calls are
// refused and its agents' messages wait.
func (d *Daemon) deployHeld(taskID string) bool {
	return d.deployHold().Holds(taskID)
}

// holdingFrom is the question a delivery path asks about one message: a
// new-context cycle holds every message, and a deploy hold holds an agent's but
// never the operator's, since the operator reaching out is what must still work.
func (d *Daemon) holdingFrom(taskID, from string) bool {
	if from != "" && d.holdingMessages(taskID) {
		return true
	}
	if from == "" && d.frozenForMove(taskID) {
		return true
	}
	return from != "" && d.deployHeld(taskID)
}

// deployHoldNote is what a sender is told while its target is held.
func (d *Daemon) deployHoldNote() string {
	return "held: " + d.roomWord() + " is being redeployed. it is delivered when the hold lifts."
}

func (d *Daemon) roomWord() string {
	if r := strings.TrimSpace(d.opts.Room); r != "" {
		return r
	}
	return "this room"
}

// deployRefusal is what a held card's gated call is answered with.
func (d *Daemon) deployRefusal(h *store.RoomHold) string {
	head := "[atrium] room deploy: " + d.roomWord() + " is being redeployed by " + orWord(h.By, "the deploy owner")
	tail := ". this call did not run. end your turn, do not poll, and run it again later."
	return fitReasons(head, h.Whys, tail)
}

// fitReasons joins head, the reasons and tail, cutting the reasons so the whole
// is at most maxHoldLine bytes.
func fitReasons(head string, whys []string, tail string) string {
	why := strings.Join(whys, "; ")
	room := maxHoldLine - len(head) - len(tail) - len(" (for: )")
	if why == "" || room <= 3 {
		return head + tail
	}
	if len(why) > room {
		why = why[:room-3] + "..."
	}
	return head + " (for: " + why + ")" + tail
}

// Outcomes a lift can have, recorded on each held card's timeline.
const (
	HoldOutcomeDeployed  = "deployed"
	HoldOutcomeSameBuild = "same-build"
	HoldOutcomeCancelled = "cancelled"
	HoldOutcomeExpired   = "expired"
	HoldOutcomeOperator  = "operator"
)

// ── the build ───────────────────────────────────────────────────────────────

var (
	buildOnce sync.Once
	buildID   string
)

// buildIdentity names the binary this daemon is running, so a room that comes
// back can say whether the deploy took. A hash of the executable, because a
// build from a working tree carries the same version and commit as the one it
// replaced. Once per process: the binary does not change under it.
func buildIdentity() string {
	buildOnce.Do(func() { buildID = hashExecutable() })
	return buildID
}

func hashExecutable() string {
	exe, err := os.Executable()
	if err != nil {
		return "unknown"
	}
	f, err := os.Open(exe)
	if err != nil {
		return "unknown"
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "unknown"
	}
	return hex.EncodeToString(h.Sum(nil))[:12]
}

// ── setting and lifting ─────────────────────────────────────────────────────

// holdStart is what `POST /v1/hold` takes to set a deploy hold.
type holdStart struct {
	By     string   `json:"by"`
	ByCard string   `json:"by_card"`
	Whys   []string `json:"whys"`
	Exempt []string `json:"exempt"`
}

// errHoldExempt is more exempt cards than a hold allows.
var errHoldExempt = small.ErrString("at most three exempt cards")

// startDeployHold sets the hold on every card here with a live session, but the
// deployer's and the exempt ones.
func (d *Daemon) startDeployHold(in holdStart) (*store.RoomHold, error) {
	if len(in.Exempt) > 3 {
		return nil, errHoldExempt
	}
	h := store.RoomHold{
		Kind: store.HoldDeploy, ID: newHoldID(), By: strings.TrimSpace(in.By),
		FromBuild: d.build, StartedAt: time.Now().UTC(),
	}
	h.ExpiresAt = h.StartedAt.Add(d.deployHoldMax())
	if t := d.cardNamed(in.ByCard); t != nil {
		h.ByCard = t.ID
	}
	for _, e := range in.Exempt {
		if t := d.cardNamed(e); t != nil {
			h.Exempt = append(h.Exempt, t.ID)
		}
	}
	for _, w := range in.Whys {
		if w = strings.TrimSpace(w); w != "" {
			h.Whys = append(h.Whys, w)
		}
	}
	h.Cards = d.holdableCards()
	if err := d.st.SetRoomHold(h); err != nil {
		return nil, err
	}
	d.loadHolds()
	log.Printf("[atrium] deploy hold %s set by %s on %d card(s)", h.ID, orWord(h.By, "an unnamed caller"), len(h.Cards))
	d.holdChanged(&h)
	return &h, nil
}

// holdableCards is every card with a session that could make a gated call: the
// ones this room supervises, and any other that is open.
func (d *Daemon) holdableCards() []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range d.sup.All() {
		if !seen[r.TaskID()] {
			seen[r.TaskID()] = true
			out = append(out, r.TaskID())
		}
	}
	if open, err := d.st.List(store.StatusRunning, store.StatusNeedsInput, store.StatusNeedsPermission); err == nil {
		for _, t := range open {
			if !seen[t.ID] && !small.IsParked(t) {
				seen[t.ID] = true
				out = append(out, t.ID)
			}
		}
	}
	sort.Strings(out)
	return out
}

func newHoldID() string {
	return "hold-" + strconv.FormatInt(time.Now().UnixNano(), 36)
}

func (d *Daemon) deployHoldMax() time.Duration {
	v, err := d.st.Setting(store.SettingDeployHoldMax)
	if err != nil {
		return deployHoldMaxDefault
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n <= 0 {
		return deployHoldMaxDefault
	}
	return time.Duration(n) * time.Minute
}

// cardNamed finds a card by id, wire name or alias, or nil.
func (d *Daemon) cardNamed(name string) *store.Task {
	name = strings.TrimPrefix(strings.TrimSpace(name), "@")
	if name == "" {
		return nil
	}
	if t, err := d.st.Get(name); err == nil {
		return t
	}
	if t, err := d.st.GetByWireName(d.st.Qualify(name)); err == nil {
		return t
	}
	if t, err := d.st.GetByAlias(name); err == nil {
		return t
	}
	return nil
}

// liftDeployHold ends the hold with no restart. Nothing is sent to a held card.
// Answers nil when there was no hold to lift.
func (d *Daemon) liftDeployHold(outcome string) (*store.RoomHold, error) {
	h, err := d.st.LiftRoomHold(store.HoldDeploy, "", outcome)
	if err != nil || h == nil {
		return h, err
	}
	d.loadHolds()
	for _, id := range h.Cards {
		d.releaseHeld(id)
	}
	log.Printf("[atrium] deploy hold %s lifted (%s)", h.ID, outcome)
	d.holdChanged(nil)
	return h, nil
}

// liftAtStartup lifts a deploy hold this room was restarted under, silently.
func (d *Daemon) liftAtStartup() {
	h := d.deployHold()
	if h == nil {
		return
	}
	outcome := HoldOutcomeDeployed
	if h.FromBuild == d.build {
		outcome = HoldOutcomeSameBuild
	}
	lifted, err := d.st.LiftRoomHold(store.HoldDeploy, h.ID, outcome)
	if err != nil || lifted == nil {
		if err != nil {
			log.Printf("[atrium] could not lift the deploy hold at startup: %v", err)
		}
		return
	}
	d.loadHolds()
	for _, id := range lifted.Cards {
		d.releaseHeld(id)
	}
	log.Printf("[atrium] deploy hold %s lifted at startup (%s)", h.ID, outcome)
}

// launchHeld refuses a launch onto a held room, which would start a runner the
// restart ends a minute later. The deployer and its exempt cards may still launch.
func (d *Daemon) launchHeld(req shared.LaunchRequest) error {
	h := d.deployHold()
	if h == nil {
		return nil
	}
	by := strings.TrimSpace(req.SpawnedBy)
	if by != "" && by == h.By {
		return nil
	}
	if t := d.cardNamed(by); t != nil && !h.Holds(t.ID) && (t.ID == h.ByCard || containsID(h.Exempt, t.ID)) {
		return nil
	}
	return small.ErrString(d.roomWord() + " is being redeployed: launch when the hold lifts")
}

func containsID(ids []string, id string) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

// holdLoop lifts a hold that has run out.
func (d *Daemon) holdLoop(ctx context.Context) {
	t := time.NewTicker(holdTickEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			d.holdTick(now)
		}
	}
}

func (d *Daemon) holdTick(now time.Time) {
	h := d.deployHold()
	if h == nil || h.ExpiresAt.IsZero() || now.Before(h.ExpiresAt) {
		return
	}
	if _, err := d.liftDeployHold(HoldOutcomeExpired); err != nil {
		log.Printf("[atrium] could not lift an expired deploy hold: %v", err)
	}
}

// holdChanged tells the board. `h` is the hold now on, or nil.
func (d *Daemon) holdChanged(h *store.RoomHold) {
	d.ap.Broadcast("hold", map[string]any{"kind": store.HoldDeploy, "hold": h})
	var ids []string
	if h != nil {
		ids = h.Cards
	} else {
		ids = d.holdableCards()
	}
	for _, id := range ids {
		d.Publish(id)
	}
}

// heldFor is a card's hold as the board draws it, or nil.
func (d *Daemon) heldFor(taskID string) any {
	h := d.deployHold()
	if !h.Holds(taskID) {
		return nil
	}
	return map[string]any{"kind": h.Kind, "by": h.By, "since": h.StartedAt}
}

// ── who is busy ─────────────────────────────────────────────────────────────

// BusyIdleAfter is how long without activity means a session is not working.
const BusyIdleAfter = 120 * time.Second

// BusyCard is the room's one rule for "working right now", shared by `atrium room
// restart` and the deploy hold's wait. A supervised session in a column that
// means it is working, with fresh activity, that is not just thinking.
//
// Stale activity is not activity: it is written when a tool starts and nothing
// writes when a turn ends. A session thinking has nothing half written.
func BusyCard(supervised bool, status string, idle time.Duration, activity string) bool {
	if !supervised {
		return false
	}
	switch status {
	case store.StatusNeedsInput, store.StatusNeedsPermission, store.StatusDone, store.StatusDead, store.StatusPaused:
		return false
	}
	return idle <= BusyIdleAfter && activity != ActivityThinking
}

// heldBusy is every held card that is still working, card id to title.
func (d *Daemon) heldBusy(h *store.RoomHold) map[string]string {
	out := map[string]string{}
	if h == nil {
		return out
	}
	for _, id := range h.Cards {
		if !h.Holds(id) {
			continue
		}
		t, err := d.st.Get(id)
		if err != nil {
			continue
		}
		what := ""
		if a := d.act.get(id); a != nil {
			what = a.What
		}
		if BusyCard(d.sup.Get(id) != nil, t.Status, time.Since(t.LastActivityAt), what) {
			out[id] = t.DisplayTitle()
		}
	}
	return out
}

// ── the endpoint ────────────────────────────────────────────────────────────

// handleHold is `/v1/hold`. GET answers the hold and who is busy. POST takes
// `{"action": "start"|"lift", ...}`. On the human listener, reached by the hub
// through its proxy.
func (d *Daemon) handleHold(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch r.Method {
	case http.MethodGet:
		_ = json.NewEncoder(w).Encode(d.holdView())
	case http.MethodPost:
		var in struct {
			Action string `json:"action"`
			holdStart
			// Outcome is `cancelled` from the deployer or `operator` from the board.
			Outcome string `json:"outcome"`
			Why     string `json:"why"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&in); err != nil {
			small.WriteJSONErr(w, http.StatusBadRequest, err)
			return
		}
		switch in.Action {
		case "start":
			_, err := d.startDeployHold(in.holdStart)
			switch {
			case errors.Is(err, store.ErrHoldExists):
				small.WriteJSONErr(w, http.StatusConflict, small.ErrString("this room is already held for a deploy"))
				return
			case errors.Is(err, errHoldExempt):
				small.WriteJSONErr(w, http.StatusBadRequest, err)
				return
			case err != nil:
				small.WriteJSONErr(w, http.StatusInternalServerError, err)
				return
			}
		case "lift":
			outcome := HoldOutcomeOperator
			if in.Outcome == HoldOutcomeCancelled {
				outcome = HoldOutcomeCancelled
			}
			h, err := d.liftDeployHold(outcome)
			if err != nil {
				small.WriteJSONErr(w, http.StatusInternalServerError, err)
				return
			}
			if h == nil {
				small.WriteJSONErr(w, http.StatusNotFound, small.ErrString("this room is not held for a deploy"))
				return
			}
		default:
			small.WriteJSONErr(w, http.StatusBadRequest, small.ErrString(`action is "start" or "lift"`))
			return
		}
		_ = json.NewEncoder(w).Encode(d.holdView())
	default:
		small.WriteJSONErr(w, http.StatusMethodNotAllowed, small.ErrString("GET or POST"))
	}
}

// holdView is the hold, the cards still busy under it, and the cards it could
// not hold because atrium does not gate them.
func (d *Daemon) holdView() map[string]any {
	h := d.deployHold()
	out := map[string]any{"room": d.roomWord(), "build": d.build, "hold": h, "busy": []map[string]string{}}
	if h == nil {
		return out
	}
	busy := d.heldBusy(h)
	list := make([]map[string]string, 0, len(busy))
	for id, title := range busy {
		list = append(list, map[string]string{"card": id, "title": title})
	}
	sort.Slice(list, func(i, j int) bool { return list[i]["title"] < list[j]["title"] })
	out["busy"] = list
	out["quiet"] = len(list) == 0
	var ungated []string
	for _, id := range h.Cards {
		if t, err := d.st.Get(id); err == nil && h.Holds(id) && t.ToolHookSeenAt == nil {
			ungated = append(ungated, t.DisplayTitle())
		}
	}
	out["ungated"] = ungated
	return out
}
