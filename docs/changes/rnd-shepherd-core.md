Docs: none, the command is hidden from --help until rnd-shepherd-accept-land lands, and the design is docs/rnd/script-minion.md

## Test plan

## @LETTER@. The shepherd takes an item to a held packet

### @LETTER@1. A trivial bug item, launch to held

1. File a small backlog bug on the hub whose fix is one line. From the main checkout run
   `atrium shepherd run <id> --repo <main checkout>`.

**Expected:** the shepherd makes `<repo>-worktrees/<id>` on `claude/<id>` at `claude/main`, writes `BRIEF.md` there,
launches an isolated item owned by `human`, then polls. When the worker calls `atrium_done` the shepherd stages
`claude/land-<id>` in `<repo>-worktrees/land-<id>`, runs gofmt, the globals check and `scripts/test-touched.sh`, prints
the packet and stops in phase `held`. `GET /_hub/shepherds/<id>` shows the row. No model ran but the worker.

### @LETTER@2. A killed shepherd resumes

1. Run it, and kill it with Ctrl-C while the worker runs. Run the same command again.

**Expected:** the row says `wait`, the second run launches no second worker, and carries on from the poll.

### @LETTER@3. A blocked worker, an empty range, a conflict

1. Have the worker block. Run the shepherd again after unblocking it. Separately, have a worker finish with no commit.

**Expected:** a blocked worker stops the shepherd in `held-blocked` with its ask in the packet, and running it again
goes back to waiting. An empty range or a failed cherry-pick stops in `exception` with the evidence on the row, and
nothing lands in either case.

### @LETTER@4. The command is hidden

1. Run `atrium --help`.

**Expected:** `shepherd` is not listed. `atrium shepherd run --help` still answers.

### Decisions where the design was silent

- The worker's worktree is a worktree of the main checkout, so collect reads its branch as a local ref and fetches
  nothing. A remote room needs the worktree made over ssh and `atrium rooms git collect`, and is not built.
- The hold posts the packet to the row and stops. The human notice and the accept, reject and correct verbs are
  rnd-shepherd-accept-land's.
- The staging branch is `claude/land-<id>`, as the brief says, not `claude/shepherd-<id>` as recommendation 6 does.
- A launch that died after the card started and before the row was saved starts a second card on resume. It is one
  window and the launch step says so.
- A red whose every failing test is in `docs/known-failing-tests.md` passes and says so in the packet.
