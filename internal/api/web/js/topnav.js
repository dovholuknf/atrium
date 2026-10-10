// ── the top nav: whole tabs, or the overflow menu ────────────────────────
//
// The chips on the right give way first (css/topnav.css). A tab that still has no room is hidden whole and listed in the `more`
// menu, so no tab is ever cut mid-word. Only on a desktop width: the phone and tablet header keeps its scrolling nav.
//
// Laid out again when the row changes size, never on a timer: a ResizeObserver on the bar, the nav, the chips and the tabs (a
// count badge or a tab that appears changes its width), and a MutationObserver on the nav for which tab is `on`. A hidden tab
// keeps its place in the layout (`visibility`), so the widths measured here do not move when the answer does.
//
// YOUR ORDER AND PLACEMENT. `atrium.tabs` in localStorage holds `{ order: [view...], place: { view: "menu" | "hidden" } }`, per
// browser like the other board preferences. The order is the DOM order of the nav. `menu` puts a tab in the `...` menu always
// (`.umenu`), `hidden` takes it off the bar and the menu (`.uhide`). The author's `hidden` attribute is left alone, since the
// docs, pulls, audit, repos and backlog tabs reveal themselves with it. A hidden tab that has a count waiting is NOT hidden: it
// goes into the `...` menu, and the `...` button carries the sum of the counts of everything in the menu, so nothing that waits
// on the human goes quiet. The overflow above still runs over the tabs left in the bar.

(function () {
  const bar = document.querySelector("header .bar");
  const nav = bar && bar.querySelector("nav");
  const more = document.getElementById("tabmore");
  const menu = document.getElementById("tabs-menu");
  const grow = bar && bar.querySelector(".grow");
  if (!bar || !nav || !more || !menu || !grow) return;
  const desktop = window.matchMedia("(min-width: 901px)");

  const KEY = "atrium.tabs";
  const defaultOrder = [...nav.querySelectorAll(".tab")].map(t => t.dataset.view);
  const names = Object.fromEntries([...nav.querySelectorAll(".tab")].map(t => [t.dataset.view, t.firstChild.textContent]));

  // Where a tab sits before anybody chose. The chatter tab is a read-through of history rather than daily work, so it starts in
  // the `...` menu. A saved `"bar"` is the one way to put such a tab back in the bar.
  const defaultPlace = { chatter: "menu" };

  function prefs() {
    let v = {};
    try { v = JSON.parse(localStorage.getItem(KEY) || "{}") || {}; } catch (e) {}
    const known = new Set(defaultOrder);
    const order = (Array.isArray(v.order) ? v.order : []).filter((x, i, a) => known.has(x) && a.indexOf(x) === i);
    for (const x of defaultOrder) if (!order.includes(x)) order.push(x);
    const place = {};
    for (const [k, w] of Object.entries(defaultPlace)) if (known.has(k)) place[k] = w;
    for (const [k, w] of Object.entries(v.place && typeof v.place === "object" ? v.place : {})) {
      if (!known.has(k)) continue;
      if (w === "menu" || w === "hidden") place[k] = w;
      else if (w === "bar") delete place[k];
    }
    return { order, place };
  }
  function savePrefs(p) {
    // What is stored is only what differs from the defaults, a tab put in the bar against its default as "bar".
    const place = {};
    for (const [k, w] of Object.entries(p.place)) if (defaultPlace[k] !== w) place[k] = w;
    for (const k of Object.keys(defaultPlace)) if (!p.place[k]) place[k] = "bar";
    const plain = p.order.every((x, i) => x === defaultOrder[i]) && !Object.keys(place).length;
    if (plain) localStorage.removeItem(KEY); else localStorage.setItem(KEY, JSON.stringify({ order: p.order, place }));
    layout();
  }
  function counted(t) {
    const n = t.querySelector(".count");
    return !!n && !!n.textContent && !n.classList.contains("zero");
  }
  // Order the nav and set who is forced into the menu or off the bar. Only writes what changed, so the observers settle.
  function applyPrefs() {
    const p = prefs();
    const cur = [...nav.querySelectorAll(".tab")];
    if (cur.some((t, i) => t.dataset.view !== p.order[i])) {
      const by = Object.fromEntries(cur.map(t => [t.dataset.view, t]));
      nav.append(...p.order.map(v => by[v]));
    }
    for (const t of cur) {
      const w = p.place[t.dataset.view];
      const menuIt = w === "menu" || (w === "hidden" && counted(t));
      const hideIt = w === "hidden" && !menuIt;
      if (t.classList.contains("umenu") !== menuIt) t.classList.toggle("umenu", menuIt);
      if (t.classList.contains("uhide") !== hideIt) t.classList.toggle("uhide", hideIt);
    }
  }

  function tabs() { return [...nav.querySelectorAll(".tab")].filter(t => !t.hidden && !t.classList.contains("uhide") && !t.classList.contains("umenu")); }
  function forced() { return [...nav.querySelectorAll(".tab.umenu")].filter(t => !t.hidden); }
  function inMenu() { return [...nav.querySelectorAll(".tab")].filter(t => !t.hidden && (t.classList.contains("ovf") || t.classList.contains("umenu"))); }

  function badge() {
    const b = more.querySelector(".count");
    let n = 0;
    for (const t of inMenu()) if (counted(t)) n += parseInt(t.querySelector(".count").textContent, 10) || 1;
    b.textContent = n ? String(n) : "";
    b.hidden = !n;
  }

  function layout() {
    applyPrefs();
    const out = new Set();
    const apply = () => nav.querySelectorAll(".tab").forEach(t => {
      if (t.classList.contains("ovf") !== out.has(t)) t.classList.toggle("ovf", out.has(t));
    });
    // The phone header scrolls its nav and has no `...` button: a tab placed in the menu is simply in the bar there.
    if (!desktop.matches) { apply(); more.hidden = true; closeMenu(); return; }
    const shown = tabs();
    const need = forced().length > 0;
    if (!shown.length) { apply(); more.hidden = !need; badge(); more.classList.remove("on"); if (!menu.hidden) fillMenu(); return; }
    const gap = parseFloat(getComputedStyle(nav).columnGap) || 0;
    const barGap = parseFloat(getComputedStyle(bar).columnGap) || 0;
    const widths = shown.map(t => t.offsetWidth);
    const total = widths.reduce((a, w) => a + w, 0) + gap * (shown.length - 1);
    // Room for the nav and the more button: from the nav's left edge to the spacer's right edge, less the gap before the next chip.
    const room = grow.getBoundingClientRect().right - nav.getBoundingClientRect().left - barGap;
    if (!need && total <= room) { apply(); more.hidden = true; more.classList.remove("on"); badge(); closeMenu(); return; }
    more.hidden = false;
    if (total <= room - barGap - more.offsetWidth) { apply(); badge(); more.classList.toggle("on", forced().some(t => t.classList.contains("on"))); if (!menu.hidden) fillMenu(); return; }
    let left = room - barGap - more.offsetWidth;
    let fit = 0;
    for (const w of widths) {
      if (w > left) break;
      left -= w + gap;
      fit++;
    }
    shown.slice(fit).forEach(t => out.add(t));
    apply();
    badge();
    more.classList.toggle("on", inMenu().some(t => t.classList.contains("on")));
    if (!menu.hidden) fillMenu();
  }

  function fillMenu() {
    menu.replaceChildren();
    const place = prefs().place;
    for (const t of inMenu()) {
      const b = document.createElement("button");
      b.type = "button";
      b.textContent = t.firstChild.textContent;
      b.dataset.view = t.dataset.view;
      if (place[t.dataset.view] === "hidden") b.title = "hidden from the bar, shown here while something is waiting";
      b.classList.toggle("on", t.classList.contains("on"));
      const n = t.querySelector(".count");
      if (n && n.textContent) {
        const c = document.createElement("span");
        c.className = "count";
        c.textContent = n.textContent;
        b.append(c);
      }
      b.onclick = () => { closeMenu(); switchView(t.dataset.view); };
      menu.append(b);
    }
    const arr = document.createElement("button");
    arr.type = "button";
    arr.className = "arrange";
    arr.textContent = "arrange tabs...";
    arr.dataset.usage = "menu:arrange tabs";
    arr.onclick = () => { closeMenu(); openArrange(); };
    menu.append(arr);
    const used = document.createElement("button");
    used.type = "button";
    used.className = "arrange";
    used.dataset.usage = "menu:controls used";
    used.textContent = "controls used";
    used.onclick = () => { closeMenu(); if (window.openControlsUsed) window.openControlsUsed(); };
    menu.append(used);
  }

  // The editor: one row per tab, up and down to order it, a select for where it shows, and a reset.
  const dlg = document.getElementById("tabsdlg");
  const list = document.getElementById("tabsdlg-list");
  function paintArrange() {
    const p = prefs();
    list.replaceChildren();
    p.order.forEach((v, i) => {
      const row = document.createElement("div");
      row.className = "tabsdlg-row";
      row.dataset.view = v;
      const mk = (txt, label, off, fn) => {
        const b = document.createElement("button");
        b.type = "button"; b.textContent = txt; b.setAttribute("aria-label", label + " " + names[v]); b.disabled = off;
        b.onclick = fn; return b;
      };
      const name = document.createElement("span");
      name.className = "name"; name.textContent = names[v];
      const sel = document.createElement("select");
      sel.setAttribute("aria-label", "where " + names[v] + " shows");
      for (const [val, txt] of [["bar", "in the bar"], ["menu", "in the ... menu"], ["hidden", "hidden"]]) {
        const o = document.createElement("option"); o.value = val; o.textContent = txt; sel.append(o);
      }
      sel.value = p.place[v] || "bar";
      sel.onchange = () => {
        const q = prefs();
        if (sel.value === "bar") delete q.place[v]; else q.place[v] = sel.value;
        savePrefs(q); paintArrange();
      };
      const move = (d) => () => {
        const q = prefs(); const j = q.order.indexOf(v);
        q.order.splice(j, 1); q.order.splice(j + d, 0, v);
        savePrefs(q); paintArrange();
        const again = list.querySelector('[data-view="' + v + '"] button[aria-label^="move ' + (d < 0 ? "up" : "down") + '"]');
        if (again && !again.disabled) again.focus();
      };
      row.append(mk("\u2191", "move up", i === 0, move(-1)), mk("\u2193", "move down", i === p.order.length - 1, move(1)), name, sel);
      list.append(row);
    });
  }
  window.openTabsArrange = openArrange;
  function openArrange() { paintArrange(); if (!dlg.open) dlg.showModal(); }
  document.getElementById("tabsdlg-close").onclick = () => dlg.close();
  document.getElementById("tabsdlg-reset").onclick = () => { localStorage.removeItem(KEY); layout(); paintArrange(); };
  // Delegated: the settings dialog is painted again at runtime, so a handler on its button would not last.
  document.addEventListener("click", (e) => { if (e.target.closest && e.target.closest("#s-arrangetabs")) openArrange(); });
  addEventListener("storage", (e) => { if (e.key === KEY || e.key === null) { layout(); if (dlg.open) paintArrange(); } });

  function closeMenu() {
    if (menu.hidden) return;
    menu.hidden = true;
    more.setAttribute("aria-expanded", "false");
  }

  more.onclick = (e) => {
    e.stopPropagation();
    if (!menu.hidden) return closeMenu();
    fillMenu();
    const r = more.getBoundingClientRect();
    menu.style.top = Math.round(r.bottom + 6) + "px";
    menu.style.left = Math.max(8, Math.min(Math.round(r.left), innerWidth - 180)) + "px";
    menu.hidden = false;
    more.setAttribute("aria-expanded", "true");
  };
  document.addEventListener("click", (e) => { if (!menu.hidden && !menu.contains(e.target)) closeMenu(); });
  document.addEventListener("keydown", (e) => { if (e.key === "Escape") closeMenu(); });

  const ro = new ResizeObserver(layout);
  ro.observe(bar);
  ro.observe(nav);
  for (const el of bar.children) if (el !== nav && el !== grow && el !== more) ro.observe(el);
  nav.querySelectorAll(".tab").forEach(t => ro.observe(t));
  new MutationObserver(layout).observe(nav, { attributes: true, subtree: true, attributeFilter: ["class", "hidden"] });
  // A count that changes is what lets a hidden tab through, and what the `...` badge sums.
  new MutationObserver(layout).observe(nav, { childList: true, characterData: true, subtree: true });
  desktop.addEventListener("change", layout);
  layout();
})();
