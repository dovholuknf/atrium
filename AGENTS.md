# AGENTS.md (atrium)

Atrium organizes coding agents. `atrium run` is the hub, `atrium room` a room per machine, `atrium ptyhost` holds
terminals. A card outlives its process, state lives on disk, and storage failure halts rather than degrades.

## Read when

| Read | When you touch or need |
| --- | --- |
| `docs/agents.tests.md` | you write, change or land a test: the checks to run first, no flat waits |
| `docs/working-here.md` | a change, a merge or a landing: the full text of every rule below |
| `docs/repo-reference.md` | ports, stack, layout, subcommands, out of scope |
| `docs/README.md` | any other doc |
| `docs/changes/README.md` | the notes a change ships |
| `internal/store/AGENTS.md` | the schema or a query |
| `internal/daemon/AGENTS.md` | the room, the permission chain, supervision, the peer bus |
| `internal/api/AGENTS.md` | the room's HTTP surface |
| `internal/api/web/AGENTS.md` | the board |
| `internal/cli/AGENTS.md` | a subcommand |
| `internal/link/`, `internal/hubstore/`, `internal/gitsync/` `AGENTS.md` | the hub, its store, the room link, git sync |
| `internal/ptyhost/AGENTS.md` | the pty host |
| `internal/guard/`, `internal/base/claudeconf/`, `internal/base/safepath/` `AGENTS.md` | the guard, hook config, paths |
| `scripts/AGENTS.md` | a script or CI |

## Rules

- Never edit a shipped migration. A hook never fails a session. Never reorder the permission chain. Files leave
  a card only to the card, an atrium path or an `open_paths` folder.
- Never edit `CHANGELOG.md`, `docs/test-plan.md` or `docs/backlog.md`. The backlog is on the hub.
- Before handing back, run `bash scripts/test-touched.sh` and nothing else.
- Branch off `claude/main`. Never push `main` or a tag.
- Build with `make build` into `build.claude/`, never the repo root.
- Smallest delta: keep every name, signature, order and line the change does not need. When a name gains new
  behavior, keep it and rename the inner one.
- Delete every comment that is not a one-line why for a non-obvious invariant, in each function or test you touch.
- Forward slashes in code paths. Markdown at 120 columns, no em-dashes, no double-hyphen dash, no semicolons in prose.
