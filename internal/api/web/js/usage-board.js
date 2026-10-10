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
    return el.getAttribute("aria-label") || el.getAttribute("title") || el.value && el.type === "button" && el.value || el.textContent;
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

  function flush(beacon) {
    if (!pending.size) return;
    const body = JSON.stringify({ counts: Object.fromEntries(pending) });
    pending.clear();
    try {
      if (beacon && navigator.sendBeacon) {
        navigator.sendBeacon("/v1/usage", new Blob([body], { type: "application/json" }));
      } else {
        fetch("/v1/usage", { method: "POST", headers: { "Content-Type": "application/json" }, body, keepalive: true }).catch(() => {});
      }
    } catch (_) { /* dropped */ }
  }
  window.boardUsageFlush = () => flush(false);
  setInterval(() => flush(false), 10000);
  addEventListener("pagehide", () => flush(true));

  // The view: what was used, most first, then what the board has but nobody has used.
  const dlg = document.getElementById("usagedlg");
  const list = document.getElementById("usagedlg-list");
  function knownNow() {
    const out = new Set();
    for (const el of document.querySelectorAll(PICK)) {
      if (el.closest("#usagedlg") || el.closest("[data-usage-skip]")) continue;
      const n = controlName(el);
      if (n) out.add(n);
    }
    return out;
  }
  async function paint() {
    list.replaceChildren();
    flush(false);
    let used = [];
    try {
      const r = await fetch("/v1/usage/board");
      if (r.ok) used = (await r.json()).controls || [];
    } catch (_) { /* shows the never list alone */ }
    const seen = new Set(used.map(u => u.control));
    const row = (name, total, last, never) => {
      const d = document.createElement("div");
      d.className = "usagedlg-row" + (never ? " never" : "");
      const a = document.createElement("span"); a.className = "name"; a.textContent = name;
      const b = document.createElement("span"); b.className = "n"; b.textContent = never ? "never" : String(total);
      const c = document.createElement("span"); c.className = "last"; c.textContent = never ? "" : last;
      d.append(a, b, c);
      list.append(d);
    };
    for (const u of used) row(u.control, u.n, u.last, false);
    for (const n of [...knownNow()].sort()) if (!seen.has(n)) row(n, 0, "", true);
  }
  window.openControlsUsed = () => { paint(); if (!dlg.open) dlg.showModal(); };
  document.getElementById("usagedlg-close").onclick = () => dlg.close();
})();
