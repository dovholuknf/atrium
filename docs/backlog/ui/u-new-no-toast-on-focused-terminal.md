# u-new-no-toast-on-focused-terminal. No question toast for the terminal you are typing in

Status: approved by clint as a pause exception, 2026-10-01. Owned by @ui. Filed by the orchestrator, from clint.

Screenshots: `D:\git\github\dovholuknf\atrium\.atrium\incoming\20261001-100246-pasted.png` and
`...\20261001-165122-pasted.png` on sg4.

## What happened

clint was on the orchestrator's terminal, attached and focused, reading its reply. The board still raised an
"orchestrator asked a question" toast over the top right of that same terminal, repeating the question he was
looking at.

clint: "I don't think that popup needs to happen if I am ON that terminal and it's focused, right?"

## Wanted

- A card's question (and its done/ready alert) raises no toast, desktop notification or sound while that card is
  the attached terminal, the board tab is visible, and the window has focus. It still goes in the notification log.
- Any one of those false (other card attached, tab hidden, window blurred, popped-out window for a different card)
  and it alerts as today.
- A popped-out window counts: focused on that card's own popped-out terminal means no toast in any window.
- Permission requests: say in the design. Recommend still showing them, since they block the card.

## Second report, 2026-10-01 16:51: "open" leaves the toast up

The same thing happened again on the orchestrator's focused terminal. clint also clicked "open" on the toast and the
toast stayed. Wanted: "open" dismisses the toast, the same as "dismiss this", after it attaches the card.
