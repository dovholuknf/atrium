package daemon

import (
	"strings"

	"github.com/dovholuknf/atrium/internal/store"
)

// First-run dialogs on a launched claude card. See backlog-2 item 67.
//
// Two of claude's dialogs sit between a launch and its prompt, and whatever is
// typed first answers them: the folder-trust dialog, which the first say
// answered "No, exit", and "Try the new fullscreen renderer?". Both are fixed
// where they start, before the process does, rather than by holding typed input
// until the runner reaches its prompt. A hold needs a signal that the prompt is
// up, and there is none that always comes: a claude in an untrusted folder runs
// no hooks at all, and one started without a prompt posts nothing until it is
// typed into. So a hold either waits forever or times out into the same dialog.
//
// Trust is written by the claude adapter in internal/runnersetup, for the launch
// folder only and under claude's own lock on ~/.claude.json. The renderer is
// declined here, by environment, so nothing shared is written for it.

// classicRendererEnv keeps a supervised claude on its classic renderer.
//
// Atrium draws a supervised terminal in xterm.js and keeps its scrollback. The
// fullscreen renderer draws on the alternate screen, which has no scrollback, so
// a card on it loses its history on the board. Claude reads this variable before
// its settings, a fresh install's fullscreen default and its rollout flag, and
// with it set claude never offers the fullscreen dialog. Claude names it itself:
// "CLAUDE_CODE_DISABLE_ALTERNATE_SCREEN=1 forces that any time".
const classicRendererEnv = "CLAUDE_CODE_DISABLE_ALTERNATE_SCREEN"

// rendererEnvKeys are the variables that choose claude's renderer. A launch
// env naming either is the operator choosing, and atrium leaves it alone.
var rendererEnvKeys = []string{classicRendererEnv, "CLAUDE_CODE_NO_FLICKER"}

// classicRendererDefault is the renderer variable a launch supplies, and
// whether to supply it. Only a claude runner atrium draws in a pty: a window
// launch is in a terminal of the operator's, whose renderer is theirs.
//
// A DEFAULT, as ATRIUM_PERM_GATE is. The inherited CLAUDE_CODE_ variables are
// already stripped (see inheritedTaint), so the launch env is the only place an
// operator can have said otherwise, and matched case-insensitively for the
// reason permGateDefault gives.
func classicRendererDefault(h *store.Harness, launchEnv map[string]string) (string, bool) {
	if h == nil || h.LaunchMode != store.LaunchPTY || !isClaude(h) {
		return "", false
	}
	for k := range launchEnv {
		for _, name := range rendererEnvKeys {
			if strings.EqualFold(k, name) {
				return "", false
			}
		}
	}
	return "1", true
}
