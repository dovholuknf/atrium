Docs: none, docs-only move

The root `AGENTS.md` keeps the index and one line per rule that must hold before any other file is read. Nothing was
lost or changed. Each moved block and its new home:

| Block in the old root | New home |
| --- | --- |
| Intro, "this file holds only what every agent needs" | trimmed in place, the facts kept |
| Index descriptions (migration rules, the halt, picker, refusals, ptyhost protocol, reading order) | the indexed file itself, each already held them |
| Documentation (backlog on the hub, a change ships its own notes) | `docs/working-here.md` Documentation, one line in root |
| Testing (test-touched, `FULL_SUITE=1`, headless units by hand) | `docs/working-here.md` Testing, one line in root |
| Branching and landing work | `docs/working-here.md` Branching and landing work, one line in root |
| Conventions: comments, forward slashes, wrap, prose, `make build` | `docs/working-here.md` Conventions, one line each in root |
| Conventions: the room link is mutual TLS, the join string | `internal/link/AGENTS.md` The shape that must not change |

`docs/README.md` lists `docs/working-here.md` in its map.

## Test plan

## @LETTER@. The root AGENTS.md is lean and loses nothing

### @LETTER@1. Memory cost

1. In a worktree of this branch, run `claude -p "/context"` from PowerShell (Git Bash rewrites `/context`).
2. The Project memory file `AGENTS.md` is about 0.8k tokens, down from 2.3k.

### @LETTER@2. Every rule has a home

1. For each row of the table above, open the new home and find the block's text.
2. Every file the root index names exists.
