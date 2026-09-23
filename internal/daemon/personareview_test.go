package daemon

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/persona"
	"github.com/dovholuknf/atrium/internal/store"
)

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@t",
		"-c", "commit.gpgsign=false", "-c", "init.defaultBranch=main"}, args...)...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func put(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A persona review reaches the launch with the persona as claude's agent and
// the run directory as its working directory. The runner's command does not
// exist, so the launch stops at the spawn, after everything worth checking.
func TestPersonaReviewSetsUpTheRunAndLaunchesAsTheAgent(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	pack := filepath.Join(t.TempDir(), "personas")
	put(t, filepath.Join(pack, "go-sec", "persona.yaml"), "id: go-sec\nname: Go Sec\nrunners: claude\n")
	put(t, filepath.Join(pack, "go-sec", "render", "claude", "go-sec.md"),
		"---\nname: go-sec\ndescription: d\n---\nbody\n")

	wt := t.TempDir()
	gitIn(t, wt, "init")
	put(t, filepath.Join(wt, "a.go"), "package a\n")
	gitIn(t, wt, "add", "-A")
	gitIn(t, wt, "commit", "-m", "one")
	gitIn(t, wt, "checkout", "-b", "feature")
	gitIn(t, wt, "remote", "add", "origin", "git@github.com:acme/widget.git")

	card, _, err := d.st.Register(store.Observed{WireName: "author", Worktree: filepath.ToSlash(wt), Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.PersonaReview(card.ID, "go-sec", "claude"); err == nil ||
		!strings.Contains(err.Error(), persona.PackPathSetting) {
		t.Fatalf("with no pack configured: %v", err)
	}
	if err := d.st.SetSetting(persona.PackPathSetting, filepath.ToSlash(pack)); err != nil {
		t.Fatal(err)
	}
	if _, err := d.PersonaReview(card.ID, "go-sec", "codex"); err == nil {
		t.Fatal("a runner the persona does not render for was launched")
	}
	if _, err := d.PersonaReview(card.ID, "../go-sec", "claude"); err == nil {
		t.Fatal("an id that climbs was launched")
	}

	if _, err := d.st.SaveHarness(store.Harness{
		ID: "claude", Label: "claude code", Enabled: true,
		Cmd: "atrium-no-such-binary-xyz", LaunchMode: store.LaunchPTY,
		PromptArgs: []string{"{prompt}"},
	}); err != nil {
		t.Fatal(err)
	}
	_, err = d.PersonaReview(card.ID, "go-sec", "claude")
	if err == nil {
		t.Fatal("the fake runner started")
	}

	runs, err := os.ReadDir(filepath.Join(d.personaRunsDir(), "go-sec"))
	if err != nil || len(runs) != 1 {
		t.Fatalf("want one run directory: %v %v", runs, err)
	}
	dir := filepath.Join(d.personaRunsDir(), "go-sec", runs[0].Name())
	target, err := os.ReadFile(filepath.Join(dir, "TARGET.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{filepath.ToSlash(wt), "github/acme/widget", "feature", "Finding schema"} {
		if !strings.Contains(string(target), want) {
			t.Errorf("TARGET.md is missing %q", want)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude", "agents", "go-sec.md")); err != nil {
		t.Fatalf("the rendered agent is not in the run directory: %v", err)
	}

	// The card the launch made sits in the run directory, not the pack and not
	// the reviewed worktree.
	all, err := d.st.List()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range all {
		if strings.EqualFold(filepath.Clean(filepath.FromSlash(c.Worktree)), filepath.Clean(dir)) {
			found = true
		}
	}
	if !found {
		t.Fatalf("no card was made in the run directory %s", dir)
	}
}

// RunnerArgs land after the runner's own arguments and before the model and
// the prompt, which is where `--agent` has to be for claude to read it as a
// flag rather than as the instruction.
func TestRunnerArgsGoBeforeTheModelAndThePrompt(t *testing.T) {
	h := &store.Harness{
		ID: "claude", Label: "claude", Cmd: "claude", Args: []string{"--foo"},
		PromptArgs: []string{"{prompt}"}, ModelArgs: []string{"--model", "{model}"},
		ResumeArgs: []string{"--resume", "{resume}"},
	}
	args, logged, err := launchArgs(h, LaunchRequest{RunnerArgs: []string{"--agent", "go-sec"}}, "review it", "opus")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(args, " "); got != "--foo --agent go-sec --model opus review it" {
		t.Fatalf("args %q", got)
	}
	if !strings.Contains(logged, "--agent go-sec") || strings.Contains(logged, "review it") {
		t.Fatalf("logged %q", logged)
	}
	if strings.Join(h.Args, " ") != "--foo" {
		t.Fatalf("the harness row was changed: %v", h.Args)
	}
	// A resume takes none of them: it replaces the base arguments.
	args, _, err = launchArgs(h, LaunchRequest{RunnerArgs: []string{"--agent", "x"}, Resume: "abc"}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(args, " "); got != "--resume abc" {
		t.Fatalf("resume args %q", got)
	}
}
