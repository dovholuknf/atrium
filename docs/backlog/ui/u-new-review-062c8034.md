# Review: u-agent-idle 062c8034, and the ui queue docs to 40719e5b

- `062c8034` on claude/u-agent-idle, base landing `3212e158`. It changes `js/cardrules.js`, `js/board.js`,
  `js/cards.js`, `m/js/{bell,home,util}.js` and the headless units, and adds the item doc.
- claude/ui-director up to `40719e5b` is docs only: 7bbcc700, b7ae2115, 5605aadb, 5c126b60 and 40719e5b, in the ui
  queue.

Both merge onto landing `fe891b44` cleanly, together.

Verdict: **OK** for hub and room, and doc-ok for the queue docs. There are two Lows.

## Points

- **One predicate.** `agentIdle(t)` is needs-input, plus `origin:agent`, plus no open question. A question is either
  `seen.answered === false` with parsed or unparsed questions, or `asks_open > 0`. These use it:
  - `isWaiting` (board.js);
  - `waitingRow` (cards.js);
  - the /m needs list and groups (home.js);
  - `activityText` (util.js);
  - the bell (bell.js).

  needs-permission is never idle.
- **The tag is the right one.** `origin:agent` is set by `atrium_launch`, and the hub adds it on a cross-room launch
  (`a2a.go:38`). So the directors that the orchestrator started, which is the reported bug, carry it. An
  operator-launched card does not.
- **The bell is better than before.** It used to skip every `isDoer` card. Now an agent card that asks a question
  rings, and an idle one does not.
- **Load order.** `cardrules.js` loads before `util.js`, `home.js` and `bell.js` on /m, and before `board.js` and
  `cards.js` on the board.
- **Tests.** mAgentIdle covers the needs list, the tab-title count, the labels, the shared predicate on three cards,
  the All-mode groups, and the bell (no ring on first load, none for idle, a ring for a question and for an operator
  card). I read the units and did not run them, per the standing note.
- **Queue docs.** "for him" is gone from the hover item, and the mobile CSS pass item is a paraphrase. There are no
  quotes or paths.

## Lows

- **L1: an ask the parser misses hides the card.** A director that ends a turn asking the operator something in
  prose that the daemon does not parse as a question (no `open_questions`, no `questions_unparsed`) now reads
  "idle", and nothing rings. That is the trade the item chose. Note it in the item, so an "it didn't tell me" report
  is checked against the question parser first.
- **L2: the item doc and the code differ on tags.** The doc says "origin:agent, atrium:director or
  atrium:subagent". The code tests only `origin:agent`. That is fine today, since an agent-launched director has it.
  But a resident director that the operator tagged `atrium:director` still reads "waiting". Make the doc match the
  code. The item's second part, "report waiting" on a worker, is not in this commit, as the doc says.

Atrium-Verdict: room-ok 3212e158..062c8034
Atrium-Verdict: hub-ok 3212e158..062c8034
Atrium-Verdict: doc-ok fe891b44..40719e5b
Quality: a small, single-predicate fix applied on every surface, with a bell that is now more right than before.
m1mini commits are unsigned.
