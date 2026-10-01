package daemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

const ocSession = "ses_f06a7011dffeuZx11lJZVjRBnv"

type ocFix struct {
	d     *Daemon
	card  string
	calls *int32
	argv  *[]string
}

// newOcFix is a daemon with one opencode card and a fake export that prints
// `out` after opencode's one non-JSON line. No opencode is needed.
func newOcFix(t *testing.T, session string, out func() ([]byte, error)) *ocFix {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.ToSlash(filepath.Join(dir, "atrium.db")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if _, err := st.SaveHarness(store.Harness{ID: "opencode", Label: "opencode", Enabled: true, Cmd: "opencode"}); err != nil {
		t.Fatal(err)
	}
	task, _, err := st.Register(store.Observed{WireName: "oc", Worktree: filepath.ToSlash(dir), Runner: "opencode"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetResumeID(task.ID, session); err != nil {
		t.Fatal(err)
	}
	var calls int32
	var argv []string
	oldExec, oldLook, oldTTL := opencodeExec, opencodeLookPath, opencodeTTL
	t.Cleanup(func() { opencodeExec, opencodeLookPath, opencodeTTL = oldExec, oldLook, oldTTL })
	opencodeLookPath = func(s string) (string, error) { return "/fake/" + s, nil }
	opencodeExec = func(ctx context.Context, bin string, args []string, max int64) ([]byte, error) {
		atomic.AddInt32(&calls, 1)
		argv = args
		return out()
	}
	return &ocFix{d: &Daemon{st: st, sup: newSupervisor()}, card: task.ID, calls: &calls, argv: &argv}
}

func ocRecorded(t *testing.T) func() ([]byte, error) {
	b, err := os.ReadFile("testdata/opencode-export.json")
	if err != nil {
		t.Fatal(err)
	}
	return func() ([]byte, error) { return append([]byte("Exporting session: ses_x\n"), b...), nil }
}

func TestOpencodeCardReadsItsExport(t *testing.T) {
	f := newOcFix(t, ocSession, ocRecorded(t))
	v, err := f.d.repliesFor(f.card, 10)
	if err != nil {
		t.Fatal(err)
	}
	if v.Source != "transcript" || len(v.Replies) != 2 || len(v.Prompts) != 2 {
		t.Fatalf("got %+v", v)
	}
	for _, r := range v.Replies {
		if r.Text != "Done." {
			t.Fatalf("reply %q: assistant text only", r.Text)
		}
	}
	if !v.Replies[0].At.Before(v.Replies[1].At) || !strings.HasPrefix(v.Prompts[1].Text, "Use your bash tool to run: echo second") || v.Prompts[0].Kind != PromptOperator {
		t.Fatalf("order or prompts: %+v", v)
	}
	if got := strings.Join(*f.argv, " "); got != "export "+ocSession {
		t.Fatalf("argv %q", got)
	}
}

func TestOpencodeRepliesPageWithBefore(t *testing.T) {
	f := newOcFix(t, ocSession, ocRecorded(t))
	// Pages cut as finishPage cuts them: complete down to next_before, nothing
	// lost or shown twice. Walk them all with n=1.
	var replies, prompts int
	var before time.Time
	for i := 0; i < 10; i++ {
		v, err := f.d.repliesPage(f.card, 1, before)
		if err != nil || v.Source != "transcript" {
			t.Fatalf("page %d: %+v, %v", i, v, err)
		}
		replies += len(v.Replies)
		prompts += len(v.Prompts)
		if i == 0 && (!v.More || v.NextBefore == "" || len(v.Replies) != 1) {
			t.Fatalf("first page %+v", v)
		}
		if !v.More {
			break
		}
		before, _ = time.Parse(time.RFC3339Nano, v.NextBefore)
	}
	if replies != 2 || prompts != 2 {
		t.Fatalf("paging saw %d replies and %d prompts, want 2 and 2", replies, prompts)
	}
	if n := atomic.LoadInt32(f.calls); n != 1 {
		t.Fatalf("paging spawned %d exports, want 1 (cached)", n)
	}
}

func TestOpencodeReplyIsCutAtReplyTextMax(t *testing.T) {
	long := strings.Repeat("x", replyTextMax+100)
	f := newOcFix(t, ocSession, func() ([]byte, error) {
		return []byte(`note
{"messages":[{"info":{"role":"assistant","time":{"created":1000}},"parts":[{"type":"reasoning","text":"no"},{"type":"text","text":"` + long + `"}]}]}`), nil
	})
	v, _ := f.d.repliesFor(f.card, 3)
	if v.Source != "transcript" || len(v.Replies) != 1 || len(v.Replies[0].Text) != replyTextMax || !v.Replies[0].Truncated {
		t.Fatalf("got %+v", v)
	}
}

func TestOpencodeFallsBackToTheScreen(t *testing.T) {
	cases := []struct {
		name, session string
		out           func() ([]byte, error)
		spawns        int32
	}{
		{"over the cap", ocSession, func() ([]byte, error) { return nil, errors.New("export is over the cap") }, 1},
		{"exec failure", ocSession, func() ([]byte, error) { return nil, errors.New("exit status 1") }, 1},
		{"timeout", ocSession, func() ([]byte, error) { return nil, context.DeadlineExceeded }, 1},
		{"no json", ocSession, func() ([]byte, error) { return []byte("nothing here"), nil }, 1},
		{"bad session id", "ses_x; rm -rf /", nil, 0},
	}
	for _, c := range cases {
		out := c.out
		if out == nil {
			out = ocRecorded(t)
		}
		f := newOcFix(t, c.session, out)
		v, err := f.d.repliesFor(f.card, 3)
		if err != nil || v.Source != "screen" {
			t.Errorf("%s: got %+v, %v", c.name, v, err)
		}
		if n := atomic.LoadInt32(f.calls); n != c.spawns {
			t.Errorf("%s: %d exports, want %d", c.name, n, c.spawns)
		}
	}
}

func TestOpencodeMissingBinaryFallsBack(t *testing.T) {
	f := newOcFix(t, ocSession, ocRecorded(t))
	opencodeLookPath = func(string) (string, error) { return "", errors.New("not found") }
	v, _ := f.d.repliesFor(f.card, 3)
	if v.Source != "screen" || atomic.LoadInt32(f.calls) != 0 {
		t.Fatalf("got %+v", v)
	}
}

func TestOpencodeOneExportInFlightAndCached(t *testing.T) {
	f := newOcFix(t, ocSession, ocRecorded(t))
	opencodeTTL = time.Minute
	done := make(chan struct{})
	for i := 0; i < 8; i++ {
		go func() { f.d.repliesFor(f.card, 3); done <- struct{}{} }()
	}
	for i := 0; i < 8; i++ {
		<-done
	}
	if n := atomic.LoadInt32(f.calls); n != 1 {
		t.Fatalf("8 polls spawned %d exports, want 1", n)
	}
}

func TestOpencodeSessionIDShape(t *testing.T) {
	for id, ok := range map[string]bool{ocSession: true, "ses_": false, "abc": false, "ses_a b": false, "ses_a/../b": false, "": false} {
		if opencodeSessionID.MatchString(id) != ok {
			t.Errorf("%q: want %v", id, ok)
		}
	}
}
