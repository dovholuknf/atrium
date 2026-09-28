# brief: atrium peer review using the mercurius review protocol

Audience: atrium agent. Requested by clint. Target: atrium backlog.

## goal

Agent-to-agent code review in atrium with mercurius-grade output, plus tool access that mercurius lacks.

## gap today

- mercurius reviewer: fresh subprocess (codex/claude/pi), sees only snapshotted artifacts. Strengths: cross-model,
  structured, bounded, calibrated, logged per round, reproducible. Weakness: no repo access, cannot read callers or
  neighbors, cannot run `go test`/`go vet`.
- atrium session: full agent with tools, watchable, in history. Weaknesses: free-form unbounded output, no calibration,
  no round records. Cross-model is solved: atrium launches any configured subprocess (codex, pi, ollama, ...).

## adopt from mercurius (source: github.com/michaelquigley/mercurius)

1. Output contract. The reviewer emits one JSON object; validate it and reject on failure. Schema:
   `internal/schema/reviewOutput.go` (+ embedded JSON Schema). Fields:
   - `verdict`: `ready_to_build` | `needs_changes` | `needs_discussion`
   - `summary`
   - `concerns[]`: `id`, `severity` (`blocker` | `major` | `minor`), `location`, `claim`, `rationale`, `suggestion?`
   - `questions[]`: `id`, `topic`, `why_it_blocks`
   - `advisory_notes[]`: `id`, `location`, `note`, `rationale`, `suggestion?`
   - Invariants: ids unique across all three arrays; `ready_to_build` requires empty `concerns` + `questions`.
2. Finding budget. `max_findings` caps `concerns` + `questions` combined; advisory notes are outside the cap.
3. Calibration. Inject `review_context` (posture, stakes, scope), `review_focus` (project invariants), and
   `settled_decisions` (guards: do-not-flag items) from the project's `mercurius.yaml`. Re-read every round.
4. Prompt. Reuse the mercurius code-review prompt: `prompt.BuildCodeReview` in `internal/prompt/prompt.go` (branch
   `http-support`, uncommitted as of 2026-09-28). It covers what to flag and not flag, fix sizing, the verdict and
   severity definitions, and the output rules.
5. Round records. Per round, persist the prompt snapshot, raw output, validated findings, and author dispositions
   (mercurius `record_round_notes`: accepted/rejected/deferred per finding id). Re-reviews read earlier rounds.
6. Independence. The reviewer starts as a fresh session, not a fork: it gets the task, the diff, and calibration only,
   with read-only tools and no author conversation context.

## residual gap

Tool access breaks mercurius's "what each round saw" snapshot guarantee: the repo can change under the reviewer. To
mitigate, record the commit SHA plus a dirty-tree hash per round, keep the reviewer read-only, and snapshot the diff
artifact. This narrows the gap but cannot close it.

## design options

- A (recommended): mercurius adds an `atrium` reviewer impl beside `codex`/`claude`/`pi`. Mercurius owns the protocol
  (prompt, schema, budget, calibration, logs); atrium owns the runner (a watchable session with repo access and any
  configured model). No duplicated protocol. It also fits the mercurius roadmap items quorums/panel (multi-reviewer
  per round).
- B: atrium re-implements items 1-6 natively. Duplicates the protocol, and the two drift over time.

## atrium-side requirements for option A

- Launch API callable by a non-interactive client: a session with a specified model/harness, cwd, read-only tool
  policy, and an initial prompt.
- Completion signal plus final-output retrieval (mercurius polls or blocks with a timeout; rounds can outlive MCP
  client timeouts).
- Final output returned as raw text for mercurius to extract and validate as JSON.
- Session id returned so mercurius can log a link to the watchable session.

## open questions

- The reviewer's view of the tree: worktree at a pinned SHA, or the live tree?
- Does a failed schema validation trigger one in-session repair turn, or fail the round as mercurius does today?
