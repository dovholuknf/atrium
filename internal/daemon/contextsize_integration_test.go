//go:build integration

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

// Every Claude card carries its size, marked past its limit.
func TestACardShowsItsContextSize(t *testing.T) {
	d := testDaemon(t)
	card := peerCard(t, d, "mine")
	prompt(t, d, card.ID)
	reply := withTranscript(t, d, card)
	reply(90_000)
	watch(t, d)
	got, _ := d.contextSizeFor(card.ID).(*ContextSize)
	if got == nil || got.Tokens != 90_000 || got.Warn || got.ThresholdK != 200 || got.Source != "default" {
		t.Fatalf("context %+v, want 90k under the default 200k", got)
	}
	reply(212_000)
	watch(t, d)
	if got, _ := d.contextSizeFor(card.ID).(*ContextSize); got == nil || got.Tokens != 212_000 || !got.Warn {
		t.Fatalf("context %+v, want 212k marked", got)
	}
}

// The hub's per-harness setting moves the line.
func TestTheLimitIsAHubSetting(t *testing.T) {
	d := testDaemon(t)
	_, worker := launchedPair(t, d)
	reply := withTranscript(t, d, worker)
	if err := d.st.SetSetting(store.SettingContextLimits, `{"claude":50}`); err != nil {
		t.Fatal(err)
	}
	reply(60_000)
	watch(t, d)
	if got, _ := d.contextSizeFor(worker.ID).(*ContextSize); got == nil || !got.Warn || got.ThresholdK != 50 || got.Source != "hub" {
		t.Fatalf("context %+v, want marked past 50k from the hub", got)
	}
}

// Plan 1: nobody hears about a card's size. Not its launcher, not anyone.
func TestNobodyIsToldAboutASize(t *testing.T) {
	d := testDaemon(t)
	launcher, worker := launchedPair(t, d)
	reply := withTranscript(t, d, worker)
	for _, k := range []int64{100_000, 160_000, 240_000, 400_000} {
		reply(k)
		watch(t, d)
	}
	if n := len(contextNotices(t, d, launcher.ID)); n != 0 {
		t.Fatalf("launcher has %d context notices, want none", n)
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

// Item 62, sa58: a card cleared onto a new session keeps its old resume id until the
// new transcript has something in it. The new session's transcript is the one read,
// before its first turn has ended.
func TestAClearedCardReadsItsNewSession(t *testing.T) {
	d := testDaemon(t)
	_, worker := launchedPair(t, d)
	reply := withTranscript(t, d, worker)
	reply(151_000)
	watch(t, d)

	cleared := filepath.Join(t.TempDir(), "cleared.jsonl")
	old := d.ctx.transcript
	d.ctx.transcript = func(cwd, id string) string {
		if id == "sess-cleared" {
			return cleared
		}
		return old(cwd, id)
	}
	no := false
	if err := d.onSession(SessionEvent{Agent: worker.WireName, Event: "start", Source: "clear",
		Resume: "sess-cleared", Resumable: &no, TaskID: worker.ID}); err != nil {
		t.Fatal(err)
	}
	if got, _ := d.st.Get(worker.ID); got.ResumeID != "sess-"+worker.WireName {
		t.Fatalf("resume id %q, want the old one kept until the new session writes", got.ResumeID)
	}
	line := fmt.Sprintf(`{"type":"assistant","timestamp":%q,"message":{"model":"claude-opus-5-5",`+
		`"usage":{"input_tokens":1,"cache_creation_input_tokens":0,"cache_read_input_tokens":%d}}}`+"\n",
		time.Now().UTC().Format(time.RFC3339Nano), 335_999)
	if err := os.WriteFile(cleared, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	watch(t, d)
	if got, _ := d.contextSizeFor(worker.ID).(*ContextSize); got == nil || got.Tokens != 336_000 {
		t.Fatalf("context %+v, want the cleared session's 336k", got)
	}
}

// The row carries the limit in force and the layer it came from, and the mark follows that limit.
func TestTheRowNamesTheLayerTheLimitCameFrom(t *testing.T) {
	d := testDaemon(t)
	card := peerCard(t, d, "layered")
	prompt(t, d, card.ID)
	reply := withTranscript(t, d, card)
	reply(250_000)
	watch(t, d)
	read := func() *ContextSize {
		t.Helper()
		got, _ := d.contextSizeFor(card.ID).(*ContextSize)
		if got == nil {
			t.Fatal("no context size")
		}
		return got
	}
	if got := read(); got.ThresholdK != 200 || got.Source != "default" || !got.Warn {
		t.Fatalf("%+v, want 200k from default, warned", got)
	}
	if err := d.st.SetSetting(store.SettingContextLimits, `{"claude":300}`); err != nil {
		t.Fatal(err)
	}
	if got := read(); got.ThresholdK != 300 || got.Source != "hub" || got.Warn {
		t.Fatalf("%+v, want 300k from hub, not warned", got)
	}
	if err := d.st.SetOverrides(card.ID, map[string]string{api.OverrideContextLimitK: "190"}); err != nil {
		t.Fatal(err)
	}
	if got := read(); got.ThresholdK != 190 || got.Source != "card" || !got.Warn {
		t.Fatalf("%+v, want 190k from card, warned", got)
	}
}
