// The growlers on the phone home: something waiting on the operator that stays until it is handled, dismissed or its
// reason ends. The hub holds the state and says the WHOLE set on every `growls` event, which `mStore` keeps. Nothing
// is fetched here. One growler is drawn in full and the rest are a count under it, which opens the stack in place.
//
// Copied in rather than shared with js/growl.js, like the rest of this page, so the board can change without touching
// the phone. Every call says `via: "phone"`. The board-only parts (title, favicon, desktop notification, sound) are
// not here: a phone has a bell and a push, and the hub sends that once per growler.
(function () {
  "use strict";

  const U = window.mUtil;
  const TAB = "m" + Math.random().toString(36).slice(2, 10);
  const NAME = {
    permission: ["permission", "permissions"], halt: ["halt", "halts"], blocked: ["blocked", "blocked"],
    question: ["question", "questions"], "deploy-hold": ["deploy-hold", "deploy-holds"],
  };
  const SNOOZES = [["15 min", 15], ["1 h", 60], ["until tomorrow 09:00", 0]];
  const UNDO_MS = 10000;

  let el = null;
  let expanded = false;
  let snoozing = "", blocking = "", liftArm = "";
  let note = "", noteTimer = 0;
  let undo = null;
  const drafts = new Map();

  function drawn() { return window.mStore.growls().filter(g => g.state === "open"); }
  function first(s) { return String(s || "").split("\n")[0]; }

  function say(text) {
    note = text;
    clearTimeout(noteTimer);
    noteTimer = setTimeout(() => { note = ""; render(); }, 4000);
    render();
  }

  // The card in the form the list uses, since one room attached is bare and two is `room~id`.
  function cardId(g) {
    const c = window.mStore.card(g.card || "") || window.mStore.card(g.card_id || "");
    return c ? c.id : (g.card || g.card_id || "");
  }

  function counts(rest) {
    const n = {};
    rest.forEach(g => { n[g.reason] = (n[g.reason] || 0) + 1; });
    return Object.keys(NAME).filter(k => n[k]).map(k => n[k] + " " + NAME[k][n[k] === 1 ? 0 : 1]).join(", ");
  }

  function full(g) {
    const perm = g.reason === "permission";
    const dead = perm && !g.subject ? " disabled" : "";
    const text = perm
      ? '<code class="gm-cmd">' + U.esc(first(g.body)) + "</code>"
      : '<div class="gm-line">' + U.esc(first(g.body)) + "</div>";
    let acts = "";
    if (perm) acts += '<button data-do="approve"' + dead + ">approve once</button>" +
      '<button data-do="block"' + dead + ">block</button>";
    else if (g.reason === "halt") acts += '<a class="btn" href="/">open the room</a>';
    else if (g.reason === "deploy-hold") {
      acts += '<button data-do="lift">' + (liftArm === g.id ? "tap again to lift" : "lift") + "</button>";
    }
    if (g.reason === "blocked" || g.reason === "question") {
      acts += '<input class="gm-reply" placeholder="reply" aria-label="reply">' + '<button data-do="reply">send</button>';
    }
    acts += '<button data-do="open">open</button><button data-do="snooze">snooze</button>' +
      '<button data-do="dismiss">dismiss this</button>';
    const snooze = snoozing === g.id
      ? '<div class="gm-opts">' + SNOOZES.map(s => '<button data-snooze="' + s[1] + '">' + s[0] + "</button>").join("") + "</div>" : "";
    const block = blocking === g.id
      ? '<div class="gm-opts"><input class="gm-why" placeholder="why not? handed back to the agent" aria-label="reason">' +
        '<button data-do="sendblock">send block</button><button data-do="cancelblock">cancel</button></div>' : "";
    const off = g.room_offline ? '<span class="gm-off">room offline</span>' : "";
    return '<div class="gm-full" data-id="' + U.esc(g.id) + '"><div class="gm-head"><b>' + U.esc(g.title) + "</b>" + off +
      "</div>" + text + '<div class="gm-acts">' + acts + "</div>" + snooze + block + "</div>";
  }

  function render() {
    if (!el) return;
    const rows = drawn();
    const gone = undo && Date.now() < undo.until ? undo.g : null;
    if (!rows.length && !gone && !note) { el.hidden = true; el.innerHTML = ""; expanded = false; return; }
    el.hidden = false;
    const a = document.activeElement;
    const focused = a && el.contains(a) && a.classList.contains("gm-reply") ? a.closest(".gm-full").dataset.id : "";
    let html = note ? '<div class="gm-note">' + U.esc(note) + "</div>" : "";
    if (gone) html += '<div class="gm-undo"><span>dismissed: ' + U.esc(gone.title) + '</span><button data-do="undo">undo</button></div>';
    if (rows.length) {
      if (expanded && rows.length > 1) {
        html += '<div class="gm-list">' + rows.map(full).join("") + "</div>" +
          '<button class="gm-strip" data-do="fold">fold</button>';
      } else {
        expanded = false;
        const rest = rows.slice(1);
        html += full(rows[0]) + (rest.length
          ? '<button class="gm-strip" data-do="expand">+' + rest.length + " more: " + counts(rest) + "</button>" : "");
      }
    }
    el.innerHTML = html;
    el.querySelectorAll(".gm-full").forEach(f => {
      const box = f.querySelector(".gm-reply");
      if (box) {
        box.value = drafts.get(f.dataset.id) || "";
        if (focused === f.dataset.id) box.focus();
      }
    });
  }

  // One call for dismiss, snooze and undismiss. A 409 means its reason ended first.
  async function post(g, body) {
    let res, d = {};
    try {
      res = await fetch("/_hub/growls/" + encodeURIComponent(g.id), {
        method: "POST", headers: { "Content-Type": "application/json" },
        body: JSON.stringify(Object.assign({ via: "phone", tab: TAB }, body)),
      });
      try { d = await res.json(); } catch (e) {}
    } catch (e) {
      say("could not reach atrium");
      return null;
    }
    if (res.status === 409) {
      window.mStore.dropGrowl(g.id);
      say("already handled");
      return null;
    }
    if (!res.ok) { say(d.error || "atrium answered " + res.status); return null; }
    if (d.growl) window.mStore.setGrowl(d.growl);
    return d.growl || null;
  }

  async function decide(g, decision, reason) {
    if (!g.subject) return;
    try {
      const headers = {};
      if (g.room && window.mNet.hub()) headers["X-Atrium-Room"] = g.room;
      await window.mNet.api("/v1/permissions/" + encodeURIComponent(g.subject) + "/decide", {
        method: "POST", headers,
        body: JSON.stringify({ decision, reason, forever: false, prefix: "", kind: "command", command: "" }),
      });
    } catch (e) {
      say("too late: " + (e.message || e));
    }
  }

  async function act(g, what, row) {
    switch (what) {
      case "approve": return decide(g, "approve", "");
      case "block": blocking = blocking === g.id ? "" : g.id; render(); return;
      case "cancelblock": blocking = ""; render(); return;
      case "sendblock": {
        const box = row.querySelector(".gm-why");
        blocking = "";
        render();
        return decide(g, "block", box ? box.value.trim() : "");
      }
      case "open": if (window.mCard) window.mCard.open(cardId(g)); return;
      case "snooze": snoozing = snoozing === g.id ? "" : g.id; render(); return;
      case "lift": {
        // Asks first, as the board does, with a second tap rather than a dialog.
        if (liftArm !== g.id) { liftArm = g.id; render(); setTimeout(() => { if (liftArm === g.id) { liftArm = ""; render(); } }, 4000); return; }
        liftArm = "";
        try {
          const headers = {};
          if (g.room && window.mNet.hub()) headers["X-Atrium-Room"] = g.room;
          await window.mNet.api("/v1/hold", {
            method: "POST", headers, body: JSON.stringify({ action: "lift", outcome: "operator", by: "operator" }),
          });
        } catch (e) { say("could not lift the hold: " + (e.message || e)); }
        render();
        return;
      }
      case "reply": {
        const box = row.querySelector(".gm-reply");
        const text = box ? box.value.trim() : "";
        const id = cardId(g);
        if (!text || !id) return;
        try {
          await window.mNet.api("/v1/tasks/" + encodeURIComponent(id) + "/message", {
            method: "POST", body: JSON.stringify({ text, when: "done" }),
          });
          drafts.delete(g.id);
          say("sent");
        } catch (e) { say("not sent: " + (e.message || e)); }
        return;
      }
      case "dismiss": {
        if (!await post(g, { do: "dismiss" })) return;
        const mine = { g, until: Date.now() + UNDO_MS };
        undo = mine;
        render();
        setTimeout(() => { if (undo === mine) { undo = null; render(); } }, UNDO_MS);
        return;
      }
    }
  }

  async function onClick(e) {
    const btn = e.target.closest("button");
    if (!btn) return;
    const what = btn.dataset.do;
    const row = btn.closest(".gm-full");
    if (btn.dataset.snooze !== undefined && row) {
      const g = window.mStore.growls().find(x => x.id === row.dataset.id);
      if (!g) return;
      let minutes = Number(btn.dataset.snooze);
      if (!minutes) {
        const t = new Date();
        t.setDate(t.getDate() + 1);
        t.setHours(9, 0, 0, 0);
        minutes = Math.ceil((t - Date.now()) / 60000);
      }
      snoozing = "";
      post(g, { do: "snooze", minutes: Math.min(10080, Math.max(1, minutes)) });
      return;
    }
    if (what === "expand" || what === "fold") { expanded = what === "expand"; snoozing = ""; render(); return; }
    if (what === "undo") {
      const u = undo;
      undo = null;
      render();
      if (u) {
        const back = await post(u.g, { do: "undismiss" });
        if (back) render();
      }
      return;
    }
    if (!row) return;
    const g = window.mStore.growls().find(x => x.id === row.dataset.id);
    if (g) act(g, what, row);
  }

  function init() {
    el = document.getElementById("m-growl");
    if (!el) return;
    el.addEventListener("click", onClick);
    el.addEventListener("input", e => {
      const row = e.target.closest(".gm-full");
      if (row && e.target.classList.contains("gm-reply")) drafts.set(row.dataset.id, e.target.value);
    });
    el.addEventListener("keydown", e => {
      if (e.key !== "Enter") return;
      const row = e.target.closest(".gm-full");
      const g = row && window.mStore.growls().find(x => x.id === row.dataset.id);
      if (!g) return;
      if (e.target.classList.contains("gm-reply")) act(g, "reply", row);
      if (e.target.classList.contains("gm-why")) act(g, "sendblock", row);
    });
    window.mStore.on("growls", () => {
      // One that came back by another route takes the undo line down.
      if (undo && drawn().some(g => g.id === undo.g.id)) undo = null;
      render();
    });
    render();
  }

  window.mGrowl = { init, render };
})();
