# u-new-tray-header-overlap. The tray's title runs into its first button

Status: not started. Owned by @ui. CSS only. Filed by the orchestrator 2026-10-01, from clint.

Screenshot: `D:\git\github\dovholuknf\atrium\.atrium\incoming\20261001-093613-pasted.png`.

## What is wrong

The notification tray's header ("THE TRAY" / "notifications") has no gap before its button row. The word
"notifications" touches the "turn off" button and its focus ring overlaps the last letters. Seen on the desktop
board at a narrow tray width.

## Wanted

- A fixed gap between the title and the first button. When the tray is too narrow for both on one line, the
  buttons wrap under the title rather than squeezing it.
- Also in the same shot, the header's deploy pill reads "nothing to deploy: no cod…", cut mid-word. Check whether
  the cut is intended (tooltip has the rest) or the pill wants a shorter text.
- Headless check at the narrowest tray width the board allows.
