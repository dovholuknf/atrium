package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/base/claudeconf"
	"github.com/dovholuknf/atrium/internal/guard"
	"github.com/spf13/cobra"
)

// The activity hook, as a subcommand rather than a script, so the command
// written into settings.json is this binary's own path. Atrium knows that and
// can install the entries itself; a script in somebody's dotfiles holds a path
// only their machine has.
//
// Everything in `docs/runtime/hooks.md` under "rules every atrium hook follows" is
// enforced here: exit 0 whatever happens, one attempt, one second, and silence
// on success.

// hookTimeout is the whole budget. This runs before every tool call in every
// session, so it is the operator's latency, not atrium's.
const hookTimeout = time.Second

// hookBudget is how long any atrium hook may take to give up on an atrium that does not answer. A session never waits
// more than 2s on atrium, and the half second left is the process starting and the payload arriving.
const hookBudget = 1500 * time.Millisecond

// hookLeft is what is left of the budget for a hook that began at start.
func hookLeft(start time.Time) time.Duration {
	return max(hookBudget-time.Since(start), 100*time.Millisecond)
}

// contextEvents are the claude hook events that take additionalContext, so the model hears atrium is down. Any other
// event rejects the field, so it gets the systemMessage alone.
var contextEvents = map[string]bool{"PreToolUse": true, "PostToolUse": true, "SessionStart": true, "UserPromptSubmit": true}

// unreachable says which hook could not reach atrium, where, and what to do about it.
func unreachable(hook, addr string, err error) string {
	why := "no answer"
	var ue *url.Error
	var ne net.Error
	switch {
	case errors.As(err, &ne) && ne.Timeout():
		why = "no answer in time"
	case errors.As(err, &ue):
		why = ue.Err.Error()
	case err != nil:
		why = err.Error()
	}
	return fmt.Sprintf("atrium's hook `atrium %s` could not reach atrium at %s (%s). The session goes on without "+
		"atrium. Start atrium with `atrium run`, or set ATRIUM_PERM_GATE=off to turn atrium's hooks off. "+
		"If you cannot do either, tell the user.", hook, addr, why)
}

// hookWarning is what a hook prints when atrium did not answer: a systemMessage for the user, and additionalContext
// for the model where the event takes it. Codex is told the systemMessage only.
func hookWarning(hookEvent, runner, msg string) []byte {
	out := map[string]any{"systemMessage": msg}
	if contextEvents[hookEvent] && !strings.EqualFold(strings.TrimSpace(runner), "codex") {
		out["hookSpecificOutput"] = map[string]any{"hookEventName": hookEvent, "additionalContext": msg}
	}
	b, _ := json.Marshal(out)
	return b
}

// activityHookEvents are the claude hook events behind the activity arguments that take additionalContext.
var activityHookEvents = map[string]string{"tool-start": "PreToolUse", "tool-end": "PostToolUse", "prompt": "UserPromptSubmit"}

// hookEvents are the events a hook may report, mapped to what /activity calls
// them. Named here rather than passed through, so a typo in settings.json is
// caught by this command instead of being posted and ignored.
var hookEvents = map[string]string{
	"tool-start": "tool-start",
	"tool-end":   "tool-end",
	// A tool that failed also stopped running, and a badge that only says what
	// is running now cannot tell the difference. Its own argument so that a
	// registered command can be matched back to the hook that wrote it, and
	// the same activity event because the card means the same thing.
	"tool-failed":    "tool-end",
	"prompt":         "prompt",
	"subagent-start": "subagent-start",
	"subagent-end":   "subagent-end",
	"idle":           "idle",
	"waiting":        "waiting",
	// Notification, filtered. Most of what it carries is not a card wanting a
	// human, so this one decides for itself whether to post at all. See
	// wantsAHuman.
	"notification": "waiting",
}

// notifying names the events whose payload decides whether to post at all.
//
// Everything else on the activity path posts unconditionally, which is what
// makes it cheap. This one cannot: Notification fires for twelve kinds of
// thing and nine of them would put a card in front of you for no reason.
const notifying = "notification"

// hookInput is the part of Claude Code's hook payload atrium reads. Everything
// else in it is ignored, and a payload that will not parse is not an error:
// the event still carries which hook fired, which is most of the value.
type hookInput struct {
	ToolName  string `json:"tool_name"`
	CWD       string `json:"cwd"`
	SessionID string `json:"session_id"`
	// AgentID and AgentType identify a subagent. Claude Code sends both on
	// SubagentStart and SubagentStop, and they are what turns "3 subagents"
	// into three named things: the id to pair a stop with its start, the type
	// to say what it is.
	AgentID   string `json:"agent_id"`
	AgentType string `json:"agent_type"`
	// ToolUseID identifies one tool-use ATTEMPT, which is what a dedup key
	// wants and what a hash of the command can never be.
	ToolUseID string `json:"tool_use_id"`
	// NotificationType says which kind of notification fired. Only the few
	// that mean a card wants a human are worth a badge. See wantsAHuman.
	//
	// Two spellings because the reference and the payload have disagreed
	// about this field's name, and reading both costs nothing while guessing
	// wrong costs the whole hook.
	NotificationType string `json:"notification_type"`
	Type             string `json:"type"`
	// Reason says why a session ended. `clear` and `resume` both mean another
	// one is starting immediately, so the card should not die and come back a
	// second later.
	Reason string `json:"reason"`
	// ToolResponse and Error are a finished tool's output, read only for a shell's command not found.
	ToolResponse json.RawMessage `json:"tool_response"`
	Error        string          `json:"error"`
}

// knownTools are the tools whose absence from a card's shell is the room's misconfiguration.
var knownTools = []string{"go", "gofmt", "make", "git", "gh", "node", "npm", "npx", "python", "python3", "pwsh"}

// missingTool is the first known tool a shell tool's output says was not found, or "".
func missingTool(in hookInput) string {
	if in.ToolName != "Bash" && in.ToolName != "PowerShell" {
		return ""
	}
	out := string(in.ToolResponse) + "\n" + in.Error
	for _, t := range knownTools {
		if strings.Contains(out, " "+t+": command not found") || strings.Contains(out, "The term '"+t+"' is not recognized") {
			return t
		}
	}
	return ""
}

// wantsAHuman reports whether a notification means a card is waiting on you.
//
// Claude Code's Notification hook fires for around a dozen kinds of thing and
// most of them are not that. Wiring it unfiltered would put a badge on a card
// for a background message nobody has to answer, and a badge that appears when
// nothing is wanted is a badge you learn to ignore, which costs the ones that
// do matter.
//
// `permission_prompt` IS included, and it used to be excluded. The reasoning
// for excluding it was that atrium's own gate is what put that prompt on
// screen, so reporting it back would be the card telling itself something it
// already knows. That is true of a prompt atrium raised and false of every
// other one, and the difference matters more than the badge does.
//
// A dialog atrium did NOT raise is a dialog atrium will type into. `runner.Say`
// writes the text and then writes Enter, and an Enter landing on a dialog
// answers it with whatever option was highlighted. While atrium's own gate is
// holding a request the runner is blocked inside a hook and draws nothing, so
// there is nothing to hit. Everything else is exposed: a session with the gate
// off, a trust prompt, a plan approval, anything a future release adds.
//
// So the kind is sent and the DAEMON decides. It knows whether it has a pending
// request for that card, which is the one fact that tells its own prompt apart
// from somebody else's, and it is not a fact this process has.
//
// An unrecognized type does NOT count. This is a filter whose whole purpose is
// to be quiet, and a new notification kind arriving in a future release should
// stay silent until somebody decides it is worth a badge, rather than turning
// into noise on upgrade.
func wantsAHuman(in hookInput) bool {
	switch notificationKind(in) {
	case "idle_prompt", "agent_needs_input", "elicitation_dialog", "permission_prompt":
		return true
	}
	return false
}

// notificationKind reads the type off whichever of the two fields carries it.
func notificationKind(in hookInput) string {
	kind := strings.ToLower(strings.TrimSpace(in.NotificationType))
	if kind == "" {
		kind = strings.ToLower(strings.TrimSpace(in.Type))
	}
	return kind
}

func newHook() *cobra.Command {
	var event, name, hubURL, runner, allow string
	c := &cobra.Command{
		Use:   "hook",
		Short: "Report what this session is doing. Run by a harness, not by hand.",
		Long: "Posts one activity event to the daemon and exits. Meant to be registered in the " +
			"runner's own hooks file, which the board can do for you from the runners tab.\n\n" +
			"It never fails a session: whatever goes wrong, it exits 0, and it says so when atrium did not answer.",
		// Hook flag errors must never interrupt a tool call.
		SilenceUsage:  true,
		SilenceErrors: true,
		// Hooks written by another build may carry flags this one does not know.
		FParseErrWhitelist: cobra.FParseErrWhitelist{UnknownFlags: true},
		Annotations:        map[string]string{runnerHook: "true"},
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Permission decisions need their own stdout protocol rather than activity reporting.
			if strings.EqualFold(strings.TrimSpace(event), permissionEvent) {
				if out := permissionHookForRunner(hubURL, readStdin(), runnerPID, runner); out != nil {
					fmt.Fprintln(cmd.OutOrStdout(), string(out))
				}
				return nil
			}
			if strings.EqualFold(strings.TrimSpace(event), noPollEvent) {
				if out := noPollHook(readStdin()); out != nil {
					fmt.Fprintln(cmd.OutOrStdout(), string(out))
				}
				return nil
			}
			if strings.EqualFold(strings.TrimSpace(event), nowEvent) {
				fmt.Fprintln(cmd.OutOrStdout(), nowLine(time.Now()))
				return nil
			}
			if strings.EqualFold(strings.TrimSpace(event), leanAgentsEvent) {
				if out := leanAgentsHook(readStdin(), allow); out != nil {
					fmt.Fprintln(cmd.OutOrStdout(), string(out))
				}
				return nil
			}
			if strings.EqualFold(strings.TrimSpace(event), preToolUseEvent) {
				if out := guardHook(readStdin(), guard.OSEnv{AgentFunc: recordedAgentAddr}); out != nil {
					fmt.Fprintln(cmd.OutOrStdout(), string(out))
				}
				return nil
			}
			if warn := reportActivity(hubURL, event, name); warn != "" {
				arg := strings.ToLower(strings.TrimSpace(event))
				fmt.Fprintln(cmd.OutOrStdout(), string(hookWarning(activityHookEvents[arg], runner, warn)))
			}
			// Explain an interactive invocation because silence looks like a hang, without prompting inside hooks.
			if interactive() {
				addr, source := hubAddressFrom(hubURL)
				fmt.Fprintf(cmd.OutOrStdout(),
					"posted %q to %s/activity\naddress came from %s\n",
					event, addr, source)
				fmt.Fprintln(cmd.OutOrStdout(),
					"whatever came back was ignored. a warning above means atrium was not listening.")
			}
			return nil
		},
	}
	c.Flags().StringVar(&event, "event", "", "tool-start, tool-end, prompt, subagent-start, subagent-end, permission or pre-tool-use")
	c.Flags().StringVar(&allow, "allow", "", "lean-agents: the agent names a lean worker may start, comma separated")
	c.Flags().StringVar(&name, "name", "", "what this session calls itself (default: the directory name)")
	c.Flags().StringVar(&hubURL, "url", "", "atrium agent address (default: $ATRIUM_HUB_URL or localhost:7777)")
	// Activity finds the card by name, while permission registration also needs the harness identity.
	c.Flags().StringVar(&runner, "runner", "", "which harness is reporting (default: $ATRIUM_RUNNER, then claude)")
	c.AddCommand(newHookInstall(), newHookStatus())
	return c
}

// newHookInstall registers hooks in settings.json from a terminal.
//
// The same edit the board's button makes. Correctness needs no daemon: the
// board re-reads settings.json on every poll and would find this anyway. The
// ping afterwards only removes the wait.
func newHookInstall() *cobra.Command {
	var event, runner string
	c := &cobra.Command{
		Use:   "install",
		Short: "Register atrium's hooks for a runner.",
		Long: "Writes the hook entries into your runner configuration, keeping a copy of the old " +
			"file first and leaving everything else in it alone.\n\n" +
			"With no --event it writes the non-optional hooks. With one, just that one, so they can be " +
			"added a few at a time.\n\n" +
			"The board notices on its own: it reads the same file every few seconds.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			// The daemon identity prevents a fresh build from installing hooks against its temporary path.
			exe := claudeconf.HookExe("")
			if exe == "" {
				return fmt.Errorf("cannot work out which atrium binary to write into your " +
					"settings. start the daemon once, or set " + claudeconf.HookExeEnv)
			}

			var only []string
			if strings.TrimSpace(event) != "" {
				only = []string{strings.ToLower(strings.TrimSpace(event))}
			}
			target := claudeconf.TargetFor(runner)
			if target.ID != runner {
				return fmt.Errorf("unknown hook runner %q", runner)
			}
			rep, res, err := claudeconf.InstallOnlyTarget(target, exe, only)
			if err != nil {
				return err
			}
			// The saved file is authoritative, so a failed board refresh must not fail installation.
			if res.Changed {
				pingHooksChanged()
			}

			out := cmd.OutOrStdout()
			if res.Changed {
				fmt.Fprintf(out, "wrote %s\n", rep.Path)
			} else {
				fmt.Fprintf(out, "%s already says this. nothing was written.\n", rep.Path)
			}
			if res.Backup != "" {
				fmt.Fprintf(out, "the old settings are at %s\n", res.Backup)
			}
			for _, h := range rep.Hooks {
				state := "not wired"
				if h.Stale {
					state = "points elsewhere"
				} else if h.Installed {
					state = "wired"
				}
				fmt.Fprintf(out, "  %-18s %s\n", h.Hook, state)
			}
			if rep.Missing > 0 {
				fmt.Fprintf(out, "%d still to go. run this again with --event, or use the board.\n",
					rep.Missing)
			}
			if target.Trust != "" {
				fmt.Fprintln(out, target.Trust)
			}
			fmt.Fprintln(out, "sessions already running keep their old settings until restarted.")
			return nil
		},
	}
	c.Flags().StringVar(&runner, "runner", "claude", "hook target: claude or codex")
	c.Flags().StringVar(&event, "event", "",
		"just this one: tool-start, tool-end, prompt, subagent-start or subagent-end")
	return c
}

// pingHooksChanged nudges a running board to re-read settings.json.
//
// Every failure is ignored and nothing is printed. No daemon running is the
// normal case for somebody setting this up before starting atrium, and it is
// not a problem: the file is already written, and a board that starts later
// reads it at that point.
func pingHooksChanged() {
	client := hookClient(hookTimeout)
	resp, err := client.Post(hubAddress("")+"/hooks-changed", "application/json", nil)
	if err != nil {
		return
	}
	resp.Body.Close()
}

// newHookStatus prints what is registered, for checking without opening the
// board.
func newHookStatus() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Say which of atrium's hooks are registered.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			// The same question install answers, asked the same way, or status
			// would report as wired what install is about to rewrite.
			exe := claudeconf.HookExe("")
			rep, err := claudeconf.Inspect(exe)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "%s%s\n", rep.Path, map[bool]string{true: "", false: " (does not exist)"}[rep.Exists])
			if rep.Unreadable != "" {
				fmt.Fprintf(out, "that file is not valid json, so nothing can be written: %s\n", rep.Unreadable)
				return nil
			}
			for _, h := range rep.Hooks {
				switch {
				case h.Stale:
					fmt.Fprintf(out, "  %-18s points elsewhere: %s\n", h.Hook, h.Found)
				case h.Installed:
					fmt.Fprintf(out, "  %-18s wired\n", h.Hook)
				default:
					fmt.Fprintf(out, "  %-18s not wired\n", h.Hook)
				}
			}
			return nil
		},
	}
}

// interactive reports whether a human typed this at a prompt, rather than
// Claude Code running it with a payload on a pipe.
//
// A character device is a terminal. Claude Code always pipes, so this is false
// in every hook context.
func interactive() bool {
	st, err := os.Stdin.Stat()
	return err != nil || st.Mode()&os.ModeCharDevice != 0
}

// readPayload reads Claude Code's hook payload from stdin, and gives up rather
// than waiting.
//
// Two ways this blocks forever. Run by hand, stdin is the terminal, so a read
// waits for someone to type EOF. Run with a pipe nobody writes to, the read
// waits on the writer. Either one would hang a
// tool call, which is the one thing a hook must never do, so an interactive
// stdin is not read at all and a pipe gets the same one second the post gets.
//
// A payload that never arrives is not a failure. The event still carries which
// hook fired, and that is most of what the board shows.
func readPayload() hookInput {
	var in hookInput
	if raw := readStdin(); len(raw) > 0 {
		_ = json.Unmarshal(raw, &in)
	}
	return in
}

// readStdin reads a hook payload, or gives up. Empty on anything unusual.
//
// Bounded in size as well as in time: a tool result can be large and none of
// it is wanted by any caller here.
func readStdin() []byte {
	if interactive() {
		return nil
	}
	done := make(chan []byte, 1)
	go func() {
		raw, err := io.ReadAll(io.LimitReader(os.Stdin, 1<<20))
		if err != nil {
			raw = nil
		}
		done <- raw
	}()

	select {
	case raw := <-done:
		return raw
	case <-time.After(hookTimeout):
		// The goroutine is left reading. The process is about to exit and
		// take it with it.
		return nil
	}
}

// reportActivity posts one event. It returns what to tell the session when atrium could not be reached, or "".
func reportActivity(hubURL, event, name string) string {
	start := time.Now()
	// One environment variable turns every atrium hook off without editing
	// settings.json.
	if strings.EqualFold(os.Getenv("ATRIUM_PERM_GATE"), "off") {
		return ""
	}
	arg := strings.ToLower(strings.TrimSpace(event))
	kind, ok := hookEvents[arg]
	if !ok {
		return ""
	}

	in := readPayload()

	// The one event that decides for itself whether to say anything. Silence
	// here is the normal case, not a failure.
	if arg == notifying && !wantsAHuman(in) {
		return ""
	}

	cwd := in.CWD
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	agent, nameSource := hookAgent(name, cwd)
	if agent == "" {
		return ""
	}

	body, err := json.Marshal(map[string]any{
		"agent":   agent,
		"task_id": os.Getenv("ATRIUM_TASK_ID"),
		// "dir" when the name was made up from the directory. Absent otherwise.
		"name_source": nameSource,
		"event":       kind,
		"tool":        in.ToolName,
		// Who the subagent is. Empty on every other event, which is fine: the
		// daemon reads these only for subagent-start and subagent-end.
		"agent_id":   in.AgentID,
		"agent_type": in.AgentType,
		// Which kind of notification this was. Empty on every other event.
		// The daemon needs it to tell a dialog it raised from one the runner
		// put up on its own. See wantsAHuman.
		"notification": notificationKind(in),
		"missing_tool": missingTool(in),
	})
	if err != nil {
		return ""
	}

	client := hookClient(min(hookTimeout, hookLeft(start)))
	addr := hubAddress(hubURL)
	resp, err := client.Post(addr+"/activity", "application/json", bytes.NewReader(body))
	if err != nil {
		return unreachable("hook --event "+arg, addr, err)
	}
	// Drained and closed so the connection can be reused, then discarded. The
	// status is not checked because there is nothing to do about it.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	resp.Body.Close()
	return ""
}

// hookClient is the one client every hook posts with. The dial gets a second of its own so a connect that hangs
// cannot spend the whole budget, and the overall timeout is the hard total.
func hookClient(total time.Duration) *http.Client {
	return &http.Client{
		Timeout: total,
		Transport: &http.Transport{
			DialContext:         (&net.Dialer{Timeout: time.Second}).DialContext,
			TLSHandshakeTimeout: time.Second,
			DisableKeepAlives:   true,
		},
	}
}
