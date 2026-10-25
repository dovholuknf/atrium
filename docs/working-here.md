# Working in this repo

The full text of the house rules the root `AGENTS.md` keeps as one line each: the backlog and change notes, testing,
branching and landing, and the conventions. Read it before a first change, a merge or a landing.

## Documentation

- **The backlog is on the hub.** Use `atrium_backlog` or `atrium backlog`. `docs/backlog.md` is frozen.
- **A change ships its own notes.** Never edit `CHANGELOG.md` or `docs/test-plan.md`. Write
  `changelog/<dept>/<yyyy-mm-dd>-<item>.md` and `docs/changes/<item>.md` (its test plan section).
  `docs/changes/README.md` has the format.

## Testing

Before handing back, run `bash scripts/test-touched.sh` (or `scripts/test-touched.ps1`) and nothing else. It diffs
against `claude/main` (`--base REF` for another) and runs the changed Go packages, the headless board units mapped
from the changed board files plus `bootClean`, and the node tests whose subject changed. It prints what it picked and
why, and exits non-zero on any failure. `--plan` prints the plan and stops.

The whole suite runs after landing, by whoever lands. A red there is a follow-up, not a reason to run it all first.
`check-board.sh`, `test-board-headless.js` and `test-board-sharded.js` refuse a full run without `FULL_SUITE=1`,
which CI (`scripts/ci.sh`), `make check` and `merge-check.ps1` set.

To run a few headless units by hand, name them: `HEADLESS_UNITS=bootClean,pulls node scripts/test-board-headless.js`,
or `node scripts/test-board-sharded.js --local --units bootClean,pulls` for parallel shards. `HEADLESS_LIST=1` lists
the units.

## Branching and landing work

A feature is not done until it lands cleanly on `dovholuknf/atrium:main`. That is the bar, not "it works on my
machine" and not "it is deployed to the live hub".

- `claude/main` is the integration branch, branched from `origin/main`. Every feature branches from it and merges
  back to it. Recreate it from `origin/main` when it is missing.
- A feature gets its own branch off `claude/main`, and merges back to `claude/main` when it is done.
- A worker is launched with `repo` and `branch`, not with a worktree made by hand: the room makes the card's home
  under its `card_root` and adds the worktree there. `scratch: true` is for throwaway work. With the room setting
  `card_root_required` on, the room refuses an agent launch whose `cwd` is outside the card root.
- `claude/main` has to fast-forward onto `origin/main`. If it does not, rebase it on `origin/main` and fix the
  conflict before calling anything done. Landing on the remote `main` is the finish line.
- A worker lands its branch with the merge queue: push it to the hub (`git push hub <branch>`), then run
  `atrium merge` (or `atrium_merge`). The hub merges it onto `claude/main`, runs `bash scripts/test-touched.sh` and
  answers merged with the sha, red with the failing tests, a conflict, or refused with the reason. A card cannot push
  `claude/main`, only the operator and the queue move it (`docs/rnd/merge-queue.md`). Never push `main` or a tag, and
  never ask the orchestrator to move `claude/main` by hand. `atrium merge --list` shows the queue, and landing on
  `main` is the operator's `atrium merge land`.

## Conventions

- Comments, especially in tests: delete every comment unless it is a one-line why for a non-obvious invariant. Trim
  a longer one to that line or remove it. Apply this to every comment in each function or test you touch, and never
  add or reword one while fixing a failing test. Never write one about what changed or how the cause was found: that
  goes in the commit, `REPORT.md` or `docs/troubleshooting-tests.md`.
- Smallest delta: a change keeps every name, signature, order and line it does not need, so the diff shows only what
  the change is. A reviewer reads every line of a diff, and a rename, reorder, re-wrap or reworded comment is a line
  to read that carries no change. When a name gains new behavior (a cache, a bound, a retry), keep the name the call
  sites use and rename the inner function instead, so no call site appears in the diff. Example: making
  `commitKnown` cached in `internal/gitsync` was done by adding `knownCommit` and changing every caller. Done right,
  `commitKnown` is the cached entry point and the raw git lookup is `commitHeld`, used only inside it.
- All paths in code use forward slashes (Go convention). Reading from gwt JSONs with backslashes is fine. Pass them
  through unchanged.
- Wrap markdown at 120 characters.
- No em-dashes (U+2014), no double-hyphen as a dash, no semicolons in prose.
- Files leave a card only to the card, an atrium path or an `open_paths` folder. A path opens when it is in the
  card, or under an atrium path (the room's card root, the directory of every card on the room live or archived, the
  room's repository clones and worktrees, WORKTREE_ROOT and each provider's worktree root), or under a folder in the
  room's `open_paths` setting (empty by default). The server decides after resolving symlinks and `..` through
  `internal/base/safepath`, and everything else answers `403` whether or not it exists. Upload, the editor's read and
  write, delete, zip and `files/open` stay against the card alone.
- Build with `make build` (writes to `build.claude/`). Never to the repo root.
