# A runner whose worktree is gone is asked to leave

Item 89. Five finished workers had their worktrees removed by hand (`git worktree remove --force`) while their
runners were still supervised at a prompt, on cards in `done`. Git unregistered the worktree, deleted what it could,
and failed on the directory the runner's process was sitting in. The runner is the holder, not a leftover child.

## The rule

On each reaper tick, for every runner in `d.sup.all()` (never `shells`): if its card is tagged `atrium:subagent` and
its directory is gone, and it was gone on the previous tick too, ask it to leave.

Being `done` is never the reason. A `done` runner stays up so a director can send it back and so a say can reach it
(item 83). The reason is that there is no longer a place for the session to work. A session in a directory that has no
repository has nothing left to do.

## Which path

The runner's launch cwd (`r.spec.cwd`) when the runner has a spec, else the card's `Worktree`.

The cwd is the directory the process holds, so it is exactly the lock that blocks the removal. `Worktree` is what a
session last reported through its hooks, which can drift (a session that `cd`s, a resume that reports late). But a
runner adopted without a spec has only the card. Cwd first, card second, and if neither is known nothing is checked.

## What "gone" means

- `Stat` on the directory says it does not exist, or
- it exists and `Lstat` on `<dir>/.git` says it does not exist (a worktree's `.git` is a file, a checkout's is a
  directory, both count as present). This is what git leaves after it unregisters a worktree it could not delete.

Any other error, a permission failure or a sharing violation, is "not known" and counts as present. Only a definite
"does not exist" ever counts.

## The two-tick guard

The daemon remembers, in memory only, the card id and the path that read as gone last tick. Gone now and the same path
gone last tick means act. Any tick that reads present drops the entry. A directory mid-creation (a launch that races
its own `git worktree add`) or a single odd stat never ends a session. Nothing is stored: a restart forgets it and
costs one more tick, which is the right way for it to be wrong.

## Acting

1. Mark the card as being wound down, in memory, so a later tick does not start a second one. Cleared when the
   goroutine returns.
2. Append one `notified` event, payload `by: reaper`, `detected: its worktree was removed`, `path`. `exited` is wrong
   because nothing has exited yet, and `exited`/`launched` also move the work ledger. `notified` is what atrium already
   uses for a thing it did to a card. A new kind would cost an event table rebuild.
3. Log one line.
   If the append fails it is logged and the ask still goes ahead: the event is a diagnostic, and a stuck runner
   should not be kept to preserve history.
4. In a goroutine, `windDown(r, 10s, d.exitKeysFor(id))`, the path `stopOne` uses. It blocks for seconds, so never on
   the tick.

Nothing is deleted. The resume id, the card and its history stay.

## What each card ends in

`awaitExit` records the real `exited` event, then files the card `dead` unless it is `shelved` or `done`.

| Card was | Ends |
| --- | --- |
| `done` | stays `done`, no runner |
| `running`, `needs-input`, `needs-permission` | `dead` (a worker that lost its directory mid-task is not working) |
| `shelved` | stays `shelved` |

## False positives considered

- A worker launched in a SUBDIRECTORY of a repository: its cwd has no `.git` of its own. Workers launched by an
  orchestrator sit at a worktree root, and only `atrium:subagent` cards are considered, but this is a real gap. It is
  not walked up for a `.git`, because a removed worktree's parent could be inside another checkout and that would turn
  the check into never firing.
- A directory replaced by delete and recreate (git checkout of a rebuilt worktree): present again on the next tick,
  guard resets.
- A network or removable drive that drops: `Stat` returns something other than not-exist on most stacks. If it does
  say not-exist twice, twenty seconds apart, the session had lost its cwd anyway.
- A human's terminal, a fixture, a shell: not `atrium:subagent`, or not in `runners`. Never looked at.
- A runner already exiting on its own: the goroutine's `windDown` returns at once when `done` is closed.
- A card deleted while a tick reads it: the store read fails and the runner is skipped.
