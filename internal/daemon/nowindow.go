package daemon

import "github.com/dovholuknf/atrium/internal/nowindow"

// hideWindow stops a spawned command flashing a console window on screen.
// See internal/nowindow.
//
// A source is a command on a timer, and the one that found this ran
// `npm view @openai/codex version` every ten minutes, so every tick opened a
// console window on the operator's desktop. That is also why the runner version
// check was moved into the daemon and reads a file instead: see runnerupdate.go.
//
// DELIBERATELY NOT APPLIED TO A RUNNER. Window launch mode exists to put a
// real terminal on screen, and `docs/terminal/supervision-design.md` is about the case
// where atrium owns the terminal instead. Both are windows somebody asked for.
// This is for the commands nobody asked to watch.
var hideWindow = nowindow.Hide
