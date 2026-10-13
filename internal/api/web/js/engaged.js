// THE CARD YOU ARE ON, remembered for a moment.
//
// `readySilenced` asks whether this window is focused and has the card's terminal attached right now. That is true for
// the whole time somebody types, except for the instants the pane is rebuilt under them (a reattach after the socket
// dropped, a context cycle, a switch settling), when `term` is null or another card's. An alert raised in one of those
// instants said the card was not being looked at, and a growler for the very terminal being typed in was drawn and
// logged. So the last card the operator typed into, or was seen watching, is kept for ENGAGED_MS and counts as on it
// while this window is focused and no OTHER card's terminal is attached.
//
// Said by the terminal's input handler (every keystroke) and by `readySilenced` (every look that found it attached).
const ENGAGED_MS = 20000;
let engagedCard = "", engagedAt = 0;

function noteEngaged(id, now) {
  if (!id) return;
  engagedCard = bareId(id);
  engagedAt = now == null ? Date.now() : now;
}

function engagedWith(id, now) {
  if (!id || !engagedCard || bareId(id) !== engagedCard) return false;
  if ((now == null ? Date.now() : now) - engagedAt > ENGAGED_MS) return false;
  // Another card's terminal on screen means the operator moved on, whatever they typed a second ago.
  try { if (term && termTask && !sameCard(termTask.id, id)) return false; } catch (e) {}
  return true;
}
