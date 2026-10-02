package daemon

import (
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

// With the setting off, and untouched, the default limit still gives a window.
func TestAutocompactKDefaultsWhenTheSettingIsOff(t *testing.T) {
	d := testDaemon(t)
	if got := d.autocompactK(&store.Task{}); got != 330 {
		t.Fatalf("default window = %dk, want 330k (300k + 10%%)", got)
	}
}

func TestAutocompactKFollowsTheLimitAndClamps(t *testing.T) {
	d := testDaemon(t)
	setK(t, d, store.SettingAutoNewContextK, "400")
	if got := d.autocompactK(&store.Task{}); got != 440 {
		t.Fatalf("400k limit gave %dk, want 440k", got)
	}
	setK(t, d, store.SettingAutoNewContextK, "50")
	// The effective limit is never under context_threshold_k (150k), so 165k is the least the
	// settings can give. The 100k floor is claude's own range and is held anyway.
	if got := d.autocompactK(&store.Task{}); got != 165 {
		t.Fatalf("a 50k limit gave %dk, want 165k (the 150k threshold floor)", got)
	}
	setK(t, d, store.SettingAutoNewContextK, "1500")
	if got := d.autocompactK(&store.Task{}); got != 1000 {
		t.Fatalf("a 1500k limit gave %dk, want the 1000k ceiling", got)
	}
}

// A ceiling card (a director) is held to the lower ceiling, and so compacts at its plus 10 percent.
func TestAutocompactKForACeilingCard(t *testing.T) {
	d := testDaemon(t)
	setK(t, d, store.SettingContextCeilingK, "200")
	got := d.autocompactK(&store.Task{Tags: []string{ContextCeilingTag}})
	if got != 220 {
		t.Fatalf("a 200k ceiling gave %dk, want 220k", got)
	}
}

// The limit is one function: what the cycle is held to is the same number, before the statusline.
func TestCardLimitIsWhatAutoThresholdStartsFrom(t *testing.T) {
	d := testDaemon(t)
	task := &store.Task{ID: "x", Tags: []string{ContextCeilingTag}}
	if d.cardLimit(task) != d.autoThreshold(task) {
		t.Fatalf("limit %d, threshold %d", d.cardLimit(task), d.autoThreshold(task))
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
