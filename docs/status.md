# Where everything stands, 2026-09-05

Every item in `docs/backlog.md`, plus what shipped over the last two nights, in one list with one status each.

`docs/backlog.md` says WHY for each of these. This says WHERE IT IS. When they disagree, the backlog is right
about the reasoning and this is right about the state.

## The statuses

| | Meaning |
| --- | --- |
| **done** | built, and the operator has looked at it and been happy |
| **needs you** | built and tested, never seen by the operator. This is the pile that matters |
| **designed** | written up and reviewed, no code |
| **decided** | a rule was settled and enforced, with nothing left to build |
| **open** | discussed, understood, nobody has started |
| **named** | mentioned once, never specified. Not a plan |
| **refused** | deliberately not done, with the reason recorded |
| **blocked** | waiting on something outside this repo |

---

## Done, and you have signed off

| Item | Note |
| --- | --- |
| Runners page as five panes | the spine down the left |
| A shell beside a wedged agent | `agent \| shell`, two terminals per card |
| The skin panel | draggable, remembers where you put it, use it / default / cancel |
| Twenty board skins | eleven dark, five lighter dark, four light |
| A light board | four light skins. `--lift` and `--hairline` are part of a skin now |
| Bracketed paste | multi-line paste arrives as one paste, not fifteen Enters |
| Session list collapse and expand | a chevron pair, only the direction that goes anywhere |
| Header density and alignment | the header answers to `tighter` now |
| One keystroke listener per terminal | every reconnect used to stack another one |
| The wind-down announcement | your design: the daemon says it is going before it goes |

## Done, needs you

**This is the pile that matters.** All of it is built and tested and none of it has been seen.

| Item | What to do |
| --- | --- |
| **A share cannot reach a shell** | the only security claim here. `share this session`, then try `?kind=shell` from its dev tools |
| **Priority on a card** | right-click, `how much it matters`. Nothing reorders, by decision |
| **Note chip** | type in `notes to self`, close without sending, look at the card |
| **Event log answers with the newest** | open a long-running card's timeline |
| **Directory picker bounded to roots** | the one change that took a capability away. Press browse |
| **Hook identity keyed on the running daemon** | the hooks board should read `wired` and stay that way through a build |
| **`expose the board`** | renamed, and it pairs with `share this session` |
| **Reserve and share in one press** | must still ask the public-share confirmation first |
| **A share that dies alerts** | hard to stage: take the network away while one is up |
| **Grouping expression refused daemon-side** | `curl` a `group_by` at `/v1/settings`, expect 400 with a reason |
| **Scroll to bottom on paste** | staged, not yet restarted |
| **A session that ended stops being retried** | type `exit` in an agent, watch the console stay quiet |
| **`atrium version`** | I ran it, you have not |
| **`make release`** | five platforms build, Linux binary is a static ELF. I could not execute it |
| **Settings dialog pinned to the top** | stops moving when panes change height |
| **Pop-out chip sized from text** | board card and stack row |

## Designed, not built

| Item | Where |
| --- | --- |
| Starting a card from a ticket, an issue or a PR | `docs/scm-design.md`, reviewed twice |
| Atrium's settings as files any SCM can hold | same document. Per-field export policy is the part that makes it possible |
| The forum: one board, many machines | `docs/federation-design-v2.md`, `docs/forum-implementation.md` |
| Multi-tenant atrium | `docs/backlog.md`. Costed, and deliberately a different product |

## Decided, nothing left to build

| Item | The rule |
| --- | --- |
| The grouping expression | an expression may be stored where it was typed. Refused at the endpoint, three tests |
| CodeMirror in the board | refused: ES modules across seven packages, no bundle, and the board is one file |
| `atrium install` | removed before shipping. Copying a file is the shallow half of installing |
| Priority is not `pinned` | pinned is "always show me this". Bolting an order onto it loses the fixture case |
| A shell is not a runner | second map on the supervisor, so nothing that means the runner sees it |

## Open: understood, nobody has started

| Item | Size | Why it is not done |
| --- | --- | --- |
| Hooks for runners that are not Claude Code | two days | needs a runner that is not Claude Code in front of it |
| Edit and import TERMINAL themes | a day | not the same thing as the board's skins |
| A focused window should not toast its own session | half day | `soloAlert` takes the wrong branch for a defensible reason |
| Approvals from a phone | a day | deprioritized: auto mode makes it moot |
| A folder rule reads text, not what a shell would make of it | unknown | expanding shell syntax is its own project |
| Stage 5: the TUI through the HTTP API | unknown | the stage that would prove the API is complete |
| Postgres | unknown | schema written for it, nothing has ever run against it |
| A card in `needs-input` with no pid and no pending request | unknown | would need a session heartbeat: a cost on every session for a rare case |
| Working directories from a repo URL | unknown | overlaps with `gwt` |
| Status inference for runners that cannot speak | unknown | heuristic, and the backlog is wary of it |
| A runner atrium can ask for help | unknown | |
| Repo metadata on GitHub | minutes | `gh repo edit` returns 403 with the current token |

## zrok and OpenZiti: less progress than it looks

Worth calling out on its own. The overlay panel got its interface work, and the underlying capability did not
move much.

| Item | Status |
| --- | --- |
| Sharing the board over zrok or ziti | **done**, both are embedded SDKs |
| Sharing one session over zrok | **needs you** |
| The panel as a switch, not a config screen | **needs you** |
| Reserve a name and share in one press | **needs you** |
| Alert when a share dies | **needs you** |
| Zrok limits legible BEFORE you press a failing button | **refused once.** Needs another API call and a shape nothing else models |
| `zrok enable` still shells out | **open.** The only executable left. Nobody has checked for an SDK equivalent |
| Enabling against a non-default instance | **open.** Works, hard to find, and a wrong endpoint gives a token error that does not name it |
| OpenZiti service, config and policy creation | **refused.** A network somebody administers is not one a board should edit |
| Enrolling a ziti identity with OIDC | **open** |
| NetFoundry as a third overlay | **named.** Not specified. `OVERLAY_UI` makes it cheap when it is |

## Named, never specified

Mentioned once. Not plans.

| Item |
| --- |
| NetFoundry front door as a third overlay |
| Governed calls from sterling |
| A "broadcast" command: one prompt to N agents |
| Per-agent transcript on disk |
| WebSocket transport for the agent listener |
| A model reading the auto-mode review and summarising what changed |
| The board as the React app the decisions table names |

## Blocked on something outside this repo

| Item | On what |
| --- | --- |
| Codex and ollama enabled by default | they ship off on purpose. Discovery reports what is on PATH |
| Rule import for anything but Claude Code | no other harness has a documented permission config |
| More than two notification buttons | Chrome on Windows renders two |
| Packaging: signing, accounts, moderation, publishing | certificates and accounts that belong to a person |

## Refused, with the reason recorded

| Item | Why |
| --- | --- |
| Authentication | single machine, loopback. Reaching it from elsewhere is an overlay's job |
| Multi tenancy and accounts | a different product, not a flag |
| Adopting sessions from the gwt ledger | tried and removed: it filled the board with sessions atrium could watch and never talk to |
| Replacing the runner | claude-code owns the tool loop, context and credentials |
| Attaching to a session atrium did not start | a console cannot be handed over after the fact |
| Injecting prompts from outside MCP | the agent loop IS the IPC |
| Storing what a runner is doing right now | it would be a lie the moment the daemon restarted |
| Atrium holding somebody else's credential | it may hold the NAME of a command that has one |

## Known gaps, stated rather than fixed

| Gap | Note |
| --- | --- |
| Supervised runners die with the daemon | ConPTY offers no reattach. Resume ids are the answer |
| A share widens what loopback means | the shutdown endpoint notices. Anything else deciding by address has the same problem |
| `wire_name` collisions solved but not enforced | nothing makes a satellite call `atrium name`. The forum handshake is the place |
| Postgres portability asserted, not tested | a CI job would settle it |
| The whole test plan is manual | by nature: it is the part a test cannot reach |
