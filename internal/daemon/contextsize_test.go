package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/api"
	"github.com/dovholuknf/atrium/internal/store"
)

// withTranscript gives a card a session id and a transcript the watcher finds,
// and returns a function that writes its last main reply at a context size.
func withTranscript(t *testing.T, d *Daemon, card *store.Task) func(tokens int64) {
	t.Helper()
	if err := d.st.SetResumeID(card.ID, "sess-"+card.WireName); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "t.jsonl")
	d.ctx.transcript = func(cwd, id string) string {
		if id == "sess-"+card.WireName {
			return path
		}
		return ""
	}
	n := 0
	return func(tokens int64) {
		t.Helper()
		n++
		line := fmt.Sprintf(`{"type":"assistant","timestamp":%q,"message":{"model":"claude-opus-5-5",`+
			`"usage":{"input_tokens":%d,"cache_creation_input_tokens":0,"cache_read_input_tokens":%d}}}`+"\n",
			time.Now().UTC().Add(time.Duration(n)*time.Second).Format(time.RFC3339Nano), n, tokens-int64(n))
		f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.WriteString(line); err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
}

func watch(t *testing.T, d *Daemon) {
	t.Helper()
	if err := d.watchContext(); err != nil {
		t.Fatal(err)
	}
}

func contextNotices(t *testing.T, d *Daemon, launcherID string) []*store.Message {
	t.Helper()
	var out []*store.Message
	for _, m := range pendingFrom(t, d, launcherID) {
		if strings.Contains(m.Text, "k context") {
			out = append(out, m)
		}
	}
	return out
}

// Every Claude card carries its size, marked past the threshold.
func TestACardShowsItsContextSize(t *testing.T) {
	d := testDaemon(t)
	card := peerCard(t, d, "mine")
	prompt(t, d, card.ID)
	reply := withTranscript(t, d, card)
	reply(90_000)
	watch(t, d)
	got, _ := d.contextSizeFor(card.ID).(*ContextSize)
	if got == nil || got.Tokens != 90_000 || got.Warn || got.ThresholdK != 150 {
		t.Fatalf("context %+v, want 90k under a 150k line", got)
	}
	reply(212_000)
	watch(t, d)
	if got, _ := d.contextSizeFor(card.ID).(*ContextSize); got == nil || got.Tokens != 212_000 || !got.Warn {
		t.Fatalf("context %+v, want 212k marked", got)
	}
}

// The gear setting moves the line.
func TestTheThresholdIsASetting(t *testing.T) {
	d := testDaemon(t)
	launcher, worker := launchedPair(t, d)
	reply := withTranscript(t, d, worker)
	if err := d.st.SetSetting(api.SettingContextThresholdK, "50"); err != nil {
		t.Fatal(err)
	}
	reply(60_000)
	watch(t, d)
	if got, _ := d.contextSizeFor(worker.ID).(*ContextSize); got == nil || !got.Warn || got.ThresholdK != 50 {
		t.Fatalf("context %+v, want marked past 50k", got)
	}
	if n := len(contextNotices(t, d, launcher.ID)); n != 1 {
		t.Fatalf("launcher has %d context notices, want 1", n)
	}
}

// One notice as a worker passes the line, however many turns it takes past it,
// and in the words the launcher acts on.
func TestTheLauncherHearsOncePerCrossing(t *testing.T) {
	d := testDaemon(t)
	launcher, worker := launchedPair(t, d)
	reply := withTranscript(t, d, worker)
	reply(100_000)
	watch(t, d)
	if n := len(contextNotices(t, d, launcher.ID)); n != 0 {
		t.Fatalf("launcher has %d context notices under the line", n)
	}
	for _, k := range []int64{160_000, 180_000, 240_000} {
		reply(k)
		watch(t, d)
		watch(t, d)
	}
	msgs := contextNotices(t, d, launcher.ID)
	want := "worker is at 160k context. Tell it to report what it has and stop, or hand off."
	if len(msgs) != 1 || msgs[0].Text != want {
		t.Fatalf("launcher has %v, want one %q", msgs, want)
	}

	// It compacts under the line and grows past it again: a new crossing.
	reply(40_000)
	watch(t, d)
	reply(155_000)
	watch(t, d)
	if n := len(contextNotices(t, d, launcher.ID)); n != 2 {
		t.Fatalf("launcher has %d context notices after a second crossing, want 2", n)
	}
}

// A raised threshold re-arms a card now under it, so a worker that jumps past
// the new line in one turn is a new crossing.
func TestARaisedThresholdReArmsTheNotice(t *testing.T) {
	d := testDaemon(t)
	launcher, worker := launchedPair(t, d)
	reply := withTranscript(t, d, worker)
	reply(160_000)
	watch(t, d)
	if err := d.st.SetSetting(api.SettingContextThresholdK, "200"); err != nil {
		t.Fatal(err)
	}
	watch(t, d)
	reply(210_000)
	watch(t, d)
	if n := len(contextNotices(t, d, launcher.ID)); n != 2 {
		t.Fatalf("launcher has %d context notices after crossing a raised line, want 2", n)
	}
}

// A card a human started is marked and nobody is told.
func TestAHumanCardGetsTheMarkAndNoNotice(t *testing.T) {
	d := testDaemon(t)
	other := peerCard(t, d, "orchestrator")
	mine := peerCard(t, d, "mine")
	prompt(t, d, mine.ID)
	reply := withTranscript(t, d, mine)
	reply(300_000)
	watch(t, d)
	if got, _ := d.contextSizeFor(mine.ID).(*ContextSize); got == nil || !got.Warn {
		t.Fatalf("context %+v, want marked", got)
	}
	if n := len(contextNotices(t, d, other.ID)); n != 0 {
		t.Fatalf("a human card sent %d context notices", n)
	}
	if claimed, _ := d.st.RecordNotice(mine.ID, NoticeContext, "sess-mine"); !claimed {
		t.Fatal("a human card claimed a context notice")
	}
}

// A restart forgets every size and still does not tell the launcher again
// about a worker it already heard about.
func TestARestartDoesNotNoticeTwice(t *testing.T) {
	d := testDaemon(t)
	launcher, worker := launchedPair(t, d)
	reply := withTranscript(t, d, worker)
	reply(200_000)
	watch(t, d)

	// What a restart leaves: the store, and nothing in memory.
	path := d.ctx.transcript
	d.ctx = newContextSizes()
	d.ctx.transcript = path
	if d.contextSizeFor(worker.ID) != nil {
		t.Fatal("a size survived the restart")
	}
	reply(210_000)
	watch(t, d)
	if got, _ := d.contextSizeFor(worker.ID).(*ContextSize); got == nil || got.Tokens != 210_000 {
		t.Fatalf("context %+v after the restart, want it read again", got)
	}
	if n := len(contextNotices(t, d, launcher.ID)); n != 1 {
		t.Fatalf("launcher has %d context notices across a restart, want 1", n)
	}
}

// A card that is not Claude has no size.
func TestANonClaudeCardHasNoSize(t *testing.T) {
	d := testDaemon(t)
	task, _, err := d.st.Register(store.Observed{WireName: "sh", Worktree: "/tmp/sh", Runner: "shell"})
	if err != nil {
		t.Fatal(err)
	}
	prompt(t, d, task.ID)
	reply := withTranscript(t, d, task)
	reply(200_000)
	watch(t, d)
	if got := d.contextSizeFor(task.ID); got != nil {
		t.Fatalf("a non-Claude card has a size: %+v", got)
	}
}
