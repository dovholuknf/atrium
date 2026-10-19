# Landing fires a hub event that cards subscribe to

Backlog item `f-land-webhook-subscriptions`, high. Status: designed, not built.

clint, 2026-10-19 17:38:

1. `scripts/land-claude-main.ps1` stops sending the orchestrator a notification (it ran `atrium tell` to `-NotifyTo`).
2. The script invokes a webhook instead.
3. Cards can subscribe to that webhook, either by hand (board) or by an agent told to (a tool or command).

## What exists

- **The CI hook** (`docs/fabric/ci-hook-design.md`, built: `internal/link/cihook.go`, `cihook_bind.go`,
  `internal/hubstore/cihook.go`). A receiver on the hub's ziti service and on the loopback route `POST /_hub/ci-hook`
  for `edge.LocalOperator` only. Routes in `hub_setting` map a run to handles resolved at delivery. Deliveries are
  rows (`ci_delivery`) worked by one loop on the growl tick, through `CIDeliverer` (`Resolve`, `Send`), which is the
  hub's say path (`controlMCP.deliverAs`). States `delivered`, `unconfirmed` (never sent again), `pending` (retried,
  name resolved again each try), `expired` after 24 hours, `refused`. Pending rows for one target collapse to the
  newest. The message is built from parsed fields only.
- **The land script** is the human's own: `scripts/land-claude-main.ps1` in the main checkout, listed in
  `.git/info/exclude`, so it is not tracked. The version on `claude/land-script-tests` (`9a41a69c2`) is the one that
  ran `atrium tell` on every outcome (pushed, not pushed, stopped), best effort.
- **The runner's card** is known to its tools: `ATRIUM_TASK_ID` is in every runner's environment, and the control MCP
  finds the caller's card (`control_own.go`, `myCardPath`).

## Decisions

### 1. A general hub event endpoint, beside the CI hook, sharing its delivery

New route `POST /_hub/events` on the hub board, `edge.LocalOperator` only, the same posture as the loopback CI hook: a
share or an overlay request is a 403 with the same sentence. It is not mounted on the ziti service.

Why not the CI hook's route: the CI hook takes one shape, a GitHub `workflow_run`, authenticated by a secret because a
GitHub runner sends it, and routed by rules the human writes. A land event comes from the human's own shell on the
hub's machine and is routed by who asked to hear it. Pushing both through one handler would mean a body that is
sometimes a `workflow_run` and a secret that is sometimes not needed. What the two share is the delivery, and that is
what is shared (4).

The body:

```json
{"kind": "land", "repo": "dovholuknf/atrium", "branch": "main",
 "outcome": "pushed", "from": "3f1c0de...", "to": "9a0b1c2...", "commits": 14, "reason": ""}
```

| Field | Rule |
| --- | --- |
| `kind` | from a closed list in code. Today only `land`. Anything else is 422 naming the kinds |
| `repo` | `owner/name`, `store.RepoSlug` rules |
| `branch` | the branch landed on, the target. `git check-ref-format --branch` rules |
| `outcome` | `pushed`, `not-pushed` or `stopped` |
| `from`, `to` | 7 to 40 hex. `to` required for `pushed` |
| `commits` | 0 to 10000 |
| `reason` | for `stopped` and `not-pushed`: up to 200 bytes, control characters stripped, `ciClean` rules |

Each kind has its own parser that checks its fields and builds its one line. The line is built from parsed fields,
never copied from the body, with `reason` the one free-text field, bounded and cleaned. For `land`:

- pushed: `Landed: dovholuknf/atrium main 3f1c0de..9a0b1c2, 14 commits.`
- not pushed: `Land not pushed: dovholuknf/atrium main, 14 commits ready at 9a0b1c2. <reason>`
- stopped: `Land stopped: dovholuknf/atrium main. <reason>`

Each line ends `(from the hub, no reply goes anywhere)`, as the CI line does.

**Bounds**, the CI hook's: 64 KiB body (413), its own token bucket of 60 a minute with a burst of 20 (429 with
`Retry-After`), 422 with one sentence on a bad field. **Dedup** on `(kind, repo, branch, outcome, from, to)`, a
`UNIQUE` constraint: a repeat answers 200 `{"duplicate": true}` and delivers nothing. **The answer** is 202
`{"event": <id>, "subscribers": <n>}` once the row is written, before delivery, so a slow room never slows the script.

### 2. Subscriptions live on the hub, per card

New hub table `event_sub`, at the end of the hubstore migration slice, `CREATE TABLE IF NOT EXISTS`:

| Column | Meaning |
| --- | --- |
| `id` | ulid, primary key |
| `kind` | `land` for now. A `CHECK` is not used, so a new kind needs no table rebuild. The code validates |
| `repo`, `branch` | filters, empty for any |
| `card` | `room~id`. The card, not a handle |
| `wake` | 1 to resume the card when parked. Default 0 |
| `created_by`, `created_at` | `board` (the human), or the card's own key |

`UNIQUE (kind, repo, branch, card)`, so subscribing twice is one row and answers `already`.

**Per card, not per handle.** A card id outlives a context cycle, a runner restart and a room restart, and a handle
can be taken by another session. The CI hook routes by handle on purpose (whoever holds the QA alias gets CI). This is
the other case: a card asked to hear something. Both exist, and neither replaces the other.

**It survives the room's restart** because it is on the hub, which keeps no runner state and needs none for this.

**It goes when the card goes:**

- A delivery whose room answers that the card does not exist deletes the subscription and writes one audit line
  `event-sub-gone`.
- Once a day the hub asks each attached room, in one request per room, which of its subscribed card ids still exist
  (`POST /v1/tasks/exist` with the ids, answering the ones that do), and deletes the rest. A room that is not attached
  is skipped, never treated as "every card gone".
- `atrium_subscribe unsubscribe` and the board's remove remove it at once.

An archived card still exists and keeps its subscriptions. A parked one gets pending deliveries (4).

### 3. Subscribing: the board, one tool, one command

**The board.** The card's detail gains one line, **Hears**: a chip per subscription (`land dovholuknf/atrium main`)
with a remove cross, and **+ land** that adds one with the card's repo and `main` filled in, both editable. It posts to
`/_hub/events/subs` through the hub. The human may add or remove a subscription on any card.

**The agent.** A new control MCP tool, `atrium_subscribe`, with `action` `subscribe`, `unsubscribe` or `list`, and
`kind`, `repo`, `branch`, `wake`. It acts on the CALLER'S card only, found as every own-card tool finds it. An agent
cannot subscribe another card: a card that should hear landings is told to subscribe, or the human does it on the
board. The tool is deferred like the others, so its schema costs nothing until loaded.

**The command.** `atrium subscribe land [--repo r] [--branch b] [--wake]`, `atrium unsubscribe land ...` and
`atrium subscriptions`, for a runner with a shell and no MCP. It takes the card from `ATRIUM_TASK_ID` and goes through
the room to the hub, like `atrium finish`. The command and the tool share one handler on the room.

Who may call `/_hub/events/subs`: the board as the human, and a room on behalf of its own card, which the hub checks
against the room's link identity: room `sg3` may only write subscriptions whose `card` is `sg3~...`.

### 4. Delivery: a queued fyi, through the CI hook's loop

Each event writes one `event_delivery` row per matching subscription. That table has the columns of `ci_delivery`
(event ref, target, state, answer, card, attempts, times). The CI hook's loop becomes the hub's delivery loop: it works
both tables on the growl tick through the same `CIDeliverer`, which is renamed `Deliverer` in the same change.
Behaviour, unchanged from CI:

- `from` is `land@<hub name>`, `kind` `fyi`, `wake` from the subscription.
- An `fyi` to a session that holds its notices (the orchestrator) is typed when its turn ends, so a land
  notice never interrupts a turn.
- A parked card without `wake` is `pending`, retried, `expired` after 24 hours.
- Pending land deliveries to one card collapse to the newest per `(repo, branch)`, the older ones `expired` with
  `superseded`. A card back after a day reads where `main` is, not every land since.
- `unconfirmed` is never sent again.

`stopped` is an `fyi` too. A land that stopped is the human's to fix at their own shell, where they already saw it.

No growler and no board notice per event: the human ran the script and watched it. Each event and each delivery that
ends `refused` or `expired` is one audit line (`event`, `event-delivery`), which the audit pane filters on.

**The record.** New hub table `hub_event`: kind, repo, branch, outcome, from, to, commits, reason, received at, and
the source (`local`). Never the raw body. `hub_event` and `event_delivery` rows older than 30 days go with the CI
sweep.

### 5. Authentication for a POST from a local script

Loopback is the authentication, the rule the board already keeps: atrium binds the board to loopback and has no login,
so anything on the hub's machine that can reach `127.0.0.1:7778` is the operator. `edge.LocalOperator` refuses a
request that came through a share, an overlay or a proxy. No secret and no token: a secret on the same machine as the
board would protect nothing the board does not already expose.

A script on a room's machine, not the hub's, is out of scope now. When it is wanted, the room's own board gets the same
route, `edge.LocalOperator` there, and forwards over the room link, where the hub records the room's link identity as
the source. Nothing in this design blocks it.

### 6. The land script

The script is the human's own file, excluded from git, so the build cannot edit it. The build ships a tracked helper,
and the script calls it.

`scripts/atrium-event.ps1`: takes `-Kind`, the fields as parameters, and `-HubUrl` (default `$env:ATRIUM_HUB_URL`, else
`http://127.0.0.1:7778`). It builds the body, POSTs it with a 5 second timeout, prints one line (`event: 2
subscribers` or `event: duplicate`), and on any failure prints one warning and exits 0. A land must never fail because
the hub is down, the posture `atrium tell` had. `scripts/atrium-event.sh` is the same for bash.

The change to `land-claude-main.ps1`, which the human applies (the change notes carry it): remove `-NotifyTo` and the
`atrium tell` block, and at each outcome call

```powershell
pwsh -File scripts/atrium-event.ps1 -Kind land -Repo dovholuknf/atrium -Branch $Target -Outcome pushed `
    -From $before -To (git rev-parse $Branch) -Commits $count
```

with `not-pushed` and `stopped` and a `-Reason` on the other paths. Then the orchestrator's card subscribes once:
`atrium_subscribe {action: "subscribe", kind: "land", repo: "dovholuknf/atrium", branch: "main"}`.

### 7. Other kinds later

A kind is a parser, a line and a closed set of filter fields, so adding one is code and no migration:

- **`deploy`**, from `atrium-deploy.ps1`: hub or room, the version, the outcome. Filters `room`.
- **`ci`**: the CI hook publishes each recorded run as an event too, so a card can subscribe to CI for one repo and
  branch without the human writing a route. Routes stay, for "whoever holds this alias".
- **`merge`**, from a merge worker landing on `claude/main`.

The filters stay `repo` and `branch` until a kind needs another, which is then an `ADD COLUMN`.

## Build stages

1. **E1, events and subscriptions.** `POST /_hub/events` with the `land` parser, `hub_event`, `event_sub`,
   `event_delivery`, the shared delivery loop and the `Deliverer` rename, `/_hub/events/subs` with the room identity
   check, `atrium_subscribe`, `atrium subscribe`, the daily existence check and `POST /v1/tasks/exist` on the room,
   `scripts/atrium-event.ps1` and `.sh`, and the land script change in the change notes.
2. **E2, the board.** The **Hears** line on the card with add and remove.
3. **E3, more kinds.** `ci` from the CI hook, then `deploy` and `merge`, each when its sender is changed.

## Tests

No live hub, the link harness (a hub and one room on loopback), injected clocks.

- **Receive:** a good land event is 202 with the subscriber count. Unknown kind, bad sha, bad repo, a `pushed` with no
  `to`, a reason over 200 bytes: 422. Over 64 KiB: 413. Over the rate: 429. A repeat: 200 duplicate, nothing
  delivered. A request through a share or a proxy: 403.
- **Line:** each outcome's line from fields, control characters stripped from `reason`, the signoff present.
- **Subscribe:** the tool subscribes the caller's card and no other. Twice is one row. A room writing a subscription for
  another room's card is refused. The board may write any.
- **Delivery:** a live card gets one fyi. A parked card without `wake` is pending, then expires at 24 hours. Three land
  events while parked deliver only the newest. A room answering no such card deletes the subscription with one audit
  line. An unconfirmed delivery is never sent again. A filter on another branch delivers nothing.
- **Gone cards:** the daily check deletes subscriptions of cards the room says are gone, and leaves a detached room's
  untouched.
- **CI unchanged:** the existing CI hook tests pass with the shared loop.
- **Script:** `atrium-event.ps1` against an `httptest`-like listener, and against nothing listening, where it warns once
  and exits 0.
- **Board** (headless, mocked): the Hears line lists chips, remove posts the subscription id, add posts kind, repo and
  branch.
