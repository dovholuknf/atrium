package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// r-007 stage 5 review: a say from the card's family resumes it, keep-alive
// leaves a parked card alone, and the parked answer says how to wake it.

func TestFamilySayWakesParked(t *testing.T) {
	say := func(t *testing.T, d *Daemon, from, id string) map[string]any {
		t.Helper()
		body := `{"from":"` + from + `","text":"FAMILY"}`
		r := httptest.NewRequest(http.MethodPost, "/v1/tasks/"+id+"/message", strings.NewReader(body))
		r.SetPathValue("id", id)
		w := httptest.NewRecorder()
		d.handleMessage(w, r)
		out := map[string]any{}
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return out
	}
	t.Run("the launcher's say wakes its parked worker", func(t *testing.T) {
		d := testDaemon(t)
		launcher, worker := launchedPair(t, d)
		liveRunner(d, worker.ID)
		if err := d.parkCard(worker.ID, store.StatusRunning, nil); err != nil {
			t.Fatal(err)
		}
		out := say(t, d, launcher.WireName, worker.ID)
		if out["delivered"] == "parked" {
			t.Fatalf("answer %v", out)
		}
		if got, _ := d.st.Get(worker.ID); isParked(got) {
			t.Fatal("the launcher's say left the worker parked")
		}
	})
	t.Run("a worker's say wakes its parked launcher", func(t *testing.T) {
		d := testDaemon(t)
		launcher, worker := launchedPair(t, d)
		liveRunner(d, launcher.ID)
		if err := d.parkCard(launcher.ID, store.StatusRunning, nil); err != nil {
			t.Fatal(err)
		}
		out := say(t, d, worker.WireName, launcher.ID)
		if out["delivered"] == "parked" {
			t.Fatalf("answer %v", out)
		}
		if got, _ := d.st.Get(launcher.ID); isParked(got) {
			t.Fatal("the worker's say left the launcher parked")
		}
	})
	t.Run("the same through tell", func(t *testing.T) {
		d := testDaemon(t)
		launcher, worker := launchedPair(t, d)
		liveRunner(d, worker.ID)
		if err := d.parkCard(worker.ID, store.StatusRunning, nil); err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(map[string]any{"from": launcher.WireName, "to": worker.WireName, "text": "FAMILY"})
		w := httptest.NewRecorder()
		d.handleTell(w, httptest.NewRequest("POST", "/tell", strings.NewReader(string(raw))))
		if w.Code != http.StatusOK {
			t.Fatalf("answered %d: %s", w.Code, w.Body.String())
		}
		if got, _ := d.st.Get(worker.ID); isParked(got) {
			t.Fatal("the launcher's tell left the worker parked")
		}
	})
	t.Run("any other peer still gets parked, told how to wake it", func(t *testing.T) {
		d := testDaemon(t)
		_, worker := launchedPair(t, d)
		peerCard(t, d, "stranger")
		liveRunner(d, worker.ID)
		if err := d.parkCard(worker.ID, store.StatusRunning, nil); err != nil {
			t.Fatal(err)
		}
		out := say(t, d, "stranger", worker.ID)
		if out["delivered"] != "parked" {
			t.Fatalf("answer %v", out)
		}
		if got, _ := d.st.Get(worker.ID); !isParked(got) {
			t.Fatal("a stranger's say woke it")
		}
		note, _ := out["warning"].(string)
		if !strings.Contains(note, "this card is parked, send again with wake=true to resume it") {
			t.Fatalf("the parked answer does not say how: %q", note)
		}
	})
}

func TestKeepaliveNeverRefreshesAParkedCard(t *testing.T) {
	f := newKAFix(t)
	f.reply(f.now.Add(-56*time.Minute), replyOpt{})
	if _, err := f.st.Park(f.task.ID, store.StatusNeedsInput, nil); err != nil {
		t.Fatal(err)
	}
	f.tick()
	if f.forks() != 0 {
		t.Fatalf("forks = %d, a parked card was refreshed", f.forks())
	}
	if got := f.k.why[f.task.ID]; got != "parked" {
		t.Fatalf("why = %q, want parked", got)
	}
}
