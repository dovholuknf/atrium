# u-new-m-code-review: read and review code on the phone

From clint by the orchestrator, 2026-09-30 night. After the scroll-yank fix.

## Why

/m filters tool noise out of the thread, and clint keeps that as the default. For real development he wants to see the
code a session changed, and to comment on it, from the phone.

## What

1. **A chip on each reply that changed files**: "N files changed +a -b". Tapping it opens the diff in the file viewer
   sheet (`m/js/viewer.js`): one section per file, collapsed by default, a unified diff with +/- line colouring and
   line numbers, wrapped at phone width with no sideways scroll, text size from the same `--m-fs` pinch variable.
2. **A card-level "changes" view**: the card's branch against its base, the same renderer.
3. **Room for review**: tapping a line quotes it into the composer ("card.js:14 why 10 not 50?").

## State of the pieces

- `docs/rnd/changes-view-design.md` is @rnd's design for the desktop view. Its endpoint, `GET
  /v1/tasks/{id}/changes?against=base|head` on the room, is @runtime's and **does not exist yet** (nothing in
  `internal/` runs `git diff`). A per-reply diff needs a third form: the files a given turn changed, which the
  transcript's tool calls can name but `git` cannot attribute. Ask @runtime and @rnd which of `against=head` or a
  commit range answers it.
- The board half of the same design reuses the board's diff styling. /m has none.

## @runtime's answer (2026-09-30 night)

The endpoint needs @rnd's approved design first, then it sits about third in @runtime's queue, after hub orchestration
and a deploy-ready item. The orchestrator reorders `docs/backlog/runtime/QUEUE.md` if clint wants it earlier. It is a
new git-exec path: bounded output, a timeout, safepath containment on the card's worktree.

The per-reply chip is answerable from the transcript. The room collects the paths of the turn's Edit, Write, MultiEdit
and NotebookEdit calls, keyed by the reply's `at`, and runs `git diff HEAD -- <those paths>` for +a -b. Proposed shape:
`GET /v1/tasks/{id}/changes?turn=<reply at>` returning `{files:[{path, added, removed}], via:"edits"|"commits",
partial:bool}`, beside the card-level `against=base|head`. Three caveats the design must carry:

- It covers what the edit tools touched. A change made by a Bash command (sed, go generate, git checkout) can be
  missing, and `partial` says so.
- Once the turn's changes are committed, `git diff HEAD` is empty for them. The chip then needs `git show <commit> --
  <paths>` for commits made in the turn (the transcript has the Bash `git commit` calls and their times), or it falls
  back to base-relative per-file counts.
- A file edited across several turns shows the cumulative diff, not that turn's slice, unless the room stores the
  pre-turn blob, which @runtime will not build.

## Order

1. A MOCK first: one real diff (d3a16dd3, the REPLIES_N change) rendered the /m way, a screenshot at 412px, and its path
   in `notes\director-reports.md`. clint sees it before any build.
2. The design goes through @rnd (phone sections, the per-reply question).
3. Build after clint likes the mock. The room endpoint is @runtime's.

@review before each deploy.
