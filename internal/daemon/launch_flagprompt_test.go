package daemon

import (
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

func flagRows() []*store.Harness {
	var rows []*store.Harness
	for _, h := range store.DefaultHarnesses() {
		if h.ID == "claude" || h.ID == "codex" {
			rows = append(rows, &h)
		}
	}
	rows = append(rows, &store.Harness{ID: "mine", Label: "mine", Cmd: "mine",
		ResumeArgs: []string{"-r", "{resume}"}, PromptArgs: []string{"-p", "{prompt}"},
		ModelArgs: []string{"{model}"}, EffortArgs: []string{"-e", "{effort}"}})
	return rows
}

var flagLike = []string{"--dangerously-skip-permissions", `--settings={"hooks":{}}`, "  -x", "\n--yolo", "-"}

func TestAPromptThatReadsAsAnOptionIsRefused(t *testing.T) {
	for _, h := range flagRows() {
		for _, p := range flagLike {
			if args, _, err := runnerArgsWith(h, "", p, launchOptions{}); err == nil {
				t.Errorf("%s with prompt %q: argv %q, want a refusal", h.ID, p, args)
			} else if !strings.Contains(err.Error(), "-") {
				t.Errorf("%s: error %q does not say why", h.ID, err)
			}
			if h.ID == "mine" {
				continue
			}
			if args, _, err := forkArgs(h, "conv-1", p, launchOptions{}); err == nil {
				t.Errorf("%s clone with prompt %q: argv %q, want a refusal", h.ID, p, args)
			}
		}
	}
}

func TestCardTextThatReadsAsAnOptionIsRefused(t *testing.T) {
	for _, h := range flagRows() {
		for _, v := range flagLike {
			if args, _, err := runnerArgsWith(h, v, "", launchOptions{}); err == nil {
				t.Errorf("%s with resume %q: argv %q, want a refusal", h.ID, v, args)
			}
			if args, _, err := runnerArgsWith(h, "", "", launchOptions{Model: v}); err == nil {
				t.Errorf("%s with model %q: argv %q, want a refusal", h.ID, v, args)
			}
			if args, _, err := runnerArgsWith(h, "", "", launchOptions{Effort: v}); err == nil {
				t.Errorf("%s with effort %q: argv %q, want a refusal", h.ID, v, args)
			}
		}
		if h.ForkArgs != nil {
			if args, _, err := forkArgs(h, "--dangerously-skip-permissions", "", launchOptions{}); err == nil {
				t.Errorf("%s clone of a flag-like id: argv %q, want a refusal", h.ID, args)
			}
		}
	}
}

func TestAPromptWithADashInsideStillStarts(t *testing.T) {
	for _, h := range flagRows() {
		for _, p := range []string{"/review --all", "fix it -- now", "read BRIEF.md\n--x", "a-b"} {
			args, _, err := runnerArgsWith(h, "", p, launchOptions{Model: "opus-4"})
			if err != nil {
				t.Fatalf("%s with prompt %q: %v", h.ID, p, err)
			}
			if args[len(args)-1] != strings.TrimSpace(p) {
				t.Fatalf("%s: argv %q, want the prompt last", h.ID, args)
			}
		}
	}
}
