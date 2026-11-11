// The hub file drop and the card folders, as sources of the one files list that docs.js draws. See docs/rnd/hub-file-drop.md.
//
// One module for the phone's /m shell and the desktop board: the drop's rows, an upload with an optional note, a delete that
// asks in the page, and a card's folders read from its own room. It talks only to `/_hub/files` with a plain same-origin
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

  function rowOf(f, reload) {
    const row = el("div", "d-row d-file" + (soon(f.expires) ? " soon" : ""));
    const name = el("span", "d-main");
    const a = el("a", "f-dl", f.name);
    a.href = "/_hub/files/" + encodeURIComponent(f.name) + "/raw";
    a.setAttribute("download", f.name);
    name.appendChild(a);
    const meta = el("span", "d-m");
    [size(f.size), fromText(f), age(f.at), left(f.expires)].filter(Boolean).forEach(bit => meta.appendChild(el("span", "d-bit", bit)));
    meta.appendChild(el("span", "d-bit d-room", f.room || "hub"));
    name.appendChild(meta);
    if (f.note) name.appendChild(el("span", "f-note", f.note));
    const act = el("span", "f-act");
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
    row.append(name, act);
    return row;
  }

  async function list() {
    let g;
    try { g = await hub("/_hub/files"); } catch (e) { g = down; }
    return g.ok && g.body ? g.body : { files: [], usage: null };
  }

  const roomOf = t => t.room || (String(t.id).includes("~") ? String(t.id).split("~")[0] : "this room");
  function roomHeaders() {
    const h = {};
    try { const room = localStorage.getItem("atrium.room"); if (room) h["X-Atrium-Room"] = room; } catch (e) {}
    return h;
  }
  // A card's folder is read from the room the card lives on, through the same listing the card dialog uses. `live` says the
  // list is still the one on screen.
  async function folders(host, st, text, redraw, live) {
    const note = t => host.appendChild(el("p", "d-note", t));
    const get = async path => {
      const r = await fetch(path, { headers: roomHeaders() });
      if (!r.ok) throw new Error("the room answered " + r.status);
      return r.json();
    };
    try {
      if (!st.fcard) {
        const g = await get("/v1/tasks");
        if (!live()) return;
        const needle = text.toLowerCase();
        const cards = ((g && g.tasks) || []).filter(t => t && t.id && (!needle || String(t.display_title || t.alias || t.id).toLowerCase().includes(needle)));
        if (!cards.length) note("no card has that name");
        cards.forEach(t => {
          const row = el("div", "d-row d-card");
          const main = el("button", "d-main");
          main.type = "button";
          main.appendChild(el("b", "d-t", t.display_title || t.alias || t.id));
          const meta = el("span", "d-m");
          meta.appendChild(el("span", "d-bit d-room", roomOf(t)));
          main.appendChild(meta);
          main.addEventListener("click", () => { st.fcard = t.id; st.fpath = ""; redraw(); });
          row.appendChild(main);
          host.appendChild(row);
        });
        return;
      }
      const res = await get("/v1/tasks/" + encodeURIComponent(st.fcard) + "/files/list" + (st.fpath ? "?path=" + encodeURIComponent(st.fpath) : ""));
      if (!live()) return;
      const bar = el("div", "d-acts");
      bar.appendChild(btn("all cards", () => { st.fcard = ""; st.fpath = ""; redraw(); }));
      if (res.parent || st.fpath) bar.appendChild(btn("up", () => { st.fpath = res.parent || ""; redraw(); }));
      bar.appendChild(el("span", "d-m", res.path || "/"));
      host.appendChild(bar);
      const rows = res.entries || [];
      if (!rows.length) note("this folder is empty");
      rows.forEach(e => {
        const row = el("div", "d-row d-entry");
        if (e.dir) {
          const main = el("button", "d-main");
          main.type = "button";
          main.appendChild(el("b", "d-t", e.name + "/"));
          main.addEventListener("click", () => { st.fpath = e.path; redraw(); });
          row.appendChild(main);
        } else {
          const main = el("span", "d-main");
          const a = el("a", "f-dl", e.name);
          a.href = "/v1/tasks/" + encodeURIComponent(st.fcard) + "/files?path=" + encodeURIComponent(e.path);
          a.setAttribute("download", e.name);
          main.append(a, el("span", "d-m", size(e.size)));
          row.appendChild(main);
        }
        host.appendChild(row);
      });
    } catch (e) { if (live()) host.appendChild(el("p", "d-note d-err", e.message)); }
  }

  function onAvailable(f) { hooks.push(f); if (avail !== null) f(avail); }

  async function init() { await ready(); }

  window.mFiles = { init, ready, onAvailable, list, rowOf, upload, folders, available: () => avail === true };
  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", init); else init();
})();
