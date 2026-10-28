//go:build integration

package daemon

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

type taskHub struct {
	*fakeRelay
	mu     sync.Mutex
	events []map[string]any
	answer func(ev map[string]any) (RelayResult, error)
}

func (h *taskHub) Hub(_ context.Context, _, _, path string, body json.RawMessage) (RelayResult, error) {
	if path != taskSyncPath {
		return RelayResult{OK: true, Data: json.RawMessage(`{"ok":true}`)}, nil
	}
	var ev map[string]any
	_ = json.Unmarshal(body, &ev)
	h.mu.Lock()
	h.events = append(h.events, ev)
	answer := h.answer
	h.mu.Unlock()
	if answer != nil {
		if res, err := answer(ev); err != nil || !res.OK || res.Data != nil {
			return res, err
		}
	}
	if ev["event"] == "file" {
		name, _ := ev["path"].(string)
		return RelayResult{OK: true, Data: json.RawMessage(`{"ok":true,"name":"f-x-t1-` + name + `","size":1,"sha256":"ab"}`)}, nil
	}
	return RelayResult{OK: true, Data: json.RawMessage(`{"ok":true,"applied":true}`)}, nil
}

func (h *taskHub) setAnswer(f func(ev map[string]any) (RelayResult, error)) {
	h.mu.Lock()
	h.answer = f
	h.mu.Unlock()
}

func (h *taskHub) kinds() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []string
	for _, ev := range h.events {
		k, _ := ev["event"].(string)
		out = append(out, k)
	}
	return out
}

type taskRig struct {
	d        *Daemon
	hub      *taskHub
	launcher *store.Task
	card     *store.Task
	row      store.CardHome
	folder   string
}

func newTaskRig(t *testing.T, outputs ...string) *taskRig {
	t.Helper()
	d, _ := homeRoom(t)
	hub := &taskHub{fakeRelay: &fakeRelay{}}
	d.SetRelay(hub)
	x := &taskRig{d: d, hub: hub}
	x.launcher, x.card, _ = itemRunning(t, d, "")
	x.attempt(t, 1, outputs...)
	return x
}

func (x *taskRig) attempt(t *testing.T, n int, outputs ...string) {
	t.Helper()
	folder, err := x.d.taskFolder(&taskSpec{BacklogID: "f-x", TaskID: "t1", Attempt: n, Outputs: outputs})
	if err != nil {
		t.Fatal(err)
	}
	if n > 1 {
		x.card = peerCard(t, x.d, "t1-a"+itoa(n))
	}
	if err := x.d.st.SetTags(x.card.ID, []string{"sdk", OriginAgentTag, ItemTag, TaskTagPrefix + "t1"}); err != nil {
		t.Fatal(err)
	}
	if err := x.d.st.CardHomeCard(filepath.ToSlash(folder), x.card.ID); err != nil {
		t.Fatal(err)
	}
	x.card, _ = x.d.st.Get(x.card.ID)
	x.folder = folder
	x.row, _, _ = x.d.st.CardHomeByPath(filepath.ToSlash(folder))
}

func (x *taskRig) write(t *testing.T, name string, size int) string {
	t.Helper()
	p := filepath.Join(x.folder, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(strings.Repeat("x", size)), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func (x *taskRig) capture() {
	x.d.captureTask(x.card, FinishRequest{Recap: "done: wrote out.txt", Why: "it was asked", Learned: "use the cache"}, false)
}

func (x *taskRig) ledger(t *testing.T) store.CardHome {
	t.Helper()
	row, ok, err := x.d.st.CardHomeByPath(x.row.Path)
	if err != nil || !ok {
		t.Fatalf("no ledger row for %s: %v", x.row.Path, err)
	}
	return row
}

func gone(path string) bool {
	_, err := os.Lstat(path)
	return os.IsNotExist(err)
}

func TestACapturedTaskSendsItsFilesAndRemovesTheFolderOnlyAfterTheHubTookIt(t *testing.T) {
	x := newTaskRig(t, "out.txt", "logs")
	x.write(t, "out.txt", 10)
	x.write(t, "logs/a.log", 3)
	x.write(t, "RECAP.md", 5)
	x.write(t, "scratch.tmp", 5)
	var atAck bool
	x.hub.setAnswer(func(ev map[string]any) (RelayResult, error) {
		if ev["event"] == "captured" {
			atAck = !gone(x.folder)
		}
		return RelayResult{OK: true}, nil
	})
	x.capture()
	if got := strings.Join(x.hub.kinds(), ","); got != "capturing,file,file,captured,reaped" {
		t.Fatalf("events: %s", got)
	}
	if !atAck {
		t.Fatal("the folder was gone before the hub took the result")
	}
	if !gone(x.folder) {
		t.Fatal("the folder is still there after the hub took the result")
	}
	if row := x.ledger(t); row.State != store.CardHomeRemoved || row.Owed != "" {
		t.Fatalf("ledger: %+v", row)
	}
	captured := x.hub.events[3]
	if captured["why"] != "it was asked" || captured["learned"] != "use the cache" || captured["line"] != "wrote out.txt" ||
		captured["recap"] != "xxxxx" {
		t.Fatalf("captured: %v", captured)
	}
	if files, _ := captured["files"].([]any); len(files) != 2 {
		t.Fatalf("files: %v", captured["files"])
	}
}

func TestAHubDownAtThePostKeepsTheFolderAndSendsWhatIsOwedWhenItIsBack(t *testing.T) {
	x := newTaskRig(t, "out.txt")
	x.write(t, "out.txt", 10)
	var mu sync.Mutex
	down := false
	x.hub.setAnswer(func(ev map[string]any) (RelayResult, error) {
		mu.Lock()
		defer mu.Unlock()
		if ev["event"] == "captured" {
			down = true
		}
		if down {
			return RelayResult{}, ErrRelayDown
		}
		return RelayResult{OK: true}, nil
	})
	x.capture()
	if gone(x.folder) {
		t.Fatal("the folder went with the hub down")
	}
	row := x.ledger(t)
	if row.State != store.CardHomeKept || !strings.Contains(row.Owed, `"capture-failed"`) {
		t.Fatalf("ledger: %+v", row)
	}
	mu.Lock()
	down = false
	mu.Unlock()
	before := len(x.hub.kinds())
	x.d.flushTaskOwed()
	if got := x.hub.kinds()[before:]; len(got) != 1 || got[0] != "capture-failed" {
		t.Fatalf("replayed: %v", got)
	}
	if row := x.ledger(t); row.Owed != "" || gone(x.folder) {
		t.Fatalf("after the replay: %+v, folder gone %v", row, gone(x.folder))
	}
}

func TestAnOutputOverFiveMegabytesFailsTheCaptureKeepsTheFolderAndTellsTheOwnerOnce(t *testing.T) {
	x := newTaskRig(t, "big.csv")
	x.write(t, "big.csv", taskCaptureFileMax+1)
	x.capture()
	if gone(x.folder) {
		t.Fatal("the folder went")
	}
	row := x.ledger(t)
	if row.State != store.CardHomeKept || !strings.Contains(row.Reason, "big.csv") {
		t.Fatalf("ledger: %+v", row)
	}
	if got := x.hub.kinds(); strings.Join(got, ",") != "capturing,capture-failed" {
		t.Fatalf("events: %v", got)
	}
	notes := noticesFromAtrium(t, x.d, x.launcher.ID)
	if len(notes) != 1 || !strings.Contains(notes[0], "big.csv") || !strings.Contains(notes[0], x.row.Path) {
		t.Fatalf("notices: %v", notes)
	}
}

func TestAnOutputLinkPointingOutsideTheFolderFailsTheCapture(t *testing.T) {
	x := newTaskRig(t, "out")
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	linkDir(t, outside, filepath.Join(x.folder, "out"))
	x.capture()
	if row := x.ledger(t); row.State != store.CardHomeKept || !strings.Contains(row.Reason, "out") {
		t.Fatalf("ledger: %+v", row)
	}
	for _, k := range x.hub.kinds() {
		if k == "file" || k == "captured" {
			t.Fatalf("sent %s for a link outside: %v", k, x.hub.kinds())
		}
	}
	if gone(x.folder) {
		t.Fatal("the folder went")
	}
}

func TestTheRemovalTakesALinkInsideTheFolderAndLeavesItsTarget(t *testing.T) {
	x := newTaskRig(t)
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "precious.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	linkDir(t, target, filepath.Join(x.folder, "link"))
	x.capture()
	if !gone(x.folder) {
		t.Fatal("the folder is still there")
	}
	if _, err := os.Stat(filepath.Join(target, "precious.txt")); err != nil {
		t.Fatal("the removal followed the link")
	}
}

func TestATaskFolderWithNoLedgerRowIsNeverRemoved(t *testing.T) {
	x := newTaskRig(t)
	if err := x.d.st.CardHomeCard(x.row.Path, "someone-else"); err != nil {
		t.Fatal(err)
	}
	x.capture()
	if gone(x.folder) {
		t.Fatal("a folder with no ledger row for the card was removed")
	}
	if got := x.hub.kinds(); len(got) != 0 {
		t.Fatalf("events: %v", got)
	}
}

func TestATaskLedgerPathOutsideTheCardRootIsNotRemoved(t *testing.T) {
	x := newTaskRig(t)
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "keep.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	root, _ := x.d.cardRoot()
	for _, p := range []string{outside, root} {
		x.d.reapTaskFolder(store.CardHome{Path: filepath.ToSlash(p), BacklogID: "f-x", TaskID: "t1", Attempt: 1})
		if gone(p) {
			t.Fatalf("%s was removed", p)
		}
	}
}

func TestARunnerStillAliveAfterTheWaitIsCaptureFailedWithTheFolderKept(t *testing.T) {
	x := newTaskRig(t, "out.txt")
	x.d.hk.taskExitWait = 100 * time.Millisecond
	x.write(t, "out.txt", 1)
	r := &runner{taskID: x.card.ID}
	x.d.sup.add(r)
	t.Cleanup(func() { x.d.sup.remove(x.card.ID) })
	x.capture()
	if row := x.ledger(t); row.State != store.CardHomeKept || gone(x.folder) {
		t.Fatalf("ledger: %+v, folder gone %v", row, gone(x.folder))
	}
}

func TestAWorkerThatEndsWithNoDoneFailsTheTaskAndKeepsTheFolder(t *testing.T) {
	x := newTaskRig(t)
	x.d.taskRunnerEnded(runExit{taskID: x.card.ID, code: 3})
	if gone(x.folder) {
		t.Fatal("the folder went")
	}
	row := x.ledger(t)
	if row.State != store.CardHomeKept || !strings.Contains(row.Reason, "no done") || row.Owed != "" {
		t.Fatalf("ledger: %+v", row)
	}
	if got := x.hub.kinds(); len(got) != 1 || got[0] != "failed" {
		t.Fatalf("events: %v", got)
	}
}

func TestARetryRunsInANewFolderAndItsCaptureRemovesBoth(t *testing.T) {
	x := newTaskRig(t)
	x.d.taskRunnerEnded(runExit{taskID: x.card.ID, code: 1})
	first := x.folder
	x.attempt(t, 2)
	if x.folder == first {
		t.Fatal("attempt 2 reused attempt 1's folder")
	}
	x.capture()
	if !gone(first) || !gone(x.folder) {
		t.Fatalf("after attempt 2 was captured: first gone %v, second gone %v", gone(first), gone(x.folder))
	}
	rows, _ := x.d.st.CardHomesOf("f-x", "t1")
	for _, r := range rows {
		if r.State != store.CardHomeRemoved {
			t.Fatalf("row: %+v", r)
		}
	}
}

func TestATaskCardIsKeptWithItsHistoryAfterTheFolderGoes(t *testing.T) {
	x := newTaskRig(t)
	x.capture()
	if !gone(x.folder) {
		t.Fatal("the folder is still there")
	}
	got, err := x.d.st.Get(x.card.ID)
	if err != nil || got == nil || taskOfTags(got.Tags) != "t1" {
		t.Fatalf("the card after reaping: %+v %v", got, err)
	}
}

func TestACardWithNoTaskTagIsNeverCapturedOrFailedAsATask(t *testing.T) {
	x := newTaskRig(t)
	if err := x.d.st.SetTags(x.card.ID, []string{"sdk"}); err != nil {
		t.Fatal(err)
	}
	human, _ := x.d.st.Get(x.card.ID)
	if isTaskCard(human) {
		t.Fatal("a card with no task tag is a task card")
	}
	x.d.startTaskCapture(human, FinishRequest{}, false)
	x.d.taskRunnerEnded(runExit{taskID: human.ID, code: 1})
	if got := x.hub.kinds(); len(got) != 0 || gone(x.folder) || x.ledger(t).State != store.CardHomeLive {
		t.Fatalf("events %v, folder gone %v, ledger %+v", got, gone(x.folder), x.ledger(t))
	}
	if taskOfTags([]string{TaskTagPrefix + "../t1"}) != "" {
		t.Fatal("a task tag that is not a task id was taken")
	}
}

func TestATaskLaunchIsRefusedWhereTheCardRootMayNotBe(t *testing.T) {
	d := testDaemon(t)
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home folder here")
	}
	if err := d.st.SetSetting(SettingCardRoot, home); err != nil {
		t.Fatal(err)
	}
	_, err = d.taskFolder(&taskSpec{BacklogID: "f-x", TaskID: "t1", Attempt: 1})
	if err == nil {
		t.Fatal("a card root at the home folder was taken")
	}
	if rows, _ := d.st.CardHomesOf("f-x", "t1"); len(rows) != 0 {
		t.Fatalf("a refused folder left a row: %+v", rows)
	}
}

func TestTheTaskBriefCarriesTheTaskTheItemTheOutputsAndTheLearnings(t *testing.T) {
	b := taskBrief(&taskSpec{BacklogID: "f-x", TaskID: "t2", Title: "count the rows", Brief: "count every row in a.csv",
		ItemTitle: "the data item", Context: "the data lives in a.csv", Outputs: []string{"count.txt"},
		Learnings: []string{"the csv has a header\nline"}}, "/cards/f-x-t2-a1-abcdef")
	for _, want := range []string{"count every row in a.csv", "the data item", "the data lives in a.csv", "- count.txt",
		"- the csv has a header line", "/cards/f-x-t2-a1-abcdef", "atrium_done", "`why`"} {
		if !strings.Contains(b, want) {
			t.Errorf("the brief has no %q:\n%s", want, b)
		}
	}
	order := []string{"## About the item", "## The task", "## Where you work", "## What to leave", "## What earlier", "## How to end"}
	at := -1
	for _, h := range order {
		i := strings.Index(b, h)
		if i < at {
			t.Fatalf("%q is out of order:\n%s", h, b)
		}
		at = i
	}
}
