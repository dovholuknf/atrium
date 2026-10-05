package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

// Every claude card starts with --autocompact at its atrium limit plus 10 percent, so the
// runner's compaction is the backstop behind atrium's new context.

func claudeWithAutocompact() *store.Harness {
	h := claudeWithModel()
	h.AutocompactArgs = []string{"--autocompact", "{autocompact}"}
	return h
}

func setK(t *testing.T, d *Daemon, key string, k string) {
	t.Helper()
	if err := d.st.SetSetting(key, k); err != nil {
		t.Fatal(err)
	}
}

func setLimits(t *testing.T, d *Daemon, v string) {
	t.Helper()
	setK(t, d, store.SettingContextLimits, v)
}

// Untouched, the default claude limit gives a window.
func TestAutocompactKDefaultsToTheClaudeLimit(t *testing.T) {
	d := testDaemon(t)
	if got := d.autocompactK(&store.Task{Runner: "claude"}); got != 220 {
		t.Fatalf("default window = %dk, want 220k (200k + 10%%)", got)
	}
}

func TestAutocompactKFollowsTheLimitAndClamps(t *testing.T) {
	d := testDaemon(t)
	setLimits(t, d, `{"claude":400}`)
	if got := d.autocompactK(&store.Task{Runner: "claude"}); got != 440 {
		t.Fatalf("400k limit gave %dk, want 440k", got)
	}
	setLimits(t, d, `{"claude":50}`)
	if got := d.autocompactK(&store.Task{Runner: "claude"}); got != 100 {
		t.Fatalf("a 50k limit gave %dk, want the 100k floor", got)
	}
	setLimits(t, d, `{"claude":1500}`)
	if got := d.autocompactK(&store.Task{Runner: "claude"}); got != 1000 {
		t.Fatalf("a 1500k limit gave %dk, want the 1000k ceiling", got)
	}
}

// A card's own limit moves its window.
func TestAutocompactKForACardOverride(t *testing.T) {
	d := testDaemon(t)
	got := d.autocompactK(&store.Task{Runner: "claude", Overrides: map[string]string{"context_limit_k": "300"}})
	if got != 330 {
		t.Fatalf("a 300k card limit gave %dk, want 330k", got)
	}
}

// The limit is one function: what the cycle is held to is the same number.
func TestCardLimitIsWhatTheCycleIsHeldTo(t *testing.T) {
	d := testDaemon(t)
	task := &store.Task{ID: "x", Runner: "claude", Overrides: map[string]string{"context_limit_k": "120"}}
	if d.cardLimit(task) != d.cycleLimit(task) || d.cycleLimit(task) != 120000 {
		t.Fatalf("limit %d, cycle %d", d.cardLimit(task), d.cycleLimit(task))
	}
}

func TestTheRunnerRowSaysHowItTakesTheWindow(t *testing.T) {
	args, logged, err := runnerArgsWith(claudeWithAutocompact(), "", "", launchOptions{Autocompact: "330k"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(args, " ") != "--autocompact 330k" || !strings.Contains(logged, "--autocompact 330k") {
		t.Fatalf("args %v logged %q", args, logged)
	}
}

// Resume replaces the base arguments, and the window must be on a resumed card too, before any prompt.
func TestAResumeAndAPromptCarryTheWindow(t *testing.T) {
	args, _, err := runnerArgsWith(claudeWithAutocompact(), "sess-1", "", launchOptions{Autocompact: "330k"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(args, " ") != "--resume sess-1 --autocompact 330k" {
		t.Fatalf("resume args %v", args)
	}
	args, _, err = runnerArgsWith(claudeWithAutocompact(), "", "go", launchOptions{Autocompact: "330k"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(args, " ") != "--autocompact 330k go" {
		t.Fatalf("prompt args %v", args)
	}
}

// A runner with no way to take it is not refused, it just gets nothing.
func TestARunnerWithoutTheFlagIsLeftAlone(t *testing.T) {
	args, _, err := runnerArgsWith(claudeWithModel(), "", "", launchOptions{Autocompact: "330k"})
	if err != nil || len(args) != 0 {
		t.Fatalf("args %v err %v", args, err)
	}
}

// Arguments that never name the value would start with whatever they spell.
func TestAutocompactArgsMustCarryThePlaceholder(t *testing.T) {
	h := claudeWithModel()
	h.AutocompactArgs = []string{"--autocompact", "auto"}
	if _, _, err := runnerArgsWith(h, "", "", launchOptions{Autocompact: "330k"}); err == nil {
		t.Fatal("a row without {autocompact} was accepted")
	}
}

// The seeded claude row, and the migrated one, carry it.
func TestTheSeededClaudeRowTakesAutocompact(t *testing.T) {
	for _, h := range store.DefaultHarnesses() {
		if h.ID == "claude" {
			if strings.Join(h.AutocompactArgs, " ") != "--autocompact {autocompact}" {
				t.Fatalf("claude row: %v", h.AutocompactArgs)
			}
			return
		}
	}
	t.Fatal("no claude row")
}

// A fake claude, a script that prints its --help. The probe asks it once.
func fakeClaude(t *testing.T, help string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "claude")
	if err := os.WriteFile(p, []byte("#!/bin/sh\ncat <<'EOF'\n"+help+"\nEOF\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestARunnerThatListsTheFlagTakesIt(t *testing.T) {
	d := testDaemon(t)
	h := claudeWithAutocompact()
	h.BinPath = fakeClaude(t, "Options:\n  --autocompact <auto|tokens>  Auto-compact window size")
	if d.autocompactArgsFor(h) == nil {
		t.Fatal("a claude that lists --autocompact was not given the flag")
	}
}

// An older claude would hard-fail on the unknown option, so the flag is left off and the card says why.
func TestAnOlderClaudeIsStartedWithoutTheFlagAndTheDetailsSayWhy(t *testing.T) {
	d := testDaemon(t)
	h := claudeWithAutocompact()
	h.BinPath = fakeClaude(t, "Options:\n  --model <model>  Model")
	if d.autocompactArgsFor(h) != nil {
		t.Fatal("a claude without --autocompact was given it")
	}
	probes := 0
	d.acProbe.help = func(string) (string, error) { probes++; return "Options:\n  --model", nil }
	d.acProbe.seen = map[string]bool{}
	d.autocompactArgsFor(h)
	d.autocompactArgsFor(h)
	if probes != 1 {
		t.Fatalf("probed %d times, want once and cached", probes)
	}
	if _, err := d.st.SaveHarness(store.Harness{ID: "claude", Label: "c", Enabled: true, Cmd: "claude",
		BinPath: h.BinPath, LaunchMode: store.LaunchPTY, AutocompactArgs: h.AutocompactArgs}); err != nil {
		t.Fatal(err)
	}
	got, _ := d.autocompactFor(&store.Task{Runner: "claude"}).(*Autocompact)
	if got == nil || got.Note != NoAutocompactNote || got.WindowK != 0 {
		t.Fatalf("details = %+v", got)
	}
}

func TestAClaudeThatCannotBeAskedIsAssumedToTakeIt(t *testing.T) {
	d := testDaemon(t)
	d.acProbe.help = func(string) (string, error) { return "", os.ErrNotExist }
	if d.autocompactArgsFor(claudeWithAutocompact()) == nil {
		t.Fatal("an unprobeable claude lost the flag")
	}
}

// A window past the model's is clamped to it, and an unnamed model is left to the 100k-1M range.
func TestTheWindowNeverPassesTheModels(t *testing.T) {
	d := testDaemon(t)
	setLimits(t, d, `{"claude":300}`)
	for model, want := range map[string]int{
		"":                      330,
		"claude-opus-5-5":       200,
		"claude-sonnet-5-5[1m]": 330,
		"CLAUDE-OPUS-5-5[1M]":   330,
	} {
		if got := d.autocompactK(&store.Task{Runner: "claude", Model: model}); got != want {
			t.Errorf("model %q: %dk, want %dk", model, got, want)
		}
	}
}
