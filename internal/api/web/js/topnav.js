// ── the top nav: whole tabs, or the overflow menu ────────────────────────
//
// The chips on the right give way first (css/topnav.css). A tab that still has no room is hidden whole and listed in the `more`
// menu, so no tab is ever cut mid-word. Only on a desktop width: the phone and tablet header keeps its scrolling nav.
//
// Laid out again when the row changes size, never on a timer: a ResizeObserver on the bar, the nav, the chips and the tabs (a
// count badge or a tab that appears changes its width), and a MutationObserver on the nav for which tab is `on`. A hidden tab
// keeps its place in the layout (`visibility`), so the widths measured here do not move when the answer does.

(function () {
  const bar = document.querySelector("header .bar");
  const nav = bar && bar.querySelector("nav");
  const more = document.getElementById("tabmore");
  const menu = document.getElementById("tabs-menu");
  const grow = bar && bar.querySelector(".grow");
  if (!bar || !nav || !more || !menu || !grow) return;
  const desktop = window.matchMedia("(min-width: 901px)");

  function tabs() { return [...nav.querySelectorAll(".tab")].filter(t => !t.hidden); }

  function layout() {
    const out = new Set();
    const apply = () => nav.querySelectorAll(".tab").forEach(t => {
      if (t.classList.contains("ovf") !== out.has(t)) t.classList.toggle("ovf", out.has(t));
    });
    if (!desktop.matches) { apply(); more.hidden = true; closeMenu(); return; }
    const shown = tabs();
    if (!shown.length) { apply(); more.hidden = true; return; }
    const gap = parseFloat(getComputedStyle(nav).columnGap) || 0;
    const barGap = parseFloat(getComputedStyle(bar).columnGap) || 0;
    const widths = shown.map(t => t.offsetWidth);
    const total = widths.reduce((a, w) => a + w, 0) + gap * (shown.length - 1);
    // Room for the nav and the more button: from the nav's left edge to the spacer's right edge, less the gap before the next chip.
    const room = grow.getBoundingClientRect().right - nav.getBoundingClientRect().left - barGap;
    if (total <= room) { apply(); more.hidden = true; more.classList.remove("on"); closeMenu(); return; }
    more.hidden = false;
    let left = room - barGap - more.offsetWidth;
    let fit = 0;
    for (const w of widths) {
      if (w > left) break;
      left -= w + gap;
      fit++;
    }
    shown.slice(fit).forEach(t => out.add(t));
    apply();
    more.classList.toggle("on", shown.slice(fit).some(t => t.classList.contains("on")));
    if (!menu.hidden) fillMenu();
  }

  function fillMenu() {
    menu.replaceChildren();
    for (const t of tabs().filter(t => t.classList.contains("ovf"))) {
      const b = document.createElement("button");
      b.type = "button";
      b.textContent = t.firstChild.textContent;
      b.dataset.view = t.dataset.view;
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
  }

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
  desktop.addEventListener("change", layout);
  layout();
})();
