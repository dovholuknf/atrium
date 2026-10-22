package cli

import "time"

// `atrium hook --event now`: prints one line, `now: <yyyy-mm-dd ddd HH:MM zone>`, which Claude Code adds to the context
// of a UserPromptSubmit. It is in the `--settings` atrium writes for a lean session (internal/daemon/lean.go), in place
// of the operator's own prompt hook that prints the same line and a long style reminder after it. It is paid on every
// prompt, so it stays one line. It reads no input and cannot fail.

// nowEvent is the --event value that selects this hook.
const nowEvent = "now"

const nowLayout = "2006-01-02 Mon 15:04 MST"

func nowLine(t time.Time) string { return "now: " + t.Format(nowLayout) }
