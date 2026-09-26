package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/claudeconf"
	"github.com/dovholuknf/atrium/internal/daemon"
	"github.com/dovholuknf/atrium/internal/runnerprofile"
	"github.com/dovholuknf/atrium/internal/store"
)

// A fake runner: no agent, no tokens.
//
// For every runner atrium writes hooks for, this installs them into a scratch
// file the way the board's button does, reads the commands back out of that
// file, and fires them in the order a session does, with that runner's
// payloads, through the same exit-code rule the binary uses, against a real
// daemon. After each hook it reads the card the way the board does.
//
// It is here because codex's tool-start, tool-end and prompt hooks all exited 1
// on a `--runner` flag only `atrium session` knew, and nothing noticed: the
// card just never showed a spinner.

// fakeExe is the binary path written into the scratch hooks file. It is never
// executed: the command's first word is dropped and the rest runs in-process.
const fakeExe = "/opt/atrium/bin/atrium"

// step is one hook a session fires, and what the card should say after it.
type step struct {
	hook    string
	payload map[string]any
	// status is the card's column afterwards, activity its badge. "-" for
	// activity means no badge at all.
	status, activity, tool string
}

// runnerShape is how one runner spells its payloads.
type runnerShape struct {
	target claudeconf.Target
	// prompt and result are the two field names that differ between runners.
	prompt, result string
}

var runnerShapes = []runnerShape{
	{target: claudeconf.Claude, prompt: "user_input", result: "tool_result"},
	// Measured against codex-cli 0.153.2: a shell call arrives as `Bash`, the
	// prompt as `prompt`, the result as `tool_response`.
	{target: claudeconf.Codex, prompt: "prompt", result: "tool_response"},
}

// sessionSteps is one whole session: open, one prompt, one tool, the turn
// ending, and the session closing.
func sessionSteps(r runnerShape, cwd, transcript string) []step {
	base := func(hook string, extra map[string]any) map[string]any {
		m := map[string]any{
			"session_id": "fake-" + r.target.ID, "cwd": cwd,
			"transcript_path": transcript, "hook_event_name": hook,
		}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}
	return []step{
		// A session that just opened is waiting for its first prompt.
		{hook: "SessionStart", payload: base("SessionStart", map[string]any{"source": "startup"}),
			status: store.StatusNeedsInput, activity: "-"},
		{hook: "UserPromptSubmit", payload: base("UserPromptSubmit", map[string]any{r.prompt: "list the files"}),
			status: store.StatusRunning, activity: daemon.ActivityThinking},
		{hook: "PreToolUse", payload: base("PreToolUse", map[string]any{
			"tool_name": "Bash", "tool_input": map[string]any{"command": "ls"}, "tool_use_id": "call-1"}),
			status: store.StatusRunning, activity: daemon.ActivityTool, tool: "Bash"},
		{hook: "PostToolUse", payload: base("PostToolUse", map[string]any{
			"tool_name": "Bash", "tool_use_id": "call-1", r.result: "a.txt"}),
			status: store.StatusRunning, activity: daemon.ActivityThinking},
		{hook: "Stop", payload: base("Stop", map[string]any{"stop_hook_active": false}),
			status: store.StatusNeedsInput, activity: daemon.ActivityIdle},
		{hook: "SessionEnd", payload: base("SessionEnd", map[string]any{"reason": "prompt_input_exit"}),
			status: store.StatusDone, activity: "-"},
	}
}

func TestFakeRunnerDrivesTheCardThroughEveryHook(t *testing.T) {
	for _, r := range runnerShapes {
		t.Run(r.target.ID, func(t *testing.T) {
			agentAddr, humanAddr := startTestDaemon(t)
			for _, k := range []string{"ATRIUM_AGENT_NAME", "ATRIUM_TASK_ID", "ATRIUM_RUNNER", "ATRIUM_PERM_GATE"} {
				t.Setenv(k, "")
			}
			t.Setenv("ATRIUM_HUB_URL", "http://"+agentAddr)

			commands := installedCommands(t, r.target)
			cwd := filepath.Join(t.TempDir(), "fake-"+r.target.ID)
			if err := os.MkdirAll(cwd, 0o755); err != nil {
				t.Fatal(err)
			}
			transcript := filepath.Join(cwd, "transcript.jsonl")
			if err := os.WriteFile(transcript, []byte("{}\n"), 0o644); err != nil {
				t.Fatal(err)
			}

			for _, s := range sessionSteps(r, cwd, transcript) {
				cmds := commands[s.hook]
				if len(cmds) == 0 {
					t.Fatalf("atrium wrote no %s hook for %s", s.hook, r.target.ID)
				}
				raw, _ := json.Marshal(s.payload)
				for _, c := range cmds {
					if code := runHookLine(t, c, string(raw)); code != 0 {
						t.Fatalf("%s: %q exited %d", s.hook, c, code)
					}
				}
				assertCard(t, humanAddr, filepath.Base(cwd), r.target.ID, s)
			}
		})
	}
}

// A message delivered at turn end continues the turn, and the Stop that ends
// the continued turn carries `stop_hook_active`. That Stop still reaches the
// room, so the card goes back to needs-input instead of staying running.
func TestFakeRunnerContinuedTurnStillEnds(t *testing.T) {
	for _, r := range runnerShapes {
		t.Run(r.target.ID, func(t *testing.T) {
			agentAddr, humanAddr := startTestDaemon(t)
			for _, k := range []string{"ATRIUM_AGENT_NAME", "ATRIUM_TASK_ID", "ATRIUM_RUNNER", "ATRIUM_PERM_GATE"} {
				t.Setenv(k, "")
			}
			t.Setenv("ATRIUM_HUB_URL", "http://"+agentAddr)

			commands := installedCommands(t, r.target)
			cwd := filepath.Join(t.TempDir(), "cont-"+r.target.ID)
			if err := os.MkdirAll(cwd, 0o755); err != nil {
				t.Fatal(err)
			}
			base := func(hook string, extra map[string]any) map[string]any {
				m := map[string]any{"session_id": "cont-" + r.target.ID, "cwd": cwd, "hook_event_name": hook}
				for k, v := range extra {
					m[k] = v
				}
				return m
			}
			fire := func(s step) {
				t.Helper()
				raw, _ := json.Marshal(s.payload)
				for _, c := range commands[s.hook] {
					if code := runHookLine(t, c, string(raw)); code != 0 {
						t.Fatalf("%s: %q exited %d", s.hook, c, code)
					}
				}
				assertCard(t, humanAddr, filepath.Base(cwd), r.target.ID, s)
			}

			fire(step{hook: "SessionStart", payload: base("SessionStart", map[string]any{"source": "startup"}),
				status: store.StatusNeedsInput, activity: "-"})
			fire(step{hook: "UserPromptSubmit", payload: base("UserPromptSubmit", map[string]any{r.prompt: "go"}),
				status: store.StatusRunning, activity: daemon.ActivityThinking})
			card, _ := findCard(t, humanAddr, filepath.Base(cwd))
			resp, err := http.Post("http://"+humanAddr+"/v1/tasks/"+card.ID+"/message", "application/json",
				strings.NewReader(`{"text":"run the tests too"}`))
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			// The Stop carries the message, so the turn goes on.
			fire(step{hook: "Stop", payload: base("Stop", map[string]any{"stop_hook_active": false}),
				status: store.StatusRunning, activity: daemon.ActivityThinking})
			// The continued turn ends.
			fire(step{hook: "Stop", payload: base("Stop", map[string]any{"stop_hook_active": true}),
				status: store.StatusNeedsInput, activity: daemon.ActivityIdle})
		})
	}
}

// A peer's say with `when: "done"` waits out the turn and is carried by that
// runner's real Stop hook, which continues the turn with it. See
// internal/daemon/saywhen.go.
func TestFakeRunnerDoneSayRidesTheStopHook(t *testing.T) {
	for _, r := range runnerShapes {
		t.Run(r.target.ID, func(t *testing.T) {
			agentAddr, humanAddr := startTestDaemon(t)
			for _, k := range []string{"ATRIUM_AGENT_NAME", "ATRIUM_TASK_ID", "ATRIUM_RUNNER", "ATRIUM_PERM_GATE"} {
				t.Setenv(k, "")
			}
			t.Setenv("ATRIUM_HUB_URL", "http://"+agentAddr)

			commands := installedCommands(t, r.target)
			cwd := filepath.Join(t.TempDir(), "done-"+r.target.ID)
			if err := os.MkdirAll(cwd, 0o755); err != nil {
				t.Fatal(err)
			}
			base := func(hook string, extra map[string]any) map[string]any {
				m := map[string]any{"session_id": "done-" + r.target.ID, "cwd": cwd, "hook_event_name": hook}
				for k, v := range extra {
					m[k] = v
				}
				return m
			}
			fire := func(s step) {
				t.Helper()
				raw, _ := json.Marshal(s.payload)
				for _, c := range commands[s.hook] {
					if code := runHookLine(t, c, string(raw)); code != 0 {
						t.Fatalf("%s: %q exited %d", s.hook, c, code)
					}
				}
				assertCard(t, humanAddr, filepath.Base(cwd), r.target.ID, s)
			}
			pending := func(id string) int {
				t.Helper()
				resp, err := http.Get("http://" + humanAddr + "/v1/tasks/" + id + "/messages")
				if err != nil {
					t.Fatal(err)
				}
				defer resp.Body.Close()
				var out struct {
					Messages []json.RawMessage `json:"messages"`
				}
				if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
					t.Fatal(err)
				}
				return len(out.Messages)
			}

			fire(step{hook: "SessionStart", payload: base("SessionStart", map[string]any{"source": "startup"}),
				status: store.StatusNeedsInput, activity: "-"})
			fire(step{hook: "UserPromptSubmit", payload: base("UserPromptSubmit", map[string]any{r.prompt: "go"}),
				status: store.StatusRunning, activity: daemon.ActivityThinking})
			card, _ := findCard(t, humanAddr, filepath.Base(cwd))
			resp, err := http.Post("http://"+humanAddr+"/v1/tasks/"+card.ID+"/message", "application/json",
				strings.NewReader(`{"text":"rebase when you are done","from":"sg4/doer","when":"done"}`))
			if err != nil {
				t.Fatal(err)
			}
			var said struct {
				When string `json:"when"`
			}
			_ = json.NewDecoder(resp.Body).Decode(&said)
			resp.Body.Close()
			if said.When != "done" {
				t.Fatalf("the answer says when %q, want done", said.When)
			}
			fire(step{hook: "PreToolUse", payload: base("PreToolUse", map[string]any{
				"tool_name": "Bash", "tool_input": map[string]any{"command": "ls"}, "tool_use_id": "call-1"}),
				status: store.StatusRunning, activity: daemon.ActivityTool, tool: "Bash"})
			if n := pending(card.ID); n != 1 {
				t.Fatalf("%d messages waiting mid-turn, want the done one still there", n)
			}
			// The Stop carries it, so the turn goes on.
			fire(step{hook: "Stop", payload: base("Stop", map[string]any{"stop_hook_active": false}),
				status: store.StatusRunning, activity: daemon.ActivityThinking})
			if n := pending(card.ID); n != 0 {
				t.Fatalf("%d messages still waiting after the Stop hook", n)
			}
		})
	}
}

// Every runner profile that names a hooks target has payloads here, and every
// hooks target belongs to a profile. Gemini and ollama have none, so there is
// nothing of theirs to fire, and the day one gets a target this fails until it
// gets a row in runnerShapes.
func TestFakeRunnerCoversEveryHooksTarget(t *testing.T) {
	covered := map[string]bool{}
	for _, r := range runnerShapes {
		covered[r.target.ID] = true
	}
	targets := map[string]bool{}
	for _, target := range claudeconf.Targets {
		targets[target.ID] = true
	}
	profiled := map[string]bool{}
	for _, p := range runnerprofile.Profiles {
		if p.Hooks == "" {
			continue
		}
		profiled[p.Hooks] = true
		if !targets[p.Hooks] {
			t.Errorf("runner %s names hooks target %q, which claudeconf does not have", p.ID, p.Hooks)
		}
		if !covered[p.Hooks] {
			t.Errorf("runner %s has hooks and no fake-runner payloads in runnerShapes", p.ID)
		}
	}
	for id := range targets {
		if !profiled[id] {
			t.Errorf("hooks target %s belongs to no runner profile", id)
		}
	}
}

// Every flag atrium writes into a hooks file is one the subcommand declares.
//
// The fake runner cannot see this on its own: a hook command ignores a flag it
// does not know rather than fail the session, so a dropped flag would pass
// there and quietly lose what it carried.
func TestEveryWrittenHookFlagIsDeclared(t *testing.T) {
	root := newRoot()
	for _, target := range claudeconf.Targets {
		for _, w := range target.Wanted {
			line := claudeconf.HookCommandForTarget(target, fakeExe, w.Event)
			args := hookArgs(t, line)
			cmd, rest, err := root.Find(args)
			if err != nil || cmd == root {
				t.Fatalf("%s %s: %q names no subcommand", target.ID, w.Hook, line)
			}
			for _, a := range rest {
				if !strings.HasPrefix(a, "--") {
					continue
				}
				name := strings.SplitN(strings.TrimPrefix(a, "--"), "=", 2)[0]
				if cmd.Flags().Lookup(name) == nil {
					t.Errorf("%s %s: %q passes --%s, which `atrium %s` does not declare",
						target.ID, w.Hook, line, name, cmd.Name())
				}
			}
		}
	}
}

// A flag no build knows still exits 0 on every hook subcommand, and a person's
// typo on `hook install` still fails.
func TestAHookNeverExitsNonZeroOnABadLine(t *testing.T) {
	t.Setenv("ATRIUM_PERM_GATE", "off")
	for _, line := range []string{
		"hook --event tool-start --from-the-future yes",
		"hook --event",
		"session --event start --unknown",
		"turn --event end --unknown x",
	} {
		defer withStdin(t, `{}`)()
		if code := run(strings.Fields(line)); code != 0 {
			t.Errorf("%q exited %d", line, code)
		}
	}
	was := os.Stderr
	if devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0); err == nil {
		os.Stderr = devnull
		defer func() { os.Stderr = was; devnull.Close() }()
	}
	if code := run([]string{"hook", "install", "--no-such-flag"}); code == 0 {
		t.Error("a typo on `hook install` exited 0, and a person typed that")
	}
}

// installedCommands writes every hook atrium offers for t into a scratch file,
// then reads the commands back out of it, keyed by the runner's hook name.
func installedCommands(t *testing.T, target claudeconf.Target) map[string][]string {
	t.Helper()
	path := filepath.Join(t.TempDir(), target.ID+"-hooks.json")
	target.Path = func() (string, error) { return path, nil }
	var all []string
	for _, w := range target.Wanted {
		all = append(all, w.Event)
	}
	if _, _, err := claudeconf.InstallOnlyTarget(target, fakeExe, all); err != nil {
		t.Fatalf("install %s hooks: %v", target.ID, err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	out := map[string][]string{}
	for hook, entries := range doc.Hooks {
		for _, e := range entries {
			for _, h := range e.Hooks {
				out[hook] = append(out[hook], h.Command)
			}
		}
	}
	return out
}

// hookArgs drops the program from a hook command line, quoted or not, and
// splits the rest. Nothing atrium writes quotes an argument.
func hookArgs(t *testing.T, line string) []string {
	t.Helper()
	rest := line
	if strings.HasPrefix(rest, `"`) {
		end := strings.Index(rest[1:], `"`)
		if end < 0 {
			t.Fatalf("unterminated quote in %q", line)
		}
		rest = rest[end+2:]
	} else if i := strings.IndexByte(rest, ' '); i >= 0 {
		rest = rest[i:]
	} else {
		rest = ""
	}
	if !strings.Contains(line, fakeExe) {
		t.Fatalf("%q does not run the atrium it was written for", line)
	}
	return strings.Fields(rest)
}

// runHookLine runs one hooks-file line through the binary's exit-code rule with
// payload on stdin, and returns the exit code.
func runHookLine(t *testing.T, line, payload string) int {
	t.Helper()
	defer withStdin(t, payload)()
	// The Stop hook writes its answer to stdout. Kept out of the test output.
	devnull, err := os.Open(os.DevNull)
	if err == nil {
		was := os.Stdout
		os.Stdout = devnull
		defer func() { os.Stdout = was; devnull.Close() }()
	}
	return run(hookArgs(t, line))
}

// assertCard polls the board's task list until the card matches the step, or
// fails with what it saw.
func assertCard(t *testing.T, humanAddr, name, runner string, s step) {
	t.Helper()
	var last string
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		card, ok := findCard(t, humanAddr, name)
		if ok {
			what, tool := "-", ""
			if card.Activity != nil {
				what, tool = card.Activity.What, card.Activity.Tool
			}
			last = fmt.Sprintf("status %s, activity %s/%s, runner %s", card.Status, what, tool, card.Runner)
			if card.Status == s.status && what == s.activity && tool == s.tool && card.Runner == runner {
				return
			}
		} else {
			last = "no card"
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("after %s the card shows %s, wanted status %s, activity %s/%s, runner %s",
		s.hook, last, s.status, s.activity, s.tool, runner)
}

type boardCard struct {
	ID       string `json:"id"`
	Name     string `json:"wire_name"`
	Status   string `json:"status"`
	Runner   string `json:"runner"`
	Activity *struct {
		What string `json:"what"`
		Tool string `json:"tool"`
	} `json:"activity"`
}

func findCard(t *testing.T, humanAddr, name string) (boardCard, bool) {
	t.Helper()
	resp, err := http.Get("http://" + humanAddr + "/v1/tasks")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body struct {
		Tasks []boardCard `json:"tasks"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	for _, c := range body.Tasks {
		if c.Name == name {
			return c, true
		}
	}
	return boardCard{}, false
}

// startTestDaemon runs a daemon on scratch ports with a scratch database, and
// never the machine's location file.
func startTestDaemon(t *testing.T) (agentAddr, humanAddr string) {
	t.Helper()
	old := log.Writer()
	log.SetOutput(io.Discard)
	t.Cleanup(func() { log.SetOutput(old) })
	dir := t.TempDir()
	agentAddr, humanAddr = freeAddr(t), freeAddr(t)
	d, err := daemon.New(daemon.Options{
		AgentAddr:    agentAddr,
		HumanAddr:    humanAddr,
		DBPath:       filepath.ToSlash(filepath.Join(dir, "atrium.db")),
		LongPoll:     2 * time.Second,
		LocationFile: filepath.Join(dir, "daemon.json"),
	})
	if err != nil {
		t.Fatalf("daemon did not start: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = d.Run(ctx); close(done) }()
	t.Cleanup(func() {
		cancel()
		<-done
		d.Close()
	})
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if resp, err := http.Get("http://" + humanAddr + "/v1/tasks"); err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return agentAddr, humanAddr
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("test daemon never answered")
	return "", ""
}

func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().String()
}
