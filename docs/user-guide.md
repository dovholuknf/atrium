# Atrium user guide

Real-world walkthroughs for the patterns this thing supports. If you want a feature reference instead, see the
[README](../README.md).

Patterns 1 to 6 walked the v1 hub and agent loop (`atrium hub`, `atrium agent`, the terminal UI, the choices
picker). Mode A is removed, so they are gone and the numbering below keeps its gaps. `docs/one-atrium-plan.md`
says why. The board does what the terminal UI did, and Mode B stays as `atrium serve`, `status` and `watch`.

## Pattern 0: having the daemon there in the morning

Nothing starts atrium on its own. A daemon started by hand once and left running looks like it comes back until
the day it does not, and starting it again from a different terminal can open a **different database**, because
which one you get depends on `WORKTREE_ROOT` in the shell you happened to be in.

```powershell
.\scripts\atrium-autostart.ps1
Start-ScheduledTask -TaskName atrium
```

That pins one command line and one database, started the same way every time. `-Remove` takes it away.

A logon task rather than a Windows service, deliberately: a service runs as SYSTEM in session 0, which cannot
open a pseudo terminal you can attach to, and supervision is most of what the daemon does.

If you start it by hand instead, **pass `--db`**. The daemon says so loudly when it opens a different database
than last time, but not being told at all is better than being told after the fact.

## Pattern 7: starting work from an issue or a ticket

Atrium does not learn what a Zendesk ticket is. Whatever already knows makes the worktree and hands it over.

The cheapest version is one line at the end of a script you already have:

```powershell
atrium launch --cwd $wtPath --title "zendesk-$id" --tags "zendesk,support,zendesk-$id" `
  --source zendesk --external $id --item-url "https://$zendeskHost/agent/tickets/$id" `
  --prompt "read this ticket, summarize it, and tell me which repository it is about"
```

The card arrives supervised, gated, tagged, and carrying a link back to the ticket.

The version that does not need you at a prompt is a **source**: a command atrium runs on a timer whose stdout is
a list of work items. Add one under **runners**. `scripts/sources/` has two working examples and a README.

Atrium holds an argv and an interval and never holds a credential. `gh` already has a token in the keyring it
uses; atrium has the path to a script that calls `gh`.

Items land in the **inbox**, which is a column that only appears when there is something in it. Pressing
**start** opens the launch dialog with everything the source knew already filled in, and starts the session onto
that same card so the work keeps its link to what it came from.

**Read the engineering versus support split in `docs/intake-design.md` before writing a source for a support
queue.** A support case names a customer rather than a repo, so it can be offered and not prepared, and it
carries somebody else's words, which is a reason to put the identifier on the card and not the subject line.

## Pattern 8: telling atrium the work is finished

Everything an agent reports lands in ready, so the board cannot tell "go and look at the result" from "answer
me". One command fixes that, from inside a session:

```powershell
atrium finish "bumped the vcpkg dep, ran the tests, opened a pull request"
```

The card moves to **done** and keeps that sentence. `--hand-back` puts it in **ready** instead, which is the
different and honest claim: handing the work over without saying it is over.

A command rather than a tool, so it works for codex and for a bare shell and not only for the runner that
happens to have a tool surface.

**Nothing tells a session this exists.** The way to make it happen is the seeded card action **write it up and
finish**, which sends exactly that instruction. Press it on any card.

## Pattern 9: things you say often, as buttons

Under **runners**, `actions` are named prompts offered on every card. Three are there to begin with. Limit one
to a tag or a runner when it only makes sense there.

`afterwards: ask the runner to quit` sends the prompt and then the harness's own exit keys, which is the "write
it up and go away" case and the reason this is not a saved snippet. It is best effort: a session atrium does not
own gets told to wrap up and has to be closed where it runs.

## Pattern 10: getting a file to a session you are not sitting at

Over an overlay this has no workaround at all, because the clipboard is on the machine with the browser.

Attach to a terminal and paste, or drag a file onto the pane. The bytes land in `.atrium/incoming` under the
card's working directory and the path is spliced into the terminal **without enter being pressed**, so you can
type a sentence around it and send it yourself.

Files only ever go into or out of one card's own directory. There is no way to ask atrium for a path that is not
below a card.

Coming the other way, a path the agent printed is a link. Hover a filename in the terminal and it underlines if
it is a real file in that card's directory, and clicking it opens the file **in the browser you are sitting at**,
in atrium's own text box. A directory opens the file drawer there instead. Nothing is decided by how a word
looks, so a version string, a hostname and a function call are left alone while `Makefile` is still a link, and
`internal/api/api.go:248:1` opens the file without the line number.

This is deliberately not the `open there` chip in the file browser. That one starts an editor on the machine the
daemon runs on, which over a share is a window in front of nobody.

A URL in the output is a link too, and that one opens in a new browser tab. The two do not fight: a URL is
matched first, so a path found inside one never claims it.

## Pattern 11: seeing a board change without restarting the board

The board is one file, and installing a new one restarts the daemon every live session is attached to. So a
change to it used to cost everybody else an interruption, taken before anybody knew whether the change was any
good.

```
# from the worktree that has the change, and after building it there
atrium preview --http 50022 --from live
```

That is a second daemon. Own database, own ports, own address file. `--from live` copies the cards the running
daemon has, because an empty board only shows you the empty state, which is the one state nobody was working on.
Ctrl-C stops it, and `--fresh` throws its cards away and starts the copy again.

Two things it deliberately will not do. It does not take the hooks: every claude session on the machine still
reports to the real daemon, so a preview is for LOOKING at a board and never a second place to work. And it
starts PASSIVE, so it does not start the fixtures or re-bind the shares it can see in the copy. Both rules, and
why running two ordinary daemons over one database is a different question with a worse answer, are in
`docs/preview-design.md`.

## Pattern 12: a session that is stuck, and one that asks another session

A session that stops shows as waiting, because a hook noticed nobody is typing. That says THAT it stopped and
never WHY, so the way to find out is to open its terminal and read back through it. From inside the session:

```powershell
atrium ask "which of these two schemas is authoritative"
atrium ask --continue "is the staging database safe to drop"
```

The question goes on the card, labelled **this agent has a question**, and it is not the same line as `why`:
that one is what the card is for and is still true next week. Without `--continue` the session is saying it has
STOPPED, and the card moves to waiting. With it the session is carrying on and the card does not move, because
a working session filed as waiting makes the count that drives every alert lie. The flag names the decision and
not the state, so the pair reads as stop by default and carry on by request. `--working` is the old name for it
and still works.

**Saying anything to that card answers it.** The question comes off, whether you type it into the message box or
press an action. Typing into the terminal does not: atrium cannot see that.

The other direction asks a peer instead of you:

```powershell
atrium peers
atrium ask --peer sg4/atrium-docs "which branch is base for the release notes"
```

That queues the question for the other session, which reads it on its next tool call or at the end of its turn,
with the command to answer already in it:

```powershell
atrium answer sg4/ziti-acme "base is main, the release branch is cut afterwards"
```

The answer arrives the same way and takes the question off the asker's card. A handle nobody has refuses and
answers with the handles that would have worked, so a guess turns into the list. Nothing here is typed into
anybody's terminal in either direction.

A blocked session that asked a peer still shows as waiting, and the card names the peer rather than reading as
a question for you. That is on purpose: it has stopped, and a peer that never answers otherwise looks exactly
like a session nobody noticed.

## Limits / what this won't do

- **No auth.** Single machine, localhost. If you bind to a non-loopback address, anything on your LAN can talk
  to the daemon. Don't do that without thinking about it first.
- **No multi-host.** If you need cross-machine, the right substrate is Agora (OpenZiti A2A), not extending
  Atrium with network identity / policy code.

## Reference -- where things live

| Thing | Path |
| --- | --- |
| Installed binary | `~\.atrium\bin\atrium.exe` on this machine, copied there by hand. There is no packaged install yet, so this path is a convention, not a product decision. Run the daemon from HERE: hooks, the logon task and the self-restart all name a path, and a build directory moves under all three |
| Build output | `<atrium-repo>\build.claude\atrium.exe`, which is what you build and then copy FROM |
| Project MCP config | `<repo or worktree>\.mcp.json` |
| Atrium repo docs | `<atrium-repo>\docs\` |
| gwt session JSONs | `$env:WORKTREE_ROOT\sessions\*.json` |
| gwt state log | `$env:WORKTREE_ROOT\watch\state.log` |
| claude MCP debug logs | check claude-code's per-session log location (varies by version) |
