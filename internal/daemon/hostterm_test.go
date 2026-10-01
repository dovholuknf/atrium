package daemon

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/ptyhost"
	"github.com/dovholuknf/atrium/internal/store"
)

// THE SETTING ON, against a real host. Each test here runs a host in this process (ptyhost.Listen) on the state
// dir the daemon's own database lives in, so `ptyhost.Address` matches, and runs a real shell under it. Nothing
// touches the live room. A "daemon crash" is `closeDB` and a new `New` on the same database, which is what a
// restarted daemon is as far as the host can tell.

func hostFixture(t *testing.T) (dir string, h *ptyhost.Host) {
	t.Helper()
	dir = t.TempDir()
	h, err := ptyhost.Listen(dir, ptyhost.Options{Logf: func(f string, a ...any) { t.Logf("host: "+f, a...) }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.Close)
	return dir, h
}

// daemonAt is a daemon with no listeners on the database in dir, with the setting on.
func daemonAt(t *testing.T, dir string) *Daemon {
	t.Helper()
	d, err := New(Options{
		AgentAddr:    freePort(t),
		HumanAddr:    freePort(t),
		DBPath:       filepath.ToSlash(filepath.Join(dir, "atrium.db")),
		LongPoll:     time.Second,
		LocationFile: filepath.Join(dir, "daemon.json"),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := d.st.SetSetting(store.SettingPtyHost, "on"); err != nil {
		t.Fatal(err)
	}
	return d
}

// scriptArgv is a shell command line that runs script.
func scriptArgv(t *testing.T, script string) (string, []string) {
	t.Helper()
	sh, args := someShell(t)
	resolved, err := exec.LookPath(sh)
	if err != nil {
		t.Fatal(err)
	}
	return resolved, append(append([]string(nil), args...), script)
}

func hostWait(t *testing.T, d time.Duration, what string, f func() bool) {
	t.Helper()
	end := time.Now().Add(d)
	for !f() {
		if time.Now().After(end) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func ringHas(r *runner, s string) bool {
	return bytes.Contains(r.buf.Snapshot(), []byte(s))
}

func ringWritten(r *runner) int64 {
	r.buf.mu.Lock()
	defer r.buf.mu.Unlock()
	return r.buf.written
}

// spawnDirect starts a run in the host through the daemon's own link and records it, with no runner and no reader
// on this side: what a daemon that then went away leaves behind.
func spawnDirect(t *testing.T, d *Daemon, taskID, kind, script string) string {
	t.Helper()
	cl, addr, err := d.hostClient(false, true)
	if err != nil {
		t.Fatal(err)
	}
	sh, argv := scriptArgv(t, script)
	sp, err := cl.Spawn(ptyhost.SpawnArgs{
		ID: taskID, Kind: kind, Argv: append([]string{sh}, argv...), Cwd: t.TempDir(),
		Cols: 100, Rows: 30, Ring: 1 << 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.st.RecordRun(store.PtyRun{RunID: sp.RunID, TaskID: taskID, Kind: kind, Host: addr}); err != nil {
		t.Fatal(err)
	}
	return sp.RunID
}

func listOf(t *testing.T, d *Daemon) map[string]ptyhost.PtyInfo {
	t.Helper()
	cl, _, err := d.hostClient(true, false)
	if err != nil {
		t.Fatal(err)
	}
	l, err := cl.List()
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]ptyhost.PtyInfo{}
	for _, i := range l {
		out[i.RunID] = i
	}
	return out
}

// restarted is a new daemon on the same database, the old one gone.
func restarted(t *testing.T, old *Daemon, dir string) *Daemon {
	t.Helper()
	_ = old.Close()
	return daemonAt(t, dir)
}

func TestReattachReplaysTheRingAndFollowsWithNothingLostOrRepeated(t *testing.T) {
	dir, _ := hostFixture(t)
	d1 := daemonAt(t, dir)
	task := cardAt(t, d1, "reattach-replay", t.TempDir())
	sh, args := someShell(t)
	if _, err := d1.spawnPTY(task.ID, sh, append(args,
		"echo AAA-first; sleep 4; echo BBB-second; sleep 4; echo CCC-third; sleep 60"),
		t.TempDir(), os.Environ()); err != nil {
		t.Fatal(err)
	}
	r1 := d1.sup.get(task.ID)
	if hostOf(r1) == nil {
		t.Fatal("the runner is not on the host")
	}
	row, err := d1.st.Run(r1.runID)
	if err != nil || row.Host == "" {
		t.Fatalf("the run was recorded with no host: %+v %v", row, err)
	}
	hostWait(t, 20*time.Second, "AAA in the first daemon's ring", func() bool { return ringHas(r1, "AAA-first") })
	cl1, _, _ := d1.hostClient(false, false)
	cut, err := cl1.Resize(r1.runID, 90, 25)
	if err != nil {
		t.Fatal(err)
	}

	d2 := restarted(t, d1, dir)
	d2.reattachRuns()
	r2 := d2.sup.get(task.ID)
	if r2 == nil || r2.runID != r1.runID {
		t.Fatalf("the run was not reattached: %+v", r2)
	}
	hostWait(t, 30*time.Second, "CCC in the reattached ring", func() bool { return ringHas(r2, "CCC-third") })
	// Byte for byte the host's own count: nothing lost between the replay and the live follow, and nothing twice.
	hostWait(t, 10*time.Second, "the ring to match the host's offset", func() bool {
		return ringWritten(r2) == listOf(t, d2)[r2.runID].OutOffset
	})

	out, cuts, _, _ := r2.buf.ReplayCuts()
	var at = -1
	for _, c := range cuts {
		if c.cols == 90 && c.rows == 25 {
			at = c.at
		}
	}
	if at != int(cut.Off) {
		t.Fatalf("the resize is at %d in the replay, the host cut it at %d (cuts %+v)", at, cut.Off, cuts)
	}
	a, b, c := bytes.Index(out, []byte("AAA-first")), bytes.Index(out, []byte("BBB-second")),
		bytes.Index(out, []byte("CCC-third"))
	if !(a >= 0 && a < at && b >= at && c > b) {
		t.Fatalf("output is out of order or on the wrong side of the cut: A=%d B=%d C=%d at=%d", a, b, c, at)
	}
}

func TestAResizeAfterReattachIsCutBeforeAnyByteAtTheNewSize(t *testing.T) {
	dir, _ := hostFixture(t)
	d1 := daemonAt(t, dir)
	task := cardAt(t, d1, "reattach-resize", t.TempDir())
	sh, args := someShell(t)
	if _, err := d1.spawnPTY(task.ID, sh, append(args, "echo AAA-first; sleep 6; echo BBB-second; sleep 60"),
		t.TempDir(), os.Environ()); err != nil {
		t.Fatal(err)
	}
	r1 := d1.sup.get(task.ID)
	hostWait(t, 20*time.Second, "AAA", func() bool { return ringHas(r1, "AAA-first") })

	d2 := restarted(t, d1, dir)
	d2.reattachRuns()
	r2 := d2.sup.get(task.ID)
	if r2 == nil {
		t.Fatal("not reattached")
	}
	if err := r2.tm().Resize(70, 20); err != nil {
		t.Fatal(err)
	}
	info := listOf(t, d2)[r2.runID]
	if info.Cols != 70 || info.Rows != 20 {
		t.Fatalf("the host is at %dx%d after the resize", info.Cols, info.Rows)
	}

	d3 := restarted(t, d2, dir)
	d3.reattachRuns()
	r3 := d3.sup.get(task.ID)
	if r3 == nil {
		t.Fatal("not reattached the second time")
	}
	hostWait(t, 30*time.Second, "BBB", func() bool { return ringHas(r3, "BBB-second") })
	out, cuts, _, _ := r3.buf.ReplayCuts()
	at := -1
	for _, c := range cuts {
		if c.cols == 70 && c.rows == 20 {
			at = c.at
		}
	}
	if at < 0 {
		t.Fatalf("no cut at the new size: %+v", cuts)
	}
	if b := bytes.Index(out, []byte("BBB-second")); b < at {
		t.Fatalf("a byte written at the new size (at %d) sits ahead of its cut (%d)", b, at)
	}
	if a := bytes.Index(out, []byte("AAA-first")); a >= at {
		t.Fatalf("a byte written before the resize (at %d) sits behind the cut (%d)", a, at)
	}
}

func TestEachVerbReachesOnlyTheRunIDItNames(t *testing.T) {
	dir, _ := hostFixture(t)
	d := daemonAt(t, dir)
	task := cardAt(t, d, "runner-and-shell", t.TempDir())
	sh, args := someShell(t)
	if _, err := d.spawnPTY(task.ID, sh, append(args, "sleep 120"), t.TempDir(), os.Environ()); err != nil {
		t.Fatal(err)
	}
	if err := d.EnsureShell(task.ID); err != nil {
		t.Fatal(err)
	}
	rn, sl := d.sup.get(task.ID), d.sup.getShell(task.ID)
	rt, st := hostOf(rn), hostOf(sl)
	if rt == nil || st == nil || rt.runID == st.runID {
		t.Fatalf("a card's runner and shell must be two runs on the host: %v %v", rt, st)
	}

	// resize
	if err := st.Resize(77, 33); err != nil {
		t.Fatal(err)
	}
	l := listOf(t, d)
	if l[st.runID].Cols != 77 || l[rt.runID].Cols == 77 {
		t.Fatalf("the resize did not stay with the shell: shell %dx%d runner %dx%d",
			l[st.runID].Cols, l[st.runID].Rows, l[rt.runID].Cols, l[rt.runID].Rows)
	}

	// write: `exit` typed into the shell ends the shell and nothing else
	if _, err := st.Write([]byte("exit\r")); err != nil {
		t.Fatal(err)
	}
	hostWait(t, 30*time.Second, "the shell to close", func() bool { return d.sup.getShell(task.ID) == nil })
	if d.sup.get(task.ID) == nil {
		t.Fatal("typing into the shell ended the runner")
	}
	hostWait(t, 10*time.Second, "the shell's exit to be collected", func() bool {
		_, still := listOf(t, d)[st.runID]
		return !still
	})
	if l := listOf(t, d); l[rt.runID].Exited || len(l) != 1 {
		t.Fatalf("the runner should be the one live run left: %+v", l)
	}
	if n := len(filedExits(t, d, task.ID)); n != 0 {
		t.Fatalf("a shell closing filed %d runner exits", n)
	}

	// signal: a kill of the runner ends the runner, once
	if err := rt.Kill(); err != nil {
		t.Fatal(err)
	}
	hostWait(t, 20*time.Second, "the runner's exit to be filed", func() bool { return len(filedExits(t, d, task.ID)) == 1 })
}

func TestALiveExitIsFiledOnceAndCollectedAfterIt(t *testing.T) {
	dir, _ := hostFixture(t)
	d := daemonAt(t, dir)
	task := cardAt(t, d, "live-exit", t.TempDir())
	sh, args := someShell(t)
	if _, err := d.spawnPTY(task.ID, sh, append(args, "echo bye; exit 3"), t.TempDir(), os.Environ()); err != nil {
		t.Fatal(err)
	}
	runID := d.sup.get(task.ID).runID
	hostWait(t, 30*time.Second, "the exit to be filed", func() bool { return len(filedExits(t, d, task.ID)) == 1 })
	hostWait(t, 10*time.Second, "the host to forget the run", func() bool {
		_, still := listOf(t, d)[runID]
		return !still
	})
	time.Sleep(300 * time.Millisecond)
	if n := len(filedExits(t, d, task.ID)); n != 1 {
		t.Fatalf("%d exits filed", n)
	}
	if n := deadChanges(t, d, task.ID); n != 1 {
		t.Fatalf("the card went dead %d times", n)
	}
	if d.sup.get(task.ID) != nil {
		t.Fatal("an exited runner is still supervised")
	}
	row, _ := d.st.Run(runID)
	if row == nil || !row.Filed {
		t.Fatalf("the run is not marked filed: %+v", row)
	}
}

// A crash between FileExit and collect must lose nothing and file nothing new.
func TestACrashBetweenFilingAndCollectingFilesNothingTwice(t *testing.T) {
	dir, _ := hostFixture(t)
	d1 := daemonAt(t, dir)
	task := cardAt(t, d1, "crash-window", t.TempDir())
	runID := spawnDirect(t, d1, task.ID, store.RunKindRunner, "echo done; exit 2")
	hostWait(t, 30*time.Second, "the run to exit", func() bool { return listOf(t, d1)[runID].Exited })
	// Filed, and never collected: the daemon died in between.
	if !d1.fileExit(runExit{taskID: task.ID, runID: runID, code: 2, lived: time.Hour}) {
		t.Fatal("the first filing was refused")
	}
	if _, still := listOf(t, d1)[runID]; !still {
		t.Fatal("the host forgot the run without being told")
	}

	d2 := restarted(t, d1, dir)
	d2.reattachRuns()
	hostWait(t, 10*time.Second, "the run to be collected", func() bool {
		_, still := listOf(t, d2)[runID]
		return !still
	})
	if n := len(filedExits(t, d2, task.ID)); n != 1 {
		t.Fatalf("%d exit events after the reattach, want the one", n)
	}
	if n := deadChanges(t, d2, task.ID); n != 1 {
		t.Fatalf("the card went dead %d times", n)
	}
}

// A runner that dies inside the startup window while no daemon is connected must leave the card as the live path
// would have: the same tail and the same `why`.
func TestAStartupFailureWhileNoDaemonIsConnectedRecordsWhatTheLivePathDoes(t *testing.T) {
	dir, _ := hostFixture(t)
	d1 := daemonAt(t, dir)
	const say = "echo boom-message; exit 7"

	live := cardAt(t, d1, "startup-live", t.TempDir())
	sh, args := someShell(t)
	if _, err := d1.spawnPTY(live.ID, sh, append(args, say), t.TempDir(), os.Environ()); err != nil {
		t.Fatal(err)
	}
	hostWait(t, 30*time.Second, "the live exit", func() bool { return len(filedExits(t, d1, live.ID)) == 1 })

	gap := cardAt(t, d1, "startup-gap", t.TempDir())
	runID := spawnDirect(t, d1, gap.ID, store.RunKindRunner, say)
	d2 := restarted(t, d1, dir)
	hostWait(t, 30*time.Second, "the run to exit with no daemon attached", func() bool {
		return listOf(t, d2)[runID].Exited
	})
	d2.reattachRuns()

	ex := filedExits(t, d2, gap.ID)
	if len(ex) != 1 {
		t.Fatalf("%d exits filed for the card that died in the gap", len(ex))
	}
	want := filedExits(t, d2, live.ID)[0]
	if ex[0]["output"] != want["output"] || ex[0]["output"] == nil {
		t.Fatalf("tail differs: gap %q, live %q", ex[0]["output"], want["output"])
	}
	a, _ := d2.st.Get(live.ID)
	b, _ := d2.st.Get(gap.ID)
	if a.Why == "" || a.Why != b.Why {
		t.Fatalf("why differs: live %q, gap %q", a.Why, b.Why)
	}
	if b.Status != store.StatusDead {
		t.Fatalf("the card is %q, not dead", b.Status)
	}
}

func TestAnUnreachableHostFallsBackToAnInProcessTerminal(t *testing.T) {
	dir := t.TempDir() // no host here
	d := daemonAt(t, dir)
	d.ph.opts = ptyhost.StartOptions{Exe: filepath.Join(dir, "no-such-atrium.exe")}
	task := cardAt(t, d, "no-host", t.TempDir())
	sh, args := someShell(t)
	if _, err := d.spawnPTY(task.ID, sh, append(args, "sleep 60"), t.TempDir(), os.Environ()); err != nil {
		t.Fatalf("the setting made a launch fail: %v", err)
	}
	r := d.sup.get(task.ID)
	if r == nil || hostOf(r) != nil {
		t.Fatalf("want an in-process runner, got %+v", r)
	}
	if row, err := d.st.Run(r.runID); err != nil || row.Host != "" {
		t.Fatalf("an in-process run must record no host: %+v %v", row, err)
	}
	// And a shell, which goes through the same place. The failure is remembered, so this does not dial again.
	if err := d.EnsureShell(task.ID); err != nil {
		t.Fatalf("a shell failed to open: %v", err)
	}
	if hostOf(d.sup.getShell(task.ID)) != nil {
		t.Fatal("the shell is on a host that is not there")
	}
	d.CloseShell(task.ID)
	_ = r.tm().Kill()
}

// An old exited run and a newer live one on the same card: the old end is history, and the card is not dead.
func TestAnOldExitedRunDoesNotKillACardWithANewerLiveOne(t *testing.T) {
	dir, _ := hostFixture(t)
	d1 := daemonAt(t, dir)
	task := cardAt(t, d1, "old-and-new", t.TempDir())
	oldRun := spawnDirect(t, d1, task.ID, store.RunKindRunner, "echo old-one; exit 1")
	hostWait(t, 30*time.Second, "the old run to exit", func() bool { return listOf(t, d1)[oldRun].Exited })
	newRun := spawnDirect(t, d1, task.ID, store.RunKindRunner, "sleep 120")

	d2 := restarted(t, d1, dir)
	d2.reattachRuns()
	r := d2.sup.get(task.ID)
	if r == nil || r.runID != newRun {
		t.Fatalf("the live run is not the one supervised: %+v", r)
	}
	if n := len(filedExits(t, d2, task.ID)); n != 1 {
		t.Fatalf("%d exit events, want the old run's one", n)
	}
	if row, _ := d2.st.Run(oldRun); row == nil || !row.Filed {
		t.Fatalf("the old run is not marked filed: %+v", row)
	}
	got, _ := d2.st.Get(task.ID)
	if got.Status == store.StatusDead || got.Why != "" {
		t.Fatalf("the card is %q with why %q, but it has a live runner", got.Status, got.Why)
	}
	hostWait(t, 10*time.Second, "the old run to be collected", func() bool {
		_, still := listOf(t, d2)[oldRun]
		return !still
	})
	if l := listOf(t, d2); l[newRun].Exited {
		t.Fatal("the live run was touched")
	}
	_ = r.tm().Kill()
}

// Off is the rollback: a host started while the setting was on keeps its runners after it is turned off, and the
// next start must pick them up rather than leave them to be started a second time.
func TestAHostStillHoldingRunnersIsReattachedWithTheSettingOff(t *testing.T) {
	dir, _ := hostFixture(t)
	d1 := daemonAt(t, dir)
	task := cardAt(t, d1, "setting-off", t.TempDir())
	run := spawnDirect(t, d1, task.ID, store.RunKindRunner, "sleep 120")

	d2 := restarted(t, d1, dir)
	if err := d2.st.SetSetting(store.SettingPtyHost, "off"); err != nil {
		t.Fatal(err)
	}
	d2.reattachRuns()
	r := d2.sup.get(task.ID)
	if r == nil || r.runID != run || hostOf(r) == nil {
		t.Fatalf("the host's run is not the card's runner with the setting off: %+v", r)
	}
	live := 0
	for _, i := range listOf(t, d2) {
		if i.ID == task.ID && !i.Exited {
			live++
		}
	}
	if live != 1 {
		t.Fatalf("%d live runs for the card, want the host's one", live)
	}
	_ = r.tm().Kill()
}

// A Spawn whose reply never came leaves a run nobody knows the id of. The fallback must not be a second copy.
func TestAStartThatDidNotAnswerLeavesNoRunBehindTheFallback(t *testing.T) {
	dir, _ := hostFixture(t)
	d := daemonAt(t, dir)
	task := cardAt(t, d, "stray", t.TempDir())
	stray := spawnDirect(t, d, task.ID, store.RunKindRunner, "sleep 120")
	other := spawnDirect(t, d, task.ID, store.RunKindShell, "sleep 120")

	d.killStrays(task.ID, store.RunKindRunner)
	hostWait(t, 20*time.Second, "the stray runner to end", func() bool { return listOf(t, d)[stray].Exited })
	if listOf(t, d)[other].Exited {
		t.Fatal("the card's shell was ended with its runner")
	}
	d.killStrays(task.ID, store.RunKindShell)
	hostWait(t, 20*time.Second, "the stray shell to end", func() bool { return listOf(t, d)[other].Exited })
}

// The run this daemon already holds is not a stray.
func TestStrayCleanupSparesTheRunThisDaemonHolds(t *testing.T) {
	dir, _ := hostFixture(t)
	d := daemonAt(t, dir)
	task := cardAt(t, d, "spared", t.TempDir())
	sh, args := someShell(t)
	if _, err := d.spawnPTY(task.ID, sh, append(args, "sleep 120"), t.TempDir(), os.Environ()); err != nil {
		t.Fatal(err)
	}
	r := d.sup.get(task.ID)
	d.killStrays(task.ID, store.RunKindRunner)
	time.Sleep(500 * time.Millisecond)
	if listOf(t, d)[r.runID].Exited {
		t.Fatal("the daemon's own runner was ended as a stray")
	}
	_ = r.tm().Kill()
}
