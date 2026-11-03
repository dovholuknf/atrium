// ── ?ui=<state>: open one piece of the board from the URL and hold it open ──
//
// For looking at a piece of UI on a mock board: one link opens it, and nothing closes it until a reload without the
// param. Without `?ui=` this file does nothing. A new state is one row in UI_HOLD_STATES.
const UI_HOLD_STATES = {
  "add-repo": uiHoldAddRepo,
  "cog-menu": uiHoldCogMenu,
};

async function uiHoldUntil(what, ms) {
  const end = Date.now() + (ms || 30000);
  for (;;) {
    const v = what();
    if (v) return v;
    if (Date.now() > end) throw new Error("ui hold: timed out");
    await new Promise(r => requestAnimationFrame(r));
  }
}

// The card a link pasted in ctrl-k draws in the lower left while it is read. Drawn without the read, so nothing leaves
// the page, and a link opening has no timeout of its own to end it.
async function uiHoldAddRepo() {
  const url = "https://github.com/openziti/ziti/pull/4640/commits";
  const id = linkOpeningBegin(linkOpeningId(), url);
  linkOpeningName(id, "openziti/ziti#4640 Deprecate router create outside enrollment");
  linkOpeningStep(id, "recognising the link");
}

async function uiHoldCogMenu() {
  const card = await uiHoldUntil(() => cardsLoaded && cardList().find(t => t.supervised));
  switchView("terms");
  openTerm(card);
  const cog = await uiHoldUntil(() => termTask && termTask.id === card.id && document.getElementById("t-cog"));
  document.removeEventListener("click", closeCardMenu);
  closeCardMenu = () => {};
  const r = cog.getBoundingClientRect();
  termSettings({ preventDefault() {}, stopPropagation() {}, clientX: r.left, clientY: r.bottom });
}

// After load, so boot.js has routed the page and cannot switch the view out from under the held state.
addEventListener("load", () => {
  const name = new URLSearchParams(location.search).get("ui");
  const open = name && UI_HOLD_STATES[name];
  if (open) open().catch(e => toast("ui hold " + name, e.message));
});
