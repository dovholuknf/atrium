# Paste a link, get a card that owns and frees everything it made (r-paste-a-link-card-lifecycle)

Status: design by r-paste-a-link-card-lifecycle, 2026-10-07. Nothing here is built yet. clint's answers are recorded
in "Interview" as they come, and they overrule the body where the two disagree.

clint, 2026-10-07: "i want to paste a link for discourse, for zendesk, for bitbucket and github and i want a card to
materialize. i want it to clone the repo if it needs to from the hub and the hub to clone if it needs, then i want the
card to make a worktree for that specific task. i want to work with the card until completion and when the card goes
away all the resources go away too. it should have its own environments, own ziti overlay, own ziti edge tunnels, it
should maintain its own inventory etc."

The goal is to replace gwt for review and support work. gwt wins today because it works from any terminal, and it
works. Every paste on the hub's pulls tab fails today.

Read for this design: `docs/rnd/pr-review-workflow.md`, `docs/rnd/scm-forge-design.md`, `docs/rnd/hub-forge-answers.md`,
`docs/rnd/pulls-view-design.md`, `docs/rnd/pulls-api.md`, `docs/runtime/intake-design.md`, `docs/runtime/scm-design.md`,
`docs/runtime/worktree-gone-design.md`, `docs/rnd/process-registry-design.md`, `docs/backlog/runtime/r-006.md`,
`docs/fabric/overlays.md`, `docs/blog/inventory.md`, and gwt (`git-worktree.ps1`: `pr`, a bare URL, `discourse`,
`zendesk`, `prune -Recapped`).

## 0. The design in one paragraph

A pasted link goes to **one verb on the hub**, `POST /_hub/open {url, why?}`. Every door calls it: the pulls tab, a
Ctrl+Alt+R prompt anywhere on the board, the launch dialog, the phone and `atrium open <url>` in any terminal. The hub
**recognises** the link against a table it owns, turns it into a **canonical key**, and looks the key up in a claim
table. A live claim answers the card that already holds the link, and the board attaches it. Otherwise the hub places
the link on the least busy room, holds the repo in its store (cloning from the forge if it must), and asks the room to
**materialise** the card: a worktree for this task alone, the PR review row when the link is a PR, and the card
started in the worktree as the row's walker. Every resource the room makes for the card is written to the card's
**inventory** as it is made. **Finish** removes exactly what the inventory lists, in reverse, and keeps the history
(findings, `walk.txt`, recap, transcript). A **sweep** on room start reconciles the inventory with the disk and the
process table, so a crash leaves nothing behind that nobody owns.

## 1. What exists, and what is missing

| step | exists | missing |
| --- | --- | --- |
| recognise a link | per-room `recognisers` table, `scripts/recognisers/*.json` loaded BY HAND with `load.ps1` | **the hub has none and only one room was loaded** (section 2) |
| one door | `POST /v1/prs` makes a row. The launch dialog chains four calls in browser JS (`fixtures.js:1531`) | **a server-side verb that does the whole chain** (section 3) |
| the claim | `pr_claim` on the hub, keyed `host/org/repo#N`, placement on the least busy room | **a claim for a link that is not a PR**, and the claim naming the CARD, not only the room |
| repo on the room | the hub holds and fetches (`/_forge/pr`, `Store.Hold`, `FetchPR`), the room clones through `HubLoopback` | nothing for a PR. A support link names no repo (section 5) |
| worktree | `POST /v1/providers/{name}/pr-worktree`, idempotent, `pr-<N>` for a fork | needs a provider row covering the host, else nothing is made |
| review | the PR runner, recipe, findings, walk drawer, move | nothing |
| a hotkey | Ctrl+Alt+N exists | **Ctrl+Alt+R** (u-review-pr-hotkey) |
| any terminal | `atrium open <url>` fills a dialog | **it should call the verb and print the card** |
| inventory | nothing. A card records its cwd only | **the whole thing** (section 6) |
| finish | `POST /v1/prs/{id}/archive` keeps the folder on purpose | **finish** (r-finish-pr-review, section 7) |
| crash sweep | worktree-gone asks a runner to leave when its tree vanishes | **the reverse: a tree whose card is gone** (section 8) |
| processes, ports | designed in process-registry-design, not built | built as part of the inventory (section 9) |
| own ziti overlay | atrium drives a share and never issues an identity | **a per-card quickstart overlay as inventory** (section 10) |

### 1.1 Why every paste on the hub fails today (u-pulls-no-recogniser-on-hub)

Confirmed on the live hub on 2026-10-07. `GET /v1/recognisers` on `:7778` merges every room, and all ten rows carry
`room: claude-sg4`. m1mini, sg3 and sg4-control have none. The hub has four rooms, so `placeNewPR`
(`internal/link/prclaim.go:174`) sends `POST /v1/prs` to the least busy one, which is almost never claude-sg4. That room
runs `d.Recognise` against its own empty table and answers `no_recogniser`. The backlog item's guess, that the HUB's
table is empty, is right too, and it matters for `POST /v1/recognise` (the launch dialog), which answers "pick a room
first" in the all view. The cause underneath both is the same: a recogniser is a per-room row that somebody loads by
hand.

## 2. Recognisers live on the hub

A link is recognised before anyone knows which room it goes to, so the table belongs to the one place that is always
asked first. This follows `cardcolors` (5815f8a1): the hub owns the rows, seeds them once from the embedded
`scripts/recognisers/*.json`, and serves them.

- **The hub's store** gets the `recognisers` table. On first start it is seeded from the JSON files, which are
  embedded in the binary. A seeded row that the operator edited is never overwritten. A new seed row in a later build
  is added once.
- **`POST /_hub/recognise {url}`** answers the resolution (`store.Resolved`) without a room. The `forge` fetch runs on
  the hub, where the forge already is.
- **A room with a link** answers `Recognise` by asking the hub, the way `Room.Forge` asks the hub for the forge. A room
  with no link keeps its own table, as a room with no hub keeps its own forge.
- **The board's recogniser settings** read and write the hub's rows in the all view, and the room's rows only on a
  room with no hub.
- **The room tables** are left in place and stop being read on a linked room. A later migration drops them.

Test: a hub whose rooms have no recogniser rows accepts `https://github.com/openziti/ziti-console/pull/967`, with and
without `/changes` and `/files`, and the row lands on the least busy room.

## 3. One verb: open a link

`POST /_hub/open {url, why?, room?}` on the hub, and `POST /v1/open` on a room with no hub. The answer is the card:

```json
{"key": "github.com/openziti/ziti-console#967", "kind": "github-pr", "room": "sg3", "card": "sg3~01a1...",
 "created": true, "worktree": "D:/worktrees/github/openziti/ziti-console/fix-x", "pr": "sg3~pr_42"}
```

1. **Recognise** on the hub (section 2). No match is `422 no_recogniser` with the sentence the board shows today.
2. **Key** from the captures, per kind: `host/org/repo#N` for a PR or issue (the PR key is unchanged),
   `zendesk:<host>/N`, `discourse:<host>/N`.
3. **Claim.** `pr_claim` grows into `link_claim {key, room, card, kind, state}`. A claim whose card is live answers that
   card with `created: false`, and the board attaches it. A second paste never makes a second card. A claim whose card
   is finished is the reopen case (Interview).
4. **Place** on the least busy room, as today, or on `room` when the caller named one.
5. **Materialise** on the room: `POST /v1/open/materialise {key, kind, captures, url, why}` does, in order, writing each
   to the inventory as it goes:
   1. the repo: the scm clone path through the hub when the room has no checkout. The hub clones from the forge when
      its store lacks the repo (`Store.Hold`). A private repo the hub cannot read fails with the hub's sentence.
   2. the worktree: the PR form of the worktree verb for a PR, the branch form (`issue-<N>`, `zendesk-<N>`,
      `discourse-<N>`, as gwt names them) for the rest. **No provider row is needed**: a host with no provider uses
      the room's worktree root and the `<host>/<org>/<repo>/<branch>` layout.
   3. the review row, for a PR: `POST /v1/prs`, as today.
   4. the card: launched in the worktree with the recogniser's prompt and tags, `link:<key>`, and `lifetime: link`.
   5. the walker, for a PR: `POST /v1/prs/{id}/walker {"action":"set"}`.
   A step that fails stops the chain and removes what the earlier steps made, using the inventory, then answers the
   failing step's sentence. Nothing half made is left behind.
6. **Record** the card on the claim and answer.

The browser chain in `fixtures.js` (`makePastedWorktree`, `startPastedReview`, `setPastedWalker`) is replaced by one
call. The pulls tab's paste, which today makes a row and nothing else, calls the same verb.

**The doors.** The pulls tab paste field. Ctrl+Alt+R anywhere on the board, prefilled from the clipboard. The launch
dialog when a pasted link is recognised (it shows what the verb will do and its start button calls it). `/m` on the
phone, one field. `atrium open <url>` in any terminal, which prints the card's board address and exits, with
`--attach` to attach in the terminal. An MCP tool, `atrium_open`, for an agent.

## 4. The kinds

| kind | key | repo | worktree branch | review | prompt |
| --- | --- | --- | --- | --- | --- |
| GitHub PR | `github.com/o/r#N` | from the URL | head branch, or `pr-<N>` for a fork | the PR runner | the recogniser's, not told to review |
| Bitbucket PR | `bitbucket.org/o/r#N` | from the URL | the same rule | the PR runner (`bb` forge) | the same |
| GitHub or Bitbucket issue | `host/o/r#iN` | from the URL | `issue-<N>` off the default branch | none | investigate and plan |
| Discourse topic | `discourse:<host>/N` | **not named** (section 5) | `discourse-<N>` | none | gwt's: read, summarise to `DISCOURSE-<N>.md`, plan an answer |
| Zendesk ticket | `zendesk:<host>/N` | **not named** | `zendesk-<N>` | none | gwt's: read, summarise, list attachments, ask which to download |

The issue key carries an `i` so issue 5 and PR 5 of one repo are two keys.

## 5. A link that names no repo

A Zendesk ticket or a Discourse topic names a person and a symptom, not a directory (intake-design). gwt asks, with
`openziti/ziti` as the default. Three choices, and the interview decides:

- **ask**: the verb answers `needs: repo` with a list of recent repos and a default, the door shows a picker, and the
  second call carries `repo`. The card starts in a worktree of that repo.
- **none**: the card starts in a scratch directory of its own under the room's support root, with no repo, and the
  agent clones what it needs through the hub once it has read the ticket (`atrium_git_url` already serves this).
- **infer**: read the text and guess. Rejected here: the text is a stranger's (section 11), so it must not choose a
  directory, which is the rule `Recognise` already states as "the captures win".

The design's default is **ask, with none as one of the choices**, since a support case often needs no code.

## 6. The inventory

A card's inventory is a table on the room that made it, `card_resources {card, seq, kind, ref, detail, made_at,
freed_at, freed_err}`. A resource is recorded **before** it is made when it has a name that can be chosen up front (a
port, a directory), and **as** it is made otherwise (a pid). Recording first is what lets a crash between the two be
swept.

| kind | ref | freed by |
| --- | --- | --- |
| `worktree` | the path | `git worktree remove`, then the directory |
| `branch` | repo path and branch | `git branch -D` when merged or when finish was told to, else kept and listed |
| `ref` | repo path and `refs/atrium/pr/<N>` | `git update-ref -d` |
| `dir` | a path (build output, env dir, temp, support downloads) | removed when it is under a root atrium owns |
| `port` | the number | released (section 9) |
| `proc` | pid, start time and argv | stopped (section 9) |
| `overlay` | the overlay's env dir and controller pid | stopped, then its directory (section 10) |
| `identity` | an identity file and its overlay | deleted with its overlay |
| `review` | the PR row id | archived, its run folder minus `src/` kept as history |

**What is kept** on finish: the card row and its transcript, the review's run folder minus `src/` (findings,
`walk.txt`, `bundle.md`), a recap when the card wrote one, and the inventory rows themselves with `freed_at`, so the
history says what the card held and what removing it said.

**Disk per card** is the sum of the `dir`, `worktree` and run folder sizes, measured when the card goes idle and on
ask, and shown on the card and the pulls row.

**Agents add to it.** An agent that makes something the card should own (a directory, a process, a port) records it
with `atrium_own {kind, ref}`, or makes it through the atrium tool that records it for them (`atrium_proc_start`,
`atrium_port`). A thing an agent made and did not record is not removed by finish, and the sweep lists it as a
leftover only when it sits under the card's worktree or env dir.

## 7. Finish

**The verb.** `finish` on the card menu, on the pulls row, from the phone, and `atrium finish <card>`. One press shows
what will go and what will be kept, and a second confirms. `POST /v1/tasks/{id}/finish {confirm}` on the room that
holds the card.

**Order.** The card's runner stops first, then its processes and overlay, then ports, then refs, the worktree and
directories, then the review row is archived. Each row gets `freed_at` or `freed_err`. A failure on one row does not
stop the rest, and the card says which rows are left.

**What blocks it** (the interview decides each): uncommitted changes in the worktree, commits on the branch that are on
no remote and not on the hub, findings still open or accepted, a running review.

**Then.** The claim is marked finished, so a re-paste is the reopen case. The card goes to done and stays in history.
A `lifetime: link` card never closes on its own `atrium_report status=done` (r-done-report-closes-card). Only finish
or clint ends it.

## 8. The sweep

On room start, and on ask from the board, the room walks every unfreed inventory row and checks it against the world:

- a row whose card is live is left alone.
- a row whose card is done or dead and was never finished is offered as a **leftover** on the board, grouped by card,
  with one "finish these" button. Nothing is removed without that press.
- a `proc` row whose pid is gone, or is a different process (the start time differs), is marked freed.
- a `dir` or `worktree` row whose path is gone is marked freed.
- the reverse direction: a worktree under the room's worktree root that no card's inventory names is listed under
  leftovers as "not owned", which is the gwt backlog r-006 talks about. gwt's own worktrees are adopted as today and
  are not listed.

## 9. Processes and ports

The process registry design (`docs/rnd/process-registry-design.md`) is the base and is built here as an inventory
kind: `atrium_proc_start {argv, cwd, ports}` runs it under the card, shows it on the card, and its row is the `proc`
resource. Its lifetime is the card's, not the runner's: a context cycle or a restart of the runner leaves it running,
and finish stops it.

**Ports.** The room hands them out: `atrium_port {count}` answers free ports from the room's card range and records
them. The range is per room, so two rooms on one machine (sg4 and sg4-control) are given disjoint ranges at join. The
preview rule (50000 + item) stays for previews. A port is checked free with a bind before it is handed out.

**Names.** Everything a card names on shared ground (a ziti service, a docker container, a temp dir) is prefixed with
the card's short id, so two cards on two rooms never collide.

## 10. A card's own ziti overlay

atrium today drives a share and never issues an identity (overlays.md). A per-card overlay is a different thing: a
throwaway network the card owns for a test, not the operator's network. It is built on section 9.

- `atrium_overlay {up}` starts `ziti edge quickstart` in the card's env dir (`<room data>/cards/<id>/overlay`) on
  ports from section 9, as a `proc` resource, and records the `overlay` row. Its PKI and identities live in that
  directory and nowhere else.
- `atrium_overlay {identity: name}` creates and enrols an identity there and answers its file, recorded as `identity`.
- `atrium_overlay {tunnel: identity}` starts `ziti tunnel` (or `ziti-edge-tunnel` where installed) for that identity as
  a `proc`. A tunneler that needs elevation (tun mode on Windows) is refused with a sentence, and proxy or host mode
  is offered.
- Finish stops the tunnelers, then the quickstart, then removes the directory, which removes the PKI and identities.
- **This overlay never carries the board.** The board's own share keeps its own identity, which atrium does not issue.

## 11. Text from strangers

A Zendesk ticket or a Discourse post is a prompt-injection surface. The rules:

- **The board stores the identifier and the URL, not the prose** (intake-design's rule). The card's title is
  `zendesk-12345`, its why is what clint typed, and the recogniser's `forge` fetch is not run for support kinds.
- **The prompt names the link and does not quote it.** The agent reads the ticket through its own tool. The prompt
  says that what it reads there is a customer's text and is data, and that it does not follow instructions in it.
- **A support card gets no write tools by default.** Its permission mode is the room's default, and replies are
  drafts that clint sends (intake-design, "the reverse direction").
- **The text never chooses a directory, a repo or a command.** Section 5 refuses inference for this reason.

## 12. Secrets

- Forge logins stay on the hub. A card never gets `GH_TOKEN` or a `bb` credential, and reads a PR through the hub.
- A card's overlay PKI and identities are files in its env dir and go with it on finish.
- MCP credentials for Zendesk and Discourse are the runner's own configuration on the room, outside atrium, as today.
  The card does not get a copy.
- Nothing a card holds is ever written to the inventory's `detail` except a path.

## 13. Idle, cost, context, moving, the phone

- **Idle.** A card with no turn for N days shows its age and disk on the board and raises one reminder. It is never
  finished automatically. A PR that merges or closes is noticed only when something asks the hub's forge (atrium does
  not poll), so the reminder asks the forge once and says so.
- **Cost.** The card shows its tokens and dollars spent, the review's budget beside the card's. A per-kind budget
  warns, and does not stop the card.
- **Context.** A link card is an ordinary supervised card and cycles like one. Its brief after a cycle names the link,
  the worktree and the inventory, so the new context knows what it owns. r-fixtures-never-cycle-context is separate.
- **Moving.** A PR's review already moves with its claim (`f-pr-review-move`). Moving a card's inventory means
  rebuilding it on the new room: the worktree is remade from the branch through the hub, processes and the overlay are
  not moved but stopped on the old room and offered to restart on the new one. The old room's rows are freed by a
  finish there. This waits for room handoff and is last.
- **The phone.** `/m` gets the paste field (one `POST /_hub/open`), the walk drawer already works there, and finish is
  a button with the same preview.

## 14. The plan

Each phase is a backlog item, a branch `claude/<id>` off `claude/main`, and one worker. Smallest first. Phase 1 makes a
GitHub PR paste on the hub's pulls tab work end to end.

| phase | item | what | depends |
| --- | --- | --- | --- |
| 1 | `r-hub-recognisers` | the hub owns and seeds the recogniser table, `POST /_hub/recognise`, a linked room recognises through the hub. Fixes u-pulls-no-recogniser-on-hub | none |
| 2 | `r-open-verb-pr` | `POST /_hub/open` for PR kinds: claim, place, materialise (worktree with no provider row needed, row, card, walker) with rollback. The pulls tab and the launch dialog call it | 1 |
| 3 | `u-review-pr-hotkey` (exists) | Ctrl+Alt+R prompt and a menu entry, calling the verb | 2 |
| 4 | `r-open-cli` | `atrium open <url>` and `atrium_open` call the verb from any terminal or agent | 2 |
| 5 | `r-card-inventory` | the `card_resources` table, written by materialise, disk per card on the card and pulls row | 2 |
| 6 | `r-finish-pr-review` (exists) | finish over the inventory, its preview and blocks, the claim's reopen | 5 |
| 7 | `r-inventory-sweep` | the sweep of section 8 and the leftovers list | 6 |
| 8 | `r-open-support-kinds` | issues, Zendesk and Discourse through the verb, the repo choice, the text rules of section 11 | 2, answers |
| 9 | `r-card-procs-ports` | `atrium_proc_start`, `atrium_port`, `atrium_own` as inventory kinds | 5 |
| 10 | `f-card-overlay` | `atrium_overlay`: quickstart, identities, tunnelers as inventory | 9 |
| 11 | `r-card-idle-cost` | idle reminder, disk and cost on the card | 5 |
| 12 | `f-card-move-inventory` | moving a card with its inventory | room handoff |

## Interview

Asked one at a time, on this card. Each answer is recorded here and overrules the body.

### Q1. A pulls-tab paste: a row only, or the worktree and a live card at once?

clint, 2026-10-07: "when i paste a link i want it to be recognized by a recognizer. i want to be able to test the url
and the recogniztion. i wnat to know if more than one recoginzer recognize the url. i want to be able to define a
hierarcy of actoins and order them in priority. i want to be presented those options if there are more than one that
match. i want a card to come up immediately with a seed prompt and i want to be able to customize the prompt that is
given"

What it changes:
- **A card comes up at once**, with a seed prompt. Section 3 holds: the paste makes the worktree, the row and the card
  in one press.
- **All matches, not the first.** Today `MatchRecogniser` stops at the first enabled row in rank order. The hub's
  `POST /_hub/recognise` answers every matching row, in rank order, each with its captures and filled prompt.
- **A recogniser is an action.** One URL can match several rows (review this PR, work on this PR as its author, a
  plain card in the PR's worktree). The rank is the priority, and the settings list can be reordered by drag.
- **One match starts it. More than one asks.** The door shows the matches in rank order with the first selected, and
  Enter takes it.
- **Test a URL.** The settings view's "try it" exists today for one row. It becomes a test of the whole table: paste a
  URL, see every row that matched, in order, with its captures, filled fields and filled prompt, and the rows that did
  not match.
- **The prompt is customisable.** Each row's prompt template is edited in settings (exists). Whether it can also be
  edited at paste time is Q2.

### Q2. One action matches: start at once, or show the prompt first?

clint, 2026-10-07: "a", then revised the same day: "oh actually i like the shift enter idea". So (c):
- **Enter** starts the card at once with the action's filled prompt.
- **Shift+Enter** opens the filled prompt in a box first. Editing it changes this card's seed only. Enter in the box
  starts the card, and Esc goes back to the link.
- The lasting change is made to the action's template in settings.
- Where more than one action matches, the same keys apply to the chosen action.

### Q3. A re-paste of a finished link

clint, 2026-10-07: "b". A re-paste of a finished link makes a fresh card and a fresh review of the current head. The
earlier review's findings are shown beside the new ones in the walk drawer, marked "from the earlier review" with
their head and their state (posted, dismissed, accepted, deferred, open). They are read from the earlier run folder,
which finish keeps, and are not copied into the new one. They are not folded with new findings. The claim keeps the
list of earlier runs for the key so the drawer can find them.

### Q4. Finish with unpushed commits and a dirty worktree

clint, 2026-10-07: "b i want t be asked. i'll watnt to keep/stash/delete i'm sure at different tiems"

Finish never refuses and never decides alone. When the worktree has uncommitted changes or the branch has commits on
no remote and not on the hub, the finish preview lists them and asks, per worktree, with three answers:
- **keep**: the worktree and branch stay on disk, are left out of the finish, and stay listed on the card as kept. The
  rest of the inventory is freed.
- **stash**: the dirty files are committed as one WIP commit, the branch is pushed to the hub under
  `stash/<card short id>/<branch>`, and then the worktree and local branch are removed. The card's history names the
  hub ref so it can be fetched back.
- **delete**: removed as they are. The commits and changes are gone.
A clean, pushed worktree is not asked about. There is no default answer: the finish waits until one is picked.

### Q5. Where a stash lives

clint, 2026-10-07: "probably onthe hub pushed. stash is "clint/atrium" stash not git stash so it's a way of holding
work withotu cluttering woorkspace"

A stash is atrium's word, not `git stash`. It holds work off every workspace: pushed to the hub as
`stash/<card short id>/<branch>`, with dirty files in one WIP commit on top, and nothing left on the room. It is
fetched back from any room. The card's history names it, and a re-paste of the same link offers to start the new card
from the stash instead of the PR or default branch.

### Q6. A link that names no repo (Zendesk, Discourse)

clint, 2026-10-07: "default it to ziti and only activate this when i paste and do the shift-enter thing?"

- **Enter** starts the card at once in a `zendesk-<N>` (or `discourse-<N>`) worktree of the action's default repo.
  The seeded Zendesk and Discourse actions default to `github.com/openziti/ziti`. The default is a field on the
  action, edited in settings.
- **Shift+Enter** opens the same box as Q2, which for these actions also shows the repo: the default selected, recent
  repos listed, and "no repo" (a scratch folder of the card's own).
- Section 5's "ask" is replaced by this. Nothing is inferred from the ticket's text.

### Q7. Idle cards, and a PR that merged

clint, 2026-10-07: "it'd be nice to have a daily "hey you need to clean some shit" sort of alert that is not mushed
into all the other alerts. a more specital report sort of thing with all the recommendatoins. mismatched rooms, rooms
offline, cards that are idle doing nothing usage wwarnings all that sorta shit"

- **No per-card reminder and no automatic finish.** Idle cards are not alerts.
- **A daily housekeeping report**, its own surface and not a growler: one report a day on the board (and the phone),
  listing recommendations with a button beside each where one exists. For link cards: idle cards with their age and
  disk, PRs that merged or closed (one forge ask per open PR card, made when the report is built), unfinished
  inventories, leftovers from the sweep, and kept stashes. Beyond link cards it covers the whole board: rooms on
  mismatched builds, rooms offline, usage warnings, and whatever else is worth a daily look.
- "Idle" in the report starts at 3 days without a turn. The report is its own item, `r-daily-housekeeping-report`,
  since it is wider than this design. This design feeds it.

### Q8. A card's own overlay

clint, 2026-10-07: "i sorta feel like we should have a bunchof instances just ready to go whenever. turn them on, use
them, abuse them, turn them off again. i feel like 'a' covers that? it can do anything it wants. the challenge will be
ziti-edge-tunnel/tunnelers that need sudo/admin privs those will constantly demand admin intervention"

- **(a): on demand.** A card brings up an overlay when it wants one, as many as it wants, uses and abuses them, and
  turns them off. Each is in the card's inventory and finish removes any left. No action starts one at paste.
- **The hard part is elevation.** A tunneler in tun mode needs admin or root, and asking clint each time is not
  acceptable. How that is solved is Q9.
- clint, adding the same day: "don't forget those resoruces need cleanup and need to be part of that backlog audit."
  Every overlay, identity and tunneler is an inventory row, freed by finish, swept after a crash (section 8), and
  listed in the daily housekeeping report (`r-daily-housekeeping-report`) while it is running on an idle or finished
  card.

### Q9. A tunneler that needs admin or root

clint, 2026-10-07: "the agent should ask for help, provie the ccommand and askthe user to run"

Atrium does not elevate and installs no privileged service. When a card needs a tunneler in tun mode (or anything
else that needs admin or root), the agent stops and asks clint, through the card's normal question path, with the
exact command to run in an elevated shell on that machine and what it is for. The command it gives is recorded on the
card's inventory as `elevated`, with the matching command that undoes it, so finish and the housekeeping report can
show clint the undo command to run too. Atrium never runs an elevated command itself.

Refinement, clint the same day: "--if-- a claudevm exists or 'a machine the user has said you can run on as admin'
then that changes things thoguh if that's the case atrium should have some record of this for agents so they can use
that one and 'check out' the resoruce or leave notes for other agents and the like. like ziti-edge-tunnel can be run
with mulitple tuns if you ru it carefully. as long as other agents know what's out there and dont fuck with one
another that'd be fine"

- **Admin machines are named by clint.** `resources.md` (read through `atrium_resources`) already lists machines by
  hand. An entry may say an agent may act as admin there. Only clint writes it.
- **Ask first, then go there.** A card that needs elevation looks for such a machine. If one is free, it uses it
  instead of asking clint. If none is, it asks with the command, as above.
- **Checkout.** Shared things on such a machine (the machine itself, a tunneler, a tun device, a port range) are
  checked out on the hub: a lease row `{resource, machine, card, since, note}`, one holder per resource, visible on the
  board and to every agent. A card leaves notes on what it set up (which tun, which identities). The lease is an
  inventory row of the card, so finish returns it and undoes what the card set up there, and the sweep and the
  housekeeping report show a lease whose card is gone.
- **Several tunnelers on one machine** are fine when each card holds a lease on its own tun name and identity set, so
  no two cards touch the same one.
- New item: `f-admin-machine-leases`.

### Q10. Opening a link from any terminal

clint, 2026-10-07: "gwt is in the midst of being retired but gwt needs to be adaptd to atrium now"

- `atrium open <url>` (and `atrium open` with the clipboard) calls the open verb, prints the card's board address,
  and attaches with `--attach`. No OS-wide hotkey or tray app.
- **gwt is adapted now, not later.** While it is retired, `gwt <url>`, `gwt pr`, `gwt zendesk` and `gwt discourse`
  call `atrium open` when an atrium hub is reachable, so a link opened from gwt makes the same card, worktree, claim
  and inventory as one pasted on the board. gwt's own worktree path stays as the fallback when no hub answers.
  Its `prune -Recapped` defers to atrium's finish for any worktree a card owns. New item: `r-gwt-calls-atrium-open`
  (the change is in the dotfiles repo).

## Open for clint

1. Answered (Interview, Q1).
2. Answered (Interview, Q3).
3. Answered (Interview, Q4).
4. Answered (Interview, Q6).
5. Answered (Interview, Q7).
6. Answered (Interview, Q8). Elevation for tunnelers is Q9.
7. Answered (Interview, Q10).
8. A cost budget per card: a warning only, or a stop?
