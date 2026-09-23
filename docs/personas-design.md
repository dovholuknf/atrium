# Personas: specialist agents that keep what they learn, on any runner

Status: design, for review. Nothing here is built.

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

## What a persona is

A persona is a **folder**, not a file. It holds one runner-neutral definition and three kinds of durable content
that differ in who writes them and how far they are trusted.

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
    MEMORY.md             the native Claude index, kept in the native format
    project_openziti_channel.md
  calibration/            what happened to the persona's findings. Machine-written, append only.
    findings.jsonl        one line per finding: repo, diff, claim, verdict, who decided
    rejected.md           false positives, summarised, read at the start of every review
  evals/                  golden cases: a diff and the findings a good review must produce
    0001-http-no-timeout/
      diff.patch
      expect.json
  render/                 GENERATED per-runner files. Never edited by hand.
```

### Why each subfolder exists

- **`persona.yaml` apart from `persona.md`.** The yaml is what tools read: the panel's selection rules, the atrium
  catalog, the render step. The markdown is what a model reads. Mixing them is how the Claude frontmatter ended up
  listing Gmail and HubSpot authentication tools on a C reviewer.
- **`checklists/`** keeps the body short. A persona reviewing a Go HTTP handler loads `go-http.md` and not the
  crypto list. Progressive disclosure is the token budget.
- **`knowledge/` against `memory/`** is the core of the design. Memory is what the model believes. Knowledge is what
  a human agreed with. Every review reads both, but the instructions say plainly that knowledge wins a conflict and
  memory is a hint to check. Promotion from memory to knowledge is the "lessons learned" review clint asked for.
- **`knowledge/` is keyed by `<host>/<org>/<repo>`,** the same layout dotagents already uses for per-repo
  `CLAUDE.md`. A review of an openziti/ziti diff loads `_general.md` and `github/openziti/ziti.md` and nothing else.
  A persona that has reviewed forty repos does not carry forty repos into every review.
- **`calibration/`** turns the review panel's existing verify pass into learning data. The panel already records
  `confirmed`, `refuted` or `uncertain` for every serious finding. Today that verdict is thrown away when the report
  is printed. Written here, it answers "how often is this persona wrong, and about what".
- **`evals/`** is what makes "restore a previous state" mean something. A persona change is a git commit. Running
  the evals before and after says whether it got better. Without evals, a revert is a guess.
- **`render/`** keeps the per-runner files out of the hand-edited tree, so there is one source of truth.

### The id is not the name

`persona.yaml` carries a stable `id` (`go-security-reviewer`) and a display `name` that may change. Everything keys
on the id: the folder, the native memory directory, calibration records, atrium's catalog. A rename edits `name`
and nothing moves.

## Runners

The body in `persona.md` is written once. A render step (`dotagents/scripts/render-personas.ps1`) turns each
persona into what each runner can load. What each runner offers differs, so each gets a different amount.

| Runner | What it loads | Memory | Tools | Measured? |
| --- | --- | --- | --- | --- |
| claude code | `render/claude/<id>.md`: subagent frontmatter from the yaml, body from the md, plus a block pointing at `knowledge/`, `checklists/` and `calibration/rejected.md` | native: `~/.claude/agent-memory/<id>` is a SYMLINK to `memory/` | from the yaml's tier | yes, today's setup |
| codex | `render/codex/AGENTS.md`, read when codex starts in a persona run directory | instructed: read `memory/`, write new lessons to `memory/inbox/` | codex's own | no, measure first |
| gemini | `render/gemini/GEMINI.md`, the same way | instructed, as codex | gemini's own | no, measure first |
| ollama | `render/ollama/Modelfile` with the body as `SYSTEM`. The diff and the knowledge file go in the prompt. | none: it cannot write files. Its findings still land in calibration through the panel. | none | partly: no tools, no file access |
| next runner | one adapter function in the render script, plus one row in `docs/other-runners.md` | whichever of the three memory modes it supports | its own | per runner |

The codex and gemini rows name the file each is documented to read. Before building either, measure it the way
`docs/other-runners.md` measured codex's hooks: a probe persona, a scratch home, and a record of what the runner
actually loaded. `docs/atrium-for-agents.md` is the brief for that.

**A persona on a runner without tools is still useful.** An ollama reviewer cannot open files, but handed a diff and
one knowledge file it can still produce findings in the panel's schema. It is a cheap second opinion, and
calibration will say whether it is worth its time.

### Memory, per runner

Claude's native memory is the only one that is automatic. So the pack treats it as the model and the others follow
it by instruction:

- **Native (claude).** The symlink makes the native directory BE `memory/`. Nothing is copied, so nothing drifts.
- **Instructed (codex, gemini).** The rendered instructions say: read `memory/MEMORY.md` at start. To record a
  lesson, write one file to `memory/inbox/` in the same format. The inbox is merged into `memory/` by the same
  human review that promotes to knowledge, so an unfamiliar runner cannot rewrite the index on its own.
- **None (ollama).** Nothing is written. Its value lands only as calibration.

## The review panel

Three changes to the skill. None changes how a review reads to clint.

1. **Selection comes from `persona.yaml`, not from a table in the skill.** Each persona declares what it reviews:
   ```yaml
   reviews:
     paths: ["**/*.go"]
     surfaces: [http, crypto, concurrency, input-validation]
     skip_when: "docs-only or generated-only diff"
   ```
   The skill's mapping table becomes the fallback. Adding a persona adds a reviewer with no edit to the skill.
2. **Each reviewer gets only what applies.** The dispatch prompt names the persona's `knowledge/_general.md`, the
   target repo's knowledge file if there is one, `calibration/rejected.md`, and the checklists the yaml maps to the
   surfaces in this diff.
3. **Verdicts are written back.** After the verify pass, the conductor appends one line per finding to that
   persona's `calibration/findings.jsonl`: date, repo, diff range, severity, claim, verdict, and who decided (the
   verifier or clint). When clint rejects a finding in the report, that rejection is recorded too, and it is the
   strongest signal the pack gets.

**Diversity as an option.** The panel may run the same persona on two runners, such as go-security-reviewer on
claude and on codex. Agreement raises confidence the way two different agents agreeing already does. Disagreement
goes to the verify pass. This is off by default because it doubles the cost.

## The lessons review

The step that keeps the pack honest. Run on demand, not on a timer, because it needs clint.

For each persona with anything new since the last review:

1. **Memory diff.** What was added or changed in `memory/` and `memory/inbox/`, with the session that wrote it.
   Claude memory files already carry `originSessionId`. For each: promote to `knowledge/`, keep as memory, or
   delete.
2. **Calibration summary.** Findings since last time, confirmed against refuted, and the refuted claims grouped. A
   cluster of refutations becomes a line in `calibration/rejected.md` ("this persona keeps flagging X, which is
   intended because Y").
3. **Eval run.** If the persona's prompt or knowledge changed, run its evals and show the score next to the last
   one.

The output is a git commit in dotagents with a message saying what was promoted, deleted and learned.

## Backup and history

Everything under `dotagents/personas/` is committed. dotagents is private, and it already syncs by script.

- **What is committed:** `persona.yaml`, `persona.md`, `checklists/`, `knowledge/`, `memory/`, `calibration/`,
  `evals/`. `render/` is generated. Commit it so a machine that has not run the render step still works, and
  check in CI or a pre-commit hook that it matches its sources.
- **When:** a scheduled task runs `dotagents/scripts/sync-personas.ps1`: commit the memory and calibration churn,
  run `pii-scan` over the staged diff, and push only if the scan is clean. A dirty scan leaves the commit local and
  says why. Hourly is enough.
- **Restore:** `git log` and `git checkout` on one persona's folder. The evals say whether the restored state is
  better.

### What must not be pushed, and to where

This is a real constraint, not a formality. Memory and calibration will quote code, hostnames and findings from
NetFoundry and customer repositories. dotagents is a private repo on clint's personal account.

- `pii-scan` catches credentials, keys and personal data. It does not catch "this is proprietary". That is a policy
  decision for clint and his employer, and the design does not make it.
- The pack supports a split if the answer is no: `knowledge/` and `calibration/` entries whose repo key is under a
  private org (`bitbucket/netfoundry/...`, `bitbucket.nf/...`) can be routed to a second remote, or kept local.
  The layout already makes that a path rule.
- **dotfiles is public (MIT).** The persona definitions live there today. Under this design `persona.yaml` and
  `persona.md` could stay public, but nothing in `knowledge/`, `memory/` or `calibration/` may ever go there. The
  simplest safe rule is: the whole pack moves to dotagents, and dotfiles keeps only the generic, forkable agent
  modules it already has.

## What atrium does, and what it does not

Atrium's rule is that it drives a tool and does not become one. It holds no credentials and pushes nothing. So:

**Atrium does:**
- **A persona catalog.** It reads a configured directory, `dotagents/personas`, the way it reads providers, and
  lists each persona with its runners, when it last ran and its calibration score.
- **Launch a persona at a card.** "Review this card's diff with go-security-reviewer" becomes one action. Atrium
  starts a fresh session in a run directory for that persona (see below), on the runner chosen, with the card's
  worktree and diff range in the prompt. The review shows up as a card, reports back through a2a, and its
  findings are written to calibration.
- **The lessons view.** The review above, drawn on the board: per persona, the memory diff since the last review
  with the session that wrote each lesson, the calibration summary, and promote, keep and delete buttons. The
  buttons edit files in the dotagents working tree and nothing else. The commit is clint's or the sync script's.

**Atrium does not:**
- run git against dotagents, push anywhere, or hold a token for it
- decide what is proprietary
- run a persona on a timer. A review is always asked for.

### The run directory

A persona session does not start in its own `dotagents/personas/<id>/` folder. It starts in a scratch run
directory, `<atrium data>/persona-runs/<id>/<run>/`, holding a rendered instruction file for that runner and a
`TARGET.md` naming the worktree and diff to review. Two reasons. A runner that reads its instructions from the
working directory then loads exactly the rendered file. And a persona session never has the dotagents tree as its
working directory, so it cannot edit its own definition by accident. It writes memory through the symlink
(claude) or the absolute inbox path (the others).

## Stages

1. **Move and back up.** Create `dotagents/personas/<id>/` for all eleven. Move each definition out of
   `dotfiles/claude/agents` into `persona.yaml` plus `persona.md`, render `render/claude/<id>.md`, and point the
   `~/.claude/agents/<id>.md` symlink at it. Move each `~/.claude/agent-memory/<id>/` into `memory/` and symlink it
   back. Delete the two orphan directories. Nothing changes in behaviour, and everything is in git.
2. **Calibration and the lessons review, by hand.** The review panel writes verdicts back. A `lessons-review` skill
   walks the steps above in chat. No atrium change yet.
3. **Evals.** Seed three golden cases per reviewer from real confirmed findings in calibration. A script runs them.
4. **Other runners.** Measure codex and gemini, write their adapters, and try one diversity panel.
5. **Atrium.** The catalog, launch-at-a-card and the lessons view.

Stages 1 and 2 are worth doing even if nothing after them is built.

## Open questions

1. May NetFoundry and customer code, as quoted in memory and calibration, live in a private repo on clint's
   personal account? The answer decides whether the private-org split is built in stage 1 or never.
2. Do `persona` and `style-harvester` belong in the pack? They are about clint's own style, not reviewing, and they
   have no memory. The design would treat them as personas with an empty `knowledge/` and no `reviews:` block.
3. Should calibration record clint's rejections from the review report, which needs the panel to ask, or only the
   verifier's verdicts, which it already has?
4. Is hourly the right sync interval, or should the sync run at the end of every panel?
