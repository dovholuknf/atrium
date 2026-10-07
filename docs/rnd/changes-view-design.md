# A Changes view on every card (design)

Status: built. The Changes view on a card (`internal/api/changes.go`).

Origin: design only, clint ranks. Written 2026-09-30 by @rnd from item 3 of `docs/rnd/competitor-features.md`.
Nothing is built. The endpoint is @runtime's, the board half is @ui's, and @review owns how a comment reads.

## The ask, in one paragraph

A tab on a card that shows what its worktree has changed, drawn with the board's existing diff styling, where the
operator selects lines and sends them to that card as a review comment. Superset, Agent Orchestrator, Orca and cmux
all ship one. It is the step between "the agent says it is done" and "accept", without leaving the board and without
opening an editor on the right machine. Atrium has nothing like it today: no code in `internal/` runs `git diff`.

## 1. The endpoint, on the room

`GET /v1/tasks/{id}/changes?against=base|head` on the room that owns the card, reached through the hub the way every
`/v1/tasks/{id}/...` route already is.

- **Where it runs.** In the card's `worktree`, which the room already records. The directory is the card's own, and
  the endpoint takes no path, so it cannot be pointed anywhere else (the posture of resilience guarantee 8, "files
  never leave a card").
- **How git runs.** Through f-019's `gitsync.Runner` (`internal/gitsync`): a clean environment with every `GIT_*`
  variable stripped, bounded, killed at the bound. That is the lesson sg3 taught about `GIT_DIR`, and it applies to a
  read as much as to a write. Read-only commands only: `git merge-base`, `git diff`, `git ls-files`.
- **`against=head`** (the default): uncommitted changes, meaning `git diff HEAD` plus untracked files that are not
  ignored (`git ls-files --others --exclude-standard`), each shown as a whole-file addition.
- **`against=base`**: everything the card's branch changed since it left the integration branch, meaning `git diff
  $(git merge-base HEAD claude/main)` plus the untracked files. The integration branch is the repo's (f-019's rule:
  `claude/main`, or `main`), never a department branch. A card whose worktree has no `claude/main` falls back to
  `head` and says so.
- **What comes back.** JSON: the base sha, the head sha, whether the tree is dirty, and a list of files, each with its
  path, status (added, modified, deleted, renamed, binary), counts, and the unified hunks as text. Binary files are
  listed without content.
- **Bounds.** At most 400 files and 2 MB of hunks. Past that, the list is cut and the answer says what was cut, so the
  view never claims to be the whole change when it is not. A file over 256 KB of diff is listed with counts only.
- **Not a git repository** answers 404 with "this card's directory is not a git worktree", and the tab says so rather
  than showing an empty diff.

The exposure is the same as `/v1/tasks/{id}/files/text` already has, which reads any file in the card's directory.
A diff can show a `.env` the agent wrote, and so can the file endpoint today.

## 2. The board half

A **Changes** tab beside the card's existing tabs, with a count badge ("12 files, +340 -41") taken from the answer.

- The file list on the left, hunks on the right, drawn with the same renderer the permission view already uses for a
  pending edit's diff, so there is one diff style on the board.
- A toggle for `uncommitted` and `since base`, remembered per card.
- It refreshes when the tab is opened and when the card's turn ends (the `Stop` event already reaches the board), never
  on a timer. A card that is mid-turn shows the last answer with "the agent is still working" above it.
- On a phone (`/m`), the same data as a list of files with their counts, and a file opens full-screen. Line selection
  on a phone waits for the phone design to settle.

## 3. Sending lines to the card

Select one or more lines (shift-click for a range, across hunks of one file), type a comment, press **Send to card**.
What reaches the card is the operator's message, through the existing `POST /v1/tasks/{id}/message`, so it is typed or
queued exactly as any message the operator sends, with no new delivery path:

```
review comment on internal/daemon/foo.go lines 120 to 128 (since base, head 1a2b3c4):
    120 +	if err != nil {
    121 +		return nil
    ...
the error is swallowed here. return it, and add a test that fails without the fix.
```

- The quoted lines are the diff lines, with their `+` and `-`, indented so they read as quotation and not as the
  instruction.
- The head sha is in the header, so a card that has moved on can tell the comment is about an older state.
- Several comments can be collected and sent as one message ("Send 3 comments"), which saves a turn per comment. This
  is what u-005's review walk does for a PR, and the two should share the comment shape. @review decides it once.

## 4. What is not built

- **Editing in the view.** The file endpoint's text editor already exists behind a content-hash precondition
  (`docs/runtime/file-transfer-design.md`). The Changes view links each file to it, and never edits itself.
- **Staging, committing or discarding.** Those change the agent's worktree under it. An agent commits its own work.
- **A diff between two cards.** Not asked for.

## 5. Tests

- The endpoint refuses any path parameter and runs only in the card's worktree.
- A git env var in the room's environment (`GIT_DIR`) does not change what is diffed.
- `head` shows uncommitted and untracked changes. `base` shows the branch since the merge base with `claude/main`, and
  falls back to `head` with the sentence when there is no `claude/main`.
- The 400-file and 2 MB bounds cut the list and say so. A binary file is listed without content.
- A card whose directory is not a worktree answers 404 with the sentence.
- A comment sent from the view arrives at the card as an operator message with the file, the lines, the head sha and
  the text.

## 6. Open questions for clint

1. Default to `uncommitted` or `since base`? This design says uncommitted, because it answers "what is it doing right
   now". Since base answers "what will I be merging".
2. Should a sent comment move the card to running at once, or wait like any message?
