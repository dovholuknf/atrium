//go:build integration

package daemon

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

func sayAny(t *testing.T, d *Daemon, body map[string]any) (int, map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(body)
	rec := httptest.NewRecorder()
	d.handleSay(rec, httptest.NewRequest(http.MethodPost, "/v1/say", bytes.NewReader(raw)))
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

// A name that matches nothing answers with candidates and queues nothing.
func TestSayMissListsCandidatesAndQueuesNothing(t *testing.T) {
	d := testDaemon(t)
	peerCard(t, d, "sa1")
	target := peerCard(t, d, "atrium-runtime")

	code, out := sayAny(t, d, map[string]any{"from": "sa1", "to": "atrium", "text": "hello"})
	if code != http.StatusNotFound {
		t.Fatalf("%d %+v", code, out)
	}
	msg, _ := out["error"].(string)
	if !strings.Contains(msg, "did you mean") || !strings.Contains(msg, "atrium-runtime") {
		t.Fatalf("error %q does not offer the candidate", msg)
	}
	if got := pendingFrom(t, d, target.ID); len(got) != 0 {
		t.Fatalf("a guess was queued: %+v", got)
	}
	rows, err := d.st.SaysFor(d.senderTask("sa1"), 10)
	if err != nil || len(rows) != 1 || rows[0].State != store.SayUnresolved {
		t.Fatalf("rows %+v, %v", rows, err)
	}
}

// An alias resolves, and the row says it was an alias.
func TestSayByAliasRecordsVia(t *testing.T) {
	d := testDaemon(t)
	peerCard(t, d, "sa1")
	target := peerCard(t, d, "sa2")
	if err := d.st.SetAlias(target.ID, "dotfiles"); err != nil {
		t.Skipf("no alias setter: %v", err)
	}
	code, out := sayAny(t, d, map[string]any{"from": "sa1", "to": "@dotfiles", "text": "hi"})
	if code != http.StatusOK || out["via"] != "alias" || out["say"] == "" {
		t.Fatalf("%d %+v", code, out)
	}
}

// A queued say becomes delivered on the hook that drains it.
func TestSayQueuedThenDeliveredByHook(t *testing.T) {
	d := testDaemon(t)
	peerCard(t, d, "sa1")
	target := peerCard(t, d, "sa2")

	code, out := sayAny(t, d, map[string]any{"from": "sa1", "to": "sa2", "text": "hi"})
	if code != http.StatusOK {
		t.Fatalf("%d %+v", code, out)
	}
	id, _ := out["say"].(string)
	row, err := d.st.SayByID(id)
	if err != nil || row.State != store.SayQueued {
		t.Fatalf("%+v %v", row, err)
	}
	msgs := pendingFrom(t, d, target.ID)
	if len(msgs) != 1 {
		t.Fatalf("pending %+v", msgs)
	}
	if err := d.st.MarkDelivered(target.ID, "permission", []string{msgs[0].ID}); err != nil {
		t.Fatal(err)
	}
	row, _ = d.st.SayByID(id)
	if row.State != store.SayDelivered || row.Channel != store.SayViaHook {
		t.Fatalf("after the hook: %+v", row)
	}
}

// A reply asked for is owed until the receiver says something back.
func TestSayReplyOwedUntilAnswered(t *testing.T) {
	d := testDaemon(t)
	peerCard(t, d, "sa1")
	target := peerCard(t, d, "sa2")

	if code, out := sayAny(t, d, map[string]any{"from": "sa1", "to": "sa2", "text": "status?", "reply": true}); code != http.StatusOK {
		t.Fatalf("%d %+v", code, out)
	}
	owed, err := d.st.RepliesOwed()
	if err != nil || owed[target.ID] != 1 {
		t.Fatalf("owed %+v %v", owed, err)
	}
	if code, out := sayAny(t, d, map[string]any{"from": "sa2", "to": "sa1", "text": "green"}); code != http.StatusOK {
		t.Fatalf("%d %+v", code, out)
	}
	owed, _ = d.st.RepliesOwed()
	if owed[target.ID] != 0 {
		t.Fatalf("still owed after a reply: %+v", owed)
	}
}

// A compact or a clear stamps the says it may have erased.
func TestSayContextResetIsStamped(t *testing.T) {
	d := testDaemon(t)
	peerCard(t, d, "sa1")
	target := peerCard(t, d, "sa2")

	_, out := sayAny(t, d, map[string]any{"from": "sa1", "to": "sa2", "text": "status?", "reply": true})
	id, _ := out["say"].(string)
	msgs := pendingFrom(t, d, target.ID)
	if len(msgs) != 1 {
		t.Fatalf("pending %+v", msgs)
	}
	if err := d.st.MarkDelivered(target.ID, "permission", []string{msgs[0].ID}); err != nil {
		t.Fatal(err)
	}
	d.sayReset(target.ID, "clear")
	row, _ := d.st.SayByID(id)
	if row.ResetKind != "clear" || row.ResetAt == "" {
		t.Fatalf("not stamped: %+v", row)
	}
}
