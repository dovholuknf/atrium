# Item dependencies: work that waits on other work, with a gate only the board resolves

Status: design, 2026-09-30, @rnd. Built by @runtime (hub side and MCP), drawn by @ui. Item
`docs/backlog/runtime/r-new-item-dependencies.md`. Stage 1 is sized for one worker day and deploys HUB ONLY, so it
needs no room restart.

## 1. The problem, and the answer in five lines

The orchestrator keeps "u-033 has to be live before the terminal batch" and "0077 sits after 0076" in its handoff,
and a context reset loses them. Directors have also started items whose work was already on `claude/main`.

1. The edge lives on the **hub**, in `hubstore`, keyed by backlog item id. The hub sees every room, holds the
   `git_repos` registry from f-019, and knows its own build, so cross-room and "live on the hub" cost nothing extra.
2. A gate is met only by something the hub **reads for itself**: git on the registered checkout, the room list, its
   own build commit. Or a human clears it on the board. No agent verb marks a gate met.
3. "Item X landed" means the integration branch holds `changelog/<dept>/<date>-X.md`. That file is already the
   per-item landing record every director writes, and the hub reads it without anybody telling it.
4. `atrium_launch` refuses a worker whose alias or title names an item with an open gate. That is the enforcement:
   worker names are item ids, so a director cannot start blocked work by accident.
5. One MCP tool, `atrium_deps`, adds, lists, checks and answers "what is ready in my department". It has no clear.

## 2. Decisions on the three open questions

**Items versus cards: the item carries the edge.** A card outlives its process and an item outlives its card, and the
thing the orchestrator loses is about items ("r-038 waits on r-037"). A card is tied to an item by the naming rule
that already holds: a worker's alias is its item id, and its title starts `<id>:`. So "the card working it" needs no
second edge. The board shows a card's gates by matching its alias (or the title prefix) to the item.

A target that is a CARD with no item id (a human's card, a director) is not in stage 1. Deciding "that card's work
is done" needs the room's work ledger on the hub, which is a room change and a room restart. Stage 2 adds `card:`
targets once the task view carries `work_state` (section 8). Until then such a wait is a named condition, which a
human clears.

**Cycles are refused at write time.** On `add`, the hub walks the open `item` edges from each new target, with
renames applied (section 4.3), and refuses if it reaches the waiting item. The error names the loop:
`r-040 waits on r-041, which waits on r-040`. Conditions are leaves and cannot form a loop.

**Cross-room is free.** The gate table is on the hub, not in a room. An item worked on sg4 that waits on an item
built on sg3 is one row, and both land on the same `claude/main`. `room:sg3` is a condition the hub answers from its
own attached list.

## 3. What a gate is

One row says "item A waits on target T". Two kinds of target:

| Target | Written as | Met when (checked by the hub) |
|---|---|---|
| another item | `r-037`, `u-033`, `91` | the integration branch tip holds `changelog/*/YYYY-MM-DD-<id>.md`, basename exact |
| a named condition | `live:u-033`, `room:sg3`, `sha:4815d47`, or free text | the fixed check below, or a human clears it |

Conditions with a check, a FIXED table in the binary, never a command a caller supplies (the same rule as the
preflight verb):

| Condition | Met when |
|---|---|
| `live:<item>` | the commit that ADDED that item's changelog file is an ancestor of the hub's own build commit |
| `room:<name>` | the room is attached to the hub at the moment of the check |
| `sha:<sha>` | the commit is an ancestor of the integration branch tip |

Anything else (`"f-006 migration on m1mini"`) is free text. It is cleared by a human only, and the list says so.

**A gate is met once and stays met.** When a check first passes, the hub stamps `met_at` and `met_by = atrium` with
what it saw ("changelog/ui/2026-09-30-u-033.md on claude/main 3f2a9c1"). It is never re-opened by a later check, so
`room:sg3` means "sg3 has attached since this was written", and a re-signed `claude/main` cannot un-land an item.

**Unknown is not met.** A hub built as `dev` with no commit answers `live:` as "hub build unknown", open. A repo not
in `git_repos`, or a git call that fails or times out, leaves the gate open with the reason on the row. A failed
check never halts anything and never clears anything.

### 3.1 Why the changelog file and not the backlog status line

Status lines go stale (every director has been bitten), and they are written by agents. A changelog entry reaches
`claude/main` only through a landing, and its name already carries the item id by the per-department rule. Known
gaps, accepted for stage 1:

- A partial landing with its own entry (`r-007-3`) does not satisfy `r-007`, and `r-007.md` landing early satisfies
  it before the later stages. The exact basename rule is what is written. A human clears or re-adds when it matters.
- An item closed without shipping (r-033, "won't do") never gets an entry. A human clears the gate with the reason.
- An rnd design lands a changelog entry too, so a design's entry must NOT carry the build item's id, or the design
  landing would read as the build landing. This design's entry is `changelog/rnd/2026-09-30-item-dependencies-design.md`
  for that reason. "Waits on the design" is written as its own item or as a `sha:` condition.

## 4. Stage 1, what @runtime builds (one worker day)

### 4.1 Schema: hubstore migration `0004_item_gate`, at the END of the slice

```sql
CREATE TABLE IF NOT EXISTS item_gate (
  id        TEXT PRIMARY KEY,               -- ULID-ish, like every other key
  repo      TEXT NOT NULL,                  -- a git_repos name
  item      TEXT NOT NULL,                  -- the waiting item id
  kind      TEXT NOT NULL CHECK (kind IN ('item','cond')),
  target    TEXT NOT NULL,
  why       TEXT NOT NULL DEFAULT '',       -- bounded, 400 bytes
  added_by  TEXT NOT NULL,                  -- handle@room, or 'human'
  added_at  TEXT NOT NULL,
  met_at    TEXT NOT NULL DEFAULT '',
  met_by    TEXT NOT NULL DEFAULT '' CHECK (met_by IN ('','atrium','human')),
  met_why   TEXT NOT NULL DEFAULT '',
  told_at   TEXT NOT NULL DEFAULT ''        -- when added_by was told the item is clear
);
CREATE UNIQUE INDEX IF NOT EXISTS item_gate_open ON item_gate (repo, item, kind, target) WHERE met_at = '';
CREATE INDEX IF NOT EXISTS item_gate_item ON item_gate (repo, item);
CREATE TABLE IF NOT EXISTS item_rename (
  repo    TEXT NOT NULL,
  from_id TEXT NOT NULL,
  to_id   TEXT NOT NULL,
  at      TEXT NOT NULL,
  by      TEXT NOT NULL,
  PRIMARY KEY (repo, from_id)
);
```

Partial indexes run on SQLite and Postgres both, so the portability rule holds. A duplicate `add` of an open gate is
a no-op that answers the existing row.

### 4.2 The check, `internal/link/deps.go` (new) with a small `internal/gitsync` helper

- `landedItems(repo) (map[id]path, tip, error)`: one `git ls-tree -r --name-only <branch> -- changelog` in the
  registered checkout, parsed with `^\d{4}-\d{2}-\d{2}-(.+)\.md$`. Cached by branch tip sha, so a list call on an
  unchanged branch runs one `git rev-parse`. Every git call gets the gitsync timeout and no network (`ls-tree`,
  `rev-parse`, `merge-base --is-ancestor`, `log --diff-filter=A -1 --format=%H <branch> -- <path>`).
- `evaluate(gate)`: section 3's table. It writes `met_*` in the same call when a check passes.
- Evaluation runs ON READ (list, check, ready, launch) and on ONE hub ticker, every 60s, that does nothing unless an
  open gate exists. The ticker is what tells the waiter (4.5). No per-room polling, no new work for rooms.

### 4.3 Renames

A slug item (`r-new-item-dependencies`) gets a number when it lands, and a gate written against the slug has to follow.
@merge's landing step already renames the files. It also calls `atrium_deps rename {from, to}`, which writes
`item_rename` and rewrites `item` and `target` on open gates in one transaction. The landed check tries both names,
so a slug that lands before the rename is still seen. A rename is a fact about a name. It clears nothing.

### 4.4 The surface

Hub endpoints, loopback only like the rest of `/_hub/` (in `serveHubAPI`'s switch):

| Endpoint | Who | What |
|---|---|---|
| `GET /_hub/deps?item=&open=1&repo=` | board, MCP | gates, each with a live `state` (`open`, `met`) and `reason` |
| `POST /_hub/deps` `{repo, item, waits_on[], why}` | board, MCP | add. Refuses a cycle, an empty id, more than 20 targets |
| `POST /_hub/deps/clear` `{id, why}` | board only | a human clears one gate. `why` required |
| `POST /_hub/deps/rename` `{repo, from, to}` | MCP (@merge) | section 4.3 |
| `GET /_hub/deps/ready?dept=&repo=` | MCP | the department's backlog items on the branch, not landed, with no open gate |

`repo` defaults to the only `git_repos` entry, and is required when there are more.

MCP tool `atrium_deps`, full class only (workers do not get it), one tool with an `action`:

- `add {item, waits_on[], why}`: `waits_on` entries are item ids or `cond` strings as in section 3.
- `list {item?, open?}`: everything blocked and on what, in one call. This is the orchestrator's "list all blocked".
- `check {item}`: `ready`, or the open gates with their reasons.
- `ready {dept}`: 4.6.
- `rename {from, to}`.

There is **no clear action**, and the tool description says so: "a gate clears when the board sees the work land, or
when a human clears it on the board. If a gate is wrong, ask the human." `POST /_hub/deps/clear` refuses a request
carrying `X-Atrium-Agent`, which every control call carries.

That refusal is tidiness, not a boundary, and the design says so plainly. An agent can `curl` the loopback endpoint
without the header, exactly as it could `curl` a permission decision. What stops that is the permission gate seeing
the command, the same line every other human-only action in atrium sits on. The hub audits every clear with its
remote address and headers (`room_audit`, kind `deps-clear`), so a clear that did not come from the board is visible.

### 4.5 Telling the waiter

When the last open gate on an item is met, the ticker sends ONE say to `added_by` of each of that item's gates (deduped,
stamped in `told_at`): "r-038 is ready: r-037 landed (changelog/runtime/2026-09-30-r-037.md on claude/main 1a2b3c4)".
It uses the hub's existing say path, so an offline room holds it the way any say is held, and the handle is resolved
at delivery, as `report_to` does. A human-added gate (`added_by = human`) tells nobody: the board shows it.

One turn per unblocked item is the whole token cost. Nothing is typed into a terminal, and no card is woken for a
gate that is still open.

### 4.6 `ready`: what a director asks for its next item

The hub lists `docs/backlog/<dept>/*.md` on the integration branch (`git ls-tree`, then ONE `git cat-file --batch`
for the first 12 lines of each, the lines `backlog-index.ps1` reads), drops items that have landed (3), drops items
with an open gate, and answers id, title and Status line. It reads `claude/main`, not the director's worktree, which
is how it avoids handing out an item already landed. Status lines are shown as written and trusted for nothing.

If the day runs short, `ready` is the piece to cut. The launch refusal (4.7) is what makes the guarantee.

### 4.7 The launch refusal

In the hub's `atrium_launch` handler, before the cap reservation: take the item id from `alias`, else from the title
up to the first `:`. If that item has an open gate, refuse:

```
r-038 waits on: r-037 (not landed: no changelog/*/*-r-037.md on claude/main 9f1e2d3). a gate clears when the
board sees the work land or a human clears it. atrium_deps check r-038 shows it again.
```

No override flag, for agents or anyone. A human who wants it started clears the gate first, which is one click and
is recorded. A launch whose id matches no gate is untouched, so every existing launch behaves as today.

### 4.8 Tests the worker writes

- Store: add, duplicate add answers the same row, partial unique index lets a met gate be re-added, rename rewrites
  open gates only, cycle refused with the loop named (direct, three long, through a rename).
- Check against a temp repo: changelog present and absent, exact basename (`r-007` not met by `r-007-3`), slug and
  renamed id both seen, `sha:` ancestor and not, `live:` with a build commit before and after the adding commit,
  `live:` with an empty commit is open with "hub build unknown", `room:` from a fake attached list, git timeout
  leaves it open with the reason.
- Met is sticky: amend the branch so the file is gone, the gate stays met.
- Clear: refused with `X-Atrium-Agent`, refused without `why`, audited.
- Launch: refused by alias, by title prefix, allowed with every gate met, allowed with no gate.
- Ticker: one say per waiter when the last gate clears, none while one is open, none twice.

## 5. What @ui draws (board, hub-served)

- On a card whose alias or title prefix names an item with open gates: a `waits on r-037` chip, one per gate up to
  two then `+N`. The tooltip carries each reason. Met gates are not shown.
- A **Blocked** list (a pane or a filter, @ui's call): every open gate, grouped by waiting item, with its reason, its
  age and who added it, and a Clear button that asks for a reason. An Add form (item, targets, why).
- The data is `GET /_hub/deps?open=1`, fetched on load and on a `deps` event the hub publishes on its existing feed
  when a gate is added, met or cleared. No new poll.

A gate is not a column. It says nothing about what the card's runner is doing, and a blocked item usually has no card
at all yet. A chip and a list follow "status is a column, activity is a badge".

### 5.1 What the board calls (as built, @runtime)

Every answer is JSON. A refusal is `{"error": "<sentence>"}` with its status, and the sentence is written to be shown.

`GET /_hub/deps?open=1` (also `item=`, `repo=`). Open to any board. Answers `{"gates": [gate, ...]}`, oldest first,
each checked on the way out, so a gate met by this very read is not in an `open=1` answer:

```json
{"id": "0199...", "repo": "github/dovholuknf/atrium", "item": "r-038", "kind": "item", "target": "r-037",
 "why": "needs the store half", "added_by": "runtime-director@sg4", "added_at": "2026-09-30T13:00:00Z",
 "state": "open", "reason": "not landed: no changelog/*/*-r-037.md on claude/main 1a2b3c4"}
```

`kind` is `item` or `cond`. `state` is `open` or `met`. A met gate also has `met_at` and `met_by` (`atrium` or
`human`), and `reason` is then what was seen when it was met. `added_by` is `human` for a gate added from the board.
The chip's tooltip is `reason`. A card's gates are the gates whose `item` equals the card's alias, or its title up to
the first `:`.

`POST /_hub/deps` `{"item": "r-038", "waits_on": ["r-037", "room:sg3"], "why": "..."}`. The Add form. `repo` only
when the hub has more than one. Leave `added_by` off, which records `human`. Answers `{"gates": [...]}` checked at
once, so a target already met comes back `met`. 409 for a loop (the sentence names it) or a bad id, 400 for an empty
entry or more than 20. Loopback only (403 otherwise).

`POST /_hub/deps/clear` `{"id": "<gate id>", "why": "<required>"}`. The Clear button: ask for the reason first, the
route refuses an empty one (400). 409 when already met, 404 for no such gate. Answers `{"gate": gate}`. Loopback only.

`GET /_hub/deps/ready?dept=runtime` answers `{"items": [{"id", "title", "status"}]}`. Optional for the board.

The hub publishes a `deps` event on its feed when a gate is added, met, cleared or renamed:
`{"repo": "...", "item": "r-038", "what": "added|met|cleared|renamed"}`. A delta: re-fetch `GET /_hub/deps?open=1`
on it and on reconnect.

## 6. How the orchestrator and directors use it

This part is instructions for whoever owns `DIRECTOR.md` and the orchestrator's cold start, not code:

- A prerequisite goes into `atrium_deps add` the moment it is known, never only into a handoff.
- A director picks work with `atrium_deps ready <dept>`, or at least runs `atrium_deps check <id>` first.
- @merge's landing step adds `atrium_deps rename` when it gives a slug its number.
- The morning question "what is stuck" is `atrium_deps list open`.

## 7. Resilience

- Nothing here can halt the hub. A hubstore write failure is the hub's existing halt, not a new one.
- A failed or slow git read leaves gates open with the reason. It never clears one and never blocks a launch that
  has no gate.
- The launch refusal reads the store only, plus at most one cached `ls-tree`. It adds no network call to a launch.
- The ticker is skipped when no open gate exists, so an idle hub pays nothing.

## 8. Later (named, not promised)

- **`card:` targets.** The room's task view grows `work_state` beside `MergedView` (`internal/api/api.go:945`),
  and a card target is met when its work item is `accepted`. Needs a room build, so it rides the next room restart.
- **`Waits on:` lines in backlog files,** read by `ready` and offered as gates to add. Offered, not applied: a file
  line is written by an agent, and adding a gate is the safe direction, so the board may import them with one click.
- **`live:<item>@<room>`,** once rooms report their build commit to the hub rather than a version string.
- **The merged-cull hook** (`atrium merged --into`), once installed, gives a second landing signal with a sha.
</content>
</invoke>
