# Open with the system (u-new-open-with-system)

Status: designed by @rnd 2026-09-30, for @runtime (tickets and the helper) and @ui (the menu). Nothing here is built.
Backlog item `docs/backlog/ui/u-new-open-with-system.md`.

## The answer

The daemon cannot open a folder for clint, and should not open files for him either. Proven on sg4 (section 1).
What works is a small helper that runs as the BROWSER's user, started by the browser through a custom URL scheme,
and told what to open by a single-use ticket the daemon issues. The browser is already in clint's session as clint,
so the window lands on his screen, in his Explorer, with his file associations.

- **Open folder** and **open with system editor** go through the helper. Windows first. macOS (`open`) and Linux
  (`xdg-open`) follow the same shape later.
- **Open in atrium** is the existing text editor, always shown, unchanged.
- **Open in editor** (`editor_command`, `internal/api/fileopen.go`) stays as it is. It runs as the room's user by
  design, and is the right tool for opening a file where the agent is working.

## 1. What was proven, 2026-09-30 18:55, sg4

The room runs as `claude`. This session is one of its runners.

| test, as `claude` | result |
| --- | --- |
| session of this process | 1, which `query user` shows is `clint`'s console session |
| `EnumWindows` from this process | sees clint's windows (Brave, Notepad++, VS Code), so it is on his desktop |
| `explorer.exe D:\tmp\rnd-open-test` | the launcher exits with code 1, no window with that title appears |
| `explorer.exe /separate,D:\tmp\rnd-open-test` | same, no window |
| `Start-Process D:\tmp\rnd-open-test` (the shell's open verb) | "Access is denied" |
| `Start-Process D:\tmp\rnd-open-test\probe.txt` | Notepad, running as `claude`, opens a visible window on clint's desktop |

So:

- **A folder cannot be opened by the daemon.** Explorer hands the request to the running shell, and the shell
  belongs to another user. There is no argument that avoids this. Running a second Explorer as `claude` would need
  settings in `claude`'s profile, and it would be a shell running as the agents' user on clint's desktop.
- **A file can be, but it is the wrong result.** It opens with `claude`'s associations (Notepad here, not clint's
  editor), as `claude`, and it works only because this room was started inside clint's session. A room started at
  boot, by a scheduled task, or over ssh is in another session, and the same call succeeds while showing nothing.
  Nothing reports that, so the menu item would sometimes do nothing silently.

## 2. The helper

### Install

`atrium open-helper install`, run once by the person who uses the browser, as themselves (not elevated). It
registers the `atrium-open:` URL scheme under that user's `HKCU\Software\Classes` pointing at the INSTALLED atrium
binary (`internal/claudeconf/whichexe.go`, never `build.claude/`), with the argument `open-helper "%1"`.
`uninstall` removes the key. Neither needs elevation, and neither touches the room.

### One open

1. The board POSTs `/v1/tasks/{id}/open-ticket` with `{"path": "...", "kind": "folder" | "file"}`.
2. The room resolves the path through `internal/safepath`: against the card's worktree for a card's files, and
   against the browse roots (`internal/api/browseroots.go`) for a folder picked in the file browser. Anything outside
   is `403`, as for download. For `file` it refuses what the default handler would run (section 3).
3. The room answers `{"url": "atrium-open://127.0.0.1:<its board port>/<ticket>"}`. The ticket is 128 random bits,
   good for 60 seconds, and single use. It is held in memory.
4. The board navigates a hidden frame to the URL. The browser asks once whether to open `atrium-open` links, and
   can remember the answer.
5. Windows starts the helper as the browser's user. The helper accepts only a host of `127.0.0.1`, `localhost` or
   `::1`, redeems the ticket with `GET /v1/open-ticket/<ticket>`, and gets back the kind and the absolute path.
6. It checks again, on its own side: the path exists, a folder is a directory, and a file's extension is not on
   the refused list. Then it runs `explorer.exe <dir>` for a folder, or `ShellExecuteW` with the `open` verb for a
   file. Never `cmd /c start`, never a shell.
7. Any failure is shown in a message box, because nothing else is watching the helper: `the ticket has expired, try
   again from the board`, `no atrium room answered at 127.0.0.1:7778 on this computer`.

### Why the ticket

A web page can navigate to `atrium-open:` URLs as easily as the board can. What it cannot do is name a path. The
helper takes nothing from the URL except where to redeem and what to redeem, and the daemon hands out a ticket only
to a request that passed security stage 0's `CrossOriginProtection` (`docs/rnd/security-design.md` section 4). A
guessed ticket is 128 random bits. A replayed one is already spent.

The redeem is a GET with no body and no side effect beyond spending the ticket, so it needs nothing from stage 0.
It is on the board listener and not the agent listener, and a lent session's guest listener refuses it like
everything else not on its allowlist.

## 3. Files the system must not open

`open` on a script runs it. The room refuses a `file` ticket, and the helper refuses to launch, for:

- `.exe .com .bat .cmd .ps1 .psm1 .vbs .vbe .js .jse .wsf .wsh .msi .msp .lnk .url .scr .hta .cpl .reg .jar .sh`,
  compared without case, including a double extension's last part (`notes.txt.lnk`).
- A file whose extension has no association. The helper checks with `AssocQueryStringW` and reports `no program is
  set to open .xyz files`. Opening one shows Windows' "how do you want to open this" dialog, which is harmless but is
  not what the item promised.
- On macOS and Linux, later: anything with the executable bit.

The menu item is not greyed for these. It is left out, and "open in atrium" and "open in editor" remain.

## 4. When the menu shows the items

Both conditions, as the item says:

1. The room is on the same machine as the hub, and
2. the request is the operator's own, by `edge.LocalOperator` (`docs/rnd/local-proxy-trust-design.md`): loopback,
   a loopback `Host`, no forwarding header, and not an overlay listener. The listener alone is not enough: a share
   started by hand at the loopback listener presents as loopback too.

Plus a third: the helper is installed on this computer. A browser cannot ask whether a URL scheme has a handler, so
the gear has a checkbox, `the open-with helper is installed on this computer`, stored in the browser's own storage,
off by default. `atrium open-helper install` prints `now tick "the open-with helper is installed" in the board's
gear`. If the box is ticked and the helper is missing, the browser does nothing, and the board says after 3 seconds
`nothing opened. Is the open-with helper installed?` The board cannot know whether it worked, so this line is shown
every time with "dismiss" and "don't show again".

## 5. Folder scope

Both, since the containment is already decided elsewhere. The card's own worktree, and any folder the file browser
can list, which the browse roots already bound. Explorer then lets clint go anywhere his own account can, which is
no more than he could do before atrium was involved.

## Stages

| stage | what | owner | size | acceptance test |
| --- | --- | --- | --- | --- |
| O1 | tickets: `POST /v1/tasks/{id}/open-ticket`, `GET /v1/open-ticket/{t}` | @runtime | small | a path outside the card is `403`, a `.ps1` file ticket is refused, a ticket redeems once and then answers `410`, one past 60 s answers `410`, a cross-site POST is refused by stage 0 |
| O2 | `atrium open-helper install`, `uninstall`, and the helper | @runtime | medium | install then uninstall leaves the HKCU key as it was. The helper given a host other than loopback exits without a request. Given a folder ticket it runs `explorer.exe` with that path as one argument, and given a `.lnk` it refuses even if the room issued the ticket. Manual on sg4: a folder opens in clint's Explorer, a `.md` in his editor |
| O3 | the menu items, the gear checkbox, the "nothing opened" line | @ui | small | headless: the items show only with a loopback board, a same-machine room and the box ticked, a `.exe` file has neither system item, clicking posts the ticket and navigates to the returned URL |

O1 needs security stage 0 on the board listener, which @runtime has in flight. O2 does not wait on O1 for its
registration half.

## Questions for later

None for clint. Decided here: a helper rather than the daemon, a checkbox rather than detection, both folder
scopes, and the refused list above. The helper is Windows only until someone uses the board on macOS or Linux
against a local room.
