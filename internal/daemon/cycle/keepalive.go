package cycle

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/dovholuknf/atrium/internal/base/cardroot"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dovholuknf/atrium/internal/api"
	"github.com/dovholuknf/atrium/internal/base/nowindow"
	"github.com/dovholuknf/atrium/internal/daemon/shared"
	"github.com/dovholuknf/atrium/internal/small"
	"github.com/dovholuknf/atrium/internal/store"
)

// THE CACHE KEEP-ALIVE. See docs/runtime/cache-keepalive-design.md, which is the
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
const KeepaliveTick = time.Minute

// keepaliveForkTimeout bounds one refresh. A fork is a process start and one
// API call.
const KeepaliveForkTimeout = 120 * time.Second

// Policy defaults. See the design's "Policy" and "The stop rule".
const (
	// keepaliveMargin is how close to expiry a refresh fires.
	KeepaliveMargin = 5 * time.Minute
	// keepaliveMinContext is the smallest context worth keeping warm. Below it
	// a saved wake is worth under $0.40 on Opus 5.5.
	KeepaliveMinContext = 50_000
	// keepaliveBudgetFraction is the stop rule: refresh spend since the card
	// went idle may reach this fraction of one full 1h rehydration of its
	// context. 1/8 fits the resume data best across models.
	KeepaliveBudgetFraction = 0.125
	// keepaliveWarmRead is the receipt threshold, as a fraction of the card's
	// context. There is no write threshold: a fork always writes its own tail,
	// the card's last reply and the prompt, and on a 55k card that tail alone
	// passed the old 5% while the fork read 99.99%.
	KeepaliveWarmRead = 0.90
	// keepaliveForkTail is the tokens a fork writes at the 1h price on top of its read: the card's last
	// reply and the prompt, measured near 2,000 in the ledger.
	KeepaliveForkTail = 2_000
)

// keepalivePrompt is what the fork is asked. The words do not matter to the
// cache, only to what the model does with its one turn.
const KeepalivePrompt = "Automated cache refresh from atrium. Reply with the single word OK. " +
	"Do not use tools. Do not continue any task."

// keepaliveHookSettings is the settings file layered onto every fork: one
// PreToolUse hook that refuses every tool. Hooks are not part of the request, so
// this keeps the prefix whole where a deny rule would not.
const KeepaliveHookSettings = `{
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
const KeepalivePricesVersion = "usageprices-2026-09-27"

// keepalivePrice is per million tokens: the 5m and 1h writes and the cache read, plus
// plain input and output for the fork's own few tokens. Cw5m is 1.25 times input,
// Anthropic's list rule, and is written out so a row's price is read and not derived.
type KeepalivePrice struct{ In, Cw5m, Cw1h, Cr, Out float64 }

// keepalivePrices mirrors `$UsagePrices` in dotfiles/claude/usage/UsageCommon.ps1
// for the models a card may be refreshed on. Only models whose effort level is
// not part of the cache key are here, because atrium cannot know a card's
// current effort, and a fork at another effort would write the whole context.
var KeepalivePrices = map[string]KeepalivePrice{
	"claude-opus-5-5":   {In: 4, Cw5m: 5, Cw1h: 8, Cr: 0.20, Out: 20},
	"claude-fable-5-1":  {In: 10, Cw5m: 12.5, Cw1h: 20, Cr: 0.25, Out: 50},
	"claude-sonnet-5-5": {In: 2, Cw5m: 2.5, Cw1h: 4, Cr: 0.10, Out: 10},
}

// keepalivePriceFor looks a model up with any `[1m]` style suffix and date
// stamp tolerated.
func KeepalivePriceFor(model string) (KeepalivePrice, bool) {
	m := strings.ToLower(strings.TrimSpace(model))
	if i := strings.IndexByte(m, '['); i >= 0 {
		m = m[:i]
	}
	for k, p := range KeepalivePrices {
		if m == k || strings.HasPrefix(m, k+"-") {
			return p, true
		}
	}
	return KeepalivePrice{}, false
}

// lastReply is what a card's transcript says about its last main reply. Every
// fact the policy needs about the card comes from here and never from a fork.
type LastReply struct {
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
const TranscriptTail = 2 << 20

// mainReply is one line of a transcript that carries a main-thread assistant
// reply's usage. A reply written in several content blocks is several lines
// with the same MessageID and the same usage, so a caller that sums keeps one
// per id.
type MainReply struct {
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
	// Text is what the reply said, joined, and ToolUse is whether it called a tool. A reply written in several
	// lines carries what all of them did once it has been through replySet.take. The turn cost report reads them.
	Text    string
	ToolUse bool
}

// Context is the whole prompt the reply was answered on.
func (r *MainReply) Context() int64 { return r.Input + r.CacheWrite + r.CacheRead }

// replyContent is the words and whether there was a tool call in an assistant reply's content blocks. Best effort:
// content that is not a list of blocks says nothing.
func ReplyContent(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 || raw[0] != '[' {
		return "", false
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) != nil {
		return "", false
	}
	var text strings.Builder
	tool := false
	for _, b := range blocks {
		switch b.Type {
		case "text":
			text.WriteString(b.Text)
		case "tool_use", "server_tool_use":
			tool = true
		}
	}
	return text.String(), tool
}

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
func ScanMainReplies(r io.Reader, fn func(*MainReply)) (int64, error) {
	return ScanReplies(r, func(m *MainReply) {
		if !m.Sidechain {
			fn(m)
		}
	})
}

// scanReplies is scanMainReplies with the inline subagent replies offered too,
// marked Sidechain. The usage record counts those as the subagent's.
func ScanReplies(r io.Reader, fn func(*MainReply)) (int64, error) {
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
				// Content is a list of blocks for an assistant reply. Read loosely: a shape that is not one is no text.
				Content json.RawMessage `json:"content"`
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
		text, tool := ReplyContent(e.Message.Content)
		fn(&MainReply{
			Text: text, ToolUse: tool,
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

// lastReplyCap bounds the read cache by count. The oldest entry goes first.
const LastReplyCap = 400

// lastReplyEntry is what one transcript read found, and where in the file it
// stopped, so a grown file is read from there and an unchanged one not at all.
type LastReplyEntry struct {
	Size   int64
	Mtime  time.Time
	Offset int64 // bytes of complete lines scanned, from the start of the file
	From   int64 // where the scan began, nonzero when the file was over the tail
	Reply  LastReply
	Ttl    time.Duration
}

// lastReplies is readLastReply's memory, one per daemon. It has its own lock and never k.mu,
// so the list handler's view stays cheap. The zero value is ready.
type LastReplies struct {
	Mu    sync.Mutex
	M     map[string]*LastReplyEntry
	Order []string
	// scans counts transcript scans, for tests.
	Scans atomic.Int64
}

func (c *LastReplies) LastReplyLoad(path string) (LastReplyEntry, bool) {
	c.Mu.Lock()
	defer c.Mu.Unlock()
	e := c.M[path]
	if e == nil {
		return LastReplyEntry{}, false
	}
	return *e, true
}

func (c *LastReplies) LastReplyStore(path string, e LastReplyEntry) {
	c.Mu.Lock()
	defer c.Mu.Unlock()
	if c.M == nil {
		c.M = map[string]*LastReplyEntry{}
	}
	if _, ok := c.M[path]; !ok {
		c.Order = append(c.Order, path)
		for len(c.Order) > LastReplyCap {
			delete(c.M, c.Order[0])
			c.Order = c.Order[1:]
		}
	}
	c.M[path] = &e
}

func (c *LastReplies) LastReplyDrop(path string) {
	c.Mu.Lock()
	defer c.Mu.Unlock()
	if _, ok := c.M[path]; !ok {
		return
	}
	delete(c.M, path)
	for i, p := range c.Order {
		if p == path {
			c.Order = append(c.Order[:i], c.Order[i+1:]...)
			break
		}
	}
}

// readLastReply finds the last main-thread assistant reply with usage in a
// transcript. The answer is cached per path on the file's size and mtime. A
// grown file is scanned from the cached offset when the whole file is inside the
// tail window, where that gives the same answer as a whole read. A file that
// shrank or whose mtime went back is read whole.
func (c *LastReplies) ReadLastReply(path string) (*LastReply, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	prev, have := c.LastReplyLoad(path)
	if have && prev.Size == info.Size() && prev.Mtime.Equal(info.ModTime()) {
		r := prev.Reply
		r.TTL = prev.Ttl
		return &r, nil
	}
	var (
		start int64
		out   *LastReply
		ttl   time.Duration
	)
	switch {
	case have && info.Size() > prev.Size && !info.ModTime().Before(prev.Mtime) &&
		prev.From == 0 && info.Size() <= TranscriptTail:
		start = prev.Offset
		r := prev.Reply
		out, ttl = &r, prev.Ttl
	case info.Size() > TranscriptTail:
		start = info.Size() - TranscriptTail
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if start > 0 {
		if _, err := f.Seek(start, io.SeekStart); err != nil {
			return nil, err
		}
	}
	c.Scans.Add(1)
	done, err := ScanMainReplies(f, func(r *MainReply) {
		switch {
		case r.Write1h > 0:
			ttl = time.Hour
		case r.Write5m > 0:
			ttl = 5 * time.Minute
		}
		out = &LastReply{Model: r.Model, At: r.At, Speed: r.Speed, Context: r.Context()}
	})
	if err != nil {
		c.LastReplyDrop(path)
		return nil, err
	}
	if out == nil {
		c.LastReplyDrop(path)
		return nil, errors.New("no main reply in the transcript")
	}
	// Size and mtime are from before the read. A write landing mid-read makes
	// the next call see a change and scan again, never a stale answer.
	c.LastReplyStore(path, LastReplyEntry{
		Size: info.Size(), Mtime: info.ModTime(), Offset: start + done, From: start,
		Reply: *out, Ttl: ttl,
	})
	out.TTL = ttl
	return out, nil
}

// forkReceipt is the part of `claude -p --output-format json` a refresh reads.
type ForkReceipt struct {
	Subtype           string            `json:"subtype"`
	IsError           bool              `json:"is_error"`
	Result            string            `json:"result"`
	NumTurns          int               `json:"num_turns"`
	SessionID         string            `json:"session_id"`
	PermissionDenials []json.RawMessage `json:"permission_denials"`
	// ModelUsage is keyed by the model that answered, so a caller that did not
	// choose the model can still price the call.
	ModelUsage map[string]json.RawMessage `json:"modelUsage"`
	Usage      *struct {
		Input      int64 `json:"input_tokens"`
		CacheWrite int64 `json:"cache_creation_input_tokens"`
		CacheRead  int64 `json:"cache_read_input_tokens"`
		Output     int64 `json:"output_tokens"`
	} `json:"usage"`
}

// Receipt outcomes, in the order they are tested. See the design's "Reading the
// receipt".
const (
	OutcomeActed   = "acted"
	OutcomeRefused = "refused"
	OutcomeFailed  = "failed"
	OutcomeWarmed  = "warmed"
	OutcomeMiss    = "miss"
)

// classifyReceipt scores one fork. ctx is the card's context from its own
// transcript. A nil receipt, or one with no usage, is `failed`.
func ClassifyReceipt(r *ForkReceipt, runErr error, ctx int64) string {
	if r != nil && r.NumTurns > 1 && len(r.PermissionDenials) == 0 {
		return OutcomeActed
	}
	if r != nil && len(r.PermissionDenials) > 0 {
		return OutcomeRefused
	}
	if runErr != nil || r == nil || r.Usage == nil || r.Subtype != "success" {
		return OutcomeFailed
	}
	if ctx > 0 && float64(r.Usage.CacheRead) >= KeepaliveWarmRead*float64(ctx) {
		return OutcomeWarmed
	}
	return OutcomeMiss
}

// receiptCost prices a fork's tokens. Cache writes are priced at the 1h rate,
// because only cards on the 1h cache are refreshed.
func ReceiptCost(r *ForkReceipt, p KeepalivePrice) float64 {
	if r == nil || r.Usage == nil {
		return 0
	}
	u := r.Usage
	return (float64(u.Input)*p.In + float64(u.CacheWrite)*p.Cw1h + float64(u.CacheRead)*p.Cr +
		float64(u.Output)*p.Out) / 1e6
}

// forkSpec is one refresh's command line.
type ForkSpec struct {
	Exe  string
	Args []string
	Dir  string
	Env  []string
	// Stdin is the prompt for a caller whose prompt is too long for a command
	// line. Nil for keep-alive.
	Stdin []byte
	// Timeout bounds the run. Zero is keepaliveForkTimeout.
	Timeout time.Duration
}

// runForkProcess runs a fork and returns its stdout. The default fork runner.
func RunForkProcess(ctx context.Context, spec ForkSpec, agentFork func(exe string, args []string) (string, []string)) ([]byte, error) {
	timeout := KeepaliveForkTimeout
	if spec.Timeout > 0 {
		timeout = spec.Timeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	exe, args := agentFork(spec.Exe, spec.Args)
	if resolved, err := exec.LookPath(exe); err == nil {
		exe, args = shared.ViaShellIfScript(resolved, args)
	}
	cmd := exec.CommandContext(ctx, exe, args...)
	cmd.Dir = spec.Dir
	cmd.Env = spec.Env
	cmd.Stdin = nil
	if spec.Stdin != nil {
		cmd.Stdin = bytes.NewReader(spec.Stdin)
	}
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	nowindow.Hide(cmd)
	err := cmd.Run()
	if err != nil && out.Len() == 0 {
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(small.FirstLine(errb.String())))
	}
	return out.Bytes(), err
}

// keepaliveCardView is what the board draws for one card.
type KeepaliveCardView struct {
	State   string    `json:"state"`
	StateAt time.Time `json:"state_at"`
	// Why is the reason a card cannot be warmed, for the greyed switch, or the
	// last skip reason while it is on.
	Why string `json:"why,omitempty"`
	// Refreshes, Missed, Spent and Budget describe the current idle stretch.
	// Refreshes counts the warmed ones and Missed the misses, which are in Spent
	// too: one miss rewrites the whole context, about eight budgets.
	Refreshes int     `json:"refreshes"`
	Missed    int     `json:"missed,omitempty"`
	Spent     float64 `json:"-"`
	Budget    float64 `json:"-"`
	// WarmUntil is when the card's cache expires, when atrium knows.
	WarmUntil *time.Time `json:"warm_until,omitempty"`
	// LastRefreshAt is the last refresh of the current idle stretch, absent when
	// there was none. NextRefreshAt is when the loop will next refresh the card,
	// sent only while decide() says "not due".
	LastRefreshAt *time.Time `json:"last_refresh_at,omitempty"`
	NextRefreshAt *time.Time `json:"next_refresh_at,omitempty"`
	// Suspended is why the whole room is suspended, so a card's badge does not
	// blame the card.
	Suspended string `json:"suspended,omitempty"`
}

// keepalive holds the loop's collaborators, so a test can run a tick with a fake
// fork, a fake clock and hand-written transcripts.
type Keepalive struct {
	// replies is the transcript read cache. The daemon points it at its own.
	Replies *LastReplies
	// agentFork and cardArgs are the seams a fork reads. The daemon points them at its hooks.
	AgentFork func(exe string, args []string) (string, []string)
	CardArgs  func(h *store.Harness, t *store.Task) ([]string, error)
	// EffortArgs is the card's launch effort in the shape its harness declares.
	EffortArgs func(h *store.Harness, t *store.Task) ([]string, error)
	// ForkEnvOf is the fork's environment over base: the one a launch builds, minus the card's identity.
	ForkEnvOf func(h *store.Harness, t *store.Task, base []string) ([]string, error)
	St        *store.Store
	Now       func() time.Time
	// fork runs one refresh and returns the receipt's bytes.
	Fork func(ctx context.Context, spec ForkSpec) ([]byte, error)
	// transcript finds a card's transcript from its directory and session id.
	Transcript func(cwd, sessionID string) string
	// window is the card's context window from telemetry, or 0.
	Window func(taskID string) int
	// broadcast tells the board, and toast raises one on it.
	Broadcast func(event string, payload any)
	// hookFile is the path of the block-all settings file.
	HookFile string
	// baseEnv is the environment a fork starts from.
	BaseEnv func() []string
	// record saves one refresh's ledger row.
	Record func(*store.KeepaliveRefresh) error
	// spent saves one refresh's row in the card's usage record. See usage.go.
	Spent func(*store.SessionUsage) error
	// holding says a new-context cycle is under way on the card, and session is the
	// session its runner last started, which moves on at a `/clear` while the resume
	// id lags. Both nil in a test that has no daemon.
	Holding func(taskID string) bool
	Session func(t *store.Task) string
	// deployHeld says the card is held for a room deploy, which ends its runner
	// and resumes it cold, so a refresh buys nothing. Nil in a test with no daemon.
	DeployHeld func(taskID string) bool
	// cold starts the new-context cycle on a card whose cache is going cold. Nil in a test that has no daemon.
	// See keepalive_cold.go.
	Cold func(t *store.Task) error

	Mu sync.Mutex
	// cooled is the last reply a card was handed off on, per card.
	Cooled map[string]time.Time
	// lastMissCard is the card of the room's most recent attempt when that
	// attempt was a miss, for "two misses on different cards in a row".
	LastMissCard string
	// why is the last skip reason per card, for the board.
	Why map[string]string
	// inFlight stops a slow fork from being started twice.
	InFlight map[string]bool
	// unsaved holds back a card whose last refresh row could not be saved. The
	// warm window and the budget come from saved rows, so without it the next
	// tick would see the old expiry and fork again.
	Unsaved map[string]UnsavedRefresh
}

// unsavedRefresh holds a card's refreshes back until its cache would expire, or
// until the card takes a real turn after the fork.
type UnsavedRefresh struct {
	At, Until time.Time
	Why       string
}

func NewKeepalive(st *store.Store) *Keepalive {
	var k *Keepalive
	k = &Keepalive{
		St:      st,
		Replies: &LastReplies{},
		Now:     func() time.Time { return time.Now().UTC() },
		Fork: func(ctx context.Context, spec ForkSpec) ([]byte, error) {
			return RunForkProcess(ctx, spec, k.AgentFork)
		},
		Transcript: api.TranscriptPath,
		Window:     func(string) int { return 0 },
		Broadcast:  func(string, any) {},
		HookFile:   filepath.Join(cardroot.StateDir(), "keepalive-block-tools.json"),
		BaseEnv:    os.Environ,
		Why:        map[string]string{},
		InFlight:   map[string]bool{},
		Unsaved:    map[string]UnsavedRefresh{},
		Cooled:     map[string]time.Time{},
		Record:     st.AddKeepaliveRefresh,
		Spent:      st.AddSessionUsage,
	}
	return k
}

// ensureHookFile writes the block-all settings file if it is missing or wrong.
func (k *Keepalive) EnsureHookFile() error {
	if got, err := os.ReadFile(k.HookFile); err == nil && string(got) == KeepaliveHookSettings {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(k.HookFile), 0o755); err != nil {
		return err
	}
	return os.WriteFile(k.HookFile, []byte(KeepaliveHookSettings), 0o600)
}

// localHasHooks reports whether a card's `.claude/settings.local.json` names
// hooks. `--setting-sources local` still loads that file, so its hooks would run
// in the fork.
func LocalHasHooks(cwd string) bool {
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
type Verdict struct {
	Act     string // "refresh", "skip", "break-even"
	Why     string
	Reply   *LastReply
	H       *store.Harness
	Price   KeepalivePrice
	Budget  float64
	Spent   float64
	Count   int
	Expiry  time.Time
	Anchor  time.Time
	History []*store.KeepaliveRefresh
}

// stretchState reads the current idle stretch of a card: the refreshes since
// its budget last restarted, their spend, and when the cache expires.
//
// Two different starting points. The BUDGET restarts at the later of the last
// real reply and the switch last being set, so turning it on by hand gives a
// fresh budget. The WARM WINDOW counts every warmed refresh since the last real
// reply, because a refresh made before a hand re-enable still refreshed the
// cache.
func (k *Keepalive) StretchState(taskID string, card *store.KeepaliveCard, r *LastReply) (
	anchor time.Time, rows []*store.KeepaliveRefresh, spent float64, count, missed int, expiry time.Time, last time.Time) {
	anchor = r.At
	// Only a switch turned ON moves the anchor. A stop is stamped too, and the
	// stopped card's tooltip wants the spend that led to the stop.
	if card != nil && card.State == store.KeepaliveOn && card.StateAt.After(anchor) {
		anchor = card.StateAt
	}
	all, _ := k.St.KeepaliveRefreshesSince(taskID, r.At)
	warm := r.At
	for _, row := range all {
		if row.At.After(last) {
			last = row.At
		}
		if row.Outcome == OutcomeWarmed && row.At.After(warm) {
			warm = row.At
		}
		if !row.At.After(anchor) {
			continue
		}
		rows = append(rows, row)
		spent += row.Cost
		switch row.Outcome {
		case OutcomeWarmed:
			count++
		case OutcomeMiss:
			missed++
		}
	}
	return anchor, rows, spent, count, missed, warm.Add(r.TTL), last
}

// followSession is the card with its resume id moved to the session its runner last
// started. After a new context the cache worth keeping warm is the new session's, and
// the stored id names the old one until the next Stop.
func (k *Keepalive) FollowSession(t *store.Task) *store.Task {
	if k.Session == nil {
		return t
	}
	s := k.Session(t)
	if s == "" || s == t.ResumeID {
		return t
	}
	cp := *t
	cp.ResumeID = s
	return &cp
}

// decide applies the design's seven rules to one card. It reads, and writes
// nothing.
func (k *Keepalive) Decide(t *store.Task, card *store.KeepaliveCard) Verdict {
	skip := func(why string) Verdict { return Verdict{Act: "skip", Why: why} }
	if card == nil || card.State != store.KeepaliveOn {
		return skip("")
	}
	if k.ColdSpent(t.ID) {
		return skip(WhyColdHandedOff)
	}
	// Its context is about to be cleared, and after it the cache is a new one.
	if k.Holding != nil && k.Holding(t.ID) {
		return skip("new context running")
	}
	if k.DeployHeld != nil && k.DeployHeld(t.ID) {
		return skip("room deploy hold")
	}
	h, err := k.St.Harness(t.Runner)
	if err != nil || !shared.IsClaude(h) {
		return skip("not a Claude card")
	}
	// A parked card's cache is meant to go cold: the park chose to let it go.
	if small.IsParked(t) {
		return skip("parked")
	}
	if strings.TrimSpace(t.ResumeID) == "" || strings.TrimSpace(t.Worktree) == "" {
		return skip("no session id yet")
	}
	if t.Status != store.StatusNeedsInput {
		return skip("not idle")
	}
	if pend, _ := k.St.PendingForTask(t.ID); len(pend) > 0 {
		return skip("a permission dialog is open")
	}
	path := k.Transcript(t.Worktree, t.ResumeID)
	if path == "" {
		return skip("no transcript")
	}
	r, err := k.Replies.ReadLastReply(path)
	if err != nil {
		return skip("no transcript")
	}
	price, ok := KeepalivePriceFor(r.Model)
	if !ok {
		return skip("model " + r.Model + " keys its cache on effort")
	}
	if strings.EqualFold(r.Speed, "fast") {
		return skip("fast mode")
	}
	if r.TTL != time.Hour {
		return skip("not on the 1h cache")
	}
	if r.Context < KeepaliveMinContext {
		return skip("context under 50k")
	}
	if LocalHasHooks(t.Worktree) {
		return skip("local hooks")
	}
	anchor, rows, spent, count, _, expiry, _ := k.StretchState(t.ID, card, r)
	now := k.Now()
	if !now.Before(expiry) {
		return skip("cache already cold")
	}
	if now.Before(RefreshDueAt(expiry)) {
		return Verdict{Act: "skip", Why: "not due", Expiry: expiry, Spent: spent, Count: count, Reply: r,
			Price: price, Budget: BudgetFor(r.Context, price)}
	}
	v := Verdict{
		Act: "refresh", Reply: r, H: h, Price: price, Budget: BudgetFor(r.Context, price),
		Spent: spent, Count: count, Expiry: expiry, Anchor: anchor, History: rows,
	}
	next := (float64(r.Context)*price.Cr + KeepaliveForkTail*price.Cw1h) / 1e6
	if spent+next > v.Budget {
		v.Act, v.Why = "break-even", "budget spent"
	}
	return v
}

// refreshDueAt is when a card whose cache expires at expiry becomes due for a
// refresh. decide() and the card view both read it, so they cannot drift.
func RefreshDueAt(expiry time.Time) time.Time { return expiry.Add(-KeepaliveMargin) }

// budgetFor is the stop rule's cap: a fraction of one full 1h rehydration.
func BudgetFor(ctx int64, p KeepalivePrice) float64 {
	return KeepaliveBudgetFraction * float64(ctx) * p.Cw1h / 1e6
}

// modelArg is the model to fork on. A card whose telemetry says it has a window
// over 200k runs the 1M variant, which is a different request.
func (k *Keepalive) ModelArg(taskID, model string) string {
	if w := k.Window(taskID); w > 200_000 && !strings.Contains(model, "[") {
		return model + "[1m]"
	}
	return model
}

// ForkEnv is the fork's environment, built by the launch code the root points ForkEnvOf at.
func (k *Keepalive) ForkEnv(h *store.Harness, t *store.Task) ([]string, error) {
	return k.ForkEnvOf(h, t, k.BaseEnv())
}

// forkCarried are the launch flags that shape the cached request, which is the
// system prompt and the tool list. Everything else in a card's launch is dropped
// from a fork on purpose: see docs/runtime/keepalive-fork-args-design.md.
var ForkCarried = map[string]bool{
	"--append-system-prompt": true, "--system-prompt": true,
	"--append-system-prompt-file": true, "--system-prompt-file": true,
	"--disallowedtools": true, "--disallowed-tools": true, "--tools": true,
	"--mcp-config": true, "--strict-mcp-config": true,
	"--agents": true, "--plugin-dir": true,
}

// keepFlags keeps the allowlisted flags of args with their values. A flag's values
// are the tokens up to the next flag, and `--flag=value` is one token. An unknown
// flag is dropped along with its values, and a bare token before any flag (a
// prompt) is dropped.
func KeepFlags(args []string) []string {
	var out []string
	keeping := false
	for _, a := range args {
		if strings.HasPrefix(a, "-") && a != "-" {
			name, _, _ := strings.Cut(a, "=")
			keeping = ForkCarried[strings.ToLower(name)]
		}
		if keeping {
			out = append(out, a)
		}
	}
	return out
}

// forkArgs is the refresh command line. Kept in one place so the test pins it.
func (k *Keepalive) ForkArgs(sessionID, model string) []string {
	return []string{
		"-p", KeepalivePrompt,
		"--resume", sessionID, "--fork-session", "--no-session-persistence",
		"--model", model,
		"--setting-sources", "local",
		"--settings", k.HookFile,
		"--max-turns", "1",
		"--output-format", "json",
	}
}

// tick looks at every card with keep-alive on, once.
func (k *Keepalive) Tick(ctx context.Context) {
	cards, err := k.St.KeepaliveCards()
	if err != nil {
		return
	}
	for _, card := range cards {
		if ctx.Err() != nil {
			return
		}
		t, err := k.St.Get(card.TaskID)
		if err != nil || t == nil {
			continue
		}
		t = k.FollowSession(t)
		card = k.ClearOnRealTurn(t, card)
		// A suspended room refreshes nothing, but its cards still clear on a real
		// turn: otherwise a stopped badge stays stuck while the card works.
		// The suspension itself is not cleared by a turn. It follows two misses on
		// two cards, which says the cache is not holding for a reason a turn does
		// not fix, and a refresh after it spends real money. A person clears it.
		if k.St.KeepaliveSuspended() != "" {
			k.ColdCheck(t, card, false)
			continue
		}
		v := k.Decide(t, card)
		k.Mu.Lock()
		holding := false
		if u, held := k.Unsaved[t.ID]; held {
			if !k.Now().Before(u.Until) || (v.Reply != nil && v.Reply.At.After(u.At)) {
				delete(k.Unsaved, t.ID)
			} else if v.Act != "skip" || v.Why == "not due" {
				v = Verdict{Act: "skip", Why: u.Why}
				holding = true
			}
		}
		k.Why[t.ID] = v.Why
		busy := k.InFlight[t.ID]
		k.Mu.Unlock()
		switch v.Act {
		case "break-even":
			k.Stop(t, store.KeepaliveBreakEven, v)
			k.ColdCheck(t, card, false)
		case "refresh":
			if busy {
				continue
			}
			k.Refresh(ctx, t, v)
		default:
			k.ColdCheck(t, card, holding || v.Why == "not due")
		}
		if k.St.KeepaliveSuspended() != "" {
			return
		}
	}
}

// clearOnRealTurn puts a self-clearing stopped card back on once the card has
// taken a real turn since it stopped.
func (k *Keepalive) ClearOnRealTurn(t *store.Task, card *store.KeepaliveCard) *store.KeepaliveCard {
	switch card.State {
	case store.KeepaliveBreakEven, store.KeepaliveMiss, store.KeepaliveFailing:
	default:
		return card
	}
	path := k.Transcript(t.Worktree, t.ResumeID)
	if path == "" {
		return card
	}
	r, err := k.Replies.ReadLastReply(path)
	if err != nil || !r.At.After(card.StateAt) {
		return card
	}
	next, err := k.St.SetKeepaliveStateAt(t.ID, store.KeepaliveOn, k.Now())
	if err != nil {
		return card
	}
	k.Broadcast("keepalive", map[string]any{"task_id": t.ID, "state": next.State})
	return next
}

// refresh runs one fork, records it, and applies what the receipt says.
func (k *Keepalive) Refresh(ctx context.Context, t *store.Task, v Verdict) {
	k.Mu.Lock()
	k.InFlight[t.ID] = true
	k.Mu.Unlock()
	defer func() {
		k.Mu.Lock()
		delete(k.InFlight, t.ID)
		k.Mu.Unlock()
	}()
	if err := k.EnsureHookFile(); err != nil {
		log.Printf("[atrium] keep-alive: could not write %s: %v", k.HookFile, err)
		return
	}
	model := k.ModelArg(t.ID, v.Reply.Model)
	// Refused, never forked without them: a fork on the wrong env or effort
	// writes a cache the card never reads.
	env, err := k.ForkEnv(v.H, t)
	var effort, carried []string
	if err == nil {
		effort, err = k.EffortArgs(v.H, t)
	}
	if err == nil {
		carried, err = k.CardArgs(v.H, t)
	}
	if err != nil {
		log.Printf("[atrium] keep-alive: not refreshing %s: %v", t.ID, err)
		k.Mu.Lock()
		k.Why[t.ID] = "launch options: " + err.Error()
		k.Mu.Unlock()
		return
	}
	spec := ForkSpec{
		Exe: v.H.Exe(), Dir: t.Worktree, Env: env,
		Args: append(append(k.ForkArgs(t.ResumeID, model), carried...), effort...),
	}
	sent := k.Now()
	out, runErr := k.Fork(ctx, spec)
	var rec *ForkReceipt
	if len(bytes.TrimSpace(out)) > 0 {
		var r ForkReceipt
		if err := json.Unmarshal(bytes.TrimSpace(out), &r); err == nil {
			rec = &r
		}
	}
	outcome := ClassifyReceipt(rec, runErr, v.Reply.Context)
	row := &store.KeepaliveRefresh{
		TaskID: t.ID, ResumeID: t.ResumeID, At: sent, Model: model, Outcome: outcome,
		Context: v.Reply.Context, TTLLeftS: int64(v.Expiry.Sub(sent).Seconds()),
		SpentBefore: v.Spent, Budget: v.Budget, Prices: KeepalivePricesVersion,
		Cost: ReceiptCost(rec, v.Price),
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
		if err := k.Spent(shared.UsageOfRefresh(row)); err != nil {
			log.Printf("[atrium] keep-alive: could not record the token use of a refresh of %s: %v", t.ID, err)
		}
	}
	if err := k.Record(row); err != nil {
		log.Printf("[atrium] keep-alive: could not record a refresh of %s: %v", t.ID, err)
		// The fork may have warmed the cache, so hold until that cache would expire.
		until := sent.Add(v.Reply.TTL)
		if v.Expiry.After(until) {
			until = v.Expiry
		}
		u := UnsavedRefresh{At: sent, Until: until,
			Why: "a refresh could not be saved, paused until " + until.Local().Format("15:04")}
		k.Mu.Lock()
		k.Unsaved[t.ID] = u
		k.Why[t.ID] = u.Why
		k.Mu.Unlock()
	}
	if runErr != nil {
		log.Printf("[atrium] keep-alive: fork for %s: %v", t.ID, runErr)
	}
	k.Apply(t, outcome, v)
	k.Broadcast("keepalive", map[string]any{"task_id": t.ID, "outcome": outcome})
}

// apply moves a card, or the room, on what one receipt said.
func (k *Keepalive) Apply(t *store.Task, outcome string, v Verdict) {
	k.Mu.Lock()
	prevMiss := k.LastMissCard
	if outcome == OutcomeMiss {
		k.LastMissCard = t.ID
	} else {
		k.LastMissCard = ""
	}
	k.Mu.Unlock()

	switch outcome {
	case OutcomeActed:
		k.Stop(t, store.KeepaliveActed, v)
		k.Suspend("a refresh fork on " + t.DisplayTitle() + " took a second turn with nothing refused")
	case OutcomeRefused:
		k.Stop(t, store.KeepaliveActed, v)
	case OutcomeMiss:
		k.Stop(t, store.KeepaliveMiss, v)
		if prevMiss != "" && prevMiss != t.ID {
			k.Suspend("two refreshes in a row on two cards missed the cache")
		}
	case OutcomeFailed:
		// Two failures in a row in this stretch stop the card.
		n := 0
		for i := len(v.History) - 1; i >= 0 && v.History[i].Outcome == OutcomeFailed; i-- {
			n++
		}
		if n >= 1 {
			k.Stop(t, store.KeepaliveFailing, v)
		}
	}
}

// stop sets a card's stopped state, and for break-even raises the toast.
func (k *Keepalive) Stop(t *store.Task, state string, v Verdict) {
	if _, err := k.St.SetKeepaliveStateAt(t.ID, state, k.Now()); err != nil {
		return
	}
	payload := map[string]any{
		"task_id": t.ID, "title": t.DisplayTitle(), "state": state,
		"refreshes": v.Count,
	}
	if state == store.KeepaliveBreakEven {
		payload["toast"] = fmt.Sprintf("keep-alive stopped on %s at break-even after %d refreshes",
			t.DisplayTitle(), v.Count)
		payload["cold_at"] = v.Expiry
	}
	k.Broadcast("keepalive", payload)
}

// suspend stops keep-alive for the room and says so.
func (k *Keepalive) Suspend(reason string) {
	if err := k.St.SetKeepaliveSuspended(reason); err != nil {
		return
	}
	log.Printf("[atrium] keep-alive suspended: %s", reason)
	k.Broadcast("keepalive", map[string]any{
		"suspended": reason, "toast": "keep-alive suspended for this room: " + reason,
	})
}

// view is a card's keep-alive as the board reads it, or nil for a card with no
// switch.
func (k *Keepalive) View(taskID string) any {
	card, err := k.St.KeepaliveCardOf(taskID)
	if err != nil || card == nil {
		return nil
	}
	out := &KeepaliveCardView{State: card.State, StateAt: card.StateAt, Suspended: k.St.KeepaliveSuspended()}
	k.Mu.Lock()
	out.Why = k.Why[taskID]
	k.Mu.Unlock()
	t, err := k.St.Get(taskID)
	if err != nil || t == nil {
		return out
	}
	return k.ViewOf(t, card, out)
}

// viewTask is view for a caller that already holds the card's row.
func (k *Keepalive) ViewTask(t *store.Task) any {
	card, err := k.St.KeepaliveCardOf(t.ID)
	if err != nil || card == nil {
		return nil
	}
	out := &KeepaliveCardView{State: card.State, StateAt: card.StateAt, Suspended: k.St.KeepaliveSuspended()}
	k.Mu.Lock()
	out.Why = k.Why[t.ID]
	k.Mu.Unlock()
	return k.ViewOf(t, card, out)
}

func (k *Keepalive) ViewOf(t *store.Task, card *store.KeepaliveCard, out *KeepaliveCardView) any {
	taskID := t.ID
	// As tick does, so the view after a new context reads the new session.
	if t = k.FollowSession(t); t.ResumeID == "" {
		return out
	}
	path := k.Transcript(t.Worktree, t.ResumeID)
	if path == "" {
		return out
	}
	r, err := k.Replies.ReadLastReply(path)
	if err != nil {
		return out
	}
	_, _, spent, count, missed, expiry, last := k.StretchState(taskID, card, r)
	out.Spent, out.Refreshes, out.Missed = spent, count, missed
	if !last.IsZero() {
		out.LastRefreshAt = &last
	}
	if v := k.Decide(t, card); v.Act == "skip" && v.Why == "not due" {
		next := RefreshDueAt(v.Expiry)
		out.NextRefreshAt = &next
	}
	if p, ok := KeepalivePriceFor(r.Model); ok {
		out.Budget = BudgetFor(r.Context, p)
	}
	if r.TTL > 0 {
		out.WarmUntil = &expiry
	}
	if at, ok := k.ColdSpentAt(taskID); ok {
		out.WarmUntil, out.Why = &at, WhyColdHandedOff
	}
	return out
}

// loop runs the tick until the daemon stops.
func (k *Keepalive) Loop(ctx context.Context) {
	t := time.NewTicker(KeepaliveTick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			k.Tick(ctx)
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
func (d *Service) KeepaliveAtLaunch(taskID string, h *store.Harness, env map[string]string) {
	if !shared.IsClaude(h) {
		return
	}
	state := store.KeepaliveOff
	if card, err := d.St.KeepaliveCardOf(taskID); err == nil && card != nil {
		state = card.State
	} else {
		if d.St.KeepaliveDefaultOn() {
			state = store.KeepaliveOn
		}
		if _, err := d.St.SetKeepaliveState(taskID, state); err != nil {
			log.Printf("[atrium] keep-alive: could not set %s on %s: %v", state, taskID, err)
			return
		}
	}
	// The runner row's own choice wins. An inherited value never reaches the
	// runner anyway: childEnvFrom drops every CLAUDE_CODE_ key from the base.
	if _, set := h.Env[KeepaliveTTLVar]; state != store.KeepaliveOff && !set && env != nil {
		env[KeepaliveTTLVar] = "1h"
	}
}

// keepaliveTTLVar is Claude Code's choice of TTL for the main conversation.
const KeepaliveTTLVar = "CLAUDE_CODE_PROMPT_CACHE_TTL"

// keepaliveSet is the per-card switch from the board. Turning it on by hand
// clears any stopped state and restarts the budget.
func (d *Service) KeepaliveSet(taskID string, on bool) (any, error) {
	t, err := d.St.Get(taskID)
	if err != nil {
		return nil, err
	}
	h, _ := d.St.Harness(t.Runner)
	if !shared.IsClaude(h) {
		return nil, errors.New("keep-alive is for Claude cards only")
	}
	state := store.KeepaliveOff
	if on {
		state = store.KeepaliveOn
	}
	if _, err := d.St.SetKeepaliveStateAt(taskID, state, d.Ka.Now()); err != nil {
		return nil, err
	}
	// A hand on the switch also lifts a hold from an unsaved refresh.
	d.Ka.Mu.Lock()
	delete(d.Ka.Unsaved, taskID)
	d.Ka.Mu.Unlock()
	return d.Ka.View(taskID), nil
}

// agentFork is agentSpawn for a keep-alive fork, which is a one-shot model call.
// A test's TestMain makes it a command that fails at once.
var AgentFork = func(exe string, args []string) (string, []string) { return exe, args }
