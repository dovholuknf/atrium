# Job: a second board window resizes a terminal and the size never comes back

Repo: atrium, branch claude/two-window-size in this directory. First run `git fetch hub` and
`git rebase hub/claude/main`. Read AGENTS.md first.

## The bug (clint, 2026-10-08)
He opened a second board window, which attached a terminal and resized the pty to its own width. He then left that
terminal in the second window, and the size did not return to the first window's. "not a huge deal". No popup wanted.

## Want (decided, build this)
The pty size follows the window that most recently had focus or input on that terminal. When a window detaches, or
loses focus while another attached window still shows that terminal, the size returns to the remaining window's size
(the most recently active one). One window: unchanged behaviour.
Where to look: the terminal attach websocket (internal/daemon/attach.go) and how it takes a resize, the pty host path
(internal/ptyhost) if resizes go through it, and the board side that sends resizes (internal/api/web/js, grep for the
resize message the attach sends). The server is the one that knows every attached window, so keep per-attach sizes
there and apply the right one, and have the board send a focus or input signal if it does not already. Tests: Go tests
on the attach side with two fake attaches (resize, focus, detach, size returns), and a headless board check
(scripts/test-board-headless.js) if the board sends something new.
Write changelog/ui/2026-10-10-u-two-window-terminal-size.md and docs/changes/u-two-window-terminal-size.md (format in
docs/changes/README.md). No BOM in any file.

## Rules
- Run tests in the FOREGROUND, only the packages you touch, with -count=1. Never end a turn while a command runs.
- Commit on your branch, one-line message, no trailer, then `git push hub claude/two-window-size`.
- Prose: wrap at 120, no em-dashes, no semicolons in prose.
- Finish with `atrium_done <sha>`, or `atrium_blocked <reason>` if stuck.

If you have `atrium_git_url`: to read code that is not in your cwd, call it, then fetch it from the URL it gives. Never ask for a paste.

`atrium_resources` lists the machines and environments you may use.
