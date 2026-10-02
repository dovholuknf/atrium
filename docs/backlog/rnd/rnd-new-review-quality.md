# rnd-new-review-quality: fewer, better findings, and a measure of whether they were worth raising

Filed by @rnd, 2026-10-02, from a survey of how automated code review is done elsewhere, asked for by clint
through the orchestrator. The survey itself stays local (it is a `*-comparison*.md` file, see `.gitignore`). This
item is what came out of it. Design work only. Nothing here is built.

## Why

- Published studies of review bots agree on one point. Developers act on most human review comments, but on only a
  small share of bot comments. The bots that improved did it by raising fewer findings, not by finding more.
- atrium's review direction is already ahead in several places: a walk that clint reads in person, nothing posted
  unasked, evidence pinned to a sha, gates before a push, and memory that @review approves. Those designs are
  `docs/rnd/review-on-atrium-design.md`, `docs/rnd/pulls-view-design.md`, `docs/rnd/change-record-design.md` and
  `docs/rnd/change-lifecycle-design.md`.
- The gaps are elsewhere. atrium has no written bar for what counts as a finding, no deterministic tools in the
  review, no context beyond the diff and the repo, and no measure of whether a finding was worth raising.

## The five to do first

1. **A written finding bar.** This goes into the stored recipe in pulls-view, and into every persona's brief in
   review-on-atrium section 7. A finding must:
   - be introduced by this change;
   - be discrete, and name a line;
   - be something the author would fix;
   - ask for no more rigour than the rest of the codebase.

   "No findings" is a good result. The verifier drops anything that fails the bar.
2. **An addressed rate for each reviewer** (change-record finding states, review-on-atrium 7.4).
   - A finding is addressed when a later commit changes its lines, or when clint marks it "fix it" in the walk.
     Rejected and skipped findings count against the rate.
   - Show each persona's rate over 30 days on the review tab. Flag a persona below a floor to @review.
3. **Deterministic tools on changed lines only, against a baseline** (a tools step in pulls-view, before prime).
   - Run the repo's own linters, and any pattern scanner it configures, at base and at head.
   - Keep only findings that are new at head and on changed lines. Put them in the prime bundle as `tools.sarif`.
   - The tools read the PR's own config, which is untrusted. Run them in the run folder with no credentials, the way
     the lifecycle's tested gate runs tests.
4. **A confidence score from the verifier, with a threshold** (the verify and merge steps in pulls-view).
   - Each finding gets a confidence from 0 to 100. Below 80, it leaves the walk but stays in the run folder, where
     "deeper" can show what was dropped.
   - Severity stays a label the reviewer picks from a fixed rubric. Only confidence decides what leaves the walk:
     a model asked to judge severity does little better than chance.
5. **Incremental review from the last reviewed head** (pulls-view already stores "head reviewed"; the hunk hash is in
   change-lifecycle 2.1).
   - A second round reviews only the new commits.
   - A finding on a hunk that has not changed keeps its state.

## Later

6. **The nearest CLAUDE.md and AGENTS.md for each changed file go into the bundle**, scoped to their directory.
7. **A never-report list, applied by code before any model pass.** It drops pre-existing issues, anything a linter
   catches, formatting, and anything on the persona's told-off list (review-on-atrium 7.4).
8. **A written packer for big diffs:**
   - files ordered by language, then size;
   - deletions folded into one list;
   - an overflow list of file names when the budget runs out.
9. **Cheap triage, only where Claude checks.** A cheap model marks trivial files, and a Claude verifier still sees
   every finding (`docs/rnd/opencode-token-routing.md`).
10. **Blast radius for the walk.** For each changed function, show its callers and the tests that reach it. This
    gives the riskiest-first order a fact to stand on (the PR review story, sections 3 and 4, not landed).
11. **A replay set.** Rerun past PRs whose findings are known (PR 378 and the next few) against every recipe change.
    The change lands only if recall holds on the set (pulls-view acceptance, `docs/backlog/release/m-001.md`).
12. **A receipt for a lesson.**
    - When a walk comment becomes a lesson, show "lesson added: <file>, pending knowledge-ok".
    - Redact anything that looks like a secret before the lesson is written.

## Not to take

- Posting by default, or a summary comment on every push.
- Learning from thumbs reactions. One reviewer gives too little signal, and a tool that tried it turned it off.
- A third party holding a forge key that can write. Pulls-view option C stays rejected.
- Generating docstrings and tests as part of review.

## Open for clint (held)

- Should the addressed rate be shown to clint, or only to @review?
- Is 80 the right confidence cut, or should it start lower and be tuned on the replay set?
