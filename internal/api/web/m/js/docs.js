// Hub documents, the views (D2). See docs/changes/u-m-docs.md and the contract docs/backlog/fabric/hub-documents-api.md.
//
// One module for the phone's /m shell and the desktop board: a list with a title filter, upload, a document with its
// versions, history and a line diff, and the "published N documents" line of a card. It talks only to `/_hub/docs` with a
// plain same-origin fetch: no Origin, no header of ours, never no-cors.
//
// EVERYTHING FROM THE HUB IS TEXT. Titles, names, handles and error sentences are set with textContent and never as
// markup. Markdown goes through the thread's safe renderer (md.js), raster images are drawn from a blob typed by their
// own first bytes, and anything else is a download. HTML and SVG are never rendered.
(function () {
  "use strict";

  const CAP = 1048576;            // text is shown up to here, the rest is a download
  const TEXT_CAP = 5242880;       // a side of a compare is read up to the hub's text cap
  const IMG_CAP = 20971520;
  const MAX_D = 2000;             // a compare is refused past this many changed lines
  const DOC_RE = /^\/d\/[a-z0-9-]{1,60}(@[1-9][0-9]*)?$/;
  const TEXT_KINDS = ["markdown", "text", "diff"];
  const CARD_TTL = 30000;
  const SAME = "this compare is too big to show here";

  const U = () => window.mUtil || { ago: () => "", ts: x => Date.parse(x) || 0 };
  const isPhone = () => !!document.getElementById("m-card");
  let els = null;
  let avail = null;               // null until asked, then true or false
  let settings = null;
  let seq = 0;
  let urls = [];
  let cur = null;                 // what the sheet shows: { v: "list"|"doc", ... }
  const hooks = [];

  function el(tag, cls, text) {
    const e = document.createElement(tag);
    if (cls) e.className = cls;
    if (text != null) e.textContent = text;
    return e;
  }
  function btn(label, fn, cls) {
    const b = el("button", "d-btn" + (cls ? " " + cls : ""), label);
    b.type = "button";
    if (fn) b.addEventListener("click", fn);
    return b;
  }
  function dropUrls() { urls.forEach(u => URL.revokeObjectURL(u)); urls = []; }

  // ── talking to the hub ───────────────────────────────────────────────────
  // The hub's own sentence for any refusal, with the rule named on a 422. No list of statuses: whatever it says is shown.
  function sentence(status, body) {
    let s = body && typeof body.error === "string" && body.error ? body.error : "the hub answered " + status;
    if (body && body.rule) s += " (" + body.rule + ")";
    return s;
  }
  async function hub(path, opts) {
    const r = await fetch(path, opts || {});
    let body = null;
    try { body = await r.clone().json(); } catch (e) {}
    return { r, status: r.status, ok: r.ok, body };
  }
  async function readCapped(r, cap) {
    if (!r.body || !r.body.getReader) {
      const all = new Uint8Array(await r.arrayBuffer());
      return { bytes: all.length > cap ? all.slice(0, cap) : all, over: all.length > cap };
    }
    const reader = r.body.getReader();
    const parts = [];
    let n = 0, over = false;
    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;
      parts.push(value);
      n += value.length;
      if (n > cap) { over = true; try { await reader.cancel(); } catch (e) {} break; }
    }
    const out = new Uint8Array(Math.min(n, cap));
    let at = 0;
    for (const c of parts) {
      if (at >= out.length) break;
      const part = at + c.length > out.length ? c.subarray(0, out.length - at) : c;
      out.set(part, at);
      at += part.length;
    }
    return { bytes: out, over };
  }
  // A cut can fall inside a character, which decodes to U+FFFD at the end. That is dropped only when something WAS cut: a
  // whole file that really ends in U+FFFD keeps it.
  const decode = (bytes, cut) => { const t = new TextDecoder("utf-8", { fatal: false }).decode(bytes); return cut ? t.replace(/\uFFFD+$/, "") : t; };

  function upload(url, file, fields, onprog) {
    return new Promise(resolve => {
      const fd = new FormData();
      fd.append("file", file, file.name);
      Object.keys(fields || {}).forEach(k => { if (fields[k] != null && fields[k] !== "") fd.append(k, fields[k]); });
      const x = new XMLHttpRequest();
      x.open("POST", url);
      if (x.upload && onprog) x.upload.onprogress = e => { if (e.lengthComputable) onprog(Math.round(e.loaded * 100 / e.total)); };
      x.onload = () => { let b = null; try { b = JSON.parse(x.responseText); } catch (e) {} resolve({ status: x.status, ok: x.status >= 200 && x.status < 300, body: b }); };
      x.onerror = () => resolve({ status: 0, ok: false, body: { error: "could not reach the hub" } });
      x.send(fd);
    });
  }

  // ── is there a hub with documents here ───────────────────────────────────
  // A room's own board has no /_hub/docs. Any failure at all hides every door, with no toast.
  function ready() {
    if (avail !== null) return Promise.resolve(avail);
    if (ready.p) return ready.p;
    ready.p = (async () => {
      try {
        const g = await hub("/_hub/docs/settings");
        avail = !!(g.ok && g.body && typeof g.body === "object" && !g.body.error);
        if (avail) settings = g.body;
      } catch (e) { avail = false; }
      hooks.forEach(f => { try { f(avail); } catch (e) {} });
      return avail;
    })();
    return ready.p;
  }

  // ── words ────────────────────────────────────────────────────────────────
  const size = n => n == null ? "" : n < 1024 ? n + " B" : n < 1048576 ? (n / 1024).toFixed(n < 10240 ? 1 : 0) + " KB" : (n / 1048576).toFixed(n < 10485760 ? 1 : 0) + " MB";
  const age = at => { const a = U().ago(Date.now() - U().ts(at)); return a === "now" ? "just now" : a ? a + " ago" : ""; };
  // The ORIGIN says how far to trust a version, and only `local` is the operator. `by` is never used for that.
  function originText(v) {
    if (!v) return "";
    if (v.origin === "local") return "you, at the machine";
    if (v.origin === "share") return "uploaded over the share";
    return v.by || "a card";
  }
  function safeName(n, fallback) { return String(n || fallback || "document").replace(/[\\/\u0000-\u001f]/g, "_").slice(0, 120) || "document"; }
  const isoDay = at => { const t = U().ts(at); return t ? new Date(t).toLocaleString() : ""; };

  // ── the sheet ────────────────────────────────────────────────────────────
  function build() {
    if (els) return els;
    const sheet = el("section", "dsheet");
    sheet.id = "m-docs";
    sheet.hidden = true;
    sheet.setAttribute("aria-label", "documents");
    try { const v = parseFloat(localStorage.getItem("atrium.mfs")); if (v) sheet.style.setProperty("--m-fs", v + "px"); } catch (e) {}
    const top = el("div", "d-top");
    const back = el("button", "d-back");
    back.type = "button";
    back.setAttribute("aria-label", "back");
    back.innerHTML = '<svg viewBox="0 0 24 24" width="22" height="22" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M15 5l-7 7 7 7"/></svg>';
    const title = el("span", "d-title");
    top.append(back, title);
    const scroll = el("div", "d-scroll");
    const body = el("div", "d-body");
    scroll.appendChild(body);
    sheet.append(top, scroll);
    document.body.appendChild(sheet);
    back.addEventListener("click", close);
    els = { sheet, title, scroll, body, back };
    if (window.mCard && window.mCard.bindPinch) window.mCard.bindPinch(scroll);
    return els;
  }
  function show(node) { els.body.textContent = ""; els.body.appendChild(node); }
  function note(text, cls) { return el("p", "d-note" + (cls ? " " + cls : ""), text); }

  // The address a view is known by. On the phone it is a real path the hub serves; on the board nothing navigates.
  function addr(st) {
    if (!isPhone()) return location.href;
    if (st.v === "list") return "/m/docs";
    return "/d/" + st.slug + (st.n ? "@" + st.n : "");
  }
  function go(st, replace) {
    build();
    const state = Object.assign({}, history.state && history.state.direct ? { direct: true } : {}, { mdocs: st });
    const card = window.mCard && window.mCard.current && window.mCard.current();
    if (card) state.mcard = card;
    try { (replace ? history.replaceState : history.pushState).call(history, state, "", addr(st)); } catch (e) {}
    render(st);
  }
  function render(st) {
    build();
    cur = st;
    els.sheet.hidden = false;
    document.body.classList.add("docs-open");
    els.scroll.scrollTop = 0;
    if (st.v === "list") renderList(st); else renderDoc(st);
  }
  function hide() {
    seq++;
    cur = null;
    dropUrls();
    if (!els) return;
    els.sheet.hidden = true;
    els.body.textContent = "";
    document.body.classList.remove("docs-open");
  }
  function close() {
    if (!els || els.sheet.hidden) return;
    if (history.state && history.state.direct && history.state.mdocs) {
      try { history.replaceState(null, "", isPhone() ? "/m/" : location.pathname); } catch (e) {}
      hide();
    } else if (history.state && history.state.mdocs) history.back();
    else hide();
  }

  // ── the list ─────────────────────────────────────────────────────────────
  function renderList(st) {
    const mine = ++seq;
    dropUrls();
    els.title.textContent = st.card ? "documents of one card" : "documents";
    const root = el("div", "d-list-view");
    const bar = el("div", "d-bar");
    const q = el("input", "d-q");
    q.type = "search";
    q.placeholder = "filter by title";
    q.setAttribute("aria-label", "filter by title");
    q.spellcheck = false;
    q.autocomplete = "off";
    q.value = st.q || "";
    const row2 = el("div", "d-bar");
    const delChip = btn("deleted", null, "d-chip");
    delChip.setAttribute("aria-pressed", st.deleted ? "true" : "false");
    const pick = el("input");
    pick.type = "file";
    pick.hidden = true;
    pick.setAttribute("aria-label", "choose a file to upload");
    const upBtn = btn("upload", () => pick.click(), "d-up");
    const titleIn = el("input", "d-tin");
    titleIn.type = "text";
    titleIn.placeholder = "title (optional)";
    titleIn.setAttribute("aria-label", "title of the new document");
    titleIn.autocomplete = "off";
    row2.append(delChip, upBtn, titleIn, pick);
    bar.append(q);
    const status = el("p", "d-status");
    status.setAttribute("role", "status");
    const list = el("div", "d-rows");
    const usage = el("p", "d-usage");
    root.append(bar, row2, status, list, usage);
    if (st.card) {
      const clear = btn("show every document", () => go({ v: "list" }, true), "d-chip");
      root.insertBefore(clear, bar);
    }
    show(root);

    let timer = 0;
    const load = async () => {
      const me = ++seq;
      const p = new URLSearchParams();
      if (q.value.trim()) p.set("q", q.value.trim());
      if (st.deleted) p.set("deleted", "1");
      if (st.card) p.set("card", st.card);
      let g;
      try { g = await hub("/_hub/docs" + (p.toString() ? "?" + p : "")); } catch (e) { g = { ok: false, status: 0, body: { error: "could not reach the hub" } }; }
      if (me !== seq || cur !== st) return;
      list.textContent = "";
      if (!g.ok) { list.appendChild(note(sentence(g.status, g.body), "d-err")); return; }
      const docs = (g.body && g.body.docs) || [];
      if (!docs.length) list.appendChild(note(st.deleted ? "nothing deleted" : q.value.trim() ? "no document has that in its title" : "no documents yet"));
      docs.forEach(d => list.appendChild(rowOf(d, st)));
      const u = g.body && g.body.usage;
      usage.textContent = u ? size(u.bytes) + " of " + size(u.cap) + " used" : "";
    };
    const again = () => { st.q = q.value; clearTimeout(timer); timer = setTimeout(load, 250); };
    q.addEventListener("input", again);
    delChip.addEventListener("click", () => { st.deleted = !st.deleted; delChip.setAttribute("aria-pressed", st.deleted ? "true" : "false"); load(); });
    pick.addEventListener("change", () => {
      const f = pick.files && pick.files[0];
      pick.value = "";
      if (f) doUpload(f);
    });
    // `override` is sent only after the operator is offered it, and only an operator is offered it: from anyone else it is a 403.
    const doUpload = async (f, override) => {
      status.textContent = "";
      status.classList.remove("d-err");
      status.textContent = "uploading 0%";
      const r = await upload("/_hub/docs", f, { title: titleIn.value.trim(), override: override ? "1" : "" }, n => { status.textContent = "uploading " + n + "%"; });
      if (cur !== st) return;
      status.textContent = "";
      if (r.ok && r.body && r.body.slug) {
        status.textContent = "uploaded ";
        const a = btn("open it", () => go({ v: "doc", slug: r.body.slug }), "d-link");
        status.appendChild(a);
        titleIn.value = "";
        load();
        return;
      }
      status.classList.add("d-err");
      status.textContent = sentence(r.status, r.body);
      if (r.status === 422 && settings && settings.operator) status.appendChild(btn("upload anyway", () => doUpload(f, true), "d-link"));
    };
    load();
  }

  function rowOf(d, st) {
    const row = el("div", "d-row" + (d.deleted ? " gone" : ""));
    const main = el("button", "d-main");
    main.type = "button";
    main.appendChild(el("b", "d-t", d.title || d.slug));
    const l = d.latest || {};
    const bits = [l.kind, d.versions > 1 ? d.versions + " versions" : "1 version", age(d.updated), originText(l)].filter(Boolean);
    main.appendChild(el("span", "d-m", bits.join(" · ")));
    main.addEventListener("click", () => go({ v: "doc", slug: d.slug }));
    row.appendChild(main);
    if (d.deleted) {
      const re = btn("restore", async () => {
        re.disabled = true;
        let r;
        try { r = await hub("/_hub/docs/" + encodeURIComponent(d.slug) + "/restore", { method: "POST" }); } catch (e) { r = { ok: false, status: 0, body: { error: "could not reach the hub" } }; }
        if (r.ok) { row.remove(); return; }
        re.disabled = false;
        row.appendChild(note(sentence(r.status, r.body), "d-err"));
      }, "d-link");
      row.appendChild(re);
    }
    return row;
  }

  // ── one document ─────────────────────────────────────────────────────────
  function sniff(b) {
    if (b.length > 8 && b[0] === 0x89 && b[1] === 0x50 && b[2] === 0x4e && b[3] === 0x47) return "image/png";
    if (b.length > 3 && b[0] === 0xff && b[1] === 0xd8 && b[2] === 0xff) return "image/jpeg";
    if (b.length > 6 && b[0] === 0x47 && b[1] === 0x49 && b[2] === 0x46 && b[3] === 0x38 && (b[4] === 0x37 || b[4] === 0x39) && b[5] === 0x61) return "image/gif";
    if (b.length > 12 && b[0] === 0x52 && b[1] === 0x49 && b[2] === 0x46 && b[3] === 0x46 && b[8] === 0x57 && b[9] === 0x45 && b[10] === 0x42 && b[11] === 0x50) return "image/webp";
    return "";
  }

  async function download(slug, v, name, b) {
    try {
      const r = await fetch("/_hub/docs/" + encodeURIComponent(slug) + "/raw?v=" + v.n);
      if (!r.ok) throw new Error("http");
      window.mMd.saveBlob(await r.blob(), safeName(name || v.name, slug));
    } catch (e) { if (b) { b.textContent = "not available"; b.disabled = true; } }
  }

  // The colour of a line of a diff document. A header (+++ or ---) is not an addition or a removal, and @@ is a hunk strip.
  function diffClass(line) {
    if (line.startsWith("@@")) return "d-hunk";
    if (line.startsWith("+++") || line.startsWith("---")) return "d-file";
    if (line.startsWith("+")) return "d-add";
    if (line.startsWith("-")) return "d-del";
    return "d-ctx";
  }
  // One div per line is drawn up to MAX_LINES, and the rest is a download, the same strip a cut text has.
  const MAX_LINES = 2000;
  function diffBlock(text, slug, v) {
    const wrap = el("div", "d-diffwrap");
    const pre = el("div", "d-diff");
    const lines = text.split(/\r?\n/);
    lines.slice(0, MAX_LINES).forEach(line => pre.appendChild(el("div", "d-line " + diffClass(line), line || "\u00a0")));
    wrap.appendChild(pre);
    if (lines.length > MAX_LINES) {
      const cut = el("div", "d-cut");
      cut.appendChild(el("span", "", "showing the first " + MAX_LINES + " of " + lines.length + " lines"));
      const more = btn("download the rest", () => download(slug, v, v.name, more));
      cut.appendChild(more);
      wrap.appendChild(cut);
    }
    return wrap;
  }

  async function renderDoc(st) {
    const mine = ++seq;
    dropUrls();
    els.title.textContent = st.slug;
    show(note("loading"));
    let g;
    try { g = await hub("/_hub/docs/" + encodeURIComponent(st.slug)); } catch (e) { g = { ok: false, status: 0, body: { error: "could not reach the hub" } }; }
    if (mine !== seq) return;
    if (!g.ok) { show(note(sentence(g.status, g.body), "d-err")); return; }
    const meta = g.body || {};
    const versions = Array.isArray(meta.versions) ? meta.versions : [];
    const v = st.n ? versions.find(x => x.n === st.n) : versions[versions.length - 1];
    els.title.textContent = meta.title || st.slug;
    const root = el("div", "d-doc");

    const head = el("div", "d-head");
    head.appendChild(el("h1", "d-h1", meta.title || st.slug));
    const ren = btn("rename", () => renameBox(), "d-link");
    head.appendChild(ren);
    root.appendChild(head);
    const renHold = el("div", "d-rename");
    root.appendChild(renHold);
    function renameBox() {
      if (renHold.firstChild) { renHold.textContent = ""; return; }
      const inp = el("input", "d-tin");
      inp.type = "text";
      inp.value = meta.title || "";
      inp.setAttribute("aria-label", "new title");
      const ok = btn("save", async () => {
        ok.disabled = true;
        let r;
        try { r = await hub("/_hub/docs/" + encodeURIComponent(st.slug) + "/title", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ title: inp.value }) }); } catch (e) { r = { ok: false, status: 0, body: { error: "could not reach the hub" } }; }
        if (r.ok) { go({ v: "doc", slug: st.slug, n: st.n }, true); return; }
        ok.disabled = false;
        renHold.appendChild(note(sentence(r.status, r.body), "d-err"));
      }, "d-link");
      renHold.append(inp, ok);
    }

    if (meta.deleted) {
      const ban = el("div", "d-banner");
      ban.appendChild(el("span", "", "deleted on " + isoDay(meta.deleted.at) + " by " + (meta.deleted.by || "someone")));
      const re = btn("restore", async () => {
        re.disabled = true;
        let r;
        try { r = await hub("/_hub/docs/" + encodeURIComponent(st.slug) + "/restore", { method: "POST" }); } catch (e) { r = { ok: false, status: 0, body: { error: "could not reach the hub" } }; }
        if (r.ok) { go({ v: "doc", slug: st.slug }, true); return; }
        re.disabled = false;
        ban.appendChild(note(sentence(r.status, r.body), "d-err"));
      }, "d-link");
      ban.appendChild(re);
      root.appendChild(ban);
      show(root);
      return;
    }
    if (!v) { root.appendChild(note("no such version", "d-err")); show(root); return; }

    // the version control, and a line about who wrote this one
    const vrow = el("div", "d-vrow");
    const sel = el("select", "d-sel");
    sel.setAttribute("aria-label", "version");
    versions.slice().reverse().forEach(x => {
      const o = el("option", "", "v" + x.n + (x === versions[versions.length - 1] ? " (newest)" : "") + " · " + originText(x));
      o.value = String(x.n);
      if (x.n === v.n) o.selected = true;
      sel.appendChild(o);
    });
    sel.addEventListener("change", () => go({ v: "doc", slug: st.slug, n: Number(sel.value) }));
    vrow.appendChild(sel);
    root.appendChild(vrow);

    const info = el("p", "d-info");
    info.appendChild(originNode(v));
    info.appendChild(document.createTextNode(" · " + [isoDay(v.at), size(v.size), v.name].filter(Boolean).join(" · ")));
    root.appendChild(info);

    // actions: history, compare, a new version, delete
    const acts = el("div", "d-acts");
    const hist = el("div", "d-hist");
    hist.hidden = true;
    acts.appendChild(btn("history", () => { hist.hidden = !hist.hidden; }));
    const textual = versions.filter(x => TEXT_KINDS.indexOf(x.kind) >= 0 && !x.missing && !x.purged);
    const cmp = el("div", "d-cmp");
    cmp.hidden = true;
    if (textual.length >= 2) acts.appendChild(btn("compare", () => { cmp.hidden = !cmp.hidden; }));
    const nv = el("input");
    nv.type = "file";
    nv.hidden = true;
    nv.setAttribute("aria-label", "choose the file for a new version");
    acts.appendChild(btn("new version", () => nv.click()));
    acts.appendChild(nv);
    const del = btn("delete", () => {
      del.hidden = true;
      const sure = el("span", "d-sure", "delete this document? ");
      const yes = btn("yes, delete", async () => {
        yes.disabled = true;
        let r;
        try { r = await hub("/_hub/docs/" + encodeURIComponent(st.slug) + "/delete", { method: "POST" }); } catch (e) { r = { ok: false, status: 0, body: { error: "could not reach the hub" } }; }
        if (r.ok) { go({ v: "doc", slug: st.slug }, true); return; }
        yes.disabled = false;
        sure.appendChild(note(sentence(r.status, r.body), "d-err"));
      }, "d-warn");
      sure.append(yes, btn("keep it", () => { sure.remove(); del.hidden = false; }));
      acts.appendChild(sure);
    }, "d-warn");
    acts.appendChild(del);
    root.appendChild(acts);
    const msg = el("p", "d-status");
    msg.setAttribute("role", "status");
    root.appendChild(msg);
    nv.addEventListener("change", async () => {
      const f = nv.files && nv.files[0];
      nv.value = "";
      if (!f) return;
      msg.classList.remove("d-err");
      msg.textContent = "uploading 0%";
      const r = await upload("/_hub/docs/" + encodeURIComponent(st.slug) + "/versions", f, {}, n => { msg.textContent = "uploading " + n + "%"; });
      if (cur !== st) return;
      if (r.ok) { go({ v: "doc", slug: st.slug }, true); return; }
      msg.classList.add("d-err");
      msg.textContent = sentence(r.status, r.body);
    });

    // history, newest first
    versions.slice().reverse().forEach(x => {
      const b = el("button", "d-hrow" + (x.n === v.n ? " on" : ""));
      b.type = "button";
      b.appendChild(el("b", "", "v" + x.n));
      b.appendChild(el("span", "", [originText(x), isoDay(x.at), size(x.size)].filter(Boolean).join(" · ")));
      b.addEventListener("click", () => go({ v: "doc", slug: st.slug, n: x.n }));
      hist.appendChild(b);
    });
    root.appendChild(hist);
    if (textual.length >= 2) root.appendChild(compareBox(st.slug, textual, cmp, v));

    const content = el("div", "d-content");
    root.appendChild(content);
    show(root);
    await body(st.slug, v, content, mine);
  }

  function originNode(v) {
    const span = el("span", "d-origin d-o-" + (v.origin || "card"));
    const label = originText(v);
    if (v.origin === "card" && v.card && window.mCard && window.mCard.pathOf) {
      let p = "";
      try { p = window.mCard.pathOf(v.card) || ""; } catch (e) { p = ""; }
      if (p) {
        const a = el("a", "", label);
        a.href = p;
        span.appendChild(a);
        return span;
      }
    }
    span.textContent = label;
    return span;
  }

  // The bytes of one version, drawn by what the hub says they are.
  async function body(slug, v, into, mine) {
    if (v.missing) { into.appendChild(note("the bytes are missing", "d-err")); return; }
    if (v.purged) { into.appendChild(note("the operator purged the bytes of this version", "d-err")); return; }
    const raw = "/_hub/docs/" + encodeURIComponent(slug) + "/raw?v=" + v.n;
    const fail = async r => {
      let b = null;
      try { b = await r.json(); } catch (e) {}
      into.appendChild(note(sentence(r.status, b), "d-err"));
    };
    try {
      if (v.kind === "image") {
        const r = await fetch(raw);
        if (mine !== seq) return;
        if (!r.ok) { await fail(r); return; }
        const got = await readCapped(r, IMG_CAP);
        if (mine !== seq) return;
        const type = got.over ? "" : sniff(got.bytes);
        if (!type) { downloadOnly(into, slug, v, "this image is not a png, jpeg, gif or webp, so it is not shown"); return; }
        // typed by what its first bytes say it is, never octet-stream, and revoked when the sheet moves on
        const obj = URL.createObjectURL(new Blob([got.bytes], { type }));
        urls.push(obj);
        const img = el("img", "d-img");
        img.alt = v.name || slug;
        img.onerror = () => { img.replaceWith(note("not available", "d-err")); };
        img.src = obj;
        into.appendChild(img);
        return;
      }
      if (TEXT_KINDS.indexOf(v.kind) < 0) { downloadOnly(into, slug, v, ""); return; }
      const r = await fetch(raw);
      if (mine !== seq) return;
      if (!r.ok) { await fail(r); return; }
      const got = await readCapped(r, CAP);
      if (mine !== seq) return;
      if (got.bytes.indexOf(0) >= 0) { downloadOnly(into, slug, v, "this does not look like text"); return; }
      const text = decode(got.bytes, got.over);
      if (v.kind === "markdown") {
        const holder = el("div", "md");
        holder.innerHTML = window.mMd.render(text, { id: "", worktree: "", noFiles: true });
        into.appendChild(holder);
      } else if (v.kind === "diff") {
        into.appendChild(diffBlock(text, slug, v));
      } else {
        const pre = el("pre", "code d-text");
        pre.appendChild(el("code", "", text));
        into.appendChild(pre);
      }
      if (got.over) {
        const cut = el("div", "d-cut");
        cut.appendChild(el("span", "", "showing the first " + size(got.bytes.length) + (v.size ? " of " + size(v.size) : "")));
        const more = btn("download the rest", () => download(slug, v, v.name, more));
        cut.appendChild(more);
        into.appendChild(cut);
      }
    } catch (e) { if (mine === seq) into.appendChild(note("could not read it", "d-err")); }
  }
  function downloadOnly(into, slug, v, why) {
    const box = el("div", "d-dl");
    if (why) box.appendChild(el("p", "", why));
    box.appendChild(el("p", "", (v.name || slug) + (v.size ? " (" + size(v.size) + ")" : "")));
    const b = btn("download", () => download(slug, v, v.name, b));
    box.appendChild(b);
    into.appendChild(box);
  }

  // ── compare two versions ─────────────────────────────────────────────────
  // Lines, with the common start and end trimmed first, then Myers' O((N+M)D). Past MAX_D changes it is refused with the
  // same sentence as an oversize side: there is no table to fill and nothing that can freeze the page.
  function myers(a, b, maxD) {
    const N = a.length, M = b.length, max = N + M;
    if (!max) return [];
    const lim = Math.min(max, maxD), off = lim + 1;
    let v = new Int32Array(2 * lim + 3);
    const trace = [];
    let found = -1;
    for (let d = 0; d <= lim && found < 0; d++) {
      trace.push(v.slice());
      for (let k = -d; k <= d; k += 2) {
        let x = (k === -d || (k !== d && v[off + k - 1] < v[off + k + 1])) ? v[off + k + 1] : v[off + k - 1] + 1;
        let y = x - k;
        while (x < N && y < M && a[x] === b[y]) { x++; y++; }
        v[off + k] = x;
        if (x >= N && y >= M) { found = d; break; }
      }
    }
    if (found < 0) return null;
    const ops = [];
    let x = N, y = M;
    for (let d = found; d >= 0; d--) {
      const vv = trace[d], k = x - y;
      const pk = (k === -d || (k !== d && vv[off + k - 1] < vv[off + k + 1])) ? k + 1 : k - 1;
      const px = vv[off + pk], py = px - pk;
      while (x > px && y > py && x > 0 && y > 0) { ops.push(["=", a[x - 1]]); x--; y--; }
      if (d > 0) { if (x === px) ops.push(["+", b[py]]); else ops.push(["-", a[px]]); }
      x = px; y = py;
    }
    return ops.reverse();
  }
  function diffOps(a, b) {
    let s = 0;
    while (s < a.length && s < b.length && a[s] === b[s]) s++;
    let ea = a.length, eb = b.length;
    while (ea > s && eb > s && a[ea - 1] === b[eb - 1]) { ea--; eb--; }
    const mid = myers(a.slice(s, ea), b.slice(s, eb), MAX_D);
    if (!mid) return null;
    return a.slice(0, s).map(l => ["=", l]).concat(mid, a.slice(ea).map(l => ["=", l]));
  }
  function compareBox(slug, textual, box, current) {
    const row = el("div", "d-cmprow");
    const mk = (label, pick) => {
      const s = el("select", "d-sel");
      s.setAttribute("aria-label", label);
      textual.forEach(x => { const o = el("option", "", "v" + x.n); o.value = String(x.n); if (x.n === pick) o.selected = true; s.appendChild(o); });
      return s;
    };
    const idx = textual.findIndex(x => x.n === current.n);
    const to = mk("compare to", textual[idx >= 0 ? idx : textual.length - 1].n);
    const from = mk("compare from", textual[Math.max(0, (idx >= 0 ? idx : textual.length - 1) - 1)].n);
    const out = el("div", "d-cmpout");
    const go2 = btn("show the changes", async () => {
      out.textContent = "";
      out.appendChild(note("reading"));
      const side = async n => {
        const r = await fetch("/_hub/docs/" + encodeURIComponent(slug) + "/raw?v=" + n);
        if (!r.ok) { let b = null; try { b = await r.json(); } catch (e) {} throw new Error(sentence(r.status, b)); }
        const got = await readCapped(r, TEXT_CAP);
        if (got.over) throw new Error(SAME);
        return decode(got.bytes, false).split(/\r?\n/);
      };
      try {
        const [a, b] = await Promise.all([side(from.value), side(to.value)]);
        const ops = diffOps(a, b);
        out.textContent = "";
        if (!ops) { out.appendChild(note(SAME, "d-err")); return; }
        if (!ops.some(o => o[0] !== "=")) { out.appendChild(note("the two versions are the same text")); return; }
        const wrap = el("div", "d-diff");
        // changed lines with a little around them, and a strip for each run of unchanged lines
        const keep = new Array(ops.length).fill(false);
        ops.forEach((o, i) => { if (o[0] !== "=") for (let j = Math.max(0, i - 3); j <= Math.min(ops.length - 1, i + 3); j++) keep[j] = true; });
        let skipped = 0;
        ops.forEach((o, i) => {
          if (!keep[i]) { skipped++; return; }
          if (skipped) { wrap.appendChild(el("div", "d-line d-hunk", "… " + skipped + " unchanged line" + (skipped === 1 ? "" : "s"))); skipped = 0; }
          wrap.appendChild(el("div", "d-line " + (o[0] === "+" ? "d-add" : o[0] === "-" ? "d-del" : "d-ctx"), (o[0] === "=" ? " " : o[0]) + o[1]));
        });
        if (skipped) wrap.appendChild(el("div", "d-line d-hunk", "… " + skipped + " unchanged line" + (skipped === 1 ? "" : "s")));
        out.appendChild(wrap);
      } catch (e) { out.textContent = ""; out.appendChild(note(e && e.message ? e.message : "could not read them", "d-err")); }
    });
    row.append(from, el("span", "", "to"), to, go2);
    box.append(row, out);
    return box;
  }

  // ── "published N documents" on a card ────────────────────────────────────
  // One request when a card opens, and at most one every 30 seconds while it stays open, however often its output moves.
  const counts = new Map();
  // The hub files a card's documents under `room~id`, and the card page holds the bare id while one room is attached.
  function filedId(id) {
    if (String(id).indexOf("~") > 0) return id;
    const t = window.mStore && window.mStore.card(id);
    // The desktop board loads this file without the phone page's network module, so no `mNet` there means no room
    // to name: the id stays bare, which is what a one-room hub files it under.
    const net = window.mNet;
    const rooms = net ? net.rooms() : [];
    const room = (t && t.room) || (net && net.room()) || (rooms.length === 1 ? rooms[0] : "");
    return room ? room + "~" + id : id;
  }
  async function paintCard(node, cardId) {
    if (!node || !cardId) return;
    cardId = filedId(cardId);
    node.hidden = true;
    if (!(await ready())) return;
    const c = counts.get(cardId) || { n: -1, at: 0, busy: false };
    counts.set(cardId, c);
    // The newest node asked about is the one drawn when the read lands. The peek rebuilds its body when its usage
    // read answers, so the node that started the read may be gone by then.
    c.node = node;
    const draw = () => {
      const n = c.node;
      if (!n || !n.isConnected || c.n <= 0) { if (n) n.hidden = true; return; }
      n.textContent = "published " + c.n + (c.n === 1 ? " document" : " documents");
      n.hidden = false;
      n.onclick = () => { openList({ card: cardId }); };
    };
    if (c.n >= 0) draw();
    if (c.busy || Date.now() - c.at < CARD_TTL) return;
    c.busy = true;
    try {
      const g = await hub("/_hub/docs?card=" + encodeURIComponent(cardId));
      if (g.ok && g.body && Array.isArray(g.body.docs)) { c.n = g.body.docs.length; c.at = Date.now(); }
    } catch (e) {}
    c.busy = false;
    draw();
  }

  // ── doors ────────────────────────────────────────────────────────────────
  async function openList(opts) { if (await ready()) go(Object.assign({ v: "list" }, opts || {})); }
  async function openDoc(slug, n) { if (await ready()) go({ v: "doc", slug, n: n || undefined }); }

  // A same-origin /d/<slug> link, anywhere on the page, opens here and does not navigate. The path must match exactly and
  // the origin must be this page's own, anything else keeps today's rules for links.
  function onClick(e) {
    if (e.defaultPrevented || e.button > 0 || e.metaKey || e.ctrlKey || e.shiftKey || avail !== true) return;
    const a = e.target && e.target.closest && e.target.closest("a[href]");
    if (!a) return;
    let u;
    try { u = new URL(a.getAttribute("href"), location.href); } catch (x) { return; }
    if (u.origin !== location.origin || !DOC_RE.test(u.pathname)) return;
    e.preventDefault();
    e.stopPropagation();
    const at = u.pathname.lastIndexOf("@");
    openDoc(at < 0 ? u.pathname.slice(3) : u.pathname.slice(3, at), at < 0 ? 0 : Number(u.pathname.slice(at + 1)));
  }

  function onAvailable(f) { hooks.push(f); if (avail !== null) f(avail); }

  async function init() {
    document.addEventListener("click", onClick, true);
    window.addEventListener("popstate", e => {
      if (!els || els.sheet.hidden) { if (e.state && e.state.mdocs && avail) render(e.state.mdocs); return; }
      if (e.state && e.state.mdocs) render(e.state.mdocs); else hide();
    });
    const btnM = document.getElementById("m-docs-btn");
    if (btnM) btnM.addEventListener("click", () => openList());
    const tab = document.querySelector('.tab[data-view="docs"]');
    if (tab) tab.addEventListener("click", e => { e.stopPropagation(); e.preventDefault(); openList(); }, true);
    onAvailable(a => {
      if (btnM) btnM.hidden = !a;
      if (tab) tab.hidden = !a;
    });
    if (!(await ready())) return;
    // an address of its own: /m/docs, or /d/<slug>[@n], on the phone's shell
    if (isPhone()) {
      const p = location.pathname;
      let st = null;
      if (p === "/m/docs") st = { v: "list" };
      else if (DOC_RE.test(p)) { const at = p.lastIndexOf("@"); st = { v: "doc", slug: at < 0 ? p.slice(3) : p.slice(3, at), n: at < 0 ? undefined : Number(p.slice(at + 1)) }; }
      if (st) {
        build();
        try { history.replaceState({ direct: true, mdocs: st }, "", addr(st)); } catch (e) {}
        render(st);
      }
    }
  }

  window.mDocs = { init, ready, onAvailable, openList, openDoc, paintCard, available: () => avail === true, close, diffClass, myers, diffOps };
  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", init); else init();
})();
