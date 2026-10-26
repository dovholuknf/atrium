//go:build integration

package daemon

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// hostRunnerCard is a card with a shell runner in the host, running for a minute.
func hostRunnerCard(t *testing.T, d *Daemon, name string) (*store.Task, *runner) {
	t.Helper()
	task := cardAt(t, d, name, t.TempDir())
	sh, args := someShell(t)
	if _, err := d.spawnPTY(task.ID, sh, append(args, "sleep 60"), t.TempDir(), os.Environ()); err != nil {
		t.Fatal(err)
	}
	r := d.sup.get(task.ID)
	if hostOf(r) == nil {
		t.Fatal("the runner is not on the host")
	}
	return task, r
}

func TestStopLeavesHostRunnersUp(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	t.Parallel()
	dir, _ := hostFixture(t)
	d1 := daemonAt(t, dir)
	task, r1 := hostRunnerCard(t, d1, "stop-leaves")
	before, err := d1.st.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	runID := r1.runID

	d1.stop.request("test")
	d1.shutdown()

	d2 := restarted(t, d1, dir)
	if info, ok := listOf(t, d2)[runID]; !ok || info.Exited {
		t.Fatalf("the runner did not survive the stop: %+v listed=%v", info, ok)
	}
	d2.reattachRuns()
	r2 := d2.sup.get(task.ID)
	if r2 == nil || r2.runID != runID {
		t.Fatalf("the second daemon did not reattach the runner: %+v", r2)
	}
	after, err := d2.st.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != before.Status {
		t.Fatalf("the card went from %s to %s across the stop", before.Status, after.Status)
	}
	if n := len(filedExits(t, d2, task.ID)); n != 0 {
		t.Fatalf("a stop that left the runner up filed %d exits", n)
	}
	if got := d2.readReopen(); len(got) != 0 {
		t.Fatalf("the reopen list holds %v, but nothing was closed", got)
	}
}

func TestStopRunnersEndsHostRunners(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	t.Parallel()
	dir, _ := hostFixture(t)
	d := daemonAt(t, dir)
	task, _ := hostRunnerCard(t, d, "stop-runners")

	d.stopRunners.Store(true)
	d.stopSupervised(2 * time.Second)

	hostWait(t, 30*time.Second, "the exit to be filed", func() bool { return len(filedExits(t, d, task.ID)) == 1 })
	time.Sleep(300 * time.Millisecond) // wait: a negative check, the exit must not be filed again
	if n := len(filedExits(t, d, task.ID)); n != 1 {
		t.Fatalf("the runner's exit was filed %d times, want once", n)
	}
	if got := d.readReopen(); len(got) != 1 || got[0] != task.ID {
		t.Fatalf("the reopen list is %v, want the card that was open", got)
	}
}

func TestReattachedCardIsNotIdleParkableUntilStop(t *testing.T) {
	d := testDaemon(t)
	card := idleAgent(t, d, "reattached", store.StatusNeedsInput)
	if !d.idleParkEligible(card, map[string]bool{}) {
		t.Fatal("the control card should be eligible")
	}
	d.sup.get(card.ID).reattached.Store(true)
	if d.idleParkEligible(card, map[string]bool{}) {
		t.Fatal("a reattached card was eligible before its first Stop")
	}
	raw, _ := json.Marshal(map[string]any{"agent": card.WireName, "task_id": card.ID})
	d.handleStop(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/stop", bytes.NewReader(raw)))
	got, _ := d.st.Get(card.ID)
	if d.sup.get(card.ID).reattached.Load() {
		t.Fatal("the Stop did not clear the reattached mark")
	}
	if !d.idleParkEligible(got, map[string]bool{}) {
		t.Fatal("the card is still held after a Stop")
	}
}

func TestReattachCountsAsStartForAWake(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	t.Parallel()
	dir, _ := hostFixture(t)
	d1 := daemonAt(t, dir)
	task, _ := hostRunnerCard(t, d1, "wake-after-reattach")
	w, _, err := d1.queueWake(task.ID, "carry on", "test")
	if err != nil {
		t.Fatal(err)
	}

	d2 := restarted(t, d1, dir)
	d2.reattachRuns()
	r := d2.sup.get(task.ID)
	if r == nil {
		t.Fatal("not reattached")
	}
	now := time.Now()
	if d2.wakeRunnerReady(w, r, now) {
		t.Fatal("the wake was typed with no settle delay after the reattach")
	}
	if !d2.wakeRunnerReady(w, r, now.Add(wakeSettle+time.Second)) {
		t.Fatal("the wake still waits for a SessionStart that a reattached runner never sends")
	}
}

func TestReattachedLineIsNotEmpty(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	t.Parallel()
	dir, _ := hostFixture(t)
	d1 := daemonAt(t, dir)
	task, _ := hostRunnerCard(t, d1, "line-after-reattach")
	d2 := restarted(t, d1, dir)
	d2.reattachRuns()
	r := d2.sup.get(task.ID)
	if r == nil {
		t.Fatal("not reattached")
	}
	if r.peerGateOpen() {
		t.Fatal("the gate is open on a line nobody has seen since the restart")
	}
	if got := r.typing().Unsure; !strings.Contains(got, "restart") {
		t.Fatalf("the line is marked %q", got)
	}
	// A Stop with no key since is not proof: the operator may have left a draft.
	if r.resetLineAtStop() || r.peerGateOpen() {
		t.Fatal("a Stop emptied a line atrium never saw")
	}
	r.noteOperatorTyped([]byte("\r"))
	if st := r.typing(); st.Unsure != "" || st.Count != 0 {
		t.Fatalf("Enter did not empty the line: %+v", st)
	}
}

func TestStopRefusedWhenHostInJob(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	t.Parallel()
	dir, _ := hostFixture(t)
	d := daemonAt(t, dir)
	d.opts.ShutdownToken = "tok"
	hostRunnerCard(t, d, "in-job")
	// What a probe answering in_job leaves behind.
	d.hostInJob.Store(true)

	ask := func(body string) (*httptest.ResponseRecorder, bool) {
		req := httptest.NewRequest(http.MethodPost, "/v1/shutdown?now=1", strings.NewReader(body))
		req.Header.Set("X-Atrium-Token", "tok")
		rec := httptest.NewRecorder()
		d.handleShutdown(rec, req)
		return rec, stopped(d)
	}
	rec, went := ask("")
	if rec.Code != http.StatusConflict || went {
		t.Fatalf("a stop on an in-job host: %d %q stopped=%v", rec.Code, rec.Body.String(), went)
	}
	for _, want := range []string{"inside a job object that refused breakaway", "inherited from the process that started this room", "restart this room once from a jobless start"} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("the refusal does not say %q: %q", want, rec.Body.String())
		}
	}
	if d.stopRunners.Load() {
		t.Fatal("a refused stop changed the daemon")
	}
	rec, _ = ask(`{"runners":true}`)
	if rec.Code != http.StatusOK || !d.stopRunners.Load() {
		t.Fatalf("--runners was refused: %d %q", rec.Code, rec.Body.String())
	}
	hostWait(t, 5*time.Second, "the stop to be requested", func() bool { return stopped(d) })
}

func TestStopWaitsForANewContextCycle(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	t.Parallel()
	dir, _ := hostFixture(t)
	d := daemonAt(t, dir)
	task, _ := hostRunnerCard(t, d, "gate-nctx")
	d.gatePoll = 10 * time.Millisecond

	d.nctx.begin(task.ID, "HANDOFF.x.md", "")
	done := make(chan struct{})
	go func() { d.shortGate(); close(done) }()
	select {
	case <-done:
		t.Fatal("the stop went ahead in the middle of a new-context cycle")
	case <-time.After(300 * time.Millisecond):
	}
	d.nctx.clear(task.ID)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the stop is still waiting after the cycle ended")
	}

	// A gate nobody releases gives up and lets the stop go, ending nothing.
	d.gateGiveUp = 200 * time.Millisecond
	d.nctx.begin(task.ID, "HANDOFF.y.md", "")
	start := time.Now()
	d.shortGate()
	if took := time.Since(start); took > 3*time.Second {
		t.Fatalf("the gate waited %s past its bound", took)
	}
}

func TestHostStateCountsHostRunners(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	t.Parallel()
	dir, _ := hostFixture(t)
	d := daemonAt(t, dir)
	hostRunnerCard(t, d, "preflight-host")

	get := func() (on, inJob bool, host, local int) {
		w := httptest.NewRecorder()
		d.handleHostState(w, httptest.NewRequest("GET", "/v1/preflight", nil))
		var got struct {
			PtyHost struct {
				On    bool `json:"on"`
				InJob bool `json:"in_job"`
			} `json:"pty_host"`
			Runners struct {
				Host      int `json:"host"`
				InProcess int `json:"in_process"`
			} `json:"runners"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		return got.PtyHost.On, got.PtyHost.InJob, got.Runners.Host, got.Runners.InProcess
	}
	// The probe reports the test process's own job: a test run from a supervised room shell is in one.
	d.hostInJob.Store(false)
	on, inJob, host, local := get()
	if !on || inJob || host != 1 || local != 0 {
		t.Fatalf("preflight on=%v in_job=%v host=%d in_process=%d, want on, not in a job, 1 and 0", on, inJob, host, local)
	}
	d.hostInJob.Store(true)
	if _, inJob, _, _ := get(); !inJob {
		t.Fatal("in_job did not follow the probe")
	}
	w := httptest.NewRecorder()
	d.handleHostState(w, httptest.NewRequest("GET", "/v1/preflight", nil))
	if !strings.Contains(w.Body.String(), "in_job_cause") || !strings.Contains(w.Body.String(), "refused breakaway") {
		t.Fatalf("a host in a job does not say why: %s", w.Body.String())
	}
}
