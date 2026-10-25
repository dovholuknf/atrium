# internal/base/claudeconf

Reading and writing Claude Code's own `settings.json`. Two files, and one of them is a landmine.

## Do not answer "which binary" with os.Executable()

`whichexe.go` exists because that mistake was made in three places at once and produced a board reporting six
working hooks as `points elsewhere`, correctly.

`os.Executable()` is the binary somebody TYPED. It is right in exactly one place, inside the daemon, where it is
the daemon. Everywhere else it means `atrium hook install` run from a fresh build writes the build directory's
path, the daemon is running from somewhere else, and the next rebuild puts the drift straight back.

**The identity is the running daemon's binary,** which the daemon records in the location file every hook
already reads to find the port. `HookExe` resolves `ATRIUM_HOOK_EXE`, then that, then the caller's own path, and
never returns an error: no daemon running is the ordinary case for somebody setting this up.

`defaultLocationPath` is duplicated from `internal/daemon/whereami.go` on purpose, because that package imports
this one and importing it back is a cycle. If the two ever disagree the symptom is a hook naming a binary that
is not the daemon's, which is the exact defect this file exists to fix, so `whichexe_test.go` asserts they
agree.

## Installing keeps the operator's own hooks

`InstallOnly` REPLACES an atrium entry rather than adding a second command beside it, and it leaves everything
else in the file alone. A row running somebody's own `atrium-session-hook.ps1` matches on purpose: marking it as
foreign would leave a duplicate hook behind.

The board's "install all" deliberately never sweeps in `Stop`. It is the one hook whose answer changes what a
session does, so it is offered and never assumed.

## The posture every hook inherits

A hook must never fail a session. Whatever is written here runs on the hot path of every tool call in every
session, and the daemon's side of that contract is in `internal/daemon/AGENTS.md`.
