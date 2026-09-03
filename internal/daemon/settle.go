package daemon

import (
	"errors"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// asExitError unwraps to an *exec.ExitError, whose Stderr is the only useful
// half of a failed command's error.
func asExitError(err error, target **exec.ExitError) bool {
	return errors.As(err, target)
}

// Proving a launched runner is actually running.
//
// The card is created before the process, and nothing checked afterwards. A
// command not on PATH, a bad flag or a missing API key left a card in `running`
// describing a process that never got off the ground.
//
// Two checks answering different questions:
//
//   - Did it survive being started? settleFor watches for an immediate exit,
//     the shape a misconfiguration takes.
//   - Is it still there? The reaper asks the operating system every twenty
//     seconds for the life of the card. See reaper.go.

// settleWindow is how long a launched runner has to prove it did not fall over
// on startup. Long enough to resolve, load and fail; short enough not to be
// noticed. Past it, the reaper takes over.
const settleWindow = 2 * time.Second

// startupFailureWindow is how long a runner has to live before its final output
// stops being read as a startup failure.
//
// Wider than settleWindow: a runner can print a banner and then fail on a
// config file or a missing key. Past this the last lines are a transcript, not
// an explanation.
const startupFailureWindow = 20 * time.Second

// settleFor waits briefly for a freshly launched runner to fall over.
//
// Returns the output it produced when it did, and "" when it is still running.
// A command that cannot start says why on its own terminal, and that text is
// already in the scrollback. Reporting a bare "exited" instead leaves a dead
// card and a dozen candidate causes.
func (d *Daemon) settleFor(taskID string, window time.Duration) (string, bool) {
	r := d.sup.get(taskID)
	if r == nil {
		// Nothing to watch. Window mode hands off to a terminal that exits at
		// once by design, so it cannot be checked this way.
		return "", true
	}
	select {
	case <-r.done:
		return lastOutput(r.buf.Snapshot(), 12), false
	case <-time.After(window):
		return "", true
	}
}

// ansi strips the escape sequences a terminal would have consumed, so a failure
// message reads as text.
var ansi = regexp.MustCompile(`\x1b\[[0-9;?]*[a-zA-Z]|\x1b\][^\x07]*\x07|\r`)

// lastOutput takes the final few non-empty lines of a terminal's scrollback.
// The end, not the beginning: a program that fails to start prints its banner
// first and its complaint last.
func lastOutput(buf []byte, maxLines int) string {
	text := ansi.ReplaceAllString(string(buf), "")
	var lines []string
	for _, ln := range strings.Split(text, "\n") {
		if t := strings.TrimSpace(ln); t != "" {
			lines = append(lines, t)
		}
	}
	if len(lines) == 0 {
		return ""
	}
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	return strings.Join(lines, "\n")
}
