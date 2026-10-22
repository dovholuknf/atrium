# AGENTS.md (atrium)

Atrium organizes coding agents: durable state, a permission gate, a web board, and terminals it owns. A card outlives
the process it describes, state lives on disk, and storage failure halts rather than degrades. One binary: `atrium run`
is the hub, `atrium room` is a room per machine, `atrium ptyhost` holds terminals. This file holds only what every
agent needs before touching anything. The rest is indexed below and loads on demand.

## Index: read when

| Read | When |
| --- | --- |
| `docs/repo-reference.md` | you need what runs and on which port, the stack, the repo layout, the subcommand table, key ideas or the out-of-scope lists |
| `docs/README.md` | you need to find any doc. Reading order: `docs/how-atrium-works.md`, `docs/fabric/hub-and-rooms.md`, `docs/runtime/agent-messaging.md` |
| `docs/changes/README.md` | you are writing the notes a change ships |
| `internal/store/AGENTS.md` | you touch the schema or any query: migration rules (add at the END, tolerate already being there), the halt |
| `internal/daemon/AGENTS.md` | you touch the room: the permission chain and its order, resilience guarantees, supervision, the peer bus |
| `internal/api/AGENTS.md` | you touch the room's HTTP surface: files never leave a card, the bounded directory picker, settings refusals |
| `internal/api/web/AGENTS.md` | you touch the board: index.html, css, js, the phone |
| `internal/cli/AGENTS.md` | you touch a subcommand |
| `internal/link/AGENTS.md`, `internal/hubstore/AGENTS.md`, `internal/gitsync/AGENTS.md` | you touch the hub, its store, or git between hub and rooms |
| `internal/ptyhost/AGENTS.md` | you touch the pty host (`docs/terminal/ptyhost-protocol.md`) |
| `internal/guard/AGENTS.md`, `internal/claudeconf/AGENTS.md`, `internal/safepath/AGENTS.md` | you touch the guard, a runner's hook config, or path containment |
| `scripts/AGENTS.md` | you touch a script or CI |

Rules that must survive without the file loaded: never edit a migration already shipped. A hook must never fail a
session. Never reorder the permission chain. Files never leave a card.

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
- A worker is launched with `repo` and `branch`, not with a worktree made by hand: the room makes the card's home under its
  `card_root` and adds the worktree there. `scratch: true` is for throwaway work. With the room setting
  `card_root_required` on, the room refuses an agent launch whose `cwd` is outside the card root.
- `claude/main` has to fast-forward onto `origin/main`. If it does not, rebase it on `origin/main` and fix the
  conflict before calling anything done. Landing on the remote `main` is the finish line.
- A merge worker merges the branch, runs `bash scripts/test-touched.sh`, and lands with
  `git push hub HEAD:claude/main`. The hub takes that only as a fast-forward of its `claude/main`, and the orchestrator's
  checkout follows it (`docs/fabric/git-sync-design.md` 4.3). A refusal means `claude/main` moved: merge again and
  push again. Never push `main` or a tag, and never ask the orchestrator to move `claude/main` by hand.

## Conventions

- Comments, especially in tests: delete every comment unless it is a one-line why for a non-obvious invariant. Trim
  a longer one to that line or remove it. Apply this to every comment in each function or test you touch, and never
  add or reword one while fixing a failing test. Never write one about what changed or how the cause was found: that
  goes in the commit, `REPORT.md` or `docs/troubleshooting-tests.md`.
- All paths in code use forward slashes (Go convention). Reading from gwt JSONs with backslashes is fine; pass
  through unchanged.
- Wrap markdown at 120 characters.
- No em-dashes (U+2014), no double-hyphen as a dash, no semicolons in prose.
- Build with `make build` (writes to `build.claude/`). Never to the repo root.
- The room link is mutual TLS. `atrium rooms add <name>` prints a one-time join string, and joining saves a
  certificate the hub signs. The name in that certificate is the room's identity, and a room the hub has no record
  of cannot attach.
