## Test plan

## @LETTER@. The orchestrator is never typed at by atrium's own notices

Run on a room with a card that launches workers. Room side, so it needs a room running this build.

### @LETTER@1. A silent stop is held for the orchestrator

1. Tag the launching card `atrium:orchestrator` (or `atrium:hold-notices`) on the board.
2. From it, launch a worker and give it a prompt that ends its turn without `atrium_report` or `atrium_say`.
3. Wait for the turn to end.

**Expected:** nothing is typed into the launcher's terminal and nothing is queued for it: its card shows no queued
message. The board rings once with the worker's silent-stop text. From the launcher, `atrium_task` with `card` empty
and `notices: true` answers one notice, `source: silent-stop`, naming the worker. The worker's work item says
`silent-stop notice held for <launcher>`.

### @LETTER@2. A report still arrives

1. Have the same worker call `atrium_report` with `status: done`.

**Expected:** the report is typed or queued for the launcher exactly as before. It is not held.

### @LETTER@3. A session that ends without a report is held too

1. Exit the worker from the board without a report.

**Expected:** the `ended without a final report` notice is on the launcher's `notices`, not in its terminal.

### @LETTER@4. An untagged launcher is unchanged

1. Remove the tag and repeat @LETTER@1.

**Expected:** the silent-stop notice is typed into the launcher's terminal, as before.

## For the board (@ui)

A held notice is one server event on the room's stream, `notice`:

```json
{"task_id": "<launcher card>", "about_card": "<worker card>", "source": "silent-stop", "toast": "<the notice text>"}
```

`source` is one of `silent-stop`, `long-tool`, `context-size`, `auto-new-context`, `ended`. The launcher's card is
republished alongside it. The board should ring its bell with `toast`, titled by `source`, and a click should land on
`about_card`. The event is sent once per notice, and the room already deduplicates notices per worker and event, so the
board needs no dedupe of its own. Until the board handles it, a held notice is on the launcher's card and on the
worker's work item, and nothing rings.
