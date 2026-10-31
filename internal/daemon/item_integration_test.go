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

// itemPair is launchedPair with the worker marked as an item.
func itemPair(t *testing.T, d *Daemon) (launcher, worker *store.Task) {
	t.Helper()
	launcher, worker = launchedPair(t, d)
	if err := d.st.SetTags(worker.ID, []string{"sdk", OriginAgentTag, ItemTag}); err != nil {
		t.Fatal(err)
	}
	return launcher, worker
}

func postSay(t *testing.T, d *Daemon, from, to, text string) (map[string]any, int) {
	t.Helper()
	raw, _ := json.Marshal(map[string]string{"from": from, "to": to, "text": text})
	w := httptest.NewRecorder()
	d.HandleSay(w, httptest.NewRequest(http.MethodPost, "/v1/say", bytes.NewReader(raw)))
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return out, w.Code
}

// A running item card cannot say anything, by either door, to anyone, and nothing is queued.
func TestASayFromARunningItemCardIsRefused(t *testing.T) {
	d := testDaemon(t)
	launcher, _ := itemPair(t, d)
	peerCard(t, d, "bystander")

	for name, say := range map[string]func(to string) (map[string]any, int){
		"tell": func(to string) (map[string]any, int) { return tell(t, d, "worker", to, "how is it going") },
		"say":  func(to string) (map[string]any, int) { return postSay(t, d, "worker", to, "how is it going") },
	} {
		for _, to := range []string{"orchestrator", "bystander", "operator"} {
			out, code := say(to)
			msg, _ := out["error"].(string)
			if code < 400 || !strings.Contains(msg, "item workers cannot say anything") {
				t.Errorf("%s to %s: %d %v, want a refusal", name, to, code, out)
			}
		}
	}
	if n := len(pendingFrom(t, d, launcher.ID)); n != 0 {
		t.Fatalf("the launcher has %d messages from an item worker", n)
	}
}

// The same say from an ordinary worker is delivered, as it was.
func TestASayFromAnOrdinaryWorkerIsStillDelivered(t *testing.T) {
	d := testDaemon(t)
	launcher, _ := launchedPair(t, d)
	if out, code := tell(t, d, "worker", "orchestrator", "how is it going"); code != http.StatusOK {
		t.Fatalf("tell: %d %v", code, out)
	}
	if out, code := postSay(t, d, "worker", "orchestrator", "and again"); code != http.StatusOK {
		t.Fatalf("say: %d %v", code, out)
	}
	if n := len(pendingFrom(t, d, launcher.ID)); n != 2 {
		t.Fatalf("the launcher has %d messages, want 2", n)
	}
}

// The operator and other sessions can still say to an item card. Only the item's own say is refused.
func TestSomeoneElseCanStillSayToAnItemCard(t *testing.T) {
	d := testDaemon(t)
	_, worker := itemPair(t, d)
	if out, code := tell(t, d, "orchestrator", "worker", "carry on"); code != http.StatusOK {
		t.Fatalf("tell: %d %v", code, out)
	}
	if n := len(pendingFrom(t, d, worker.ID)); n != 1 {
		t.Fatalf("the item card has %d messages, want 1", n)
	}
}

// An item's result is one notice per (card, generation), however the words differ.
func TestAnItemsResultIsOneNoticePerGeneration(t *testing.T) {
	d := testDaemon(t)
	launcher, _ := itemPair(t, d)
	stubCommits(t, d, true, true)
	finishWith(t, d, FinishRequest{Agent: "worker", Status: ReportDone, SHA: "abc1234", Recap: "done abc1234", Ended: true})
	finishWith(t, d, FinishRequest{Agent: "worker", Status: ReportDone, SHA: "def5678", Recap: "done def5678, reworded", Ended: true})
	if n := len(pendingFrom(t, d, launcher.ID)); n != 1 {
		t.Fatalf("an item's result queued %d notices, want 1", n)
	}
}

// An ordinary worker's two reports are two notices, as they always were.
func TestAnOrdinaryWorkersTwoDonesAreTwoNotices(t *testing.T) {
	d := testDaemon(t)
	launcher, _ := launchedPair(t, d)
	stubCommits(t, d, true, true)
	finishWith(t, d, FinishRequest{Agent: "worker", Status: ReportDone, SHA: "abc1234", Recap: "done abc1234", Ended: true})
	finishWith(t, d, FinishRequest{Agent: "worker", Status: ReportDone, SHA: "def5678", Recap: "done def5678", Ended: true})
	if n := len(pendingFrom(t, d, launcher.ID)); n != 2 {
		t.Fatalf("an ordinary worker queued %d notices, want 2", n)
	}
}
