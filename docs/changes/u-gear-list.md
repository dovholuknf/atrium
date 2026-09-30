## Test plan

## @LETTER@. The terminal list's controls live in the gear

Before: `docs/backlog/ui/img/u-gear-list/terminal-list-before.jpg`. After: `terminal-list-after.jpg` and the gear
section in `gear-terminal-list.jpg`.

### @LETTER@1. The list has no controls of its own

Open the terminals view. Above the rows there is only the bar with the width buttons. The "sorted by activity · by
project · hiding inactive subagents" row and the "cache: ..." line are gone.

### @LETTER@2. The gear has a terminal list section

Open the gear, then "terminal list". It has sort, hide inactive, group and cache. Changing sort, hiding agents or
subagents or the grouping there changes the list the way the old row did, and the choices are the ones stored before.
The same section serves a phone. Run `HEADLESS_ONLY=gearTermList node scripts/test-board-headless.js`.

### @LETTER@3. The cache figures follow events

With the gear open on the section and a Claude card warming or going cold, the cache line changes with no request.

## @LETTER@. The growler reply: one rule for links, one send per choice

### @LETTER@4. Question text has no links on either surface

A question body holding `[x](https://...)` shows the text as it is on the desktop growler and on /m. The rule is the
desktop's: the review file does not argue against it, and a question is model output. Run `growlLinks`.

### @LETTER@5. A choice sends once

Double press a choice. One message goes, every choice button is disabled until the send answers, and a failed send
lights them again. Run `growlChoiceOnce`.
