// The phone page's composer: one real textarea for one card, mounted into #m-compose.
//
// A REAL TEXTAREA, so autocorrect, dictation, swipe typing, paste, tap-to-cursor and the magnifier are the
// phone's own. Nothing here rewrites what the person types while they type it.
//
// SEND is the button, and Enter too where there is a hardware keyboard: Enter sends and Shift+Enter is a newline.
// On a touch-only device Enter inserts a newline, because a phone keyboard has no shift to say "this one is a
// newline". The message goes through `POST /v1/tasks/{id}/message`, the same call the board's say box makes
// (`sayNow` in js/settings-spine.js), and the answer is reported as the daemon gave it:
//
//   terminal            typed into the session's terminal already. Says "sent".
//   queued              the typing gate was shut (someone typing at the desk, a dialog open, a turn the runner
//                       will not take input in). Says "queued, delivered when the line is clear". Never "sent".
//   queued-unconfirmed  queued, and nothing is known to be able to drain it. Queued, plus the daemon's warning.
//   parked, undeliverable
//                       nothing was queued. The text stays in the box and the reason is shown.
//
// LINE BREAKS. The daemon types a message as one bracketed paste followed by Enter when the runner's
// harness has `bracketed_paste` (`typeLabelledGuarded` in internal/daemon/messages.go). Without it a raw
// newline is an Enter and submits the message early. So for a runner that cannot take a paste, or one whose
// harness this page could not read, the composer JOINS the lines with a space and says so under the box while
// the person is typing. It does not refuse: a dictated paragraph with a stray break should still go.
//
// A DRAFT per card lives in localStorage and is cleared only by a send the daemon accepted. Every storage
// access is wrapped, because a private window throws.
//
// ATTACHED FILES (u-028). `attach(files)` uploads a batch in ONE request, puts the returned paths in the box at the
// caret and shows one chip per file above it: a thumbnail for an image (an object URL of the local File, revoked
// when the chip goes), a glyph and the name for anything else. The chip says "uploading" while the request is in
// flight and shows the reason when it fails, and a failure inserts no path. The chip's X removes the chip and the
// exact span of text its path was inserted as, tracked through later edits of the text around it. A paste that
// carries an image and no text goes through the same `attach`. Upload computes its own destination server side
// and takes no path, so nothing here can aim it anywhere.
//
// No timers that fetch. The only requests are the send, one read of `/v1/harnesses`, made the first time a
// card's capability is needed, and an upload when a file is attached.
(function () {
  "use strict";

  const DRAFT = "atrium.m.draft.";
  const MAX_LINES = 6;
  // What a chip fills the box with. They fill, they never send.
  const QUICK = ["yes", "go ahead", "no", "stop"];
  // Statuses in which the card is waiting on the person, so quick replies make sense.
  const WAITING = ["needs-input", "needs-permission", "waiting"];

  let cur = null; // the one mounted composer
  let harnesses = null; // a Promise of { runner id: bracketed_paste }

  function guard(fn) { try { return fn(); } catch (e) { return null; } }
  function readDraft(id) { return guard(() => localStorage.getItem(DRAFT + id)) || ""; }
  function writeDraft(id, text) {
    guard(() => text ? localStorage.setItem(DRAFT + id, text) : localStorage.removeItem(DRAFT + id));
  }

  // Which runners take a bracketed paste, read once. A failed read answers an empty map, so every runner is
  // treated as one that cannot, which is the safe reading. It is retried on the next mount.
  function pasteMap() {
    if (!harnesses) {
      harnesses = fetch("/v1/harnesses")
        .then(r => r.ok ? r.json() : Promise.reject(new Error(r.status)))
        .then(j => {
          const m = {};
          for (const h of j.harnesses || []) m[h.id] = !!h.bracketed_paste;
          return m;
        })
        .catch(() => { harnesses = null; return {}; });
    }
    return harnesses;
  }

  function cardOf(id) {
    return guard(() => window.mStore && window.mStore.card(id)) || null;
  }

  // A card with something open for the person: a question, or a waiting status.
  function isAsking(card) {
    if (!card) return false;
    const q = card.questions || card.open_questions;
    if (Array.isArray(q) && q.length) return true;
    return WAITING.includes(card.status);
  }

  function el(tag, cls, text) {
    const n = document.createElement(tag);
    if (cls) n.className = cls;
    if (text != null) n.textContent = text;
    return n;
  }

  // The page follows the visual viewport. Where the layout viewport shrinks with the keyboard
  // (interactive-widget=resizes-content) the gap is zero and this does nothing. Where it does not (iOS), the
  // gap is what the keyboard covers, and the composer is lifted by it.
  function followKeyboard(root) {
    const vv = window.visualViewport;
    if (!vv) return () => {};
    let raf = 0;
    const apply = () => {
      raf = 0;
      const gap = Math.max(0, Math.round(window.innerHeight - vv.height - vv.offsetTop));
      root.style.setProperty("--mc-lift", gap + "px");
      root.classList.toggle("lifted", gap > 0);
    };
    const on = () => { if (!raf) raf = requestAnimationFrame(apply); };
    vv.addEventListener("resize", on);
    vv.addEventListener("scroll", on);
    apply();
    return () => {
      vv.removeEventListener("resize", on);
      vv.removeEventListener("scroll", on);
      if (raf) cancelAnimationFrame(raf);
    };
  }

  function grow(ta, maxLines) {
    ta.style.height = "auto";
    const cs = getComputedStyle(ta);
    const line = parseFloat(cs.lineHeight) || 22;
    const pad = parseFloat(cs.paddingTop) + parseFloat(cs.paddingBottom);
    const max = line * (maxLines || MAX_LINES) + pad;
    ta.style.height = Math.min(ta.scrollHeight, max) + "px";
    ta.style.overflowY = ta.scrollHeight > max ? "auto" : "hidden";
  }

  // What a daemon answer means, as { kind, text }. kind is sent, queued or refused.
  function outcome(res) {
    const d = res && res.delivered;
    const warn = res && res.warning ? " " + res.warning : "";
    if (d === "terminal") return { kind: "sent", text: "sent" };
    if (d === "parked" || d === "undeliverable") {
      return { kind: "refused", text: "not sent." + (warn || " The session cannot be reached.") };
    }
    // queued, queued-unconfirmed, or anything a newer daemon adds. Anything that is not a terminal write is
    // not "sent".
    return { kind: "queued", text: "queued, delivered when the line is clear." + warn };
  }

  // HOST AGNOSTIC. The composer does not know where its text goes, the host says.
  //
  //   opts.send(text)   how the text leaves, returning (or resolving to) { kind, text } as `outcome` does, or
  //                     throwing. Default: POST /v1/tasks/{id}/message, the /m page's way. The board's phone
  //                     terminal passes a function that writes into the attached terminal (js/tcompose.js).
  //   opts.canPaste()   whether the runner takes a bracketed paste. Default: read the harness list.
  //   opts.maxLines     where the box stops growing and scrolls. Default 6.
  //   opts.compact      a one line bar for a host with little room (the board's terminal).
  //   opts.follow       lift over the keyboard by the visual viewport. Default true. The board's phone layout
  //                     is already sized to the visual viewport, so it passes false.
  //   opts.noteMs       clear the note after this long, so it does not hold height. Default: kept.
  //   opts.upload(files) how a batch goes up, resolving to { paths: [...] } in file order. Default: POST
  //                     /v1/tasks/{id}/files as multipart. The board passes its own `api` call.
  //   opts.canUpload()  whether files may be attached at all (a guest link has no file endpoint). Default true.
  //
  // A SEND NEVER HAPPENS DURING AN IME COMPOSITION. Android keyboards compose a swiped word in the box and
  // fire compositionstart, update and end, then input. Send is the button, so what has to hold is that the
  // tap commits the word first (a blur ends the composition and the box holds the final text) and only then
  // reads the box. See `commitComposition`.
  function mount(host, cardId, opts) {
    unmount();
    opts = opts || {};
    const maxLines = opts.maxLines || MAX_LINES;
    const offs = [];
    const root = el("div", "mc" + (opts.compact ? " compact" : ""));
    root.dataset.card = cardId;

    const chips = el("div", "mc-chips");
    chips.setAttribute("role", "group");
    chips.setAttribute("aria-label", "quick replies");
    for (const q of QUICK) {
      const b = el("button", "mc-chip", q);
      b.type = "button";
      b.dataset.q = q;
      chips.appendChild(b);
    }

    const note = el("div", "mc-note");
    note.setAttribute("role", "status");
    note.setAttribute("aria-live", "polite");

    const files = el("div", "mc-files");
    files.setAttribute("role", "group");
    files.setAttribute("aria-label", "attached files");

    const row = el("div", "mc-row");
    const ta = el("textarea", "mc-box");
    ta.rows = 1;
    ta.placeholder = "Message this session";
    ta.setAttribute("aria-label", "message to this session");
    ta.setAttribute("enterkeyhint", "enter");
    ta.setAttribute("autocapitalize", "sentences");
    ta.setAttribute("autocomplete", "off");
    ta.setAttribute("spellcheck", "true");
    const send = el("button", "mc-send");
    send.type = "button";
    send.disabled = true;
    send.setAttribute("aria-label", "send");
    // An arrow, drawn, so there is no font dependence.
    send.innerHTML = '<svg viewBox="0 0 24 24" width="22" height="22" aria-hidden="true">' +
      '<path d="M12 19V5M6 11l6-6 6 6" fill="none" stroke="currentColor" stroke-width="2.4" ' +
      'stroke-linecap="round" stroke-linejoin="round"/></svg>';
    row.append(ta, send);
    root.append(files, chips, note, row);
    host.replaceChildren(root);

    const state = {
      el: root, host, id: cardId, offs, sending: false, pasteOK: null, ta, refresh: () => refresh(),
      atts: [], uploading: 0, last: "", opts, files
    };
    cur = state;

    const say = (kind, text) => {
      note.dataset.kind = kind || "";
      note.textContent = text || "";
      note.classList.toggle("on", !!text);
      clearTimeout(state.noteTimer);
      if (text && opts.noteMs && kind !== "hint" && kind !== "busy") {
        state.noteTimer = setTimeout(() => say("", ""), opts.noteMs);
      }
    };
    const refresh = () => {
      // Not while a file is still going up: the message would leave without its path.
      send.disabled = !ta.value.trim() || state.sending || state.uploading > 0;
      writeDraft(cardId, ta.value);
      grow(ta, maxLines);
    };
    const hint = () => {
      if (state.sending) return;
      // Said while typing, before it is a surprise.
      if (/\n/.test(ta.value.trim()) && state.pasteOK === false) {
        say("hint", "This runner takes one line, so your line breaks are joined with spaces.");
      } else if (note.dataset.kind === "hint") {
        say("", "");
      }
    };
    const showChips = () => {
      chips.classList.toggle("on", isAsking(cardOf(cardId)));
    };

    // The runner's paste capability, for the line-break note and for the send.
    const capability = async () => {
      if (opts.canPaste) { state.pasteOK = !!opts.canPaste(); return state.pasteOK; }
      const card = cardOf(cardId);
      const map = await pasteMap();
      state.pasteOK = !!(card && map[card.runner]);
      return state.pasteOK;
    };
    capability().then(hint);

    ta.addEventListener("input", () => { follow(state, ta.value); refresh(); hint(); });
    // Enter sends and Shift+Enter is a newline, the chat convention. Not while a word is being composed, where Enter
    // commits the word, and not on a device whose only pointer is a finger, whose keyboard cannot say Shift.
    const hardKeys = () => { try { return window.matchMedia("(any-pointer: fine)").matches; } catch (err) { return false; } };
    ta.addEventListener("keydown", e => {
      if (e.key !== "Enter" || e.shiftKey || e.isComposing || e.keyCode === 229 || state.composing) return;
      if (!hardKeys()) return;
      e.preventDefault();
      submit();
    });
    ta.addEventListener("paste", e => {
      if (opts.canUpload && !opts.canUpload()) return;
      const dt = e.clipboardData;
      if (!dt) return;
      const got = [];
      for (const item of dt.items || []) {
        if (item.kind !== "file") continue;
        const f = item.getAsFile();
        if (f) got.push(f);
      }
      if (!got.length) for (const f of dt.files || []) got.push(f);
      // A clipboard that also carries text is a text paste: a page's copied image comes with its markup, and
      // the person meant the words.
      if (!got.length || dt.getData("text/plain")) return;
      e.preventDefault();
      attach(got);
    });
    chips.addEventListener("click", e => {
      const b = e.target.closest(".mc-chip");
      if (!b) return;
      const have = ta.value.trim();
      ta.value = have ? have + " " + b.dataset.q : b.dataset.q;
      refresh();
      ta.focus();
      const n = ta.value.length;
      try { ta.setSelectionRange(n, n); } catch (err) {}
    });
    files.addEventListener("click", e => {
      const x = e.target.closest(".mc-fx");
      if (!x) return;
      const a = state.atts.find(t => t.el === x.parentNode);
      if (a) removeAtt(state, a);
    });
    ta.addEventListener("compositionstart", () => { state.composing = true; });
    ta.addEventListener("compositionend", () => { state.composing = false; });
    // Keeps the caret and the keyboard: the button never takes focus, so a tap on it neither opens nor closes
    // the soft keyboard.
    send.addEventListener("pointerdown", e => e.preventDefault());
    send.addEventListener("click", () => {
      commitComposition();
      submit();
    });
    // A tap while a word is still being composed ends the composition, so the box holds the final text.
    function commitComposition() {
      if (!state.composing && !ta.matches(":focus")) return;
      if (state.composing) {
        const v = ta.value, at = ta.selectionEnd;
        ta.blur();
        ta.focus({ preventScroll: true });
        if (ta.value !== v) ta.value = v;
        try { ta.setSelectionRange(at, at); } catch (err) {}
        state.composing = false;
      }
    }

    async function submit() {
      if (state.sending) return;
      let text = ta.value.trim();
      if (!text) return;
      state.sending = true;
      send.disabled = true;
      root.classList.add("sending");
      say("busy", "sending");
      let ok = false;
      try {
        // An unknown capability is read as "cannot", so a send never guesses a paste into a runner that
        // submits on a raw newline.
        const paste = state.pasteOK == null ? await capability() : state.pasteOK;
        const joined = !paste && /\n/.test(text);
        if (joined) text = text.split(/\s*\n\s*/).filter(Boolean).join(" ");
        let o;
        if (opts.send) {
          o = await opts.send(text);
        } else {
          const r = await fetch("/v1/tasks/" + encodeURIComponent(cardId) + "/message", {
            method: "POST", headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ text, when: "immediate" })
          });
          let body = null, raw = "";
          try { raw = await r.text(); body = JSON.parse(raw); } catch (e) {}
          if (!r.ok) throw new Error((body && body.error) || raw.trim() || r.statusText || "failed");
          o = outcome(body);
        }
        if (!o || !o.kind) o = { kind: "sent", text: "sent" };
        ok = o.kind !== "refused";
        say(o.kind, o.text + (ok && joined ? " (line breaks joined)" : ""));
        if (ok) {
          ta.value = "";
          writeDraft(cardId, "");
          clearAtts(state);
        }
      } catch (e) {
        // The text stays where it is.
        say("refused", "not sent: " + (e && e.message ? e.message : e));
      } finally {
        state.sending = false;
        root.classList.remove("sending");
        refresh();
        if (ok) ta.focus();
      }
    }

    if (window.mStore) offs.push(window.mStore.on("cards", showChips));
    if (opts.follow !== false) offs.push(followKeyboard(root));

    ta.value = readDraft(cardId);
    state.last = ta.value;
    refresh();
    showChips();
    if (ta.value && !opts.compact) say("hint", "Draft kept.");
  }

  // ---- attached files

  // Where one edit landed, as the old range [p, oldEnd) replaced by new text ending at newEnd: the common prefix
  // and suffix of the two strings, which is exact for a single edit.
  function editOf(a, b) {
    let p = 0;
    const max = Math.min(a.length, b.length);
    while (p < max && a.charCodeAt(p) === b.charCodeAt(p)) p++;
    let ea = a.length, eb = b.length;
    while (ea > p && eb > p && a.charCodeAt(ea - 1) === b.charCodeAt(eb - 1)) { ea--; eb--; }
    return { p, oldEnd: ea, newEnd: eb };
  }

  // The box changed from state.last to `now`: move every tracked span with it. A span the edit reached into is
  // no longer known by position and is found by its text when it is removed.
  function follow(state, now) {
    const old = state.last;
    state.last = now;
    if (old === now) return;
    const ed = editOf(old, now);
    const delta = ed.newEnd - ed.oldEnd;
    for (const a of state.atts) {
      if (a.start == null || a.broken) continue;
      if (a.end <= ed.p) continue;
      if (a.start >= ed.oldEnd) { a.start += delta; a.end += delta; continue; }
      a.broken = true;
    }
  }

  function revoke(a) {
    if (!a.url) return;
    try { URL.revokeObjectURL(a.url); } catch (e) {}
    a.url = "";
  }

  function clearAtts(state) {
    for (const a of state.atts) revoke(a);
    state.atts = [];
    state.files.replaceChildren();
    state.files.classList.remove("on");
  }

  function chipOf(a) {
    const c = el("div", "mc-file");
    let mark;
    if (a.url) {
      mark = el("img", "mc-thumb");
      mark.src = a.url;
      mark.alt = "";
    } else {
      mark = el("span", "mc-glyph", "📄");
      mark.setAttribute("aria-hidden", "true");
    }
    const name = el("span", "mc-fname", a.name);
    const st = el("span", "mc-fstate");
    const x = el("button", "mc-fx", "✕");
    x.type = "button";
    x.setAttribute("aria-label", "remove " + a.name);
    // Takes no focus, so the keyboard and the caret stay where they are.
    x.addEventListener("pointerdown", e => e.preventDefault());
    c.append(mark, name, st, x);
    a.el = c;
    a.stEl = st;
    paintChip(a);
    return c;
  }

  function paintChip(a) {
    const word = a.status === "up" ? "uploading" : a.status === "err" ? a.err : "";
    a.el.dataset.state = a.status;
    a.stEl.textContent = word;
    a.el.setAttribute("aria-label", a.name + (word ? ": " + word : ""));
  }

  // Puts `text` at the caret as `insert` does and returns where it landed, so a caller can name spans in it.
  function place(state, text) {
    const ta = state.ta;
    const v = ta.value;
    let a = ta.selectionStart, b = ta.selectionEnd;
    if (typeof a !== "number") { a = b = v.length; }
    const before = v.slice(0, a), after = v.slice(b);
    const lead = before && !/\s$/.test(before) ? " " : "";
    const tail = /^\s/.test(after) ? "" : " ";
    const put = lead + text.trim() + tail;
    ta.value = before + put + after;
    follow(state, ta.value);
    const at = before.length + put.length;
    state.refresh();
    ta.focus();
    try { ta.setSelectionRange(at, at); } catch (e) {}
    return before.length + lead.length;
  }

  // The chip goes and so does its path, and nothing else in the text.
  function removeAtt(state, a) {
    const i = state.atts.indexOf(a);
    if (i < 0) return;
    state.atts.splice(i, 1);
    revoke(a);
    a.el.remove();
    state.files.classList.toggle("on", state.atts.length > 0);
    if (!a.path) return;
    const ta = state.ta;
    const v = ta.value;
    let s = a.broken ? -1 : a.start;
    if (s < 0 || v.slice(s, s + a.path.length) !== a.path) s = v.indexOf(a.path);
    if (s < 0) return;
    const caret = ta.selectionStart;
    ta.value = v.slice(0, s) + v.slice(s + a.path.length);
    follow(state, ta.value);
    try {
      const c = caret > s ? Math.max(s, caret - a.path.length) : caret;
      ta.setSelectionRange(c, c);
    } catch (e) {}
    state.refresh();
  }

  async function defaultUpload(id, list) {
    const form = new FormData();
    for (const f of list) form.append("file", f, f.name);
    const headers = {};
    const room = guard(() => localStorage.getItem("atrium.room"));
    if (room) headers["X-Atrium-Room"] = room;
    const r = await fetch("/v1/tasks/" + encodeURIComponent(id) + "/files", { method: "POST", headers, body: form });
    let body = null, raw = "";
    try { raw = await r.text(); body = JSON.parse(raw); } catch (e) {}
    if (!r.ok) throw new Error((body && body.error) || raw.trim() || r.statusText || "failed");
    return body || {};
  }

  // Uploads a batch as one request and returns once every chip has settled, with the paths that went in.
  async function attach(list) {
    const state = cur;
    list = Array.from(list || []);
    if (!state || !list.length) return [];
    if (state.opts.canUpload && !state.opts.canUpload()) return [];
    const mine = list.map(f => ({
      file: f, name: f.name || "file", status: "up", path: "", err: "", start: null, end: null, broken: false,
      url: f.type && f.type.indexOf("image/") === 0 ? guard(() => URL.createObjectURL(f)) || "" : ""
    }));
    for (const a of mine) {
      state.atts.push(a);
      state.files.appendChild(chipOf(a));
    }
    state.files.classList.add("on");
    state.files.scrollLeft = state.files.scrollWidth;
    state.uploading++;
    state.refresh();
    let res = null, failed = "";
    try {
      res = await (state.opts.upload ? state.opts.upload(list) : defaultUpload(state.id, list));
    } catch (e) {
      failed = e && e.message ? e.message : String(e || "failed");
    }
    state.uploading--;
    if (cur !== state) { for (const a of mine) revoke(a); return []; }
    const paths = (res && res.paths) || [];
    const landed = [];
    mine.forEach((a, i) => {
      if (!state.atts.includes(a)) return; // its chip was removed while it was going up
      if (failed || !paths[i]) { a.status = "err"; a.err = failed || "no path came back"; }
      else { a.status = "ok"; a.path = String(paths[i]); landed.push(a); }
      paintChip(a);
    });
    if (landed.length) {
      let at = place(state, landed.map(a => a.path).join(" "));
      for (const a of landed) { a.start = at; a.end = at + a.path.length; at = a.end + 1; }
    } else {
      state.refresh();
    }
    return landed.map(a => a.path);
  }

  function unmount() {
    if (!cur) return;
    clearAtts(cur);
    cur.offs.forEach(f => { try { f(); } catch (e) {} });
    if (cur.host && cur.el.parentNode === cur.host) cur.host.replaceChildren();
    cur = null;
  }

  // Text put into the box at the caret, where the person left it, with the words either side kept. A space is
  // added before it when it would run into a word and after it, so the next thing typed is a new word. The box
  // is then focused, so the keyboard comes up. Returns false when nothing is mounted. Used for the paths of
  // uploaded files.
  function insert(text) {
    if (!cur || !text) return false;
    place(cur, text);
    return true;
  }

  window.mCompose = { mount, unmount, insert, attach };
})();
