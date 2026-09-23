# Personas: specialist agents that keep what they learn, on any runner

Status: stage 1 built (the pack and the claude render). Stage 5a measured codex and ollama, and stage 5b built
their adapters and the generated memory index. Gemini is unmeasured.

## The problem

clint has eleven specialist agents: go-security-reviewer, codebase-steward, c-systems-reviewer, csharp-expert,
functional-tester, nonfunctional-tester, network-expert, windows-enterprise-veteran, doc-humanizer, persona and
style-harvester. The review panel skill (`dotfiles/claude/skills/review-panel`) picks from them by the files a
diff touches and runs them in parallel.

Four things are wrong with how they live today.

1. **What they learn is not backed up.** Nine of them carry `memory: user`, which gives each one a directory under
   `~/.claude/agent-memory/<name>/`. That directory is real and in use: codebase-steward holds fourteen lessons,
   such as "every controller client must thread the zitified transport". None of it is in git. A disk failure
   loses it, and nobody can see how it changed.
2. **A rename orphans the memory.** The directory is keyed by the agent's name. `grizzled-c-greybeard` and
   `pedantic-go-sec-dev` are still there, empty, from agents that were renamed. A rename with lessons in it would
   silently start the agent over.
3. **Nothing checks a lesson.** A lesson is written by the model, in the middle of a review, and read back as
   truth on every later review. A wrong one compounds. There is no step where a human looks at it, and no way to
   tell whether an agent got better or worse after a change to its prompt.
4. **They only run on Claude.** The definition is a Claude Code subagent file. Codex, gemini, ollama and the next
   runner cannot use it, so a second opinion from a different model means writing the persona again.

## Principles

- **Every file in a persona is plain markdown or yaml that any LLM provider can read.** Nothing depends on one
  runner's format. Runner-specific files are generated from the neutral ones.
- **Only the human pushes.** No agent, script, scheduled task or atrium feature ever pushes the pack, or holds a
  credential that could. What leaves the machine is decided by the person who runs `git push`.
- **History is git.** A decision about a persona is a commit with a message that says what was decided. There is no
  second log beside it.

## What a persona is

A persona is a **folder**, not a file. It holds one runner-neutral definition and the durable content it has
built up, split by who writes it and how far it is trusted.

```
dotagents/personas/<id>/
  persona.yaml            identity and metadata: id, name, description, when to use, what it reviews, tiers
  persona.md              the instructions body, runner neutral: voice, mandate, method, output contract
  checklists/             reusable, human-written checklists the persona loads on demand
    go-http.md
    go-concurrency.md
  knowledge/              REVIEWED facts, written or promoted by a human. Trusted.
    _general.md           applies everywhere
    github/openziti/ziti.md
    github/dovholuknf/atrium.md
  memory/                 MODEL-WRITTEN lessons, unreviewed. Read as hints, never as rules.
    MEMORY.md             GENERATED index, one line per lesson from its frontmatter, in Claude's native format
    github-openziti-channel_multilistener.md
    rejected.md           what this persona got wrong, one line each, appended by the review conductor
  evals/                  golden cases: a diff and the findings a good review must produce
    0001-http-no-timeout/
      diff.patch
      expect.json
  render/                 GENERATED per-runner files. Never edited by hand.
```

### Why each piece exists

- **`persona.yaml` apart from `persona.md`.** The yaml is what tools read: the panel's selection rules, the atrium
  catalog, the render step. The markdown is what a model reads. Mixing them is how the Claude frontmatter ended up
  listing Gmail and HubSpot authentication tools on a C reviewer.
- **`checklists/`** keeps the body short. A persona reviewing a Go HTTP handler loads `go-http.md` and not the
  crypto list. Loading on demand is the token budget.
- **`knowledge/` against `memory/`** is the core of the design. Memory is what the model believes. Knowledge is what
  a human agreed with. Every review reads both, and the instructions say plainly that knowledge wins a conflict and
  memory is a hint to check. Promoting memory to knowledge is the lessons review below.
- **`knowledge/` is keyed by `<host>/<org>/<repo>`,** the layout dotagents already uses for per-repo `CLAUDE.md`. A
  review of an openziti/ziti diff loads `_general.md` and `github/openziti/ziti.md` and nothing else. A persona that
  has reviewed forty repos does not carry forty repos into every review.
- **Every `memory/` file names its repo and its reason:** a `repo:` line in its frontmatter (or `repo: general`),
  and a one-line `Why:` in its body saying what taught it. A review loads
  the general ones and the target repo's, the same way it loads knowledge. It also lets the person committing see
  which repositories a change quotes. The file is named `<repo slug>_<topic>.md`, where the slug is the repo key
  with `/` as `-` (`github-openziti-ziti_channel-close.md`), so a directory listing groups by repo. The name is a
  convention for people. The render step reads only the frontmatter.
- **`memory/rejected.md`** records findings that were wrong, so the same persona stops raising them. See "What a
  wrong finding teaches" below.
- **`evals/`** is what makes "restore a previous state" mean something. A persona change is a commit. Running the
  evals before and after says whether it got better. Without evals a revert is a guess.
- **`render/`** keeps the per-runner files out of the hand-edited tree, so there is one source of truth.
- **`memory/MEMORY.md` is generated** by the render step from each lesson's frontmatter. No runner edits it. Two
  runners writing lessons at once each add one file and never touch a shared one, and a runner that ignores the
  index format cannot corrupt it.

### The id is not the name

`persona.yaml` carries a stable `id` (`go-security-reviewer`) and a display `name` that may change. Everything keys
on the id: the folder, the native memory directory, atrium's catalog. A rename edits `name` and nothing moves.

## Runners

The body in `persona.md` is written once. A render step (`dotagents/scripts/render-personas.ps1`) turns each
persona into what each runner can load, for each runner listed in the persona's `runners:` line
(`runners: [claude, codex, ollama]`). `render-personas.ps1 -Check` exits non-zero when `render/` or any
`memory/MEMORY.md` does not match its sources, and is the only check.

| Runner | What it loads | Memory | Tools | Measured? |
| --- | --- | --- | --- | --- |
| claude code | `render/claude/<id>.md`: subagent frontmatter from the yaml, body from the md | native: `~/.claude/agent-memory/<id>` is a SYMLINK to `memory/` | from the yaml's tier | yes, today's setup |
| codex | `render/codex/AGENTS.md`, copied into a persona run directory beside `TARGET.md` | instructed: read by absolute path, write one lesson file into `memory/` | codex's own, started with `--sandbox workspace-write --add-dir <persona>/memory` | yes, codex-cli 0.154.0 |
| gemini | not rendered | instructed, as codex, once measured | gemini's own | no, quota exhausted when stage 5 ran |
| ollama | `render/ollama/Modelfile`: `FROM` the yaml's `ollama_model:` (default `gemma4:latest`), the body as `SYSTEM`. The diff, the repo's knowledge and memory files, and `rejected.md` go in the prompt. | read only, through the prompt. It cannot write. | none | yes, ollama 0.32.14, qwen3:8b and gemma4 |
| next runner | one adapter function in the render script, plus one row in atrium's `docs/other-runners.md` | whichever of the three modes it supports | its own | per runner |

Each runner was measured the way atrium's `docs/other-runners.md` measured codex's hooks: a probe persona, a scratch
home, and a record of what the runner actually loaded. The results are in that page's "Personas on other runners"
section.

The codex and ollama renders cut the body at its `# Persistent Agent Memory` heading. That section is Claude Code's
native memory instructions, with a `~/.claude/agent-memory` path and a step that edits `MEMORY.md` by hand, and it is
wrong on every other runner. The claude render keeps it for now, because stage 1 froze that render byte for byte.
Moving the section into the claude adapter, and teaching it the `repo:` and `Why:` lines, is one render change for
clint to approve. Until then a claude-written lesson reaches the contract at its first lessons review.

A persona on a runner without tools is still useful. An ollama reviewer cannot open files, but handed a diff and
the right knowledge file it can still produce findings in the panel's schema. It is a cheap second opinion, and
measurement says it needs the verify pass: see "Read only" below.

### Memory, per runner

Claude's native memory is the only automatic one. The pack treats it as the model and the others follow it by
instruction.

Every runner that writes a lesson writes the same thing: one ordinary file in `memory/`, with `name`,
`description` and `repo:` in its frontmatter and a one-line `Why:` in its body. There is no inbox. `MEMORY.md` is
generated from those files by the render step, so no runner ever edits the index, and a new lesson file shows up as
`-Check` failing until the render runs. That failure is also how clint sees that a review learned something.

- **Native (claude).** The symlink makes the native directory BE `memory/`. Nothing is copied, so nothing drifts.
  Claude Code's own memory instructions still tell the model to add a line to `MEMORY.md`. The next render replaces
  the index with the generated one, which lists that file anyway, so the edit is harmless and short-lived.
- **Instructed (codex, gemini).** The rendered `AGENTS.md` names the persona directory through `TARGET.md` and says:
  read `knowledge/`, `memory/MEMORY.md` and `rejected.md` by absolute path. To record a lesson, write one new file
  in `memory/` in the shape above. Never edit `MEMORY.md`, `rejected.md` or another lesson. The run grants write
  access to `memory/` and nothing else in the pack (`--add-dir` on codex), so the persona cannot edit its own
  definition.
- **Read only (ollama).** It reads what it is handed and writes nothing. What it gets wrong still reaches
  `rejected.md`, because the conductor writes that file, not the reviewer. Measured on one Go diff with SSRF, path
  traversal, an unbounded read and no client timeout: both local models returned a parseable json array with every
  schema field. qwen3:8b missed SSRF and path traversal and raised two false findings. gemma4 found path traversal,
  the timeout and the unbounded read, missed SSRF in the array, over-rated three findings as `blocking`, and added
  prose after the json. Neither cited file:line in `evidence`, and line numbers were off. So the shape fits the
  panel. The content is a second opinion that the verify pass must check, never a reviewer on its own.

## What a wrong finding teaches

When the review panel's verify pass refutes a finding, or clint skips one when choosing which to apply, one of two
things is true. The conductor picks by asking: would a different persona make the same mistake?

- **No: this persona misread something.** The conductor appends one line to that persona's `memory/rejected.md`:
  the date, the repo, the claim, and why it was wrong. A line applies only to reviews of its own repo, unless its
  repo is `general`, and the dispatch prompt says so. The next review by that persona, on any runner, reads the
  file and does not raise it again. Example: "c-systems-reviewer flagged a leak in `ziti_conn_close`, but the
  buffer is freed by the loop's close callback."
- **Yes: the code is intended, and any reviewer would trip on it.** That is a fact about the repo, not about a
  persona. It goes where every runner already looks for that repo: the repo's file in dotagents (`CLAUDE.md`, with
  `AGENTS.md` for other runners) and `settled_decisions` in its `mercurius.yaml`. Example: "atrium's permission hook
  fails open when the daemon is unreachable, on purpose. Do not flag it."

Both are ordinary file changes in dotagents, so the next commit captures them and atrium's nag reminds clint to
make it. Whether `rejected.md` earns its place is an open question: the lessons review will show whether it stops
repeat findings.

## The review panel

Three changes to the skill. None changes how a review reads to clint.

1. **Selection comes from `persona.yaml`.** Each persona declares what it reviews:
   ```yaml
   reviews:
     paths: ["**/*.go"]
     surfaces: [http, crypto, concurrency, input-validation]
     skip_when: "docs-only or generated-only diff"
   ```
   `paths` and `surfaces` are matched mechanically. `skip_when` is free text the conductor reads and applies with
   judgment, the way the skill's selection rule already works. The skill's mapping table becomes the fallback. Adding a persona adds a reviewer with no edit to the skill.
2. **Each reviewer gets only what applies.** The dispatch prompt names the persona's `knowledge/_general.md`, the
   target repo's knowledge file, the general and target-repo memory files, `memory/rejected.md`, and the checklists
   the yaml maps to the surfaces in this diff.
3. **Wrong findings are written down,** as above, after the verify pass and after clint picks what to apply.

**Diversity, off by default.** The panel may run one persona on two runners, such as go-security-reviewer on claude
and on codex. Agreement raises confidence the way two different agents agreeing already does. Disagreement goes to
the verify pass. It doubles the cost, so it is asked for, never assumed.

## The lessons review

The step that keeps the pack honest. On demand, because it needs clint.

For each persona with anything new since its last review:

1. **Memory diff.** What `git diff` shows in `memory/`. For each lesson: promote to
   `knowledge/`, keep as memory, or delete. The lesson's own `Why:` line says what taught it. No session id or run
   id is recorded: the reason is what matters, and it lives in the lesson.
2. **Rejections.** New lines in `rejected.md`. A cluster of the same mistake becomes one line in the persona's
   `persona.md` or a checklist, and the individual lines go.
3. **Evals.** If the persona's prompt, checklists or knowledge changed, run its evals and show the result beside
   the last one.

The output is a commit in dotagents whose message says what was promoted, deleted and changed, and ends with one
trailer line per persona reviewed: `Lessons-reviewed: <persona-id>`.

**What "new" means is a git fact.** A review of a persona covers every change under its folder since the most recent
commit carrying `Lessons-reviewed: <persona-id>`, plus anything uncommitted. A persona that has never been reviewed
covers its whole history. There is no marker file and no state outside git.

## Backup: commit and push are clint's

Everything under `dotagents/personas/` is committed. dotagents is a private repo that clint already maintains.
`render/` is committed too, so a machine that has not run the render step still works.

There is no sync script and no scheduled task. Atrium watches the pack and asks.

- **Uncommitted.** Memory and rejections change during reviews. When the pack has uncommitted changes, the board
  shows "persona pack: 4 files changed, not committed" with the personas they belong to.
- **Unpushed.** When dotagents has commits its upstream does not, the board shows "persona pack: 2 commits not
  pushed".
- **The nag escalates** on the same backoff atrium uses for a stuck agent: 1m, 2m, 5m, 10m, 30m, 1h, then hourly up
  to 24h, and resets when the state changes. It can be snoozed.
- **Before pushing,** the nag's text suggests `/safe-to-push` in the dotagents checkout. `pii-scan` catches
  credentials, keys and personal data. It does not know what is proprietary, and the design does not decide that.
  The person pushing does, with the list of repositories each commit quotes in front of them.

## What atrium does, and what it does not

Atrium drives a tool and does not become one. It holds no credentials and pushes nothing.

**Atrium does:**
- **The pack nag,** above. It reads git state in the configured dotagents path. It runs `git status` and
  `git rev-list @{u}..HEAD`, both read only.
- **A persona catalog.** It reads a configured directory, `dotagents/personas`, the way it reads providers, and
  lists each persona with the runners it renders for and when it last ran.
- **Launch a persona at a card.** "Review this card's diff with go-security-reviewer" becomes one action. Atrium
  starts a fresh session in a run directory for that persona, on the chosen runner, with the card's worktree and
  diff range in the prompt. The review shows up as a card and reports back through a2a.
- **The lessons view.** The lessons review, drawn on the board: per persona, the memory diff since the last
  review, new rejections, and promote, keep and delete buttons. The buttons
  edit files in the dotagents working tree and nothing else. The commit is clint's.

**Atrium does not:** run a git command that writes, push, hold a token for any remote, decide what is proprietary,
or run a persona on a timer. A review is always asked for.

### The run directory

A persona session does not start in its own `dotagents/personas/<id>/` folder. It starts in a scratch run
directory, `<atrium data>/persona-runs/<id>/<run>/`, holding the rendered instruction file for that runner and a
`TARGET.md` naming the worktree and diff to review. A runner that reads its instructions from the working
directory then loads exactly the rendered file. And a persona session never has the dotagents tree as its working
directory, so it cannot edit its own definition by accident. It writes memory through the symlink (claude) or the
absolute `memory/` path it was granted (the others).

`TARGET.md` carries three lines the rendered instructions rely on: `persona_dir:` (the absolute path of
`dotagents/personas/<id>`), `repo:` (the repo key, `github/openziti/ziti`), and what to review. The rendered file
holds no absolute path, so `render/` is the same on every machine and `-Check` holds everywhere.

## Stages

1. **Move.** First an inventory: map every `~/.claude/agent-memory/*` directory to a persona id, and stop if a
   non-empty directory maps to none. Then create `dotagents/personas/<id>/` for every agent. Split each definition in `dotfiles/claude/agents`
   into `persona.yaml` and `persona.md`, render `render/claude/<id>.md`, and point `~/.claude/agents/<id>.md` at
   it. Move each `~/.claude/agent-memory/<id>/` into `memory/`, add `repo:` lines, and symlink it back. Delete the
   two orphan directories. Behaviour does not change, and everything is in git for clint to commit.
2. **The panel and the lessons review, by hand.** The review panel reads `persona.yaml` and writes `rejected.md` and
   repo facts. A `lessons-review` skill walks the review in chat.
3. **The atrium nag.** The one atrium piece with value on its own.
4. **Evals.** Seed three golden cases per reviewer from real confirmed findings. A script runs them.
5. **Other runners,** in two steps, so no adapter is written for behaviour nobody has seen.
   - **5a. Measure.** Per runner, a probe persona in a scratch home, and a record in atrium's
     `docs/other-runners.md` of what it loaded, what it could read, and whether it could write a lesson where told.
     Done for codex and ollama. Gemini waits for its quota.
   - **5b. Build only what passed.** The codex and ollama adapters, the generated `MEMORY.md`, and `runners:` in
     `persona.yaml`. Done. Still to do: gemini (after 5a), a conductor step that assembles the ollama prompt, and one
     diversity panel.
6. **The rest of atrium.** The catalog, launching a persona at a card, and the lessons view.

Stages 1 to 3 are worth doing even if nothing after them is built.

## Stage 4: evals, as built

Built 2026-09-23 in dotagents, uncommitted for clint: nine cases and `scripts/run-persona-evals.ps1`.

### A case

`personas/<id>/evals/<nnnn>-<slug>/` holds three files.

- `diff.patch` is a real diff from an openziti repo, trimmed to the hunks that matter. Where the commit that
  introduced a bug could be found, the diff is that commit, not a reversed fix, because a reversed fix deletes the
  guard and often the comment that explains it, which gives the answer away. All nine are introducing commits.
- `context.md` is what the reviewer gets besides the diff: the repo key and the few facts from outside the diff a
  reviewer needs, such as a dependency's ownership rule or the house exemplar. It never names the bug.
- `expect.json` holds `must_find` (a claim, a file, a severity floor), `must_not_find` (a known false positive and why
  it is wrong), and a `source` block naming the checkout, the diff's commit, the commit or review that confirmed the
  bug, and the memory file it came from. The source is for people. The reviewer never sees it.

Seeded: go-security-reviewer from openziti/channel (heartbeat option loaded into the wrong field, a uint32 frame
length sum that wraps on peer input, the multi-listener close race). c-systems-reviewer from ziti-sdk-c (a json-c
tokener leak, an `edge_error` leak, an uninitialized length passed with an unchecked NULL body). codebase-steward from
ziti-openwrt (dnsmasq entries owned by a value pattern), tlsuv (a public vtable member documented never NULL) and
sdk-golang (a bitset const block that steals a type's doc comment). Each case has one must-not-find.

### The run

One persona per run, one review per case, no retries.

1. **The reviewer** is `claude -p --agent <id> --tools "" --settings '{"disableAllHooks":true}'
   --no-session-persistence --output-format json`, prompt on stdin, from a scratch directory. The prompt is the
   context, the persona's knowledge and memory for the case's repo, the diff, and the severity scale and finding
   schema read verbatim from the review-panel skill.
2. **The judge** is haiku with its own `--system-prompt` and a `--json-schema`. It gets the expectations and the
   findings' file and claim, never the diff, and returns, per expectation, the index of the finding that states the
   same defect or -1. It is told it compares and does not review.
3. **The score** is computed in the script, not by a model. A must-find is a hit when the judge matched it, the
   finding names the same file by path suffix or basename, and its severity is at least the floor. A must-not-find
   fails when any finding matches it.

Output is one table and a total on stdout, and `evals/_results/<yyyy-MM-dd-HHmm>.json` with the raw reviews, which
the dotagents `.gitignore` excludes. `-Rescore <results.json>` re-grades a saved run with the current `expect.json`
and judge and runs no review, for when the grading changed and the persona did not.

### What the claude CLI actually does (2.1.280, measured)

- **`--agent <id>` works headless** and loads the rendered persona through the `~/.claude/agents` symlink, with the
  persona's own model (sonnet, opus). `--model` overrides it. This is the claude runner row as designed.
- **Native memory in `-p` is the index only.** `--agent` injects the `MEMORY.md` lines, not the lesson bodies. A
  reviewer without the Read tool never sees a lesson, so the runner inlines the memory files for the case's repo, as
  the panel's dispatch prompt tells a reviewer to read them. The design assumed native memory meant the lessons.
- **`--agent` also loads clint's global `CLAUDE.md`,** as a panel subagent does. The run keeps it, so the eval
  measures the persona as the panel runs it.
- **Hooks fire in `-p` sessions.** Without `disableAllHooks`, every eval review and judge call would show up on the
  atrium board and pass through the permission hook.
- **`--bare` is not usable here.** It would drop `CLAUDE.md` and hooks in one flag, but it reads only
  `ANTHROPIC_API_KEY` and never the OAuth login this machine uses.
- **A judge call is not cheap even with `--system-prompt`:** about 7.3k input tokens before the prompt, and
  `--json-schema` costs a second turn. The answer arrives in `structured_output`.
- `--output-format json` reports `usage` and `total_cost_usd` per call, which is where the token counts come from.

### Starting score

| Persona | Model | Score | Tokens | Cost |
| --- | --- | --- | --- | --- |
| go-security-reviewer | sonnet | 8/8 | 105k | $0.42 |
| codebase-steward | opus | 9/9 | 110k | $0.65 |
| c-systems-reviewer | sonnet | 6/6 | 80k | $0.23 |

One run of all three is about 295k tokens and $1.30, of which the haiku judge is about 8k tokens and 2 cents a case.

c-systems-reviewer first scored 5/6: it reported the tokener leak and the `json_object` leak as two findings, and the
judge would not match either to the one expectation naming both. The judge rule now says a finding that states any
part of a multi-part expectation matches, and `-Rescore` of the saved run gave 6/6.

A perfect start means the set can show a regression but not an improvement. Seven of the nine cases come from lessons
in the persona's own memory, and the run inlines that memory, so part of what a clean score shows is that the memory
is read. What would make the set discriminate: cases no memory file describes, a no-memory run to measure what memory
adds (not built, since `--agent` always injects the index, so it needs `--system-prompt` with the rendered body
instead), and more than one run per case to see the noise.

## Open questions

1. Do `persona` and `style-harvester` belong in the pack? They are about clint's own style, not reviewing, and have
   no memory. The design would carry them with an empty `knowledge/` and no `reviews:` block.
2. Does `rejected.md` stop repeat findings? Decided to try it and judge it at the first few lessons reviews.
3. The persona definitions live in the public dotfiles repo today, and the pack moves them to private dotagents.
   Should a public copy of `persona.yaml` and `persona.md` stay in dotfiles for people who fork it, generated from
   dotagents, or does dotfiles simply lose them?

## Review history

- Round 1 (Mercurius, codex gpt-5.5, needs discussion). C1: the private-content rule did not cover `memory/`.
  Resolved by removing every automatic push: only the human pushes, so no routing between remotes is built, and
  memory files carry `repo:` lines so the pusher can see what is quoted. Q1, where the pack may live: clint's call
  at push time. Q2, whether calibration records clint's rejections: calibration as a separate log was dropped.
  History is git, and wrong findings go to `rejected.md` or the repo's dotagents file. A1: `render-personas.ps1
  -Check` is named as the one check.
- Round 2 (Mercurius, codex gpt-5.5, needs changes). C1: no defined baseline for "new since the last lessons
  review". Fixed with a `Lessons-reviewed: <persona-id>` commit trailer. C2: non-Claude lessons carried no session
  id for attribution. clint rejected session ids as noise: every lesson carries a one-line `Why:` instead, and the
  lessons view shows no session. A1: `skip_when` is free text the conductor reads.
- Round 3 (Mercurius, codex gpt-5.5, needs changes). C1: stage 1 needs a memory inventory before the move. Fixed.
  C3: a rejection is scoped to its repo unless marked general. Fixed. C2 (the inbox contract for codex and gemini),
  A1 (split stage 5) and A2 (no upstream configured) deferred to the stages they belong to. clint stopped the review
  here to build stage 1: the later stages get their own short review when they are reached.
- Stage 5, decided by measurement. C2: the inbox is dropped. Every runner writes lessons as ordinary memory files
  with `repo:` and `Why:`, and `MEMORY.md` is generated by the render step, so no runner edits the index. A codex
  probe wrote a lesson in exactly that shape and left the index alone. A1: stage 5 is split into 5a (measure) and
  5b (build only what passed).
