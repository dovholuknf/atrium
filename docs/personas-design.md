# Personas: specialist agents that keep what they learn, on any runner

Status: design, round 2. Nothing here is built.

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
    MEMORY.md             the index, in the format Claude's native memory uses
    project_openziti_channel.md
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
  which repositories a change quotes.
- **`memory/rejected.md`** records findings that were wrong, so the same persona stops raising them. See "What a
  wrong finding teaches" below.
- **`evals/`** is what makes "restore a previous state" mean something. A persona change is a commit. Running the
  evals before and after says whether it got better. Without evals a revert is a guess.
- **`render/`** keeps the per-runner files out of the hand-edited tree, so there is one source of truth.

### The id is not the name

`persona.yaml` carries a stable `id` (`go-security-reviewer`) and a display `name` that may change. Everything keys
on the id: the folder, the native memory directory, atrium's catalog. A rename edits `name` and nothing moves.

## Runners

The body in `persona.md` is written once. A render step (`dotagents/scripts/render-personas.ps1`) turns each
persona into what each runner can load. `render-personas.ps1 -Check` exits non-zero when `render/` does not match
its sources, and is the only check.

| Runner | What it loads | Memory | Tools | Measured? |
| --- | --- | --- | --- | --- |
| claude code | `render/claude/<id>.md`: subagent frontmatter from the yaml, body from the md, and a block naming the knowledge, checklists and memory files to read | native: `~/.claude/agent-memory/<id>` is a SYMLINK to `memory/` | from the yaml's tier | yes, today's setup |
| codex | `render/codex/AGENTS.md`, read when codex starts in a persona run directory | instructed: read `memory/`, write new lessons to `memory/inbox/` | codex's own | no, measure first |
| gemini | `render/gemini/GEMINI.md`, the same way | instructed, as codex | gemini's own | no, measure first |
| ollama | `render/ollama/Modelfile` with the body as `SYSTEM`. The diff, the repo's knowledge and memory files, and `rejected.md` go in the prompt. | read only, through the prompt. It cannot write. | none | partly: no tools, no file access |
| next runner | one adapter function in the render script, plus one row in atrium's `docs/other-runners.md` | whichever of the three modes it supports | its own | per runner |

The codex and gemini rows name the file each is documented to read. Before building either, measure it the way
atrium's `docs/other-runners.md` measured codex's hooks: a probe persona, a scratch home, and a record of what the
runner actually loaded. atrium's `docs/atrium-for-agents.md` is the brief for that.

A persona on a runner without tools is still useful. An ollama reviewer cannot open files, but handed a diff and
the right knowledge file it can still produce findings in the panel's schema. It is a cheap second opinion.

### Memory, per runner

Claude's native memory is the only automatic one. The pack treats it as the model and the others follow it by
instruction.

- **Native (claude).** The symlink makes the native directory BE `memory/`. Nothing is copied, so nothing drifts.
- **Instructed (codex, gemini).** The rendered instructions say: read `memory/MEMORY.md` at start. To record a
  lesson, write one file to `memory/inbox/` in the same format, with a `repo:` line and a one-line `Why:`. The lessons review merges the
  inbox, so an unfamiliar runner never rewrites the index on its own.
- **Read only (ollama).** It reads what it is handed and writes nothing. What it gets wrong still reaches
  `rejected.md`, because the conductor writes that file, not the reviewer.

## What a wrong finding teaches

When the review panel's verify pass refutes a finding, or clint skips one when choosing which to apply, one of two
things is true. The conductor picks by asking: would a different persona make the same mistake?

- **No: this persona misread something.** The conductor appends one line to that persona's `memory/rejected.md`:
  the date, the repo, the claim, and why it was wrong. The next review by that persona, on any runner, reads the
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

1. **Memory diff.** What `git diff` shows in `memory/` and `memory/inbox/`. For each lesson: promote to
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
absolute inbox path (the others).

## Stages

1. **Move.** Create `dotagents/personas/<id>/` for all eleven. Split each definition in `dotfiles/claude/agents`
   into `persona.yaml` and `persona.md`, render `render/claude/<id>.md`, and point `~/.claude/agents/<id>.md` at
   it. Move each `~/.claude/agent-memory/<id>/` into `memory/`, add `repo:` lines, and symlink it back. Delete the
   two orphan directories. Behaviour does not change, and everything is in git for clint to commit.
2. **The panel and the lessons review, by hand.** The review panel reads `persona.yaml` and writes `rejected.md` and
   repo facts. A `lessons-review` skill walks the review in chat.
3. **The atrium nag.** The one atrium piece with value on its own.
4. **Evals.** Seed three golden cases per reviewer from real confirmed findings. A script runs them.
5. **Other runners.** Measure codex and gemini, write their adapters, try one diversity panel.
6. **The rest of atrium.** The catalog, launching a persona at a card, and the lessons view.

Stages 1 to 3 are worth doing even if nothing after them is built.

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
