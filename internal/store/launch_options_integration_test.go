//go:build integration

package store

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// A card keeps its effort and extras the way it keeps its model, and its JSON
// names the env keys and never carries a value.
func TestACardRemembersItsLaunchOptions(t *testing.T) {
	s := openTestStore(t)
	task, _, err := s.Register(Observed{WireName: "one", Worktree: "/tmp/one"})
	if err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"TOKEN": "s3cret", "A_FLAG": "1"}
	if err := s.SetLaunchOptions(task.ID, " low ", []string{"--verbose"}, env); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Effort != "low" || strings.Join(got.LaunchArgs, " ") != "--verbose" {
		t.Fatalf("the card remembers effort %q args %q", got.Effort, got.LaunchArgs)
	}
	if got.LaunchEnv["TOKEN"] != "s3cret" {
		t.Fatalf("the env value a restart needs is gone: %v", got.LaunchEnv)
	}
	if strings.Join(got.LaunchEnvKeys, ",") != "A_FLAG,TOKEN" {
		t.Fatalf("the env keys are %q", got.LaunchEnvKeys)
	}
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "s3cret") {
		t.Fatalf("an env value reached the card's JSON: %s", raw)
	}

	// Empty clears, as SetModel does.
	if err := s.SetLaunchOptions(task.ID, "", nil, nil); err != nil {
		t.Fatal(err)
	}
	got, _ = s.Get(task.ID)
	if got.Effort != "" || len(got.LaunchArgs) != 0 || len(got.LaunchEnvKeys) != 0 {
		t.Fatalf("clearing left %+v", got)
	}
}

// 0065 backfills the effort mapping onto an existing claude and codex row that
// has none, and leaves an operator's own mapping alone. Run by forgetting the
// migration on a database that already has the columns, which is also the
// "already there" case the brief asks it to tolerate.
func TestLaunchOptionsMigrationBackfillsOnlyEmptyRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveHarness(Harness{ID: "claude", Cmd: "claude", LaunchMode: LaunchPTY}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveHarness(Harness{ID: "codex", Cmd: "codex", LaunchMode: LaunchPTY,
		EffortArgs: []string{"--mine", "{effort}"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`DELETE FROM schema_migration WHERE name = '0065_launch_options'`); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatalf("0065 did not tolerate its columns already being there: %v", err)
	}
	defer s.Close()
	claude, err := s.Harness("claude")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(claude.EffortArgs, " ") != "--effort {effort}" {
		t.Fatalf("claude's effort args are %q", claude.EffortArgs)
	}
	codex, err := s.Harness("codex")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(codex.EffortArgs, " ") != "--mine {effort}" {
		t.Fatalf("the operator's codex mapping was overwritten: %q", codex.EffortArgs)
	}
}

// The command a launch recorded comes back with the card, and a card nothing launched has none.
func TestACardKeepsTheCommandItWasLaunchedWith(t *testing.T) {
	s := openTestStore(t)
	task, _, err := s.Register(Observed{WireName: "one", Worktree: "/tmp/one"})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get(task.ID); got.LaunchCmd != nil {
		t.Fatalf("a joined card has a launch command: %+v", got.LaunchCmd)
	}
	want := LaunchCmd{Exe: "claude", Args: []string{"--resume", "x", "--autocompact", "253k"}, EnvKeys: []string{"ATRIUM_TASK_ID"}}
	if err := s.SetLaunchCommand(task.ID, want); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(task.ID)
	if err != nil || got.LaunchCmd == nil || got.LaunchCmd.Exe != "claude" ||
		strings.Join(got.LaunchCmd.Args, " ") != "--resume x --autocompact 253k" ||
		strings.Join(got.LaunchCmd.EnvKeys, ",") != "ATRIUM_TASK_ID" {
		t.Fatalf("got %+v, %v", got.LaunchCmd, err)
	}
}
