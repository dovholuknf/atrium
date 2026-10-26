// BOARD USAGE: one delegated listener counts the clicks on the board's controls, so a cut is made from data. A
// control's name is its most stable handle (see controlName). Counts are batched to POST /v1/usage every 10 s and on
// pagehide. Best effort: a failed post is dropped. Nothing is typed text, terminal input, scroll or hover.
(function () {
  const PICK = 'button, a[data-usage], a[data-action], a[data-view], a[id], [role="button"], [role="tab"], [role="menuitem"], summary, select, ' +
    'input[type="checkbox"], input[type="radio"], input[type="button"], input[type="submit"], .tab, .pill, ' +
    '[data-action], [data-view], [data-usage]';
  const MAXLEN = 200;
  const pending = new Map();

  function clip(s) {
    s = String(s || "").replace(/\s+/g, " ").trim();
    while (new TextEncoder().encode(s).length > MAXLEN) s = s.slice(0, -1);
    return s;
  }
  function labelOf(el) {
    return el.getAttribute("aria-label") || el.getAttribute("title") || el.value && el.type === "button" && el.value || textOf(el);
  }
  // textContent with a space between text nodes, so a card title and its cwd in two spans do not run together.
  function textOf(el) {
    const out = [];
    const w = document.createTreeWalker(el, NodeFilter.SHOW_TEXT);
    while (w.nextNode()) out.push(w.currentNode.nodeValue);
    return out.join(" ");
  }
  // A label with its digits made '#' and cut to 40, so "stash 3 files" and "stash 12 files" are one control.
  function tidy(s) {
    return clip(String(s || "").replace(/\d+/g, "#")).slice(0, 40).trim();
  }
  const ROW = '[data-id], [data-task], li, tr, .row, .card, .term-row';
  // The most stable name: data-usage, then data-action, data-view, the id, a dialog's id plus the button's label, a
  // button in a repeated row as the row's class plus the button's label, and last the label alone. Never text that
  // varies per card (a dialog title, a row's own text), or the counts split per card and not per control.
  function controlName(el) {
    if (!el || !el.getAttribute) return "";
    const d = el.dataset || {};
    if (d.usage) return clip(d.usage);
    if (d.action) return clip("action:" + d.action);
    if (d.view) return clip("view:" + d.view);
    const dlg = el.closest && el.closest("dialog");
    if (dlg) {
      const dn = dlg.dataset.usage || dlg.id || "dialog";
      return clip("dialog:" + dn + " / " + (el.id || tidy(labelOf(el))));
    }
    if (el.id) return clip("id:" + el.id);
    const l = tidy(labelOf(el));
    if (!l) return "";
    const row = el.closest && el.closest(ROW);
    if (row && row !== el) return clip((row.classList[0] || row.tagName.toLowerCase()) + ":" + l);
    return clip("label:" + l);
  }
  window.boardControlName = controlName;

  // Typing, the terminal and the usage view's own rows are not board controls.
  function skip(el) {
    return !!(el.closest && el.closest(".xterm, textarea, #usagedlg-list, [data-usage-skip]"));
  }
  function count(el) {
    const c = el && el.closest && el.closest(PICK);
    if (!c || skip(c) || c.disabled) return;
    const name = controlName(c);
    if (name) pending.set(name, (pending.get(name) || 0) + 1);
  }
  // A select counts when it changes, not when it is opened. Everything else counts on click, and Enter on a button
  // arrives as a click.
  document.addEventListener("click", (e) => {
    const c = e.target && e.target.closest && e.target.closest(PICK);
    if (c && c.tagName === "SELECT") return;
    count(e.target);
  }, true);
  document.addEventListener("change", (e) => { if (e.target && e.target.tagName === "SELECT") count(e.target); }, true);

  // Always a keepalive fetch, which survives the page closing and, unlike a beacon, carries the room header the board
  // adds to every fetch. A beacon went to the hub with no room named and was refused with several rooms attached.
  function flush() {
    if (!pending.size) return Promise.resolve();
    const body = JSON.stringify({ counts: Object.fromEntries(pending) });
    pending.clear();
    try {
      return fetch("/v1/usage", { method: "POST", headers: { "Content-Type": "application/json" }, body, keepalive: true }).catch(() => {});
    } catch (_) { return Promise.resolve(); /* dropped */ }
  }
  window.boardUsageFlush = () => flush();
  setInterval(() => flush(), 10000);
  addEventListener("pagehide", () => flush());

  // The view: what was used, then what the board has but nobody has used. Opens at once from a SNAPSHOT of the last
  // read (the totals) and of the last look at the DOM (the known controls), then reads again and repaints if it changed.
  // Sorted by count, most first, or by name; a click on the same header flips it. Never rows stay at the bottom.
  const dlg = document.getElementById("usagedlg");
  const list = document.getElementById("usagedlg-list");
  const heads = { name: document.getElementById("usagedlg-h-name"), n: document.getElementById("usagedlg-h-n") };
  const snap = { used: null, known: null, at: 0 };
  const sort = { key: "n", dir: -1 };
  function knownNow() {
    const out = new Set();
    for (const el of document.querySelectorAll(PICK)) {
      if (el.closest("#usagedlg") || el.closest("[data-usage-skip]")) continue;
      const n = controlName(el);
      if (n) out.add(n);
    }
    return out;
  }
  function byName(x, y) { return x.name < y.name ? -1 : x.name > y.name ? 1 : 0; }
  function order(rows, never) {
    const k = sort.key === "n" && !never ? (x, y) => sort.dir * (x.total - y.total) || byName(x, y)
      : (x, y) => (sort.key === "name" ? sort.dir : 1) * byName(x, y);
    return rows.slice().sort(k);
  }
  function draw() {
    const used = (snap.used || []).map(u => ({ name: u.control, total: u.n, last: u.last }));
    const seen = new Set(used.map(u => u.name));
    const never = [...(snap.known || [])].filter(n => !seen.has(n)).map(name => ({ name, total: 0, last: "" }));
    const row = (r, isNever) => {
      const d = document.createElement("div");
      d.className = "usagedlg-row" + (isNever ? " never" : "");
      const a = document.createElement("span"); a.className = "name"; a.textContent = r.name;
      const b = document.createElement("span"); b.className = "n"; b.textContent = isNever ? "never" : String(r.total);
      const c = document.createElement("span"); c.className = "last"; c.textContent = isNever ? "" : r.last;
      d.append(a, b, c);
      return d;
    };
    list.replaceChildren(...order(used, false).map(r => row(r, false)), ...order(never, true).map(r => row(r, true)));
    for (const k of ["name", "n"]) {
      const on = sort.key === k;
      heads[k].setAttribute("aria-sort", on ? (sort.dir < 0 ? "descending" : "ascending") : "none");
      heads[k].dataset.arrow = on ? (sort.dir < 0 ? "\u25BC" : "\u25B2") : "";
    }
  }
  async function refresh() {
    await flush();
    if (typeof hubGateHold === "function") await hubGateHold();
    try {
      const r = await fetch("/v1/usage/board");
      if (typeof hubGateStatus === "function" && hubGateStatus(r.status)) hubGateTrip();
      if (r.ok) snap.used = (await r.json()).controls || [];
    } catch (_) { /* keeps the last totals */ }
    snap.known = knownNow();
    snap.at = Date.now();
    if (dlg.open) draw();
  }
  function sortBy(key) {
    if (sort.key === key) sort.dir = -sort.dir;
    else { sort.key = key; sort.dir = key === "n" ? -1 : 1; }
    draw();
  }
  heads.name.onclick = () => sortBy("name");
  heads.n.onclick = () => sortBy("n");
  window.boardUsageSort = (key, dir) => { sort.key = key; sort.dir = dir; draw(); };
  window.openControlsUsed = () => {
    if (snap.used) draw();
    if (!dlg.open) dlg.showModal();
    refresh();
  };
  document.getElementById("usagedlg-close").onclick = () => dlg.close();
  // A snapshot is ready before anyone asks: once the page has settled, and again whenever the room's counts may have moved.
  const idle = window.requestIdleCallback || ((f) => setTimeout(f, 1500));
  setTimeout(() => idle(refresh), 4000);
})();
