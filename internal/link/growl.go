package link

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dovholuknf/atrium/internal/edge"
)

// The persistent growler: something waiting on a human, shown on every screen
// until it is handled, dismissed or its reason ends. See
// docs/rnd/persistent-growler-design.md, which this is stage R1 of.
//
// ── what earns one ──────────────────────────────────────
//
// The reasons the notify trigger already works out, by the same function
// (cardReason, which is NotifyIdentity without the origin:agent skip):
//
//   - `permission`, once a request has waited growl.perm_after (two minutes).
//     An origin:agent card growls here too, because a blocked agent is frozen
//     whoever launched it.
//   - `question`, at once, for a turn that ended on Open Questions or an
//     atrium_report with status `question`. Not for an origin:agent card.
//   - `blocked`, at once, for an atrium_report with status `blocked`. Not for an
//     origin:agent card either: its launcher is told.
//   - `halt`, from a room's own /v1/health, asked on the ticker.
//   - `deploy-hold`, from a room's own /v1/hold, once a deploy hold has been
//     on for growl.hold_after (fifteen minutes). A deploy restarts its room in
//     a minute or two, so a hold still there is a deploy that stalled, and
//     every card it holds is frozen until somebody lifts it.
//
// `input` without a report and `finished` never growl.
//
// ── who writes, and when ────────────────────────────────
//
// After every announcement a room makes, and on a 30 second ticker. The ticker
// is needed because announcements happen on change: a permission that waited
// 1 min 59 s and then nothing moved would never be promoted. It also asks each
// attached room's health, ends snoozes, and counts reminders.
//
// ── what a screen hears ─────────────────────────────────
//
// The `growls` event carries THE WHOLE LIVE SET, not a delta, for the reason
// `rooms` does (events.go): it is small, bounded by what is blocked rather than
// by cards, and a screen that missed one heals on the next. It goes out when
// the set changes, on a reminder, and once to every stream as it opens.
//
// ── what it must not break ──────────────────────────────
//
// A growler is never in the path of the thing it is about. Approve and block
// go to the room the way the perms view does, and the growler resolves because
// the request left. A store failure here is the hub's halt like any other, and
// the permission is still answerable from the board.

// Growl settings, in the hub_setting table.
const (
	settingGrowlPermAfter = "growl.perm_after"
	settingGrowlHoldAfter = "growl.hold_after"
	// settingGrowlSince is when growlers began on this hub. A question asked
	// before it is history, not news, and raises nothing.
	settingGrowlSince = "growl.since"
	// settingGrowlLadder is which reasons re-raise on growlBackoff, a JSON
	// object of reason to bool, e.g. {"permission":true,"question":false}. A
	// reason it does not name takes growlLadderDefault. See ladder.
	settingGrowlLadder = "growl_ladder"
)

// growlLadderDefault is the ladder when nothing is set: what blocks something
// keeps reminding, a permission, a hub halt and a stalled deploy hold, and a
// question or a block rings once (clint, 2026-10-02: no growler reminders for
// questions). A reason ringing once is still raised, kept in the bell and
// phoned once, and a human can snooze it for a reminder of their own.
var growlLadderDefault = map[string]bool{ReasonPermission: true, "halt": true, "deploy-hold": true}

// growlLadderReasons is every reason the setting may name.
var growlLadderReasons = []string{ReasonPermission, ReasonQuestion, reasonBlocked, "halt", "deploy-hold"}

const (
	growlTick         = 30 * time.Second
	growlPermAfter    = 2 * time.Minute
	growlHoldAfter    = 15 * time.Minute
	growlPruneEvery   = time.Hour
	growlBodyMax      = 200
	growlFillTries    = 3
	growlSnoozeMaxMin = 7 * 24 * 60
)

// growlBackoff is when a growler reminds, measured from its raise. The
// operator's backoff (EscalationBackoff in internal/daemon), stopping at the
// two hour step: past it the growler stays on screen and stops ringing.
var growlBackoff = []time.Duration{
	time.Minute, 2 * time.Minute, 5 * time.Minute, 10 * time.Minute,
	30 * time.Minute, time.Hour, 2 * time.Hour,
}

// growlUrgency orders the stack, most urgent first.
var growlUrgency = map[string]int{
	ReasonPermission: 1, "halt": 2, reasonBlocked: 3, ReasonQuestion: 4, "deploy-hold": 5,
}

// The card reasons R1 derives. Passed to the store, which touches no other
// reason when it syncs a room's cards.
var growlCardReasons = []string{ReasonPermission, ReasonQuestion, reasonBlocked}

// reasonBlocked is a growler reason and never a notify one.
const reasonBlocked = "blocked"

// ErrGrowlNotFound and ErrGrowlStale are what a GrowlStore answers an action
// with. Stale carries the row as it is now.
var (
	ErrGrowlNotFound = errors.New("no growler has that id")
	ErrGrowlStale    = errors.New("that growler is already handled")
)

// GrowlRow is one growler, as the endpoint and the event carry it.
type GrowlRow struct {
	ID     string `json:"id"`
	Room   string `json:"room"`
	CardID string `json:"card_id"`
	// Card is the tagged id, room~id. CardID is the room's own.
	Card      string    `json:"card"`
	Reason    string    `json:"reason"`
	Urgency   int       `json:"urgency"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	Subject   string    `json:"subject"`
	RaisedAt  time.Time `json:"raised_at"`
	State     string    `json:"state"`
	Until     string    `json:"until"`
	Reminders int       `json:"reminders"`
	ChangedAt time.Time `json:"changed_at"`
	// ChangedVia is board, phone or notification for a human, hub for the hub.
	ChangedVia string `json:"changed_via"`
	ChangedTab string `json:"changed_tab"`
	// RoomOffline is worked out when read: the room is not attached. Its
	// growlers stay open, because a network blinking is not a reason ending.
	RoomOffline bool `json:"room_offline"`
}

// GrowlStore is what the growler needs from the hub's database, by room NAME.
type GrowlStore interface {
	// Sync applies one room's card growlers for `reasons`. See
	// hubstore.Store.GrowlSync.
	Sync(room string, reasons []string, want []GrowlRow, present map[string]bool) (raised []string, changed bool, err error)
	// Room raises or ends a room's halt or deploy hold. See
	// hubstore.Store.GrowlRoom.
	Room(room, reason string, on bool, g GrowlRow) (raised, changed bool, err error)
	Fill(id, subject, body string) (bool, error)
	Live() ([]GrowlRow, error)
	// Act answers ErrGrowlNotFound, or ErrGrowlStale with the row.
	Act(id, state string, until time.Time, via, tab string) (GrowlRow, error)
	Wake() ([]string, error)
	Reminded(id string, n int) error
	Prune() error
	Setting(name string) (string, error)
	SetSetting(name, value string) error
	Rooms() ([]string, error)
	Cards(room string) ([]CardState, error)
}

// roomHealth is one room's answer to /v1/health. ok is false for a room that
// did not answer, which says nothing either way.
type roomHealth struct {
	ok     bool
	halted bool
	cause  string
}

// roomHold is one room's answer to /v1/hold. ok is false for a room that did
// not answer.
type roomHold struct {
	ok bool
	// on is false for a room with no deploy hold.
	on        bool
	id, by    string
	whys      []string
	startedAt time.Time
}

// pendingPerm is one request a room is holding, as its /v1/permissions says.
type pendingPerm struct {
	ID          string    `json:"id"`
	TaskID      string    `json:"task_id"`
	Tool        string    `json:"tool"`
	Command     string    `json:"command"`
	RequestedAt time.Time `json:"requested_at"`
}

// Growler derives, stores and announces growlers.
type Growler struct {
	st  GrowlStore
	now func() time.Time

	// Wired by the proxy. Each is safe to leave nil in a test.
	attached func(room string) bool
	say      func(Event)
	health   func(ctx context.Context, room string) roomHealth
	hold     func(ctx context.Context, room string) roomHold
	pending  func(ctx context.Context, room string) []pendingPerm
	phone    func(Notice)
	// prWait is the sweep for PRs claimed by a room that is offline. See prclaim.go.
	prWait func() bool

	// pubMu is publish's, held while a set is built and compared.
	pubMu  sync.Mutex
	mu     sync.Mutex
	last   string
	pruned time.Time
	since  time.Time
	// filling is which rooms have a permission fill running, so a burst of
	// announcements is one request to that room.
	filling map[string]bool
	// fillMiss is how many times a permission growler's request was not in its
	// room's pending list. See fill.
	fillMiss map[string]int
}

// NewGrowler returns a growler over a store. Start runs its ticker.
func NewGrowler(st GrowlStore) *Growler {
	return &Growler{st: st, now: time.Now, filling: map[string]bool{}, fillMiss: map[string]int{}}
}

// permAfter is how long a permission waits before it growls.
func (g *Growler) permAfter() time.Duration { return g.after(settingGrowlPermAfter, growlPermAfter) }

// holdAfter is how long a deploy hold lasts before it growls.
func (g *Growler) holdAfter() time.Duration { return g.after(settingGrowlHoldAfter, growlHoldAfter) }

func (g *Growler) after(name string, def time.Duration) time.Duration {
	if v, err := g.st.Setting(name); err == nil && v != "" {
		if d, err := time.ParseDuration(v); err == nil && d >= 0 {
			return d
		}
	}
	return def
}

// ladder is which reasons remind on the backoff right now. Read on each tick,
// so a change takes effect on the next one. An unreadable value is the default.
func (g *Growler) ladder() map[string]bool {
	out := map[string]bool{}
	for r, on := range growlLadderDefault {
		out[r] = on
	}
	if v, err := g.st.Setting(settingGrowlLadder); err == nil && v != "" {
		var set map[string]bool
		if json.Unmarshal([]byte(v), &set) == nil {
			for r, on := range set {
				out[r] = on
			}
		}
	}
	return out
}

// growlEnded is a card whose session is over. It wants no growler whatever it
// asked or was blocked on, because nobody is left to answer.
func growlEnded(c CardState) bool {
	status := c.Status
	if status == "" {
		var p struct {
			Status string `json:"status"`
		}
		_ = json.Unmarshal(c.Payload, &p)
		status = p.Status
	}
	return status == "done" || status == "dead"
}

// growlID is the notify identity with the room in front.
func growlID(room, identity string) string { return room + "|" + identity }

// derive is what a room's cards want right now, and the names of those cards
// for a reminder to the phone.
// growlSince is when growlers began here, written the first time it is asked.
//
// THE QUESTIONS ALREADY WAITING WHEN GROWLERS ARRIVED ARE NOT RAISED. The
// first deploy turned eight unanswered Open Questions, some three days old,
// into growlers at once, each reminding the phone on the backoff. So a question
// older than this is left to its `?` chip, and a permission or a block, which
// is an agent frozen now, still growls whenever it began.
func (g *Growler) growlSince() time.Time {
	g.mu.Lock()
	since := g.since
	g.mu.Unlock()
	if !since.IsZero() {
		return since
	}
	if v, err := g.st.Setting(settingGrowlSince); err == nil && v != "" {
		if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
			since = t
		}
	}
	// Kept in memory whether or not it could be written, so a failed write
	// does not move it to "now" on every call and read every question as old.
	// The write is tried again at the next start.
	if since.IsZero() {
		since = g.now().UTC()
		_ = g.st.SetSetting(settingGrowlSince, since.Format(time.RFC3339Nano))
	}
	g.mu.Lock()
	g.since = since
	g.mu.Unlock()
	return since
}

func (g *Growler) derive(room string, cards []CardState) (want []GrowlRow, present map[string]bool) {
	present = map[string]bool{}
	after := g.permAfter()
	at := g.now()
	since := g.growlSince()
	// before is whether a reason began before growlers did. An unreadable time
	// is taken as new.
	before := func(ts string) bool {
		t, err := time.Parse(time.RFC3339Nano, ts)
		return err == nil && t.Before(since)
	}
	for _, c := range cards {
		present[c.ID] = true
		// A CARD THAT ENDED OR EXITED TAKES ITS GROWLERS WITH IT. It is still
		// announced, so sync ends what it had. A question asked before the
		// session ended would otherwise stand for as long as the card is kept.
		if growlEnded(c) {
			continue
		}
		nc, ok := cardReason(c.ID, c.Payload)
		if !ok {
			continue
		}
		row := GrowlRow{ID: growlID(room, nc.Identity), Room: room, CardID: c.ID, Reason: nc.Reason}
		switch nc.Reason {
		case ReasonPermission:
			// An unreadable time cannot be measured, so it is taken as due: a
			// growler too early is a dismiss, and one never raised is the miss
			// this whole feature is for.
			if since, err := time.Parse(time.RFC3339Nano, nc.At); err == nil && at.Sub(since) < after {
				continue
			}
			row.Title = nc.Name + " wants permission"
		case ReasonQuestion:
			if nc.Agent || before(nc.At) {
				continue
			}
			row.Title = nc.Name + " asked a question"
			if len(nc.Questions) > 1 {
				row.Title = fmt.Sprintf("%s asked %d questions", nc.Name, len(nc.Questions))
			}
			if len(nc.Questions) > 0 {
				row.Body = clip(nc.Questions[0])
			}
		case ReasonInput:
			// A REPORT, which the notifier reads as input. The identity is the
			// card, the report's status and when the card started waiting.
			if nc.Report == "" || nc.Agent || (nc.Report == ReasonQuestion && before(nc.At)) {
				continue
			}
			row.Reason = nc.Report
			row.ID = growlID(room, c.ID+"|"+nc.Report+"|"+nc.At)
			row.Title = nc.Name + " is blocked"
			if nc.Report == ReasonQuestion {
				row.Title = nc.Name + " has a question"
			}
			row.Body = clip(nc.Ask)
		default:
			continue
		}
		want = append(want, row)
	}
	return want, present
}

// clip is one line, bounded.
func clip(s string) string {
	s = firstLineOf(s)
	if r := []rune(s); len(r) > growlBodyMax {
		s = string(r[:growlBodyMax-1]) + "…"
	}
	return s
}

// Announced is called after a room's announcement is cached. One transaction,
// and a fill of any new permission off this goroutine.
func (g *Growler) Announced(room string, cards []CardState) {
	if g.sync(room, cards) {
		g.publish(nil)
	}
}

// sync derives and stores one room. Reports whether the live set changed.
func (g *Growler) sync(room string, cards []CardState) bool {
	want, present := g.derive(room, cards)
	raised, changed, err := g.st.Sync(room, growlCardReasons, want, present)
	if err != nil {
		log.Printf("[hub] growlers could not record %q: %v", room, err)
		return false
	}
	for _, id := range raised {
		if strings.Contains(id, "|"+ReasonPermission+"|") {
			go g.fill(context.Background(), room)
			break
		}
	}
	return changed
}

// fill asks a room which request each of its permission growlers is about, so
// the board can approve and block it by id and show the command.
func (g *Growler) fill(ctx context.Context, room string) {
	if g.pending == nil {
		return
	}
	g.mu.Lock()
	if g.filling[room] {
		g.mu.Unlock()
		return
	}
	g.filling[room] = true
	g.mu.Unlock()
	defer func() {
		g.mu.Lock()
		delete(g.filling, room)
		g.mu.Unlock()
	}()
	live, err := g.st.Live()
	if err != nil {
		return
	}
	var need []GrowlRow
	g.mu.Lock()
	for _, r := range live {
		// A request the room never lists is given up on after a few asks, so a
		// growler whose status lags is not a request to that room every tick.
		if r.Room == room && r.Reason == ReasonPermission && r.Subject == "" && g.fillMiss[r.ID] < growlFillTries {
			need = append(need, r)
		}
	}
	g.mu.Unlock()
	if len(need) == 0 {
		return
	}
	perms := g.pending(ctx, room)
	// The oldest request per card, which is the one that made it wait.
	oldest := map[string]pendingPerm{}
	for _, p := range perms {
		if o, ok := oldest[p.TaskID]; !ok || p.RequestedAt.Before(o.RequestedAt) {
			oldest[p.TaskID] = p
		}
	}
	moved := false
	for _, r := range need {
		p, ok := oldest[r.CardID]
		if !ok {
			g.mu.Lock()
			g.fillMiss[r.ID]++
			g.mu.Unlock()
			continue
		}
		body := clip(p.Command)
		if body == "" {
			body = p.Tool
		}
		if ch, err := g.st.Fill(r.ID, p.ID, body); err == nil && ch {
			moved = true
		}
	}
	if moved {
		g.publish(nil)
	}
}

// Start runs the ticker until ctx ends.
func (g *Growler) Start(ctx context.Context) {
	go func() {
		t := time.NewTicker(growlTick)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				g.tick(ctx)
			}
		}
	}()
}

// tick is one pass: every room's cards again, every attached room's health,
// snoozes due, reminders due, any permission still missing its request.
func (g *Growler) tick(ctx context.Context) {
	changed := false
	rooms, err := g.st.Rooms()
	if err != nil {
		return
	}
	names := map[string]string{}
	for _, room := range rooms {
		cards, err := g.st.Cards(room)
		if err != nil {
			continue
		}
		for _, c := range cards {
			if nc, ok := cardReason(c.ID, c.Payload); ok {
				names[room+"|"+c.ID] = nc.Name
			}
		}
		if g.sync(room, cards) {
			changed = true
		}
	}
	if g.checkRooms(ctx, rooms) {
		changed = true
	}
	if g.prWait != nil && g.prWait() {
		changed = true
	}
	remind, err := g.st.Wake()
	if err != nil {
		return
	}
	// woke is the snoozes that ended just now. They re-raise once whatever the
	// ladder says, and the phone hears it once too.
	woke := map[string]bool{}
	for _, id := range remind {
		woke[id] = true
	}
	if len(remind) > 0 {
		changed = true
	}
	live, err := g.st.Live()
	if err != nil {
		return
	}
	at := g.now()
	missing := map[string]bool{}
	// Misses for growlers that have ended are forgotten with them.
	g.mu.Lock()
	for id := range g.fillMiss {
		keep := false
		for _, r := range live {
			keep = keep || r.ID == id
		}
		if !keep {
			delete(g.fillMiss, id)
		}
	}
	g.mu.Unlock()
	ladder := g.ladder()
	phone := func(r GrowlRow) {
		if g.phone == nil || (r.Reason != ReasonPermission && r.Reason != ReasonQuestion) {
			return
		}
		name := names[r.Room+"|"+r.CardID]
		if name == "" {
			name = r.CardID
		}
		g.phone(Notice{Name: name, Reason: r.Reason, Card: tagFor(r.Room, r.CardID), Room: r.Room})
	}
	for _, r := range live {
		if r.Reason == ReasonPermission && r.Subject == "" {
			missing[r.Room] = true
		}
		if r.State != "open" {
			continue
		}
		// A REMIND-ME THAT CAME DUE IS ONE REMINDER, asked for by a human. It
		// goes to the phone even where the ladder is off.
		if woke[r.ID] {
			phone(r)
		}
		// A reason off the ladder rings once, when it is raised, and never
		// again unless a snooze wakes it.
		if !ladder[r.Reason] {
			continue
		}
		due := 0
		for _, step := range growlBackoff {
			if at.Sub(r.RaisedAt) >= step {
				due++
			}
		}
		if due <= r.Reminders {
			continue
		}
		if err := g.st.Reminded(r.ID, due); err != nil {
			continue
		}
		remind = append(remind, r.ID)
		// A REMINDER GOES TO THE PHONE TOO, on the same backoff (clint,
		// 2026-09-30), through the notify sink, which holds it back while a
		// desktop tab is visible and does nothing while notify is off.
		phone(r)
	}
	for room := range missing {
		go g.fill(ctx, room)
	}
	g.mu.Lock()
	prune := at.Sub(g.pruned) >= growlPruneEvery
	if prune {
		g.pruned = at
	}
	g.mu.Unlock()
	if prune {
		_ = g.st.Prune()
	}
	if changed || len(remind) > 0 {
		g.publish(dedupe(remind))
	}
}

func dedupe(ids []string) []string {
	seen := map[string]bool{}
	out := ids[:0]
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// checkRooms asks each attached room whether its store has halted and whether
// a deploy hold has outlived its window. A room that does not answer is left
// as it was: silence is neither healthy nor halted, held nor free.
func (g *Growler) checkRooms(ctx context.Context, rooms []string) bool {
	var ask []string
	for _, room := range rooms {
		if g.attached == nil || g.attached(room) {
			ask = append(ask, room)
		}
	}
	type answer struct {
		health roomHealth
		hold   roomHold
	}
	got := make([]answer, len(ask))
	var wg sync.WaitGroup
	for i, room := range ask {
		wg.Add(1)
		go func(i int, room string) {
			defer wg.Done()
			if g.health != nil {
				got[i].health = g.health(ctx, room)
			}
			if g.hold != nil {
				got[i].hold = g.hold(ctx, room)
			}
		}(i, room)
	}
	wg.Wait()
	changed := false
	record := func(room, reason string, on bool, row GrowlRow) {
		_, ch, err := g.st.Room(room, reason, on, row)
		if err != nil {
			log.Printf("[hub] growlers could not record %q's %s: %v", room, reason, err)
			return
		}
		changed = changed || ch
	}
	at := g.now()
	for i, room := range ask {
		if h := got[i].health; h.ok {
			record(room, "halt", h.halted, GrowlRow{
				ID: room + "|halt|" + at.UTC().Format(time.RFC3339Nano), Room: room, Reason: "halt",
				Title: room + " has halted", Body: clip(h.cause),
			})
		}
		if h := got[i].hold; h.ok {
			// UNDER THE WINDOW IS NOT A HOLD YET, and not a hold ended either: a
			// growler already raised for this hold stays until the hold lifts.
			if h.on && at.Sub(h.startedAt) < g.holdAfter() {
				continue
			}
			body := "held by " + h.by
			if len(h.whys) > 0 {
				body += ": " + strings.Join(h.whys, ", ")
			}
			record(room, "deploy-hold", h.on, GrowlRow{
				ID: room + "|deploy-hold|" + h.id, Room: room, Reason: "deploy-hold",
				Title: room + " is still held for a deploy", Body: clip(body), Subject: h.id,
			})
		}
	}
	return changed
}

// view is the live set, ordered and decorated for a screen.
func (g *Growler) view() ([]GrowlRow, error) {
	live, err := g.st.Live()
	if err != nil {
		return nil, err
	}
	for i := range live {
		g.decorate(&live[i])
	}
	// URGENCY, THEN LONGEST WAITING. Inside one urgency the oldest first,
	// because the agent frozen longest is the one costing most.
	sort.SliceStable(live, func(i, j int) bool {
		a, b := live[i], live[j]
		if a.Urgency != b.Urgency {
			return a.Urgency < b.Urgency
		}
		if !a.RaisedAt.Equal(b.RaisedAt) {
			return a.RaisedAt.Before(b.RaisedAt)
		}
		return a.ID < b.ID
	})
	if live == nil {
		live = []GrowlRow{}
	}
	return live, nil
}

func (g *Growler) decorate(r *GrowlRow) {
	r.Urgency = growlUrgency[r.Reason]
	if r.CardID != "" {
		r.Card = tagFor(r.Room, r.CardID)
	}
	r.RoomOffline = g.attached != nil && !g.attached(r.Room)
}

// payload is the endpoint's body and the event's data.
func (g *Growler) payload(remind []string) ([]byte, string, error) {
	rows, err := g.view()
	if err != nil {
		return nil, "", err
	}
	body := map[string]any{"growls": rows, "perm_after_seconds": int(g.permAfter() / time.Second)}
	fp, _ := json.Marshal(body)
	if len(remind) > 0 {
		body["remind"] = remind
	}
	data, err := json.Marshal(body)
	return data, string(fp), err
}

// publish sends the set when it differs from the last one sent, and always
// when there is a reminder in it.
func (g *Growler) publish(remind []string) {
	if g.say == nil {
		return
	}
	// BUILT AND COMPARED UNDER ONE LOCK, so two callers at once cannot send the
	// older set last and leave every screen a set behind until the next tick.
	g.pubMu.Lock()
	defer g.pubMu.Unlock()
	data, fp, err := g.payload(remind)
	if err != nil {
		return
	}
	same := fp == g.last
	g.last = fp
	if same && len(remind) == 0 {
		return
	}
	g.say(Event{Kind: "growls", Data: data})
}

// act is one POST, validated.
func (g *Growler) act(id, do string, minutes int, via, tab string) (GrowlRow, int, error) {
	var state string
	var until time.Time
	switch do {
	case "dismiss":
		state = "dismissed"
	case "acted":
		state = "acted"
	// The design doc says reopen and the board was built on undismiss. One
	// action under two names, rather than a board and a doc that disagree.
	case "reopen", "undismiss":
		state = "open"
	case "snooze":
		if minutes < 1 || minutes > growlSnoozeMaxMin {
			return GrowlRow{}, http.StatusBadRequest,
				fmt.Errorf("a snooze is 1 to %d minutes", growlSnoozeMaxMin)
		}
		state, until = "snoozed", g.now().Add(time.Duration(minutes)*time.Minute)
	default:
		return GrowlRow{}, http.StatusBadRequest,
			errors.New(`"do" is dismiss, reopen (or undismiss), snooze or acted`)
	}
	switch via {
	case "board", "phone", "notification":
	default:
		return GrowlRow{}, http.StatusBadRequest, errors.New(`"via" is board, phone or notification`)
	}
	if tab != "" && !validTab(tab) {
		return GrowlRow{}, http.StatusBadRequest, errors.New(`"tab" is a short plain token`)
	}
	row, err := g.st.Act(id, state, until, via, tab)
	switch {
	case errors.Is(err, ErrGrowlNotFound):
		return GrowlRow{}, http.StatusNotFound, err
	case errors.Is(err, ErrGrowlStale):
		g.decorate(&row)
		return row, http.StatusConflict, err
	case err != nil:
		return GrowlRow{}, http.StatusServiceUnavailable, err
	}
	g.decorate(&row)
	g.publish(nil)
	return row, http.StatusOK, nil
}

// ── the proxy's side ────────────────────────────────────

// SetGrowler wires the growler to this hub: which rooms are attached, the
// event stream, each room's health and pending requests, and the phone.
func (p *Proxy) SetGrowler(g *Growler) {
	g.attached = p.hub.Has
	g.say = p.feeds.broadcast
	g.health = p.roomHealth
	g.hold = p.roomHold
	g.pending = p.roomPending
	g.phone = func(x Notice) {
		if n := p.notifier(); n != nil {
			n.Remind(x)
		}
	}
	g.prWait = func() bool { return p.prWarnSweep(g) }
	p.mu.Lock()
	p.growl = g
	p.mu.Unlock()
}

func (p *Proxy) growler() *Growler {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.growl
}

// roomGet is one GET against one room, bounded, decoded into out.
func (p *Proxy) roomGet(ctx context.Context, room, path string, out any) bool {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+hostFor(room)+path, nil)
	if err != nil {
		return false
	}
	res, err := p.roomClient(room).Do(req)
	if err != nil {
		return false
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<16))
		return false
	}
	return json.NewDecoder(io.LimitReader(res.Body, 8<<20)).Decode(out) == nil
}

func (p *Proxy) roomHealth(ctx context.Context, room string) roomHealth {
	var body struct {
		Halted bool `json:"halted"`
		Cause  any  `json:"cause"`
	}
	if !p.roomGet(ctx, room, "/v1/health", &body) {
		return roomHealth{}
	}
	h := roomHealth{ok: true, halted: body.Halted}
	if body.Cause != nil {
		h.cause = fmt.Sprint(body.Cause)
	}
	return h
}

func (p *Proxy) roomHold(ctx context.Context, room string) roomHold {
	var body struct {
		Hold *struct {
			ID        string    `json:"id"`
			By        string    `json:"by"`
			Whys      []string  `json:"whys"`
			StartedAt time.Time `json:"started_at"`
		} `json:"hold"`
	}
	if !p.roomGet(ctx, room, "/v1/hold", &body) {
		return roomHold{}
	}
	if body.Hold == nil {
		return roomHold{ok: true}
	}
	return roomHold{ok: true, on: true, id: body.Hold.ID, by: body.Hold.By, whys: body.Hold.Whys,
		startedAt: body.Hold.StartedAt}
}

func (p *Proxy) roomPending(ctx context.Context, room string) []pendingPerm {
	var body struct {
		Permissions []pendingPerm `json:"permissions"`
	}
	if !p.roomGet(ctx, room, "/v1/permissions", &body) {
		return nil
	}
	return body.Permissions
}

// serveGrowls answers GET /_hub/growls and POST /_hub/growls/{id}.
//
// OPEN LIKE PRESENCE, NOT LOOPBACK ONLY: dismissing from the phone over an
// overlay is the point. Nothing here runs a program or names one.
func (p *Proxy) serveGrowls(w http.ResponseWriter, r *http.Request, sub string) {
	g := p.growler()
	if g == nil {
		http.NotFound(w, r)
		return
	}
	fail := func(code int, msg string) {
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
	}
	if sub == "growls" {
		if r.Method != http.MethodGet {
			fail(http.StatusMethodNotAllowed, "that has to be a GET")
			return
		}
		data, _, err := g.payload(nil)
		if err != nil {
			fail(http.StatusServiceUnavailable, err.Error())
			return
		}
		_, _ = w.Write(data)
		return
	}
	if r.Method != http.MethodPost {
		fail(http.StatusMethodNotAllowed, "that has to be a POST")
		return
	}
	id := strings.TrimPrefix(sub, "growls/")
	var body struct {
		Do      string `json:"do"`
		Minutes int    `json:"minutes"`
		Via     string `json:"via"`
		Tab     string `json:"tab"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4<<10)).Decode(&body); err != nil {
		fail(http.StatusBadRequest, "could not read that: "+err.Error())
		return
	}
	row, code, err := g.act(id, body.Do, body.Minutes, body.Via, body.Tab)
	switch code {
	case http.StatusOK, http.StatusConflict:
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(map[string]any{"growl": row})
	default:
		fail(code, err.Error())
	}
}

// serveGrowlLadder answers GET and PUT /_hub/growl-ladder: which reasons remind
// on the backoff, every reason named, true or false. A PUT sets only the
// reasons it names and is for the machine the hub runs on, like /_hub/hosts.
func (p *Proxy) serveGrowlLadder(w http.ResponseWriter, r *http.Request) {
	g := p.growler()
	if g == nil {
		http.NotFound(w, r)
		return
	}
	fail := func(code int, msg string) {
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
	}
	switch r.Method {
	case http.MethodGet:
	case http.MethodPut:
		if !edge.LocalOperator(r) {
			fail(http.StatusForbidden, "the reminders are set only from the machine the hub runs on"+edge.ProxyNote(r))
			return
		}
		var set map[string]bool
		if err := json.NewDecoder(io.LimitReader(r.Body, 4<<10)).Decode(&set); err != nil {
			fail(http.StatusBadRequest, "could not read that: "+err.Error())
			return
		}
		next := g.ladder()
		for reason, on := range set {
			if !contains(growlLadderReasons, reason) {
				fail(http.StatusBadRequest, "a reason is one of "+strings.Join(growlLadderReasons, ", "))
				return
			}
			next[reason] = on
		}
		b, _ := json.Marshal(next)
		if err := g.st.SetSetting(settingGrowlLadder, string(b)); err != nil {
			fail(http.StatusInternalServerError, "could not save that: "+err.Error())
			return
		}
		p.RecordAudit("", "growl-ladder-set", string(b))
	default:
		fail(http.StatusMethodNotAllowed, "that has to be a GET or a PUT")
		return
	}
	cur := g.ladder()
	out := map[string]bool{}
	for _, reason := range growlLadderReasons {
		out[reason] = cur[reason]
	}
	_ = json.NewEncoder(w).Encode(out)
}

// openGrowls hands one stream the live set as it opens, so a fresh tab has it
// without a fetch.
func (p *Proxy) openGrowls(s *sub) {
	g := p.growler()
	if g == nil {
		return
	}
	data, _, err := g.payload(nil)
	if err != nil {
		return
	}
	// Waited for, briefly: `publish` sends nothing for a set that has not
	// changed, so a tab that missed this would see no growler for hours.
	select {
	case s.ch <- Event{Kind: "growls", Data: data}:
	case <-time.After(time.Second):
	}
}
