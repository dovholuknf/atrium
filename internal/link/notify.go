package link

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// The hub's notify trigger and its one sink, an operator-written command.
//
// ── what this is for ────────────────────────────────────
//
// Somebody minimised the browser and wants a phone to buzz when a card needs
// them. Rooms already announce their whole card list to the hub within two
// seconds of a change, so the hub is the one place that sees every room. After
// each announcement it works out, per card, WHY a human is wanted (if at all),
// compares that with what it last acted on, and hands only the changes to a sink.
//
// The sink is an interface with one implementation, because Web Push is meant to
// arrive later as a second one behind the same trigger.
//
// ── the credential rule ─────────────────────────────────
//
// ATRIUM HOLDS THE NAME OF A COMMAND AND NEVER A CREDENTIAL. The command is an
// argv the operator wrote, run with no shell. Whatever token it needs to reach
// ntfy, a Signal bot or anything else lives in the operator's own script, on the
// operator's own disk. Nothing atrium stores is somebody else's secret, which is
// the line docs/fabric/overlays.md draws and CLAUDE.md repeats.
//
// ── what the command is told ────────────────────────────
//
// FOUR FIELDS AND NO MORE: the card's name, why it wants a human, the card id
// (`room~id`) and the room. In the environment as ATRIUM_NOTIFY_NAME, _REASON,
// _CARD and _ROOM, and as one JSON line on stdin. NEVER IN ARGV. A card's name is
// text a model or an operator wrote, and argv is where a shell, or a program
// that parses flags, would act on it. Never the command a permission wants to
// run and never a recap: this leaves the machine on its way to a phone.
//
// ── never delaying anything ─────────────────────────────
//
// FIRE AND FORGET. The announcement is acknowledged to the room before the sink
// is ever considered, the sink runs on one worker goroutine fed by a bounded
// queue, and a full queue drops its OLDEST entry and counts the drop. A card, a
// hook and the permission chain are never slower for this, whatever the command
// does.
//
// ── failure is a state, reported ────────────────────────
//
// Exit 0 is ok. Anything else, or a timeout, is a failure recorded with the
// first line of stderr, and three in a row switch the feature off with the
// reason attached, as a source does. The reason is persisted, so a restart
// cannot make a broken command look like a working one.

// Notify settings, in the hub_setting table.
const (
	settingNotifyEnabled  = "notify.enabled"
	settingNotifyCommand  = "notify.command"
	settingNotifyDisabled = "notify.disabled_reason"
	settingNotifySeeded   = "notify.seeded."
)

// Notify reasons.
const (
	ReasonPermission = "permission"
	ReasonQuestion   = "question"
	ReasonInput      = "input"
	ReasonFinished   = "finished"
)

const (
	notifyQueueMax    = 32
	notifyMaxFailures = 3
	notifyStdoutLimit = 16 << 10
	notifyStderrLimit = 8 << 10
	tabBackstop       = 10 * time.Minute
	// streamGrace is how long a tab whose event stream dropped still counts as
	// visible, which covers the board reconnecting its stream.
	streamGrace        = 30 * time.Second
	notifyPruneEvery   = time.Hour
	notifyTabIDMaxSize = 128
)

// notifyTimeout is a variable so a test can prove a timeout kills the process
// without waiting ten seconds.
var notifyTimeout = 10 * time.Second

// Notice is what a sink is given. These four fields are the whole payload.
type Notice struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
	Card   string `json:"card"`
	Room   string `json:"room"`
}

// Result is one sink run.
type Result struct {
	OK       bool   `json:"ok"`
	ExitCode int    `json:"exit_code"`
	Output   string `json:"output"`
	TookMS   int64  `json:"took_ms"`
	// Err is the one-line reason for a failure. Not part of the test answer.
	Err string `json:"-"`
}

// Sink is somewhere a notice goes. Wave 1 has one, the operator's command.
type Sink interface {
	Send(ctx context.Context, n Notice) Result
}

// NotifyStore is what the notifier needs from the hub's database, by room NAME
// so this package never learns room ids.
type NotifyStore interface {
	Setting(name string) (string, error)
	SetSetting(name, value string) error
	// Record stores the identities and returns the card ids that changed. See
	// hubstore.Store.NotifyRecord.
	Record(room string, ids map[string]string, present []string, silent bool) ([]string, error)
	// Rooms and Cards let turning the feature on seed from what is cached.
	Rooms() ([]string, error)
	Cards(room string) ([]CardState, error)
	Prune() error
}

// ── identity ────────────────────────────────────────────

// notifyCard is one card's current reason to notify.
type notifyCard struct {
	ID       string
	Name     string
	Reason   string
	Identity string
	// At is the timestamp that made it a reason, the last part of Identity.
	At string
	// Agent is an origin:agent card. NotifyIdentity never returns one, and the
	// growler takes one for a permission only.
	Agent bool
	// Questions are the open questions, for a question.
	Questions []string
	// Report is `blocked` or `question` when the card is waiting because its
	// session said so with atrium_report, and Ask is what it asked for. The
	// notifier reads neither field, and a reported card notifies as input. That
	// includes one whose first turn has not ended, which said nothing before:
	// a session that reported blocked is waiting on somebody.
	Report, Ask string
}

// notifySeen is the part of a card's seen row the notifier reads.
type notifySeen struct {
	QuestionsAt       string   `json:"questions_at"`
	OpenQuestions     []string `json:"open_questions"`
	QuestionsUnparsed bool     `json:"questions_unparsed"`
	TurnEndedAt       string   `json:"turn_ended_at"`
	Unseen            bool     `json:"unseen"`
}

// NotifyIdentity works out why a card wants a human right now, from the opaque
// payload the room announced. ok is false when it does not, or when the card is
// not the operator's business.
//
// ONE REASON PER CARD, BY A FIXED PRIORITY: permission, then question, then
// input, then finished. A card can be several at once (a question asked while it
// waits for input) and the most demanding one is the one worth a buzz. The
// identity is the card id, the reason and the timestamp that made it a reason,
// so the same waiting spell republished is one identity and a second spell is
// another.
//
// origin:agent cards are skipped: an agent started another agent, and the
// operator is told about the one that started it (item 44).
func NotifyIdentity(id string, payload json.RawMessage) (notifyCard, bool) {
	nc, ok := cardReason(id, payload)
	if !ok || nc.Agent {
		return notifyCard{}, false
	}
	return nc, true
}

// cardReason is NotifyIdentity without the origin:agent skip, which is the
// caller's to apply. The growler needs the difference: a blocked agent is
// frozen whoever launched it, so its permission still growls.
func cardReason(id string, payload json.RawMessage) (notifyCard, bool) {
	// THE SEEN FIELDS ARE UNDER `seen`, the room's seen row as /v1/state sends
	// it beside the stored card (internal/api/state.go). They were read at the
	// top level, where no payload ever carried them, so a question and a finished
	// turn never notified (f-023). A room on an older build sends no `seen`, and
	// its cards still notify for permission and input.
	var p struct {
		Title        string   `json:"title"`
		Alias        string   `json:"alias"`
		Status       string   `json:"status"`
		Tags         []string `json:"tags"`
		WaitingSince string   `json:"waiting_since"`
		LastActivity string   `json:"last_activity_at"`
		// WHY IT WAITS, which a report sets to its status: `blocked` or
		// `question` (internal/daemon/finish.go). The stored card has always
		// carried it, so the hub reads it with no room change.
		WaitingReason string `json:"waiting_reason"`
		Recap         string `json:"recap"`
		// A pointer, so a room that sent no seen row at all (an older build) is
		// told apart from a card whose seen row says it never finished a turn.
		Seen *notifySeen `json:"seen"`
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		return notifyCard{}, false
	}
	sent := p.Seen != nil
	if !sent {
		p.Seen = &notifySeen{}
	}
	// The same match the control tools use, trimmed and case-insensitive, so a
	// tag written ` Origin:Agent ` is skipped here as it is there.
	agent := hasOriginTag(p.Tags)
	waited := p.WaitingSince
	if waited == "" {
		waited = p.LastActivity
	}
	report, ask := "", ""
	if p.Status == "needs-input" && (p.WaitingReason == "blocked" || p.WaitingReason == ReasonQuestion) {
		report = p.WaitingReason
		// The report's ask is on the recap after `needs: `. See finish.go.
		if i := strings.LastIndex(p.Recap, "needs: "); i >= 0 {
			ask = strings.TrimSpace(p.Recap[i+len("needs: "):])
		}
	}
	var reason, at string
	switch {
	case p.Status == "needs-permission":
		reason, at = ReasonPermission, waited
	case p.Seen.QuestionsAt != "" && (len(p.Seen.OpenQuestions) > 0 || p.Seen.QuestionsUnparsed):
		reason, at = ReasonQuestion, p.Seen.QuestionsAt
	// A CARD THAT HAS NEVER FINISHED A TURN IS NOT WAITING ON ANYBODY NEW.
	// Whoever launched it is at it already or handed it its prompt, so an
	// `input` for a session that has only just started is noise
	// (atrium-87300, 2026-09-30). Only where the room sent its seen row: an
	// older room cannot say, and keeps notifying as before.
	// A session that REPORTED is waiting on somebody, finished turn or not.
	case p.Status == "needs-input" && sent && p.Seen.TurnEndedAt == "" && report == "":
		return notifyCard{}, false
	case p.Status == "needs-input":
		reason, at = ReasonInput, waited
	case p.Seen.TurnEndedAt != "" && p.Seen.Unseen:
		reason, at = ReasonFinished, p.Seen.TurnEndedAt
	default:
		return notifyCard{}, false
	}
	name := strings.TrimSpace(p.Alias)
	if name == "" {
		name = strings.TrimSpace(p.Title)
	}
	if name == "" {
		name = id
	}
	return notifyCard{ID: id, Name: name, Reason: reason, Identity: id + "|" + reason + "|" + at,
		At: at, Agent: agent, Questions: p.Seen.OpenQuestions, Report: report, Ask: ask}, true
}

// ── presence ────────────────────────────────────────────

// presence is which desktop board tabs are visible right now.
//
// EDGE TRIGGERED, NO HEARTBEAT. A tab says so when it becomes visible or hidden
// and never otherwise. What keeps that honest is the event stream: a tab opens
// its stream with `tab=<id>`, and when THAT request ends the tab is dropped
// after streamGrace, so a crashed tab or a sleeping laptop clears itself and a
// stream that merely reconnected does not. The backstop is for a tab the hub can
// tie to no stream at all: it expires ten minutes after its last `visible: true`.
type presence struct {
	mu      sync.Mutex
	visible map[string]time.Time
	streams map[string]int
	// ended is when a visible tab's last stream closed. See Open.
	ended map[string]time.Time
	now   func() time.Time
}

func newPresence() *presence {
	return &presence{visible: map[string]time.Time{}, streams: map[string]int{},
		ended: map[string]time.Time{}, now: time.Now}
}

// validTab keeps the id a short plain token: it is a map key and nothing else.
func validTab(tab string) bool {
	if tab == "" || len(tab) > notifyTabIDMaxSize {
		return false
	}
	for _, r := range tab {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
		default:
			return false
		}
	}
	return true
}

// Set is a tab reporting a change.
func (p *presence) Set(tab string, visible bool) {
	if !validTab(tab) {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if visible {
		p.visible[tab] = p.now()
		// Said again, so it is visible now whatever its old stream did.
		delete(p.ended, tab)
	} else {
		delete(p.visible, tab)
		delete(p.ended, tab)
	}
}

// Open notes a tab's event stream starting. It returns the function that ends it.
func (p *presence) Open(tab string) func() {
	if !validTab(tab) {
		return func() {}
	}
	p.mu.Lock()
	p.streams[tab]++
	delete(p.ended, tab)
	p.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			p.mu.Lock()
			defer p.mu.Unlock()
			if p.streams[tab]--; p.streams[tab] <= 0 {
				delete(p.streams, tab)
				// ITS STREAM ENDED, SO THE TAB IS GOING, BUT NOT YET. A stream
				// that drops and reconnects is the same tab, and it does not post
				// `visible` again, so deleting it here left a watched board
				// counted as unwatched for good. It keeps its visibility for
				// streamGrace. A reconnect inside that holds it, and a tab that
				// really went is dropped when the grace runs out.
				if _, seen := p.visible[tab]; seen {
					p.ended[tab] = p.now()
				}
			}
		})
	}
}

// Count is how many tabs are visible now.
func (p *presence) Count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := 0
	for tab, at := range p.visible {
		ended, dropped := p.ended[tab]
		switch {
		case p.streams[tab] > 0:
			n++
		case dropped && p.now().Sub(ended) < streamGrace:
			// Its stream dropped a moment ago, and a reconnect is expected.
			n++
		case !dropped && p.now().Sub(at) < tabBackstop:
			// A tab the hub never tied to a stream, on the ten minute backstop.
			n++
		default:
			delete(p.visible, tab)
			delete(p.ended, tab)
		}
	}
	return n
}

// ── the sink: a command ─────────────────────────────────

// CommandSink runs an operator-written argv, with no shell.
type CommandSink struct {
	// Argv is read on every send, so a changed command applies at once.
	Argv func() []string
}

// Send runs the command once, bounded the way a source is: a timeout with the
// process killed, and output capped WHILE it is read.
func (c CommandSink) Send(ctx context.Context, n Notice) Result {
	began := time.Now()
	argv := c.Argv()
	res := Result{ExitCode: -1}
	done := func() Result { res.TookMS = time.Since(began).Milliseconds(); return res }
	if len(argv) == 0 || strings.TrimSpace(argv[0]) == "" {
		res.Err = "no command is set"
		res.Output = res.Err
		return done()
	}
	runCtx, cancel := context.WithTimeout(ctx, notifyTimeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, argv[0], argv[1:]...)
	hideWindow(cmd)
	line, _ := json.Marshal(n)
	// A NUL CANNOT BE IN AN ENVIRONMENT VALUE, and exec refuses the whole run
	// for one, which three times over switched notify off because of a card's
	// name. So the environment drops it, and stdin, which is JSON and escapes
	// it, still carries the name whole.
	noNUL := func(s string) string { return strings.ReplaceAll(s, "\x00", "") }
	cmd.Env = append(os.Environ(),
		"ATRIUM_NOTIFY_NAME="+noNUL(n.Name),
		"ATRIUM_NOTIFY_REASON="+noNUL(n.Reason),
		"ATRIUM_NOTIFY_CARD="+noNUL(n.Card),
		"ATRIUM_NOTIFY_ROOM="+noNUL(n.Room),
	)
	cmd.Stdin = bytes.NewReader(append(line, '\n'))
	// Bounded WHILE reading, not after: see readSource in internal/daemon.
	var out, errOut bytes.Buffer
	cmd.Stdout = &limitedWriter{w: &out, left: notifyStdoutLimit}
	cmd.Stderr = &limitedWriter{w: &errOut, left: notifyStderrLimit}
	// A grandchild holding the pipes open would otherwise outlive the kill.
	cmd.WaitDelay = 2 * time.Second

	err := cmd.Run()
	res.Output = strings.TrimSpace(out.String())
	if e := strings.TrimSpace(errOut.String()); e != "" {
		if res.Output != "" {
			res.Output += "\n"
		}
		res.Output += e
	}
	switch {
	case runCtx.Err() == context.DeadlineExceeded:
		res.Err = fmt.Sprintf("took longer than %s and was stopped", notifyTimeout)
	case err == nil:
		res.OK, res.ExitCode = true, 0
	default:
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			res.ExitCode = ee.ExitCode()
		}
		res.Err = err.Error()
		if first := firstLineOf(errOut.String()); first != "" {
			res.Err = first
		}
	}
	return done()
}

func firstLineOf(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	return s
}

// limitedWriter takes at most `left` bytes and tells the child everything
// landed, so the child is not the one that finds out about the limit.
type limitedWriter struct {
	w    io.Writer
	left int
}

func (l *limitedWriter) Write(p []byte) (int, error) {
	if l.left <= 0 {
		return len(p), nil
	}
	take := p
	if len(take) > l.left {
		take = take[:l.left]
	}
	n, err := l.w.Write(take)
	l.left -= n
	if err != nil {
		return n, err
	}
	return len(p), nil
}

// ── the notifier ────────────────────────────────────────

// NotifyStatus is `GET /_hub/notify`.
type NotifyStatus struct {
	Enabled        bool     `json:"enabled"`
	Command        []string `json:"command"`
	LastRunAt      *string  `json:"last_run_at"`
	LastOKAt       *string  `json:"last_ok_at"`
	LastError      string   `json:"last_error"`
	Failures       int      `json:"failures"`
	DisabledReason string   `json:"disabled_reason"`
	Sent           int      `json:"sent"`
	Dropped        int      `json:"dropped"`
	// Suppressed says a desktop tab is visible right now, so a changed identity
	// is stored and the command does not run. VisibleTabs is how many.
	Suppressed  bool `json:"suppressed"`
	VisibleTabs int  `json:"visible_tabs"`
}

// Notifier is the trigger, the queue and the worker.
type Notifier struct {
	st   NotifyStore
	sink Sink
	pres *presence

	mu       sync.Mutex
	enabled  bool
	command  []string
	disabled string
	failures int
	lastRun  time.Time
	lastOK   time.Time
	lastErr  string
	sent     int
	dropped  int
	seeded   map[string]bool
	queue    []Notice
	wake     chan struct{}
	pruned   time.Time
}

// NewNotifier reads its settings and returns a notifier with the command sink.
// Call Start to run the worker.
func NewNotifier(st NotifyStore) *Notifier {
	n := &Notifier{st: st, pres: newPresence(), seeded: map[string]bool{}, wake: make(chan struct{}, 1)}
	n.sink = CommandSink{Argv: func() []string {
		n.mu.Lock()
		defer n.mu.Unlock()
		return append([]string(nil), n.command...)
	}}
	if v, _ := st.Setting(settingNotifyEnabled); v == "on" {
		n.enabled = true
	}
	if v, _ := st.Setting(settingNotifyCommand); v != "" {
		_ = json.Unmarshal([]byte(v), &n.command)
	}
	n.disabled, _ = st.Setting(settingNotifyDisabled)
	return n
}

// SetSink swaps the sink, which is how a second one will be added. For tests
// too.
func (n *Notifier) SetSink(s Sink) { n.sink = s }

// Start runs the one worker goroutine until ctx ends.
func (n *Notifier) Start(ctx context.Context) {
	go n.work(ctx)
}

// Presence is the tab tracker, for the event stream and the endpoint.
func (n *Notifier) Presence() *presence { return n.pres }

func (n *Notifier) work(ctx context.Context) {
	for {
		notice, ok := n.pop()
		if !ok {
			select {
			case <-ctx.Done():
				return
			case <-n.wake:
			}
			continue
		}
		n.mu.Lock()
		on := n.enabled
		n.mu.Unlock()
		if !on {
			continue
		}
		res := n.sink.Send(ctx, notice)
		n.record(res)
	}
}

func (n *Notifier) pop() (Notice, bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if len(n.queue) == 0 {
		return Notice{}, false
	}
	x := n.queue[0]
	n.queue = n.queue[1:]
	return x, true
}

// enqueue never blocks. A full queue drops its oldest and counts it.
func (n *Notifier) enqueue(x Notice) {
	n.mu.Lock()
	if len(n.queue) >= notifyQueueMax {
		n.queue = n.queue[1:]
		n.dropped++
	}
	n.queue = append(n.queue, x)
	n.mu.Unlock()
	select {
	case n.wake <- struct{}{}:
	default:
	}
}

// record is the outcome of one real notification.
func (n *Notifier) record(res Result) {
	n.mu.Lock()
	n.lastRun = time.Now()
	if res.OK {
		n.lastOK, n.lastErr, n.failures = n.lastRun, "", 0
		n.sent++
		n.mu.Unlock()
		return
	}
	n.lastErr = res.Err
	n.failures++
	trip := n.failures >= notifyMaxFailures && n.enabled
	if trip {
		n.enabled = false
		n.disabled = fmt.Sprintf("%d failures in a row, the last: %s", n.failures, res.Err)
	}
	reason := n.disabled
	n.mu.Unlock()
	if trip {
		_ = n.st.SetSetting(settingNotifyEnabled, "off")
		_ = n.st.SetSetting(settingNotifyDisabled, reason)
		log.Printf("[hub] the notify command is switched off: %s", reason)
	}
}

// Announced is called after a room's announcement is safely cached. It costs a
// few map lookups and one database transaction and never waits on the sink.
func (n *Notifier) Announced(room string, cards []CardState) {
	n.mu.Lock()
	on := n.enabled
	n.mu.Unlock()
	if !on {
		return
	}
	ids, info, present := notifyPlan(cards)
	first := !n.isSeeded(room)
	fire, err := n.st.Record(room, ids, present, first)
	if err != nil {
		log.Printf("[hub] notify could not record %q: %v", room, err)
		return
	}
	if first {
		n.markSeeded(room)
		return
	}
	n.maybePrune()
	if len(fire) == 0 {
		return
	}
	// WHILE A DESKTOP TAB IS VISIBLE the identity is stored (Record did that) and
	// the sink does not run: the operator is looking at the board.
	if n.pres.Count() > 0 {
		return
	}
	for _, id := range fire {
		c := info[id]
		n.enqueue(Notice{Name: c.Name, Reason: c.Reason, Card: tagFor(room, id), Room: room})
	}
}

// Remind is a growler reminding, sent to the sink like a change would be: held
// back while a desktop tab is visible, and dropped by the worker while notify
// is off. See growl.go.
func (n *Notifier) Remind(x Notice) {
	if n.pres.Count() > 0 {
		return
	}
	n.enqueue(x)
}

func notifyPlan(cards []CardState) (ids map[string]string, info map[string]notifyCard, present []string) {
	ids, info = map[string]string{}, map[string]notifyCard{}
	for _, c := range cards {
		present = append(present, c.ID)
		if nc, ok := NotifyIdentity(c.ID, c.Payload); ok {
			ids[c.ID] = nc.Identity
			info[c.ID] = nc
		}
	}
	return ids, info, present
}

func (n *Notifier) isSeeded(room string) bool {
	key := strings.ToLower(room)
	n.mu.Lock()
	if n.seeded[key] {
		n.mu.Unlock()
		return true
	}
	n.mu.Unlock()
	v, _ := n.st.Setting(settingNotifySeeded + key)
	if v == "" {
		return false
	}
	n.mu.Lock()
	n.seeded[key] = true
	n.mu.Unlock()
	return true
}

func (n *Notifier) markSeeded(room string) {
	key := strings.ToLower(room)
	n.mu.Lock()
	n.seeded[key] = true
	n.mu.Unlock()
	_ = n.st.SetSetting(settingNotifySeeded+key, time.Now().UTC().Format(time.RFC3339))
}

// unseedAll forgets which rooms were seeded, in memory and in the store. Best
// effort: a room it misses is at worst the flood this exists to prevent.
func (n *Notifier) unseedAll() {
	n.mu.Lock()
	n.seeded = map[string]bool{}
	n.mu.Unlock()
	rooms, err := n.st.Rooms()
	if err != nil {
		return
	}
	for _, room := range rooms {
		_ = n.st.SetSetting(settingNotifySeeded+strings.ToLower(room), "")
	}
}

func (n *Notifier) maybePrune() {
	n.mu.Lock()
	if time.Since(n.pruned) < notifyPruneEvery {
		n.mu.Unlock()
		return
	}
	n.pruned = time.Now()
	n.mu.Unlock()
	_ = n.st.Prune()
}

// seed stores every room's cached identities without notifying, which is what
// turning the feature on does so it does not announce everything already
// waiting.
func (n *Notifier) seed() error {
	rooms, err := n.st.Rooms()
	if err != nil {
		return err
	}
	for _, room := range rooms {
		cards, err := n.st.Cards(room)
		if err != nil {
			return err
		}
		if len(cards) == 0 {
			continue
		}
		ids, _, present := notifyPlan(cards)
		if _, err := n.st.Record(room, ids, present, true); err != nil {
			return err
		}
		n.markSeeded(room)
	}
	return nil
}

// Status is the GET answer.
func (n *Notifier) Status() NotifyStatus {
	n.mu.Lock()
	defer n.mu.Unlock()
	s := NotifyStatus{
		Enabled: n.enabled, Command: append([]string{}, n.command...), LastError: n.lastErr,
		Failures: n.failures, DisabledReason: n.disabled, Sent: n.sent, Dropped: n.dropped,
	}
	if !n.lastRun.IsZero() {
		v := n.lastRun.UTC().Format(time.RFC3339)
		s.LastRunAt = &v
	}
	if !n.lastOK.IsZero() {
		v := n.lastOK.UTC().Format(time.RFC3339)
		s.LastOKAt = &v
	}
	s.VisibleTabs = n.pres.Count()
	s.Suppressed = s.VisibleTabs > 0
	return s
}

// commandFound is the PUT check: an argv[0] that will actually run.
func commandFound(argv []string) error {
	if len(argv) == 0 {
		return errors.New("the command must be a non-empty array of strings")
	}
	for _, a := range argv {
		if strings.ContainsRune(a, 0) {
			return errors.New("the command holds a NUL byte")
		}
	}
	first := strings.TrimSpace(argv[0])
	if first == "" {
		return errors.New("the command's first element is empty")
	}
	if filepath.IsAbs(first) {
		if st, err := os.Stat(first); err != nil || st.IsDir() {
			return fmt.Errorf("%s does not exist or is a directory", first)
		}
		return nil
	}
	if _, err := exec.LookPath(first); err != nil {
		return fmt.Errorf("%s was not found on the hub's PATH", first)
	}
	return nil
}

// Configure applies a PUT: validate, persist, and seed on the way from off to on.
func (n *Notifier) Configure(enabled bool, command []string) error {
	// CHECKED ONLY WHEN IT IS GOING TO RUN. A command that has since gone from
	// the PATH must not stop notify being turned OFF, which is what somebody does
	// about a command that stopped working. Turning it on checks it again.
	if enabled {
		if err := commandFound(command); err != nil {
			return err
		}
	}
	raw, _ := json.Marshal(command)
	if command == nil {
		raw = []byte("[]")
	}
	n.mu.Lock()
	was := n.enabled
	n.mu.Unlock()
	if enabled && !was {
		// EVERY ROOM STARTS UNSEEDED AGAIN. Nothing is recorded while notify is
		// off, so a room seeded last time would otherwise have everything that
		// piled up since announced at once on its next change. seed() below
		// marks the rooms it can, and any other room is seeded silently by its
		// first announcement.
		n.unseedAll()
		if err := n.seed(); err != nil {
			return fmt.Errorf("could not seed what is already waiting: %w", err)
		}
	}
	if err := n.st.SetSetting(settingNotifyCommand, string(raw)); err != nil {
		return err
	}
	on := "off"
	if enabled {
		on = "on"
	}
	if err := n.st.SetSetting(settingNotifyEnabled, on); err != nil {
		return err
	}
	n.mu.Lock()
	n.command = append([]string(nil), command...)
	n.enabled = enabled
	if enabled {
		n.disabled, n.failures = "", 0
	}
	n.mu.Unlock()
	if enabled {
		_ = n.st.SetSetting(settingNotifyDisabled, "")
	}
	return nil
}

// Test runs the command once, now, with a test card. Presence does not suppress
// it and it does not count toward the three failures.
func (n *Notifier) Test(ctx context.Context) (Result, error) {
	n.mu.Lock()
	have := len(n.command) > 0
	n.mu.Unlock()
	if !have {
		return Result{}, errors.New("no command is set")
	}
	res := n.sink.Send(ctx, Notice{Name: "atrium test", Reason: "test", Card: "test~test", Room: "test"})
	n.mu.Lock()
	n.lastRun = time.Now()
	if res.OK {
		n.lastOK, n.lastErr = n.lastRun, ""
	} else {
		n.lastErr = res.Err
	}
	n.mu.Unlock()
	return res, nil
}
