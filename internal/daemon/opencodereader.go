package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// opencode's transcript, for the replies endpoint (stage 1 of r-opencode-bubbles).
//
// READ THROUGH `opencode export <session id>`, never opencode's own files. Its
// database is opened and written by the running opencode, its schema is internal,
// and auth.json sits beside it. The export is the supported interface. It prints
// one non-JSON line and then the session as JSON: `messages[]`, each with
// `info.role`, `info.time.created` (ms) and `parts[]`. Only `text` parts are read:
// reasoning, tool, step-start and step-finish are not the conversation.
//
// BOUNDED. A timeout, a cap on stdout (over it is a failure, not a cut), no
// shell, the session id checked before it is an argument. At most one export is
// in flight per card, and its answer is reused for `opencodeTTL`, so a poll does
// not spawn a process per request. Any failure is an error and the screen answers.

var (
	opencodeTimeout = 8 * time.Second
	opencodeTTL     = 3 * time.Second
	// opencodeMaxOut is the most an export may print. A session over it is not read.
	opencodeMaxOut int64 = 8 << 20
)

var opencodeSessionID = regexp.MustCompile(`^ses_[A-Za-z0-9]+$`)

// opencodeWaitDelay is how long after the process exits (or is killed) its output
// pipe may stay open. A grandchild that kept stdout, as cmd.exe's child does for an
// npm .cmd shim, cannot hold a read longer than this.
var opencodeWaitDelay = time.Second

// cappedWriter keeps at most max bytes. The first byte past it marks over, calls
// stop, and fails the write, so the copy ends and the process is killed.
type cappedWriter struct {
	mu   sync.Mutex
	buf  bytes.Buffer
	max  int64
	over bool
	stop func()
}

func (w *cappedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if int64(w.buf.Len()+len(p)) > w.max {
		w.over = true
		w.stop()
		return 0, errors.New("export is over the cap")
	}
	return w.buf.Write(p)
}

// opencodeEnv is the room's environment without atrium's own: opencode loads
// plugins, and atrium's plugin would announce this session as a card's.
func opencodeEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(strings.ToUpper(kv), "ATRIUM_") {
			env = append(env, kv)
		}
	}
	return env
}

// opencodeExec runs a binary with an argv and returns its stdout, at most max
// bytes. Bounded by ctx AND by opencodeWaitDelay: stdout is a writer, not a pipe
// read to EOF, and the kill takes the whole process tree, so a child that holds
// the pipe cannot hold the call. What was printed comes back with the error, so a
// good answer printed before a timeout is kept; over the cap returns nothing.
// A variable so a test needs no opencode.
var opencodeExec = func(ctx context.Context, bin string, args []string, max int64) ([]byte, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = opencodeEnv()
	w := &cappedWriter{max: max, stop: cancel}
	cmd.Stdout = w
	cmd.WaitDelay = opencodeWaitDelay
	prepareTree(cmd)
	hideWindow(cmd)
	err := cmd.Run()
	reapTree(cmd)
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.over {
		return nil, fmt.Errorf("export is over %d bytes", max)
	}
	if err == nil && ctx.Err() != nil {
		err = ctx.Err()
	}
	return append([]byte(nil), w.buf.Bytes()...), err
}

// opencodeLookPath resolves the binary: a variable for tests.
var opencodeLookPath = exec.LookPath

type opencodeReader struct {
	d   *Daemon
	mu  sync.Mutex
	m   map[string]*opencodeCard // by card id
	sem chan struct{}            // exports in flight across cards
}

// opencodeSlots is how many cards may export at once.
const opencodeSlots = 3

type opencodeCard struct {
	mu      sync.Mutex // held across an export: one in flight per card
	session string
	at      time.Time
	replies []Reply
	prompts []Prompt
}

// opencodeSource is the daemon's opencode reader, made on first use.
func opencodeSource(d *Daemon) transcriptReader {
	d.opencodeOnce.Do(func() {
		d.opencode = &opencodeReader{d: d, m: map[string]*opencodeCard{}, sem: make(chan struct{}, opencodeSlots)}
	})
	return d.opencode
}

// isOpenCode reports whether a runner row runs opencode.
func isOpenCode(h *store.Harness) bool {
	if h == nil {
		return false
	}
	if strings.EqualFold(h.ID, "opencode") {
		return true
	}
	leaf := strings.ToLower(filepath.Base(filepath.FromSlash(strings.TrimSpace(h.Exe()))))
	for _, ext := range []string{".exe", ".cmd", ".bat", ".ps1"} {
		leaf = strings.TrimSuffix(leaf, ext)
	}
	return leaf == "opencode"
}

func (r *opencodeReader) page(t *store.Task, n int, before time.Time) (replyPage, bool, error) {
	if r.d == nil || r.d.st == nil {
		return replyPage{}, false, nil
	}
	h, err := r.d.st.Harness(t.Runner)
	if err != nil || !isOpenCode(h) {
		return replyPage{}, false, nil
	}
	session := strings.TrimSpace(t.ResumeID)
	if session == "" {
		return replyPage{}, false, nil
	}
	if !opencodeSessionID.MatchString(session) {
		err := fmt.Errorf("session id %q is not an opencode session id", session)
		logRepliesFallback("opencode:"+t.ID, err)
		return replyPage{}, false, err
	}
	rs, ps, err := r.read(t.ID, h, session)
	if err != nil {
		logRepliesFallback("opencode:"+session, err)
		return replyPage{}, false, err
	}
	var rb []Reply
	var pb []Prompt
	for _, x := range rs {
		if before.IsZero() || x.At.Before(before) {
			rb = append(rb, x)
		}
	}
	for _, x := range ps {
		if before.IsZero() || x.At.Before(before) {
			pb = append(pb, x)
		}
	}
	pg := finishPage(rb, pb, n, time.Time{})
	pg.replies, pg.prompts = append([]Reply{}, pg.replies...), append([]Prompt{}, pg.prompts...)
	return pg, true, nil
}

// read is the card's whole session as replies and prompts, oldest first, from the
// cache when it is fresh, else from one export.
func (r *opencodeReader) read(card string, h *store.Harness, session string) ([]Reply, []Prompt, error) {
	r.mu.Lock()
	c := r.m[card]
	if c == nil {
		if len(r.m) > 400 {
			r.m = map[string]*opencodeCard{}
		}
		c = &opencodeCard{}
		r.m[card] = c
	}
	r.mu.Unlock()

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.session == session && time.Since(c.at) < opencodeTTL {
		return c.replies, c.prompts, nil
	}
	bin, err := opencodeLookPath(h.Exe())
	if err != nil {
		return nil, nil, fmt.Errorf("opencode is not on PATH: %w", err)
	}
	r.sem <- struct{}{}
	ctx, cancel := context.WithTimeout(context.Background(), opencodeTimeout)
	out, err := opencodeExec(ctx, bin, []string{"export", session}, opencodeMaxOut)
	cancel()
	<-r.sem
	rs, ps, perr := parseOpencodeExport(out)
	if perr != nil {
		if err != nil {
			return nil, nil, fmt.Errorf("opencode export: %w", err)
		}
		return nil, nil, perr
	}
	// A complete answer printed before a timeout or a held pipe is kept.
	c.session, c.at, c.replies, c.prompts = session, time.Now(), rs, ps
	return rs, ps, nil
}

// parseOpencodeExport reads an export: everything before the first `{` is
// skipped. Replies are the text of each assistant message, prompts the text of
// each user message, both oldest first, each cut at replyTextMax.
func parseOpencodeExport(out []byte) ([]Reply, []Prompt, error) {
	i := bytes.IndexByte(out, '{')
	if i < 0 {
		return nil, nil, errors.New("opencode export: no JSON in the output")
	}
	var doc struct {
		Messages []struct {
			Info struct {
				Role string `json:"role"`
				Time struct {
					Created int64 `json:"created"`
				} `json:"time"`
			} `json:"info"`
			Parts []struct {
				Type      string `json:"type"`
				Text      string `json:"text"`
				Synthetic bool   `json:"synthetic"`
			} `json:"parts"`
		} `json:"messages"`
	}
	if err := json.NewDecoder(bytes.NewReader(out[i:])).Decode(&doc); err != nil {
		return nil, nil, fmt.Errorf("opencode export: %w", err)
	}
	replies, prompts := []Reply{}, []Prompt{}
	for _, m := range doc.Messages {
		var texts []string
		for _, p := range m.Parts {
			if p.Type != "text" || p.Synthetic {
				continue
			}
			if s := strings.TrimSpace(p.Text); s != "" {
				texts = append(texts, s)
			}
		}
		if len(texts) == 0 {
			continue
		}
		text, cut := keepHead(strings.Join(texts, "\n\n"), replyTextMax)
		at := time.UnixMilli(m.Info.Time.Created).UTC()
		switch m.Info.Role {
		case "assistant":
			replies = append(replies, Reply{At: at, Text: text, Truncated: cut})
		case "user":
			kind := PromptOperator
			if strings.HasPrefix(text, "[atrium] ") {
				kind = PromptPeer
			}
			prompts = append(prompts, Prompt{At: at, Text: text, Truncated: cut, Kind: kind})
		}
	}
	sort.SliceStable(replies, func(i, j int) bool { return replies[i].At.Before(replies[j].At) })
	sort.SliceStable(prompts, func(i, j int) bool { return prompts[i].At.Before(prompts[j].At) })
	return replies, prompts, nil
}
