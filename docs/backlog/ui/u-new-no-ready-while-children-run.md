# u-new-no-ready-while-children-run: no "ready" alert while a card waits on its own children

Filed by the orchestrator, 2026-10-01, from clint's screenshot `.atrium/incoming/20261001-111735-pasted.png`.

## What happened

clint launched `pr-tlsuv-378` (card 01a0f7e0) to review openziti/tlsuv#378. It launched four child cards through
atrium (tagged `atrium:subagent`, `origin:agent`, spawned_by_id 01a0f7e0). At 11:17 it ended a turn with "I'm
waiting for its done message", and the board raised a desktop alert: "pr-tlsuv-378 ... is ready. finished its turn
and wants your next instruction". It did not want an instruction. It was waiting on a child it launched, and the
child's report woke it a few minutes later.

## The rule

A card that ends a turn while a card it spawned (`spawned_by_id` = this card) is still running is not waiting on
the human. No ready alert, no toast, no bell. The alert fires on the first turn end with no running children.

## Also

clint read the four children as things that "should be subagents". They are tagged as subagents, but three carry
the parent's title (`tlsuv/boringssl-keychain`) and sit in the list as peers of the parent. Show a spawned card
under its parent (or behind a count on the parent's row), not as a sibling with the same name.

## Owner

@ui for the alert and the list. @runtime only if the board needs a "has running children" fact it cannot derive
from `spawned_by_id` and `status`.
