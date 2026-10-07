---
title: Patterns
description: Everyday recipes. Starting work from a ticket, finishing it, files, previews, stuck sessions, and Defender.
---

# Patterns

Recipes for the things people do with atrium every day. Each one says what to run and why it works that way. The
pages under **Using atrium** have the full detail.

## Atrium there in the morning

Register this machine's room to start at login, once. [Install](./install.md#start-at-login) has the line for each
platform. It pins one command line and one database, started the same way every time.

Started by hand instead, a room opens whichever database `WORKTREE_ROOT` in that shell points at, and a different
one looks exactly like your board losing everything. If you start it by hand, **pass `--db`**. Atrium warns when it
opens a different database than last time, but not needing the warning is better.

On Windows it is a logon task, not a service, on purpose: a service runs in session 0, which cannot open a terminal
you can attach to.

## Starting work from an issue or a ticket

Atrium does not learn what a Zendesk ticket is. Whatever already knows makes the worktree and hands it over. The
cheapest version is one line at the end of a script you already have:

```powershell
atrium launch --cwd $wtPath --title "zendesk-$id" --tags "zendesk,support,zendesk-$id" `
  --source zendesk --external $id --item-url "https://$zendeskHost/agent/tickets/$id" `
  --prompt "read this ticket, summarize it, and tell me which repository it is about"
```

The card arrives supervised, gated, tagged and linked back to the ticket. The version that does not need you at a
prompt is a **source**, a command atrium runs on a timer. [Intake and sources](./intake.md) covers it. Atrium holds
the command and an interval and never a credential: `gh` already has its token, and atrium has the path to a script
that calls `gh`.

## Telling atrium the work is finished

Every turn that ends lands in **ready**, so the board cannot tell "go and look at the result" from "answer me". One
command from inside the session fixes that:

```bash
atrium finish "bumped the dependency, ran the tests, opened a pull request"
```

The card moves to **finished** and keeps the sentence. `--hand-back` puts it in **ready** instead: handing the work
over without claiming it is over. It is a command rather than an MCP tool so it works for every runner, a bare shell
included.

Nothing tells a session the command exists. The seeded card action **write it up and finish** sends exactly that
instruction. Press it on any card.

## Things you say often, as buttons

**Actions** on the **rooms** tab are named prompts offered on every card. Limit one to a tag or a runner when it only
makes sense there. An action can ask the runner to quit afterwards: atrium sends the prompt, then the runner's own
exit keys. A session atrium does not own gets told to wrap up and has to be closed where it runs.

## Getting a file to a session you are not sitting at

Over an overlay the clipboard is on the machine with the browser, so there is no other way. Attach to the terminal
and paste, or drag a file onto the pane. The bytes land in `.atrium/incoming` under the card's directory, and the
path is typed into the terminal **without pressing enter**, so you can write a sentence around it.

The other way, a path the agent printed is a link. Click it and the file opens in the browser you are sitting at.
Files only ever go into or out of one card's own directory. [Files](./files.md) has the rest.

## Looking at a board change without restarting anything

A new board build means restarting whatever serves the board. On the hub that costs nobody a session, so most board
changes are a hub restart. To look at a change before anybody else sees it, run a preview from the worktree that has
it, after building it there:

```bash
atrium preview --from live
```

That is a second atrium with its own database, ports and address file. `--from live` copies the cards the running
room has, because an empty board shows only the empty state. It takes no hooks, so every session still reports to
the real room, and it starts passive: no fixtures, no shares. It is a place to look, never a second place to work.
Ctrl-C stops it, and `--fresh` starts the copy again.

## A session that is stuck, and one that asks another

A stopped session shows as waiting, which says that it stopped and never why. From inside the session it can say:

```bash
atrium ask "which of these two schemas is authoritative"
atrium ask --continue "is the staging database safe to drop"
```

The question goes on the card as **this agent has a question**. Without `--continue` the session has stopped and
the card moves to ready. With it the session carries on and the card stays put, because a working session filed as
waiting makes every alert count lie. Saying anything to the card answers it. Typing into the terminal does not,
because atrium cannot see that.

A session can ask a peer instead of you:

```bash
atrium peers
atrium ask --peer sg4/atrium-docs "which branch is base for the release notes"
atrium answer sg4/ziti-acme "base is main"
```

The card of a session waiting on a peer names the peer, so a peer that never answers does not look like a session
nobody noticed. [Messages and peers](./messages.md) has the delivery rules.

## Windows Defender eating the machine

A Windows room running several agents can lose more CPU to Defender than to the agents. Real-time protection scans
every file a Go build writes, every test binary, and every file the headless board tests touch. On one room
`MsMpEng.exe` sat at 103% while two agents built and tested.

`scripts/room-defender.ps1` works out the exclusions. Run it as the account the agents run as, not elevated:

```powershell
pwsh -File scripts\room-defender.ps1 local        # this machine
pwsh -File scripts\room-defender.ps1 sg3          # a room, over ssh
```

It reads that account's Go caches, the clone's build folder and the worktree root, and moves `go test`'s link
output inside an excluded path. Exclusions need elevation, so it prints one `Add-MpPreference` line for an
administrator to paste into an elevated shell. It never writes a script for them, because the agents' account could
change a script it owns. Every path is checked first, and a drive root, a wildcard or a quote is left out with a
warning. `scripts/provision-room.ps1` runs it on every Windows room it provisions.

The agents' account should be a standard user of its own, not an administrator and not your everyday login. Atrium
warns when it is not.
