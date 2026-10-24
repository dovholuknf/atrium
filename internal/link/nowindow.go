package link

import "github.com/dovholuknf/atrium/internal/base/nowindow"

// hideWindow stops a command the link runs unwatched opening a console on the
// operator's desktop. See internal/nowindow.
var hideWindow = nowindow.Hide
