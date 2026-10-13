## Test plan

## @LETTER@. Items, stage 1

An item is opt-in per launch: `item: true` on `atrium_launch` and `atrium_launch_worker`, or `atrium launch --item`. The
card carries the tag `atrium:item`. Nothing is stored beyond the tag, so there is no migration. Room-side Go, so a room
deploy follows. The design is `docs/rnd/atrium-items.md` and `docs/rnd/atrium-items-decisions.md`.

### @LETTER@1. An item worker is told nothing about a launcher

1. From a director, call `atrium_launch_worker` with `item: true`, a `brief` and a `cwd`.
2. Read the new worker's BRIEF.md prompt in its terminal, and its `atrium_done` and `atrium_blocked` tool descriptions.

**Expected:** the card has the tag `atrium:item`. The ending lines say "call either atrium_done or atrium_blocked" and
never say "launcher". The commit-visible line that names the launcher is not added. The first prompt in the card's
history is from `atrium`, not from the launcher.

### @LETTER@2. The item toolset

1. In the item worker, list the control tools.
2. In an ordinary worker launched the same way without `item`, list them again.

**Expected:** the item worker has no `atrium_say`, `atrium_peers`, `atrium_task` or `atrium_report`, and keeps
`atrium_done`, `atrium_blocked`, `atrium_status` and the git, files, CI and resources tools. The ordinary worker has all
four, and its ending lines are the ones it always had.

### @LETTER@3. A running item cannot say

1. `atrium tell <launcher> hello` from the item worker's shell, or POST `/v1/say` with `from` set to the item.
2. Say something TO the item from the board, and from the launcher.

**Expected:** the first is refused with "item workers cannot say anything" and nothing is queued. The second is
delivered. After the item has ended (`done` or `dead`) a say from it is no longer refused by this rule.

### @LETTER@4. A result is one notice

1. Have the item worker call `atrium_done` with a sha, then again with another sha.

**Expected:** the launcher gets ONE notice, keyed `(card, generation, result)`. An ordinary worker's two dones are two
notices, as before. A `blocked` from an item has its own key (`blocked`), so a worker that was blocked and later
finished tells its launcher both. Only item cards use the new key. Every other card keeps the time-based key.
