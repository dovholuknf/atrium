package daemon

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/api"
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
	// Brief is context to hand the new session, written to BRIEF.md in its
	// directory before it starts and read first.
	//
	// A FILE, NOT A LONGER PROMPT, and that is the whole difference. A prompt is
	// said once, is the first thing to fall out of a compaction, and cannot be
	// consulted afterwards. A file in the working directory can be re-read at any
	// point, survives compaction, and is there when a human takes the card over.
	// See writeBriefFile. Written on the room's own machine, which is why this
	// runs here rather than wherever the caller was: the hub has no directory to
	// write to. Ignored on a resume, which continues a conversation and takes no
	// first prompt.
	Brief string `json:"brief,omitempty"`
	// Model names the model this session runs on, handed over as the harness's
	// ModelArgs say to.
	//
	// ONE TIME WITH RESPECT TO THE HARNESS, STICKY WITH RESPECT TO THE CARD.
	// The form's control is unticked every time it opens, so nobody has to
	// remember to turn it back, and nothing is written to the runner's row.
	// The card keeps it, and `reopenSaved` replays it, so a session that
	// started on a model stays on it across a restart.
	//
	// Naming one for a harness with no ModelArgs is REFUSED. See runnerArgs.
	Model string `json:"model,omitempty"`
	// Effort is the thinking effort, handed over as the harness's EffortArgs or
	// EffortEnv say to. Args and Env are extra argv and environment for the
	// runner, used as given. All three are sticky with respect to the card, as
	// Model is, and none is checked against a list. See
	// docs/launch-options-design.md.
	Effort string            `json:"effort,omitempty"`
	Args   []string          `json:"args,omitempty"`
	Env    map[string]string `json:"env,omitempty"`
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
	// Interactive marks a launch somebody pressed, as opposed to one that
	// happened on its own.
	//
	// NOT ON THE WIRE. Set by the board's launch handler and by nothing else,
	// so a fixture coming up at boot, a source's queued launch and a peer
	// asking for one are all automated by default. The one thing it decides is
	// whether the runner version check is allowed to hold the launch up: there
	// is somebody in front of an interactive launch to read the answer, and
	// nobody in front of the others. See runnerupdate.go.
	Interactive bool `json:"-"`
	// Window is which pile this card belongs to, and it is the board's
	// grouping key. `active-work`, `pull-requests`, `tangent`, `discourse`, a
	// repo name, or anything else: atrium stores the string and groups by it
	// without knowing what any of them mean.
	Window string `json:"window,omitempty"`
	// Theme is the terminal palette this session comes up in. A launcher that
	// keeps a repo-to-color map sends the answer rather than atrium keeping a
	// second copy of that map. Empty leaves it to the board.
	Theme string `json:"theme,omitempty"`
	// Throwaway requests a temporary directory, with directory, card, and
	// transcript cleanup at exit. Ignore it when a directory or card was supplied.
	Throwaway bool `json:"throwaway,omitempty"`
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
	// SpawnedBy is the handle of the session asking for this launch, sent by
	// the hub's `atrium_launch` from the caller's own identity header. The
	// board's dialog sends nothing and is recorded as `@human`. See
	// store.SetLineage.
	SpawnedBy string `json:"spawned_by,omitempty"`
	// SpawnedByID is the launcher's card on ANOTHER room, as `room~id`, sent by
	// the hub when it launches here for a session on another room. Taken only
	// in that tagged form, so it can never name a card on this room. See
	// docs/cross-room-say-design.md.
	SpawnedByID string `json:"spawned_by_id,omitempty"`
	// Lean starts a claude session with only what a worker needs, and MCP names
	// the servers from the runner's MCP config it keeps beside atrium-control.
	// Recorded on the card as tags, so a reopen starts it lean again. See lean.go.
	//
	// A pointer, because absent and false differ. Absent leaves it to the card.
	// False starts a lean card with the full setup and takes the lean tags off
	// it, so its next reopen is not lean either.
	Lean *bool    `json:"lean,omitempty"`
	MCP  []string `json:"mcp,omitempty"`
	// ReportTo is who this card's reports go to: a handle, alias or card id on
	// this room, resolved as `atrium_say` resolves one. The card's launcher is
	// set to that card, and the name is kept as given and resolved again at each
	// delivery. An unknown name refuses the launch. See reportto.go.
	//
	// THE BOARD AND THE CLI ONLY. A launch carrying the agent marker is refused
	// for sending it, so no session can point another card's reports at a third
	// party. It sets the launcher and nothing else: no tag, room or permission.
	ReportTo string `json:"report_to,omitempty"`
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
func runnerArgs(h *store.Harness, resume, rawPrompt, rawModel string) (args []string, logged string, err error) {
	return runnerArgsWith(h, resume, rawPrompt, launchOptions{Model: rawModel})
}

// launchOptions is what a launch passes to the runner beyond its prompt: the
// two convenience fields each harness row maps, and extra argv used as given.
// See docs/launch-options-design.md.
type launchOptions struct {
	Model, Effort string
	Args          []string
}

// runnerArgsWith is runnerArgs with the effort and the extra argv as well.
func runnerArgsWith(h *store.Harness, resume, rawPrompt string, o launchOptions) (args []string, logged string, err error) {
	rawModel := o.Model
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
	// THE MODEL GOES ON BEFORE THE PROMPT AND AFTER EVERYTHING ELSE.
	//
	// Before the prompt because a prompt is a bare positional argument for
	// both runners that take one, and a flag after it would be read as part of
	// the instruction rather than as a flag. After the resume arguments
	// because those REPLACE the base arguments: a resumed session keeps the
	// model it was started on, which is the whole point of the card holding
	// it.
	//
	// Effort goes on the same way, after the model. The extra argv goes last
	// before the prompt, so it can override an earlier flag on a runner that
	// takes the last one.
	if args, err = withMapped(h, args, "model", "name", h.ModelArgs, h.ModelEnv, rawModel); err != nil {
		return nil, "", err
	}
	if args, err = withMapped(h, args, "effort", "level", h.EffortArgs, h.EffortEnv, o.Effort); err != nil {
		return nil, "", err
	}
	if len(o.Args) > 0 {
		args = append(append([]string{}, args...), o.Args...)
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

// withMapped appends one convenience field (model or effort) in the shape the
// harness row declares, or refuses when the row has no shape for it. The
// placeholder is the field's own name in braces.
//
// A row that maps the field by env var only adds no argv here: launchOptionEnv sets
// the variable. Either mapping counts as the runner being able to take it.
func withMapped(h *store.Harness, args []string, field, noun string, tmpl []string, envName, raw string) ([]string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return args, nil
	}
	if len(tmpl) == 0 {
		if strings.TrimSpace(envName) != "" {
			return args, nil
		}
		// REFUSED, NOT IGNORED. Starting on the default after being asked
		// for something else is invisible until the output or the bill is
		// wrong, and by then nobody remembers which session was which.
		return nil, fmt.Errorf("%s has no way to be given a%s %s. "+
			"set %s arguments on the runner, using {%s} where the %s goes, or a %s env var",
			h.Label, article(field), field, field, field, noun, field)
	}
	// The same refusal `{resume}` gets, for the same reason: arguments that
	// never mention the value run, and run with the wrong one.
	ph := "{" + field + "}"
	var carries bool
	for _, a := range tmpl {
		if strings.Contains(a, ph) {
			carries = true
		}
	}
	if !carries {
		return nil, fmt.Errorf("%s takes a%s %s as %s, which never uses the %s "+
			"asked for, so it would start on whatever that spells. put %s where "+
			"the %s goes", h.Label, article(field), field, shellJoin(tmpl), noun, ph, noun)
	}
	next := make([]string, 0, len(args)+len(tmpl))
	next = append(next, args...)
	for _, a := range tmpl {
		next = append(next, strings.ReplaceAll(a, ph, value))
	}
	return next, nil
}

func article(word string) string {
	if strings.ContainsRune("aeiou", rune(word[0])) {
		return "n"
	}
	return ""
}

// launchOptionEnv is the environment one launch adds over the harness's own: the
// extra env as given, then the model and effort vars the row maps. See the env
// order in docs/launch-options-design.md.
//
// Three collisions are refused rather than settled by order, because each
// would run the session on a value nobody can see was chosen. An `ATRIUM_` key
// would lose to atrium's own block, and those are how the session is known. A
// key the row maps for a field that was also asked for is two values for one
// variable. And a row that maps model and effort to one variable cannot carry
// both.
func launchOptionEnv(h *store.Harness, extra map[string]string, model, effort string) (map[string]string, error) {
	out := map[string]string{}
	for k, v := range extra {
		k = strings.TrimSpace(k)
		if k == "" || strings.Contains(k, "=") {
			return nil, fmt.Errorf("%q is not an environment variable name", k)
		}
		if strings.HasPrefix(strings.ToUpper(k), "ATRIUM_") {
			return nil, fmt.Errorf("%s is atrium's own, and a launch cannot set it. "+
				"atrium sets the ATRIUM_ variables that say which session this is", k)
		}
		out[k] = v
	}
	model, effort = strings.TrimSpace(model), strings.TrimSpace(effort)
	modelEnv, effortEnv := strings.TrimSpace(h.ModelEnv), strings.TrimSpace(h.EffortEnv)
	if model != "" && effort != "" && modelEnv != "" && strings.EqualFold(modelEnv, effortEnv) {
		return nil, fmt.Errorf("%s takes both model and effort in %s, so it cannot be given "+
			"both. name one of them, or give the runner two variables", h.Label, modelEnv)
	}
	for _, m := range []struct{ name, field, value string }{
		{modelEnv, "model", model}, {effortEnv, "effort", effort},
	} {
		if m.name == "" || m.value == "" {
			continue
		}
		for k := range out {
			if strings.EqualFold(k, m.name) {
				return nil, fmt.Errorf("%s was given in env and is also where %s takes its %s. "+
					"name the %s or set %s, not both", k, h.Label, m.field, m.field, k)
			}
		}
		out[m.name] = m.value
	}
	return out, nil
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
		return &ResumeBusy{
			HolderID:    t.ID,
			HolderTitle: t.DisplayTitle(),
			Worktree:    t.Worktree,
			Resume:      resume,
		}
	}
	return nil
}

// ResumeBusy is the refusal above, as something a client can act on.
//
// It used to be a `fmt.Errorf` whose text named both ways out and offered
// neither: "braid its transcript into a thread neither of them wrote. attach to
// that one, or start a fresh session here instead", under a single `ok` button.
// Three figures of speech in nine words, and the only actionable sentence in
// the dialog was the tail of the hardest one. The operator then had to work out
// which remedy he wanted and go and perform it somewhere else.
//
// Both remedies are things the daemon already knows how to do, and it knows
// WHICH card is holding the conversation because it just found it. So it says
// so in fields rather than in prose, and the board draws two buttons.
//
// The message stays readable on its own, because a CLI caller and an old board
// both only get `Error()`.
type ResumeBusy struct {
	// HolderID is the card already running this conversation. The board
	// attaches to it directly rather than asking the operator to find it.
	HolderID string `json:"holder_id"`
	// HolderTitle is what that card is called. A display title, so two cards
	// can plausibly wear it, which is why the directory is here too.
	HolderTitle string `json:"holder_title"`
	// Worktree tells them apart when the title does not.
	Worktree string `json:"worktree"`
	// Resume is the conversation id. The operator cannot see it anywhere else
	// and it is the thing they would search for.
	Resume string `json:"resume"`
}

// Error says what happened and what it means, and leaves what to press to the
// buttons. No metaphor: a modal that appears when somebody is blocked is a
// different audience from a comment explaining why the guard exists.
func (e *ResumeBusy) Error() string {
	where := e.HolderTitle
	if e.Worktree != "" {
		where = fmt.Sprintf("%s (%s)", e.HolderTitle, e.Worktree)
	}
	return fmt.Sprintf(
		"%s already has this conversation open. two sessions writing to one "+
			"conversation interleave their turns, and the saved history ends up "+
			"matching neither.", where)
}

// ResumeConflict is what the API sends when it recognises this refusal.
//
// A method rather than a type the API imports, because `internal/daemon`
// already imports `internal/api` and the reverse would be a cycle. The API
// tests for the METHOD, so the daemon can grow a second actionable refusal
// without the API learning its name.
func (e *ResumeBusy) ResumeConflict() map[string]any {
	return map[string]any{
		"kind":         "resume-busy",
		"holder_id":    e.HolderID,
		"holder_title": e.HolderTitle,
		"worktree":     e.Worktree,
		"resume":       e.Resume,
	}
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

// Launch starts a runner, holding the card's and the resume's launch lock across
// the whole call so its check-then-spawn region is atomic against another
// caller. See keyedmutex.go: without this two launches onto one card, or two
// resumes of one conversation, both pass the liveness guard before either
// registers and both spawn.
//
// RestartRunner holds the same lock itself and calls launchLocked, so a restart
// and a launch onto the same card serialize rather than braid.
func (d *Daemon) Launch(req LaunchRequest) (*store.Task, error) {
	// The store keys cards by their bare id. The aggregate board addresses a card
	// as `room~id`, and the hub proxy rewrites that in the request PATH on the way
	// here, but a task id carried in the request BODY is off that path, so a
	// resume-onto can still arrive tagged and miss with `sql: no rows`. Strip the
	// routing prefix here, once, so the lock key and the lookup both see the bare
	// id. A card id is a ULID and carries no `~` of its own, so the first `~` is
	// the tag join. This is a safety net under the board's own untagging.
	if i := strings.IndexByte(req.TaskID, '~'); i > 0 {
		req.TaskID = req.TaskID[i+1:]
	}
	unlock := d.launching.lock(launchKeys(req.TaskID, req.Resume)...)
	defer unlock()
	if t := d.repeatLaunch(req.TaskID); t != nil {
		return t, nil
	}
	t, err := d.launchLocked(req)
	if err == nil && req.TaskID != "" {
		d.startedAt.Store(req.TaskID, time.Now())
		// A runner started onto a parked card is the card waking up, whoever
		// pressed what. `unpark` itself calls launchLocked, so it never comes here.
		if _, uerr := d.st.Unpark(req.TaskID, "launch"); uerr != nil {
			log.Printf("[atrium] could not clear the parked mark on %s: %v", req.TaskID, uerr)
		}
	}
	return t, err
}

// repeatWindow is how long after a launch onto a card a second launch onto it
// counts as the same press arriving twice.
const repeatWindow = 30 * time.Second

// repeatLaunch answers the card a launch just started, when the same card is
// asked for again while that runner is still up.
//
// A SECOND PRESS IS NOT A SECOND LAUNCH. The resume dialog showed nothing for
// the settle window, so clint pressed `launch` again. The first press started
// the runner, and the second waited on the card lock above, then met
// `ontoRefusal` ("already has a runner on it") and raised an error over a
// launch that had worked. What the second press asked for is already true, so
// it gets the first one's answer: the card, with its runner.
//
// Only inside the window and only while that runner is live. A launch onto a
// card whose runner has been up for minutes is a different request made on
// purpose, and it keeps its refusal. RestartRunner calls launchLocked
// directly and never comes through here, so a restart is never mistaken for a
// repeat.
func (d *Daemon) repeatLaunch(taskID string) *store.Task {
	if taskID == "" {
		return nil
	}
	at, ok := d.startedAt.Load(taskID)
	if !ok || time.Since(at.(time.Time)) > repeatWindow || d.sup.get(taskID) == nil {
		return nil
	}
	t, err := d.st.Get(taskID)
	if err != nil {
		return nil
	}
	log.Printf("[atrium] %s: a second launch arrived while the first was starting, answered with the same card",
		t.DisplayTitle())
	return t
}

func (d *Daemon) launchLocked(req LaunchRequest) (*store.Task, error) {
	h, err := d.st.Harness(req.Harness)
	if err != nil {
		return nil, fmt.Errorf("unknown harness %q", req.Harness)
	}
	if !h.Enabled {
		return nil, fmt.Errorf("%s is not enabled. turn it on in harness settings first", h.Label)
	}
	// Is there a newer one, asked here because here is the only moment the
	// answer changes anything: updating replaces the binary, so it has to
	// happen while the runner is NOT running, and thirty seconds from now it
	// will be. Never fails a launch, and only holds one up when a person
	// pressed it. See runnerupdate.go.
	d.checkRunnerUpdate(h, req.Interactive)
	if err := d.resumeIsFree(req.Resume); err != nil {
		return nil, err
	}
	// Resolved before anything exists, so an unknown name costs nothing. Its card
	// becomes the launcher as if it had called `atrium_launch` itself.
	reportTo := strings.TrimSpace(req.ReportTo)
	var reportCard *store.Task
	if reportTo != "" {
		if reportCard, err = d.resolveReportTo(req, reportTo); err != nil {
			return nil, err
		}
		req.SpawnedBy, req.SpawnedByID = reportCard.WireName, reportCard.ID
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
		if errors.Is(err, sql.ErrNoRows) {
			// Named, with the room, rather than "sql: no rows". A start that lands
			// here reached the wrong room. See backlog-2 item 63.
			return nil, fmt.Errorf("could not start onto it: %w", api.NotOnRoom(req.TaskID, d.opts.Room))
		}
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
	// Create a temporary directory only for a throwaway without a supplied
	// directory or existing card.
	throwaway := req.Throwaway && cwd == "" && task == nil
	if throwaway {
		tmp, err := makeThrowawayDir()
		if err != nil {
			return nil, err
		}
		cwd = tmp
	}
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
	// The briefing lands in the directory and the runner is told to read it
	// first. On a fresh start only: a resume continues a conversation and takes
	// no first prompt, and rewriting the file under a session that already read
	// it would be a second source of truth it believes. See writeBriefFile.
	briefPath := ""
	if brief := strings.TrimSpace(req.Brief); brief != "" && req.Resume == "" {
		p, err := writeBriefFile(cwd, brief)
		if err != nil {
			return nil, err
		}
		briefPath = filepath.ToSlash(p)
		wanted = briefPrompt(wanted)
	}
	// THE CARD'S MODEL IS THE FALLBACK, exactly as its prompt is, and for a
	// different reason: a relaunch or an unshelve of a card that was started
	// on a model has to come back on that model, or the session changes
	// underneath somebody because they pressed start twice.
	//
	// UNLIKE the prompt, this applies on a resume too. Resuming is the case
	// that matters most: `reopenSaved` resumes every card after a restart, and
	// that is where a model silently reverting would otherwise happen.
	model := strings.TrimSpace(req.Model)
	if model == "" && task != nil {
		model = task.Model
	}
	// The effort and the extras fall back to the card the same way, each on
	// its own, so a relaunch naming only a model keeps the card's effort.
	effort, extraArgs, extraEnv := strings.TrimSpace(req.Effort), req.Args, req.Env
	if task != nil {
		if effort == "" {
			effort = task.Effort
		}
		if len(extraArgs) == 0 {
			extraArgs = task.LaunchArgs
		}
		if len(extraEnv) == 0 {
			extraEnv = task.LaunchEnv
		}
	}
	opts := launchOptions{Model: model, Effort: effort, Args: extraArgs}
	args, logged, err := runnerArgsWith(h, req.Resume, wanted, opts)
	if err != nil {
		return nil, err
	}
	addedEnv, err := launchOptionEnv(h, extraEnv, model, effort)
	if err != nil {
		return nil, err
	}
	// An agent-launched claude session gets the Stop hook for itself, so its
	// turn ends are reported even where the operator never installed it. The
	// marker is on the request for a new launch and on the card for a reopen.
	// A card told who to report to gets the Stop hook too, so its silent stops
	// are seen, and still does NOT get the agent marker.
	agent := hasTag(req.Tags, OriginAgentTag) || agentLaunched(task) || reportTo != "" ||
		(task != nil && d.reportsToLauncher(task))
	lean, leanMCP := leanOptions(req, task)
	if lean && !isClaude(h) {
		return nil, fmt.Errorf("%s cannot start lean. lean is a claude launch option", h.Label)
	}
	// finishArgs adds the Stop hook, or for a lean launch the whole lean set,
	// which carries the Stop hook itself.
	finishArgs := func(a []string) ([]string, error) {
		if !lean {
			return withStopHook(h, a, agent), nil
		}
		stop := ""
		if agent {
			stop = stopHookCommand()
		}
		return leanArgs(a, readUserSettings(), stop, leanMCP, os.ReadFile)
	}
	if args, err = finishArgs(args); err != nil {
		return nil, err
	}
	// retag writes req.Tags even when it came out empty, which is a lean card
	// whose only tag was the lean one.
	retag := false
	if req.Lean != nil {
		base := req.Tags
		if len(base) == 0 && task != nil {
			base = task.Tags
		}
		if *req.Lean {
			req.Tags = mergeTags(base, leanTags(leanMCP))
		} else if kept, cut := withoutLeanTags(base); cut {
			req.Tags, retag = kept, true
		}
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
		agentName = d.launchedName(req.Title, cwd)
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
	case filepath.ToSlash(cwd) != task.Worktree:
		// Launching a card somewhere else is a choice, not a cd, so it is the
		// one time a card that already has a directory is moved. The Register
		// after the spawn only refreshes pid and runner.
		if err := d.st.SetWorktree(task.ID, filepath.ToSlash(cwd)); err != nil {
			return nil, err
		}
		task.Worktree = filepath.ToSlash(cwd)
	}

	// Mark the card before spawning so even an immediate exit cleans up its
	// temporary directory. Use the default cleanup note unless the operator
	// supplied their own reason for the session.
	if throwaway {
		if err := d.st.SetThrowaway(task.ID, true); err != nil {
			return nil, err
		}
		if err := d.st.SetWhy(task.ID, throwawayWhy); err != nil {
			return nil, err
		}
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

	atrium := map[string]string{
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
	}
	// WHICH ROOM THIS SESSION BELONGS TO, so its HTTP control MCP registration
	// resolves ${ATRIUM_ROOM} and the hub scopes control calls to this room.
	// Only set when this daemon is a room: a plain daemon with no hub has no
	// room to name, and an empty value would leave every session's control
	// calls to the aggregate view, which is the honest answer there.
	if room := strings.TrimSpace(d.opts.Room); room != "" {
		atrium["ATRIUM_ROOM"] = room
	}
	// The cache keep-alive's switch for a new Claude card, from the room default,
	// and the 1h cache pin when it is on. See keepalive.go.
	d.keepaliveAtLaunch(task.ID, h, atrium)
	// The usage record flags the first turn of a resumed runner. See usage.go.
	d.usage.launched(task.ID, req.Resume != "")
	if lean {
		leanEnv(atrium)
	}
	// ROUTE A LAUNCHED SESSION'S OWN PERMISSION PROMPTS THROUGH ATRIUM'S GATE.
	//
	// A launched runner is a claude session that never ran `atrium join`, so
	// with the gate unset the permission hook lets it through and its Bash and
	// edit approvals are claude's OWN prompts, in a terminal nobody is sitting in
	// front of. The board-wide switch lives in atrium's gate and only reaches
	// requests that arrive there, so those prompts sit unanswered while "accept
	// everything" is on and the operator wonders why a session he turned loose
	// is still asking.
	//
	// `on` makes the runner's PreToolUse gate post every tool call to
	// /permission, where the same chain every joined session runs decides it: a
	// standing rule, a shelved card, per-session auto and board-wide auto all
	// apply. With auto off it still gates to the operator exactly as a joined
	// session does, so this routes the approvals without weakening them.
	//
	// A DEFAULT, not an override. The harness's own env wins, so an operator can
	// set ATRIUM_PERM_GATE=off on a runner that should never gate, and only the
	// default is supplied here.
	if gate, ok := permGateDefault(h.Env); ok {
		atrium["ATRIUM_PERM_GATE"] = gate
	}
	// No fullscreen renderer dialog to eat the first say. See firstrun.go.
	launchEnv := overEnv(h.Env, addedEnv)
	if v, ok := classicRendererDefault(h, launchEnv); ok {
		atrium[classicRendererEnv] = v
	}
	env := childEnvFrom(base, launchEnv, atrium)
	d.prepareRunnerSetup(h, cwd, env)
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
			// The same model, effort and extras as the resume, which is what
			// the card asked for. Built from `h.Args` alone this came back on
			// the runner's default model.
			base, _, err := runnerArgsWith(h, "", "", opts)
			if err != nil {
				d.launchFailed(task.ID, err.Error())
				return nil, err
			}
			freshArgs, err := finishArgs(base)
			if err != nil {
				d.launchFailed(task.ID, err.Error())
				return nil, err
			}
			fresh = &launchSpec{cmd: h.Exe(), args: freshArgs, cwd: cwd, env: env}
		}
		pid, err := d.spawnPTYResume(task.ID, h.Exe(), args, cwd, env, req.Resume != "", fresh)
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
		cmdName, cmdArgs := h.Exe(), args
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
	if len(req.Tags) > 0 || retag {
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
	// WHO LAUNCHED IT, so a worker's report and a silent stop have somewhere to
	// go. Written once: `SetLineage` leaves a card that already has a parent
	// alone, so a reopen cannot rename it. The parent's card is looked up here
	// on a best-effort basis, and a handle that resolves to nothing still names
	// it. See docs/a2a-reliability-design.md.
	// The name is kept only on a card with no launcher yet, which is the one
	// SetLineage below will write. A reopen names nothing new.
	reportedTo := ""
	if reportCard != nil && created.SpawnedBy == "" && created.SpawnedByID == "" {
		if err := d.st.SetReportTo(created.ID, reportTo); err != nil {
			return nil, err
		}
		reportedTo = reportCard.ID
	}
	if by := strings.TrimSpace(req.SpawnedBy); by != "" {
		parentID := ""
		if by != store.HumanLauncher {
			if p, err := d.st.GetByWireName(d.st.Qualify(by)); err == nil && p.ID != created.ID {
				parentID = p.ID
			}
		}
		// A launcher on another room, named by its tagged card, so a notice
		// held for it reaches that card and not a later one with its handle.
		if _, room, err := SplitAddress(by); err == nil && d.otherRoom(room) != "" {
			if tag := strings.TrimSpace(req.SpawnedByID); strings.Contains(tag, "~") {
				parentID = tag
			}
		}
		if err := d.st.SetLineage(created.ID, by, parentID); err != nil {
			return nil, err
		}
	}
	// AN ALIAS TO MENTION IT BY, from the title's prefix: `sa89` from
	// `sa89: typing gate`, `saorch` from `saorch: merger`. A fresh start on a
	// card with none only, so a reopen keeps whatever the operator chose. One
	// another live card holds already is not a reason to refuse the launch, so
	// the card starts without one and its `alias_note` says who has it. See
	// store/alias.go.
	if req.Resume == "" {
		if _, err := d.st.GiveDefaultAlias(created.ID, req.Title); err != nil {
			log.Printf("[atrium] %s starts with no alias: %v", created.ID, err)
		}
	}
	// A WORK ITEM, for a card another session launched: the brief, the
	// launcher, and a work state only a report, a verdict or the session
	// ending moves. A fresh start only, so a resume or a reopen of an older
	// card does not put it on the ledger after the fact. The board's own
	// dialog gets none: clint did not ask for a queue of verdicts. See
	// internal/store/ledger.go.
	if req.Resume == "" && (hasTag(req.Tags, OriginAgentTag) || reportTo != "") {
		if t, err := d.st.Get(created.ID); err == nil {
			if _, err := d.st.CreateWorkItem(t, store.NewWorkItem{
				Brief: briefHead(req.Brief, req.Prompt), BriefPath: briefPath,
			}); err != nil {
				return nil, err
			}
		}
	}
	// WHICH MODEL THIS CARD IS RUNNING ON, written after the runner is up
	// rather than before, so a launch that was refused leaves nothing behind.
	//
	// `model` and not `req.Model`: the card's own value is the fallback, so a
	// relaunch that named nothing keeps what it was started on. Writing the
	// request instead would clear it, which is the restart bug in miniature.
	if err := d.st.SetModel(created.ID, model); err != nil {
		return nil, err
	}
	if err := d.st.SetLaunchOptions(created.ID, effort, extraArgs, extraEnv); err != nil {
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
		"via": via, "mode": h.LaunchMode, "prompted": prompt != "", "model": model,
		"source": source, "external_id": req.ExternalID, "window": req.Window,
		// `cmd` is the command before the lean flags, which carry the whole
		// settings copy. These two say what was added.
		"lean": lean, "mcp": leanMCP,
		// `cmd` carries the effort and extra args already. The env is keys
		// only, because its values never leave the room's database.
		"effort": effort, "env_keys": sortedKeys(extraEnv),
		// What was named and the card it resolved to at launch. See reportto.go.
		"report_to": reportTo, "report_to_id": reportedTo,
	}); err != nil {
		return nil, err
	}
	// The opening prompt is a prompt. It goes on the command line rather than
	// through a door that records one, so without this a worker's FIRST turn
	// owed its launcher nothing, and one that ended it without a report was
	// never a silent stop (item 62, sa42). Before the settle, so it is on the
	// card before the runner can have ended a turn on it.
	if prompt != "" {
		// `from_peer` is whoever asked for this launch, which is what decides
		// whether the prompt is owed to a launcher. A reopen by the operator
		// asks as nobody. See store.promptOwes.
		ev := map[string]any{"text": prompt, "via": "launch"}
		if by := strings.TrimSpace(req.SpawnedBy); by != "" && by != store.HumanLauncher {
			ev["from_peer"] = by
		}
		if err := d.st.AppendEvent(created.ID, store.EventPrompted, ev); err != nil {
			return nil, err
		}
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

// launchedName is the wire name a launched runner reports itself under.
//
// PREFER THE TITLE, THEN DISAMBIGUATE. The board shows a card's title, and a
// launcher that passed one has already said what to call this work. A name
// slugged from that title reads the same as the card. The old fallback -- the
// directory leaf with a random number stapled on -- did not: it produced
// `unique-names-84523`, matched nothing on the board, and changed every launch,
// so a session had no stable handle a peer could name. With no title the
// directory leaf is the base, which is what a session joining on its own would
// have called itself.
//
// UNIQUE AGAINST WHAT IS LIVE, not against everything ever registered. A wire
// name is the key registration matches on, so two live sessions sharing one
// silently hand the second the first's card, its history and its permission
// rules (see store/tenant.go). A dead card's name is free to take back:
// relaunching into the same worktree should land on the same name and the same
// card, which is the point of a stable, title-derived name.
func (d *Daemon) launchedName(title, cwd string) string {
	return launchedName(title, cwd, d.wireNameTaken())
}

// wireNameTaken reports, for a local name, whether a live session already wears
// it. Compared against LocalName because the argument is the unqualified name a
// launch is about to hand out, while a stored wire name carries this atrium's
// tenant prefix.
func (d *Daemon) wireNameTaken() func(string) bool {
	taken := map[string]bool{}
	if tasks, err := d.st.List(); err == nil {
		for _, t := range tasks {
			if d.runnerIsLive(t) {
				taken[store.LocalName(t.WireName)] = true
			}
		}
	}
	return func(name string) bool { return taken[name] }
}

// launchedName is the testable core of the method above: base from the title
// when there is one, otherwise the directory leaf, then a numeric suffix until
// nothing live holds it.
func launchedName(title, cwd string, taken func(string) bool) string {
	base := nameSlug(title)
	if base == "" {
		base = filepath.Base(cwd)
	}
	name := base
	for n := 2; taken(name); n++ {
		name = fmt.Sprintf("%s-%d", base, n)
	}
	return name
}

// nameSlug reduces a launch title to something usable as a wire name: lower
// case, runs of anything else collapsed to a single dash, capped so a sentence
// of a title does not become a sentence of a name. Empty when the title has no
// usable characters, which is the signal to fall back to the directory leaf.
func nameSlug(title string) string {
	const max = 40
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(strings.TrimSpace(title)) {
		if b.Len() >= max {
			break
		}
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		default:
			if !dash && b.Len() > 0 {
				b.WriteByte('-')
				dash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
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
	case upper == "ATRIUM_AGENT_NAME" || upper == "ATRIUM_TASK_ID" ||
		upper == "ATRIUM_RUNNER" || upper == "ATRIUM_ROOM":
		// Replaced below with this launch's own values. ATRIUM_ROOM is here too
		// so a daemon started from inside a session cannot leak that session's
		// room to the ones it launches: a child gets THIS daemon's room or none.
		return true
	case strings.HasPrefix(upper, "ATRIUM_DEBUG_"):
		// Diagnostics for THIS process. The live scripts turn on
		// ATRIUM_DEBUG_INPUTLAG for the room, and a runner that inherited it
		// logged lag from every atrium binary it ran and failed `go test` in
		// internal/link. The whole prefix, because every switch under it is a
		// debug readout for the process it was set on. A runner that wants one
		// names it in its harness env, which is applied after this filter.
		return true
	}
	return false
}

// permGateDefault is the ATRIUM_PERM_GATE value a launch supplies, and whether
// to supply one at all.
//
// A launched session should route its tool approvals through atrium's gate so
// the one board-wide switch controls them. `on` is that default. It is skipped
// only when the harness already names the variable, so an operator who set
// ATRIUM_PERM_GATE=off on a runner keeps that runner ungated. Matched
// case-insensitively because it is a shell variable and its name is the only
// thing that decides which env entry wins.
func permGateDefault(harnessEnv map[string]string) (string, bool) {
	for k := range harnessEnv {
		if strings.EqualFold(k, "ATRIUM_PERM_GATE") {
			return "", false
		}
	}
	return "on", true
}

// briefFileName is what a briefing is called in the new session's directory.
//
// One fixed name so a second launch into the same directory replaces the
// briefing rather than littering it with dated copies nobody reads. A stale
// brief is worse than a missing one: the session believes it. Not CLAUDE.md,
// deliberately: that file loads into every session in the directory forever,
// including ones nobody meant to brief.
const briefFileName = "BRIEF.md"

// writeBriefFile puts the briefing where the new session will find it, and
// returns the path written.
//
// Overwrites. See briefFileName. The directory is expected to exist already:
// Launch has stat'd cwd by the time this runs, so a missing one is a launch
// failure that happened earlier and not here.
func writeBriefFile(cwd, brief string) (string, error) {
	path := filepath.Join(cwd, briefFileName)
	body := brief
	if !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return "", fmt.Errorf("could not write the briefing to %s: %w", path, err)
	}
	return filepath.ToSlash(path), nil
}

// briefPrompt puts the instruction to read the briefing ahead of the task.
//
// AHEAD, because the order is what makes it work: a session that reads the task
// first starts answering it, and the briefing arrives as correction. The task
// still has to be in the prompt rather than only in the file, or the session
// reads a briefing and sits there waiting to be told what to do with it.
func briefPrompt(prompt string) string {
	read := "Read " + briefFileName + " in this directory first. It is your briefing, written " +
		"for you by another agent, and it holds everything you are expected to know. " +
		"Re-read it whenever you lose the thread rather than guessing."
	if strings.TrimSpace(prompt) == "" {
		return read + " Then do what it asks."
	}
	return read + "\n\nThen: " + prompt
}

// childEnv builds the environment for a launched runner: everything inherited
// except the tainted keys, then the harness's own settings, then atrium's.
func childEnv(harnessEnv map[string]string, atrium map[string]string) []string {
	return childEnvFrom(os.Environ(), harnessEnv, atrium)
}

// overEnv is the harness env with one launch's additions over it. A copy, so
// the harness row is never written to.
func overEnv(harnessEnv, added map[string]string) map[string]string {
	if len(added) == 0 {
		return harnessEnv
	}
	out := make(map[string]string, len(harnessEnv)+len(added))
	for k, v := range harnessEnv {
		out[k] = v
	}
	for k, v := range added {
		out[k] = v
	}
	return out
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
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
