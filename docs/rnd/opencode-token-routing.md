# OpenCode token routing: using clint's Kimi and OpenCode Go models where they pay off

Status: design by @rnd, 2026-10-02. Nothing built. Asked by clint: "i want rnd to figure out how we can token min using
opencode. i have kimi and various 'opencode go' llms from them. which are different than claude/codex... i want to
leverage it efficiently".

Sources, all read 2026-10-02:
- opencode.ai/docs/go, /docs/zen, /docs/cli, /docs/agents, /docs/permissions, /docs/plugins, and
  /legal/terms-of-service (updated 2026-08-15);
- platform.kimi.ai/docs/pricing/chat;
- the sst/opencode releases (v1.18.34, 2026-09-30);
- support.claude.com article 11049741.

In this repo:
- `scripts/opencode/atrium.js` (the plugin, at 4ea3dc66 and 6bce194f);
- `docs/backlog/review/review-new-security-audit-kimi.md` (@review's grading of the Kimi audit);
- `/Users/claude/r-opencode-bubbles-brief.md` (@runtime's transcript work).

`opencode models` was not run: opencode is not installed on m1mini, so the model list comes from the Go docs.
Secondary sources are marked **unverified**.

## 0. The answer

- **Use the cheap models where a Claude step already checks the result, and nowhere else.** The one measurement
  we have says it plainly. Kimi K2.7 Code's audit had mostly accurate file and line citations, but it got 0 of 27 severities
  right, tested nothing, and missed every finding that needed two files read together. So it finds and cites
  well, and judges badly.
- **Three fits are worth having now, each with Claude checking:**
  1. The PR runner's **second opinion**. It wants a different model family anyway, never holds the walk, and is
     settled by Claude.
  2. **First-pass search and reading**: "find every place that does X", with file and line answers that Claude
     then reads.
  3. **Drafts**: test scaffolding, doc first drafts, log triage summaries, all reviewed before they land.
- **Judgment stays on Claude**: severities, security verdicts, design, anything cross-file, and anything that lands
  without a review.
- **Measure before routing.** A one-week bake-off on real tasks from this week, graded by @review, decides which
  model gets which task kind.
- **One blocker before any unattended use.** OpenCode's terms forbid "any processes that run or are activated while
  you are not logged into the Services" and programmatic extraction of output. A factory worker is exactly that. Get
  a written answer from OpenCode before atrium runs Go models unattended (question 1).

## 1. What OpenCode Go costs and limits, against Claude

**OpenCode Go** is a flat plan: $10 a month (Go) or $40 (Go Plus, about 3 times the allowances).
- Each model gets a monthly allowance of token value, priced at Zen rates.
- The allowance is spent through rolling windows: 20% per 5 hours, 50% per week, 100% per month.
- At the limit, you drop to the free models, or "Use balance" bills Zen credit instead.
- No concurrency or rate limits are documented.
- It is reachable outside the opencode app through an OpenAI-compatible endpoint
  (`opencode.ai/zen/go/v1/chat/completions`).
- Retention is 0 days for most models, 30 days for Grok. The Muse Spark variants train on prompts.

| Go model | Monthly allowance (Go plan) | About requests a month | Zen price in / out / cached read, $ per Mtok |
| --- | --- | --- | --- |
| Kimi K2.7 Code | $60 | 6,750 | 0.95 / 4.00 / 0.19 |
| Kimi K2.6 | $60 | 5,750 | 0.95 / 4.00 / 0.16 (official page) |
| Kimi K3 | $15 | 490 | 3.00 / 15.00 / 0.30 |
| DeepSeek V4 Flash, V4.1 Flash | $30, $60 | 65,000, 130,000 | 0.14 / 0.28 / 0.028 (V4 Flash) |
| GLM-5.3 Flash | $60 | 31,580 | 0.15 / 0.50 / 0.03 |
| Qwen3.8 Flash | $30 | 27,000 | 0.15 / 0.47 / 0.016 |
| MiniMax M3 | $60 | 16,000 | 0.30 / 1.20 / 0.06 |

Zen is pay-per-token on the same prices, hosted in the US, with auto-reload of $20 under $5.

**Against Claude.**
- clint's Claude spend this week is not in hand. usage-3 (the token usage evaluation on m1mini) is still running and
  has not published.
- What we do know: PR 378 cost about $7 on Claude, and the pulls-view flow targets about $1.50
  (`docs/rnd/pulls-view-design.md` section 8, Sonnet 5.5 rates: input $2, 1h cache write $4, cache read $0.20,
  output $10 per Mtok). The orchestrator's relays alone are about 4 million context tokens a day
  (`docs/rnd/factory-refactor.md` section 2.2).
- Claude Max is per 5-hour session plus a weekly cap, and Anthropic publishes no hour figures. The secondary figures
  (Max 20x, $200: about 240 to 480 Sonnet hours and 24 to 40 Opus hours a week) are **unverified**.

**What that means.** Go's limit is a monthly dollar value per model, not a token rate. K2.7 Code's $60 of Zen value
a month, on a $10 plan, is about 63 million uncached input tokens, or 315 million cached reads, or 15 million output
tokens, or a mix of those. That headroom is real. But the
value of a cheap token depends on how often its output has to be redone, which is section 2's question.

## 2. Which work fits, by evidence

**The evidence we have is one graded run** (@review, `review-new-security-audit-kimi.md`): Kimi K2.7 Code's security
audit of atrium, 27 Critical and High findings.
- 6 were wrong.
- 20 were overstated.
- 1 was confirmed, and it was already documented.
- 0 held their stated severity.
- 6 pointed at real defects.
- The citations were mostly accurate (C9 named the wrong table, H9 the wrong HTTP verb).
- It ran no tests, and it missed every finding that needed two files read together.

On published benchmarks:
- K2.7 Code has vendor-only scores (Kimi Code Bench v2 62.0) and no SWE-bench or Terminal-Bench score.
- K3 has independent scores that are strong but **unverified** (SWE-bench 93.4%, Terminal-Bench 2.1 80.9%), and
  Moonshot says K3 "still trails" the top Claude and GPT models.

| Task kind | Fit | Why, from the evidence |
| --- | --- | --- |
| First-pass search ("where does X happen") | **yes, Claude reads the hits** | citations were mostly accurate. Finding is what it did well |
| Bulk reading and summarising (logs, long files, transcripts) | **yes, as input to a Claude step** | a summary is checked by the step that uses it |
| Log triage | **yes, first pass** | a ranking of suspects, not a verdict |
| Test scaffolding | **trial in the bake-off** | it "tested nothing" on its own. A scaffold is checked by running it |
| Doc first drafts | **trial in the bake-off** | prose quality is unmeasured. @review reads every doc anyway |
| Mechanical edits (rename, move, a pattern applied across files) | **trial in the bake-off** | checkable by tests and a diff read |
| PR runner second opinion (step 6) | **yes** | it must be a different model family, never holds the walk, and Claude settles every dispute (step 7) |
| PR runner panel reviewer forks | **no, not yet** | severities and cross-file reasoning are exactly what it got wrong. Revisit after the bake-off |
| Interview agents (spec interviews with clint) | **no** | clint's time is the cost, and a weak question wastes it |
| Severities, security verdicts, design, review verdicts | **no, Claude** | 0 of 27 severities right |
| Anything that lands unreviewed | **no** | nothing checks it |

**The bake-off.** One week, on real tasks from this week, each run on Claude Sonnet, Kimi K2.7 Code, Kimi K3 and one
Flash model (DeepSeek V4 Flash), and graded blind by @review:
1. **Search**: "find every caller of `launcherOf` and say which follow `report_to`". Graded on recall and precision
   against the code.
2. **Summarise**: one day's `run.log` and `review.json` from a PR run, as a one-screen summary. Graded on facts right
   and wrong.
3. **Triage**: the 12 red daemon tests on macOS from r-pr-run's report, sorted into causes. Graded against the known
   causes (unix socket paths, `/private/var`).
4. **Scaffold**: test stubs for one `internal/otelx` value class (from the OTel design). Graded on whether they
   compile and test the right thing.
5. **Second opinion**: re-read the merged findings of one PR run already walked (tlsuv 378). Graded on disputes that
   turn out right.

Each grade records:
- the quality score;
- the tokens spent;
- the Claude tokens it took to fix or verify the result;
- the wall time.

The cheap model wins a task kind only if quality plus the Claude fix cost beats Claude alone.

## 3. How atrium routes it

What exists:
- a runner row per harness, with a model on launch (`atrium_launch` takes `runner` and `model`);
- the opencode row on sg4-control, defaulting to `opencode-go/kimi-k2.7-code`;
- a lean worker's agent list.

Three additions, each small:

1. **A "cheap tier" as a named runner row, chosen by task kind.** The row is opencode with a fixed model per kind, set
   from the bake-off's results. A director picks it in `atrium_launch` by the row's name, like any runner. The
   director's brief template says which task kinds may use it (section 2's table). The model choice stays in
   the row, so moving a kind from K2.7 Code to K3 is a row edit, not a brief edit.
2. **The PR runner's second opinion on opencode.**
   - The recipe's `second` field names the opencode runner and model.
   - Step 6 runs `opencode run --format json -m <model>` on the bundle and the merged findings, read-only (permission
     config `edit: deny, bash: deny`).
   - **A PR can ship code that opencode runs.** opencode loads project plugins from `.opencode/plugins/` automatically
     at startup, and a plugin is code, so `edit: deny` and `bash: deny` do not stop it. This is the PR-hooks class
     @review closed in r-pr-run. So step 6 runs with its cwd in `<run>/work`, never in or under the PR checkout, and
     with no project config or `.opencode` from the PR. Whether opencode reads `opencode.json` from parent
     directories is **unverified**, and K3 checks it.
   - Step 7 settles on Claude, as designed.
   - Cost on Kimi: one read of the bundle (60k tokens at $0.95 per Mtok is about $0.06) plus output, on the flat plan.
3. **A verifier pass for anything a cheap model produces that a person will rely on.** This is landscape borrow 3.
   - A Claude fork of the task's cached context reads the cheap output and answers per item: holds, wrong, or
     overstated.
   - At Sonnet rates on a cached 60k context, each item costs about 60k cache read ($0.012) plus 2k output ($0.02),
     so **about $0.03 an item**, plus a one-time cache write of the 60k context ($0.24 at $4 per Mtok). A 20-item
     result costs about $0.88 to verify.
   - The bake-off's "Claude fix cost" is this number. Where the cheap model is often wrong, the verifier costs more
     than it saved, and that kind stays on Claude.

Fan-out, lean-agents style (one director, many cheap workers in parallel), comes after the bake-off. It multiplies
whatever quality the bake-off measured.

## 4. What the opencode runner still lacks

From the plugin (`scripts/opencode/atrium.js`) and @runtime's transcript work:

- **The gate fails open, and opencode's own ask cannot be answered.** The plugin waits on atrium's gate, and anything
  but an explicit deny lets the tool run under opencode's defaults, which allow bash and edits. The plugin's header
  says 1.18.34 has no trigger for opencode's own ask. The plugin docs read today list `permission.asked` and
  `permission.replied`, but as events a plugin observes, not a hook that returns a decision. The gap closes only if a
  plugin can answer an ask through the SDK's permission-reply call, which is **unverified**. K2 checks it.
- **The plugin directory's name.** The docs name `plugins/` (plural), and the plugin's header installs to
  `~/.config/opencode/plugin/`. K2 confirms the singular still loads.
- **"No gate when atrium is off"**, as the row's own label says. With the daemon down, nothing gates an opencode card.
  **For factory work the row should carry an opencode permission config instead of the defaults.** That means
  `bash: ask` for anything not on an allow list, `edit` limited to the worktree (path patterns for `edit` are
  **unverified**), `external_directory: deny` and
  `webfetch: deny`, with opencode's `doom_loop` left at ask. Then a dead daemon fails closed. That turns the
  second-prompt problem the header describes into a stop, which is the right failure for an unattended worker.
- **Subagents** are reported (a child session's created and idle), but a subagent's tool calls go through the same
  fail-open gate. Under the permission config above they are covered by opencode's own rules.
- **Resume.** A resumed session (`--session`) is never "created", so the plugin notes it on its first event. Resume is
  through `opencode run -s <id>` or `-c`, and `--fork` forks on continue, which is what a PR runner second-opinion
  fork would use.
- **Transcript bubbles.** @runtime's r-opencode-bubbles work reads `opencode export <session>`, so the phone and board
  draw replies as they do for Claude.
- **Usage.** Atrium's usage rows read Claude transcripts. An opencode card's tokens are not counted today, and the
  bake-off needs them. `opencode export` carries per-message token counts **(unverified)**, so the same reader can
  feed `session_usage`.

## 5. Stages

### K1. The bake-off. @rnd runs it, @review grades. About 2 days. Needs question 1 answered for unattended runs.

- The five tasks of section 2, four models each, on a room with an opencode login.
- **Acceptance:** a graded table of quality, tokens, the Claude fix cost and time per task kind and model, published
  as an update to this doc.

### K2. The opencode row for factory work. @runtime. About 1 day.

- The permission config of section 4, so a dead daemon fails closed.
- Usage rows for opencode cards, from `opencode export`.
- A check of whether a plugin can answer `permission.asked` through the SDK, and that `plugin/` (singular) still
  loads, against the installed binary.
- **Acceptance:** with the daemon stopped, an opencode card's `rm` and an edit outside its worktree are refused. A
  card's tokens show in its usage.

### K3. The second opinion on opencode. @runtime. About 1 day. After K2.

- The recipe's `second` names opencode and a model. Step 6 runs read-only on it, with its cwd in `<run>/work` and
  no project config or `.opencode` from the PR. Step 7 settles on Claude.
- **Acceptance:** a PR run shows a Kimi second opinion with disputes settled, on the flat plan. A marker plugin at
  `src/.opencode/plugins/marker.js` in the PR checkout never fires, and a parent-directory `opencode.json` is shown to
  be ignored.

### K4. The cheap tier and the verifier. @runtime and @review. About 2 days. After K1.

- A runner row per task kind the bake-off passed, and the briefs' list of allowed kinds.
- The verifier fork on that output, and its cost recorded.
- **Acceptance:** one week of a passed task kind on the cheap tier, verified, with the Claude tokens saved measured
  against the same week's Claude-only baseline.

## 6. Questions for clint, in plain words

1. **The terms.** OpenCode's terms forbid anything that runs while you are not logged in, and programmatic extraction
   of output. Atrium's workers would do both. Will you ask OpenCode in writing whether unattended agent use under your
   Go plan is allowed, before atrium runs their models without you watching? **Suggested: yes. Until then, use them
   only in cards you are watching.**
2. **Which code they may see.** Go's models mostly keep nothing, but they are run by other companies, some outside
   the US. Should cheap models only see public repos, such as atrium and openziti, until you say otherwise for a
   private one? A PR from outside to a public repo is public too. A PR on a private repo is not. **Suggested: yes,
   public repos and public PRs only.**
3. **The bake-off.** Run the one-week comparison on real tasks from this week (Claude against Kimi K2.7 Code, Kimi K3
   and DeepSeek Flash, graded by @review) before routing any work to them? **Suggested: yes, after the pause.**
4. **Go or Go Plus.** The $10 plan's monthly limit per model is enough for the bake-off and the PR second opinion.
   Upgrade to the $40 plan only if the bake-off routes a task kind with real volume? **Suggested: stay on $10 until
   the bake-off says otherwise.**
