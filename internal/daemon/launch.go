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
	// SourceKind and SourceURL are the same two under the names a launcher
	// resolving a URL calls them.
	//
	// TWO NAMES FOR ONE THING, on purpose and only here. `source` and `url`
	// are what the store and the intake path have always called them, and
	// renaming those would touch every source ever configured. A caller that
	// turned `github.com/o/r/pull/9` into a worktree is thinking in the other
	// vocabulary, and refusing it over a word is a round trip for nothing.
	// The explicit pair wins when both are sent.
	SourceKind string `json:"source_kind,omitempty"`
	SourceURL  string `json:"source_url,omitempty"`
	// What a launcher knows about the work that the directory cannot say.
	//
	// ATRIUM IS NOT LEARNING GIT. None of this is derived here and none of it
	// is checked: whoever resolved the URL and made the worktree already knows
	// the answers, and a daemon that re-derives them is a second implementation
	// to disagree with the first. Every one of them is optional, and a session
	// that joined on its own leaves them all empty.
	Repo   string `json:"repo,omitempty"`
	Org    string `json:"org,omitempty"`
	Host   string `json:"host,omitempty"`
	Branch string `json:"branch,omitempty"`
	// Window is which pile this card belongs to, and it is the board's
	// grouping key. `active-work`, `pull-requests`, `tangent`, `discourse`, a
	// repo name, or anything else: atrium stores the string and groups by it
	// without knowing what any of them mean.
	Window string `json:"window,omitempty"`
	// Theme is the terminal palette this session comes up in. A launcher that
	// keeps a repo-to-color map sends the answer rather than atrium keeping a
	// second copy of that map. Empty leaves it to the board.
	Theme string `json:"theme,omitempty"`
	// IfRunning is what to do when this directory already has a card.
	//
	// FOR CALLERS THAT ARE NOT A PERSON. A human pressing start in the launch
	// dialog is looking at the board and means it. A script is not: `gwt new`
	// run twice, or run on a worktree that already has a pinned session, would
	// otherwise put a second runner in a directory that has one, and the two
	// write to the same files while neither knows about the other.
	//
	//	""       start anyway, which is what the board does and the default
	//	"skip"   hand back the card that is there and start nothing
	//	"adopt"  start onto that card, unless something is already running on it
	//
	// The same question `startFixture` answers, moved to where every caller
	// can ask it. It lived in that one caller, which is why the endpoint under
	// it never asked.
	IfRunning string `json:"if_running,omitempty"`
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
		// THE ID HAS TO GO SOMEWHERE.
		//
		// Resume arguments with no `{resume}` in them run, and resume the
		// wrong conversation. `codex resume --last` is the one found in the
		// wild: it takes the most recent codex session on the machine, which
		// on a board with several cards is somebody else's, and the card whose
		// id was discarded shows a conversation it has nothing to do with.
		//
		// Refused rather than corrected. Which spelling a runner wants is the
		// operator's to write, and a launcher that guessed would be inventing
		// a command line for a program it knows nothing about.
		var carries bool
		for _, a := range h.ResumeArgs {
			if strings.Contains(a, "{resume}") {
				carries = true
			}
		}
		if !carries {
			return nil, "", fmt.Errorf("%s resumes with %s, which never uses the id this card "+
				"recorded, so it would pick up whichever conversation that runner saw last. "+
				"put {resume} where the id goes", h.Label, shellJoin(h.ResumeArgs))
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

// What to do about a card that is already in this directory.
type runningVerdict int

const (
	// startAnyway is the board's behaviour and the default. Two sessions in
	// one repo is something an operator does deliberately.
	startAnyway runningVerdict = iota
	// handBack returns the card that is there and starts nothing.
	handBack
	// startOnto continues that card rather than making a second one.
	startOnto
)

// handOverTo decides, given what the caller asked for and whether a runner is
// live on the card that is already here.
//
// A LIVE RUNNER OVERRIDES `adopt`. Adopting means continuing the work filed
// here, and a second process on one card is not that: both write to the same
// directory and the card ends up describing whichever spoke last. So `adopt`
// with something running degrades to `skip` rather than to `start anyway`,
// which is the direction that cannot surprise anybody.
func handOverTo(ifRunning string, live bool) runningVerdict {
	switch ifRunning {
	case "skip":
		return handBack
	case "adopt":
		if live {
			return handBack
		}
		return startOnto
	}
	return startAnyway
}

// WHAT A LAUNCH ONTO AN EXISTING CARD DOES TO ITS STATUS.
//
// The thing being eliminated is a card whose status disagrees with whether a
// process exists. A daemon restart kills every supervised runner and files its
// card dead, which is correct. The cards are then started again ONTO, with
// `task_id`, and the pid on the card was updated while the status was not. So
// the board drew a dead card, the sweep archived it off the board, and the
// process behind it went on posting activity and raising permission requests
// against a card atrium was drawing as finished.
//
// Some of those cards recovered because their SessionStart hook fired and
// moved them, and some did not, which makes the behaviour "it depends on
// whether a hook fired". That is not a rule. This is:
//
//   - dead:    atrium's own conclusion that there is no process. A launch is
//     proof to the contrary, so the card comes back.
//   - done:    somebody said the work was finished, and starting a runner onto
//     it is picking it back up. It has to come back: `done` is never
//     swept and `turnResumed` only revives a card from a waiting
//     state, so a live runner left under a done card works forever in
//     the finished column with nothing able to move it.
//   - backlog: an offered item nobody had started. Starting it is what the
//     inbox is for, so it comes back.
//   - shelved: REFUSED. See ontoRefusal.
//
// It lands in `needs-input` with `started` rather than in `running`, which is
// where session.go puts a session that has only just come up, and for the same
// reason: the process exists and has not done anything yet. Both paths landing
// in the same column is the whole point, since the complaint was that they did
// not.
func statusAfterLaunchOnto(status string) (string, bool) {
	switch status {
	case store.StatusDead, store.StatusDone, store.StatusBacklog:
		return store.StatusNeedsInput, true
	}
	// Already in a column that means a process exists. Nothing to correct, and
	// a session that started and got to work during the settle window must not
	// be dragged back to `needs-input` to announce work it has begun.
	return "", false
}

// ontoRefusal is when a runner may NOT be started onto an existing card.
//
// A SHELVED CARD IS A STANDING NO, and the permission chain in daemon.go is
// where that is spent: every request from a shelved card is refused unanswered,
// so a runner started onto one asks, gets a refusal it did not earn, and
// freezes behind a card nobody is looking at. The other way out is worse. A
// launch that quietly moved the card out of shelved would overturn the
// operator putting the work down, which is a decision, not a stale value, and
// it is the one status here that somebody chose by hand.
//
// So neither, and the launch is refused with the way through named. Unshelving
// still works: the board moves the card out of shelved and THEN asks for the
// runner (see api.patchTask), so by the time this is asked the card is no
// longer shelved. This refuses the other callers, which are the adopt path and
// anything posting `task_id` at /v1/launch.
//
// A live runner is refused for the reason `handOverTo` already refuses it: two
// processes on one card write to one directory and the card ends up describing
// whichever spoke last.
func ontoRefusal(t *store.Task, live bool) error {
	if live {
		return fmt.Errorf("%s already has a runner on it. two processes on one card write "+
			"to one directory and the card describes whichever spoke last. attach to that "+
			"one, or terminate it and start again", t.DisplayTitle())
	}
	if t.Status == store.StatusShelved {
		return fmt.Errorf("%s is shelved, and a shelved card is a standing no: every "+
			"permission request from it is refused unanswered, so a runner started onto it "+
			"freezes behind a card nobody is looking at. unshelve it, which starts the same "+
			"conversation again", t.DisplayTitle())
	}
	return nil
}

// runnerIsLive answers whether there is a process behind this card right now.
//
// The supervisor first, because it is not a guess: it holds an entry only
// while the process atrium started is running, and drops it in `awaitExit`.
//
// The pid only for a card that is in a column claiming to run, which is the
// same set the reaper vets. A pid is never an identity: the operating system
// recycles them, so the pid on a card that has been dead for an hour can be
// true about somebody else's process entirely, and believing it there would
// refuse the relaunch this whole change exists to make work.
func (d *Daemon) runnerIsLive(t *store.Task) bool {
	if d.sup.get(t.ID) != nil {
		return true
	}
	switch t.Status {
	case store.StatusRunning, store.StatusNeedsInput, store.StatusNeedsPermission:
		return t.PID > 0 && processAlive(t.PID)
	}
	return false
}

// startedOnto moves a card that now has a process on it, once the process has
// proved it is going to stay.
//
// The card is read again rather than trusted from before the spawn, because
// the runner's own SessionStart hook may have landed during the settle window
// and moved it already. Re-reading makes the two paths agree instead of race.
func (d *Daemon) startedOnto(taskID string) {
	t, err := d.st.Get(taskID)
	if err != nil {
		log.Printf("[atrium] status after starting onto %s: %v", taskID, err)
		return
	}
	want, move := statusAfterLaunchOnto(t.Status)
	if !move {
		return
	}
	if err := d.st.SetStatusBecause(taskID, want, store.WaitingStarted); err != nil {
		log.Printf("[atrium] status after starting onto %s: %v", taskID, err)
		return
	}
	log.Printf("[atrium] %s was %s and now has a runner on it", t.DisplayTitle(), t.Status)
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
		// Before anything is spawned, so a refusal costs nothing and leaves
		// the card exactly as it was.
		if err := ontoRefusal(t, d.runnerIsLive(t)); err != nil {
			return nil, err
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

	// ONE SESSION PER DIRECTORY, when the caller asks for it. See `IfRunning`.
	if req.TaskID == "" && req.IfRunning != "" {
		here, err := d.st.AdoptableTask(filepath.ToSlash(cwd))
		if err != nil {
			return nil, err
		}
		if here != "" {
			t, err := d.st.Get(here)
			if err != nil {
				return nil, err
			}
			// A LIVE RUNNER ENDS IT EITHER WAY. `adopt` means continue the
			// work already filed here, and starting a second process onto one
			// card is not that: both would write to the same directory and the
			// card would describe whichever spoke last.
			switch handOverTo(req.IfRunning, d.sup.get(here) != nil) {
			case handBack:
				log.Printf("[atrium] not starting a second runner in %s, %s is already there",
					cwd, t.DisplayTitle())
				return t, nil
			case startOnto:
				// `AdoptableTask` excludes done and dead and NOT shelved, so
				// this is a real way to reach a shelved card. Adopting one
				// would start a runner every request of which is refused
				// unanswered, so it is refused here instead, by name.
				if err := ontoRefusal(t, false); err != nil {
					return nil, err
				}
				task = t
				agentName = t.WireName
				req.TaskID = t.ID
			}
		}
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

	// Whether this launch is onto a card that already existed, which is the
	// only case with a status to correct. A card created below is created
	// running.
	onto := task != nil
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
		// Which harness this is, for the hooks it will run.
		//
		// A hook knows which file it was registered in and nothing else, and
		// the hooks file is per runner while this row is per harness: two rows
		// can both run codex with different models and both read the same
		// hooks.json. The launcher is the only thing that knows which row it
		// started, so it says so, and the hook prefers this over the runner
		// name baked into its own command line.
		"ATRIUM_RUNNER": h.ID,
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
	// What the launcher knew. See `store.SetPlace`: empty never overwrites, so
	// a caller that sends three of six leaves the other three alone.
	if err := d.st.SetPlace(created.ID, store.Place{
		Repo: req.Repo, Org: req.Org, Host: req.Host,
		Branch: req.Branch, Window: req.Window, Theme: req.Theme,
	}); err != nil {
		return nil, err
	}
	source, url := req.Source, req.URL
	if req.SourceKind != "" {
		source = req.SourceKind
	}
	if req.SourceURL != "" {
		url = req.SourceURL
	}
	if err := d.st.SetOrigin(created.ID, source, req.ExternalID, url); err != nil {
		return nil, err
	}
	if err := d.st.AppendEvent(created.ID, store.EventLaunched, map[string]any{
		"harness": h.ID, "cmd": logged, "cwd": cwd, "resume": req.Resume,
		"via": via, "mode": h.LaunchMode, "prompted": prompt != "",
		"source": source, "external_id": req.ExternalID, "window": req.Window,
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

	// The process is real and staying, so the card stops saying it is not.
	// After the settle, because a runner that fell over in the first two
	// seconds is what `launchFailed` files as dead, and moving the card before
	// that would have it announce a session that never was.
	if onto {
		d.startedOnto(created.ID)
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
	case upper == "ATRIUM_AGENT_NAME" || upper == "ATRIUM_TASK_ID" || upper == "ATRIUM_RUNNER":
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
