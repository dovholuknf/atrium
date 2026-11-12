package link

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/base/edge"
	"github.com/dovholuknf/atrium/internal/hubstore"
	"github.com/dovholuknf/atrium/internal/link/notify"
	"github.com/dovholuknf/atrium/internal/link/wire"
)

// atrium_remind: a card asks the hub to bring something back to it later. The hub keeps the row, so it survives a hub
// or room restart and a parked card. When one is due the hub raises a `reminder` growler on that card, which rings the
// bell and the phone like any growler and can be snoozed the same way. With `wake` it also says the text to the card,
// resuming it if it is parked.
//
//	POST /_hub/remind   {action: set|cancel|list, card, room, by, in|at, text, wake, id}
//
// A room asks over the hub relay, which stamps `room` from the link, so a room can only name its own cards.

const (
	remindBodyMax    = 4 << 10
	remindTextMax    = 500
	remindTick       = 30 * time.Second
	remindWakeGiveUp = 24 * time.Hour
	remindFrom       = "remind@hub"
)

// Reminders is the endpoint and the loop that brings each due reminder.
type Reminders struct {
	Store   *hubstore.Store
	Growler func() *Growler
	Deliver Deliverer
	// Audit writes one hub-level line. Nil records nothing.
	Audit func(kind, detail string)
	// Now is the clock, injected by tests.
	Now func() time.Time
}

func (h *Reminders) now() time.Time {
	if h.Now != nil {
		return h.Now().UTC()
	}
	return time.Now().UTC()
}

func (h *Reminders) audit(kind, detail string) {
	if h.Audit != nil {
		h.Audit(kind, detail)
	}
}

type remindRequest struct {
	Action string `json:"action"`
	ID     string `json:"id"`
	Card   string `json:"card"`
	Room   string `json:"room"`
	By     string `json:"by"`
	In     string `json:"in"`
	At     string `json:"at"`
	Text   string `json:"text"`
	Wake   bool   `json:"wake"`
}

func remindFail(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func remindReply(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

var remindPart = regexp.MustCompile(`^(\d+)([wdhms])`)

// parseRemindIn reads "3d", "4h", "90m" or a run of them such as "1d12h".
func parseRemindIn(s string) (time.Duration, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	unit := map[string]time.Duration{"w": 7 * 24 * time.Hour, "d": 24 * time.Hour, "h": time.Hour, "m": time.Minute, "s": time.Second}
	var d time.Duration
	for rest := s; rest != ""; {
		m := remindPart.FindStringSubmatch(rest)
		if m == nil {
			return 0, fmt.Errorf("in is a duration such as 3d, 4h or 90m, not %q", s)
		}
		n, err := strconv.Atoi(m[1])
		if err != nil || n > 100000 {
			return 0, fmt.Errorf("in is a duration such as 3d, 4h or 90m, not %q", s)
		}
		d += time.Duration(n) * unit[m[2]]
		rest = rest[len(m[0]):]
	}
	if d <= 0 {
		return 0, errors.New("in has to be more than zero")
	}
	return d, nil
}

func remindClean(s string, max int) string {
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || r >= 0x20 && r != 0x7f {
			return r
		}
		return -1
	}, s)
	if r := []rune(strings.TrimSpace(s)); len(r) > max {
		return string(r[:max])
	}
	return strings.TrimSpace(s)
}

// Serve is /_hub/remind. Loopback only, like the events routes.
func (h *Reminders) Serve(w http.ResponseWriter, r *http.Request) {
	if !edge.LocalOperator(r) {
		remindFail(w, http.StatusForbidden, "reminders are set from the machine the hub runs on"+edge.ProxyNote(r))
		return
	}
	var in remindRequest
	switch r.Method {
	case http.MethodGet:
		in.Action, in.Card, in.Room = "list", r.URL.Query().Get("card"), r.URL.Query().Get("room")
	case http.MethodPost:
		if wire.DocsCrossOrigin.Check(r) != nil {
			remindFail(w, http.StatusForbidden, "a page on another origin cannot write here")
			return
		}
		raw, err := io.ReadAll(io.LimitReader(r.Body, remindBodyMax))
		if err != nil || json.Unmarshal(raw, &in) != nil {
			remindFail(w, http.StatusBadRequest, "that is not a JSON object")
			return
		}
	default:
		remindFail(w, http.StatusMethodNotAllowed, "that has to be a GET or a POST")
		return
	}
	in.Card, in.Room = strings.TrimSpace(in.Card), strings.TrimSpace(in.Room)
	if in.Room != "" {
		switch {
		case in.Card == "":
		case !strings.Contains(in.Card, "~"):
			in.Card = in.Room + "~" + in.Card
		case !strings.HasPrefix(in.Card, in.Room+"~"):
			remindFail(w, http.StatusForbidden, "room "+in.Room+" may only set reminders for its own cards")
			return
		}
	}
	if _, _, ok := hubstore.SplitEventCard(in.Card); !ok {
		remindFail(w, http.StatusUnprocessableEntity, "card is room~id")
		return
	}
	switch in.Action {
	case "", "list":
		rs, err := h.Store.ReminderPendingOf(in.Card)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		remindReply(w, map[string]any{"reminders": rs})
	case "set":
		h.set(w, in)
	case "cancel":
		h.cancel(w, in)
	default:
		remindFail(w, http.StatusUnprocessableEntity, "action is set, cancel or list")
	}
}

func (h *Reminders) set(w http.ResponseWriter, in remindRequest) {
	now := h.now()
	text := remindClean(in.Text, remindTextMax)
	var due time.Time
	switch {
	case text == "":
		remindFail(w, http.StatusUnprocessableEntity, "say what to remind about, in text")
		return
	case (in.In == "") == (in.At == ""):
		remindFail(w, http.StatusUnprocessableEntity, "give in (such as 3d) or at (RFC3339), and not both")
		return
	case in.In != "":
		d, err := parseRemindIn(in.In)
		if err != nil {
			remindFail(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		due = now.Add(d)
	default:
		t, err := time.Parse(time.RFC3339, strings.TrimSpace(in.At))
		if err != nil {
			remindFail(w, http.StatusUnprocessableEntity, "at is an RFC3339 time such as 2026-11-15T09:00:00Z")
			return
		}
		due = t.UTC()
	}
	if !due.After(now) {
		remindFail(w, http.StatusUnprocessableEntity, "that time has already passed")
		return
	}
	if due.After(now.Add(hubstore.ReminderMaxAhead)) {
		remindFail(w, http.StatusUnprocessableEntity, "a reminder can be at most 90 days out")
		return
	}
	by := remindClean(in.By, 200)
	if in.Room == "" || by == "" {
		by = "board"
		if in.Room != "" {
			by = in.Card
		}
	}
	rem, err := h.Store.ReminderAdd(hubstore.Reminder{Card: in.Card, Text: text, DueAt: due, Wake: in.Wake, CreatedBy: by})
	if errors.Is(err, hubstore.ErrReminderFull) {
		remindFail(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	h.audit("remind-set", fmt.Sprintf("%s set a reminder for %s on %s", by, rem.DueAt.Format(time.RFC3339), rem.Card))
	remindReply(w, map[string]any{"reminder": rem})
}

func (h *Reminders) cancel(w http.ResponseWriter, in remindRequest) {
	if id := strings.TrimSpace(in.ID); id != "" {
		rem, ok, err := h.Store.ReminderGet(id)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if !ok || rem.Card != in.Card {
			remindReply(w, map[string]any{"removed": 0})
			return
		}
		done, err := h.Store.ReminderCancel(id)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		n := 0
		if done {
			n = 1
			h.audit("remind-cancel", "a reminder on "+in.Card+" was cancelled")
		}
		remindReply(w, map[string]any{"removed": n})
		return
	}
	n, err := h.Store.ReminderCancelCard(in.Card)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if n > 0 {
		h.audit("remind-cancel", fmt.Sprintf("%d reminder(s) on %s were cancelled", n, in.Card))
	}
	remindReply(w, map[string]any{"removed": n})
}

// Pass brings every reminder that is due. A growler already raised for one is not raised again, so a pass that stopped
// half way, or a restart, is safe to run over.
func (h *Reminders) Pass(ctx context.Context) {
	due, err := h.Store.ReminderDue()
	if err != nil || len(due) == 0 {
		return
	}
	g := h.Growler()
	if g == nil {
		return
	}
	for _, r := range due {
		room, id, ok := hubstore.SplitEventCard(r.Card)
		if !ok {
			_ = h.Store.ReminderFire(r.ID)
			continue
		}
		row := GrowlRow{ID: "remind|" + r.ID, CardID: id, Reason: notify.ReasonReminder,
			Title: "Reminder: " + notify.Clip(r.Text), Body: r.Text}
		if !g.Remind(room, row, id) {
			continue
		}
		if r.Wake && !h.wake(ctx, r, room, id) {
			continue
		}
		if err := h.Store.ReminderFire(r.ID); err == nil {
			h.audit("remind-fired", "the reminder on "+r.Card+" came due")
		}
	}
}

// wake says the text to the card and reports whether that is finished: delivered, refused, or given up on after a day.
func (h *Reminders) wake(ctx context.Context, r hubstore.Reminder, room, id string) bool {
	if h.Deliver == nil {
		return true
	}
	a := h.Deliver.Send(ctx, CITarget{Card: r.Card, Room: room, ID: id}, remindFrom,
		"Reminder from atrium: "+r.Text, "fyi", true)
	if a.State == hubstore.CIPending {
		return h.now().Sub(r.DueAt) > remindWakeGiveUp
	}
	return true
}

// Run passes on a ticker until ctx ends.
func (h *Reminders) Run(ctx context.Context) {
	h.Pass(ctx)
	t := time.NewTicker(remindTick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			h.Pass(ctx)
			_ = h.Store.ReminderPrune()
		}
	}
}

// SetReminders turns reminders on for this hub. A hub that never calls it answers /_hub/remind 404.
func (p *Proxy) SetReminders(h *Reminders) {
	p.mu.Lock()
	p.remind = h
	p.mu.Unlock()
}

// NewReminders is the hub's reminders with its growler, its say path and its audit wired.
func (p *Proxy) NewReminders(st *hubstore.Store) *Reminders {
	return &Reminders{Store: st, Growler: p.growler, Deliver: p.Deliverer(),
		Audit: func(kind, detail string) { p.RecordAudit("", kind, detail) }}
}

func (p *Proxy) serveRemind(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	h := p.remind
	p.mu.Unlock()
	if h == nil {
		http.NotFound(w, r)
		return
	}
	h.Serve(w, r)
}
