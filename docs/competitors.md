# Competitors: the field around atrium, and whether atrium can be extended

Standing reference, and an honest partial one. Only two tools were read at source depth: bb (this file's subject, in
depth in `docs/bb.md`) and Charon (`docs/charon.md`). The rest of the survey is not done and section 7 says what is
left. This file exists so the extensibility question clint asked has an answer now, and so the next pass starts from
a list and not from a blank page.

Written 2026-09-29. Nothing was executed. Where a claim rests on a README or a design note and not on traced code, it
says so.

## 1. How the field was picked, and what that produced

The brief asked for the popular boards, supervisors and orchestrators for coding agents, ranked by adoption. What was
actually done:

- bb was found from the brief and read fully (`docs/bb.md`).
- Charon was already covered (`docs/charon.md`).
- Three `gh search repos` queries were tried for the rest, and they were **not useful**. Two returned nothing and one
  returned mostly unrelated prompt packs (a family of `*-claude` repositories from one author) and a handful of
  sub-1,000-star projects (`SethGammon/Citadel` at 923 stars, `agent-era/devteam` at 265). None of those is the
  well known tool this survey is meant to find, and none was read. Search by keyword on GitHub ranks the packs above
  the products, so the method itself was wrong.

The next pass should pick candidates from named sources (the awesome lists for Claude Code, the Claude Code and Codex
docs' own mentions of third party tools, and a search on topic tags with a star floor) and only then read the top of
the list. No star counts are quoted below for tools that were not looked up.

| Tool | Stars (2026-09-29) | Read at source depth | Where |
| --- | --- | --- | --- |
| bb (`get-bb/bb`) | about 3,990, 565 forks | Yes, static | `docs/bb.md` |
| Charon (`Lomchat/charon`) | not looked up | Yes, static, 2026-09-03 | `docs/charon.md` |
| Everything else | not looked up | No | Section 7 |

## 2. bb in one paragraph, and how it differs

A TypeScript monorepo that is the *client* of the Claude Agent SDK, Codex's app server and ACP agents, with a SQLite
server, host daemons on each machine and a web, desktop and phone front end. Its distinguishing idea is that
nearly everything, including the providers themselves, is a plugin on a public API, and it ships thirty-nine of them.
Full reading in `docs/bb.md`. The one line comparison:

| | atrium | bb |
| --- | --- | --- |
| Relationship to the agent | Supervises the real interactive `claude`, owns its terminal | Is the SDK client, no terminal |
| Extension runs | Out of process, as a bounded command | In the server process, as trusted code |
| Extension is shared by | Nothing yet, by hand | A marketplace manifest, npm or git, with semver over tags |
| Human at a keyboard in the session | The design centre | Not a case |
| Permission memory | Durable standing rules, most specific wins | Session grants, in memory as read |
| Platform | Windows native first, one Go binary | macOS and Linux, Windows only in WSL2 |

## 3. Extensibility: is atrium in a good position?

The question, as clint put it: "atrium would be powerful if people are able to customize it to do things they want,
like the review panel and the director of review. Are we in a good position to support that sort of extension?"

**Short answer: half. Atrium has a good set of small extension points and no way to package, name, share or trust a
set of them. A user can already build a director by hand, and cannot yet hand it to anyone else.**

### 3.1 What atrium has today

Taken from `CLAUDE.md` (the repo layout and the subcommand table), `runtime/DIRECTOR.md` and the design docs named
there. Not re-traced through the code for this pass, so read each as "documented", and re-verify before building on
one.

| Extension point | What a user can put in it | Where it lives |
| --- | --- | --- |
| **Runners / harness rows** | Which program a card launches, with its arguments and scoping | `internal/store/harness.go`, `internal/daemon/launch.go`, `docs/runner-scoping-design.md`, `docs/other-runners.md` |
| **Sources** | A command on a timer that finds work and offers it to the inbox, bounded and reported on its own row | `internal/store/sources.go`, `internal/daemon/sources.go` |
| **Actions** | A named stored prompt the operator wrote, said to a card | `internal/store/actions.go`, `internal/daemon/actions.go` |
| **Fixtures** | Terminals that come up with the daemon | `internal/store/fixtures.go`, `internal/daemon/fixtures.go` |
| **Hooks** | Which Claude hooks are wired, and writing the missing ones | `internal/api/hooks.go`, `internal/claudeconf/hooks.go`, `docs/hooks.md` |
| **The atrium-control MCP tools** | `atrium_launch`, `atrium_say`, `atrium_peers`, `atrium_report`, `atrium_task`, `atrium_cull` and the rest, so an agent can drive the board | `atrium control` |
| **Peer bus** | `atrium peers` and `atrium tell`, queued and never typed | `internal/daemon/peers.go` |
| **Tags and groups** | Free labels on a card that a person or a director can act on (`dept:`, `atrium:subagent`) | task attributes |
| **Launch briefs and lean launches** | The prompt and the skill set a worker starts with | `docs/launch-options-design.md`, `docs/lean-workers-design.md` |
| **Overlays** | How the board is reached from elsewhere | `docs/overlays.md`, `internal/daemon/overlay*.go` |
| **Permission rules and auto mode** | Standing rules, import and export, a review of what auto approved | `internal/store/rules.go`, `docs/auto-mode.md` |
| **Outbound configuration** | Atrium's own settings as files a repository holds, with a per-field rule for what may leave the machine | `docs/scm-design.md`. Designed, "nothing here is built" |
| **The director pattern** | A resident orchestrator with a written brief that dispatches workers, reviews, merges and reports | `runtime/DIRECTOR.md` and the orchestrator's own brief |

Skins were named in the brief. They were not found as an extension point in `CLAUDE.md` or in the doc list, and are not
counted here until someone shows where they live.

The design principle underneath all of it is a good one and worth stating because it is the opposite of bb's:
**nothing a user writes shares an address space with the permission gate.** A source is a command with bounded output.
A hook posts and forgets. An MCP tool goes through the same HTTP surface a human would. A failing extension parks or
switches itself off with the reason on its row (`CLAUDE.md`, resilience 6). That is a safer footing for an extension
story than bb's, and it is a real advantage.

The director itself is the telling example. `runtime/DIRECTOR.md` is a page of prose: how a worker's worktree is made,
which model and tags to launch with, a cap of two workers, what every brief must say, how to merge, whom to tell, what
never to do. It is the whole of "a director of review" as it exists, and it is a *convention held in a file plus a
script* (`scripts/new-worktree.ps1`) plus tags, launched by another agent. It works. It is also not something a user
can define in the board, choose from a list, or receive from anyone else.

### 3.2 What atrium lacks

Every item is a gap between "a user can do this by hand" and "a user can define and share this".

1. **A named, versioned bundle.** Nothing says "these harness rows, this source, these actions, this brief, these
   tags are one thing called `review-panel` at version 1.2". Today they are rows in a database and files in a
   worktree with no common name. Without a bundle there is nothing to share, update, or remove cleanly.
2. **A role as data.** A director is a brief plus a set of tags plus a worker limit plus a merge rule plus a report
   rule. Only the brief is a file, and only by convention. No place holds "the role called review-director" for the
   board to offer at launch.
3. **An admission point a user can own.** "Do not start a fourth worker", "hold anything tagged `dept:review` until
   the panel has three verdicts", "reject a launch without a brief". Today these live in a director's prose and rely
   on the director obeying it. The permission chain is the only checkpoint and it is about tools, not work.
4. **A way to be told when things happen.** A director polls `atrium_peers` and waits for reports. bb has events
   (`thread.idle`, `thread.failed`, `interaction.pending`, `message.queued`) a plugin can subscribe to. Atrium has the
   `event` table and the SSE stream, but nothing lets a user's command be *called* on an event.
5. **A trust story for someone else's extension.** The outbound half of `docs/scm-design.md` designs a per-field rule
   for what may leave the machine and says nothing is built. There is no equivalent for what may *come in*: what
   a shared bundle is allowed to add to your permission rules, to your hooks, or to a runner's arguments.
6. **A place to test one.** bb ships a fake host. Atrium has none, so an extension author can only run against a live
   board.
7. **A source of extensions.** No index, no install command, no version resolution.

### 3.3 How the tools that were read compare

| | bb | Charon | atrium |
| --- | --- | --- | --- |
| Unit of extension | Plugin: a directory with a `package.json` `bb` key and a `server.ts` | None found. Peer MCP server and skills are the only user-shaped surface | Loose rows and files |
| API breadth | Settings, storage, HTTP, RPC, CLI, agent tools, providers, UI slots, events, a hook, machines, AI services | Not extensible | Commands, hooks, MCP tools, rules |
| A veto point on work | `message.dispatch`, fail closed, ten second box | None found | The permission chain, tools only |
| Roles / templates | Task presets (provider, model, reasoning, permission mode, instructions) and workflow scripts | Not found | Director brief by convention |
| Sharing | Marketplace manifest, npm or git, semver over tags, moved-tag refusal (a plan, partly live) | None | None |
| Trust | Reviewed at listing, then trusted, in process | Single user, single password | Out of process by construction |
| Test kit | Fake host, published | 39 Python test files, internal | None for extensions |

The row that matters most is the veto point. It is what would let a user *enforce* a director's rules and not merely
write them down, and bb's fencing of it (a question and not an event, fail closed, time boxed, human override) is the
reusable part.

### 3.4 Recommendation: the smallest next step

**Do not build a plugin API.** bb's is 5,500 lines of contract for the backend and frontend together, still marked
experimental in most of its interesting members, and its cost is mostly the in-process trust that atrium has ruled out.

**Build a pack, and only a pack, first.**

A pack is a directory with one manifest file, `atrium-pack.json`, that names things atrium already has:

```
review-panel/
  atrium-pack.json      name, version, description, requires.atrium, and the parts below
  brief.md              the role's launch brief (the DIRECTOR.md shape, per role)
  actions/              named prompts, one file each
  sources/              source definitions: command, interval, bounds
  runners/              harness rows
  tags.json             tags the role uses and what each means
  limits.json           max concurrent workers, who reports to whom
```

`atrium pack install <path-or-git-url>[@ref]` reads the manifest and **shows a diff of what it would add**: rows, rules,
hooks, arguments. Nothing is applied until a human accepts it on the board. Every row a pack creates carries the pack's
name and version as its origin, so removal is exact and an operator's own edits are never overwritten (the "observed
versus overrides" idea in `CLAUDE.md`, applied to extensions). A pack may not add a standing permission rule, register
a hook, or widen a runner's arguments without an explicit line in the diff saying so, and the board renders those
lines differently.

Why this and not something else:

- It adds **no new execution path.** A pack contains data that atrium's existing bounded machinery already runs.
- It makes "a director of X" a thing a user defines by writing a brief and a tag list, which is what a director is.
- It carries the moved-tag lesson from bb's plan for free: install records the resolved commit, and an update that
  finds a different commit behind the same ref refuses.
- It gives the trust question somewhere to live, which item 5 in the gaps needs before sharing is safe.

**Second, and only after packs exist: a gate.** The `message.dispatch` idea, as an out-of-process command with the same
bounds and switch-off rule as a source (`docs/bb.md` section 5, item 1). That is what turns a director's written
limits into enforced ones.

**Third, if asked for: an event-called command.** A source is a command on a timer. The same row with a trigger on an
event instead of an interval reuses the whole runner, and answers gap 4 without a plugin API.

Cost, roughly: the pack manifest, loader and diff is a week of careful work, most of it in the diff and the
provenance column, and the schema change goes at the end of the migration slice. The gate is about the same again.
These are guesses from the shape of the existing code, not estimates from tracing it.

## 4. Ideas worth stealing, ranked

Across everything read (bb, plus Charon's list in `docs/charon.md` section 5, which is not repeated). Cost is a guess
from the shape of the code and is labelled as one.

| Rank | Idea | From | Cost | What it needs |
| --- | --- | --- | --- | --- |
| 1 | **Packs**: a named, versioned directory of atrium's existing rows, with a diff-before-apply install and provenance on every row | bb's manifest, collection manifest and moved-tag refusal | Medium | A manifest, a loader, an origin column at the END of the migration slice, a diff view on the board |
| 2 | **A dispatch gate**: an out-of-process command asked "proceed, wait or reject" before a launch, prompt or peer message, fail closed | bb `message.dispatch` | Medium | A gate table shaped like sources, a runner beside `sources.go`, a human bypass, three-strikes switch-off |
| 3 | **Per-context brief selection**: choose the launch brief and skill set by tag and runner, and quote card attributes as untrusted | bb `agents.configure` | Small | Brief templates keyed on tags. Check `launch-options-design.md` first |
| 4 | **A permission ceiling per host or room**: a maximum that clamps auto mode and rule application, settable only from the board | bb `maxPermissionMode` | Small | One settings value and one check at step 5 of the chain. Does not reorder the chain |
| 5 | **Batched child outcomes to a parent or director**, with a truncation marker and "this is not the final result" guidance | bb `child-thread-notifications.ts` | Small | A 2 second batch in the message queue, keyed by the receiving card |
| 6 | **A fake daemon for extension authors** | bb `plugin-sdk/testing` | Medium | Nothing until packs exist. Do not start it earlier |
| 7 | **Orchestration patterns as text**: adversarial verify, judge panel, loop-until-dry, completeness critic, "no silent caps" | bb `workflows` `orchestration.md` | None | Copy into the review director's brief and the panel's instructions |
| 8 | **Archive with an undo grace** | bb `ARCHIVE_UNDO_GRACE_MS` | Small | Check `sweep.go`, which already separates archiving from deleting |

Explicitly refused: an in-process plugin runtime, a JavaScript runtime for scripted orchestration, and a cloud
pairing service. Reasons in `docs/bb.md` section 6.

## 5. Where atrium is ahead

Against bb, in `docs/bb.md` section 4. Against Charon, in `docs/charon.md` section 6. The two shared points are the
strongest: durable standing rules with a decision log, and a real terminal with a human in it.

## 6. What the two agree on

Both bb and Charon drive the Claude Agent SDK in-process and both built a permission surface on top of `canUseTool`.
Neither has a matchable durable rule store in the provider path, and both document, in their own words, that an
approval card is not a security boundary. Two independent projects arriving at SDK-client architecture, and neither at
supervising a terminal, is evidence that atrium's shape is the uncommon one. That is either a gap in the market or a
reason nobody else wants it. The question is worth putting to clint and not deciding here.

## 7. Not covered yet

- **The rest of the field.** No other tool was read. Candidates to look up by name, none verified for adoption or
  behaviour: Claude Code's own subagent and team features, Codex's app server and cloud surface, and the various
  worktree-per-agent managers and Kanban-style boards. The candidate list should come from curated lists and topic
  tags with a star floor, not from keyword search (section 1).
- **Per tool: overlap, stealable ideas, where atrium is ahead.** Done for bb and Charon only.
- **Skins.** Named in the brief and not located as an atrium extension point.
- **Atrium's extension points were taken from `CLAUDE.md` and the design docs, not re-traced in code.** In particular:
  how launch briefs are stored, whether tags can drive behaviour, and what the atrium-control tools can and cannot
  change. Section 3.1 should be re-verified before anything is built on it.
- **bb's items in `docs/bb.md` section 7.** Workflow replay, `concurrency-limit` counting, safe mode, whether session
  grants persist, and the non-Claude bridges.
- **Nothing was executed.** Neither bb nor any other tool was run.
