# u-new-resume-spinner

## What changed
- `js/card-menu.js`: `resumeOpening` state per card (`opening`, then `slow`), `openingChip`, `openingPaint`,
  `openingStart`, `openingSlow`, `openingEnd`, `openingPoint`. `resumeNow` refuses a second resume while `opening`
  and points at the chip. `resumeStart` starts the state, and a failed launch turns it into the reason.
- `js/board.js`, `js/stack.js`: the chip is in the card and row templates. `css/cards.css`: its look.
- `js/terminal.js`, `js/terminal-links.js`: the pane's wait banner keeps "opening the conversation" through
  `openTerm` and the socket open, and the first output frame ends it. `css/terminal.css`: no spinner on the reason.
- `scripts/test-board-headless.js`: `resumeSpinner` section.
- Changelog `changelog/ui/2026-10-04-u-new-resume-spinner.md`. Design note in the item file.

## Design notes
- Bound is 30 seconds (`RESUME_BOUND_MS`). Past it, or on a failed launch, spinner becomes the reason and resume is
  allowed again. Late output still clears it.
- Resume into its own window clears the chip once the window opens. That window does not say "opening" itself.

## Incomplete: /m
/m has no card menu, long press or resume entry, so there is nothing to attach the spinner to and no test for it.
The question is written in the item file: add a resume action to /m first, or drop the clause.

## Tests
`NODE_PATH=<repo with node_modules>/node_modules HEADLESS_ONLY=resumeSpinner node scripts/test-board-headless.js`:
passed. It covers the chip, a second resume (one launch only), the pane line, first output, the bound and a failed
launch. Also passed: `HEADLESS_ONLY=busyGuard,land,pasteSpinner,cardRoute`. Run on claude/main 90ba936e the same
section only takes the "before" pictures (its asserts are for the new code).
`bash scripts/check-board.sh`: see the end of this file.

## PNGs, `docs/screens/u-new-resume-spinner/`
before-1-card, before-2-opening, before-3-pane-opening, before-4-output, before-6-failed (old code, nothing shown),
after-1-card, after-2-opening (row chip), after-3-pane-opening (pane banner), after-4-output, after-5-slow (the
reason, bound shortened to 400ms for the test), after-6-failed.

## check-board.sh
Two FAILs (`follow-scroll ... term.write's callback`, `onData ... isAutoReport`), identical on claude/main 90ba936e,
so not from this change. The title-allowlist list it prints is also pre-existing (hubrepos, changereq, index).
