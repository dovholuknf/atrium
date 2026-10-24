// Package nowindow stops a child process flashing a console window on the
// operator's desktop.
//
// The hub and the rooms run without a console of their own, so a console child
// they start gets a NEW one rather than inheriting one that is already there.
// That is the window: it appears for as long as the command takes, in front of
// whatever somebody was doing. `git` is the worst offender, because gitsync runs
// it constantly, but every unwatched child does it.
//
// `HideWindow` alone is not enough, because the console is allocated before
// anything is asked about how to show it. CREATE_NO_WINDOW stops the
// allocation.
//
// THIS COVERS THE CHILD AND NOT ITS CHILDREN. A `pwsh` started hidden is quiet,
// and anything that script shells out to allocates its own console.
//
// NOT FOR A WINDOW SOMEBODY ASKED FOR: a runner's terminal, an editor or a
// terminal opened from a card, a pty child atrium supervises, or a process
// detached with a console of its own on purpose.
package nowindow
