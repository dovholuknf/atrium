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
  const SNOOZES = [["15 min", 15], ["1 h", 60], ["tomorrow 9am", 0]];
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
    // A question is read whole, in a box that scrolls. Anything else keeps its one line.
    const asks = g.reason === "blocked" || g.reason === "question";
    const split = asks ? replies.split(g.body) : { text: g.body, choices: [] };
    const text = perm
      ? '<code class="gm-cmd">' + U.esc(first(g.body)) + "</code>"
      : asks ? '<div class="gm-body">' + bodyHTML(split.text) + "</div>"
        : '<div class="gm-line">' + U.esc(first(g.body)) + "</div>";
    const offered = asks ? replies.of({ ask: g.body, fixed: g.fixed }) : [];
    const choices = offered.length
      ? replies.buttons(offered, { cls: "gm-choices", narrow: true, disabled: choiceSent.get(g.id) === g.body }).outerHTML : "";
    const big = compose === g.id;
    let reply = "";
    if (asks) {
      reply = '<div class="gm-replybox' + (big ? " big" : "") + '"><textarea class="gm-reply" rows="1" placeholder="reply" aria-label="reply"></textarea>' +
        '<div class="gm-replyacts"><button data-do="reply">send</button><button data-do="compose" aria-label="' +
        (big ? "fold the reply box" : "open a bigger reply box") + '">' + (big ? "&#10514;" : "&#10530;") + "</button></div></div>";
    }
    let acts = "";
    if (perm) acts += '<button data-do="approve"' + dead + ">approve once</button>" +
      '<button data-do="block"' + dead + ">block</button>";
    else if (g.reason === "halt") acts += '<a class="btn" href="/">open the room</a>';
    else if (g.reason === "deploy-hold") {
      acts += '<button data-do="lift">' + (liftArm === g.id ? "tap again to lift" : "lift") + "</button>";
    }
    acts += '<button data-do="open">open</button><button data-do="snooze">remind me</button>' +
      '<button data-do="dismiss">dismiss this</button>';
    const snooze = snoozing === g.id
      ? '<div class="gm-opts">' + SNOOZES.map(s => '<button data-snooze="' + s[1] + '">' + s[0] + "</button>").join("") + "</div>" : "";
    const block = blocking === g.id
      ? '<div class="gm-opts"><input class="gm-why" placeholder="why not? handed back to the agent" aria-label="reason">' +
        '<button data-do="sendblock">send block</button><button data-do="cancelblock">cancel</button></div>' : "";
    const off = g.room_offline ? '<span class="gm-off">room offline</span>' : "";
    return '<div class="gm-full" data-id="' + U.esc(g.id) + '"><div class="gm-head"><b>' + U.esc(g.title) + "</b>" + off +
      "</div>" + text + choices + reply + '<div class="gm-acts">' + acts + "</div>" + snooze + block + "</div>";
  }

  // The body as paragraphs, lists and code blocks, every character escaped first. No inline markup and no links,
  // the same rule as the desktop growler: a question is model output and some of it echoes what a tool read.
  function bodyHTML(text) {
    const lines = String(text || "").replace(/\r/g, "").split("\n");
    const out = [];
    const num = /^\s*(\d+)[.)]\s+(.*)$/, dot = /^\s*[-*]\s+(.*)$/, fence = /^\s*```/;
    for (let i = 0; i < lines.length;) {
      const l = lines[i];
      if (fence.test(l)) {
        const code = [];
        for (i++; i < lines.length && !fence.test(lines[i]); i++) code.push(lines[i]);
        i++;
        out.push("<pre><code>" + U.esc(code.join("\n")) + "</code></pre>");
      } else if (!l.trim()) {
        i++;
      } else if (num.test(l) || dot.test(l)) {
        const ordered = num.test(l), re = ordered ? num : dot, items = [];
        const start = ordered ? Number(num.exec(l)[1]) : 0;
        for (; i < lines.length && re.test(lines[i]); i++) items.push("<li>" + U.esc(re.exec(lines[i])[ordered ? 2 : 1]) + "</li>");
        out.push(ordered ? '<ol start="' + start + '">' + items.join("") + "</ol>" : "<ul>" + items.join("") + "</ul>");
      } else {
        const para = [];
        for (; i < lines.length && lines[i].trim() && !fence.test(lines[i]) && !num.test(lines[i]) && !dot.test(lines[i]); i++) {
          para.push(U.esc(lines[i]));
        }
        out.push("<p>" + para.join("<br>") + "</p>");
      }
    }
    return out.join("");
  }

  // The growlers whose choice is in flight or answered, by the body they answered, so a redraw keeps the buttons
  // disabled and a new question on the same growler gets live ones.
  const choiceSent = new Map();

  // Which growler's reply box is opened to full size. The text is the draft either way.
  let compose = "";

  // A reply box that grows with what is typed, up to the cap in the stylesheet, then scrolls.
  function grow(box) {
    box.style.height = "auto";
    box.style.height = box.scrollHeight + "px";
  }

  // THE DRAWN NODES ARE KEPT WHEN THEIR MARKUP IS THE SAME. Every `growls` event asks for a draw, and replacing a node
  // drops its `:hover` and whatever is typed in it. Only a part that changed is swapped. Answers the nodes it made.
  const sigs = new WeakMap();
  function reconcile(parts) {
    const made = [];
    parts.forEach((html, i) => {
      const old = el.children[i];
      if (old && sigs.get(old) === html) return;
      const t = document.createElement("template");
      t.innerHTML = html;
      const n = t.content.firstElementChild;
      sigs.set(n, html);
      if (old) el.replaceChild(n, old);
      else el.appendChild(n);
      made.push(n);
    });
    while (el.children.length > parts.length) el.lastElementChild.remove();
    return made;
  }
  let caret = [0, 0];

  function render() {
    if (!el) return;
    const rows = drawn();
    const gone = undo && Date.now() < undo.until ? undo.g : null;
    if (!rows.length && !gone && !note) { el.hidden = true; el.innerHTML = ""; expanded = false; return; }
    el.hidden = false;
    const a = document.activeElement;
    const focused = a && el.contains(a) && a.classList.contains("gm-reply") ? a.closest(".gm-full").dataset.id : "";
    if (focused) caret = [a.selectionStart, a.selectionEnd];
    const parts = [];
    if (note) parts.push('<div class="gm-note">' + U.esc(note) + "</div>");
    if (gone) parts.push('<div class="gm-undo"><span>dismissed: ' + U.esc(gone.title) + '</span><button data-do="undo">undo</button></div>');
    if (rows.length) {
      if (expanded && rows.length > 1) {
        rows.forEach(g => parts.push(full(g)));
        parts.push('<button class="gm-strip" data-do="fold">fold</button>');
      } else {
        expanded = false;
        const rest = rows.slice(1);
        parts.push(full(rows[0]));
        if (rest.length) parts.push('<button class="gm-strip" data-do="expand">+' + rest.length + " more: " + counts(rest) + "</button>");
      }
    }
    reconcile(parts).forEach(n => {
      const fulls = n.classList.contains("gm-full") ? [n] : [];
      fulls.forEach(f => {
        const box = f.querySelector(".gm-reply");
        if (!box) return;
        box.value = drafts.get(f.dataset.id) || "";
        grow(box);
        if (focused === f.dataset.id) {
          box.focus();
          try { box.setSelectionRange(caret[0], caret[1]); } catch (e) {}
        }
      });
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

  // A reply typed, or a choice pressed. Both are the same message, through the operator message path.
  // A choice is one answer: every button of the question is disabled until the send answers, and lit again only if
  // it failed, so a double press cannot send twice.
  async function sendChoice(g, row, text) {
    if (choiceSent.get(g.id) === g.body) return;
    choiceSent.set(g.id, g.body);
    const btns = [...row.querySelectorAll(".gm-choices button")];
    btns.forEach(b => { b.disabled = true; });
    if (!await sendReply(g, text, null)) {
      choiceSent.delete(g.id);
      btns.forEach(b => { b.disabled = false; });
    }
  }

  async function sendReply(g, text, box) {
    const id = cardId(g);
    if (!text || !id) return false;
    try {
      await window.mNet.api("/v1/tasks/" + encodeURIComponent(id) + "/message", {
        method: "POST", body: JSON.stringify({ text, when: "done" }),
      });
      drafts.delete(g.id);
      if (box) { box.value = ""; grow(box); }
      say("sent");
      return true;
    } catch (e) { say("not sent: " + (e.message || e)); return false; }
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
        return sendReply(g, box ? box.value.trim() : "", box);
      }
      case "compose": {
        // The text is the draft either way, and the box is rebuilt from it at the other size.
        const box = row.querySelector(".gm-reply");
        if (box) drafts.set(g.id, box.value);
        compose = compose === g.id ? "" : g.id;
        render();
        const back = el.querySelector('.gm-full[data-id="' + CSS.escape(g.id) + '"] .gm-reply');
        if (back) { caret = [back.value.length, back.value.length]; back.focus(); back.setSelectionRange(caret[0], caret[1]); }
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
    if (btn.dataset.choice !== undefined && row) {
      const g = window.mStore.growls().find(x => x.id === row.dataset.id);
      if (g) sendChoice(g, row, btn.dataset.choice);
      return;
    }
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
      if (row && e.target.classList.contains("gm-reply")) { drafts.set(row.dataset.id, e.target.value); grow(e.target); }
    });
    // Enter sends and Shift+Enter is a newline. Escape folds a full-size box, keeping the text.
    el.addEventListener("keydown", e => {
      const row = e.target.closest(".gm-full");
      const g = row && window.mStore.growls().find(x => x.id === row.dataset.id);
      if (!g || e.isComposing) return;
      if (e.key === "Escape" && e.target.classList.contains("gm-reply") && compose === g.id) {
        e.preventDefault();
        compose = "";
        render();
        return;
      }
      if (e.key !== "Enter") return;
      if (e.target.classList.contains("gm-reply") && !e.shiftKey) { e.preventDefault(); act(g, "reply", row); }
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
