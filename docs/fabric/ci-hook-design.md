# The hub hears CI: webhooks over the ziti-hook service

Backlog item `f-hub-ci-hook`. Status: designed, not built. The build waits for the code freeze.

clint, 2026-10-17: CI notifies the hub, and the hub tells the QA director, or the orchestrator. The sender exists: the
GitHub Action `dovholuknf/ziti-hook` POSTs a body over OpenZiti to the service `ziti-hook` (intercept
`ziti-hook.ziti:80`), which `#atrium-hubs` may bind and the identity `ziti-hook-gh` may dial. Only a Python test
listener receives today.

## What exists

- `atrium run --identity <file>` is the hub's ziti identity. `link.Ziti{Identity, Service}.Listen()` binds a service
  with it. The room link (`transport.go`) and the board (`--board-transport ziti`, `atrium_run.go`) each bind one.
- A transport after the first that cannot listen is left out and logged, and the hub runs without it. The board on
  ziti is the same: a failed bind serves loopback only. Neither retries.
- `controlMCP.deliverAs` (`control_relay.go`) resolves a name on a room and posts `/v1/tasks/{id}/message` with a
  `from`, a `kind` and `wake`. A post whose answer was lost is `unconfirmed` and never sent again. The hub holds no
  message: a cross-room say the target cannot take is kept by the SENDER'S room.
- `/_forge/ci` (`atrium_ci`) is a room asking the hub to poll GitHub. Nothing on the hub is told when a run ends.
- `hub_setting` holds hub-owned values such as the VAPID private key, which atrium mints and never returns.
- `github.com/golang-jwt/jwt/v5` is already a direct dependency.

## What the sender sends

The action runs inside a workflow and sends the body it is given. The `ci` workflow's last job, which `needs` every
other job and runs on `failure()`, builds that body from the run's own push or pull request context. The body has the
fields of a `workflow_run` event, because that is the shape the hub reads: `workflow_run` with `id`, `run_attempt`, `name`, `path`, `head_branch`, `head_sha`, `event`, `conclusion`, `html_url`
and `repository.full_name`. That is the one shape the hub accepts.

## Decisions

### 1. The hub binds `ziti-hook` with its own identity, and retries

New flag `atrium run --ci-hook-service <name>`, empty for off. It uses `--identity`, the identity the link and the
board already use, which carries `#atrium-hubs`. One ziti context per identity file: `link.Ziti` gains a
`ListenService(name)` so the link, the board and the hook share one context and one router session, the shape
`f-link-one-conn-direct` wants for direct.

The listener gets its own `http.Server` with one handler, `POST /`. Every other method and path is a 404. It never
serves the board, the API or the control MCP, so a GitHub runner holding the dial identity reaches this handler and
nothing else. `ReadHeaderTimeout` 10 s, `ReadTimeout` 30 s, `IdleTimeout` 60 s.

**The controller is down.** The hub starts without the hook, the same posture as a left-out link transport. Unlike
the board, it retries the bind in the background: 30 s, doubling to a 10 minute cap, forever. A missed webhook is not
recoverable by the sender, so the hub should come back on its own. The first failure and the recovery are each one
audit line (`ci-hook-down`, `ci-hook-up`). A bound listener that dies (`Accept` error) goes back to the retry loop.
The state is one row in `/_hub/inventory` listener states beside the link sides, so the board shows it where it
already shows a transport that is down.

The same handler is mounted on the hub board at `POST /_hub/ci-hook`, for `edge.LocalOperator` only. That is for the
local script (decision 6) and for tests. It is not a second public door: a share or an overlay request is a 403.

zrok private shares are out of scope. ziti-hook supports them, and atrium adds no zrok surface for now.

### 2. Authentication: a shared secret first, a GitHub OIDC token second

**Phase 1, the secret.** Header `X-Ziti-Hook-Secret`, the action's default. The hub mints it:
`atrium ci-hook secret` prints a new 32-byte random value once, base64url, and stores only its SHA-256 in
`hub_setting` as `ci_hook.secret_sha256`. It runs on the hub's machine only (it opens the hub store directly, like
`atrium rooms`). The value is atrium's own, like the VAPID key, so storing a hash of it does not break the rule that
atrium never holds somebody else's credential. The secret is never logged, never returned by an API, and never in an
audit line.

`atrium ci-hook secret --rotate` keeps the previous hash valid for 24 hours as `ci_hook.secret_prev_sha256` with its
expiry, so changing the GitHub secret is not a race. The compare is `subtle.ConstantTimeCompare` on the hashes.

A missing or wrong secret is a 401 with no body detail, and one audit line `ci-hook-refused` naming the reason
(`no secret`, `wrong secret`) and nothing from the body.

**Phase 2, OIDC.** The `notify` job sets `permissions: id-token: write` and the action's `audience`. The hub reads
`Authorization: Bearer <jwt>` and checks:

- RS256 only, signed by a key in `https://token.actions.githubusercontent.com/.well-known/jwks`. The set is cached for
  an hour and fetched again on an unknown `kid`, at most once a minute, each fetch bounded to 5 s. Only the hub talks
  to GitHub, so this fits the forge rule.
- `iss` is `https://token.actions.githubusercontent.com`. `aud` equals `ci_hook.audience`, default `atrium-hub:<hub
  name>`. `exp`, `nbf` and `iat` with 60 s leeway.
- `repository` is in `ci_hook.repos`, and equals the payload's `repository.full_name`, so a token from one repo cannot
  report a run of another.
- `job_workflow_ref` names a workflow listed for that repo in `ci_hook.repos`.

`ci_hook.require` is `secret` (the phase 1 default), `oidc` or `both`. Phase 2 makes `both` the default once a repo is
set up for it. The ziti dial policy stays the first gate in every mode. The secret and the token are defence in depth,
because the dial identity sits in GitHub secrets of every repo that sends.

The ziti-hook blocker (no ext-jwt login in `ziti-sdk-py`) does not touch this. OIDC here authenticates the POST, not
the ziti session.

### 3. Routing: rules from run to handle, every match delivers

`hub_setting` `ci_hook.routes` is a JSON list. Each rule:

| Field | Meaning |
| --- | --- |
| `repo` | `owner/name`, required |
| `workflow` | the workflow `name` or `path`, empty for any |
| `branch` | `head_branch`, empty for any |
| `conclusion` | a list from `success`, `failure`, `cancelled`, `timed_out`, `action_required`, empty for any |
| `change` | true: only when this conclusion differs from the last one recorded for the same repo, workflow and branch |
| `to` | a handle, alias or `name@room`, resolved when it is delivered |
| `kind` | `fyi` or `needs`. Empty: `needs` for `failure` and `timed_out`, `fyi` otherwise |
| `wake` | true to resume a parked target. Default false |

EVERY matching rule delivers, once per resolved card, so two rules naming the same session send one message. A run no
rule matches is recorded and goes nowhere.

The proposed default (clint has not confirmed it): every finished run of `dovholuknf/atrium` to the QA director, and
`failure` on `main` with `change: true` to the orchestrator. It ships as the documented example and NOT as the default
value, because the handles are clint's to name. An empty table records every run and tells nobody. Setting the table
is `PUT /_hub/ci-hook/routes`, LocalOperator only, which validates each rule and refuses the whole list on one bad rule.

The message is one line, built by the hub from parsed fields and never from free text in the body: `CI <conclusion>:
<repo> <workflow> on <branch> at <sha7>, attempt <n>. <html_url>`. The URL is kept only when it is on `github.com`.

### 4. Delivery: the say path, and a hub-side pending list for a target that cannot take it

The hub posts through `deliverAs` with `from` `ci@<hub name>`. The build checks the room renders this as a peer
message and not as the operator. The text ends `(from CI, no reply goes anywhere)`, because `ci@hub` is not a session.

Each delivery is a row (decision 5) in one of these states:

| State | When | Then |
| --- | --- | --- |
| `delivered` | the room answered `terminal`, `queued` or `typed-queued` | done |
| `unconfirmed` | the post went and the answer was lost | done. Never sent again, the say rule |
| `pending` | no card holds the name, the card is parked and `wake` is off, or the room is not attached | retried |
| `expired` | still pending after 24 hours | done, with an audit line |
| `refused` | the name is ambiguous or the room refused the post | done, with the room's sentence |

The hub holds no message today, and this is the one exception. It is safe because nothing else holds a CI notice:
there is no sender room to keep it. The pending list is retried on the growl ticker (30 s), and the name is resolved
again on every try, so a new session that takes the alias `qa` gets what the last holder missed. That is the "next
session with that alias" case, and it needs no extra mechanism. 24 hours matches what a room holds a cross-room say
for.

Pending rows for the same target collapse on retry: only the newest run per repo, workflow and branch is sent, and the
older ones are marked `expired` with the reason `superseded`. A director back after a day reads where CI stands, not
a backlog of reds.

### 5. Bounds and the record

- **Size.** `http.MaxBytesReader` at 512 KiB. A `workflow_run` event is tens of KB. Above it: 413.
- **Rate.** One token bucket for the hook, 60 per minute, burst 20. Above it: 429 with `Retry-After`. The dial policy
  already limits who can reach it, so this guards against a runaway loop, not a stranger.
- **Shape.** A body without `workflow_run.id`, `run_attempt`, `conclusion` and `repository.full_name` is 422 with one
  sentence. A `repository.full_name` not in `ci_hook.repos` (when that list is set) is 403.
- **Dedup.** Key `(repo, run_id, run_attempt)`, a `UNIQUE` constraint. A repeat answers 200 `{"duplicate":true}` and
  delivers nothing. A re-run is a new attempt and a new notice.
- **The record.** New hubstore table `ci_run`, added at the end of the migration slice: repo, workflow, branch, sha,
  run id, attempt, conclusion, url, received at, how it authenticated, and one `ci_delivery` row per target with its
  state and the room's answer. NEVER THE BODY. Rows older than 30 days are swept with the other hub sweeps. Each
  arrival is also one audit line `ci-hook` naming repo, workflow, branch, conclusion and the targets told. `change`
  reads the last `ci_run` row for the same repo, workflow and branch.
- **The answer to the sender** is 202 `{"run": <id>, "targets": <n>}` once the row is written, before delivery. The
  action fails a step on 300 and above, so a slow room never fails CI, and delivery runs on one worker fed by the
  pending list.

No new board tab. The audit pane already shows the lines, and a refused or expired delivery is the one thing a human
acts on, so it is an audit line of its own kind that the pane can filter on.

### 6. Atrium's own workflow: the script decides, the workflow carries

`scripts/ci-notify.sh` takes the hub URL and the secret as parameters, and an event file path only to replay a recorded
`workflow_run` event. With no event file it builds the body from the run's own context (`GITHUB_REPOSITORY`,
`GITHUB_RUN_ID`, `GITHUB_RUN_ATTEMPT`, `GITHUB_WORKFLOW`, the head branch and sha, `GITHUB_EVENT_NAME`), in the shape the
hub reads, and every one of those falls back to the repo's git state when run by hand. It writes the body, and POSTs it
when a URL is given. Locally it POSTs to `http://127.0.0.1:7778/_hub/ci-hook`, which is how a person tests a route
without GitHub. (`scripts/ci-notify.ps1` replays an event file only.)

The `notify` job in `.github/workflows/ci.yml`: `needs` every other job of the workflow and `if: failure()`, so a green
run starts nothing and docs and release never notify. It checks out, runs `scripts/ci-notify.sh` with no URL to write
the body, then the `ziti-hook` step with that body as `payload` and the secrets as inputs. The action is the transport,
in the place `actions/checkout` sits: it carries bytes and decides nothing. Every decision about what to send is in the
script. There is no `ci-notify.yml` and no `workflow_run` trigger. The route's `conclusion` filter stays as a second
guard.

### 7. Tests with no GitHub and no controller

The handler takes an `http.Request`, the ziti server takes a `net.Listener`, and delivery is an interface, so all of
it runs on loopback.

- **Handler** (`httptest`): secret right, wrong, missing, the rotated one inside and after its 24 hours. 413 on size.
  429 on rate. 422 on shape. 403 on an unlisted repo. A duplicate answers 200 and delivers nothing.
- **OIDC:** a test RSA key and a JWKS served by `httptest`, the URL injected. Wrong `aud`, expired, wrong `iss`, HS256,
  unknown `kid` (one refetch, then refused), `repository` not matching the payload.
- **Routing:** table tests over rules: every match, the dedup per card, `change` against a seeded last run, the default
  `kind`.
- **Delivery:** a fake deliverer answering each state. Parked then a live card holding the alias: one delivery.
  Unconfirmed: never retried. Pending past 24 hours on an injected clock: expired with one audit line. Three reds then
  a green while pending: only the green is sent.
- **Bind retry:** a fake listen function that fails twice and then succeeds. The hub keeps serving, the state row
  goes down then up, and two audit lines exist. An `Accept` error goes back to the loop.
- **End to end:** the existing link test harness, a hub and one room. POST a fixture to the loopback route and see the
  message on the card.
- **Script:** `ci-notify.sh` against a fixture event, a non-`workflow_run` event, and with no event file against a faked GitHub
  environment (a push and a pull request), comparing the body and the exit
  code.

A live ziti check is a manual step in the change notes, with the throwaway-listener bind tweak the ziti-hook design
already lists.

## Build order

1. Phase 1: the handler, the secret, routing, delivery with the pending list, `ci_run`, the loopback route, the
   script and workflow. Ziti bind with retry.
2. Phase 2: OIDC and `ci_hook.require`.

## Open

- The handle for the QA director and whether the default rules match what clint wants. The table is a setting, so
  this needs no code change.
