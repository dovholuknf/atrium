# Close is a workflow: decisions (rnd-005)

Interview of clint on the "Open choices" of `docs/rnd/close-workflow.md`, 2026-10-13, by interview-close-workflow.
Everything else in that design stands. Where an answer here differs from the design's recommendation, this file wins.

## Invariants added during the interview

Given by clint through the orchestrator, not asked as questions. They bind the whole design.

1. **Close deletes only what atrium recorded creating.** Its worktree, its branches, and scratch under the scratch
   root. Never the card's cwd and never anything under it. A claude or codex started in a home folder closes without
   deleting a single file. Every removal goes through the inventory and `internal/safepath`, as `reclaimScratch`
   (internal/daemon/reclaim.go:106) already does.
2. **The close dialog names each thing it deletes.** "Clean up" means different things per runner (claude, codex), so
   the confirmation never says "clean up". It lists every row by kind and path, as `CLOSE_KIND_WORDS` does.
3. **Closing a PR review card removes the BRIEF.md atrium wrote into the PR worktree.** PR 1451's report told clint to
   delete it by hand. Close removes it the way `removeWorktree` does in internal/daemon/cull.go:395, and only the
   untracked BRIEF.md atrium wrote.

## Answers

| # | Choice | Design recommended | Clint chose |
| --- | --- | --- | --- |
| 1 | Where learnings go | A, collect a file | A, kept in two places, never in the cwd |
| 2 | No answer by the deadline | A, close | **B, hold** |
| 3 | May the wrap-up commit unsaved work | A, list only | A, list only, untracked files included |
| 4 | A parked card | A, skip | A, skip |
| 5 | Can a custom step hold | A, yes | A, yes |
| 6 | "tell the agent" after a hold | A, stays armed | A, stays armed |
| 7 | Skip the wrap-up from the start | A, no skip box | A, no skip box |
| 8 | PR card, PR open versus merged or closed | (new) | A, same close, different words |

### 1. Where learnings go: A, in two places

Asked: r-004's commit on the card's branch makes every atrium close ask keep or delete about an unmerged hub branch.
Options were A (collect a file and file it on the hub), B (r-004 as written), C (dotagents for every repo). A
per-repo `learned/...` hub branch written by the room was offered as D.

Clint first leaned to B because "the room needs to publish to the hub somehow". Told that A also publishes to the hub,
over the room link rather than git, he chose A, and added that it should live in two places until it is cleaned, for
redundancy.

What gets built:

- The agent writes `LEARNED.md` under the card's **scratch folder**, never in the cwd (invariant 1: a home folder card
  must not be left with a stray `.atrium/`). Step 5 of `wrapUpRules` names that path.
- Before the scratch folder is freed, the room copies the file into its own data folder and files it on the hub as a
  report on the card, tagged with the repo.
- The room's copy is deleted only once the hub's copy has been folded into AGENTS.md (by the minion for atrium, by
  clint for dotagents). Until then it is in both places.
- An empty or missing file files nothing.

### 2. No answer by the deadline: B, hold

Against the recommendation. When the deadline passes, the close goes to HELD with `held_by: deadline` and the reason
"the wrap-up did not answer in 5 minutes". The card goes to needs-input, the `close-hold` growler pops, and the three
buttons of section 4.3 show. A close never finishes unattended when the agent stalls. Test 7 in section 9 asserts the
hold.

### 3. Commit unsaved work: A, list only

Fixed step 4 stands: list, never commit, stash or push. New constraint: git status is how clint finds stray files, so
step 4 lists **untracked** files as well as modified ones, and the recheck (2.1) counts untracked files as dirty.

### 4. Parked card: A, skip

No live process means no wrap-up. Straight to FREEING with "no session to wrap up".

### 5. Custom step can hold: A, yes

"If you cannot finish a step, hold" covers custom steps. The hold reason names the step.

### 6. "tell the agent" after a hold: A, stays armed

Clint did not follow the first wording. Asked again with an example (agent holds over 3 unpushed commits, clint says
"push them, then finish"), he chose A: the message is said, the close goes back to wrapping with a fresh deadline, and
the next turn end without a hold closes the card. With choice 2 a stall after the message is a hold again, so the
card never closes behind his back. "keep it open" is how he says never mind.

### 7. Skip the wrap-up: A, no skip box

"close anyway" on the strip is the only shortcut.

### 8. PR card, open versus merged or closed: A

New choice from clint's PR input. Close frees the same rows whatever the PR's state: stops and archives the review,
sets its run folder aside without `src/`, keeps findings and walk in history, removes atrium's BRIEF.md (invariant 3).
Close asks the forge for the PR state, bounded, and treats no answer as open:

- Merged or closed: the "N findings are not posted" warning is dropped, since posting is moot.
- Open: the dialog says "the PR is still open, N findings are not posted", and they stay in history to post later.

Cost accepted: one forge call per PR card close.
