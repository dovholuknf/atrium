## Test plan

## @LETTER@. One copy of the question, and the drawer

### @LETTER@1. One copy

1. Let two cards ask questions, one asking one and one asking two, and open the bell.

**Expected:** each question appears once, in its numbered row. No plain text repeats it.

### @LETTER@2. The drawer

1. Click the second question of the card that asked two.
2. Answer it, use back and next, then send.
3. Open another question and close the tray.

**Expected:** a drawer slides out of the tray's left edge, flush with it and level with the clicked question, holding
the answer box, "n of m answered" and the ctrl + enter hint. It slides back on close, on send and when the tray
closes. On a phone it is a sheet along the bottom as before.

### @LETTER@3. Headless

1. Run `node scripts/test-tray-answers.js`. Set `TRAY_SHOTS=dir` and `TRAY_SKIN=daylight` for PNGs.

**Expected:** passes.

Screenshots: `docs/changes/img/f-tray-question-one-copy-and-drawer/` on abyss and daylight.
