# haiku-trial: pop-out on an already popped-out card must raise it

SHA: 790c410f (code). This report is committed on top of it.

## Root cause

The dialog "that card is open in another window" comes from `takeSoloCard` in `solo.js`, which the freshly opened
pop-out runs on boot. It asks whenever `stillOutThere` gets an answer. `popOutTask` did the right thing (name lookup,
then ask the holder to close, then open a fresh window), but the fresh window then asked the user anyway whenever a
holder still answered the roll call: an older build that ignores `solo-yield`, or a window that had not let go yet.

Name lookup: I checked the naming paths and they agree (`bareId` is the same everywhere; names are set on open, on the
alias boot path and in `soloSwitch`). I could not reproduce a name miss in a browser on this machine. The gap I did
find is that the `#term=<id>` boot path never set `window.name`, so a window opened from a pasted url could never be
raised by name.

## Change

- `popOutTask` writes a one-use handover flag (`atrium.handover.<bareId>`, 15 s) when it has asked a holder to let go
  and is about to open the replacement.
- `takeSoloCard` consumes that flag and attaches without the dialog. It re-sends `solo-yield` to the holder.
- `bootTerminalOnly` names a `#term=` window `atrium-term-<bareId>`, so the board can raise it by name.

## Tests

`node scripts/test-board-sharded.js --local --units popoutRaise,cardUrlWinName`: 2 of 2 passed (popoutRaise PASS,
cardUrlWinName PASS, no skips). Changes to the units: popoutRaise now expects a click on a named pasted-url window to
raise it (not close it), and that the replacement window shows no dialog. cardUrlWinName gained a `#term=` naming case.
I did not confirm that the replacement-dialog assertion fails on the old code.

## Timing

- T1 13:47:28 EDT, T2 13:51:04 EDT (the last `date` before writing this report): about 4 minutes wall time.
- Tokens T1..T2 (assistant entries, deduped by message.id): input 42, cache creation 57,217, cache read 768,943,
  output 15,369. Total 841,571.
- Slowest part: the first `npm install` and the sharded run's retry. The suite took 8.8 s on the second run.
- Faster next time: a single-unit run first, and a browser repro of the name miss before choosing a fix.
