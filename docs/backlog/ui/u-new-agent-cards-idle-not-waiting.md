# Agent-launched cards read "idle", not "waiting for you"

From clint, 2026-10-02, by the orchestrator, from a phone screenshot on sg4 (not readable from m1mini). Queued after
the /m bubbles item (u-new-m-bubbles-copy-reply.md). Lands through @review.

## Bug

On /m, the directors (rnd, ui, runtime) show "waiting for you" when they are only idle between turns, waiting on their
workers. None is waiting on the operator.

## Fix

A card launched by an agent (tag origin:agent; the code tests only that tag) that ends a turn with no open
question reads "idle", not "waiting for you", and drops out of the waiting-for-you count and the bell. "Waiting for
you" stays for a card with an open question, a permission ask, or a card the operator launched. Places to look: the
"ready" reason in m/js/home.js, activityText in m/js/util.js, the bell in m/js/bell.js (it already has an isDoer
test, see cardrules.js DOER_TAG), and the desktop board's count and wait labels (stack.js). One shared predicate,
both surfaces. Headless: an agent card with no question reads idle and is not counted or belled; a question, a
permission ask or an operator-launched card still reads waiting.

## Second part: "report waiting" on a worker card

The /m needs list shows "report waiting" only for a card whose spawned_by is @human (home.js). A worker card showed it
although its director launched it, and the director never received the report (found only when asked). Check where a
worker's report is addressed and surfaced: if the report is addressed to the operator rather than the launching
session, or spawned_by is wrong for a card launched by an agent, that is the delivery half and belongs to @runtime;
file it to them with what was found. The board half is only the label.

## Known limit

A director that asks the operator something in prose the question parser misses reads idle and does not ring. Accepted
(review L1); a parser miss was already invisible in the open-questions count.
