# Merge hygiene

Features MUST be able to merge back to `main` cleanly. This is a hard requirement, not a preference. A feature
that cannot land on `main` without a human untangling a doubled file is not finished.

## What went wrong once, so it does not go wrong again

A large integration branch was squash-merged into a `main` that had moved on. It built with `go build` and it
passed `go vet`, and it was still broken: the entire settings dialog in `internal/api/web/index.html` was
present TWICE, with about forty duplicate element ids. `getElementById` silently returns the first match, so
half the settings controls were dead and nothing errored. Only `bash scripts/check-board.sh` caught it.

Two separate failures, one root cause.

- **Two branches rewrote the same region of one big file.** git's line merge cannot align two independent
  rewrites of the same block, so it keeps BOTH. The result compiles and is nonsense.
- **Two branches grabbed the same migration number.** `0053_provider` and `0053_shell_is_not_a_runner` were both
  added at the end of the slice on branches that shared a base, so both claimed `0053`.

## The rules

1. **Stay close to `main`.** Rebase a feature branch on `main`, or merge `main` into it, often enough that
   divergence stays small. The pain is linear in how far the branches have drifted, and a long-lived branch off
   an old base is the thing that produces an untangleable merge.

2. **Do not let two live branches restructure the same section in parallel.** The known hotspots are
   `internal/api/web/index.html` (one file, the whole board) and `internal/store/schema.go` (one slice, every
   migration). If two pieces of work both need to move the same section, sequence them.

3. **Migration numbers collide.** Append at the END of the slice, and expect to renumber on merge. A migration
   is recorded by NAME, so two distinct names sharing a number still both run, but the numbers must be made
   unique and monotonic before the branch lands.

4. **`go build` is not enough after a merge.** It cannot see a duplicated DOM id or a resurrected setting. After
   any non-trivial merge, run:
   - `bash scripts/check-board.sh` for duplicate ids and duplicate settings or runner panes.
   - a grep for every symbol the destination branch DELETED (for example a removed setting like
     `worktree_command`), because a merge from an older branch can quietly bring it back.

5. **Prefer a real `git merge` over `--squash` for a large integration.** A normal merge surfaces
   reintroductions as conflicts you resolve and review. A squash flattens everything into one blob and lets a
   silent concatenation through as if it were intended.
