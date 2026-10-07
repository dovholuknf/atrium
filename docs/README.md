# docs

**If you are an agent, read `docs/agents.md` first.** It is the bring-up: what atrium is now, what runs where, how to
build and test, the house rules, and which of the docs below to trust.

The published manual is the site, `website/docs/`, at `https://dovholuknf.github.io/atrium/`. This folder is for the
people and agents who work on atrium: how it is built, what was decided, and the designs behind it.

## Reading order

A new human: `README.md`, then the site: what atrium is, install, quick start, rooms and the hub, how it works.
`FEATURES.md` for everything else it does.

A new agent: `docs/agents.md`, `docs/how-atrium-works.md`, `docs/fabric/hub-and-rooms.md`,
`docs/runtime/agent-messaging.md`, then the `AGENTS.md` of the package being changed.

## The map

| Doc | For | What it answers |
| --- | --- | --- |
| `agents.md` | an agent starting work on atrium | the bring-up, and which docs to trust |
| `how-atrium-works.md` | a contributor or an agent | the one architecture page: processes, ports, cards, hooks, the permission chain, state |
| `fabric/hub-and-rooms.md` | a contributor or an agent | the hub, rooms, joining, ports, cross-room addressing, as built |
| `story.md` | anyone | how atrium got here, each step with what broke and the archived doc behind it |
| `runtime/wiring-a-runner.md` | an agent wiring up a new runner | the hook contract, measured |
| `user-guide.md` | a human using atrium | the working patterns, until they move to the site |
| `room-accounts.md` | an operator | running a room as its own account, and what atrium warns about |
| `decisions-log.md` | anyone about to re-argue something | clint's decisions and why |
| `fabric/hub-decisions.md` | the same, for the hub | the hub and room decisions, 1 to 19 |
| `test-plan.md`, `changes/` | the merger | the test plan, and the sections waiting to be folded into it |
| `backlog/<dept>/` | the directors, and `atrium backlog import` | one file per backlog item. The live backlog is on the hub |
| `archive/` | anyone curious | designs a later doc replaced, each with a `Status: superseded by` line |
| `blog/` | the blog | post ideas and outlines, nothing published |
| `backlog.md`, `backlog-2.md` | nobody new | the old backlog, frozen |

## The folders

| Where | What | Owner |
| --- | --- | --- |
| `runtime/` | the room's daemon, the store, hooks, messaging, launching, keep-alive, intake | @runtime |
| `terminal/` | the pseudo terminal, the pty host, attach, input, resize | @terminal |
| `ui/` | the board | @ui |
| `fabric/` | the hub, rooms, overlays, git between rooms, reaching atrium from elsewhere | @fabric |
| `review/` | code review, its director and its memory | @review |
| `release/` | packaging, landing work | @merge |
| `orchestrator/` | the orchestrator's boot and wrap-up checklists | @orchestrator |
| `rnd/` | designs, spikes, research and reads of other tools | @rnd |
| `archive/` | superseded designs | nobody, they do not change |

## The status line

Every design, spike, plan and research doc has one line after its title that says whether to trust the body:

- `Status: built. <where in the code>. <what differs from this design, if anything>.`
- `Status: partly built. <what is, what is not>.`
- `Status: proposed, not built.`
- `Status: superseded by <doc>. Kept as the record of <what>.`
- `Status: research, <date>. Its conclusion: <one line>.`

Whoever builds or replaces a design updates its line in the same commit. A line that says built is checked against the
code, so it names where the code is.

## Where a new doc goes

A design that is not built yet goes in `rnd/`, whoever writes it, with `Status: proposed, not built.` When it is
built, the director of the area that owns the code moves it into that area's folder with `git mv`, updates its status
line, and fixes every path that names it (`git grep docs/rnd/<name>`). A reference for code that exists goes straight
into its area. A design that a later doc replaces moves to `archive/` with a `superseded by` line.

`test-plan.md` and `changes/` stay at the top because every brief, `DIRECTOR.md` and `CLAUDE.md` name them there, and
`scripts/fold-changes.ps1` reads them there.
