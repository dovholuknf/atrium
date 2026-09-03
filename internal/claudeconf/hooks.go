package claudeconf

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Reading and writing the hook entries in Claude Code's settings.json.
//
// This is the one place atrium edits a file it does not own. Two things make
// that safe and both have to stay: the whole file is decoded into a generic
// map so keys atrium knows nothing about survive the round trip, and the
// previous contents are copied aside before anything is replaced. A malformed
// settings.json breaks every claude session on the machine, which is far worse
// than a hook that never got installed.

// HookEvent is one hook atrium wants registered, and what it buys.
type HookEvent struct {
	// Hook is Claude Code's name for the event, the key in settings.json.
	Hook string `json:"hook"`
	// Event names this to atrium, and is what identifies an entry already in
	// settings.json, so it has to be unique across every subcommand.
	Event string `json:"event"`
	// Sub is the atrium subcommand that reports it. Activity rides `hook`; a
	// session opening or closing rides `session`, which also has to find the
	// runner's process.
	Sub string `json:"-"`
	// Arg is what the subcommand's own --event expects, which is not always
	// the name above.
	Arg string `json:"-"`
	// Why is one line for the board, so the list is readable without the docs.
	Why string `json:"why"`
	// Optional keeps a hook out of "install everything". It is offered, its
	// state is reported, and it is never written unless it was asked for by
	// name. Only the Stop hook is optional, and see the note on it below.
	Optional bool `json:"optional,omitempty"`
	// Warn is what has to be understood before installing an optional hook.
	// Shown by the board next to the switch rather than buried in the docs.
	Warn string `json:"warn,omitempty"`
}

// WantedHooks are the hooks atrium can register, in the order they read best.
//
// All but one of these only REPORT. They post a fact and exit, nothing
// downstream reads the result, and the worst a broken one can do is leave the
// board out of date.
//
// Stop is the exception, and it is why it is `Optional`. A Stop hook is the
// only one whose output changes what the session does: answering it with a
// block tells the model to keep working. That is exactly what makes it
// valuable, because it is the only way to reach a session sitting idle, and a
// session sitting idle is when you most want to reach it. It is also the only
// way to hang every session on the machine. So it is offered by name, with
// what it does said out loud, and it is never swept in by "install all".
var WantedHooks = []HookEvent{
	{Hook: "SessionStart", Event: "session-start", Sub: "session", Arg: "start",
		Why: "a card appears when a session opens, before it does anything"},
	{Hook: "SessionEnd", Event: "session-end", Sub: "session", Arg: "end",
		Why: "the card goes to finished when the session closes"},
	{Hook: "PreToolUse", Event: "tool-start", Sub: "hook", Arg: "tool-start",
		Why: "which tool a session is running right now"},
	{Hook: "PostToolUse", Event: "tool-end", Sub: "hook", Arg: "tool-end",
		Why: "when that tool finished, so the card stops claiming it"},
	{Hook: "UserPromptSubmit", Event: "prompt", Sub: "hook", Arg: "prompt",
		Why: "you answered, so the card leaves needs-input"},
	{Hook: "SubagentStart", Event: "subagent-start", Sub: "hook", Arg: "subagent-start",
		Why: "the subagent count going up"},
	{Hook: "SubagentStop", Event: "subagent-end", Sub: "hook", Arg: "subagent-end",
		Why: "and coming back down"},
	{Hook: "Stop", Event: "turn-end", Sub: "turn", Arg: "end",
		Why:      "a message reaches a session that is sitting idle, and the card moves to needs-input",
		Optional: true,
		Warn: "this is the only hook that can change what a session does. it answers the end of a " +
			"turn, and answering with a message makes the session keep working on it. that is how an " +
			"idle session is reached at all, and it is also the one hook a bug in could stop a session " +
			"ending. it refuses to block twice in a row and it keeps going whenever atrium is not " +
			"reachable."},
}

// eventFor finds a wanted hook by its atrium event name.
func eventFor(event string) (HookEvent, bool) { return eventForTarget(Claude, event) }

func eventForTarget(t Target, event string) (HookEvent, bool) {
	for _, w := range t.Wanted {
		if w.Event == event {
			return w, true
		}
	}
	return HookEvent{}, false
}

// HookStatus is one wanted hook, and whether it is installed.
type HookStatus struct {
	HookEvent
	// Installed is true when a command for this event is already registered.
	Installed bool `json:"installed"`
	// Found is the command that satisfied it, so a hook installed under a
	// different path or an older script is visible rather than being reported
	// as simply present.
	Found string `json:"found,omitempty"`
	// Stale marks a command that reports this event but is not the binary
	// atrium is running from. Usually the old dotfiles script, or a second
	// checkout.
	Stale bool `json:"stale"`
	// Want is the command atrium would write.
	Want string `json:"want"`
}

// HookReport is everything the board needs to explain the situation.
type HookReport struct {
	// Path is the settings file this describes, which is the one atrium would
	// write to.
	Path string `json:"path"`
	// Exists is false when there is no settings.json at all, which is normal
	// on a fresh machine and means the write creates it.
	Exists  bool         `json:"exists"`
	Hooks   []HookStatus `json:"hooks"`
	Missing int          `json:"missing"`
	// Unreadable carries a parse error. Nothing is written when this is set,
	// because rewriting a file atrium could not read would lose whatever is
	// in it.
	Unreadable string `json:"unreadable,omitempty"`
	// Runner is which runner's configuration this describes, so a board
	// showing more than one knows which rows belong together.
	Runner string `json:"runner,omitempty"`
	// Trust is what has to happen after the file is written, when writing it
	// is not enough. Codex refuses to run a hook it has not been shown.
	Trust string `json:"trust,omitempty"`
}

// UserSettingsPath is the file atrium reads and writes: the one in the home
// directory.
//
// Not the project file. A hook belongs to the operator, not to a repository,
// and a per-project hook would report only the sessions that happened to be
// started there.
func UserSettingsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", "settings.json"), nil
}

// The hooks section is walked as generic maps rather than decoded into a
// struct.
//
// A struct round trip drops every key it does not declare, and this file
// belongs to the operator: a `timeout`, a matcher shape atrium has never seen,
// or a key Claude Code adds next month would all be deleted by a write that
// only meant to add one command. Generic maps are more code to read and they
// give the file back the way it arrived.

// hooksSection pulls `hooks` out as the shape it is: hook name to a list of
// matcher entries. A section in any other shape yields nothing, which reads
// as "nothing installed".
func hooksSection(doc map[string]json.RawMessage) map[string][]any {
	out := map[string][]any{}
	rawHooks, ok := doc["hooks"]
	if !ok {
		return out
	}
	var generic map[string]any
	if err := json.Unmarshal(rawHooks, &generic); err != nil {
		return out
	}
	for hook, v := range generic {
		list, ok := v.([]any)
		if !ok {
			continue
		}
		out[hook] = list
	}
	return out
}

// commandsIn reads the command strings out of one matcher entry.
func commandsIn(entry any) []string {
	m, ok := entry.(map[string]any)
	if !ok {
		return nil
	}
	list, ok := m["hooks"].([]any)
	if !ok {
		return nil
	}
	var out []string
	for _, h := range list {
		hm, ok := h.(map[string]any)
		if !ok {
			continue
		}
		if s, ok := hm["command"].(string); ok && strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	return out
}

// Inspect reports which wanted hooks are already registered.
//
// exe is the absolute path of the atrium binary, which is what makes a
// registered command "ours" rather than someone else's.
func Inspect(exe string) (*HookReport, error) { return InspectTarget(Claude, exe) }

// InspectTarget is Inspect for one runner's configuration. See target.go: the
// file format is shared and only the path and the wanted set differ.
func InspectTarget(t Target, exe string) (*HookReport, error) {
	path, err := t.Path()
	if err != nil {
		return nil, err
	}
	rep := &HookReport{Path: filepath.ToSlash(path), Runner: t.ID, Trust: t.Trust}

	raw, err := os.ReadFile(path)
	switch {
	case os.IsNotExist(err):
		// No file is a clean slate, not a problem.
	case err != nil:
		return nil, err
	default:
		rep.Exists = true
	}

	var doc map[string]json.RawMessage
	if rep.Exists {
		if err := json.Unmarshal(raw, &doc); err != nil {
			rep.Unreadable = err.Error()
		}
	}
	installed := registeredCommands(doc)

	for _, w := range t.Wanted {
		st := HookStatus{HookEvent: w, Want: hookCommand(t, exe, w)}
		for _, cmd := range installed[w.Hook] {
			if !reportsEventFor(t, cmd, w.Event) {
				continue
			}
			st.Installed = true
			st.Found = cmd
			st.Stale = !sameBinary(cmd, exe)
			break
		}
		// An optional hook that is not installed is not missing. It was never
		// promised, and counting it would leave the board permanently offering
		// to fix something that is off on purpose. A stale one still counts:
		// somebody installed it, and it is now pointing at the wrong binary.
		if (!st.Installed && !w.Optional) || st.Stale {
			rep.Missing++
		}
		rep.Hooks = append(rep.Hooks, st)
	}
	return rep, nil
}

// InstallResult says what an install actually did, so a caller can tell "I
// changed your settings" from "there was nothing to change". Both are success,
// and reporting them the same way is how a no-op comes to read as a write.
type InstallResult struct {
	// Changed is false when every named hook was already correct.
	Changed bool `json:"changed"`
	// Backup is where the previous file was copied, empty when nothing was
	// written or when there was no file to copy.
	Backup string `json:"backup,omitempty"`
}

// Install writes every wanted hook. See InstallOnly.
func Install(exe string) (*HookReport, InstallResult, error) {
	return InstallOnly(exe, nil)
}

// InstallOnly writes the named events, or all of them when events is empty.
//
// Already-correct entries are left exactly as they are, including any timeout
// or matcher the operator set. A stale one is replaced, because two commands
// reporting the same event would double every count.
func InstallOnly(exe string, events []string) (*HookReport, InstallResult, error) {
	return InstallOnlyTarget(Claude, exe, events)
}

// InstallOnlyTarget is InstallOnly for one runner's configuration.
func InstallOnlyTarget(t Target, exe string, events []string) (*HookReport, InstallResult, error) {
	var none InstallResult
	path, err := t.Path()
	if err != nil {
		return nil, none, err
	}

	raw, readErr := os.ReadFile(path)
	doc := map[string]json.RawMessage{}
	existed := readErr == nil
	if existed {
		if err := json.Unmarshal(raw, &doc); err != nil {
			// Refuse rather than overwrite. Whatever is in there is the
			// operator's, and atrium cannot merge into something it cannot
			// parse.
			return nil, none, fmt.Errorf("%s is not valid json, so nothing was changed: %w", path, err)
		}
	} else if !os.IsNotExist(readErr) {
		return nil, none, readErr
	}

	wanted := map[string]bool{}
	for _, e := range events {
		wanted[e] = true
	}
	matched, changed := 0, 0
	for _, w := range t.Wanted {
		if len(wanted) > 0 && !wanted[w.Event] {
			continue
		}
		// An empty list means "everything atrium normally wants", and an
		// optional hook is by definition not that. It is written only when
		// named, so nobody installs a Stop hook by pressing install all.
		if len(wanted) == 0 && w.Optional {
			continue
		}
		matched++
		did, err := upsert(t, doc, w.Hook, exe, w.Event)
		if err != nil {
			return nil, none, err
		}
		if did {
			changed++
		}
	}
	if matched == 0 {
		return nil, none, fmt.Errorf("no hook matches %v", events)
	}
	// Nothing to do is a success with no side effects. Writing anyway would
	// leave a backup of a file that never changed, and running install a few
	// times while working through the list would bury the one backup worth
	// having.
	if changed == 0 {
		rep, err := InspectTarget(t, exe)
		return rep, none, err
	}

	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, none, err
	}
	out = append(out, '\n')

	res := InstallResult{Changed: true}
	if existed {
		backup := path + ".atrium-" + time.Now().Format("20060102-150405") + ".bak"
		if err := os.WriteFile(backup, raw, 0o600); err != nil {
			return nil, none, fmt.Errorf("could not keep a copy of the old settings, so nothing was changed: %w", err)
		}
		res.Backup = filepath.ToSlash(backup)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, none, err
	}
	// Written through a symlink rather than over it. `settings.json` is
	// commonly a link into a dotfiles repo, and replacing the link with a
	// plain file silently detaches the repo: every later edit on either side
	// goes somewhere the other cannot see, and the divergence is only noticed
	// much later. Resolved explicitly rather than relying on the write
	// following the link, since that is platform behaviour and this is a
	// promise.
	target := path
	if real, err := filepath.EvalSymlinks(path); err == nil {
		target = real
	}
	if err := os.WriteFile(target, out, 0o600); err != nil {
		return nil, none, err
	}

	rep, err := InspectTarget(t, exe)
	return rep, res, err
}

// upsert puts atrium's command under one hook name, replacing a stale one and
// leaving everything else in place.
// changed reports whether anything was actually written, so an install that
// finds everything already correct writes no file and keeps no backup.
func upsert(t Target, doc map[string]json.RawMessage, hook, exe, event string) (changed bool, err error) {
	all := hooksSection(doc)
	if raw, ok := doc["hooks"]; ok {
		var probe map[string]any
		if err := json.Unmarshal(raw, &probe); err != nil {
			return false, fmt.Errorf("the hooks section is not in the shape claude code uses: %w", err)
		}
	}

	want := HookCommandFor(exe, event)
	entries := all[hook]

	// An entry that already reports this event is corrected in place, so a
	// matcher or timeout the operator set survives.
	for _, entry := range entries {
		m, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		list, _ := m["hooks"].([]any)
		for _, h := range list {
			hm, ok := h.(map[string]any)
			if !ok {
				continue
			}
			if s, _ := hm["command"].(string); reportsEvent(s, event) {
				// Already exactly right. Rewriting it would produce another
				// backup of a file nothing changed in.
				if s == want {
					if t, _ := hm["type"].(string); t == "command" {
						return false, nil
					}
				}
				hm["command"] = want
				hm["type"] = "command"
				all[hook] = entries
				return true, store(doc, all)
			}
		}
	}

	// Otherwise it joins the first catch-all entry, since claude code runs
	// every command under a matching entry and the permission hook usually
	// already lives there.
	for _, entry := range entries {
		m, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		matcher, _ := m["matcher"].(string)
		if matcher != "" && matcher != "*" {
			continue
		}
		list, _ := m["hooks"].([]any)
		m["hooks"] = append(list, map[string]any{"type": "command", "command": want})
		all[hook] = entries
		return true, store(doc, all)
	}

	all[hook] = append(entries, map[string]any{
		"matcher": "",
		"hooks":   []any{map[string]any{"type": "command", "command": want}},
	})
	return true, store(doc, all)
}

func store(doc map[string]json.RawMessage, all map[string][]any) error {
	b, err := json.Marshal(all)
	if err != nil {
		return err
	}
	doc["hooks"] = b
	return nil
}

// registeredCommands pulls every command string out of the hooks section,
// keyed by hook name. A section it cannot read yields nothing, which reports
// as "not installed" rather than as an error.
func registeredCommands(doc map[string]json.RawMessage) map[string][]string {
	out := map[string][]string{}
	for hook, entries := range hooksSection(doc) {
		for _, e := range entries {
			out[hook] = append(out[hook], commandsIn(e)...)
		}
	}
	return out
}

// reportsEvent decides whether a registered command is atrium reporting this
// event. Matched on what the command says rather than on the path, so the old
// PowerShell script counts as installed and gets replaced instead of being
// added alongside it.
//
// The subcommand is part of the match. `session` takes `--event start`, which
// on its own is too short to be sure about: `hook --event start` would be a
// different thing entirely, and a script called `-Event start` could be
// anyone's.
func reportsEvent(command, event string) bool { return reportsEventFor(Claude, command, event) }

func reportsEventFor(t Target, command, event string) bool {
	low := strings.ToLower(command)
	if !strings.Contains(low, "atrium") {
		return false
	}
	w, ok := eventForTarget(t, event)
	if !ok {
		return hasEventArg(low, event)
	}
	if w.Sub == "session" {
		// Ours when it runs the session subcommand, or when it is the script
		// that subcommand replaced.
		named := strings.Contains(low, " session ") ||
			strings.Contains(low, "atrium-session-hook")
		return named && hasEventArg(low, w.Arg)
	}
	if w.Sub == "turn" {
		// `end` is not a unique word: `session --event end` ends the same way.
		// They live in different hook arrays, so nothing collides today, but
		// that is the settings file's shape rather than a fact about the
		// commands, and a misfiled entry would be rewritten into the wrong
		// subcommand.
		return strings.Contains(low, " turn ") && hasEventArg(low, w.Arg)
	}
	// Activity args are unique words, so the subcommand adds nothing and
	// leaving it out keeps matching the old activity script.
	return hasEventArg(low, w.Arg)
}

func hasEventArg(low, arg string) bool {
	return strings.Contains(low, "-event "+arg) || strings.Contains(low, "--event "+arg)
}

// sameBinary reports whether a command runs the atrium we are running.
func sameBinary(command, exe string) bool {
	return strings.Contains(
		strings.ToLower(filepath.ToSlash(command)),
		strings.ToLower(filepath.ToSlash(exe)))
}

// HookCommandFor is the command line for one event. It has to agree exactly
// with what `atrium hook` accepts, so both live off the same shape.
func HookCommandFor(exe, event string) string {
	return HookCommandForTarget(Claude, exe, event)
}

// HookCommandForTarget is HookCommandFor for one runner's event set.
//
// The COMMAND is the same for both runners, because the atrium subcommand
// behind an event does not care who called it: a session starting is a session
// starting. Only which events exist differs.
func HookCommandForTarget(t Target, exe, event string) string {
	w, ok := eventForTarget(t, event)
	if !ok {
		return quoted(exe) + " hook --event " + event
	}
	return hookCommand(t, exe, w)
}

func hookCommand(t Target, exe string, w HookEvent) string {
	return quoted(exe) + " " + w.Sub + " --event " + w.Arg
}

// quoted is the executable's path, quoted when it needs to be. A path with a
// space in it is one argument, and every configuration file this writes into
// is read by a shell that would otherwise split it.
func quoted(exe string) string {
	p := filepath.ToSlash(exe)
	if strings.ContainsAny(p, " \t") {
		return `"` + p + `"`
	}
	return p
}
