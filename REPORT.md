# u-new-reply-suggestions-build: RS1 and RS4

## What changed
- RS1 (6e8dec94): `internal/api/web/js/replies.js` with `replies.split`, `replies.of`, `replies.buttons`, `replies.label`.
  The board growler (`js/growl.js`), the phone growler (`m/js/growl.js`) and `/m` compose (`m/js/compose.js`) all use it.
  The two copies of the `{choices}` split are gone. `replies.js` loads before the growlers in `index.html` and
  `m/index.html`. Buttons are `rq-btn` in an `rq-row`, with the full words on `data-choice` and `title`. `stop` is
  appended to every list. Labels are cut to 28 on a phone (first clause, then a word boundary) and 60 on the board.
- RS4 (second commit): `replies.of` reads `card.replies` and `card.fixed` (the room's word), falls back to the ask's own
  `{choices}`, then the fixed four when `fixed`. `replies.wantsBox` marks an open question (`fixed === false`, nothing
  offered): no buttons, and compose focuses the box once. Both growlers pass `g.replies` and `g.fixed` through.
- Compose had no quick replies before (they were removed earlier, and `mCompose` asserts none for a card with no
  fields). It now draws them from `mStore.card(id)`, or from `opts.card` where a caller supplies it.
- Existing tests that counted choice buttons (`growlChoices`, `growlChoiceOnce`) now expect `stop` as well.
- Design file Status line updated. Changelog: `changelog/ui/2026-10-04-u-new-reply-suggestions-build.md`.

## Tests
Playwright is not installed in this worktree, so it ran against the main checkout's modules:
`NODE_PATH=/Users/claude/git/github/dovholuknf/atrium/node_modules HEADLESS_ONLY=<sections> node scripts/test-board-headless.js`
- New sections `replies` (RS1) and `repliesOf` (RS4), both in the default run: pass.
- `growlActions,growlAttention,growlChoiceOnce,growlChoices,growlLinks,growlModal,growlOff,growlOnIt,growlPhone,`
  `growlPopout,growlQuestionBody,growlQuiet,growlRemind,growlReplyGrow,growlStable,growlStack,mCompact,mCompose,`
  `mComposeImages,mGrowl,mGrowlQuestion,mSendFree,mTypeSteady,sayEnter,sendArrow`: all pass.
- The full default run was not made. No Go changed, so `go test` was not run.

## Screens
`docs/screens/u-new-reply-suggestions-build/`, `before-*` from c721241b and `after-*` from this branch, each as
`<tag>-<scene>-<surface>.png`. Scenes: `options-stop`, `fixed-four`, `open-question` (R5), `long-option`. Surfaces:
`board` (1400), `board-phone`, `m-growl`, `m-compose` (390 wide). Taken by `HEADLESS_ONLY=repliesShots REPLIES_SHOTS=<dir>
REPLIES_TAG=before|after`, which runs on a tree without `replies.js` too. In `before-*` compose has no buttons, and the
growlers have no `stop` and ignore `fixed`.

## What @runtime still owes
- RS2: `atrium ask --choice` and the help sentences.
- RS3: `closingOptions`, the `options` column, and `replies` (list) and `fixed` (bool) on the card view and the notify
  payload, which are also the growler row's fields. For an open question (R5) send `replies: []` and `fixed: false`
  explicitly. `fixed` absent is read as "the room does not say", so no box focus, to spare every ready card a keyboard.
- Until RS3 lands, a card has neither field and only an ask's `{choices}` draws buttons.

## Not done
- The growler does not focus its reply box for an open question, only compose does, because a redraw would steal focus.
- The board's compact terminal bar (`tcompose`) shows no quick replies: `mStore` is not on the board.
