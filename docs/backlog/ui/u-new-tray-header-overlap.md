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

## Design note

Status: done 2026-10-04.

- The tray head used to be one row that never wrapped, with the title ellipsised to make room. That is replaced by a
  wrapping row. The title keeps the width of its own words and a fixed gap (`--sp-4`) separates it from the first
  button, with `--sp-2` between wrapped lines. At 320px the last two buttons wrap onto a second line.
- At 900px and under the title was not shown at all, because the phone rules hide every bare `.grow` and the tray
  title sits in one. `#toastlog .dlg-head > .grow` now sets `display: block`. The overlap in the shot was at the
  desktop width, where the gap was 6.6px.
- The deploy pill: the cut is not intended and is already fixed. On a desktop (over 900px) the pill draws its short
  form ("ready", "not ready 2/3") and the full line is the tooltip, see `drShort` and css/topnav.css. The "no cod…"
  shot predates that. No change here.
- Test: headless section `trayHead` at 320, 360, 901 and 1400px.
- Screens: `docs/screens/u-new-tray-header-overlap/`.
