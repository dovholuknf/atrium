// Resume from the phone: a long press on a card in the list opens a small sheet with "resume the last conversation".
// The launch, the "opening the conversation" state, the 30 second bound and the failure wording are js/resume-opening.js,
// the same copy the board's card menu uses. This file is only the gesture, the sheet and the wait for first output.
//
// The press has to leave scroll and text selection alone: it is cancelled by any movement past SLOP, by the finger
// lifting, by the system taking the touch (scroll) and by the list scrolling under it. The row is not selectable, the
// platform's own long-press callout is turned off for it, and the contextmenu event (what Android and a mouse send for
// a long press or a right click) opens the same sheet instead of the browser's.
(function () {
  "use strict";

  const HOLD_MS = 550;
  const SLOP = 10;

  let host = null, sheet = null, openFor = "";
  let timer = 0, startX = 0, startY = 0, pressRow = null, swallow = 0;
  // The card's activity when the resume was asked for. First output is the card moving past it.
  const baseline = new Map();

  const actOf = t => t ? [t.last_activity_at, t.prompted_at, t.output_at].join("|") : "";
  const key = id => window.mNet.bareId(id);

  // ── the sheet ────────────────────────────────────────────────────────────
  function build() {
    if (sheet) return;
    sheet = document.createElement("div");
    sheet.id = "m-rmenu";
    sheet.className = "rmenu";
    sheet.hidden = true;
    sheet.innerHTML = '<div class="rmenu-back" data-close="1"></div>' +
      '<div class="rmenu-box" role="dialog" aria-label="card actions"><p class="rmenu-name"></p>' +
      '<button type="button" class="rmenu-go" data-act="resume"></button>' +
      '<p class="rmenu-why" role="status"></p>' +
      '<button type="button" class="rmenu-x" data-close="1">cancel</button></div>';
    document.body.appendChild(sheet);
    sheet.addEventListener("click", e => {
      if (e.target.closest("[data-close]")) { close(); return; }
      const b = e.target.closest("[data-act]");
      if (b && !b.disabled) { const id = openFor; close(); resume(id); }
    });
  }

  function open(id) {
    const t = window.mStore.card(id);
    if (!t) return;
    build();
    openFor = id;
    const why = cannotResumeCard(t);
    const o = openingState(id);
    const go = sheet.querySelector(".rmenu-go");
    sheet.querySelector(".rmenu-name").textContent = window.mUtil.cardName(t).main;
    go.textContent = o && o.state === "opening" ? openingText + "…" : "resume the last conversation";
    go.disabled = !!why;
    sheet.querySelector(".rmenu-why").textContent = why;
    sheet.hidden = false;
    document.body.classList.add("sheet-open");
    if (navigator.vibrate) { try { navigator.vibrate(12); } catch (e) {} }
  }

  function close() {
    if (!sheet) return;
    sheet.hidden = true;
    openFor = "";
    if (!document.querySelector("#m-card.on")) document.body.classList.remove("sheet-open");
  }

  // ── the resume ───────────────────────────────────────────────────────────
  async function resume(id) {
    const t = window.mStore.card(id);
    if (!t || cannotResumeCard(t)) return;
    const o = openingState(id);
    if (o && o.state === "opening") { point(id); return; }
    baseline.set(key(id), actOf(t));
    openingStart(id);
    try {
      await window.mNet.api("/v1/launch", {
        method: "POST", headers: { "Content-Type": "application/json" },
        body: JSON.stringify(resumeLaunchBody(id, t, t.resume_id, false))
      });
    } catch (e) {
      openingSlow(id, resumeFailText(e));
      return;
    }
    // Do not wait for the next poll to find out the card is running.
    window.mNet.refresh();
  }

  // A second resume while the first is opening: the row pulses, nothing is launched.
  function point(id) {
    document.querySelectorAll(".row").forEach(el => {
      if (key(el.dataset.id) !== key(id)) return;
      const c = el.querySelector(".opening");
      if (!c) return;
      c.classList.remove("nudge");
      void c.offsetWidth;
      c.classList.add("nudge");
    });
  }

  // The card is running and has moved since the click: the runner printed. A card that has gone ends it too.
  function check() {
    resumeOpening.forEach((o, k) => {
      if (o.state !== "opening") return;
      const t = window.mStore.cards().find(c => key(c.id) === k);
      if (!t) { openingEnd(k); return; }
      if (t.supervised && actOf(t) !== baseline.get(k)) openingEnd(k);
    });
  }

  // ── the long press ───────────────────────────────────────────────────────
  function cancel() { clearTimeout(timer); timer = 0; pressRow = null; }

  function init() {
    const list = document.getElementById("m-list");
    if (!list) return;
    list.addEventListener("pointerdown", e => {
      if (e.pointerType === "mouse" && e.button !== 0) return;
      const row = e.target.closest(".row");
      if (!row) return;
      cancel();
      pressRow = row;
      startX = e.clientX; startY = e.clientY;
      timer = setTimeout(() => {
        const id = pressRow && pressRow.dataset.id;
        cancel();
        if (!id) return;
        // The lift that ends this press must not also open the card.
        swallow = Date.now() + 800;
        open(id);
      }, HOLD_MS);
    });
    list.addEventListener("pointermove", e => {
      if (timer && Math.hypot(e.clientX - startX, e.clientY - startY) > SLOP) cancel();
    });
    ["pointerup", "pointercancel", "pointerleave"].forEach(n => list.addEventListener(n, cancel));
    window.addEventListener("scroll", cancel, { passive: true, capture: true });
    list.addEventListener("contextmenu", e => {
      const row = e.target.closest(".row");
      if (!row) return;
      e.preventDefault();
      cancel();
      swallow = Date.now() + 800;
      open(row.dataset.id);
    });
    // Capture, so it runs before the list's own handler that opens the card.
    list.addEventListener("click", e => {
      if (Date.now() < swallow && e.target.closest(".row")) { e.stopImmediatePropagation(); e.preventDefault(); swallow = 0; }
    }, true);
    window.mStore.on("cards", check);
    window.addEventListener("resume-opening", () => { if (window.mHome) window.mHome.render(); });
  }

  window.mResume = { init, open, close, resume };
})();
