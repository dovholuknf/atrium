# Review: runner switch design, `atrium move --runner` (aa8291a7, m1mini, 2026-10-02): HOLD

`docs/rnd/runner-switch-design.md`, alone in its commit.

The shape is right:
- the switch reuses the move's phases;
- it forces a cold brief, because no runner resumes another's transcript;
- `--back` resumes the original Claude conversation with the stand-in's handoff;
- it captures early at 90%, because an exhausted account cannot write a handoff;
- the hub builds a brief from data when no capture exists;
- it is triggered by hand, or by clint's yes on a numbered decision;
- it limits runners per card by role.

It holds on R3, the row the design rests on. As written it cannot be checked, and it checks only half of what
keeps a stand-in gated.

## Medium (holds): R3, "its gate is in place"

- **Codex: "approved once" cannot be checked.** `docs/runtime/other-runners.md` ("Trust") says atrium never reaches
  into codex's trust store, on purpose. So a row that refuses "when the hook has not been approved once" has
  nothing to read. Make it a proof, not a lookup. The room's preflight runs codex once in a scratch folder on a
  harmless tool call, and passes only if atrium's hook reported it. S1's acceptance already wants "refused at R3 with
  hooks unapproved". This is how to test that.
- **Codex: the permission mode is the other half.** The measured codex session ran with `permission_mode:
  bypassPermissions` (other-runners.md, the SessionStart sample). Claude falls back to its own prompt when atrium's
  gate cannot answer. A codex card in bypass mode falls back to nothing: a dead daemon, or a hook that did not fire,
  leaves it ungated. R3 also checks the codex row's approval and sandbox settings. It refuses bypass, the opencode
  doc's fail-closed rule applied to codex.
- **Opencode: a config file existing is not the config in effect.** opencode merges a project `opencode.json` and
  loads `.opencode/plugins/` from the worktree (the plugin docs, checked in the token-routing review). For a card
  working in an outside repo (OpenWiki was one), that repo's own config can loosen the permissions, and its plugins
  run as code. R3 (or a new R8) refuses an opencode switch when the worktree's repo carries `opencode.json` or
  `.opencode/`, unless the repo is ours, and checks the effective permission with opencode's own resolution.

## Smaller

- **`BRIEF.switch.md` is not gitignored.** The `.gitignore` has `HANDOFF.*.md` and nothing for briefs. The file holds
  the last ten message subjects, the memory text and the handoff, which can carry clint's words and private paths,
  and the repo is public. Add `BRIEF.*.md`, or write it outside the worktree and point to it.
- **Codex is another vendor too.** A stand-in on codex sends the card's private repo, its memory text and its brief
  to OpenAI. R5 covers OpenCode Go's public-repo rule. Q2 should also ask clint whether private repos may go to
  codex, and R5 should honour his answer.
- **@review on codex.** The brief's item 4 points at `CLAUDE.md`. For a review card, add
  `docs/backlog/review/REVIEWER-NOTES.md` and `QUEUE.md`, or a codex @review starts without the known classes and the
  known reds. Section 5's rule that the verdict file says which runner wrote it is good. Make it a line every
  verdict carries ("written by codex, model X"), because the trailers cannot show it.
- **Early capture at 90%.** Section 3.3's "about $0.40 a card" comes from the operator-focus contexts. On @review's
  large context it is more. That is fine for a rare event, but say "once per reset window" next to the cost.

## Section 5, judgment cards limited to claude and codex: right

Verdicts and designs are judgment, and the one graded cheap-model run (the Kimi audit) is why opencode stays off
them. Codex as a peer frontier model is a fair stand-in. A wider list per card is clint's numbered decision, never
a director's flag. Build workers may use opencode on public repos only, after the terms answer and the bake-off. That
is consistent with the routing doc.

## Verdict

HOLD on R3: a proof by preflight for codex hook trust, a codex permission-mode check, and the opencode project
config and plugins from an outside repo. Fold in the gitignore, the codex question in Q2, and the review card's
brief. A re-read covers sections 2, 3.1, 5 and 7 and S1's acceptance. doc-ok on OK.

Quality: careful. It reuses the move and holds to the routing doc's findings, and it sees the exhausted-account
problem that a naive design would miss. The gap is that a gate's presence was taken as its effect.

## Re-read at 32516973 (2026-10-02): OK, doc-ok

One commit, only the runner-switch doc, and every point is closed.
- **R3 is split three ways.** R3a is a codex preflight that passes only if atrium's own hook reported a harmless
  call, kept per room and codex version and re-run after an update. R3b refuses bypass, whether `bypassPermissions`
  or the dangerous-bypass flag. R3c checks opencode's merged permission in effect, and refuses an outside repo
  carrying `opencode.json` or `.opencode/`.
- **The brief.** It is `HANDOFF.<alias>.switch.md`, which `.gitignore`'s `HANDOFF.*.md` covers, with a
  `.git/info/exclude` line for other repos, at 0600. Keeping it inside the worktree is right, because the factory
  opencode config denies `external_directory`. S1's acceptance checks that `git status` never shows it.
- **The rest.** R5 and Q2 cover codex on private repos, memory included. A review card's brief carries
  REVIEWER-NOTES.md and QUEUE.md. Every verdict and design carries a "written by <runner>, <model>" line. The early
  capture runs once per reset window per reading kind.

Verdict: OK, doc-ok aa8291a7^..32516973. It lands alone by cherry-pick of the two.
