// The hub file drop, the view. See docs/rnd/hub-file-drop.md.
//
// One module for the phone's /m shell and the desktop board: the drop's files in a table, an upload with an optional
// note, a delete that asks in the page, and the usage bar. It talks only to `/_hub/files` with a plain same-origin
// fetch. A file is NEVER previewed or rendered here: a name is a download link to /raw, which the hub always sends as an
// attachment. Everything from the hub is set with textContent and never as markup.
(function () {
  "use strict";

  const DAY = 86400000;
  const U = () => window.mUtil || { ago: () => "", ts: x => Date.parse(x) || 0 };
  let els = null;
  let avail = null;               // null until asked, then true or false
  let seq = 0;
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
  const down = { ok: false, status: 0, body: { error: "could not reach the hub" } };
  function upload(file, note, onprog) {
    return new Promise(resolve => {
      const fd = new FormData();
      fd.append("file", file, file.name);
      if (note) fd.append("note", note);
      const x = new XMLHttpRequest();
      x.open("POST", "/_hub/files");
      if (x.upload && onprog) x.upload.onprogress = e => { if (e.lengthComputable) onprog(Math.round(e.loaded * 100 / e.total)); };
      x.onload = () => { let b = null; try { b = JSON.parse(x.responseText); } catch (e) {} resolve({ status: x.status, ok: x.status >= 200 && x.status < 300, body: b }); };
      x.onerror = () => resolve(down);
      x.send(fd);
    });
  }

  // A room's own board has no /_hub/files. Any failure at all, a 404 included, hides every door with no toast.
  function ready() {
    if (avail !== null) return Promise.resolve(avail);
    if (ready.p) return ready.p;
    ready.p = (async () => {
      try {
        const g = await hub("/_hub/files");
        avail = !!(g.ok && g.body && typeof g.body === "object" && !g.body.error);
      } catch (e) { avail = false; }
      hooks.forEach(f => { try { f(avail); } catch (e) {} });
      return avail;
    })();
    return ready.p;
  }

  const size = n => n == null ? "" : n < 1024 ? n + " B" : n < 1048576 ? (n / 1024).toFixed(n < 10240 ? 1 : 0) + " KB" : (n / 1048576).toFixed(n < 10485760 ? 1 : 0) + " MB";
  const age = at => { const a = U().ago(Date.now() - U().ts(at)); return a === "now" ? "just now" : a ? a + " ago" : ""; };
  function left(at) {
    const ms = U().ts(at) - Date.now();
    if (!U().ts(at)) return "";
    if (ms <= 0) return "expired";
    const h = Math.floor(ms / 3600000);
    return h < 48 ? (h < 1 ? "under an hour" : h + " h") : Math.floor(ms / DAY) + " days";
  }
  const soon = at => { const t = U().ts(at); return !!t && t - Date.now() < DAY; };
  const fromText = f => f.origin === "local" ? "you, at the machine" : f.origin === "share" ? "the share" : (f.by || "a card") + (f.room ? "@" + f.room : "");

  function build() {
    if (els) return els;
    const sheet = el("section", "dsheet");
    sheet.id = "m-files";
    sheet.hidden = true;
    sheet.setAttribute("aria-label", "files");
    try { const v = parseFloat(localStorage.getItem("atrium.mfs")); if (v) sheet.style.setProperty("--m-fs", v + "px"); } catch (e) {}
    const top = el("div", "d-top");
    const back = el("button", "d-back");
    back.type = "button";
    back.setAttribute("aria-label", "back");
    back.innerHTML = '<svg viewBox="0 0 24 24" width="22" height="22" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M15 5l-7 7 7 7"/></svg>';
    const title = el("span", "d-title", "files");
    top.append(back, title);
    const scroll = el("div", "d-scroll");
    const body = el("div", "d-body");
    scroll.appendChild(body);
    sheet.append(top, scroll);
    document.body.appendChild(sheet);
    back.addEventListener("click", hide);
    els = { sheet, body, scroll };
    return els;
  }
  function hide() {
    seq++;
    if (!els) return;
    els.sheet.hidden = true;
    els.body.textContent = "";
    document.body.classList.remove("docs-open");
  }

  function render() {
    build();
    els.sheet.hidden = false;
    document.body.classList.add("docs-open");
    els.scroll.scrollTop = 0;
    const root = el("div", "f-view");
    const head = el("div", "d-lhead");
    const heading = el("h2", "d-lh", "Files");
    const count = el("span", "d-count");
    heading.appendChild(count);
    head.appendChild(heading);

    const bar = el("div", "d-bar");
    const pick = el("input");
    pick.type = "file";
    pick.hidden = true;
    pick.setAttribute("aria-label", "choose a file to put in the drop");
    const noteIn = el("input", "d-tin");
    noteIn.type = "text";
    noteIn.placeholder = "note (optional)";
    noteIn.setAttribute("aria-label", "a note for the new file");
    noteIn.autocomplete = "off";
    const up = el("div", "d-upload");
    up.append(btn("upload", () => pick.click(), "d-up"), noteIn, pick);
    bar.appendChild(up);
    const status = el("p", "d-status");
    status.setAttribute("role", "status");
    const table = el("div", "f-rows");
    const usage = el("p", "d-usage");
    const meter = el("div", "d-meter");
    const fill = el("i");
    meter.appendChild(fill);
    const foot = el("div", "d-foot");
    foot.append(meter, usage);
    root.append(head, bar, status, table, foot);
    els.body.textContent = "";
    els.body.appendChild(root);

    const load = async () => {
      const me = ++seq;
      let g;
      try { g = await hub("/_hub/files"); } catch (e) { g = down; }
      if (me !== seq) return;
      table.textContent = "";
      if (!g.ok) { table.appendChild(el("p", "d-note d-err", sentence(g.status, g.body))); return; }
      const files = (g.body && g.body.files) || [];
      if (!files.length) table.appendChild(el("p", "d-note", "no files in the drop"));
      else {
        const h = el("div", "f-row f-h");
        ["name", "size", "from", "when", "expires", ""].forEach(t => h.appendChild(el("span", "f-c", t)));
        table.appendChild(h);
      }
      files.forEach(f => table.appendChild(rowOf(f, load)));
      const u = g.body && g.body.usage;
      usage.textContent = u ? size(u.bytes) + " of " + size(u.cap) + " used" : "";
      fill.style.width = u && u.cap ? Math.max(1, Math.min(100, u.bytes * 100 / u.cap)).toFixed(1) + "%" : "0%";
      meter.hidden = !(u && u.cap);
      count.textContent = files.length ? String(files.length) : "";
    };
    pick.addEventListener("change", async () => {
      const f = pick.files && pick.files[0];
      pick.value = "";
      if (!f) return;
      status.classList.remove("d-err");
      status.textContent = "uploading 0%";
      const r = await upload(f, noteIn.value.trim(), n => { status.textContent = "uploading " + n + "%"; });
      if (!r.ok) { status.classList.add("d-err"); status.textContent = sentence(r.status, r.body); return; }
      status.textContent = "uploaded " + (r.body && r.body.name || f.name);
      noteIn.value = "";
      load();
    });
    load();
  }

  function rowOf(f, reload) {
    const row = el("div", "f-row" + (soon(f.expires) ? " soon" : ""));
    const name = el("span", "f-c f-name");
    const a = el("a", "f-dl", f.name);
    a.href = "/_hub/files/" + encodeURIComponent(f.name) + "/raw";
    a.setAttribute("download", f.name);
    name.appendChild(a);
    if (f.note) name.appendChild(el("span", "f-note", f.note));
    const exp = el("span", "f-c f-exp", left(f.expires));
    if (soon(f.expires)) exp.title = "goes within a day";
    const act = el("span", "f-c f-act");
    const del = btn("delete", () => {
      del.hidden = true;
      const sure = el("span", "d-sure", "delete it? ");
      const yes = btn("yes", async () => {
        yes.disabled = true;
        let r;
        try { r = await hub("/_hub/files/" + encodeURIComponent(f.name) + "/delete", { method: "POST" }); } catch (e) { r = down; }
        if (r.ok) { reload(); return; }
        yes.disabled = false;
        sure.appendChild(el("span", "d-err", sentence(r.status, r.body)));
      }, "d-warn");
      sure.append(yes, btn("keep it", () => { sure.remove(); del.hidden = false; }));
      act.appendChild(sure);
    }, "d-warn");
    act.appendChild(del);
    row.append(name, el("span", "f-c", size(f.size)), el("span", "f-c", fromText(f)), el("span", "f-c", age(f.at)), exp, act);
    return row;
  }

  async function openList() { if (await ready()) render(); }
  function onAvailable(f) { hooks.push(f); if (avail !== null) f(avail); }

  async function init() {
    const b = document.getElementById("m-files-btn");
    if (b) b.addEventListener("click", () => openList());
    const tab = document.querySelector('.tab[data-view="files"]');
    if (tab) tab.addEventListener("click", e => { e.stopPropagation(); e.preventDefault(); openList(); }, true);
    onAvailable(a => {
      if (b) b.hidden = !a;
      if (tab) tab.hidden = !a;
    });
    await ready();
  }

  window.mFiles = { init, ready, onAvailable, openList, close: hide, available: () => avail === true };
  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", init); else init();
})();
