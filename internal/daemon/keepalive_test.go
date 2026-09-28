package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// The keep-alive with a fake fork, a fake clock and a hand-written transcript.
// Nothing here starts claude.

type kaFix struct {
	t      *testing.T
	st     *store.Store
	k      *keepalive
	task   *store.Task
	now    time.Time
	path   string
	mu     sync.Mutex
	specs  []forkSpec
	answer func() ([]byte, error)
	events []map[string]any
}

// receipt builds a fork's JSON answer.
func receipt(turns, denials int, subtype string, read, write int64) []byte {
	d := make([]json.RawMessage, denials)
	for i := range d {
		d[i] = json.RawMessage(`{"tool_name":"Read"}`)
	}
	b, _ := json.Marshal(map[string]any{
		"subtype": subtype, "num_turns": turns, "session_id": "fork-session-1",
		"permission_denials": d,
		"usage": map[string]any{
			"input_tokens": 2, "cache_creation_input_tokens": write,
			"cache_read_input_tokens": read, "output_tokens": 4,
		},
	})
	return b
}

func newKAFix(t *testing.T) *kaFix {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.ToSlash(filepath.Join(dir, "atrium.db")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	wt := filepath.Join(dir, "wt")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	task, _, err := st.Register(store.Observed{WireName: "ka-card", Worktree: filepath.ToSlash(wt), Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetResumeID(task.ID, "card-session-1"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetStatus(task.ID, store.StatusNeedsInput); err != nil {
		t.Fatal(err)
	}
	f := &kaFix{t: t, st: st, now: time.Now().UTC().Truncate(time.Second).Add(time.Hour),
		path: filepath.Join(dir, "card-session-1.jsonl")}
	// Switched on at launch, long before any reply in these tests.
	if _, err := st.SetKeepaliveStateAt(task.ID, store.KeepaliveOn, f.now.Add(-48*time.Hour)); err != nil {
		t.Fatal(err)
	}
	f.answer = func() ([]byte, error) { return receipt(1, 0, "success", 297_000, 1_000), nil }
	k := newKeepalive(st)
	k.now = func() time.Time { return f.now }
	k.transcript = func(cwd, id string) string {
		if id == "card-session-1" {
			return f.path
		}
		return ""
	}
	k.hookFile = filepath.Join(dir, "block.json")
	k.baseEnv = func() []string {
		return []string{"PATH=/bin", "ATRIUM_TASK_ID=someone-else", "ATRIUM_PERM_GATE=on", "CLAUDECODE=1"}
	}
	k.fork = func(ctx context.Context, spec forkSpec) ([]byte, error) {
		f.mu.Lock()
		f.specs = append(f.specs, spec)
		f.mu.Unlock()
		return f.answer()
	}
	k.broadcast = func(event string, payload any) {
		if m, ok := payload.(map[string]any); ok && event == "keepalive" {
			f.mu.Lock()
			f.events = append(f.events, m)
			f.mu.Unlock()
		}
	}
	f.k = k
	f.task, _ = st.Get(task.ID)
	return f
}

type replyOpt struct {
	model string
	ctx   int64
	speed string
	ttl5m bool
}

// reply appends one main assistant reply to the card's transcript.
func (f *kaFix) reply(at time.Time, o replyOpt) {
	f.t.Helper()
	if o.model == "" {
		o.model = "claude-opus-5-5"
	}
	if o.ctx == 0 {
		o.ctx = 300_000
	}
	cc := map[string]any{"ephemeral_1h_input_tokens": 500, "ephemeral_5m_input_tokens": 0}
	if o.ttl5m {
		cc = map[string]any{"ephemeral_1h_input_tokens": 0, "ephemeral_5m_input_tokens": 500}
	}
	line, _ := json.Marshal(map[string]any{
		"type": "assistant", "timestamp": at.Format(time.RFC3339Nano),
		"message": map[string]any{"model": o.model, "usage": map[string]any{
			"input_tokens": 2, "cache_creation_input_tokens": 500,
			"cache_read_input_tokens": o.ctx - 502, "output_tokens": 30,
			"speed": o.speed, "cache_creation": cc,
		}},
	})
	fh, err := os.OpenFile(f.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		f.t.Fatal(err)
	}
	defer fh.Close()
	fmt.Fprintln(fh, string(line))
}

func (f *kaFix) tick() {
	f.task, _ = f.st.Get(f.task.ID)
	f.k.tick(context.Background())
}

func (f *kaFix) forks() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.specs)
}

func (f *kaFix) state() string {
	c, err := f.st.KeepaliveCardOf(f.task.ID)
	if err != nil || c == nil {
		return ""
	}
	return c.State
}

func (f *kaFix) ledger() []*store.KeepaliveRefresh {
	rows, err := f.st.KeepaliveRefreshesSince(f.task.ID, time.Time{})
	if err != nil {
		f.t.Fatal(err)
	}
	return rows
}

func (f *kaFix) toasts() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, e := range f.events {
		if s, ok := e["toast"].(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// A warm, idle card inside the margin is refreshed with exactly the guarded
// command line, in its own directory, with atrium's hooks off.
func TestKeepaliveRefreshesAnIdleCardInsideTheMargin(t *testing.T) {
	f := newKAFix(t)
	f.reply(f.now.Add(-56*time.Minute), replyOpt{})
	f.tick()
	if f.forks() != 1 {
		t.Fatalf("forks = %d, want 1", f.forks())
	}
	spec := f.specs[0]
	want := []string{
		"-p", keepalivePrompt, "--resume", "card-session-1", "--fork-session", "--no-session-persistence",
		"--model", "claude-opus-5-5", "--setting-sources", "local", "--settings", f.k.hookFile,
		"--max-turns", "1", "--output-format", "json",
	}
	if strings.Join(spec.Args, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("args:\n got %q\nwant %q", spec.Args, want)
	}
	if spec.Dir != f.task.Worktree {
		t.Fatalf("dir = %q, want the card's worktree %q", spec.Dir, f.task.Worktree)
	}
	env := strings.Join(spec.Env, "\n")
	for _, must := range []string{"ATRIUM_PERM_GATE=off", "CLAUDE_CODE_PROMPT_CACHE_TTL=1h", "PATH=/bin"} {
		if !strings.Contains(env, must) {
			t.Fatalf("fork env lacks %s:\n%s", must, env)
		}
	}
	for _, mustNot := range []string{"ATRIUM_TASK_ID=", "ATRIUM_PERM_GATE=on", "CLAUDECODE="} {
		if strings.Contains(env, mustNot) {
			t.Fatalf("fork env carries %s:\n%s", mustNot, env)
		}
	}
	b, err := os.ReadFile(f.k.hookFile)
	if err != nil || !strings.Contains(string(b), `"PreToolUse"`) || !strings.Contains(string(b), "exit 2") {
		t.Fatalf("block-all hook file not written: %v %s", err, b)
	}
	rows := f.ledger()
	if len(rows) != 1 || rows[0].Outcome != outcomeWarmed {
		t.Fatalf("ledger = %+v, want one warmed row", rows)
	}
	r := rows[0]
	if r.ResumeID != "card-session-1" || r.ForkSession != "fork-session-1" || r.Context != 300_000 {
		t.Fatalf("the row mixed up the card and the fork: %+v", r)
	}
	if r.Prices != keepalivePricesVersion || r.Cost <= 0 {
		t.Fatalf("row not priced: %+v", r)
	}
	if f.state() != store.KeepaliveOn {
		t.Fatalf("state = %s, want on", f.state())
	}
	// The refresh moved the warm window, so the next tick has nothing to do.
	f.tick()
	if f.forks() != 1 {
		t.Fatal("a second refresh fired right after a warmed one")
	}
}

// Each rule blocks a refresh on its own.
// A paid fork whose row will not save must not fork again on the next ticks,
// though the saved rows still show the old expiry. A real turn lifts the hold.
func TestKeepaliveUnsavedRefreshDoesNotForkAgain(t *testing.T) {
	f := newKAFix(t)
	f.k.record = func(*store.KeepaliveRefresh) error { return store.ErrClosed }
	f.reply(f.now.Add(-56*time.Minute), replyOpt{})
	f.tick()
	if f.forks() != 1 {
		t.Fatalf("forks = %d, want 1", f.forks())
	}
	for i := 0; i < 3; i++ {
		f.now = f.now.Add(time.Minute)
		f.tick()
	}
	if f.forks() != 1 {
		t.Fatalf("forks = %d after three more ticks, want 1", f.forks())
	}
	v, _ := f.k.view(f.task.ID).(*keepaliveCardView)
	if v == nil || !strings.Contains(v.Why, "could not be saved") {
		t.Fatalf("view = %+v, want the unsaved reason", v)
	}
	f.k.record = f.st.AddKeepaliveRefresh
	f.reply(f.now, replyOpt{})
	f.now = f.now.Add(56 * time.Minute)
	f.tick()
	if f.forks() != 2 {
		t.Fatalf("forks = %d after a real turn, want 2", f.forks())
	}
}

// Turning the switch on by hand lifts the hold from an unsaved refresh.
func TestKeepaliveHandOnLiftsTheUnsavedHold(t *testing.T) {
	f := newKAFix(t)
	f.k.record = func(*store.KeepaliveRefresh) error { return store.ErrClosed }
	f.reply(f.now.Add(-56*time.Minute), replyOpt{})
	f.tick()
	f.k.record = f.st.AddKeepaliveRefresh
	f.now = f.now.Add(time.Minute)
	d := &Daemon{st: f.st, ka: f.k}
	if _, err := d.keepaliveSet(f.task.ID, true); err != nil {
		t.Fatal(err)
	}
	f.tick()
	if f.forks() != 2 {
		t.Fatalf("forks = %d after a hand re-enable, want 2", f.forks())
	}
}

func TestKeepaliveEachRuleBlocksARefresh(t *testing.T) {
	cases := []struct {
		name  string
		setup func(f *kaFix)
	}{
		{"switched off", func(f *kaFix) {
			f.reply(f.now.Add(-56*time.Minute), replyOpt{})
			f.st.SetKeepaliveState(f.task.ID, store.KeepaliveOff)
		}},
		{"room suspended", func(f *kaFix) {
			f.reply(f.now.Add(-56*time.Minute), replyOpt{})
			f.st.SetKeepaliveSuspended("test")
		}},
		{"not idle", func(f *kaFix) {
			f.reply(f.now.Add(-56*time.Minute), replyOpt{})
			f.st.SetStatus(f.task.ID, store.StatusRunning)
		}},
		{"permission dialog open", func(f *kaFix) {
			f.reply(f.now.Add(-56*time.Minute), replyOpt{})
			if _, _, err := f.st.RecordPermission(f.task.ID, "Bash", "ls", "k1", ""); err != nil {
				f.t.Fatal(err)
			}
			f.st.SetStatus(f.task.ID, store.StatusNeedsInput)
		}},
		{"not due yet", func(f *kaFix) { f.reply(f.now.Add(-30*time.Minute), replyOpt{}) }},
		{"already cold", func(f *kaFix) { f.reply(f.now.Add(-61*time.Minute), replyOpt{}) }},
		{"small context", func(f *kaFix) { f.reply(f.now.Add(-56*time.Minute), replyOpt{ctx: 40_000}) }},
		{"effort-keyed model", func(f *kaFix) {
			f.reply(f.now.Add(-56*time.Minute), replyOpt{model: "claude-opus-5"})
		}},
		{"fast mode", func(f *kaFix) { f.reply(f.now.Add(-56*time.Minute), replyOpt{speed: "fast"}) }},
		{"5m cache", func(f *kaFix) {
			f.reply(f.now.Add(-4*time.Minute), replyOpt{ttl5m: true})
		}},
		{"local hooks", func(f *kaFix) {
			f.reply(f.now.Add(-56*time.Minute), replyOpt{})
			dir := filepath.Join(filepath.FromSlash(f.task.Worktree), ".claude")
			os.MkdirAll(dir, 0o755)
			os.WriteFile(filepath.Join(dir, "settings.local.json"),
				[]byte(`{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"x"}]}]}}`), 0o600)
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newKAFix(t)
			c.setup(f)
			f.tick()
			if f.forks() != 0 {
				t.Fatalf("%s: a refresh fired", c.name)
			}
		})
	}
}

// Local settings without hooks do not block it.
func TestKeepaliveLocalSettingsWithoutHooksAreFine(t *testing.T) {
	f := newKAFix(t)
	f.reply(f.now.Add(-56*time.Minute), replyOpt{})
	dir := filepath.Join(filepath.FromSlash(f.task.Worktree), ".claude")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "settings.local.json"), []byte(`{"permissions":{"allow":["Read"]}}`), 0o600)
	f.tick()
	if f.forks() != 1 {
		t.Fatal("a local settings file with no hooks blocked the refresh")
	}
}

// seed writes warmed rows for the current stretch, the latest one `last` ago.
func (f *kaFix) seed(n int, cost float64, last time.Duration) {
	for i := n - 1; i >= 0; i-- {
		at := f.now.Add(-last - time.Duration(i)*time.Hour)
		if err := f.st.AddKeepaliveRefresh(&store.KeepaliveRefresh{
			TaskID: f.task.ID, ResumeID: "card-session-1", At: at, Outcome: outcomeWarmed, Cost: cost,
			Context: 300_000,
		}); err != nil {
			f.t.Fatal(err)
		}
	}
}

// At break-even the refresh is not sent, the card stops, and exactly one toast
// says so.
func TestKeepaliveStopsAtBreakEvenWithOneToast(t *testing.T) {
	f := newKAFix(t)
	// A 300k Opus 5.5 card: budget is 1/8 of 300k x $8/M = $0.30, and a
	// refresh reads 300k at $0.20/M = $0.06. Five spent leaves no room.
	f.reply(f.now.Add(-6*time.Hour), replyOpt{})
	f.seed(5, 0.06, 56*time.Minute)
	f.tick()
	if f.forks() != 0 {
		t.Fatal("a refresh past the budget was sent")
	}
	if f.state() != store.KeepaliveBreakEven {
		t.Fatalf("state = %s, want %s", f.state(), store.KeepaliveBreakEven)
	}
	toasts := f.toasts()
	if len(toasts) != 1 || !strings.Contains(toasts[0], "break-even after 5 refreshes, $0.30") {
		t.Fatalf("toasts = %q", toasts)
	}
	f.tick()
	if len(f.toasts()) != 1 {
		t.Fatal("a second toast for the same stop")
	}
	// The view carries the spend for the card's tooltip.
	v, _ := f.k.view(f.task.ID).(*keepaliveCardView)
	if v == nil || v.Refreshes != 5 || v.Spent < 0.299 || v.Budget < 0.299 {
		t.Fatalf("view = %+v", v)
	}
}

// Four spent still leaves room for the fifth.
func TestKeepaliveRefreshesUpToTheBudget(t *testing.T) {
	f := newKAFix(t)
	f.reply(f.now.Add(-5*time.Hour), replyOpt{})
	f.seed(4, 0.06, 56*time.Minute)
	f.tick()
	if f.forks() != 1 {
		t.Fatal("the last refresh inside the budget was not sent")
	}
}

// A real turn clears a break-even stop and starts a fresh budget.
func TestKeepaliveRealTurnClearsTheStopAndTheBudget(t *testing.T) {
	f := newKAFix(t)
	f.reply(f.now.Add(-6*time.Hour), replyOpt{})
	f.seed(5, 0.06, 56*time.Minute)
	f.tick()
	if f.state() != store.KeepaliveBreakEven {
		t.Fatalf("state = %s", f.state())
	}
	// The card is answered, and later goes idle again for 56 minutes.
	f.now = f.now.Add(2 * time.Hour)
	f.reply(f.now.Add(-56*time.Minute), replyOpt{})
	f.tick()
	if f.state() != store.KeepaliveOn {
		t.Fatalf("state = %s after a real turn, want on", f.state())
	}
	if f.forks() != 1 {
		t.Fatal("the fresh budget did not allow a refresh")
	}
}

// Turning it on by hand restarts the budget too.
func TestKeepaliveHandOnRestartsTheBudget(t *testing.T) {
	f := newKAFix(t)
	f.reply(f.now.Add(-6*time.Hour), replyOpt{})
	f.seed(5, 0.06, 56*time.Minute)
	f.tick()
	d := &Daemon{st: f.st, ka: f.k}
	if _, err := d.keepaliveSet(f.task.ID, true); err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(time.Second)
	f.tick()
	if f.forks() != 1 {
		t.Fatal("turning it on by hand did not restart the budget")
	}
}

// A card turned off by hand stays off through a real turn.
func TestKeepaliveHandOffSurvivesARealTurn(t *testing.T) {
	f := newKAFix(t)
	f.st.SetKeepaliveState(f.task.ID, store.KeepaliveOff)
	f.now = f.now.Add(time.Minute)
	f.reply(f.now.Add(-56*time.Minute), replyOpt{})
	f.tick()
	if f.state() != store.KeepaliveOff || f.forks() != 0 {
		t.Fatalf("state = %s, forks = %d", f.state(), f.forks())
	}
}

func TestKeepaliveClassifiesReceiptsInOrder(t *testing.T) {
	var none *forkReceipt
	parse := func(b []byte) *forkReceipt {
		var r forkReceipt
		json.Unmarshal(b, &r)
		return &r
	}
	cases := []struct {
		name string
		r    *forkReceipt
		err  error
		want string
	}{
		{"two turns nothing refused", parse(receipt(2, 0, "success", 297_000, 1_000)), nil, outcomeActed},
		{"a tool refused", parse(receipt(2, 1, "error_max_turns", 297_000, 1_000)), nil, outcomeRefused},
		{"no receipt", none, fmt.Errorf("exit 1"), outcomeFailed},
		{"not success", parse(receipt(1, 0, "error_during_execution", 297_000, 1_000)), nil, outcomeFailed},
		{"warmed", parse(receipt(1, 0, "success", 297_000, 1_000)), nil, outcomeWarmed},
		{"read too little", parse(receipt(1, 0, "success", 200_000, 1_000)), nil, outcomeMiss},
		{"wrote too much", parse(receipt(1, 0, "success", 290_000, 60_000)), nil, outcomeMiss},
	}
	for _, c := range cases {
		if got := classifyReceipt(c.r, c.err, 300_000); got != c.want {
			t.Errorf("%s: got %s, want %s", c.name, got, c.want)
		}
	}
}

// A fork that acted stops the card for good and suspends the room.
func TestKeepaliveActedSuspendsTheRoom(t *testing.T) {
	f := newKAFix(t)
	f.answer = func() ([]byte, error) { return receipt(2, 0, "success", 297_000, 1_000), nil }
	f.reply(f.now.Add(-56*time.Minute), replyOpt{})
	f.tick()
	if f.state() != store.KeepaliveActed {
		t.Fatalf("state = %s", f.state())
	}
	if f.st.KeepaliveSuspended() == "" {
		t.Fatal("the room was not suspended")
	}
	// A real turn does not clear an acted stop.
	f.st.SetKeepaliveSuspended("")
	f.now = f.now.Add(2 * time.Hour)
	f.reply(f.now.Add(-56*time.Minute), replyOpt{})
	f.tick()
	if f.state() != store.KeepaliveActed || f.forks() != 1 {
		t.Fatalf("state = %s, forks = %d after a real turn", f.state(), f.forks())
	}
}

// A refused tool stops the card but leaves the room alone.
func TestKeepaliveRefusedStopsTheCardOnly(t *testing.T) {
	f := newKAFix(t)
	f.answer = func() ([]byte, error) { return receipt(2, 1, "error_max_turns", 297_000, 1_000), nil }
	f.reply(f.now.Add(-56*time.Minute), replyOpt{})
	f.tick()
	if f.state() != store.KeepaliveActed {
		t.Fatalf("state = %s", f.state())
	}
	if f.st.KeepaliveSuspended() != "" {
		t.Fatal("a refused tool suspended the room")
	}
}

// Two failures in a row stop the card.
func TestKeepaliveTwoFailuresStopTheCard(t *testing.T) {
	f := newKAFix(t)
	f.answer = func() ([]byte, error) { return nil, fmt.Errorf("claude not found") }
	f.reply(f.now.Add(-56*time.Minute), replyOpt{})
	f.tick()
	if f.state() != store.KeepaliveOn {
		t.Fatalf("one failure stopped it: %s", f.state())
	}
	f.now = f.now.Add(time.Minute)
	f.tick()
	if f.state() != store.KeepaliveFailing {
		t.Fatalf("state = %s after two failures", f.state())
	}
}

// Two misses on two different cards suspend the room. One miss stops its card.
func TestKeepaliveTwoMissesOnTwoCardsSuspend(t *testing.T) {
	f := newKAFix(t)
	f.answer = func() ([]byte, error) { return receipt(1, 0, "success", 1_000, 290_000), nil }
	f.reply(f.now.Add(-56*time.Minute), replyOpt{})
	f.tick()
	if f.state() != store.KeepaliveMiss {
		t.Fatalf("state = %s", f.state())
	}
	if f.st.KeepaliveSuspended() != "" {
		t.Fatal("one miss suspended the room")
	}
	// A second card, same transcript shape.
	other, _, _ := f.st.Register(store.Observed{WireName: "ka-card-2", Worktree: f.task.Worktree, Runner: "claude"})
	f.st.SetResumeID(other.ID, "card-session-1")
	f.st.SetStatus(other.ID, store.StatusNeedsInput)
	f.st.SetKeepaliveState(other.ID, store.KeepaliveOn)
	f.tick()
	if f.st.KeepaliveSuspended() == "" {
		t.Fatal("two misses on two cards did not suspend the room")
	}
}

// A new card takes the room default, an existing one keeps its own switch, and
// the 1h pin follows the switch.
func TestKeepaliveAtLaunch(t *testing.T) {
	f := newKAFix(t)
	d := &Daemon{st: f.st, ka: f.k}
	h, err := f.st.Harness("claude")
	if err != nil {
		t.Fatal(err)
	}
	fresh, _, _ := f.st.Register(store.Observed{WireName: "fresh", Worktree: f.task.Worktree, Runner: "claude"})
	env := map[string]string{}
	d.keepaliveAtLaunch(fresh.ID, h, env)
	if c, _ := f.st.KeepaliveCardOf(fresh.ID); c == nil || c.State != store.KeepaliveOn {
		t.Fatalf("a new card did not take the default on: %+v", c)
	}
	if env[keepaliveTTLVar] != "1h" {
		t.Fatalf("no 1h pin: %v", env)
	}
	// The default goes off. The existing card keeps its switch on relaunch.
	f.st.SetKeepaliveDefault(false)
	env = map[string]string{}
	d.keepaliveAtLaunch(fresh.ID, h, env)
	if c, _ := f.st.KeepaliveCardOf(fresh.ID); c.State != store.KeepaliveOn {
		t.Fatal("changing the default changed an existing card")
	}
	// A card created now starts off, with no pin.
	later, _, _ := f.st.Register(store.Observed{WireName: "later", Worktree: f.task.Worktree, Runner: "claude"})
	env = map[string]string{}
	d.keepaliveAtLaunch(later.ID, h, env)
	if c, _ := f.st.KeepaliveCardOf(later.ID); c == nil || c.State != store.KeepaliveOff {
		t.Fatalf("a new card ignored the default off: %+v", c)
	}
	if _, ok := env[keepaliveTTLVar]; ok {
		t.Fatal("a card with keep-alive off was pinned")
	}
	// Not Claude: no switch at all.
	sh := &store.Harness{ID: "shell", Cmd: "pwsh"}
	third, _, _ := f.st.Register(store.Observed{WireName: "third", Worktree: f.task.Worktree, Runner: "shell"})
	d.keepaliveAtLaunch(third.ID, sh, map[string]string{})
	if c, _ := f.st.KeepaliveCardOf(third.ID); c != nil {
		t.Fatal("a non-Claude card got a switch")
	}
}

// The 1M window variant is asked for when telemetry says so.
func TestKeepaliveForksOnTheOneMillionVariant(t *testing.T) {
	f := newKAFix(t)
	f.k.window = func(string) int { return 1_000_000 }
	f.reply(f.now.Add(-56*time.Minute), replyOpt{})
	f.tick()
	if f.forks() != 1 || !strings.Contains(strings.Join(f.specs[0].Args, " "), "claude-opus-5-5[1m]") {
		t.Fatalf("specs = %+v", f.specs)
	}
}
