package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/dovholuknf/atrium/internal/store"
)

// r-007 stage 3: the human touch, parking, and a say to a parked card. See
// docs/rnd/keepalive-policy-design.md sections 1, 4, 5 and 6.

func humanStamp(t *testing.T, d *Daemon, id string) (string, bool) {
	t.Helper()
	got, err := d.st.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if got.HumanAt == nil {
		return "", false
	}
	return got.HumanVia, true
}

// stamped waits for the off-path write to land.
func stamped(t *testing.T, d *Daemon, id, via string) {
	t.Helper()
	until(t, "human_via="+via, func() bool {
		v, ok := humanStamp(t, d, id)
		return ok && v == via
	})
}

// notStamped gives the async write time to land, then checks nothing did.
func notStamped(t *testing.T, d *Daemon, id string) {
	t.Helper()
	time.Sleep(60 * time.Millisecond)
	if v, ok := humanStamp(t, d, id); ok {
		t.Fatalf("a touch was stamped (%s) where there should be none", v)
	}
}

// sendKeys attaches, sends each frame as an `in`, and waits for the daemon to
// have read them, by sending a resize after and letting it settle.
func sendFrames(t *testing.T, d *Daemon, path string, frames ...string) {
	t.Helper()
	srv := httptest.NewServer(d.ap.Handler())
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+path, nil)
	if err != nil {
		t.Fatalf("could not attach: %v", err)
	}
	defer c.CloseNow()
	c.SetReadLimit(64 << 20)
	go func() {
		for {
			if _, _, err := c.Read(ctx); err != nil {
				return
			}
		}
	}()
	for _, f := range frames {
		raw, _ := json.Marshal(attachIn{T: "in", D: f})
		if err := c.Write(ctx, websocket.MessageText, raw); err != nil {
			t.Fatal(err)
		}
	}
	time.Sleep(80 * time.Millisecond)
}

func TestHumanTouchKeyCounts(t *testing.T) {
	d := testDaemon(t)
	card := peerCard(t, d, "typist")
	typedRunner(t, d, card.ID)
	sendFrames(t, d, "/v1/tasks/"+card.ID+"/attach", "a")
	stamped(t, d, card.ID, ViaTyped)
}

func TestHumanTouchReportsDoNotCount(t *testing.T) {
	d := testDaemon(t)
	card := peerCard(t, d, "quiet")
	typedRunner(t, d, card.ID)
	// A focus report, an SGR mouse click report and a device attributes reply.
	sendFrames(t, d, "/v1/tasks/"+card.ID+"/attach", "\x1b[I", "\x1b[<0;10;5M", "\x1b[?62;c")
	notStamped(t, d, card.ID)
}

func TestHumanTouchShellAttachIgnored(t *testing.T) {
	d := testDaemon(t)
	card := peerCard(t, d, "shelly")
	r, _ := typedRunner(t, d, card.ID)
	d.sup.mu.Lock()
	delete(d.sup.runners, card.ID)
	d.sup.mu.Unlock()
	d.sup.addShell(r)
	sendFrames(t, d, "/v1/tasks/"+card.ID+"/attach?kind=shell", "ls\r")
	notStamped(t, d, card.ID)
}

func activityPrompt(t *testing.T, d *Daemon, card *store.Task) {
	t.Helper()
	d.onActivity(ActivityEvent{TaskID: card.ID, Agent: card.WireName, Event: "prompt"})
}

func TestHumanTouchPeerPromptIgnored(t *testing.T) {
	d := testDaemon(t)
	card := peerCard(t, d, "listener2")
	r, _ := typedRunner(t, d, card.ID)
	r.typeMu.Lock()
	r.peerSent, r.peerCause = time.Now(), store.UsageSay
	r.typeMu.Unlock()
	activityPrompt(t, d, card)
	notStamped(t, d, card.ID)

	// The wake after a restart is atrium's own typing too.
	r.typeMu.Lock()
	r.peerSent, r.peerCause = time.Now(), store.UsageRestartWake
	r.typeMu.Unlock()
	d.humanTouched.Delete(card.ID)
	activityPrompt(t, d, card)
	notStamped(t, d, card.ID)

	// Twin: after the window it is a person's.
	r.typeMu.Lock()
	r.peerSent = time.Now().Add(-peerPromptWindow - time.Second)
	r.typeMu.Unlock()
	activityPrompt(t, d, card)
	stamped(t, d, card.ID, ViaPrompt)
}

func TestHumanTouchPromptWithNoPeerCounts(t *testing.T) {
	d := testDaemon(t)
	card := peerCard(t, d, "solo")
	typedRunner(t, d, card.ID)
	activityPrompt(t, d, card)
	stamped(t, d, card.ID, ViaPrompt)
}

func TestHumanTouchPermissionByHuman(t *testing.T) {
	d := testDaemon(t)
	card := peerCard(t, d, "asker")
	p, _, err := d.st.RecordPermission(card.ID, "Bash", "ls", "k1", "")
	if err != nil {
		t.Fatal(err)
	}
	got, err := d.decide(p.ID, "approve", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if got.DecidedBy != store.DecidedBySelf {
		t.Fatalf("decide recorded %q, want a self decision", got.DecidedBy)
	}
	stamped(t, d, card.ID, ViaPermission)

	// A rule or auto mode deciding is not a person, and never runs decide.
	other := peerCard(t, d, "ruled")
	q, _, err := d.st.RecordPermission(other.ID, "Bash", "ls", "k2", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.st.DecidePermissionBy(q.ID, "approve", "", "rule"); err != nil {
		t.Fatal(err)
	}
	notStamped(t, d, other.ID)
}

func TestHumanTouchThrottled(t *testing.T) {
	d := testDaemon(t)
	card := peerCard(t, d, "hammer")
	before := touchWrites.Load()
	for i := 0; i < 100; i++ {
		d.humanTouch(card.ID, ViaTyped)
	}
	if n := touchWrites.Load() - before; n != 1 {
		t.Fatalf("100 keys caused %d writes, want 1", n)
	}
	// A minute on, the next one writes.
	d.humanTouched.Store(card.ID, touchMark{at: time.Now().Add(-humanTouchEvery - time.Second), via: ViaTyped})
	d.humanTouch(card.ID, ViaTyped)
	if n := touchWrites.Load() - before; n != 2 {
		t.Fatalf("a key a minute later made %d writes in all, want 2", n)
	}
	// A different via is a different fact and is not throttled by the first.
	d.humanTouch(card.ID, ViaPermission)
	if n := touchWrites.Load() - before; n != 3 {
		t.Fatalf("a new via made %d writes in all, want 3", n)
	}
}

func TestHumanTouchStoreFailureSwallowed(t *testing.T) {
	d := testDaemon(t)
	// A card that is not there: the write updates nothing and must not panic or
	// block the caller. A halted store would return an error the same way.
	d.humanTouch("no-such-card", ViaTyped)
	time.Sleep(30 * time.Millisecond)
}

// ---- parking ----

func parkedCard(t *testing.T, d *Daemon, name, status string) *store.Task {
	t.Helper()
	card := peerCard(t, d, name)
	if err := d.st.SetStatus(card.ID, status); err != nil {
		t.Fatal(err)
	}
	if err := d.parkCard(card.ID, status, map[string]any{"by": "test"}); err != nil {
		t.Fatal(err)
	}
	got, err := d.st.Get(card.ID)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestParkKeepsStatusAndWritesOneEvent(t *testing.T) {
	d := testDaemon(t)
	card := peerCard(t, d, "sleeper")
	if err := d.st.SetStatus(card.ID, store.StatusNeedsInput); err != nil {
		t.Fatal(err)
	}
	// The wind-down has filed it dead by the time it is parked.
	if err := d.st.SetStatus(card.ID, store.StatusDead); err != nil {
		t.Fatal(err)
	}
	if err := d.parkCard(card.ID, store.StatusNeedsInput, nil); err != nil {
		t.Fatal(err)
	}
	// Twice is one.
	if err := d.parkCard(card.ID, store.StatusNeedsInput, nil); err != nil {
		t.Fatal(err)
	}
	got, _ := d.st.Get(card.ID)
	if !isParked(got) || got.Status != store.StatusNeedsInput {
		t.Fatalf("parked=%v status=%s, want parked at needs-input", isParked(got), got.Status)
	}
	evs, err := d.st.Events(card.ID, 100)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range evs {
		if e.Kind == store.EventStatusChanged && strings.Contains(string(e.Payload), `"parked":true`) {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("%d parked events, want 1", n)
	}
}

func TestSessionGoneFalseWhenParked(t *testing.T) {
	d := testDaemon(t)
	card := parkedCard(t, d, "donepark", store.StatusDone)
	if d.sessionGone(card) {
		t.Fatal("a parked done card reads as gone")
	}
	if d.sayGate(card) != sayParked {
		t.Fatalf("gate for a parked done card is %q, want parked", d.sayGate(card))
	}
}

func TestParkedPrecedesGone(t *testing.T) {
	d := testDaemon(t)
	card := parkedCard(t, d, "both", store.StatusDone)
	// Unparked, the same card is gone, so the order is what says parked.
	if _, err := d.st.Unpark(card.ID, "test"); err != nil {
		t.Fatal(err)
	}
	fresh, _ := d.st.Get(card.ID)
	if d.sayGate(fresh) == sayParked {
		t.Fatal("an unparked card answered parked")
	}
	// Both callers: the message endpoint and the tell endpoint.
	if _, err := d.st.Park(card.ID, store.StatusDone, nil); err != nil {
		t.Fatal(err)
	}
	peerCard(t, d, "alice")
	if out := sayViaMessage(t, d, "alice", card.ID, "hello"); out["reachable"] != "kept" {
		t.Fatalf("message answered %v", out)
	}
	out, _ := tell(t, d, "alice", "both", "hello")
	if out["reachable"] != "kept" {
		t.Fatalf("tell answered %v", out)
	}
}

func TestUnparkClearsFlagAndStamps(t *testing.T) {
	d := testDaemon(t)
	card := parkedCard(t, d, "wakes", store.StatusRunning)
	liveRunner(d, card.ID)
	if err := d.unpark(card.ID, ViaResume); err != nil {
		t.Fatal(err)
	}
	got, _ := d.st.Get(card.ID)
	if isParked(got) {
		t.Fatal("still parked")
	}
	stamped(t, d, card.ID, ViaResume)
	evs, _ := d.st.Events(card.ID, 100)
	found := false
	for _, e := range evs {
		if strings.Contains(string(e.Payload), `"parked":false`) {
			found = true
		}
	}
	if !found {
		t.Fatal("no parked:false event")
	}
}

func TestUnparkByPeerDoesNotStamp(t *testing.T) {
	d := testDaemon(t)
	card := parkedCard(t, d, "peerwoken", store.StatusRunning)
	liveRunner(d, card.ID)
	if err := d.unpark(card.ID, "say"); err != nil {
		t.Fatal(err)
	}
	notStamped(t, d, card.ID)
}

func TestUnparkIsIdempotent(t *testing.T) {
	d := testDaemon(t)
	card := parkedCard(t, d, "twice", store.StatusRunning)
	liveRunner(d, card.ID)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := d.unpark(card.ID, "say"); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	evs, _ := d.st.Events(card.ID, 100)
	n := 0
	for _, e := range evs {
		if strings.Contains(string(e.Payload), `"parked":false`) {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("%d unpark events from four triggers, want 1", n)
	}
}

func TestUnparkFailureLeavesItParked(t *testing.T) {
	d := testDaemon(t)
	card := parkedCard(t, d, "nowhere", store.StatusRunning)
	// No runner and a harness that does not exist: the launch fails.
	if err := d.unpark(card.ID, ViaResume); err == nil {
		t.Fatal("unpark with nothing to launch reported success")
	}
	got, _ := d.st.Get(card.ID)
	if !isParked(got) {
		t.Fatal("a failed resume cleared the flag")
	}
}

func TestReopenRequestCarriesTheCard(t *testing.T) {
	d := testDaemon(t)
	card := peerCard(t, d, "shape")
	got, _ := d.st.Get(card.ID)
	req := d.reopenRequest(got)
	if req.TaskID != card.ID || req.Harness != "claude" || req.Cwd != got.Worktree || req.Model != got.Model {
		t.Fatalf("reopenRequest %+v does not describe the card", req)
	}
}

func TestReaperAndReopenSkipParked(t *testing.T) {
	d := testDaemon(t)
	card := parkedCard(t, d, "skipped", store.StatusRunning)
	d.saveReopen([]*runner{{taskID: card.ID, buf: newRing(64, 80)}})
	if w := d.reopenWanted(); len(w) != 0 {
		t.Fatalf("reopen wants %d parked cards", len(w))
	}
	d.reapOnce()
	after, _ := d.st.Get(card.ID)
	if after.Status != store.StatusRunning || !isParked(after) {
		t.Fatalf("the reaper touched a parked card: %s parked=%v", after.Status, isParked(after))
	}
}

func TestParkedCardNeverSilent(t *testing.T) {
	d := testDaemon(t)
	orch := peerCard(t, d, "orchestrator")
	card := peerCard(t, d, "worker")
	if err := d.st.SetTags(card.ID, []string{OriginAgentTag, SubagentTag}); err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetLineage(card.ID, "orchestrator", orch.ID); err != nil {
		t.Fatal(err)
	}
	liveRunner(d, card.ID)
	prompt(t, d, card.ID)
	if err := d.parkCard(card.ID, store.StatusRunning, nil); err != nil {
		t.Fatal(err)
	}
	endRunner(d, card.ID)
	stopTurn(t, d, "worker")
	if n := len(pendingFrom(t, d, orch.ID)); n != 0 {
		t.Fatalf("a parked card raised %d notices", n)
	}
	stuckAgrees(t, d, card.ID, false)
}

func TestDirectorWithParkedWorkerNotSilent(t *testing.T) {
	d := testDaemon(t)
	orch, director, worker := directorRig(t, d)
	if err := d.parkCard(worker.ID, store.StatusRunning, nil); err != nil {
		t.Fatal(err)
	}
	stopTurn(t, d, "director")
	if n := len(pendingFrom(t, d, orch.ID)); n != 0 {
		t.Fatalf("the orchestrator got %d notices while a worker was parked", n)
	}
	stuckAgrees(t, d, director.ID, false)
}

func TestAttachAloneDoesNotUnparkButAKeyDoes(t *testing.T) {
	d := testDaemon(t)
	card := parkedCard(t, d, "peeked", store.StatusRunning)

	// Looking, with reports only: stays parked.
	sendFrames(t, d, "/v1/tasks/"+card.ID+"/attach", "\x1b[I", "\x1b[?62;c")
	got, _ := d.st.Get(card.ID)
	if !isParked(got) {
		t.Fatal("an attach with no key resumed the card")
	}

	// A real key tries to resume. There is nothing to launch here, so it stays
	// parked, but the attempt is what is under test: the launch error comes back
	// on the socket rather than the key being swallowed.
	srv := httptest.NewServer(d.ap.Handler())
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/v1/tasks/"+card.ID+"/attach", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	raw, _ := json.Marshal(attachIn{T: "in", D: "x"})
	if err := c.Write(ctx, websocket.MessageText, raw); err != nil {
		t.Fatal(err)
	}
	var seen strings.Builder
	for !strings.Contains(seen.String(), "resuming") {
		_, data, err := c.Read(ctx)
		if err != nil {
			t.Fatalf("socket closed before the key was answered: %v (%q)", err, seen.String())
		}
		seen.Write(data)
	}
}

// ---- say ----

func TestSayToParkedIsKeptAndDoesNotWake(t *testing.T) {
	d := testDaemon(t)
	card := parkedCard(t, d, "asleep", store.StatusRunning)
	peerCard(t, d, "alice")
	out := sayViaMessage(t, d, "alice", card.ID, "hi")
	if out["delivered"] != "queued" || out["reachable"] != "kept" {
		t.Fatalf("answer %v", out)
	}
	if n := len(pendingFrom(t, d, card.ID)); n != 1 {
		t.Fatalf("%d messages kept for a parked card, want 1", n)
	}
	got, _ := d.st.Get(card.ID)
	if !isParked(got) {
		t.Fatal("a say without wake resumed it")
	}
}

func sayWake(t *testing.T, d *Daemon, from, id, text string) map[string]any {
	t.Helper()
	body := `{"from":"` + from + `","text":"` + text + `","wake":true}`
	r := httptest.NewRequest(http.MethodPost, "/v1/tasks/"+id+"/message", strings.NewReader(body))
	r.SetPathValue("id", id)
	w := httptest.NewRecorder()
	d.handleMessage(w, r)
	out := map[string]any{}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return out
}

func TestSayToParkedWithWakeResumesThenQueuesNotTyped(t *testing.T) {
	d := testDaemon(t)
	card := parkedCard(t, d, "woken", store.StatusRunning)
	_, f := typedRunner(t, d, card.ID)
	peerCard(t, d, "alice")
	out := sayWake(t, d, "alice", card.ID, "WAKETEXT")
	if out["delivered"] == "parked" || out["delivered"] == "terminal" {
		t.Fatalf("answer %v", out)
	}
	got, _ := d.st.Get(card.ID)
	if isParked(got) {
		t.Fatal("wake=true did not resume")
	}
	if strings.Contains(f.written(), "WAKETEXT") {
		t.Fatal("the woken card was typed into")
	}
	if n := len(pendingFrom(t, d, card.ID)); n != 1 {
		t.Fatalf("%d messages queued, want 1", n)
	}
}

func TestOperatorSayResumesImmediately(t *testing.T) {
	d := testDaemon(t)
	card := parkedCard(t, d, "boss", store.StatusRunning)
	liveRunner(d, card.ID)
	body := `{"text":"OPERATORSAY"}`
	r := httptest.NewRequest(http.MethodPost, "/v1/tasks/"+card.ID+"/message", strings.NewReader(body))
	r.SetPathValue("id", card.ID)
	w := httptest.NewRecorder()
	d.handleMessage(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("answered %d: %s", w.Code, w.Body.String())
	}
	got, _ := d.st.Get(card.ID)
	if isParked(got) {
		t.Fatal("the operator's say did not resume")
	}
	if n := len(pendingFrom(t, d, card.ID)); n != 1 {
		t.Fatalf("%d messages queued, want 1", n)
	}
	stamped(t, d, card.ID, ViaResume)
}

func TestWakeIgnoredWhenNotParked(t *testing.T) {
	d := testDaemon(t)
	card := peerCard(t, d, "awake")
	_, f := typedRunner(t, d, card.ID)
	peerCard(t, d, "alice")
	out := sayWake(t, d, "alice", card.ID, "PLAIN")
	if out["delivered"] == "parked" {
		t.Fatalf("answer %v", out)
	}
	_ = f
	got, _ := d.st.Get(card.ID)
	if isParked(got) {
		t.Fatal("parked by a say")
	}
}

func TestTellToParked(t *testing.T) {
	d := testDaemon(t)
	card := parkedCard(t, d, "tellee", store.StatusRunning)
	peerCard(t, d, "alice")
	out, code := tell(t, d, "alice", "tellee", "hello")
	if code != http.StatusOK || out["queued"] != true || out["reachable"] != "kept" {
		t.Fatalf("tell to a parked card: %d %v", code, out)
	}
	if n := len(pendingFrom(t, d, card.ID)); n != 1 {
		t.Fatalf("%d kept, want 1", n)
	}
}

func TestTellWakeResumesThenQueuesNotTyped(t *testing.T) {
	d := testDaemon(t)
	card := parkedCard(t, d, "tellwoken", store.StatusRunning)
	_, f := typedRunner(t, d, card.ID)
	peerCard(t, d, "alice")
	raw, _ := json.Marshal(map[string]any{"from": "alice", "to": "tellwoken", "text": "TELLWAKE", "wake": true})
	w := httptest.NewRecorder()
	d.handleTell(w, httptest.NewRequest("POST", "/tell", strings.NewReader(string(raw))))
	if w.Code != http.StatusOK {
		t.Fatalf("answered %d: %s", w.Code, w.Body.String())
	}
	got, _ := d.st.Get(card.ID)
	if isParked(got) {
		t.Fatal("still parked")
	}
	if strings.Contains(f.written(), "TELLWAKE") {
		t.Fatal("typed into a woken card")
	}
	if n := len(pendingFrom(t, d, card.ID)); n != 1 {
		t.Fatalf("%d queued, want 1", n)
	}
}

func TestPeersMarksParked(t *testing.T) {
	d := testDaemon(t)
	parkedCard(t, d, "sleepy", store.StatusRunning)
	peerCard(t, d, "alice")
	found := false
	peers, err := d.peers("alice")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range peers {
		if strings.HasSuffix(p.Handle, "sleepy") {
			found = true
			if !p.Parked {
				t.Fatal("a parked card is not marked in the roster")
			}
		}
	}
	if !found {
		t.Fatal("sleepy is not in the roster")
	}
}

func TestReportToParkedLauncherWakes(t *testing.T) {
	d := testDaemon(t)
	launcher, worker := launchedPair(t, d)
	liveRunner(d, launcher.ID)
	if err := d.parkCard(launcher.ID, store.StatusRunning, nil); err != nil {
		t.Fatal(err)
	}
	rec, out := finishWith(t, d, FinishRequest{Agent: worker.WireName, Status: ReportDone, NoCommit: "nothing to commit", Recap: "done it"})
	if rec.Code != http.StatusOK {
		t.Fatalf("the report answered %d: %v", rec.Code, out)
	}
	got, _ := d.st.Get(launcher.ID)
	if isParked(got) {
		t.Fatal("a report did not resume its parked launcher")
	}
}
