package daemon

import (
	"runtime"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

// Launch options: a model and an effort each runner row maps, and extra argv
// and env passed as given. See docs/launch-options-design.md.

func claudeWithEffort() *store.Harness {
	h := claudeWithModel()
	h.EffortArgs = []string{"--effort", "{effort}"}
	return h
}

// The interviewer case, and the ORDER: base, model, effort, extra args, prompt.
func TestEffortAndExtraArgsGoBeforeThePrompt(t *testing.T) {
	args, logged, err := runnerArgsWith(claudeWithEffort(), "", "ask clint", launchOptions{
		Model: "claude-haiku-4-5-20251001", Effort: "low", Args: []string{"--verbose"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "--model claude-haiku-4-5-20251001 --effort low --verbose ask clint"
	if got := strings.Join(args, " "); got != want {
		t.Fatalf("argv is %q, want %q", got, want)
	}
	if !strings.Contains(logged, "--effort low") || strings.Contains(logged, "ask clint") {
		t.Fatalf("the logged line is %q", logged)
	}
}

// Not a list: a level atrium has never heard of is passed through, and the
// runner is the one to refuse it.
func TestAnEffortIsNotCheckedAgainstAList(t *testing.T) {
	args, _, err := runnerArgsWith(claudeWithEffort(), "", "", launchOptions{Effort: "turbo-9000"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(args, " ") != "--effort turbo-9000" {
		t.Fatalf("argv is %q", args)
	}
}

// Codex takes it as a config override, which is only a template.
func TestCodexTakesEffortAsAConfigOverride(t *testing.T) {
	h := codexLike("resume", "{resume}")
	h.EffortArgs = []string{"-c", "model_reasoning_effort={effort}"}
	args, _, err := runnerArgsWith(h, "", "", launchOptions{Effort: "high"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(args, " ") != "-c model_reasoning_effort=high" {
		t.Fatalf("argv is %q", args)
	}
}

// Refused, never ignored, for a runner with no way to take an effort.
func TestAnEffortForARunnerWithoutOneIsRefused(t *testing.T) {
	_, _, err := runnerArgsWith(claudeWithModel(), "", "", launchOptions{Effort: "low"})
	if err == nil || !strings.Contains(err.Error(), "{effort}") {
		t.Fatalf("got %v, want a refusal naming {effort}", err)
	}
	h := claudeWithModel()
	h.EffortArgs = []string{"--effort"}
	if _, _, err := runnerArgsWith(h, "", "", launchOptions{Effort: "low"}); err == nil {
		t.Fatal("effort args with no {effort} in them should be refused")
	}
}

// A row that maps a field by env var only takes it with no argv.
func TestAFieldMappedByEnvNeedsNoArgs(t *testing.T) {
	h := &store.Harness{ID: "x", Label: "x", Cmd: "x", ModelEnv: "X_MODEL", EffortEnv: "X_EFFORT"}
	args, _, err := runnerArgsWith(h, "", "", launchOptions{Model: "m1", Effort: "low"})
	if err != nil || len(args) != 0 {
		t.Fatalf("args %q err %v", args, err)
	}
	env, err := launchOptionEnv(h, map[string]string{"OTHER": "1"}, "m1", "low")
	if err != nil {
		t.Fatal(err)
	}
	if env["X_MODEL"] != "m1" || env["X_EFFORT"] != "low" || env["OTHER"] != "1" {
		t.Fatalf("env is %v", env)
	}
}

// The three collisions the design review asked to be refused.
func TestLaunchEnvCollisionsAreRefused(t *testing.T) {
	h := &store.Harness{ID: "x", Label: "x", Cmd: "x", ModelEnv: "X_MODEL", EffortEnv: "X_EFFORT"}
	if _, err := launchOptionEnv(h, map[string]string{"ATRIUM_TASK_ID": "t"}, "", ""); err == nil {
		t.Fatal("an ATRIUM_ key should be refused")
	}
	if _, err := launchOptionEnv(h, map[string]string{"x_model": "a"}, "b", ""); err == nil {
		t.Fatal("a model given twice, in env and as model, should be refused")
	}
	// Given only in env, the pass-through does its job.
	if env, err := launchOptionEnv(h, map[string]string{"X_MODEL": "a"}, "", ""); err != nil || env["X_MODEL"] != "a" {
		t.Fatalf("env %v err %v", env, err)
	}
	same := &store.Harness{ID: "y", Label: "y", Cmd: "y", ModelEnv: "Y_OPTS", EffortEnv: "Y_OPTS"}
	if _, err := launchOptionEnv(same, nil, "m", "low"); err == nil {
		t.Fatal("model and effort in one variable should be refused when both are asked for")
	}
}

// The env the runner gets: the harness's own, then the launch's, then atrium's.
func TestLaunchEnvSitsBetweenTheHarnessAndAtrium(t *testing.T) {
	harness := map[string]string{"A": "harness", "B": "harness"}
	env := childEnvFrom(nil, overEnv(harness, map[string]string{"B": "launch"}),
		map[string]string{"ATRIUM_TASK_ID": "t1"})
	last := map[string]string{}
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		last[k] = v
	}
	if last["A"] != "harness" || last["B"] != "launch" || last["ATRIUM_TASK_ID"] != "t1" {
		t.Fatalf("env is %v", env)
	}
	if harness["B"] != "harness" {
		t.Fatal("overEnv wrote to the harness row's map")
	}
}

// A real launch: the runner starts with the effort and the extras, the card
// keeps them, and the launched event names the env keys and not the values.
// A shell stands in for the runner, with an effort template it will not choke
// on, because what is under test is the launch path and not any runner.
func TestALaunchCarriesItsOptionsOntoTheCard(t *testing.T) {
	d := testDaemon(t)
	cmd, effort := "sh", []string{"-c", "sleep 60", "{effort}"}
	if runtime.GOOS == "windows" {
		cmd, effort = "cmd.exe", []string{"/k", "rem", "{effort}"}
	}
	if _, err := d.st.SaveHarness(store.Harness{
		ID: "opttest", Label: "options test", Enabled: true,
		Cmd: cmd, LaunchMode: store.LaunchPTY, EffortArgs: effort,
	}); err != nil {
		t.Fatal(err)
	}
	task, err := d.Launch(LaunchRequest{
		Harness: "opttest", Cwd: t.TempDir(), Effort: "low",
		Args: []string{"extra"}, Env: map[string]string{"OPT_TOKEN": "s3cret"},
	})
	if err != nil {
		t.Skipf("could not spawn a test runner on this machine: %v", err)
	}
	t.Cleanup(func() { _ = d.StopRunner(task.ID) })
	if task.Effort != "low" || strings.Join(task.LaunchArgs, " ") != "extra" ||
		strings.Join(task.LaunchEnvKeys, ",") != "OPT_TOKEN" {
		t.Fatalf("the card says effort %q args %q env %q", task.Effort, task.LaunchArgs, task.LaunchEnvKeys)
	}
	events, err := d.st.Events(task.ID, 50)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range events {
		if e.Kind != store.EventLaunched {
			continue
		}
		p := string(e.Payload)
		if strings.Contains(p, "s3cret") {
			t.Fatalf("an env value reached the event log: %s", p)
		}
		if !strings.Contains(p, "OPT_TOKEN") || !strings.Contains(p, "low extra") {
			t.Fatalf("the launched event does not say what was passed: %s", p)
		}
		return
	}
	t.Fatal("no launched event")
}

// A reopen replays the effort and the extras from the card.
func TestAReopenedCardComesBackWithItsLaunchOptions(t *testing.T) {
	d := reopenDaemon(t)
	task, _, err := d.st.Register(store.Observed{WireName: "opts", Worktree: t.TempDir(), Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetLaunchOptions(task.ID, "low", []string{"--x"}, map[string]string{"K": "v"}); err != nil {
		t.Fatal(err)
	}
	d.saveReopen([]*runner{{taskID: task.ID, buf: newRing(64, 80)}})
	wanted := d.reopenWanted()
	if len(wanted) != 1 || wanted[0].Effort != "low" || wanted[0].LaunchEnv["K"] != "v" ||
		strings.Join(wanted[0].LaunchArgs, " ") != "--x" {
		t.Fatalf("the card reopen rebuilds from is %+v", wanted)
	}
}
