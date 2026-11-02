package forge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dovholuknf/atrium/internal/base/edge"
	"github.com/dovholuknf/atrium/internal/hubstore"
	"github.com/dovholuknf/atrium/internal/link/wire"
)

// The hub hears a local script and tells the cards that asked. See docs/fabric/land-webhook-design.md.
//
// POST /_hub/events takes one event from the machine the hub runs on, records the fields it parsed, writes one delivery
// per matching subscription and answers 202 before any delivery is tried. Delivery is the CI hook's loop (CIHook.Pass
// runs EventHook.Pass), so a land notice and a CI notice are the same kind of fyi with the same expiry and never resent
// once unconfirmed.

const (
	eventMaxBody     = 64 << 10
	eventRatePerMin  = 60
	eventRateBurst   = 20
	eventMaxReason   = 200
	eventMaxCommits  = 10000
	eventExistEvery  = 24 * time.Hour
	eventSubBodyMax  = 4 << 10
	eventSignoff     = "(from the hub, no reply goes anywhere)"
	eventKindLand    = "land"
	eventAuditEvent  = "event"
	eventAuditDelivr = "event-delivery"
	eventAuditGone   = "event-sub-gone"
)

// hubSubsPath is the one events path a room may ask. A room never posts an event. hubAsk stamps `room` and `by` from the
// link, so a room can only write subscriptions of its own cards.
const HubSubsPath = "/_hub/events/subs"

// eventKinds is the closed list of kinds, each with a parser that checks its own fields and builds its one line.
var eventKinds = map[string]func(raw []byte) (hubstore.HubEvent, string, error){
	eventKindLand: parseLand,
}

func eventKindNames() string {
	names := make([]string, 0, len(eventKinds))
	for k := range eventKinds {
		names = append(names, k)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// EventHook is the receiver for local events, the subscriptions, and the delivery of one to the other.
type EventHook struct {
	Store   *hubstore.Store
	Deliver Deliverer
	// Audit writes one hub-level line. Nil records nothing.
	Audit func(kind, detail string)
	// Now is the clock, injected by tests.
	Now func() time.Time
	// From is the sender a delivery names, `land@<hub>`.
	From string
	// Rooms names the rooms attached now. Exist asks one of them which of these card ids it still has. Either nil
	// skips the daily check.
	Rooms func() []string
	Exist func(ctx context.Context, room string, ids []string) ([]string, error)

	bucket    ciBucket
	kick      chan struct{}
	mu        sync.Mutex
	swept     time.Time
	checked   time.Time
	checkSeen bool
}

// NewEventHook builds the receiver.
func NewEventHook(st *hubstore.Store, d Deliverer, audit func(kind, detail string)) *EventHook {
	return &EventHook{Store: st, Deliver: d, Audit: audit, From: "land@hub"}
}

func (h *EventHook) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
}

func (h *EventHook) audit(kind, detail string) {
	if h.Audit != nil {
		h.Audit(kind, detail)
	}
}

// ── receiving ──────────────────────────────────────────

// Receive is POST /_hub/events.
func (h *EventHook) Receive(w http.ResponseWriter, r *http.Request) {
	if !h.allow() {
		w.Header().Set("Retry-After", "1")
		ciReply(w, http.StatusTooManyRequests, map[string]string{"error": "too many events, slow down"})
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, eventMaxBody))
	if err != nil {
		var big *http.MaxBytesError
		if errors.As(err, &big) {
			ciReply(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "the body is over 64 KiB"})
			return
		}
		ciReply(w, http.StatusBadRequest, map[string]string{"error": "the body could not be read"})
		return
	}
	var head struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		ciReply(w, http.StatusUnprocessableEntity, map[string]string{"error": "the body is not a JSON object"})
		return
	}
	parse, ok := eventKinds[head.Kind]
	if !ok {
		ciReply(w, http.StatusUnprocessableEntity, map[string]string{
			"error": fmt.Sprintf("kind is one of: %s", eventKindNames())})
		return
	}
	ev, line, err := parse(raw)
	if err != nil {
		ciReply(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	ev, dup, err := h.Store.EventAdd(ev)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if dup {
		ciReply(w, http.StatusOK, map[string]bool{"duplicate": true})
		return
	}
	subs, err := h.Store.EventSubsMatching(ev.Kind, ev.Repo, ev.Branch)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	told := make([]string, 0, len(subs))
	for _, s := range subs {
		if _, err := h.Store.EventDeliveryAdd(hubstore.EventDelivery{
			EventRef: ev.ID, Target: s.Card, Kind: "fyi", Wake: s.Wake, Text: line}); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		told = append(told, s.Card)
	}
	h.audit(eventAuditEvent, fmt.Sprintf("%s %s %s on %s. told: %s", ev.Kind, ev.Outcome, ev.Repo+" "+ev.Branch, ev.Source,
		ciOrNobody(told)))
	if len(told) > 0 && h.kick != nil {
		select {
		case h.kick <- struct{}{}:
		default:
		}
	}
	ciReply(w, http.StatusAccepted, map[string]any{"event": ev.ID, "subscribers": len(told)})
}

func (h *EventHook) allow() bool {
	b := &h.bucket
	b.mu.Lock()
	defer b.mu.Unlock()
	now := h.now()
	if b.last.IsZero() {
		b.tokens = eventRateBurst
	} else if d := now.Sub(b.last).Seconds(); d > 0 {
		b.tokens += d * eventRatePerMin / 60
		if b.tokens > eventRateBurst {
			b.tokens = eventRateBurst
		}
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// ── the land kind ──────────────────────────────────────

var (
	eventShaRe     = regexp.MustCompile(`^[0-9a-fA-F]{7,40}$`)
	eventOutcomes  = []string{"pushed", "not-pushed", "stopped"}
	errLandOutcome = errors.New("outcome is pushed, not-pushed or stopped")
)

type landBody struct {
	Kind    string `json:"kind"`
	Repo    string `json:"repo"`
	Branch  string `json:"branch"`
	Outcome string `json:"outcome"`
	From    string `json:"from"`
	To      string `json:"to"`
	Commits int    `json:"commits"`
	Reason  string `json:"reason"`
}

func parseLand(raw []byte) (hubstore.HubEvent, string, error) {
	var in landBody
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		return hubstore.HubEvent{}, "", fmt.Errorf("a land event is kind, repo, branch, outcome, from, to, commits and reason: %v", err)
	}
	in.Repo, in.Branch, in.Outcome = strings.TrimSpace(in.Repo), strings.TrimSpace(in.Branch), strings.TrimSpace(in.Outcome)
	in.From, in.To = strings.ToLower(strings.TrimSpace(in.From)), strings.ToLower(strings.TrimSpace(in.To))
	switch {
	case !ciRepoRe.MatchString(in.Repo) || len(in.Repo) > 200:
		return hubstore.HubEvent{}, "", errors.New("repo is required, as owner/name")
	case !validBranch(in.Branch):
		return hubstore.HubEvent{}, "", errors.New("branch is required, and has to be a branch name git accepts")
	case !slices.Contains(eventOutcomes, in.Outcome):
		return hubstore.HubEvent{}, "", errLandOutcome
	case in.From != "" && !eventShaRe.MatchString(in.From):
		return hubstore.HubEvent{}, "", errors.New("from is 7 to 40 hex characters")
	case in.To != "" && !eventShaRe.MatchString(in.To):
		return hubstore.HubEvent{}, "", errors.New("to is 7 to 40 hex characters")
	case in.Outcome == "pushed" && in.To == "":
		return hubstore.HubEvent{}, "", errors.New("a pushed land needs to, the commit it landed at")
	case in.Commits < 0 || in.Commits > eventMaxCommits:
		return hubstore.HubEvent{}, "", fmt.Errorf("commits is 0 to %d", eventMaxCommits)
	case len(in.Reason) > eventMaxReason:
		return hubstore.HubEvent{}, "", fmt.Errorf("reason is at most %d bytes", eventMaxReason)
	}
	reason := ciClean(in.Reason, eventMaxReason)
	ev := hubstore.HubEvent{Kind: eventKindLand, Repo: in.Repo, Branch: in.Branch, Outcome: in.Outcome, From: in.From,
		To: in.To, Commits: in.Commits, Reason: reason}
	return ev, landLine(ev), nil
}

func short7(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

func commitsOf(n int) string {
	if n == 1 {
		return "1 commit"
	}
	return fmt.Sprintf("%d commits", n)
}

// landLine is the one line a land says, built from parsed fields. Reason is the one free-text field, bounded and cleaned.
func landLine(e hubstore.HubEvent) string {
	var line string
	switch e.Outcome {
	case "pushed":
		span := short7(e.To)
		if e.From != "" {
			span = short7(e.From) + ".." + short7(e.To)
		}
		line = fmt.Sprintf("Landed: %s %s %s, %s.", e.Repo, e.Branch, span, commitsOf(e.Commits))
	case "not-pushed":
		line = fmt.Sprintf("Land not pushed: %s %s, %s ready", e.Repo, e.Branch, commitsOf(e.Commits))
		if e.To != "" {
			line += " at " + short7(e.To)
		}
		line += "."
	default:
		line = fmt.Sprintf("Land stopped: %s %s.", e.Repo, e.Branch)
	}
	if e.Reason != "" && e.Outcome != "pushed" {
		line += " " + e.Reason
	}
	return line + " " + eventSignoff
}

// validBranch follows `git check-ref-format --branch` closely enough to keep a name out of a line that git would refuse.
func validBranch(b string) bool {
	if b == "" || len(b) > 200 || b == "@" || strings.HasPrefix(b, "-") || strings.HasPrefix(b, "/") ||
		strings.HasSuffix(b, "/") || strings.HasSuffix(b, ".") || strings.HasSuffix(b, ".lock") ||
		strings.Contains(b, "..") || strings.Contains(b, "@{") || strings.Contains(b, "//") {
		return false
	}
	for _, r := range b {
		if r < 0x21 || r == 0x7f || strings.ContainsRune("~^:?*[\\", r) {
			return false
		}
	}
	for _, part := range strings.Split(b, "/") {
		if strings.HasPrefix(part, ".") || strings.HasSuffix(part, ".lock") {
			return false
		}
	}
	return true
}

// ── subscriptions ──────────────────────────────────────

// subRequest is a body to POST /_hub/events/subs. Room is set from the room link by the hub's relay and never by a
// caller that is not a room, and it limits the cards the request may name to that room's.
type subRequest struct {
	Action string `json:"action"`
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Repo   string `json:"repo"`
	Branch string `json:"branch"`
	Wake   bool   `json:"wake"`
	Card   string `json:"card"`
	Room   string `json:"room"`
	By     string `json:"by"`
}

func subFail(w http.ResponseWriter, code int, msg string) {
	ciReply(w, code, map[string]string{"error": msg})
}

// Subs is `/_hub/events/subs`: subscribe, unsubscribe and list. The board writes any card, and a room only its own.
func (h *EventHook) Subs(w http.ResponseWriter, r *http.Request) {
	var in subRequest
	switch r.Method {
	case http.MethodGet:
		in.Action, in.Card, in.Room = "list", r.URL.Query().Get("card"), r.URL.Query().Get("room")
	case http.MethodPost:
		raw, err := io.ReadAll(io.LimitReader(r.Body, eventSubBodyMax))
		if err != nil || json.Unmarshal(raw, &in) != nil {
			subFail(w, http.StatusBadRequest, "that is not a JSON object")
			return
		}
	default:
		subFail(w, http.StatusMethodNotAllowed, "that has to be a GET or a POST")
		return
	}
	in.Card = strings.TrimSpace(in.Card)
	in.Room = strings.TrimSpace(in.Room)
	if in.Room != "" {
		switch {
		case in.Card == "":
		case !strings.Contains(in.Card, "~"):
			in.Card = in.Room + "~" + in.Card
		case !strings.HasPrefix(in.Card, in.Room+"~"):
			subFail(w, http.StatusForbidden, "room "+in.Room+" may only write subscriptions for its own cards")
			return
		}
	}
	switch in.Action {
	case "list":
		h.subList(w, in)
	case "subscribe":
		h.subAdd(w, in)
	case "unsubscribe":
		h.subRemove(w, in)
	default:
		subFail(w, http.StatusUnprocessableEntity, "action is subscribe, unsubscribe or list")
	}
}

func (h *EventHook) subList(w http.ResponseWriter, in subRequest) {
	var subs []hubstore.EventSub
	var err error
	switch {
	case in.Card != "":
		subs, err = h.Store.EventSubsOf(in.Card)
	case in.Room != "":
		subFail(w, http.StatusUnprocessableEntity, "a room lists the subscriptions of a card, and this names none")
		return
	default:
		subs, err = h.Store.EventSubsAll()
	}
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if subs == nil {
		subs = []hubstore.EventSub{}
	}
	ciReply(w, http.StatusOK, map[string]any{"subscriptions": subs})
}

func (h *EventHook) subAdd(w http.ResponseWriter, in subRequest) {
	in.Kind, in.Repo, in.Branch = strings.TrimSpace(in.Kind), strings.TrimSpace(in.Repo), strings.TrimSpace(in.Branch)
	if _, ok := eventKinds[in.Kind]; !ok {
		subFail(w, http.StatusUnprocessableEntity, "kind is one of: "+eventKindNames())
		return
	}
	if _, _, ok := hubstore.SplitEventCard(in.Card); !ok {
		subFail(w, http.StatusUnprocessableEntity, "card is room~id")
		return
	}
	if in.Repo != "" && (!ciRepoRe.MatchString(in.Repo) || len(in.Repo) > 200) {
		subFail(w, http.StatusUnprocessableEntity, "repo is owner/name, or empty for any")
		return
	}
	if in.Branch != "" && !validBranch(in.Branch) {
		subFail(w, http.StatusUnprocessableEntity, "branch has to be a branch name git accepts, or empty for any")
		return
	}
	by := "board"
	if in.Room != "" {
		by = strings.TrimSpace(in.By)
		if by == "" || len(by) > 200 {
			by = in.Card
		}
	}
	sub, existed, err := h.Store.EventSubAdd(hubstore.EventSub{Kind: in.Kind, Repo: in.Repo, Branch: in.Branch,
		Card: in.Card, Wake: in.Wake, CreatedBy: ciClean(by, 200)})
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if !existed {
		h.audit("event-sub", fmt.Sprintf("%s subscribed to %s %s %s", sub.Card, sub.Kind, orAny(sub.Repo), orAny(sub.Branch)))
	}
	ciReply(w, http.StatusOK, map[string]any{"subscription": sub, "already": existed})
}

func orAny(s string) string {
	if s == "" {
		return "any"
	}
	return s
}

func (h *EventHook) subRemove(w http.ResponseWriter, in subRequest) {
	if id := strings.TrimSpace(in.ID); id != "" {
		sub, ok, err := h.Store.EventSubGet(id)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if !ok {
			ciReply(w, http.StatusOK, map[string]any{"removed": 0})
			return
		}
		if in.Room != "" && !strings.HasPrefix(sub.Card, in.Room+"~") {
			subFail(w, http.StatusForbidden, "room "+in.Room+" may only write subscriptions for its own cards")
			return
		}
		if _, err := h.Store.EventSubDelete(id); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		h.audit("event-unsub", fmt.Sprintf("%s unsubscribed from %s %s %s", sub.Card, sub.Kind, orAny(sub.Repo), orAny(sub.Branch)))
		ciReply(w, http.StatusOK, map[string]any{"removed": 1})
		return
	}
	if _, _, ok := hubstore.SplitEventCard(in.Card); !ok {
		subFail(w, http.StatusUnprocessableEntity, "name the subscription's id, or the card and the kind, repo and branch")
		return
	}
	subs, err := h.Store.EventSubsOf(in.Card)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	n := 0
	for _, s := range subs {
		if s.Kind != strings.TrimSpace(in.Kind) || s.Repo != strings.TrimSpace(in.Repo) || s.Branch != strings.TrimSpace(in.Branch) {
			continue
		}
		if ok, err := h.Store.EventSubDelete(s.ID); err == nil && ok {
			n++
			h.audit("event-unsub", fmt.Sprintf("%s unsubscribed from %s %s %s", s.Card, s.Kind, orAny(s.Repo), orAny(s.Branch)))
		}
	}
	ciReply(w, http.StatusOK, map[string]any{"removed": n})
}

// ── delivery ───────────────────────────────────────────

// Pass tries every pending event delivery once, after the day's existence check and the hourly sweep.
func (h *EventHook) Pass(ctx context.Context) {
	h.mu.Lock()
	defer h.mu.Unlock()
	now := h.now()
	if now.Sub(h.swept) >= ciSweepEvery {
		h.swept = now
		_, _ = h.Store.EventSweep()
	}
	if !h.checkSeen || now.Sub(h.checked) >= eventExistEvery {
		h.checkSeen, h.checked = true, now
		h.checkExist(ctx)
	}
	if h.Deliver == nil {
		return
	}
	pending, err := h.Store.EventDeliveriesPending()
	if err != nil {
		return
	}
	live := pending[:0:0]
	for _, d := range pending {
		if !d.Created.Add(ciPendingFor).After(now) {
			h.finish(d, hubstore.CIExpired, "no session took it in 24 hours", d.Card, eventAuditDelivr)
			continue
		}
		live = append(live, d)
	}
	newest := map[string]string{}
	for _, d := range live {
		newest[eventStream(d)] = d.ID
	}
	for _, d := range live {
		if newest[eventStream(d)] != d.ID {
			h.finish(d, hubstore.CIExpired, "superseded", d.Card, eventAuditDelivr)
			continue
		}
		h.try(ctx, d)
	}
}

func eventStream(d hubstore.EventDelivery) string {
	return strings.ToLower(d.Target) + "\x00" + d.EventKind + "\x00" + strings.ToLower(d.Repo) + "\x00" + d.Branch
}

func (h *EventHook) finish(d hubstore.EventDelivery, state, detail, card, auditKind string) {
	if err := h.Store.EventDeliverySet(d.ID, state, detail, card); err != nil {
		return
	}
	if auditKind != "" {
		h.audit(auditKind, fmt.Sprintf("%s %s on %s to %s: %s", d.EventKind, d.Repo, d.Branch, d.Target, detail))
	}
}

func (h *EventHook) try(ctx context.Context, d hubstore.EventDelivery) {
	room, id, ok := hubstore.SplitEventCard(d.Target)
	if !ok {
		h.finish(d, hubstore.CIRefused, "the subscription's card is not room~id", "", eventAuditDelivr)
		return
	}
	a := h.Deliver.Send(ctx, CITarget{Card: d.Target, Room: room, ID: id}, h.From, d.Text, d.Kind, d.Wake)
	switch a.State {
	case hubstore.CIRefused:
		if a.Gone {
			h.dropGone(d.Target, a.Detail)
			h.finish(d, hubstore.CIRefused, "the card is gone", d.Target, "")
			return
		}
		h.finish(d, a.State, a.Detail, d.Target, eventAuditDelivr)
	case hubstore.CIDelivered, hubstore.CIUnconfirmed:
		h.finish(d, a.State, a.Detail, d.Target, "")
	default:
		h.finish(d, hubstore.CIPending, a.Detail, d.Target, "")
	}
}

// dropGone deletes a card's subscriptions and writes the one audit line.
func (h *EventHook) dropGone(card, why string) {
	n, err := h.Store.EventSubDeleteCard(card)
	if err != nil || n == 0 {
		return
	}
	h.audit(eventAuditGone, fmt.Sprintf("%s: %d subscription(s) removed, the room says it has no such card (%s)", card, n, why))
}

// checkExist asks each attached room which of its subscribed cards it still has and deletes the rest. A room that is not
// attached, or does not answer, is skipped: that is never the same as every card gone.
func (h *EventHook) checkExist(ctx context.Context) {
	if h.Rooms == nil || h.Exist == nil {
		return
	}
	subs, err := h.Store.EventSubsAll()
	if err != nil || len(subs) == 0 {
		return
	}
	byRoom := map[string]map[string]bool{}
	for _, s := range subs {
		if room, id, ok := hubstore.SplitEventCard(s.Card); ok {
			if byRoom[room] == nil {
				byRoom[room] = map[string]bool{}
			}
			byRoom[room][id] = true
		}
	}
	for _, room := range h.Rooms() {
		set := byRoom[room]
		if len(set) == 0 {
			continue
		}
		ids := make([]string, 0, len(set))
		for id := range set {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		have, err := h.Exist(ctx, room, ids)
		if err != nil {
			continue
		}
		kept := map[string]bool{}
		for _, id := range have {
			kept[id] = true
		}
		for _, id := range ids {
			if !kept[id] {
				h.dropGone(room+"~"+id, "daily check")
			}
		}
	}
}

// ── the hub's door ─────────────────────────────────────

// SetEvents turns events on for this hub. A hub that never calls it answers /_hub/events 404.
func (p *Service) SetEvents(h *EventHook) {
	p.mu.Lock()
	p.ev = h
	p.mu.Unlock()
}

func (p *Service) EventHookOf() *EventHook {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.ev
}

// NewEventHook is the hub's event hook with its say path, its rooms and its existence check wired.
func (p *Service) NewEventHook(st *hubstore.Store, from string, audit func(kind, detail string)) *EventHook {
	h := NewEventHook(st, p.Deliverer(), audit)
	if strings.TrimSpace(from) != "" {
		h.From = from
	}
	h.Rooms = func() []string {
		var out []string
		for _, r := range p.Rooms() {
			out = append(out, r.Name)
		}
		return out
	}
	h.Exist = func(ctx context.Context, room string, ids []string) ([]string, error) {
		c := p.ctlCaller()
		if c == nil {
			return nil, errors.New("the hub's control server is not up")
		}
		var out struct {
			Exist []string `json:"exist"`
		}
		if err := c.Ask(ctx, http.MethodPost, "/v1/tasks/exist", room, map[string]any{"ids": ids}, &out); err != nil {
			return nil, err
		}
		return out.Exist, nil
	}
	return h
}

// serveHubEvents answers POST /_hub/events and GET or POST /_hub/events/subs. Loopback only: a share or an overlay request is
// a 403.
func (p *Service) ServeHubEvents(w http.ResponseWriter, r *http.Request, sub string) {
	h := p.EventHookOf()
	if h == nil {
		http.NotFound(w, r)
		return
	}
	if !edge.LocalOperator(r) {
		ciReply(w, http.StatusForbidden, map[string]string{
			"error": "events are posted from the machine the hub runs on" + edge.ProxyNote(r)})
		return
	}
	switch {
	case sub == "events" && r.Method == http.MethodPost:
		h.Receive(w, r)
	case sub == "events/subs" && (r.Method == http.MethodGet || r.Method == http.MethodPost):
		if r.Method == http.MethodPost && wire.DocsCrossOrigin.Check(r) != nil {
			subFail(w, http.StatusForbidden, "a page on another origin cannot write here")
			return
		}
		h.Subs(w, r)
	default:
		subFail(w, http.StatusMethodNotAllowed, "that has to be a POST to /_hub/events, or a GET or POST to /_hub/events/subs")
	}
}
