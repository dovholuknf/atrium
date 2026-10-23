Docs: none, website/docs/cli.md and docs/repo-reference.md are updated on the branch

## Test plan

## @LETTER@. A human's CLI reaches the backlog, CI and fyi from any room

### @LETTER@1. The backlog from a room that is not the hub's machine

1. On a room whose machine does not run the hub, run `atrium backlog file --dept ops --title "cli parity check"`.
2. Run `atrium backlog priority <id> low`, then `atrium backlog status <id> dropped`.
3. Run `atrium backlog show <id>`.

**Expected:** each write answers the filed id, the new priority and the new status, with no "written only from the
machine the hub runs on" refusal. `show` gives `[dropped]`, priority `low`, filed on that room.

### @LETTER@2. The backlog with the hub down

1. Run `atrium backlog --board-addr 127.0.0.1:1 list`.

**Expected:** a named board is asked alone: the error says the hub is not answering at `127.0.0.1:1`, and nothing is
asked through the room.

### @LETTER@3. A CI log to a file

1. Run `atrium ci runs dovholuknf/atrium --limit 3` and pick a failed run's id.
2. Run `atrium ci run dovholuknf/atrium <id>`.
3. Run `atrium ci log dovholuknf/atrium --run <id>`.

**Expected:** `runs` lists three runs with their conclusions. `run` lists the jobs and their steps. `log` prints a path
under the temp folder's `atrium-ci/` and a line count, and the file holds the failed steps' log.

### @LETTER@4. An fyi from the CLI

1. Run `atrium tell <an orchestrator's handle> "cli fyi check" --kind fyi` while it is mid-turn.
2. Run `atrium tell <any handle> hi --kind urgent`.

**Expected:** the first is delivered at the end of the orchestrator's turn and no answer is owed. The second is
refused with "--kind is fyi or needs" and nothing is sent.
