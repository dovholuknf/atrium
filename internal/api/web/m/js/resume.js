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
  // THE TERMINALS TAB MENU, the same items in the same order (docs/interviews/INTERVIEW-card-menu-2026-10-09.md, and
  // `termMenuItems` in js/terminal-list.js, which the phone page does not load): restart, resume, new context, add to
  // group, remove from group, then settings, then stop and terminate and clean up. Each has its (?), and with no hover a
  // tap on it shows the help under the row. No grey lines. Nothing asks "are you sure?" but a clean up of a folder holding
  // work that exists nowhere else, which has its own "never show me this again" tick, kept under the same key the board
  // uses (js/browser-dialogs.js, SKIP_KEY).
  const SKIP_KEY = "atrium.skipconfirm";
  const CLEANUP_KEY = "cleanup-unsaved";
  const skipped = () => { try { return JSON.parse(localStorage.getItem(SKIP_KEY) || "{}") || {}; } catch (e) { return {}; } };
  const setSkipped = on => {
    const all = skipped();
    if (on) all[CLEANUP_KEY] = true; else delete all[CLEANUP_KEY];
    try { localStorage.setItem(SKIP_KEY, JSON.stringify(all)); } catch (e) {}
  };
  const readKey = k => { try { return JSON.parse(localStorage.getItem(k) || "{}") || {}; } catch (e) { return {}; } };
  const patch = (id, body) => window.mNet.api("/v1/tasks/" + encodeURIComponent(id), { method: "PATCH", body: JSON.stringify(body) });
  const post = (id, tail, body) => window.mNet.api("/v1/tasks/" + encodeURIComponent(id) + "/" + tail,
    { method: "POST", body: JSON.stringify(body || {}) });
  const said = text => { if (!sheet) return; const w = sheet.querySelector(".rmenu-why"); w.textContent = text || ""; };

  // The named groups, when the terminals tab is grouping by them, as the board's `groupItem` reads them.
  function groupsOf(t) {
    const shared = readKey("atrium.grouping"), own = readKey("atrium.grouping.terms");
    const mode = own.mode !== undefined ? own.mode : shared.mode;
    const groups = mode === "custom" && Array.isArray(shared.groups) ? shared.groups : [];
    const mine = t.tags || [];
    return { custom: mode === "custom", all: groups, inside: groups.filter(g => mine.indexOf(g) >= 0), tags: mine,
      made: shared.made || {} };
  }

  // Makes a group, as the board's `createGroupFor` does, and files the card in it. A name already a group only files the card.
  function createGroup(id, g, raw) {
    const name = groupNameOf(raw);
    if (!name) return Promise.resolve();
    if (g.all.indexOf(name) < 0) {
      const shared = readKey("atrium.grouping");
      shared.groups = (Array.isArray(shared.groups) ? shared.groups : []).concat(name);
      shared.made = Object.assign({}, shared.made, { [name]: Date.now() });
      try { localStorage.setItem("atrium.grouping", JSON.stringify(shared)); } catch (e) {}
    }
    return g.tags.indexOf(name) >= 0 ? Promise.resolve() : patch(id, { tags: g.tags.concat(name) });
  }
  const tagOf = (g, name) => groupIsNew(name, g.made) ? "new" : "";

  function items(id, t) {
    const over = ["done", "dead", "paused"].indexOf(t.status) >= 0;
    const stopped = t.status === "paused";
    const g = groupsOf(t);
    const why = cannotResumeCard(t);
    const done = () => window.mNet.refresh();
    return [
      !over && cardState(t) !== "off" && (t.supervised || (t.parked_at && t.resume_id)) ? {
        label: "restart",
        help: "Stops the agent and starts it again on the same conversation, with the same model and settings.",
        act: () => post(id, t.supervised ? "restart" : "resume").then(done)
      } : null,
      stopped || (!t.supervised && t.worktree && t.resume_id) ? {
        label: "resume", off: why && !stopped ? why : "",
        help: "Starts the agent again on this card and picks the conversation back up where it stopped.",
        act: () => stopped ? patch(id, { status: "running" }).then(done) : resume(id)
      } : null,
      t.supervised && !over && !(t.new_context && t.new_context.step !== "failed") ? {
        label: "new context",
        help: "Has the agent commit, write a handoff note, clear its context, then carry on from the note.",
        act: () => post(id, "new-context").then(done)
      } : null,
      g.custom ? {
        label: "add to group",
        help: "Files this card into one of your named groups.",
        pick: groupMenuNames(g.all).map(name => ({ label: name, tag: tagOf(g, name), on: g.tags.indexOf(name) >= 0,
          act: () => patch(id, { tags: g.tags.indexOf(name) >= 0 ? g.tags.filter(x => x !== name) : g.tags.concat(name) }).then(done) }))
          .concat({ label: "create group...", create: true, act: name => createGroup(id, g, name).then(done) })
      } : null,
      g.inside.length ? {
        label: "remove from group",
        help: "Takes the card out of its group. Nothing else about it changes.",
        pick: g.inside.length > 1 ? groupMenuNames(g.inside).map(name => ({ label: name, tag: tagOf(g, name), act: () => patch(id, { tags: g.tags.filter(x => x !== name) }).then(done) })) : null,
        act: () => patch(id, { tags: g.tags.filter(x => x !== g.inside[0]) }).then(done)
      } : null,
      { sep: true },
      { label: "settings",
        help: "Opens this card's settings. The context limit lives there.",
        act: () => { if (window.mCard) window.mCard.open(id); } },
      { sep: true },
      t.supervised && !over ? {
        label: "stop",
        help: "Stops the agent so it can be resumed later. The card is left out of the list unless the paused filter is on.",
        act: () => patch(id, { status: "paused" }).then(done)
      } : null,
      {
        label: "terminate and clean up", danger: true,
        help: "Stops the agent and removes the folder and branch it was working in, when atrium created them. A folder it did not create stays, and it says so first. It asks about changes that exist nowhere else.",
        act: () => cleanUp(id, t),
        both: true,
        pick: [{ label: "and don't clean up",
          help: "Stops the session and ends the card. The folder, the branch and any hub branch are left untouched.",
          act: () => endKeeping(id) }]
      }
    ].filter(Boolean).filter((it, i, all) => !it.sep || (i > 0 && i < all.length - 1 && !all[i - 1].sep));
  }

  // Terminate and clean up, with the one question. `asks` are folders holding work that exists nowhere else.
  async function cleanUp(id, t) {
    let pv;
    try { pv = await window.mNet.api("/v1/tasks/" + encodeURIComponent(id) + "/close"); }
    catch (e) { openSheet(id, "could not read what cleaning up frees: " + e.message); return; }
    const answers = {};
    const asks = pv.asks || [];
    if (!(pv.items || []).length && !asks.length) { showForeign(id, t); return; }
    if (asks.length && !skipped()[CLEANUP_KEY]) { showAsk(id, t, pv); return; }
    asks.forEach(a => { answers[String(a.seq)] = "delete"; });
    finishCleanUp(id, answers);
  }

  async function endKeeping(id) {
    let out;
    try { out = await post(id, "close", { confirm: true, keep: true }); }
    catch (e) { openSheet(id, "could not terminate: " + e.message); return; }
    if (out && out.session_error) { openSheet(id, "could not terminate: " + out.session_error); return; }
    window.mNet.refresh();
  }

  async function finishCleanUp(id, answers) {
    let out;
    try { out = await post(id, "close", { confirm: true, answers }); }
    catch (e) { openSheet(id, "could not clean up: " + e.message); return; }
    if (out && out.session_error) { openSheet(id, "could not terminate: " + out.session_error); return; }
    close();
    window.mNet.refresh();
  }

  // A preview with no items frees nothing: atrium removes only what it created, and this folder is not. Say so first.
  function showForeign(id, t) {
    const box = sheet.querySelector(".rmenu-box");
    box.innerHTML = '<p class="rmenu-name"></p><p class="rmenu-ask"></p>' +
      '<button type="button" class="rmenu-go rmenu-danger" data-ans="go">terminate</button>' +
      '<button type="button" class="rmenu-x" data-close="1">cancel</button>';
    box.querySelector(".rmenu-name").textContent = window.mUtil.cardName(t).main;
    box.querySelector(".rmenu-ask").textContent = "Nothing here was created by atrium, so nothing is removed." +
      (t.worktree ? " " + t.worktree + (t.branch ? " and branch " + t.branch : "") + " stay." : "") + " The session is stopped.";
    box.querySelector("[data-ans]").onclick = () => finishCleanUp(id, {});
    reveal(id);
  }

  // The menu closed itself when the item was tapped, so the question has to bring the sheet back up.
  function reveal(id) {
    openFor = id;
    sheet.hidden = false;
    document.body.classList.add("sheet-open");
  }

  function showAsk(id, t, pv) {
    const box = sheet.querySelector(".rmenu-box");
    const lines = (pv.asks || []).map(a => `<li><b></b> on <i></i>: ${a.dirty} changed files, ${a.unpushed} commits on no remote and not on the hub</li>`).join("");
    box.innerHTML = '<p class="rmenu-name"></p><p class="rmenu-ask">This folder holds work that exists nowhere else:</p><ul class="rmenu-asks">' + lines + '</ul>' +
      '<label class="rmenu-tick"><input type="checkbox" class="rmenu-never"> never show me this again, just delete</label>' +
      (pv.can_stash ? '<button type="button" class="rmenu-go" data-ans="stash">stash to the hub</button>' : "") +
      '<button type="button" class="rmenu-go rmenu-danger" data-ans="delete">delete it</button>' +
      '<button type="button" class="rmenu-x" data-close="1">cancel</button>';
    box.querySelector(".rmenu-name").textContent = window.mUtil.cardName(t).main;
    box.querySelectorAll(".rmenu-asks li").forEach((li, i) => {
      li.querySelector("b").textContent = pv.asks[i].path;
      li.querySelector("i").textContent = pv.asks[i].branch || "no branch";
    });
    box.querySelectorAll("[data-ans]").forEach(b => b.onclick = () => {
      const ans = b.dataset.ans;
      // Only a delete leaves the tick standing, so ticking it and stashing is not a standing delete.
      if (ans === "delete" && box.querySelector(".rmenu-never").checked) setSkipped(true);
      const answers = {};
      pv.asks.forEach(a => { answers[String(a.seq)] = ans; });
      finishCleanUp(id, answers);
    });
    reveal(id);
  }

  function build() {
    if (sheet) return;
    sheet = document.createElement("div");
    sheet.id = "m-rmenu";
    sheet.className = "rmenu";
    sheet.hidden = true;
    document.body.appendChild(sheet);
    sheet.addEventListener("click", e => {
      if (e.target.closest("[data-close]")) { close(); return; }
      // The (?) explains and does nothing else.
      const q = e.target.closest(".rmenu-q");
      if (q) {
        const row = q.closest(".rmenu-row");
        const help = row.nextElementSibling;
        if (help && help.classList.contains("rmenu-help")) help.hidden = !help.hidden;
        return;
      }
      const pick = e.target.closest("[data-pick]");
      if (pick) {
        const it = current[Number(pick.dataset.i)].pick[Number(pick.dataset.j)];
        if (it.create) { askName(pick, it); return; }
        close(); it.act(); return;
      }
      if (e.target.closest(".rmenu-newgroup")) return;
      const b = e.target.closest("[data-i]");
      if (!b || b.disabled) return;
      const it = current[Number(b.dataset.i)];
      if (it.pick && !(it.both && !e.target.closest(".rmenu-more"))) {
        const list = b.closest(".rmenu-row").parentNode.querySelector('.rmenu-picks[data-i="' + b.dataset.i + '"]');
        if (list) list.hidden = !list.hidden;
        return;
      }
      close();
      Promise.resolve(it.act()).catch(err => openSheet(openFor, String(err && err.message || err)));
    });
  }

  let current = [];

  // "create group..." turns into a name field in place. Enter makes the group and files the card, Esc or "cancel" puts the entry back.
  function askName(btn, it) {
    const form = document.createElement("form");
    form.className = "rmenu-newgroup";
    form.innerHTML = '<input type="text" class="rmenu-name-in" aria-label="group name" placeholder="group name" autocapitalize="none" autocomplete="off">' +
      '<button type="submit" class="rmenu-item">create</button><button type="button" class="rmenu-item rmenu-nocreate">cancel</button>';
    const input = form.querySelector("input");
    const back = () => { form.replaceWith(btn); };
    form.addEventListener("submit", e => {
      e.preventDefault();
      const name = input.value;
      if (!groupNameOf(name)) { input.focus(); return; }
      close(); Promise.resolve(it.act(name)).catch(err => openSheet(openFor, String(err && err.message || err)));
    });
    form.querySelector(".rmenu-nocreate").addEventListener("click", back);
    input.addEventListener("keydown", e => { if (e.key === "Escape") { e.preventDefault(); e.stopPropagation(); back(); } });
    btn.replaceWith(form);
    input.focus();
  }

  function open(id) { openSheet(id, ""); }

  function openSheet(id, note) {
    const t = window.mStore.card(id);
    if (!t) return;
    build();
    openFor = id;
    current = items(id, t);
    const o = openingState(id);
    const esc = s => String(s).replace(/[&<>"]/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c]));
    const rows = current.map((it, i) => {
      if (it.sep) return '<div class="rmenu-sep"></div>';
      const label = it.label === "resume" && o && o.state === "opening" ? openingText + "…" : it.label;
      return '<div class="rmenu-row"><button type="button" class="rmenu-item' + (it.label === "resume" ? " rmenu-resume" : "") + (it.danger ? " danger" : "") + '" data-i="' + i + '"' +
        (it.off ? " disabled" : "") + ">" + esc(label) + (it.pick ? ' <span class="rmenu-more">&#9656;</span>' : "") +
        '</button><button type="button" class="rmenu-q" aria-label="what is ' + esc(it.label) + '?">?</button></div>' +
        '<p class="rmenu-help" hidden>' + esc(it.help || "") + (it.off ? " It cannot be resumed: " + esc(it.off) + "." : "") + "</p>" +
        (it.pick ? '<div class="rmenu-picks" data-i="' + i + '" hidden>' + it.pick.map((p, j) =>
          '<button type="button" class="rmenu-item sub' + (p.on ? " on" : "") + '" data-pick="1" data-i="' + i + '" data-j="' + j + '">' + esc(p.label) + (p.tag ? ' <span class="newtag">' + esc(p.tag) + "</span>" : "") + "</button>" + (p.help ? '<p class="rmenu-help">' + esc(p.help) + "</p>" : "")).join("") + "</div>" : "");
    }).join("");
    sheet.innerHTML = '<div class="rmenu-back" data-close="1"></div>' +
      '<div class="rmenu-box" role="dialog" aria-label="card actions"><p class="rmenu-name"></p>' + rows +
      '<p class="rmenu-why" role="status"></p><button type="button" class="rmenu-x" data-close="1">cancel</button></div>';
    sheet.querySelector(".rmenu-name").textContent = window.mUtil.cardName(t).main;
    said(note);
    sheet.hidden = false;
    document.body.classList.add("sheet-open");
    if (!note && navigator.vibrate) { try { navigator.vibrate(12); } catch (e) {} }
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
