// The file viewer: a file of the card read in the page instead of downloaded. Read only. See docs/changes/u-m-viewer.md.
//
// A full-height sheet over the thread, never a replacement for it, so the thread keeps its place. Opening writes a history
// entry, so Back closes the sheet and lands on the thread where it was. Everything comes through the card's files endpoint,
// whose bound is internal/safepath: outside the card is a 403, and that is said as "not in this card's folder".
//
// Text is set with textContent, markdown goes through the thread's safe renderer, and an image is typed by its extension.
// A type it cannot show is asked about first and downloaded only on a yes.
(function () {
  "use strict";

  const CAP = 1048576;          // a text preview stops here
  const IMG_CAP = 20971520;     // an image is read whole up to here
  const SNIFF = 4096;           // a text type with a NUL byte in this much is not text

  const EXT = {
    md: "markdown", markdown: "markdown",
    json: "json",
    png: "image", jpg: "image", jpeg: "image", gif: "image", webp: "image",
  };
  "txt log csv tsv ini conf env diff patch".split(" ").forEach(e => { EXT[e] = "text"; });
  "js mjs ts go py rb rs java c h cpp sh ps1 sql html css xml yaml yml toml proto svg".split(" ").forEach(e => { EXT[e] = "code"; });
  const NAMES = ["makefile", "dockerfile", "license", "readme"];
  const IMG_TYPE = { png: "image/png", jpg: "image/jpeg", jpeg: "image/jpeg", gif: "image/gif", webp: "image/webp" };

  let els = null;
  let cur = null;      // { card, path }
  let seq = 0;

  const base = p => String(p).replace(/\/+$/, "").split(/[\\/]/).pop();
  const extOf = p => { const m = /\.([A-Za-z0-9]+)$/.exec(base(p)); return m ? m[1].toLowerCase() : ""; };
  function kindOf(path) {
    const e = extOf(path);
    if (EXT[e]) return EXT[e];
    return !e && NAMES.indexOf(base(path).toLowerCase()) >= 0 ? "text" : "";
  }

  function headers(extra) {
    const h = Object.assign({}, extra || {});
    try { const room = localStorage.getItem("atrium.room"); if (room) h["X-Atrium-Room"] = room; } catch (e) {}
    return h;
  }
  const url = (card, path) => "/v1/tasks/" + encodeURIComponent(card) + "/files?path=" + encodeURIComponent(path);

  function size(n) {
    if (n == null) return "";
    if (n < 1024) return n + " B";
    if (n < 1048576) return (n / 1024).toFixed(n < 10240 ? 1 : 0) + " KB";
    return (n / 1048576).toFixed(n < 10485760 ? 1 : 0) + " MB";
  }

  // What the daemon said about the file without reading it. A refusal is carried, anything else that is not an answer
  // leaves the size unknown rather than failing the open.
  async function stat(card, path) {
    try {
      const r = await fetch(url(card, path), { method: "HEAD", headers: headers() });
      if (r.status === 403 || r.status === 404) return { status: r.status };
      if (!r.ok) return { status: 0, size: null };
      const n = Number(r.headers.get("Content-Length"));
      return { status: 200, size: isFinite(n) && r.headers.has("Content-Length") ? n : null };
    } catch (e) { return { status: 0, size: null }; }
  }

  // ── the sheet ────────────────────────────────────────────────────────────
  function el(tag, cls, text) {
    const e = document.createElement(tag);
    if (cls) e.className = cls;
    if (text != null) e.textContent = text;
    return e;
  }

  function show(node) {
    els.body.textContent = "";
    els.body.appendChild(node);
  }

  function say(text, cls) { show(el("p", "v-note" + (cls ? " " + cls : ""), text)); }

  function button(label, fn) {
    const b = el("button", "v-btn", label);
    b.type = "button";
    b.addEventListener("click", fn);
    return b;
  }

  async function download(card, path, btn) {
    try {
      const r = await fetch(url(card, path), { headers: headers() });
      if (!r.ok) throw new Error("HTTP " + r.status);
      window.mMd.saveBlob(await r.blob(), base(path));
    } catch (e) { if (btn) { btn.textContent = "not available"; btn.disabled = true; } }
  }

  // A type it will not show: asked about, and downloaded only on a yes.
  function ask(card, path, n) {
    const box = el("div", "v-ask");
    box.appendChild(el("p", "", "Can't preview " + base(path) + (n != null ? " (" + size(n) + ")" : "") + ". Download?"));
    const row = el("div", "v-row");
    const yes = button("Download", () => download(card, path, yes));
    row.append(yes, button("Cancel", close));
    box.appendChild(row);
    show(box);
  }

  function refusal(status) {
    say(status === 403 ? "not in this card's folder" : status === 404 ? "no such file" : "could not read that file", "v-err");
  }

  async function render(card, path) {
    const mine = ++seq;
    cur = { card, path };
    els.title.textContent = base(path);
    els.scroll.scrollTop = 0;
    say("loading");
    const st = await stat(card, path);
    if (mine !== seq) return;
    if (st.status === 403 || st.status === 404) { refusal(st.status); return; }
    const kind = kindOf(path);
    if (!kind) { ask(card, path, st.size); return; }
    if (kind === "image") {
      if (st.size != null && st.size > IMG_CAP) { ask(card, path, st.size); return; }
      try {
        const r = await fetch(url(card, path), { headers: headers() });
        if (mine !== seq) return;
        if (!r.ok) { refusal(r.status); return; }
        const blob = new Blob([await r.blob()], { type: IMG_TYPE[extOf(path)] });
        if (mine !== seq) return;
        const img = document.createElement("img");
        img.className = "v-img";
        img.alt = base(path);
        img.onerror = () => say("not available", "v-err");
        img.src = URL.createObjectURL(blob);
        show(img);
      } catch (e) { say("could not read that file", "v-err"); }
      return;
    }
    // text of some kind: the first megabyte
    let buf, total = st.size;
    try {
      const over = st.size == null || st.size > CAP;
      const r = await fetch(url(card, path), { headers: headers(over ? { Range: "bytes=0-" + (CAP - 1) } : {}) });
      if (mine !== seq) return;
      if (!r.ok) { refusal(r.status); return; }
      buf = new Uint8Array(await r.arrayBuffer());
      if (r.status === 206) {
        const m = /\/(\d+)$/.exec(r.headers.get("Content-Range") || "");
        if (m) total = Number(m[1]);
      }
      if (buf.length > CAP) { total = total == null ? buf.length : total; buf = buf.slice(0, CAP); }
    } catch (e) { say("could not read that file", "v-err"); return; }
    if (mine !== seq) return;
    if (buf.slice(0, SNIFF).indexOf(0) >= 0) { ask(card, path, total); return; }
    let text = new TextDecoder("utf-8").decode(buf);
    const cut = total != null && total > buf.length;
    const wrap = el("div", "v-doc");
    if (kind === "markdown") {
      const holder = el("div", "md");
      const t = window.mStore && window.mStore.card(card);
      holder.innerHTML = window.mMd.render(text, { id: card, worktree: (t && t.worktree) || "" });
      wrap.appendChild(holder);
    } else {
      if (kind === "json" && !cut) { try { text = JSON.stringify(JSON.parse(text), null, 2); } catch (e) {} }
      const pre = el("pre", "code v-text");
      pre.appendChild(el("code", "", text));
      wrap.appendChild(pre);
    }
    if (cut) {
      const note = el("div", "v-cut");
      note.appendChild(el("span", "", "showing the first " + size(buf.length) + " of " + size(total)));
      const more = button("download the rest", () => download(card, path, more));
      note.appendChild(more);
      wrap.appendChild(note);
    }
    show(wrap);
  }

  // ── opening and leaving ──────────────────────────────────────────────────
  function open(card, path) {
    if (!els || !card || !path) return;
    const was = !els.sheet.hidden;
    els.sheet.hidden = false;
    document.body.classList.add("viewer-open");
    // One entry per file. A reader going on to a file named in this one steps through them with Back.
    try { history.pushState({ mcard: card, mview: path }, "", location.href); } catch (e) {}
    if (!was) els.scroll.scrollTop = 0;
    render(card, path);
  }

  function hide() {
    seq++;
    cur = null;
    els.sheet.hidden = true;
    els.body.textContent = "";
    document.body.classList.remove("viewer-open");
  }

  // The chevron and Back are one thing: the history entry is stepped back and popstate does the closing.
  function close() {
    if (!els || els.sheet.hidden) return;
    if (history.state && history.state.mview) history.back();
    else hide();
  }

  function init() {
    els = { sheet: document.getElementById("m-viewer"), title: document.getElementById("m-viewer-title"),
      scroll: document.getElementById("m-viewer-scroll"), body: document.getElementById("m-viewer-body"),
      back: document.getElementById("m-viewer-back") };
    if (!els.sheet) { els = null; return; }
    els.back.addEventListener("click", close);
    window.addEventListener("popstate", e => {
      if (els.sheet.hidden) return;
      const v = e.state && e.state.mview;
      if (!v) hide();
      else if (!cur || cur.path !== v) render(e.state.mcard, v);
    });
    if (window.mCard && window.mCard.bindPinch) window.mCard.bindPinch(els.scroll);
  }

  window.mViewer = { init, open, close, reset: () => { if (els && !els.sheet.hidden) hide(); }, isOpen: () => !!els && !els.sheet.hidden, kindOf };
})();
