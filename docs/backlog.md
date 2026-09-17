# What is left

Open items only. Ranked by how good the thing is, not by how long it takes.

**Effort is a note inside an entry and never the ordering.** Sorting by cost buries the thing that would change
how the tool feels under a week of quiet plumbing, every time.

Finished work is not here. `CHANGELOG.md` is what landed and `docs/status.md` is where each thing stands. A
backlog that also holds its own history is a backlog nobody reads to the bottom of, which is what this became
before it was rewritten on 2026-09-06.

Each entry says what it is, why it is worth doing, and what it depends on. Nothing else.

## Two shapes, one list

There are entries in two forms and this file is the index of both. They are not competing lists and neither is
the stale copy of the other.

**Numbered themes, below.** Standing work, ranked. An entry is a direction rather than a ticket, it has no
date, and it survives being half done. These are edited in place as the picture changes.

**Dated items, in `docs/backlog/`.** One file each, named `backlog-YYYY-MM-DD-NNN`, raised from a specific
complaint and opening with the prompt that raised it, verbatim. These are append-only: an item is written once
and its status line is the only thing that moves. The verbatim prompt is the point, because the wording of the
complaint is usually more precise than any summary of it.

A theme becomes a dated item when somebody hits it and says so. A dated item does not graduate into a theme.

**The two numberings are separate and neither may be renumbered.** Theme `7` and `backlog-2026-09-15-007` are
different things. The numbers are identifiers, not positions: sessions are handed work by number, so
renumbering silently reassigns somebody else's item. See the hazard note at the bottom of this file.

---

## 1. Rooms, past the first stage

**Stage one landed.** Another machine runs its own atrium, dials this one over ziti, and its cards appear on
one board. Two Linux machines are doing it now.

What is left is what makes it more than a status page:

- **Permission requests from a room.** A card on another machine that is blocked waiting for a human is
  invisible unless somebody opens that room's own board, which defeats the point. The design is settled and it
  is the one thing the forum cannot answer on a leaf's behalf: the leaf holds the channel, so the hub forwards
  the decision and the leaf unblocks its own request. `docs/federation-design-v2.md` has it.
- **Attach by redirect.** A pty cannot leave the machine that made it and never will. What CAN happen is the
  board noticing a remote card and sending you to that room's own board with the right terminal already open.
  The row carries the address already, so this is a link that knows about `#term=`.
- **A room that is not a shell script.** Right now a room is `atrium room` under `nohup`. It should be the same
  service the packaging work installs, so a room survives a reboot.

**Why it is first.** It is the only thing here that changes what atrium IS rather than what it does. One
person, many machines, one place to look.

**Effort:** permissions is two days. The rest is a day.

## 2. Starting a card from a ticket, an issue or a pull request

Designed in `docs/scm-design.md`, reviewed, not built. A table of URL recognisers, one row each, that turns a
pull request or a ticket into a filled-in launch dialog.

**Why it is high.** It is the difference between atrium being where you watch work and atrium being where work
starts. Every session today begins with somebody pasting context by hand.

**Depends on** nothing. The intake path, the launch API and the card origin fields all exist.

**Effort:** two days, most of it in the recognisers rather than the plumbing.

## 3. Multi-tenant atrium

**Back on the list.** It was written down as the one idea here that would be a PRODUCT rather than a feature,
and parked because it contradicts every design note that says one person, one machine, no auth.

Two of those objections moved tonight:

- **Authentication.** No longer hypothetical. The published board can require an OIDC sign-in and atrium still
  holds no credentials, which is the shape a tenant boundary would need.
- **Federation.** Rooms are the same idea inside out and the cheaper half of it, and they work.

What has NOT moved, and is still the hard part: **the supervisor**. The daemon owns a pty per runner in the
operator's own logon session, which is why the autostart is a task and not a service. A cluster has no logon
session, so runners would have to move into containers with the terminal proxied, which is a different program.
A runner also holds credentials, and ten developers on one deployment means ten sets somebody is responsible
for.

**Still probably a fork rather than a flag.** Bolting tenancy onto a tool designed around one person would cost
the tool its clarity. Worth deciding deliberately rather than drifting into.

**Effort:** weeks, and a decision before any of them.

## 4. Publish a release

It builds, it installs, and it starts by itself on all three operating systems. Nothing has been published.

Six commands, written out at the end of `docs/packaging.md`. Scoop first, because it exercises the release
shape end to end before anything harder depends on it.

**Why it is here rather than higher.** It is the only thing on this list that lets somebody who is not the
author use atrium. It is not higher because it is entirely mechanical and needs no thought, only an account and
a decision.

**Still open under it:** an apt and yum repository, which is what buys `apt upgrade` and costs a GPG key with a
storage, trust and rotation story. Homebrew, Chocolatey and the Microsoft Store, each needing a certificate or
an account. And **self-update against a package manager**, which is unsolved: `restart_atrium` renames a staged
binary over the running one, and under a package manager that file belongs to the manager.

**Effort:** an afternoon for the first publish. The rest is unevenly weeks.

## 5. A runner that asks ANOTHER runner for help

**The verb landed.** `atrium ask` lets a session say it is stuck and what would unstick it, and the ask goes on
the card where the board already draws it. Stopped and working are told apart, because filing a working
session as waiting makes the count that drives every alert lie.

What is left is the half that makes it a team rather than a list: the ask currently reaches a HUMAN. It should
be able to reach another agent.

- **Route an ask to a peer.** `atrium ask --peer <handle>` would queue the question for another session
  through the channel that already exists, rather than putting it on a card and waiting for somebody to read
  it.
- **A card that shows it is asking.** The ask lands in `why`, which is drawn, and nothing distinguishes "this
  is what the card is for" from "this is what it needs right now".
- **An answer that comes back.** Today a human reads the ask and types into the terminal. A peer answering
  would want the reply to land the same way an ask does.

**Depends on** nothing new. The peer bus, the MCP tools and the verb all exist.

**Effort:** a day.

## 6. Hooks for runners that are not Claude Code

Everything atrium knows about a session comes from Claude Code's hooks. A codex or an aider session gets a
card, a terminal and a permission gate, and no activity, no session lifecycle, and no resume id.

**Why it matters more than it looks.** The harness table already offers four runners as equals and only one of
them tells the truth about itself. That is a promise the board is making and not keeping.

**Effort:** two days, and most of it is finding out what each runner will actually tell you.

## 7. Themes you can edit, and themes you can bring

TERMINAL themes, not the board's skins, which are done. Today the sixteen ANSI colours per theme are baked into
the page and a new one is a code change.

**Why.** People have a colour scheme they have used for years and want it here, and the import format that
matters is Windows Terminal's, which is already the source of the ones that shipped.

**Effort:** a day.

## 8. Themes, part two: a check that finds a bad colour without hovering it

Both open CSS nits are fixed, and one of them generalises into a rule worth enforcing: **a hover that changes
lightness in a fixed direction is wrong on half of twenty skins.** Anything meaning "more prominent" has to
move relative to the skin's own text colour rather than toward white.

`docs/css-nits.md` has the design for `scripts/check-contrast.js`: parse every skin out of the stylesheet,
composite the `rgba` lifts against the surface they actually sit on, and fail below a WCAG floor. It catches
"invisible" and "far too light", which is the whole nit list so far. It does not catch "ugly".

**Why it is worth building rather than fixing nits as they turn up.** Nobody is going to open twenty skins and
hover every element by hand, so the alternative is finding these one screenshot at a time.

**Effort:** a day, and the hazard is named in that document: comparing a raw `--lift` against a text colour
passes everything and proves nothing.

## 9. zrok limits, before you press a button that fails

An account has limits, and today the way you discover one is a share that refuses after several seconds inside
somebody else's API.

**Refused once already** because it needs an API shape the SDK does not expose. Worth another look now that
`overlay_reserve.go` already talks to the zrok REST API directly for names.

**Related and unresolved:** the demo account's `POST /share` returns `500` with an empty body for every share,
public or private, and the plain `zrok` CLI fails identically. That is an instance problem rather than an
atrium one, and it is why the reserved-share work is built but unproven.

**Effort:** a day.

## 10. Postgres

The schema is written for it and nothing has ever run it there: text ULID keys, RFC3339 timestamps, `CHECK`
instead of enums, `?` placeholders.

**Why it is low.** Nothing needs it until multi-tenant does, and it is the easy half of that.

**Effort:** a day to find out, longer to trust.

## 11. Approvals from a phone

Deprioritized on purpose: auto mode makes it moot most of the time. Still the right answer for the case auto
mode is wrong for, which is a decision you actually want to make.

**Depends on** the board being reachable, which it now is, and on the login that now sits in front of it.

**Effort:** a day.

## 12. Small debts

Kept together because none is worth its own entry, and the list is short on purpose.

- **Path completion is unproven in a browser.** It is built and guarded by two invariants, and nobody has
  watched it complete a path.
- **The popped-window close is unproven in a browser.** Same. The cause is known and the teardown now has a
  caller, and three previous fixes were also believed to work.
- **Auth has no PKCE, no refresh and no roles.** Everybody who gets in gets the whole board, which is the same
  grant a share has always been.
- **`atrium room` has no UI.** A room is added by running a command on that machine, and the hub only lists
  what turns up.

---

## Dated items

One file each in `docs/backlog/`, newest last. Every one opens with the prompt that raised it. This is the
index and the files are the entries: read the file before starting one, because the verbatim complaint usually
says something the title cannot.

Status lives on each item's own page, because it is the thing that moves and an index that also tracks state
is two places to remember. A list rather than a table, because a table row cannot wrap at 120 and these
titles are the complaint rather than a label.

- **[001](backlog/backlog-2026-09-13-001.md)** Named SCM providers, with roots, worktree support and
  auto-discovery.
- **[002](backlog/backlog-2026-09-13-002.md)** Resume remembers where it was, and the conversation list
  lives in the menu.
- **[003](backlog/backlog-2026-09-14-003.md)** Paste a link, press enter, get a card. Its own flow, on
  ctrl-shift-n.
- **[004](backlog/backlog-2026-09-14-004.md)** The new agent dialog asks ten questions, and one of them is
  required.
- **[005](backlog/backlog-2026-09-14-005.md)** "Directories already on the board" is six buttons that read
  as a suggestion.
- **[006](backlog/backlog-2026-09-14-006.md)** Resizing a pane shows the same output twice, once at each
  width.
- **[007](backlog/backlog-2026-09-15-007.md)** Open questions from the child-runner spike. Decisions, not
  work.
- **[008](backlog/backlog-2026-09-15-008.md)** The resume refusal is written in metaphor and nobody knows
  what it means.
- **[009](backlog/backlog-2026-09-15-009.md)** "what did it do?" answers a different question from the one it
  asks.
- **[010](backlog/backlog-2026-09-16-010.md)** Keep an idle session's prompt cache warm.
- **[011](backlog/backlog-2026-09-16-011.md)** What a turn actually cost, and what a subagent cost.
- **[012](backlog/backlog-2026-09-16-012.md)** Reach a machine that is not on an overlay.
- **[013](backlog/backlog-2026-09-16-013.md)** A runner in a container.
- **[014](backlog/backlog-2026-09-16-014.md)** Never type into a terminal that has a dialog on it.
- **[015](backlog/backlog-2026-09-16-015.md)** Build a terminal pane per card, kept alive, capped by a
  setting. Largely replaces 006, and needs 016 first.
- **[016](backlog/backlog-2026-09-16-016.md)** A hidden viewer must not decide the terminal's size.
  Prerequisite for 015.

**001 is the big one** and the only one with a plan: `backlog/backlog-2026-09-13-001-PLAN.md`, 839 lines over
thirteen sections, and it deletes the rb3.1 attempt it replaces.

Two of the rest are not the day of work the others are. **006** recommends re-replaying scrollback at the new
width and opens with two questions that have to be answered before anything is changed, chiefly whether xterm
is producing the ragged overflow rather than the replay. **007** is decisions, and says so: nothing should be
built against the spike until they are answered.

---

## Deep backlog

Wanted, and not until everything above is done. An entry here is parked on purpose rather than forgotten.

- **A button on the terminal that spins up an agent for what you are looking at.** The mechanism exists:
  `POST /v1/launch` takes a directory, a prompt, tags and a window, so a button is a form over it. What is not
  settled is whether it is a good idea. A control that starts a claude session is one mis-click from a process
  nobody meant to start, in a directory somebody else is working in, and the terminal is the surface where
  mis-clicks happen most. Revisit once launching from the board has been lived with.


## Out of scope, deliberately

Unchanged, except where an entry above says otherwise.

- **Injecting prompts into a running claude session from outside MCP.** There is no IPC channel into a claude
  process. This holds for the peer bus too: one session messaging another is QUEUED and delivered by a hook,
  never typed into the target's terminal, even where atrium owns one and could.
- **Replacing `gwt sessions`.** Mode B is a cross-session aggregator and `gwt sessions` remains the per-repo
  workflow tool.
- **Persistence in the hub.** The hub is intentionally amnesiac. The daemon reverses this on purpose, which is
  the entire point of v2.
- **Storing what a runner is doing right now.** It would be a lie the moment the daemon restarted.
- **Federating a pseudo terminal.** Not a policy, a fact: the daemon owns each pty, closing one takes the
  process with it, and ConPTY has no reattach.
- **Authentication on loopback.** The published board can now require a sign-in. The local one cannot and must
  not: every hook, the CLI and the MCP server talk to it, and none of them was ever going to carry a
  credential.

---

## Editing this file

**Neither numbering may be renumbered.** Sessions are handed work by number and a preview port is derived from
it, so moving a number puts two sessions on one port and neither finds out until something fails to bind.

That has a consequence worth knowing before it bites: theme `1` and `backlog-2026-09-13-001` both read as
"one". Anything deriving a port from a bare number has to be told WHICH list. Say `theme 1` or `item 001`,
never `1`.

**Many sessions share this file and none of them sees the others.** Work happens in a worktree per session off
one commit, so this is the document everybody has a reason to touch and nobody sees the conflict until merge.
Adding a dated item is cheap, because it is a new file plus one row in the table. Rewriting a theme is not.

**Finished work leaves, it is not struck through.** `CHANGELOG.md` is what landed and `docs/status.md` is where
each thing stands.
