package daemon

import (
	"errors"
	"fmt"
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
	// value is the runner's own session id, which for claude comes off the
	// card that adopted it from the gwt ledger.
	Resume string `json:"resume"`
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

// Launch starts a runner and returns the card it created.
func (d *Daemon) Launch(req LaunchRequest) (*store.Task, error) {
	h, err := d.st.Harness(req.Harness)
	if err != nil {
		return nil, fmt.Errorf("unknown harness %q", req.Harness)
	}
	if !h.Enabled {
		return nil, fmt.Errorf("%s is not enabled. turn it on in harness settings first", h.Label)
	}

	cwd := strings.TrimSpace(req.Cwd)
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

	args := h.Args
	if req.Resume != "" {
		if len(h.ResumeArgs) == 0 {
			return nil, fmt.Errorf("%s has no resume arguments configured", h.Label)
		}
		args = make([]string, 0, len(h.ResumeArgs))
		for _, a := range h.ResumeArgs {
			args = append(args, strings.ReplaceAll(a, "{resume}", req.Resume))
		}
	}

	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = filepath.Base(cwd)
	}

	inner := shellJoin(append([]string{h.Cmd}, args...))

	// The card exists before the process does, and its name is what the runner
	// will report when it first does something. Without this the runner would
	// announce itself as the directory leaf and land on whichever card already
	// claimed that name, which on a repo with two sessions is the wrong one.
	agentName := fmt.Sprintf("%s-%d", filepath.Base(cwd), time.Now().UnixNano()%100000)
	task, _, err := d.st.Register(store.Observed{
		WireName: agentName, Worktree: filepath.ToSlash(cwd), Runner: h.ID,
	})
	if err != nil {
		return nil, err
	}

	env := childEnv(h.Env, map[string]string{
		"ATRIUM_AGENT_NAME": agentName,
		"ATRIUM_TASK_ID":    task.ID,
	})
	via := ""

	if h.LaunchMode == store.LaunchPTY {
		// Atrium owns the process. That is what makes terminate, the liveness
		// reaper and browser attach work, and it is also why this runner dies
		// with the daemon rather than outliving it the way window mode does.
		pid, err := d.spawnPTY(task.ID, h.Cmd, args, cwd, env)
		if err != nil {
			return nil, err
		}
		via = "pty"
		// Recording the pid is the point: terminate and the liveness reaper
		// both key off it, and a window mode launch never has one.
		if _, _, err := d.st.Register(store.Observed{
			WireName: agentName, Worktree: filepath.ToSlash(cwd), Runner: h.ID, PID: pid,
		}); err != nil {
			return nil, err
		}
	} else {
		argv := expandTemplate(TerminalTemplate, cwd, title, h.Cmd, args)
		cmd := exec.Command(argv[0], argv[1:]...)
		cmd.Dir = cwd
		cmd.Env = env
		if err := cmd.Start(); err != nil {
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
	if err := d.st.AppendEvent(created.ID, store.EventLaunched, map[string]any{
		"harness": h.ID, "cmd": inner, "cwd": cwd, "resume": req.Resume,
		"via": via, "mode": h.LaunchMode,
	}); err != nil {
		return nil, err
	}
	d.publishTask(created.ID)
	return d.st.Get(created.ID)
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
	out := make([]string, 0, len(os.Environ())+len(harnessEnv)+len(atrium))
	for _, kv := range os.Environ() {
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
// exits, so its pid belongs to a process that is already gone. Owning the
// process, which is what pty mode is for, is what makes killing reliable.
func (d *Daemon) Kill(taskID string) error {
	t, err := d.st.Get(taskID)
	if err != nil {
		return err
	}
	if t.PID <= 0 {
		return errors.New("atrium does not know this runner's process. " +
			"a window-mode launch is handed to the terminal and owns itself, so close it there")
	}
	proc, err := os.FindProcess(t.PID)
	if err != nil {
		return fmt.Errorf("process %d is gone", t.PID)
	}
	if err := proc.Kill(); err != nil {
		return fmt.Errorf("could not stop process %d: %w", t.PID, err)
	}
	if err := d.st.AppendEvent(taskID, store.EventExited, map[string]any{
		"pid": t.PID, "by": "you",
	}); err != nil {
		return err
	}
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
