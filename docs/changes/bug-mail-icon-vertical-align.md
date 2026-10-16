## Test plan

## @LETTER@. Mail chip envelope is vertically centred

Pictures: `docs/changes/img/bug-mail-icon-vertical-align/` (`desktop-before.png`, `desktop-after.png`, `phone-before.png`,
`phone-after.png`, each cropped to a card's chip row with a message count). Headless: `HEADLESS_UNITS=mailChip,bootClean
node scripts/test-board-headless.js`, with `MAIL_SHOTS=<dir>` and `MAIL_SHOT_TAG=<name>` to write the pictures.

The cause: the chip drew U+2709 as text. The glyph comes from a fallback font and its ink sits low in the line box, so
it was never centred against the count. It is now a small inline svg in a flex chip with `align-items: center`
(`.chip.mail` in `css/cards.css`, `answerChip` in `js/peek-answers.js`).

### @LETTER@1. Envelope alignment

1. Answer two of a card's questions in its peek and click away, so the card holds drafts. Look at its chip row on the
   board, then on a phone.

**Expected:** the envelope, its count `2` and the neighbouring chips share one centre line, within a pixel.
