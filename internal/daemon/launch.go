package daemon

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// LaunchRequest is what the board sends to start a runner.
type LaunchRequest struct {
	Harness string `json:"harness"`
	Cwd     string `json:"cwd"`
	Title   string `json:"title"`
	Why     string `json:"why"`
	// Resume picks a conversation back up instead of starting a new one. The
	// value is the runner's own session id, recorded by the session hooks.
	Resume string `json:"resume"`
	// TaskID starts a runner onto a card that already exists, instead of making
	// one. Unshelving uses it: the card, its history and its resume id are the
	// reason to pick the work back up, so a second card would defeat the point.
	TaskID string `json:"task_id,omitempty"`
	// Tags are what the operator calls this work. A script that starts a
	// session from a ticket knows things the path does not.
	Tags []string `json:"tags,omitempty"`
	// Prompt is the first instruction the runner gets, handed over as the
	// harness's PromptArgs say to. This is how a card raised from an issue
	// starts with the issue in front of it rather than at an empty cursor.
	Prompt string `json:"prompt,omitempty"`
	// Source, ExternalID and URL record where this work came from. See
	// store.SetOrigin and docs/intake-design.md.
	Source     string `json:"source,omitempty"`
	ExternalID string `json:"external_id,omitempty"`
	URL        string `json:"url,omitempty"`
}

// TerminalTemplate wraps a command so it opens in a real terminal window.
// {cwd}, {title} and {cmd} are substituted. Configurable because which
// terminal you want is a preference, not a property of atrium.
//
// Windows Terminal is tried first and falls back to cmd, so this works on a
// bare machine too.
var TerminalTemplate = defaultTerminal()

func defaultTerminal() []string {
	if runtime.GOOS != "windows" {
		return []string{"x-terminal-emulator", "-e", "{cmd}"}
	}
	if _, err := exec.LookPath("wt.exe"); err == nil {
		return []string{"wt.exe", "-w", "atrium", "new-tab", "--title", "{title}", "-d", "{cwd}", "{cmd}"}
	}
	return []string{"cmd.exe", "/c", "start", "{title}", "/D", "{cwd}", "cmd.exe", "/k", "{cmd}"}
}

// expandTemplate builds the argv for the terminal wrapper.
//
// {cmd} expands to the runner's command and each of its arguments as separate
// argv entries. Substituting a single joined string there instead makes the
// terminal look for one executable literally named "cmd.exe /c echo hi", which
// fails with "the system cannot find the file specified".
func expandTemplate(tmpl []string, cwd, title, cmd string, args []string) []string {
	out := make([]string, 0, len(tmpl)+len(args))
	for _, part := range tmpl {
		if part == "{cmd}" {
			out = append(out, cmd)
			out = append(out, args...)
			continue
		}
		part = strings.ReplaceAll(part, "{cwd}", cwd)
		part = strings.ReplaceAll(part, "{title}", title)
		// A template may still embed {cmd} inside a larger string, as a shell
		// wrapper would. That case does want the joined form.
		part = strings.ReplaceAll(part, "{cmd}", shellJoin(append([]string{cmd}, args...)))
		out = append(out, part)
	}
	return out
}

// runnerArgs builds the argument list for one launch, and separately the
// command line worth writing into the audit log.
//
// The two are not the same thing on purpose. A seed prompt is routinely longer
// than everything else on the line put together, and a card raised from a
// support case carries somebody else's words, which docs/intake-design.md says
// to keep out of atrium's own storage wherever it can be. What the log needs
// to answer is "what was started here", and the prompt is on the card already.
//
// Resuming and prompting are refused together rather than combined. The
// conversation being picked back up already has its instruction, and what a
// runner does with a resume flag and a bare prompt argument at the same time
// is per-runner and mostly undefined. Saying something to a resumed session is
// what the message channel is for.
//
// Every argument stays its own argv element and the prompt is never joined
// into a command string. expandTemplate carries the same rule for the same
// reason: a joined prompt with a quote in it becomes a shell's problem rather
// than the runner's.
func runnerArgs(h *store.Harness, resume, rawPrompt string) (args []string, logged string, err error) {
	args = h.Args
	if resume != "" {
		if len(h.ResumeArgs) == 0 {
			return nil, "", fmt.Errorf("%s has no resume arguments configured", h.Label)
		}
		args = make([]string, 0, len(h.ResumeArgs))
		for _, a := range h.ResumeArgs {
			args = append(args, strings.ReplaceAll(a, "{resume}", resume))
		}
	}
	logged = shellJoin(append([]string{h.Cmd}, args...))

	prompt := strings.TrimSpace(rawPrompt)
	if prompt == "" {
		return args, logged, nil
	}
	if resume != "" {
		return nil, "", errors.New("a resumed conversation already has its instruction. " +
			"start it, then say something to it from the card")
	}
	if len(h.PromptArgs) == 0 {
		return nil, "", fmt.Errorf("%s has no way to take an opening prompt. "+
			"set prompt arguments on the runner, using {prompt} where the text goes", h.Label)
	}
	next := make([]string, 0, len(args)+len(h.PromptArgs))
	next = append(next, args...)
	for _, a := range h.PromptArgs {
		next = append(next, strings.ReplaceAll(a, "{prompt}", prompt))
	}
	return next, logged, nil
}

// Launch starts a runner and returns the card it created.
// resumeIsFree refuses to start a second runner on a conversation that one
// atrium already owns.
//
// Two processes resuming the same session id both append to one transcript.
// That file is append only, so nothing is shredded byte by byte; what happens
// is worse to diagnose. Each process resumed with its own snapshot of the
// history and writes turns that do not account for the other's, so the file
// becomes two conversations braided together, and the next resume replays the
// braid as one confused thread. Nothing reports an error at any point.
//
// Claude Code guards its own backgrounded sessions and does NOT guard this
// case: two foreground resumes of the same id both open, silently. So the
// guard has to be here.
//
// Only against runners atrium owns, which is the honest limit. A session
// started in a terminal atrium never saw is not in `sup`, and pretending
// otherwise would be a check that passes for the wrong reason.
//
// A fresh start is always allowed. Two runners in one directory with no shared
// conversation is a real thing to want: they write to the same files, which is
// the operator's business, and they write to different transcripts.
func (d *Daemon) resumeIsFree(resume string) error {
	resume = strings.TrimSpace(resume)
	if resume == "" {
		return nil
	}
	tasks, err := d.st.List()
	if err != nil {
		// The store is the thing that is broken, and it says so elsewhere. A
		// launch is not the place to also report it.
		return nil
	}
	for _, t := range tasks {
		if t.ResumeID != resume || d.sup.get(t.ID) == nil {
			continue
		}
		return fmt.Errorf(
			"%s is already running this conversation. two runners on one session id "+
				"braid its transcript into a thread neither of them wrote. attach to "+
				"that one, or start a fresh session here instead",
			t.DisplayTitle())
	}
	return nil
}

func (d *Daemon) Launch(req LaunchRequest) (*store.Task, error) {
	h, err := d.st.Harness(req.Harness)
	if err != nil {
		return nil, fmt.Errorf("unknown harness %q", req.Harness)
	}
	if !h.Enabled {
		return nil, fmt.Errorf("%s is not enabled. turn it on in harness settings first", h.Label)
	}
	if err := d.resumeIsFree(req.Resume); err != nil {
		return nil, err
	}

	// The card exists before the process does, and its name is what the runner
	// will report when it first does something. Without this the runner would
	// announce itself as the directory leaf and land on whichever card already
	// claimed that name, which on a repo with two sessions is the wrong one.
	//
	// Resolved first, because a card carries defaults for the rest of this: an
	// offered item knows the directory the work belongs in and the instruction
	// it was raised with, and neither has to be retyped to start it.
	var (
		task      *store.Task
		agentName string
	)
	if req.TaskID != "" {
		// Onto a card that already exists. Its wire name is kept, so the
		// resumed session reports back to the same card rather than splitting
		// the work in two.
		t, err := d.st.Get(req.TaskID)
		if err != nil {
			return nil, fmt.Errorf("no card %s to start onto: %w", req.TaskID, err)
		}
		task = t
		agentName = t.WireName
	}

	cwd := strings.TrimSpace(req.Cwd)
	if cwd == "" && task != nil {
		cwd = task.Worktree
	}
	if cwd == "" {
		cwd = h.Cwd
	}
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	cwd = filepath.FromSlash(cwd)
	if fi, err := os.Stat(cwd); err != nil || !fi.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", cwd)
	}

	// A card's own prompt is the fallback, not an override. Whoever pressed
	// start may have edited it in the dialog, and what they typed wins over
	// what the source guessed.
	//
	// Not on a resume. The prompt stays on the card after the first start, so
	// that a start which failed can be repeated, and unshelving an
	// intake-raised card is a resume onto a conversation that has already been
	// given it. Falling back here would refuse that launch for carrying an
	// instruction the operator never asked to send twice.
	wanted := strings.TrimSpace(req.Prompt)
	if wanted == "" && task != nil && req.Resume == "" {
		wanted = task.Prompt
	}
	args, logged, err := runnerArgs(h, req.Resume, wanted)
	if err != nil {
		return nil, err
	}
	prompt := wanted

	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = filepath.Base(cwd)
	}

	claimed := task != nil && task.WireName == ""
	if agentName == "" {
		agentName = fmt.Sprintf("%s-%d", filepath.Base(cwd), time.Now().UnixNano()%100000)
	}
	switch {
	case task == nil:
		t, _, err := d.st.Register(store.Observed{
			WireName: agentName, Worktree: filepath.ToSlash(cwd), Runner: h.ID,
		})
		if err != nil {
			return nil, err
		}
		task = t
	case claimed:
		// A card offered by a source has never been on the wire, so it has no
		// name for Register to match and no pid for the fallback. Registering
		// the generated name here would find nothing and make a SECOND card,
		// leaving the item in the inbox and its session on a card with no link
		// to what it was for.
		t, err := d.st.Claim(task.ID, store.Observed{
			WireName: agentName, Worktree: filepath.ToSlash(cwd), Runner: h.ID,
		})
		if err != nil {
			return nil, err
		}
		task = t
	}

	// A prepare command runs first, in the directory the runner will use, and
	// the runner inherits whatever environment it left. Failing here fails the
	// launch: the point of preparing is that the runner needs what it sets up,
	// so starting without it produces an agent that cannot find its tools and
	// no explanation of why.
	base := os.Environ()
	if h.Prepare != "" {
		prepared, err := captureEnv(h.Prepare, cwd)
		if err != nil {
			d.launchFailed(task.ID, err.Error())
			return nil, err
		}
		base = base[:0]
		for k, v := range prepared {
			base = append(base, k+"="+v)
		}
		log.Printf("[atrium] prepared the environment for %s with: %s",
			h.ID, firstLine(h.Prepare))
	}

	env := childEnvFrom(base, h.Env, map[string]string{
		"ATRIUM_AGENT_NAME": agentName,
		"ATRIUM_TASK_ID":    task.ID,
	})
	via := ""

	if h.LaunchMode == store.LaunchPTY {
		// Atrium owns the process. That is what makes terminate, the liveness
		// reaper and browser attach work, and it is also why this runner dies
		// with the daemon rather than outliving it the way window mode does.
		// What to run if the resume id turns out to be stale: the same thing
		// without it. Supplied only when this launch used one, so a plain
		// start has nothing to fall back to and nothing to retry.
		var fresh *launchSpec
		if req.Resume != "" {
			fresh = &launchSpec{cmd: h.Cmd, args: h.Args, cwd: cwd, env: env}
		}
		pid, err := d.spawnPTYResume(task.ID, h.Cmd, args, cwd, env, req.Resume != "", fresh)
		if err != nil {
			// The card was created before the process, so a failure to start
			// has to move it. Left in `running` it describes a process that
			// never existed.
			d.launchFailed(task.ID, err.Error())
			return nil, err
		}
		via = "pty"
		// Terminate and the liveness reaper both key off the pid. A window mode
		// launch never has one.
		if _, _, err := d.st.Register(store.Observed{
			WireName: agentName, Worktree: filepath.ToSlash(cwd), Runner: h.ID, PID: pid,
		}); err != nil {
			return nil, err
		}
	} else {
		// Resolve the command before handing it to a terminal, for the same
		// reason pty mode does: a tool installed through npm is a `.cmd`, and
		// neither Windows Terminal nor CreateProcess can start one. Without
		// this, `codex` reaches wt.exe as a bare name and comes back as
		// 0x80070002, "the system cannot find the file specified", about a
		// file that is on PATH.
		cmdName, cmdArgs := h.Cmd, args
		if resolved, err := exec.LookPath(cmdName); err == nil {
			cmdName, cmdArgs = viaShellIfScript(resolved, args)
		}
		argv := expandTemplate(TerminalTemplate, cwd, title, cmdName, cmdArgs)
		cmd := exec.Command(argv[0], argv[1:]...)
		cmd.Dir = cwd
		cmd.Env = env
		if err := cmd.Start(); err != nil {
			d.launchFailed(task.ID, err.Error())
			return nil, fmt.Errorf("could not start %s: %w", h.Label, err)
		}
		// The terminal wrapper exits as soon as it has handed off, so reap it
		// rather than leaving a zombie. The runner keeps running, which is also
		// why atrium never learns its pid.
		go func() { _ = cmd.Wait() }()
		via = argv[0]
	}

	created, err := d.st.Get(task.ID)
	if err != nil {
		return nil, err
	}
	if req.Title != "" {
		if err := d.st.SetOverrides(created.ID, map[string]string{"title": req.Title}); err != nil {
			return nil, err
		}
	}
	if req.Why != "" {
		if err := d.st.SetWhy(created.ID, req.Why); err != nil {
			return nil, err
		}
	}
	if len(req.Tags) > 0 {
		if err := d.st.SetTags(created.ID, req.Tags); err != nil {
			return nil, err
		}
	}
	if err := d.st.SetOrigin(created.ID, req.Source, req.ExternalID, req.URL); err != nil {
		return nil, err
	}
	if err := d.st.AppendEvent(created.ID, store.EventLaunched, map[string]any{
		"harness": h.ID, "cmd": logged, "cwd": cwd, "resume": req.Resume,
		"via": via, "mode": h.LaunchMode, "prompted": prompt != "",
		"source": req.Source, "external_id": req.ExternalID,
	}); err != nil {
		return nil, err
	}

	// Starting is not running. A command on PATH still falls over on a bad
	// flag, a missing key or a broken config, and does so within a moment.
	if out, alive := d.settleFor(created.ID, settleWindow); !alive {
		d.launchFailed(created.ID, out)
		msg := fmt.Sprintf("%s exited as soon as it started", h.Label)
		if out != "" {
			msg += ":\n" + out
		}
		return nil, errors.New(msg)
	}

	d.publishTask(created.ID)
	return d.st.Get(created.ID)
}

// launchFailed moves a card whose runner never got going, and records why.
//
// The reason goes on the card, not only in the event log: the board shows the
// card, and a dead card with no explanation sends you to a terminal to find
// out.
func (d *Daemon) launchFailed(taskID, reason string) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "the runner exited immediately and said nothing"
	}
	if err := d.st.AppendEvent(taskID, store.EventExited, map[string]any{
		"by": "launch", "detected": "never started", "output": reason,
	}); err != nil {
		log.Printf("[atrium] record launch failure for %s: %v", taskID, err)
	}
	// Prefixed so it reads as atrium reporting rather than as something typed
	// into the why field.
	if err := d.st.SetWhy(taskID, "failed to start: "+firstLine(reason)); err != nil {
		log.Printf("[atrium] note launch failure for %s: %v", taskID, err)
	}
	if err := d.st.SetStatus(taskID, store.StatusDead); err != nil {
		log.Printf("[atrium] status after launch failure for %s: %v", taskID, err)
	}
	log.Printf("[atrium] %s never started: %s", taskID, firstLine(reason))
	d.publishTask(taskID)
}

// firstLine keeps a card's one line summary to one line.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimSpace(s)
	if len(s) > 200 {
		s = s[:200] + "..."
	}
	return s
}

// inheritedTaint names environment variables that must not reach a launched
// runner.
//
// The daemon is often started from inside a claude session, so its environment
// carries that session's markers. Passing them on makes the new session think
// it is a child of the old one, which among other things silently turns off
// transcript saving. A launched runner is a top level session and has to start
// with a clean slate.
func inheritedTaint(key string) bool {
	upper := strings.ToUpper(key)
	switch {
	case strings.HasPrefix(upper, "CLAUDE_CODE_"):
		return true
	case strings.HasPrefix(upper, "CLAUDECODE"):
		return true
	case upper == "ATRIUM_AGENT_NAME" || upper == "ATRIUM_TASK_ID":
		// Replaced below with this launch's own values.
		return true
	}
	return false
}

// childEnv builds the environment for a launched runner: everything inherited
// except the tainted keys, then the harness's own settings, then atrium's.
func childEnv(harnessEnv map[string]string, atrium map[string]string) []string {
	return childEnvFrom(os.Environ(), harnessEnv, atrium)
}

// childEnvFrom is childEnv over a given base rather than this process's own.
//
// The base is what a prepare command left behind when there is one, which is
// how a shell function that puts a toolchain on PATH reaches the runner.
func childEnvFrom(base []string, harnessEnv map[string]string, atrium map[string]string) []string {
	out := make([]string, 0, len(base)+len(harnessEnv)+len(atrium))
	for _, kv := range base {
		if i := strings.Index(kv, "="); i > 0 && inheritedTaint(kv[:i]) {
			continue
		}
		out = append(out, kv)
	}
	for k, v := range harnessEnv {
		out = append(out, k+"="+v)
	}
	for k, v := range atrium {
		out = append(out, k+"="+v)
	}
	return out
}

// Kill stops the runner behind a card.
//
// This only works when atrium knows the runner's own process id. A window-mode
// launch does not qualify: the terminal wrapper hands the session off and
// exits, so its pid belongs to a process that is already gone. Only pty mode
// owns the process, and only an owned process can be stopped reliably.
func (d *Daemon) Kill(taskID string) error {
	t, err := d.st.Get(taskID)
	if err != nil {
		return err
	}
	if t.PID <= 0 {
		return errors.New("atrium does not know this runner's process. " +
			"a window-mode launch is handed to the terminal and owns itself, so close it there")
	}
	// A process that is already gone is not a failure. The request was "make
	// this stop running", and it is not running, so the card converges to dead
	// and the caller is told it worked. Reporting an error here made asking to
	// terminate an already-dead card produce a dialog and change nothing.
	gone := false
	proc, err := os.FindProcess(t.PID)
	if err != nil {
		gone = true
	} else if err := proc.Kill(); err != nil {
		if !processAlive(t.PID) {
			gone = true
		} else {
			return fmt.Errorf("could not stop process %d: %w", t.PID, err)
		}
	}

	detected := "you"
	if gone {
		detected = "already gone"
	}
	if err := d.st.AppendEvent(taskID, store.EventExited, map[string]any{
		"pid": t.PID, "by": detected,
	}); err != nil {
		return err
	}
	d.act.forget(taskID)
	if err := d.st.SetStatus(taskID, store.StatusDead); err != nil {
		return err
	}
	d.publishTask(taskID)
	return nil
}

// shellJoin quotes what needs quoting so a path with spaces survives being
// handed to a terminal as one string.
func shellJoin(parts []string) string {
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p == "" {
			continue
		}
		if strings.ContainsAny(p, " \t\"") {
			out = append(out, `"`+strings.ReplaceAll(p, `"`, `\"`)+`"`)
			continue
		}
		out = append(out, p)
	}
	return strings.Join(out, " ")
}
