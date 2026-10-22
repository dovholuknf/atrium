## Test plan

## @LETTER@. The restart cover returns after a reload mid-restart

### @LETTER@1. Reload during a planned restart

1. Run `HEADLESS_UNITS=atriumDown,bootClean node scripts/test-board-headless.js`.

**Expected:** both units pass. The unit sees the restart cover open on the board that loads after `down.html`, even when
the cover clears within one frame.
