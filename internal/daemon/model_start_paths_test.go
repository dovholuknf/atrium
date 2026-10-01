package daemon

import (
	"encoding/json"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// EVERY START PATH LAUNCHES WITH THE CARD'S MODEL. Storing it is half the job
// (r-card-model): a `/model` switch that a room restart's resume puts back on the
// runner's default is a switch that did not happen.
//
// A shell stands in for the runner, with a model template it will not choke on,
// because what is under test is the argv the launch path builds. The launched
// event carries it in `cmd`, which is what these read.

const modelStartModel = "claude-sonnet-5-5"

// modelShellCard launches a card on a shell whose harness maps a model.
func modelShellCard(t *testing.T, d *Daemon) *store.Task {
	t.Helper()
	cmd, args, model := "sh", []string{"-c", "read x"}, []string{"--model", "{model}"}
	if runtime.GOOS == "windows" {
		cmd, args, model = "cmd.exe", []string{"/d", "/k"}, []string{"rem", "{model}"}
	}
	home := t.TempDir()
	sealed := map[string]string{"HOME": home, "USERPROFILE": home, "ATRIUM_HUB_URL": "http://127.0.0.1:1",
		"ATRIUM_BOARD_URL": "http://127.0.0.1:1"}
	if _, err := d.st.SaveHarness(store.Harness{ID: "modeltest", Label: "model test", Enabled: true,
		Cmd: cmd, Args: args, ModelArgs: model, LaunchMode: store.LaunchPTY, Env: sealed}); err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp("", "atrium-model-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	task, err := d.Launch(LaunchRequest{Harness: "modeltest", Cwd: dir})
	if err != nil {
		t.Skipf("could not spawn a test runner on this machine: %v", err)
	}
	t.Cleanup(func() {
		if r := d.sup.get(task.ID); r != nil {
			windDown(r, time.Second, d.exitKeysFor(task.ID))
		}
		for end := time.Now().Add(10 * time.Second); time.Now().Before(end) && d.sup.get(task.ID) != nil; {
			time.Sleep(50 * time.Millisecond)
		}
	})
	return task
}

// lastLaunch is the newest launched event on a card: its command line and model.
func lastLaunch(t *testing.T, d *Daemon, id string) (cmd, model string, n int) {
	t.Helper()
	evs, err := d.st.Events(id, 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range evs {
		if e.Kind != store.EventLaunched {
			continue
		}
		var p struct{ Cmd, Model string }
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			t.Fatal(err)
		}
		cmd, model = p.Cmd, p.Model
		n++
	}
	return cmd, model, n
}

// THE ROOM RESTART. The wind-down saves what was open, every session ends, and
// the boot resumes each card from the store. A model recorded after the card first
// started has to be on the command line the resume builds.
func TestARoomRestartResumesACardOnTheModelItWasSwitchedTo(t *testing.T) {
	d := testDaemon(t)
	task := modelShellCard(t, d)
	if _, model, _ := lastLaunch(t, d, task.ID); model != "" {
		t.Fatalf("the first launch named model %q, want the default", model)
	}
	if err := d.st.SetModel(task.ID, modelStartModel); err != nil {
		t.Fatal(err)
	}

	d.saveReopen([]*runner{d.sup.get(task.ID)})
	if !d.stopOne(task.ID, 5*time.Second) {
		t.Fatal("the card had no runner to stop")
	}
	waitGone(t, d, task.ID)
	if err := d.onSession(SessionEvent{Agent: task.WireName, Event: "end", Reason: "prompt_input_exit",
		TaskID: task.ID, Runner: "modeltest"}); err != nil {
		t.Fatal(err)
	}

	d.reopenSaved()
	if d.sup.get(task.ID) == nil {
		t.Fatal("the card did not come back")
	}
	cmd, model, n := lastLaunch(t, d, task.ID)
	if n != 2 || model != modelStartModel || !strings.Contains(cmd, modelStartModel) {
		t.Fatalf("the resume launched %d times, model %q, command %q: want the second on %s",
			n, model, cmd, modelStartModel)
	}
	if got, _ := d.st.Get(task.ID); got.Model != modelStartModel {
		t.Fatalf("the card's model after the resume is %q", got.Model)
	}
}

// A PARKED CARD'S WAKE, a restart of one runner, an unshelve, a fixture and a
// board relaunch all name the card and no model. Each takes the card's own.
func TestALaunchOntoACardNamingNoModelKeepsTheCardsModel(t *testing.T) {
	d := testDaemon(t)
	task := modelShellCard(t, d)
	if err := d.st.SetModel(task.ID, modelStartModel); err != nil {
		t.Fatal(err)
	}
	if err := d.StopRunner(task.ID); err != nil {
		t.Fatal(err)
	}
	waitGone(t, d, task.ID)

	if _, err := d.Launch(LaunchRequest{Harness: "modeltest", Cwd: task.Worktree, TaskID: task.ID}); err != nil {
		t.Fatal(err)
	}
	cmd, model, n := lastLaunch(t, d, task.ID)
	if n != 2 || model != modelStartModel || !strings.Contains(cmd, modelStartModel) {
		t.Fatalf("the relaunch ran %d times, model %q, command %q: want the second on %s", n, model, cmd, modelStartModel)
	}
}

// The request the park wake, the one-runner restart and the reopen all build.
func TestTheRequestAStartBuildsCarriesTheCardsModel(t *testing.T) {
	d := testDaemon(t)
	task := cardFor(t, d, "on-a-model")
	if err := d.st.SetModel(task.ID, modelStartModel); err != nil {
		t.Fatal(err)
	}
	fresh, _ := d.st.Get(task.ID)
	if got := d.reopenRequest(fresh).Model; got != modelStartModel {
		t.Fatalf("the request carries model %q, want %s", got, modelStartModel)
	}
}
