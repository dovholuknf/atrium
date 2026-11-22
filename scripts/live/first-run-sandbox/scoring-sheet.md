# atrium first run: sandbox scoring sheet

Built from the first-run review's "first ten minutes" (`r-first-run-review`, sections 5.3 and 7) for the 0.0.6
release. Score the release as a stranger would meet it: one clean Windows, the README's install line, then the
board. Owner item: `u-new-user-experience-owner`.

## Before you start

- The host needs Windows Pro, Enterprise or Education with Windows Sandbox on. Turn it on once from an admin
  PowerShell with `Enable-WindowsOptionalFeature -Online -FeatureName Containers-DisposableClientVM`, then reboot.
- Keep this sheet open on the host. The sandbox forgets everything when it closes, so write times and notes here.
- Double-click `atrium-first-run.wsb`. It maps no host folder, has networking and the clipboard, and opens a
  PowerShell window in the sandbox's home folder with the install line printed.
- Start a stopwatch when that PowerShell window appears. "Time" is the stopwatch reading when the row's last
  expected thing shows. "Target" is the review's budget for that row.
- The sandbox account `WDAGUtilityAccount` is an administrator, so the admin hint in row 6 is expected.
- Nothing else is installed: no git, no Go, no coding agent. Rows 8 to 10 are path A (no agent). Path B (rows 11 to
  14) installs git and Claude Code, which needs your own Claude login inside the sandbox.

Mark Pass with `x` for pass or `F` for fail. A row that is right but confusing is a fail with a note: the review's
bar is that a new user needs no doc.

## The first ten minutes

| # | Do | You should see | Target | Time | Pass | Notes |
| --- | --- | --- | --- | --- | --- | --- |
| 1 | Paste `irm https://raw.githubusercontent.com/dovholuknf/atrium/main/install.ps1 \| iex` | `installing atrium v0.0.6 (atrium_v0.0.6_windows_amd64.zip)`, `checksum ok`, `installed ...\.atrium\bin\atrium.exe`, `added ... to your user PATH`, `board: http://127.0.0.1:7778`. No red text, no Defender or SmartScreen stop | 1:00 | | [ ] | |
| 2 | Wait, touch nothing | A browser opens on its own at `http://127.0.0.1:7778/#start=C:/Users/WDAGUtilityAccount`. Edge's own first-run screens do not count against atrium, note them | 1:15 | | [ ] | |
| 3 | Read the board | A "getting started" card: "WDAGUtilityAccount, your board is empty", a count and bar, and rows: the hub is running (ticked), this machine is a room (ticked, name and account shown), start your first agent in your own folder, bring in a session you already have, harden this setup (optional chip) | 1:45 | | [ ] | |
| 4 | Hover each circled ? on the card | Each explains its row in one or two plain sentences. No paragraph of help text sits on the card itself | 2:30 | | [ ] | |
| 5 | Close the browser tab, open `http://127.0.0.1:7778` by hand | Same board, same checklist state. The "getting started" pill shows the count | 2:45 | | [ ] | |
| 6 | Open a new PowerShell window, type `atrium run` | It says atrium is already running and where, or starts nothing harmful. Write down exactly what it prints. Then `atrium stop`, and `atrium run` again: the banner names the board address, says agents run as you, an administrator, and gives one next step. No browser opens this time (not a first start) | 4:00 | | [ ] | |
| 7 | `atrium --help` | Grouped help with a short "start here" group first, not sixty flat verbs. The root line does not say Claude Code only | 4:30 | | [ ] | |

## Path A: no agent installed

| # | Do | You should see | Target | Time | Pass | Notes |
| --- | --- | --- | --- | --- | --- | --- |
| 8 | `mkdir $HOME\work\thing` in PowerShell | (setup only) | | | | |
| 9 | On the board, "start an agent" | The new agent dialog offers folders, including the one `atrium run` started in. "where it works" says in place by default. With no runner on the machine it says so in plain words and how to get one (install links), never a bare error | 5:30 | | [ ] | |
| 10 | Type or pick `C:\Users\WDAGUtilityAccount\work\thing`, a plain folder, not git | Either a terminal card starts in that folder, or the dialog says why it cannot and what to do. No card-root refusal and no word about `claude/*` branches | 6:00 | | [ ] | |

## Path B: with Claude Code

| # | Do | You should see | Target | Time | Pass | Notes |
| --- | --- | --- | --- | --- | --- | --- |
| 11 | Install Git for Windows (Edge, git-scm.com, defaults), then `irm https://claude.ai/install.ps1 \| iex`, then `claude` once to log in, then exit it | Claude Code's own install. Not scored against atrium, but note the time it took | | | | |
| 12 | Back on the board, without restarting atrium, "start an agent" | Claude is found and offered as the runner by default. No "restart atrium" needed. If it asks for a restart, the message says exactly that | +0:30 | | [ ] | |
| 13 | Launch Claude in `work\thing`, in place | A card appears with a live terminal running Claude in that folder. No card-root or worktree question | +1:00 | | [ ] | |
| 14 | Ask it to create `hello.txt` | The first tool call waits as needs permission. The card says in one line what the gate is and offers allow once and always. Pressing always lets the next same call pass without asking | +2:30 | | [ ] | |
| 15 | Let the turn end | A notice "... is idle, its first turn is done". The checklist ticks "start your first agent" | +3:00 | | [ ] | |

## After first value (not timed against the ten minutes)

| # | Do | You should see | Pass | Notes |
| --- | --- | --- | --- | --- |
| 16 | `cd $HOME\work; git init repo; cd repo; git commit --allow-empty -m init` (path B only), then launch in it "in a copy" | The copy is made from the current HEAD with no `origin` and no `claude/main`, and the dialog says where the copy lives | [ ] | |
| 17 | Start `claude` by hand in `work\thing` in a plain PowerShell window, then "adopt a session" on the board | That session is listed. Adopting it ends it there and resumes it in a card with its history | [ ] | |
| 18 | "harden this setup" | The wizard opens from the chip, every screen can be skipped, and closing it leaves the board working | [ ] | |
| 19 | `atrium hook uninstall` | It says what it removed from `~\.claude\settings.json` and your own entries are left. `atrium hook install` puts them back | [ ] | |
| 20 | `atrium uninstall` | It stops the room and the hub, removes the hooks, and says which folder to delete by hand. The board stops answering | [ ] | |

## Summary

| Question | Answer |
| --- | --- |
| Paste to the board in the browser (rows 1 to 3) | |
| Paste to a first idle card (row 15, path B) | |
| Did you need a doc, a search or a guess anywhere? Which row? | |
| The one thing to fix first | |
| Anything atrium printed that a stranger would not understand | |

Not covered here: a second machine (`rooms add`) and autostart at logon. Only one sandbox runs at a time and a
restart wipes it, so those need a real second machine.
