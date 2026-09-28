package daemon

import (
	"bufio"
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

	"github.com/dovholuknf/atrium/internal/api"
	"github.com/dovholuknf/atrium/internal/store"
)

// THE CACHE KEEP-ALIVE. See docs/cache-keepalive-design.md, which is the
// argument. This file is the mechanism.
//
// An idle Claude card's prompt cache expires an hour after its last request, and
// the next turn then writes the whole context again at twice the input price. A
// refresh shortly before expiry reads the cache instead, at a fortieth of that
// on Opus 5.5, and the read resets the hour.
//
// The refresh is a FORKED HEADLESS RESUME of the card's conversation: a separate
// `claude -p` that reads the same prefix, answers "OK" and is thrown away. The
// card's terminal, transcript and conversation are never touched. Unguarded,
// a fork is a working copy of the agent (the design's probe 2 edited files and
// opened a review), so every guard below is load-bearing:
//
//   - a PreToolUse hook that refuses every tool, from a settings file atrium
//     owns, because denying tools by name changes the cached prefix;
//   - `--max-turns 1`, so a refused tool call cannot turn into a second turn;
//   - `--setting-sources local`, so the user's and the project's hooks do not
//     run on every refresh (a card whose local settings carry hooks is skipped);
//   - `--no-session-persistence` and `--fork-session`, so nothing is written
//     under the card's session or any other;
//   - the receipt is checked, and a fork that tried to act stops the card, and
//     one that acted suspends the room.

// keepaliveTick is how often cards are looked at. A refresh is due inside a five
// minute margin, so once a minute is plenty.
const keepaliveTick = time.Minute

// keepaliveForkTimeout bounds one refresh. A fork is a process start and one
// API call.
const keepaliveForkTimeout = 120 * time.Second

// Policy defaults. See the design's "Policy" and "The stop rule".
const (
	// keepaliveMargin is how close to expiry a refresh fires.
	keepaliveMargin = 5 * time.Minute
	// keepaliveMinContext is the smallest context worth keeping warm. Below it
	// a saved wake is worth under $0.40 on Opus 5.5.
	keepaliveMinContext = 50_000
	// keepaliveBudgetFraction is the stop rule: refresh spend since the card
	// went idle may reach this fraction of one full 1h rehydration of its
	// context. 1/8 fits the resume data best across models.
	keepaliveBudgetFraction = 0.125
	// keepaliveWarmRead and keepaliveWarmWrite are the receipt thresholds, as
	// fractions of the card's context.
	keepaliveWarmRead  = 0.90
	keepaliveWarmWrite = 0.05
)

// keepalivePrompt is what the fork is asked. The words do not matter to the
// cache, only to what the model does with its one turn.
const keepalivePrompt = "Automated cache refresh from atrium. Reply with the single word OK. " +
	"Do not use tools. Do not continue any task."

// keepaliveHookSettings is the settings file layered onto every fork: one
// PreToolUse hook that refuses every tool. Hooks are not part of the request, so
// this keeps the prefix whole where a deny rule would not.
const keepaliveHookSettings = `{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "*",
        "hooks": [
          { "type": "command", "command": "echo 'atrium cache refresh: tools are blocked' >&2; exit 2" }
        ]
      }
    ]
  }
}
`

// keepalivePricesVersion names the price table below, and is written on every
// ledger row so a stale table shows.
const keepalivePricesVersion = "usageprices-2026-09-27"

// keepalivePrice is per million tokens: the 1h write and the cache read, plus
// plain input and output for the fork's own few tokens.
type keepalivePrice struct{ In, Cw1h, Cr, Out float64 }

// keepalivePrices mirrors `$UsagePrices` in dotfiles/claude/usage/UsageCommon.ps1
// for the models a card may be refreshed on. Only models whose effort level is
// not part of the cache key are here, because atrium cannot know a card's
// current effort, and a fork at another effort would write the whole context.
var keepalivePrices = map[string]keepalivePrice{
	"claude-opus-5-5":  {In: 4, Cw1h: 8, Cr: 0.20, Out: 20},
	"claude-fable-5-1": {In: 10, Cw1h: 20, Cr: 0.25, Out: 50},
}

// keepalivePriceFor looks a model up with any `[1m]` style suffix and date
// stamp tolerated.
func keepalivePriceFor(model string) (keepalivePrice, bool) {
	m := strings.ToLower(strings.TrimSpace(model))
	if i := strings.IndexByte(m, '['); i >= 0 {
		m = m[:i]
	}
	for k, p := range keepalivePrices {
		if m == k || strings.HasPrefix(m, k+"-") {
			return p, true
		}
	}
	return keepalivePrice{}, false
}

// lastReply is what a card's transcript says about its last main reply. Every
// fact the policy needs about the card comes from here and never from a fork.
type lastReply struct {
	Model   string
	At      time.Time
	Speed   string
	Context int64
	// TTL is the cache lifetime the reply wrote at, from its cache_creation
	// split. Zero when it wrote nothing and read everything, which says nothing
	// about the TTL, so the last reply that did write is used for it.
	TTL time.Duration
}

// transcriptTail bounds how much of a transcript is read. The last main reply
// is at the end, and a transcript reaches ninety megabytes in a day.
const transcriptTail = 2 << 20

// mainReply is one line of a transcript that carries a main-thread assistant
// reply's usage. A reply written in several content blocks is several lines
// with the same MessageID and the same usage, so a caller that sums keeps one
// per id.
type mainReply struct {
	MessageID string
	Model     string
	At        time.Time
	Speed     string
	// Input, CacheWrite and CacheRead are the request's input side. Their sum is
	// the context the reply was answered on.
	Input, CacheWrite, CacheRead int64
	// Write5m and Write1h are CacheWrite split by TTL.
	Write5m, Write1h int64
	Output           int64
	// Sidechain is a subagent's reply written inline, as older Claude Code
	// did. Only scanReplies offers these.
	Sidechain bool
}

// Context is the whole prompt the reply was answered on.
func (r *mainReply) Context() int64 { return r.Input + r.CacheWrite + r.CacheRead }

// scanMainReplies calls fn for every main-thread assistant reply with usage in
// a transcript, in file order. Subagent replies (`isSidechain`) are not the
// card's prefix and are skipped.
//
// It returns how many bytes of COMPLETE lines it read, so a caller reading a
// transcript that is still being written can start its next read there and
// never on half a line. A last line with no newline is still offered to fn when
// it parses, and not counted, so the next read offers it again.
//
// The one transcript reader: the keep-alive's last reply and the usage record
// are both built on it.
func scanMainReplies(r io.Reader, fn func(*mainReply)) (int64, error) {
	return scanReplies(r, func(m *mainReply) {
		if !m.Sidechain {
			fn(m)
		}
	})
}

// scanReplies is scanMainReplies with the inline subagent replies offered too,
// marked Sidechain. The usage record counts those as the subagent's.
func scanReplies(r io.Reader, fn func(*mainReply)) (int64, error) {
	type usage struct {
		Input         int64  `json:"input_tokens"`
		CacheWrite    int64  `json:"cache_creation_input_tokens"`
		CacheRead     int64  `json:"cache_read_input_tokens"`
		Output        int64  `json:"output_tokens"`
		Speed         string `json:"speed"`
		CacheCreation struct {
			OneHour  int64 `json:"ephemeral_1h_input_tokens"`
			FiveMins int64 `json:"ephemeral_5m_input_tokens"`
		} `json:"cache_creation"`
	}
	offer := func(line []byte) {
		if !bytes.Contains(line, []byte(`"assistant"`)) || !bytes.Contains(line, []byte(`"usage"`)) {
			return
		}
		var e struct {
			Type      string `json:"type"`
			Sidechain bool   `json:"isSidechain"`
			Timestamp string `json:"timestamp"`
			Message   struct {
				ID    string `json:"id"`
				Model string `json:"model"`
				Usage *usage `json:"usage"`
			} `json:"message"`
		}
		if json.Unmarshal(line, &e) != nil || e.Type != "assistant" || e.Message.Usage == nil {
			return
		}
		at, err := time.Parse(time.RFC3339Nano, e.Timestamp)
		if err != nil {
			return
		}
		u := e.Message.Usage
		fn(&mainReply{
			MessageID: e.Message.ID, Model: e.Message.Model, At: at, Speed: u.Speed,
			Input: u.Input, CacheWrite: u.CacheWrite, CacheRead: u.CacheRead,
			Write5m: u.CacheCreation.FiveMins, Write1h: u.CacheCreation.OneHour, Output: u.Output,
			Sidechain: e.Sidechain,
		})
	}
	br := bufio.NewReaderSize(r, 1<<20)
	var done int64
	for {
		line, err := br.ReadSlice('\n')
		if errors.Is(err, bufio.ErrBufferFull) {
			// A line longer than the buffer, a pasted image or a big tool
			// result. Gathered whole, to the bound the old scanner had.
			whole := append([]byte(nil), line...)
			for errors.Is(err, bufio.ErrBufferFull) && len(whole) < 32<<20 {
				line, err = br.ReadSlice('\n')
				whole = append(whole, line...)
			}
			if errors.Is(err, bufio.ErrBufferFull) {
				return done, errors.New("a transcript line over 32MB")
			}
			line = whole
		}
		if err == io.EOF {
			offer(line)
			return done, nil
		}
		if err != nil {
			return done, err
		}
		done += int64(len(line))
		offer(line)
	}
}

// readLastReply finds the last main-thread assistant reply with usage in a
// transcript.
func readLastReply(path string) (*lastReply, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() > transcriptTail {
		if _, err := f.Seek(info.Size()-transcriptTail, io.SeekStart); err != nil {
			return nil, err
		}
	}
	var (
		out *lastReply
		ttl time.Duration
	)
	_, err = scanMainReplies(f, func(r *mainReply) {
		switch {
		case r.Write1h > 0:
			ttl = time.Hour
		case r.Write5m > 0:
			ttl = 5 * time.Minute
		}
		out = &lastReply{Model: r.Model, At: r.At, Speed: r.Speed, Context: r.Context()}
	})
	if err != nil {
		return nil, err
	}
	if out == nil {
		return nil, errors.New("no main reply in the transcript")
	}
	out.TTL = ttl
	return out, nil
}

// forkReceipt is the part of `claude -p --output-format json` a refresh reads.
type forkReceipt struct {
	Subtype           string            `json:"subtype"`
	NumTurns          int               `json:"num_turns"`
	SessionID         string            `json:"session_id"`
	PermissionDenials []json.RawMessage `json:"permission_denials"`
	Usage             *struct {
		Input      int64 `json:"input_tokens"`
		CacheWrite int64 `json:"cache_creation_input_tokens"`
		CacheRead  int64 `json:"cache_read_input_tokens"`
		Output     int64 `json:"output_tokens"`
	} `json:"usage"`
}

// Receipt outcomes, in the order they are tested. See the design's "Reading the
// receipt".
const (
	outcomeActed   = "acted"
	outcomeRefused = "refused"
	outcomeFailed  = "failed"
	outcomeWarmed  = "warmed"
	outcomeMiss    = "miss"
)

// classifyReceipt scores one fork. ctx is the card's context from its own
// transcript. A nil receipt, or one with no usage, is `failed`.
func classifyReceipt(r *forkReceipt, runErr error, ctx int64) string {
	if r != nil && r.NumTurns > 1 && len(r.PermissionDenials) == 0 {
		return outcomeActed
	}
	if r != nil && len(r.PermissionDenials) > 0 {
		return outcomeRefused
	}
	if runErr != nil || r == nil || r.Usage == nil || r.Subtype != "success" {
		return outcomeFailed
	}
	if ctx > 0 && float64(r.Usage.CacheRead) >= keepaliveWarmRead*float64(ctx) &&
		float64(r.Usage.CacheWrite) < keepaliveWarmWrite*float64(ctx) {
		return outcomeWarmed
	}
	return outcomeMiss
}

// receiptCost prices a fork's tokens. Cache writes are priced at the 1h rate,
// because only cards on the 1h cache are refreshed.
func receiptCost(r *forkReceipt, p keepalivePrice) float64 {
	if r == nil || r.Usage == nil {
		return 0
	}
	u := r.Usage
	return (float64(u.Input)*p.In + float64(u.CacheWrite)*p.Cw1h + float64(u.CacheRead)*p.Cr +
		float64(u.Output)*p.Out) / 1e6
}

// forkSpec is one refresh's command line.
type forkSpec struct {
	Exe  string
	Args []string
	Dir  string
	Env  []string
}

// runForkProcess runs a fork and returns its stdout. The default fork runner.
func runForkProcess(ctx context.Context, spec forkSpec) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, keepaliveForkTimeout)
	defer cancel()
	exe := spec.Exe
	args := spec.Args
	if resolved, err := exec.LookPath(exe); err == nil {
		exe, args = viaShellIfScript(resolved, args)
	}
	cmd := exec.CommandContext(ctx, exe, args...)
	cmd.Dir = spec.Dir
	cmd.Env = spec.Env
	cmd.Stdin = nil
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	hideWindow(cmd)
	err := cmd.Run()
	if err != nil && out.Len() == 0 {
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(firstLine(errb.String())))
	}
	return out.Bytes(), err
}

// keepaliveCardView is what the board draws for one card.
type keepaliveCardView struct {
	State   string    `json:"state"`
	StateAt time.Time `json:"state_at"`
	// Why is the reason a card cannot be warmed, for the greyed switch, or the
	// last skip reason while it is on.
	Why string `json:"why,omitempty"`
	// Refreshes, Spent and Budget describe the current idle stretch.
	Refreshes int     `json:"refreshes"`
	Spent     float64 `json:"spent"`
	Budget    float64 `json:"budget,omitempty"`
	// WarmUntil is when the card's cache expires, when atrium knows.
	WarmUntil *time.Time `json:"warm_until,omitempty"`
}

// keepalive holds the loop's collaborators, so a test can run a tick with a fake
// fork, a fake clock and hand-written transcripts.
type keepalive struct {
	st  *store.Store
	now func() time.Time
	// fork runs one refresh and returns the receipt's bytes.
	fork func(ctx context.Context, spec forkSpec) ([]byte, error)
	// transcript finds a card's transcript from its directory and session id.
	transcript func(cwd, sessionID string) string
	// window is the card's context window from telemetry, or 0.
	window func(taskID string) int
	// broadcast tells the board, and toast raises one on it.
	broadcast func(event string, payload any)
	// hookFile is the path of the block-all settings file.
	hookFile string
	// baseEnv is the environment a fork starts from.
	baseEnv func() []string
	// record saves one refresh's ledger row.
	record func(*store.KeepaliveRefresh) error
	// spent saves one refresh's row in the card's usage record. See usage.go.
	spent func(*store.SessionUsage) error

	mu sync.Mutex
	// lastMissCard is the card of the room's most recent attempt when that
	// attempt was a miss, for "two misses on different cards in a row".
	lastMissCard string
	// why is the last skip reason per card, for the board.
	why map[string]string
	// inFlight stops a slow fork from being started twice.
	inFlight map[string]bool
	// unsaved holds back a card whose last refresh row could not be saved. The
	// warm window and the budget come from saved rows, so without it the next
	// tick would see the old expiry and fork again.
	unsaved map[string]unsavedRefresh
}

// unsavedRefresh holds a card's refreshes back until its cache would expire, or
// until the card takes a real turn after the fork.
type unsavedRefresh struct {
	at, until time.Time
	why       string
}

func newKeepalive(st *store.Store) *keepalive {
	return &keepalive{
		st:         st,
		now:        func() time.Time { return time.Now().UTC() },
		fork:       runForkProcess,
		transcript: api.TranscriptPath,
		window:     func(string) int { return 0 },
		broadcast:  func(string, any) {},
		hookFile:   filepath.Join(StateDir(), "keepalive-block-tools.json"),
		baseEnv:    os.Environ,
		why:        map[string]string{},
		inFlight:   map[string]bool{},
		unsaved:    map[string]unsavedRefresh{},
		record:     st.AddKeepaliveRefresh,
		spent:      st.AddSessionUsage,
	}
}

// ensureHookFile writes the block-all settings file if it is missing or wrong.
func (k *keepalive) ensureHookFile() error {
	if got, err := os.ReadFile(k.hookFile); err == nil && string(got) == keepaliveHookSettings {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(k.hookFile), 0o755); err != nil {
		return err
	}
	return os.WriteFile(k.hookFile, []byte(keepaliveHookSettings), 0o600)
}

// localHasHooks reports whether a card's `.claude/settings.local.json` names
// hooks. `--setting-sources local` still loads that file, so its hooks would run
// in the fork.
func localHasHooks(cwd string) bool {
	b, err := os.ReadFile(filepath.Join(cwd, ".claude", "settings.local.json"))
	if err != nil {
		return false
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(b, &m) != nil {
		// A file that will not parse cannot be shown to be hook-free.
		return true
	}
	raw, ok := m["hooks"]
	return ok && len(bytes.TrimSpace(raw)) > 0 && string(bytes.TrimSpace(raw)) != "{}" &&
		string(bytes.TrimSpace(raw)) != "null"
}

// verdict is one card's decision on one tick.
type verdict struct {
	act     string // "refresh", "skip", "break-even"
	why     string
	reply   *lastReply
	h       *store.Harness
	price   keepalivePrice
	budget  float64
	spent   float64
	count   int
	expiry  time.Time
	anchor  time.Time
	history []*store.KeepaliveRefresh
}

// stretchState reads the current idle stretch of a card: the refreshes since
// its budget last restarted, their spend, and when the cache expires.
//
// Two different starting points. The BUDGET restarts at the later of the last
// real reply and the switch last being set, so turning it on by hand gives a
// fresh budget. The WARM WINDOW counts every warmed refresh since the last real
// reply, because a refresh made before a hand re-enable still refreshed the
// cache.
func (k *keepalive) stretchState(taskID string, card *store.KeepaliveCard, r *lastReply) (
	anchor time.Time, rows []*store.KeepaliveRefresh, spent float64, count int, expiry time.Time) {
	anchor = r.At
	// Only a switch turned ON moves the anchor. A stop is stamped too, and the
	// stopped card's tooltip wants the spend that led to the stop.
	if card != nil && card.State == store.KeepaliveOn && card.StateAt.After(anchor) {
		anchor = card.StateAt
	}
	all, _ := k.st.KeepaliveRefreshesSince(taskID, r.At)
	warm := r.At
	for _, row := range all {
		if row.Outcome == outcomeWarmed && row.At.After(warm) {
			warm = row.At
		}
		if !row.At.After(anchor) {
			continue
		}
		rows = append(rows, row)
		spent += row.Cost
		if row.Outcome == outcomeWarmed {
			count++
		}
	}
	return anchor, rows, spent, count, warm.Add(r.TTL)
}

// decide applies the design's seven rules to one card. It reads, and writes
// nothing.
func (k *keepalive) decide(t *store.Task, card *store.KeepaliveCard) verdict {
	skip := func(why string) verdict { return verdict{act: "skip", why: why} }
	if card == nil || card.State != store.KeepaliveOn {
		return skip("")
	}
	h, err := k.st.Harness(t.Runner)
	if err != nil || !isClaude(h) {
		return skip("not a Claude card")
	}
	if strings.TrimSpace(t.ResumeID) == "" || strings.TrimSpace(t.Worktree) == "" {
		return skip("no session id yet")
	}
	if t.Status != store.StatusNeedsInput {
		return skip("not idle")
	}
	if pend, _ := k.st.PendingForTask(t.ID); len(pend) > 0 {
		return skip("a permission dialog is open")
	}
	path := k.transcript(t.Worktree, t.ResumeID)
	if path == "" {
		return skip("no transcript")
	}
	r, err := readLastReply(path)
	if err != nil {
		return skip("no transcript")
	}
	price, ok := keepalivePriceFor(r.Model)
	if !ok {
		return skip("model " + r.Model + " keys its cache on effort")
	}
	if strings.EqualFold(r.Speed, "fast") {
		return skip("fast mode")
	}
	if r.TTL != time.Hour {
		return skip("not on the 1h cache")
	}
	if r.Context < keepaliveMinContext {
		return skip("context under 50k")
	}
	if localHasHooks(t.Worktree) {
		return skip("local hooks")
	}
	anchor, rows, spent, count, expiry := k.stretchState(t.ID, card, r)
	now := k.now()
	if !now.Before(expiry) {
		return skip("cache already cold")
	}
	if expiry.Sub(now) > keepaliveMargin {
		return verdict{act: "skip", why: "not due", expiry: expiry, spent: spent, count: count, reply: r,
			price: price, budget: budgetFor(r.Context, price)}
	}
	v := verdict{
		act: "refresh", reply: r, h: h, price: price, budget: budgetFor(r.Context, price),
		spent: spent, count: count, expiry: expiry, anchor: anchor, history: rows,
	}
	next := float64(r.Context) * price.Cr / 1e6
	if spent+next > v.budget {
		v.act, v.why = "break-even", "budget spent"
	}
	return v
}

// budgetFor is the stop rule's cap: a fraction of one full 1h rehydration.
func budgetFor(ctx int64, p keepalivePrice) float64 {
	return keepaliveBudgetFraction * float64(ctx) * p.Cw1h / 1e6
}

// modelArg is the model to fork on. A card whose telemetry says it has a window
// over 200k runs the 1M variant, which is a different request.
func (k *keepalive) modelArg(taskID, model string) string {
	if w := k.window(taskID); w > 200_000 && !strings.Contains(model, "[") {
		return model + "[1m]"
	}
	return model
}

// forkEnv is the fork's environment: the one a launch builds, minus the card's
// identity, with atrium's hooks off and the card's 1h cache pin.
//
// THE CARD'S OWN LAUNCH ENV AND EFFORT GO IN TOO, layered as a launch layers
// them (see launchOptionEnv). A fork without the card's env can reach a
// different endpoint or account than the card it is warming, and then it warms
// nothing and bills somebody else. The model is not passed here: the fork names
// it as `--model`, from the card's last reply.
func (k *keepalive) forkEnv(h *store.Harness, t *store.Task) ([]string, error) {
	added, err := launchOptionEnv(h, t.LaunchEnv, "", t.Effort)
	if err != nil {
		return nil, err
	}
	env := overEnv(h.Env, added)
	base := make([]string, 0, 64)
	for _, kv := range k.baseEnv() {
		if !strings.HasPrefix(strings.ToUpper(kv), "ATRIUM_PERM_GATE=") {
			base = append(base, kv)
		}
	}
	extra := map[string]string{"ATRIUM_PERM_GATE": "off"}
	if _, set := env[keepaliveTTLVar]; !set {
		extra[keepaliveTTLVar] = "1h"
	}
	return childEnvFrom(base, env, extra), nil
}

// forkEffortArgs is the card's launch effort in the shape its harness declares,
// appended to the fork's command line. Empty when the card was launched on the
// runner's default. The card's extra args are NOT carried: they were written
// for an interactive start and a fork is a one-turn print run.
func forkEffortArgs(h *store.Harness, t *store.Task) ([]string, error) {
	return withMapped(h, nil, "effort", "level", h.EffortArgs, h.EffortEnv, t.Effort)
}

// forkArgs is the refresh command line. Kept in one place so the test pins it.
func (k *keepalive) forkArgs(sessionID, model string) []string {
	return []string{
		"-p", keepalivePrompt,
		"--resume", sessionID, "--fork-session", "--no-session-persistence",
		"--model", model,
		"--setting-sources", "local",
		"--settings", k.hookFile,
		"--max-turns", "1",
		"--output-format", "json",
	}
}

// tick looks at every card with keep-alive on, once.
func (k *keepalive) tick(ctx context.Context) {
	if k.st.KeepaliveSuspended() != "" {
		return
	}
	cards, err := k.st.KeepaliveCards()
	if err != nil {
		return
	}
	for _, card := range cards {
		if ctx.Err() != nil {
			return
		}
		t, err := k.st.Get(card.TaskID)
		if err != nil || t == nil {
			continue
		}
		card = k.clearOnRealTurn(t, card)
		v := k.decide(t, card)
		k.mu.Lock()
		if u, held := k.unsaved[t.ID]; held {
			if !k.now().Before(u.until) || (v.reply != nil && v.reply.At.After(u.at)) {
				delete(k.unsaved, t.ID)
			} else if v.act != "skip" || v.why == "not due" {
				v = verdict{act: "skip", why: u.why}
			}
		}
		k.why[t.ID] = v.why
		busy := k.inFlight[t.ID]
		k.mu.Unlock()
		switch v.act {
		case "break-even":
			k.stop(t, store.KeepaliveBreakEven, v)
		case "refresh":
			if busy {
				continue
			}
			k.refresh(ctx, t, v)
		}
		if k.st.KeepaliveSuspended() != "" {
			return
		}
	}
}

// clearOnRealTurn puts a self-clearing stopped card back on once the card has
// taken a real turn since it stopped.
func (k *keepalive) clearOnRealTurn(t *store.Task, card *store.KeepaliveCard) *store.KeepaliveCard {
	switch card.State {
	case store.KeepaliveBreakEven, store.KeepaliveMiss, store.KeepaliveFailing:
	default:
		return card
	}
	path := k.transcript(t.Worktree, t.ResumeID)
	if path == "" {
		return card
	}
	r, err := readLastReply(path)
	if err != nil || !r.At.After(card.StateAt) {
		return card
	}
	next, err := k.st.SetKeepaliveStateAt(t.ID, store.KeepaliveOn, k.now())
	if err != nil {
		return card
	}
	k.broadcast("keepalive", map[string]any{"task_id": t.ID, "state": next.State})
	return next
}

// refresh runs one fork, records it, and applies what the receipt says.
func (k *keepalive) refresh(ctx context.Context, t *store.Task, v verdict) {
	k.mu.Lock()
	k.inFlight[t.ID] = true
	k.mu.Unlock()
	defer func() {
		k.mu.Lock()
		delete(k.inFlight, t.ID)
		k.mu.Unlock()
	}()
	if err := k.ensureHookFile(); err != nil {
		log.Printf("[atrium] keep-alive: could not write %s: %v", k.hookFile, err)
		return
	}
	model := k.modelArg(t.ID, v.reply.Model)
	// Refused, never forked without them: a fork on the wrong env or effort
	// writes a cache the card never reads.
	env, err := k.forkEnv(v.h, t)
	var effort []string
	if err == nil {
		effort, err = forkEffortArgs(v.h, t)
	}
	if err != nil {
		log.Printf("[atrium] keep-alive: not refreshing %s: %v", t.ID, err)
		k.mu.Lock()
		k.why[t.ID] = "launch options: " + err.Error()
		k.mu.Unlock()
		return
	}
	spec := forkSpec{
		Exe: v.h.Exe(), Args: append(k.forkArgs(t.ResumeID, model), effort...), Dir: t.Worktree, Env: env,
	}
	sent := k.now()
	out, runErr := k.fork(ctx, spec)
	var rec *forkReceipt
	if len(bytes.TrimSpace(out)) > 0 {
		var r forkReceipt
		if err := json.Unmarshal(bytes.TrimSpace(out), &r); err == nil {
			rec = &r
		}
	}
	outcome := classifyReceipt(rec, runErr, v.reply.Context)
	row := &store.KeepaliveRefresh{
		TaskID: t.ID, ResumeID: t.ResumeID, At: sent, Model: model, Outcome: outcome,
		Context: v.reply.Context, TTLLeftS: int64(v.expiry.Sub(sent).Seconds()),
		SpentBefore: v.spent, Budget: v.budget, Prices: keepalivePricesVersion,
		Cost: receiptCost(rec, v.price),
	}
	if rec != nil {
		row.ForkSession = rec.SessionID
		if rec.Usage != nil {
			row.CacheRead, row.CacheWrite = rec.Usage.CacheRead, rec.Usage.CacheWrite
			row.Input, row.Output = rec.Usage.Input, rec.Usage.Output
		}
	}
	// The card's usage record too, where it sits beside the turns. A fork that
	// never reached the API spent nothing and is not a row there.
	if rec != nil && rec.Usage != nil {
		if err := k.spent(usageOfRefresh(row)); err != nil {
			log.Printf("[atrium] keep-alive: could not record the token use of a refresh of %s: %v", t.ID, err)
		}
	}
	if err := k.record(row); err != nil {
		log.Printf("[atrium] keep-alive: could not record a refresh of %s: %v", t.ID, err)
		// The fork may have warmed the cache, so hold until that cache would expire.
		until := sent.Add(v.reply.TTL)
		if v.expiry.After(until) {
			until = v.expiry
		}
		u := unsavedRefresh{at: sent, until: until,
			why: "a refresh could not be saved, paused until " + until.Local().Format("15:04")}
		k.mu.Lock()
		k.unsaved[t.ID] = u
		k.why[t.ID] = u.why
		k.mu.Unlock()
	}
	if runErr != nil {
		log.Printf("[atrium] keep-alive: fork for %s: %v", t.ID, runErr)
	}
	k.apply(t, outcome, v)
	k.broadcast("keepalive", map[string]any{"task_id": t.ID, "outcome": outcome})
}

// apply moves a card, or the room, on what one receipt said.
func (k *keepalive) apply(t *store.Task, outcome string, v verdict) {
	k.mu.Lock()
	prevMiss := k.lastMissCard
	if outcome == outcomeMiss {
		k.lastMissCard = t.ID
	} else {
		k.lastMissCard = ""
	}
	k.mu.Unlock()

	switch outcome {
	case outcomeActed:
		k.stop(t, store.KeepaliveActed, v)
		k.suspend("a refresh fork on " + t.DisplayTitle() + " took a second turn with nothing refused")
	case outcomeRefused:
		k.stop(t, store.KeepaliveActed, v)
	case outcomeMiss:
		k.stop(t, store.KeepaliveMiss, v)
		if prevMiss != "" && prevMiss != t.ID {
			k.suspend("two refreshes in a row on two cards missed the cache")
		}
	case outcomeFailed:
		// Two failures in a row in this stretch stop the card.
		n := 0
		for i := len(v.history) - 1; i >= 0 && v.history[i].Outcome == outcomeFailed; i-- {
			n++
		}
		if n >= 1 {
			k.stop(t, store.KeepaliveFailing, v)
		}
	}
}

// stop sets a card's stopped state, and for break-even raises the toast.
func (k *keepalive) stop(t *store.Task, state string, v verdict) {
	if _, err := k.st.SetKeepaliveStateAt(t.ID, state, k.now()); err != nil {
		return
	}
	payload := map[string]any{
		"task_id": t.ID, "title": t.DisplayTitle(), "state": state,
		"refreshes": v.count, "spent": v.spent, "budget": v.budget,
	}
	if state == store.KeepaliveBreakEven {
		payload["toast"] = fmt.Sprintf("keep-alive stopped on %s at break-even after %d refreshes, $%.2f",
			t.DisplayTitle(), v.count, v.spent)
		payload["cold_at"] = v.expiry
	}
	k.broadcast("keepalive", payload)
}

// suspend stops keep-alive for the room and says so.
func (k *keepalive) suspend(reason string) {
	if err := k.st.SetKeepaliveSuspended(reason); err != nil {
		return
	}
	log.Printf("[atrium] keep-alive suspended: %s", reason)
	k.broadcast("keepalive", map[string]any{
		"suspended": reason, "toast": "keep-alive suspended for this room: " + reason,
	})
}

// view is a card's keep-alive as the board reads it, or nil for a card with no
// switch.
func (k *keepalive) view(taskID string) any {
	card, err := k.st.KeepaliveCardOf(taskID)
	if err != nil || card == nil {
		return nil
	}
	out := &keepaliveCardView{State: card.State, StateAt: card.StateAt}
	k.mu.Lock()
	out.Why = k.why[taskID]
	k.mu.Unlock()
	t, err := k.st.Get(taskID)
	if err != nil || t == nil || t.ResumeID == "" {
		return out
	}
	path := k.transcript(t.Worktree, t.ResumeID)
	if path == "" {
		return out
	}
	r, err := readLastReply(path)
	if err != nil {
		return out
	}
	_, _, spent, count, expiry := k.stretchState(taskID, card, r)
	out.Spent, out.Refreshes = spent, count
	if p, ok := keepalivePriceFor(r.Model); ok {
		out.Budget = budgetFor(r.Context, p)
	}
	if r.TTL > 0 {
		out.WarmUntil = &expiry
	}
	return out
}

// loop runs the tick until the daemon stops.
func (k *keepalive) loop(ctx context.Context) {
	t := time.NewTicker(keepaliveTick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			k.tick(ctx)
		}
	}
}

// keepaliveAtLaunch gives a NEW Claude card its switch from the room default,
// and pins its main conversation to the 1h cache unless the switch is off.
// Called from Launch, which owns the environment.
//
// A card that already has a switch keeps it. Launch also runs when a card is
// reopened after a restart or resumed, and the board default applies to new
// cards only: a card somebody turned off stays off.
func (d *Daemon) keepaliveAtLaunch(taskID string, h *store.Harness, env map[string]string) {
	if !isClaude(h) {
		return
	}
	state := store.KeepaliveOff
	if card, err := d.st.KeepaliveCardOf(taskID); err == nil && card != nil {
		state = card.State
	} else {
		if d.st.KeepaliveDefaultOn() {
			state = store.KeepaliveOn
		}
		if _, err := d.st.SetKeepaliveState(taskID, state); err != nil {
			log.Printf("[atrium] keep-alive: could not set %s on %s: %v", state, taskID, err)
			return
		}
	}
	// The runner row's own choice wins. An inherited value never reaches the
	// runner anyway: childEnvFrom drops every CLAUDE_CODE_ key from the base.
	if _, set := h.Env[keepaliveTTLVar]; state != store.KeepaliveOff && !set && env != nil {
		env[keepaliveTTLVar] = "1h"
	}
}

// keepaliveTTLVar is Claude Code's choice of TTL for the main conversation.
const keepaliveTTLVar = "CLAUDE_CODE_PROMPT_CACHE_TTL"

// keepaliveSet is the per-card switch from the board. Turning it on by hand
// clears any stopped state and restarts the budget.
func (d *Daemon) keepaliveSet(taskID string, on bool) (any, error) {
	t, err := d.st.Get(taskID)
	if err != nil {
		return nil, err
	}
	h, _ := d.st.Harness(t.Runner)
	if !isClaude(h) {
		return nil, errors.New("keep-alive is for Claude cards only")
	}
	state := store.KeepaliveOff
	if on {
		state = store.KeepaliveOn
	}
	if _, err := d.st.SetKeepaliveStateAt(taskID, state, d.ka.now()); err != nil {
		return nil, err
	}
	// A hand on the switch also lifts a hold from an unsaved refresh.
	d.ka.mu.Lock()
	delete(d.ka.unsaved, taskID)
	d.ka.mu.Unlock()
	return d.ka.view(taskID), nil
}
