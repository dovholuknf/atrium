package claudeconf

import (
	"os"
	"strings"
	"testing"
)

// How codex reads a hook command, and the ways atrium used to get it wrong.
//
// Measured against codex-cli 0.153.2 with a probe hook: the same command with
// two quotes around the program printed `hook: SessionStart Failed`, and
// without them printed `hook: SessionStart Completed`. Quotes on the ARGUMENTS
// were honored in both. See `docs/other-runners.md`.

// The whole bug in one line: codex runs the first word, so the first word has
// to be a path and not a quoted path.
func TestCodexCommandDoesNotQuoteTheProgram(t *testing.T) {
	got := HookCommandForTarget(Codex, "C:/tools/atrium.exe", "session-start")
	if strings.HasPrefix(got, `"`) {
		t.Fatalf("codex was given a quoted program, which it cannot run: %s", got)
	}
	if !strings.HasPrefix(got, "C:/tools/atrium.exe ") {
		t.Fatalf("codex's command does not start with the binary: %s", got)
	}
}

// And claude still gets its quotes, because a shell reads that one and would
// otherwise split the path in half.
func TestClaudeCommandStillQuotesAPathWithASpace(t *testing.T) {
	got := HookCommandFor("C:/Program Files/atrium/atrium.exe", "session-start")
	if !strings.HasPrefix(got, `"C:/Program Files/atrium/atrium.exe"`) {
		t.Fatalf("claude's command lost its quotes, so the shell will split it: %s", got)
	}
}

// A path codex cannot be given at all. There is no third spelling: unquoted it
// runs `C:/Program`, quoted it looks for a program whose name starts with a
// quote. Refusing before the file is touched is the difference between one
// message and a hook that fails silently once per event forever.
func TestCodexInstallRefusesAPathWithASpace(t *testing.T) {
	path := withCodexHome(t, "")

	_, res, err := InstallOnlyTarget(Codex, "C:/Program Files/atrium/atrium.exe", nil)
	if err == nil {
		t.Fatal("codex was pointed at a path with a space in it and nothing complained")
	}
	if res.Changed {
		t.Fatal("a refused install reported that it changed something")
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatal("a refused install wrote hooks.json anyway")
	}
	if !strings.Contains(err.Error(), "space") {
		t.Fatalf("the refusal does not say what is wrong: %v", err)
	}
}

// The board has to be able to say it before the button is pressed, so the same
// answer rides on the report rather than only on the error.
func TestCodexReportSaysWhyItCannotBeWired(t *testing.T) {
	withCodexHome(t, "")

	rep, err := InspectTarget(Codex, "C:/Program Files/atrium/atrium.exe")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Refused == "" {
		t.Fatal("the report offers a wiring that would be refused, and says nothing")
	}

	// Claude is unaffected: a shell reads that file and quotes work there.
	clean, err := Inspect("C:/Program Files/atrium/atrium.exe")
	if err != nil {
		t.Fatal(err)
	}
	if clean.Refused != "" {
		t.Fatalf("claude refused a path it can quote: %s", clean.Refused)
	}
}

// A codex session that did not say it was codex came up on the board wearing
// claude's colour and offering claude's resume for an id claude never issued.
func TestCodexCommandNamesTheRunner(t *testing.T) {
	got := HookCommandForTarget(Codex, "C:/tools/atrium.exe", "session-start")
	if !strings.Contains(got, "--runner codex") {
		t.Fatalf("a codex hook does not say it is codex: %s", got)
	}
	// Claude does not, and must not start: every settings.json already
	// installed says nothing, and the subcommand's own default is claude.
	if c := HookCommandFor("C:/tools/atrium.exe", "session-start"); strings.Contains(c, "--runner") {
		t.Fatalf("claude's command grew a runner flag, so every installed hook now reads stale: %s", c)
	}
}

// Installing for one runner has to write THAT runner's line.
//
// `upsert` took a target and then asked the claude helpers for the command and
// for whether an entry matched. The two agreed for as long as the sets held
// the same events with the same subcommands behind them, and the accident ends
// the moment either one says something the other does not.
func TestInstallWritesTheTargetsOwnCommand(t *testing.T) {
	withCodexHome(t, "")

	rep, _, err := InstallOnlyTarget(Codex, "C:/tools/atrium.exe", []string{"session-start"})
	if err != nil {
		t.Fatal(err)
	}
	var found string
	for _, h := range rep.Hooks {
		if h.Event == "session-start" {
			found = h.Found
		}
	}
	if !strings.Contains(found, "--runner codex") {
		t.Fatalf("claude's command was written into codex's file: %q", found)
	}
}

// What was written has to be recognised as installed on the next read, or the
// board offers to wire something it just wired and rewrites the file every
// time it is asked.
func TestCodexRecognisesWhatItJustWrote(t *testing.T) {
	withCodexHome(t, "")

	if _, _, err := InstallOnlyTarget(Codex, "C:/tools/atrium.exe", nil); err != nil {
		t.Fatal(err)
	}
	rep, err := InspectTarget(Codex, "C:/tools/atrium.exe")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Missing != 0 {
		t.Fatalf("%d codex hooks read as missing straight after being written", rep.Missing)
	}

	// And a second install changes nothing, which is what tells a no-op from
	// a write that keeps a backup of a file nobody edited.
	_, res, err := InstallOnlyTarget(Codex, "C:/tools/atrium.exe", nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("installing twice rewrote the file the second time")
	}
}

// Codex does not fire these two, so offering them would be a switch that
// reports success and changes nothing.
func TestCodexOffersOnlyHooksCodexHas(t *testing.T) {
	for _, w := range Codex.Wanted {
		switch w.Hook {
		case "Notification", "PostToolUseFailure":
			t.Fatalf("codex offers %s, which codex does not fire", w.Hook)
		}
	}
	var compact bool
	for _, w := range Codex.Wanted {
		if w.Hook == "PreCompact" {
			compact = true
		}
	}
	if !compact {
		t.Fatal("codex fires PreCompact and atrium does not ask for it")
	}
}

// An entry written before atrium had a second runner is the right binary and
// the right subcommand, and it reports claude whoever ran it. The path check
// cannot see that, so without a second check it reads as wired, is not counted
// as missing, and the board never offers the button that would fix it.
func TestACodexEntryThatDoesNotSayCodexReadsAsStale(t *testing.T) {
	withCodexHome(t, `{
      "hooks": {
        "SessionStart": [
          {"matcher": "", "hooks": [
            {"type": "command", "command": "C:/tools/atrium.exe session --event start"}
          ]}
        ]
      }
    }`)

	rep, err := InspectTarget(Codex, "C:/tools/atrium.exe")
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range rep.Hooks {
		if h.Event == "session-start" && !h.Stale {
			t.Fatal("an entry reporting the wrong runner reads as wired, so nothing offers to fix it")
		}
	}
	if rep.Missing == 0 {
		t.Fatal("a codex file full of claude's commands reports nothing missing")
	}

	// And installing corrects it in place rather than adding a second command
	// beside it, which would report the event twice.
	if _, _, err := InstallOnlyTarget(Codex, "C:/tools/atrium.exe", []string{"session-start"}); err != nil {
		t.Fatal(err)
	}
	after, err := InspectTarget(Codex, "C:/tools/atrium.exe")
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range after.Hooks {
		if h.Event == "session-start" && h.Stale {
			t.Fatalf("still stale after being installed: %q", h.Found)
		}
	}
}

// Claude must not move. Every settings.json already installed says nothing
// about a runner, and marking those stale would tell everybody that six
// working hooks need rewiring.
func TestClaudeEntriesAreNotStaleForSayingNoRunner(t *testing.T) {
	withHome(t, `{
      "hooks": {
        "SessionStart": [
          {"matcher": "", "hooks": [
            {"type": "command", "command": "C:/tools/atrium.exe session --event start"}
          ]}
        ]
      }
    }`)

	rep, err := Inspect("C:/tools/atrium.exe")
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range rep.Hooks {
		if h.Event == "session-start" && h.Stale {
			t.Fatal("an ordinary claude hook was marked as pointing elsewhere")
		}
	}
}
