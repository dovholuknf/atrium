# The pulls API: routes and JSON (r-pr-store, part 0)

Fixed before the store exists so @ui can build the pulls view against it. Design:
`docs/rnd/pulls-view-design.md`. Nothing here changes after it is committed. Later parts may ADD a field or a route,
and never rename or retype one.

All routes are on the human listener (`:7778`), JSON in and out, and none exists on the agent listener. A board pause
does not apply to any of them: a PR clint asked for is not a director's work (design section 6). No route makes a card.

## The row

One shape, used by the list, the detail, the POST answers and the SSE event.

```json
{
  "id": "pr_01k8x2m4q7",
  "url": "https://github.com/openziti/tlsuv/pull/378",
  "host": "github.com",
  "org": "openziti",
  "repo": "tlsuv",
  "org_repo": "openziti/tlsuv",
  "number": 378,
  "title": "tls engine: session resumption on reconnect",
  "why": "the author asked for a security look",
  "head": "ad5ddf4c0e7d1b2a9f3e4d5c6b7a8f9e0d1c2b3a",
  "head7": "ad5ddf4",
  "state": "running",
  "run_state": "panel",
  "run_error": "",
  "cost_usd": 0.71,
  "started_at": "2026-10-01T14:02:11Z",
  "ready_at": "",
  "created_at": "2026-10-01T14:02:10Z",
  "archived_at": "",
  "run_dir": "D:/worktrees/claude/reviews/github-openziti-tlsuv/pr-378-ad5ddf4",
  "second": {"state": "none", "summary": "", "error": ""},
  "author": "ekoby",
  "findings": {"high": 0, "med": 6, "low": 7, "nit": 4, "leak": 1},
  "walk": {"done": 0, "skipped": 0, "deferred": 0, "open": 17},
  "walker_task": ""
}
```

| field | type | meaning |
| --- | --- | --- |
| `id` | string | the row's key. Stable for the life of the row |
| `url` | string | the URL as the caller sent it, trimmed |
| `host`, `org`, `repo` | string | captured by the recogniser that matched `url` |
| `org_repo` | string | `org + "/" + repo`, for display and filtering |
| `number` | int | the PR number |
| `title` | string | what `gh` last said. `""` until the fetch step has run. Observed, never typed |
| `why` | string | the caller's reason, `""` when none. Up to 2000 characters |
| `head` | string | the full SHA the review is of. `""` until the fetch step, or until the caller gave one |
| `head7` | string | the first 7 characters of `head`, `""` when `head` is |
| `state` | string | one of `queued fetching running ready failed aborted`, below |
| `run_state` | string | the step in progress: `fetch prime panel verify critics merge second settle write`, else `""` |
| `run_error` | string | `""` unless `state` is `failed`, then `<step>: <first line of why>` |
| `cost_usd` | number | what the run has cost so far, from the forks' recorded usage. `0` before any |
| `started_at` | string | RFC3339 UTC, when the runner began. `""` before |
| `ready_at` | string | RFC3339 UTC, when `state` became `ready`. `""` otherwise |
| `created_at` | string | RFC3339 UTC, when the row was made |
| `archived_at` | string | RFC3339 UTC, `""` unless archived. P1 never sets it, an archive route comes later |
| `run_dir` | string | the run folder, forward slashes. For display. Files are reached through the routes below |
| `second` | object | the second opinion. `state` is `none pending done failed`, `summary` and `error` are text |
| `author` | string | the PR author's login, as `gh` last said. `""` until the fetch step has run. Observed, never typed |
| `findings` | object | finding counts by severity, and `leak` for findings carrying a `Leak:` line. A leak is also counted under its severity |
| `walk` | object | the walk states of the findings, from `walk.txt`. `done skipped deferred open` sum to the finding count |
| `walker_task` | string | the card id of the walker, `""` when none was launched or set |

Times are text and unset is `""`, never `null`, so a client does not branch on two kinds of empty.

`state` is a fact about the run, written by the daemon and never by a person:

| state | meaning |
| --- | --- |
| `queued` | known, not started. Waits for a runner slot or, for a source-found PR, for a `review` click |
| `fetching` | the diff and a checkout at the head are being fetched (`run_state` is `fetch`) |
| `running` | the recipe is running. `run_state` names the step |
| `ready` | finding files and `walk.txt` are written. Ready to walk |
| `failed` | a step failed or the budget stopped it. `run_error` says which and why. `retry` reruns it |
| `aborted` | clint stopped the run. The run folder is deleted. The row stays |

`findings` and `walk` are never stored. Every route that returns a row reads the run folder (`findings/` and `walk.txt`)
at that moment, so they cannot go stale in the index. Both objects are always present with all their keys, and every
count is `0` while the row is not `ready` or when the folder is gone. The row text `walking N of M` is `walk.done +
walk.skipped + walk.deferred` of the finding count, and `walked` is `walk.open == 0` and `walk.deferred == 0` with a
finding count above `0`. The client works those out. The SSE event carries them too, read when the event is sent.

Every error body is JSON with two keys: `{"error": "<a sentence>", "code": "<stable word>"}`. A client branches on
`code` and shows `error`. A halted store answers 503 with the body the rest of the API uses for it:
`{"error": "atrium is halted and will not recover without a restart", "cause": "...", "halted": true}`.

## POST /v1/prs

Starts a review of a PR. The one door every intake uses (design section 6).

Request:

```json
{"url": "https://github.com/openziti/tlsuv/pull/378", "why": "the author asked for a security look", "head": ""}
```

`url` is required. `why` and `head` are optional. `head` is a full or 7 to 40 character hex SHA when the caller already
knows it (gwt does). Without it the run folder is named `pr-<n>-pending` and the fetch step fills the head in.

The URL is matched against the recogniser table (`POST /v1/recognise` uses the same table). The recogniser must capture
`host`, `org`, `repo` and `num`. The daemon creates the row, creates the run folder, and hands the run to the runner.
The answer is the row AFTER the runner's `Start` returned, so a runner that fails at once is already `failed`.

Answers:

| status | when | body |
| --- | --- | --- |
| `201` | a row was made | `{"pr": <row>, "created": true}` |
| `200` | no `head` was sent and the PR already has a row that is `queued`, `fetching` or `running` (the folder moved off `pending`, so the pull request is the identity), or the same PR at the same head already has a row that is `queued fetching running` or `ready` | `{"pr": <row>, "created": false}`, nothing started |
| `200` | the same PR at the same head has a row that is `failed` or `aborted` | that row, reset and started again as `retry` does: `{"pr": <row>, "created": false}` |
| `400` | the body is not JSON, `url` is empty, `head` is not hex, or `why` is over 2000 characters | `{"error": "...", "code": "bad_request"}` |
| `422` | no recogniser matches the URL | `{"error": "no recogniser matches this", "code": "no_recogniser"}` |
| `422` | a recogniser matched and did not capture host, org, repo and a numeric `num` | `{"error": "...", "code": "not_a_pr"}` |
| `501` | the daemon wired no recogniser | `{"error": "no daemon wired", "code": "not_wired"}` |
| `503` | the store is halted | the halted body above |

`reviews_root` is a daemon setting. Changing it orphans existing rows: a row keeps the folder it was made in, and the
drawer routes answer `403 outside` for a folder outside the new root until the folders or the setting are moved back.

The state checks of `retry` and `abort` are made in the same statement that changes the row, and `start` is serialised
with them, so two racing requests for one row get one success and a `409` carrying the state it is now in.

## GET /v1/prs

The index. Newest first by `created_at`, with `id` breaking ties.

Query, all optional: `state=<one of the six>` (repeatable), `org_repo=<org/repo>`, `archived=1` to include archived
rows, which are left out otherwise.

```json
{
  "prs": [ <row>, <row> ],
  "counts": {"queued": 0, "fetching": 0, "running": 1, "ready": 2, "failed": 1, "aborted": 0},
  "nav_count": 3
}
```

`counts` covers every non-archived row, whatever the filter, and always has all six keys. `nav_count` is the number on
the `pulls` nav item: rows in `ready` plus rows in `failed`, non-archived, which is the reviews that are waiting on
clint. It rides the alerting path as a question does. An empty index is `{"prs": [], "counts": {...}, "nav_count": 0}`,
never `null`.

Errors: `400 {"code": "bad_request"}` for an unknown `state`, `503` when halted.

## GET /v1/prs/{id}

One row, plus the tail of its log.

```json
{"pr": <row>, "run_log": "14:02:11 fetch start\n14:02:40 fetch end 0.00\n"}
```

`run_log` is the last 8 KiB of `run.log` from the run folder, cut at a line start, `""` when there is no folder or no
log. The findings themselves are served by `GET /v1/prs/{id}/findings`, below, and are not in this body.

Errors: `404 {"error": "no such pr", "code": "not_found"}`, `503` when halted.

## POST /v1/prs/{id}/retry

Reruns a `failed` row from the step that failed. Body ignored. Sets `state` to `queued`, clears `run_error`, and hands
the run to the runner. Answers `202 {"pr": <row>}` with the row after `Start` returned.

Errors: `404 not_found`. `409 {"error": "only a failed or aborted row can be retried", "code": "not_retryable",
"state": "<its state>"}`. An `aborted` row retries from the start, in a new run folder. `503` when halted.

## POST /v1/prs/{id}/abort

Stops a run. Body ignored. Tells the runner to stop, deletes the run folder, `steps/` and `review.json` with it
(standing rule 44), and sets `state` to `aborted` with `run_error` `""`. The row stays. Answers `200 {"pr": <row>}`.

Errors: `404 not_found`. `409 {"error": "only a queued, fetching or running row can be aborted", "code":
"not_abortable", "state": "<its state>"}`. A `ready` row is not aborted, since its folder holds work clint may have
walked. `503` when halted.

## POST /v1/prs/{id}/start

The `review` click on a `queued` row: a source-found PR without clint's review requested (design section 6). Body
ignored. Hands the run to the runner as `POST /v1/prs` does for a new row. Answers `202 {"pr": <row>}` with the row
after `Start` returned.

Errors: `404 not_found`. `409 {"error": "only a queued row can be started", "code": "not_startable", "state":
"<its state>"}`. `503` when halted.

## The drawer routes

Every route below resolves its file through `internal/safepath` against the row's `run_dir`, and `run_dir` itself must
be inside `reviews_root`. A path that resolves outside the folder, a symlink that leaves it, and a file that is not
there all answer the same `403 {"error": "outside the review folder", "code": "outside"}`, so the route is not an oracle
for what is on the machine. No request carries a path: a finding is named by its `key`, and the daemon maps that to a
file in `findings/`.

### GET /v1/prs/{id}/findings

Every finding of the run, parsed, in walk order. Answers `200`:

```json
{
  "pr": <row>,
  "findings": [
    {
      "key": "f-3a9c01d4e2",
      "position": 1,
      "file": "01-med-share.go-L165.txt",
      "sev": "med",
      "path": "controller/share.go",
      "line": 165,
      "code": "committed = true",
      "link": "https://github.com/openziti/zrok/pull/1277/files#diff-a5528fbeR165",
      "comment": "* LLM review says ...\n* Suggested fix: ...\n* Add a test to `x_test.go`: ..., expect ...",
      "evidence": {"Id": "3a9c01d4e2", "Cause": "pre-existing", "Raised by": "go-security-reviewer (medium)"},
      "leak": "",
      "hunk": "@@ -160,6 +160,9 @@\n ...",
      "walk": {"state": "open", "at": "", "url": ""},
      "text": "https://github.com/openziti/zrok/pull/1277\nMED controller/share.go line 165: committed = true\n...",
      "hash": "7f1c..."
    }
  ]
}
```

| field | meaning |
| --- | --- |
| `key` | stable across renumbering and re-anchoring: `f-` plus the `Id:` line of Evidence when there is one, else `f-` plus the first 10 hex of SHA-256 over `path`, a newline and `code` |
| `position` | 1-based place in the walk, the file's `NN` prefix |
| `file` | the file name now, which changes when the walker renumbers. Never shown outside a menu |
| `sev` | `high`, `med`, `low` or `nit`. A finding named `NN-blocking-...` and labelled BLOCKING is `high` here and counts under `high` |
| `blocking` | bool, additive. `true` only for a BLOCKING finding, so the board can tell it from a plain `high` |
| `path`, `line`, `code`, `link` | from the label line and the deep link line. `line` is `0` and `link` is `""` when the file has none |
| `comment` | the bullets, verbatim, before Evidence |
| `evidence` | each `Key: value` line of Evidence, keys as written, in an object. `{}` when there is no Evidence |
| `leak` | the `Leak:` value, `""` when none |
| `hunk` | the lines of `pr.diff` around `line`, `""` when `pr.diff` is missing or has no hunk there |
| `walk` | `state` is `open done skipped deferred`. `at` is RFC3339 UTC and `url` the comment link, both `""` unless set |
| `text` | the whole file, line ends normalised to `\n` |
| `hash` | what a write must quote back: the hex SHA-256 of the file's bytes on disk |

The answer carries an `ETag` over the findings' modification times and `walk.txt`'s. A request with a matching
`If-None-Match` answers `304` with no body. Errors: `404 not_found` (no such row), `409 {"error": "the review is not
ready", "code": "not_ready", "state": "<its state>"}` for a row that is not `ready`, `403 outside`, `503`.

### PUT /v1/prs/{id}/findings/{key}

Writes one finding's text, behind a content-hash precondition. It mirrors `PUT /v1/tasks/{id}/files/text`.

```json
{"text": "...", "hash": "7f1c...", "eol": "\n"}
```

`hash` is required and is the `hash` the client last read. `eol` is `\n` or `\r\n` and defaults to `\n`. The body is
limited to 2 MiB. Answers `200 {"ok": true, "hash": "...", "key": "f-..."}` with the new hash and the key the
finding now has (an edit to the label line or `Id:` can change it).

Errors:

| status | body |
| --- | --- |
| `400` | `{"error": "a write has to say what it was based on", "code": "bad_request"}` for no `hash`, or the body is not JSON or is over the limit |
| `403` | `outside` |
| `404` | `{"error": "no such finding", "code": "not_found"}` for an unknown row or key |
| `409` | `{"error": "that file changed while you were editing it. nothing was written.", "code": "changed", "text": "<its text now>", "hash": "<its hash now>"}` |

### POST /v1/prs/{id}/findings/{key}/walk

Sets one finding's state in `walk.txt`. The daemon replaces that finding's line and nothing else, under a lock, so the
client sends no hash and a concurrent mark of another finding is not lost.

```json
{"state": "done", "url": "https://github.com/openziti/zrok/pull/1277#discussion_r1234"}
```

`state` is `done`, `deferred`, `skipped` or `open`. `url` is optional, only meaningful with `done`, and must be an
`http` or `https` URL under 2000 characters. `at` is set by the daemon to now for every state but `open`, which clears
`at` and `url`. Answers `200`:

```json
{"ok": true, "walk": {"state": "done", "at": "2026-10-01T14:20:00Z", "url": "..."}, "counts": {"done": 1, "skipped": 0, "deferred": 0, "open": 16}}
```

Errors: `400 {"code": "bad_request"}` for an unknown `state`, and `400 {"code": "bad_url"}` for a `url` that is not
`http` or `https`, `403 outside`, `404 not_found`, `503`.

### POST /v1/prs/{id}/walker

Launches the walker, or records one.

```json
{"action": "launch"}
```

`action` is `launch` (the default when the body is empty), `set` with `"task": "<card id>"`, or `clear`.

`launch` starts a runner from the recipe's `walker_brief` with the run folder as its working directory (design
section 7). The card carries the tags `atrium:subagent`, `dept:review`, `review`, `pr` and `pr:<org>/<repo>#<n>`, and
its id is stored as the row's `walker_task`. A `ready` row only. If the row already names a live walker, nothing is
launched. Answers `201 {"pr": <row>, "task": "<card id>", "launched": true}`, or `200 {"pr": <row>, "task":
"<card id>", "launched": false}` for the existing walker. `set` and `clear` answer `200 {"pr": <row>}`.

Errors: `400 bad_request` for an unknown `action` or `set` with no `task`. `404 not_found`. `409 {"code": "not_ready",
"state": "<its state>"}` for `launch` on a row that is not `ready`. `500 {"error": "...", "code": "launch_failed"}`
when the runner would not start. `501 {"code": "not_wired"}` in a build with no launcher. `503`.

## SSE: event `pr`

On `GET /v1/events`, the stream the board already holds. One event whenever a row is created or any field changes,
including `cost_usd` and `run_state` as steps finish:

```
event: pr
data: {"pr": <row>}
```

`data` is the same `{"pr": <row>}` as a POST answer, so one function renders both. There is no delete event, because no
route deletes a row. A client that missed events refetches `GET /v1/prs`. Events for one row arrive in the order the
changes were made, so a client replaces the row it holds with the one in the event.
