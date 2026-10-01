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
  "second": {"state": "none", "summary": "", "error": ""}
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
| `run_dir` | string | the run folder, forward slashes. For display. Files are reached through a later route |
| `second` | object | the second opinion. `state` is `none pending done failed`, `summary` and `error` are text |

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

Walk progress (`walking N of M`, `walked`) is NOT a row field. It is read from `walk.txt` by the route that opens the
drawer, a later part, so it cannot go stale in the index.

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
| `200` | the same PR at the same head already has a row that is `queued fetching running` or `ready` | `{"pr": <row>, "created": false}`, nothing started |
| `200` | the same PR at the same head has a row that is `failed` or `aborted` | that row, reset and started again as `retry` does: `{"pr": <row>, "created": false}` |
| `400` | the body is not JSON, `url` is empty, `head` is not hex, or `why` is over 2000 characters | `{"error": "...", "code": "bad_request"}` |
| `422` | no recogniser matches the URL | `{"error": "no recogniser matches this", "code": "no_recogniser"}` |
| `422` | a recogniser matched and did not capture host, org, repo and a numeric `num` | `{"error": "...", "code": "not_a_pr"}` |
| `501` | the daemon wired no recogniser | `{"error": "no daemon wired", "code": "not_wired"}` |
| `503` | the store is halted | the halted body above |

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
log. Findings and walk state are served by routes a later part adds, and they are not in this body.

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
