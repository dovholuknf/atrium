// The phone folds the group, sort and show pill rows behind one "filters" button, and says how many of them are not at
// their defaults. The rows are hidden by CSS on a phone only (see css/phone.css), so on a desktop this button is not
// drawn and nothing here is visible. The count is read from the pills themselves, so every place that repaints them
// keeps it right without calling in.
(function () {
  const btns = () => Array.from(document.querySelectorAll(".filtersbtn"));
  const on = id => Array.from(document.querySelectorAll("#" + id + " button.on"));

  function count() {
    let n = 0;
    const seg = on("stack-seg").map(b => b.dataset.v);
    if (!(seg.length === 1 && seg[0] === "needs-input")) n++;
    const sort = on("stack-sort")[0];
    if (sort && sort.dataset.sort !== "activity") n++;
    const g = on("stack-group")[0];
    if (g && !/setGroupMode\('project'\)/.test(g.getAttribute("onclick") || "")) n++;
    return n;
  }
  function boardCount() {
    let n = 0;
    const s = on("board-sort")[0];
    if (s && !/setBoardSort\('activity'\)/.test(s.getAttribute("onclick") || "")) n++;
    const g = on("board-group")[0];
    if (g && !/setGroupMode\('project'\)/.test(g.getAttribute("onclick") || "")) n++;
    return n;
  }

  function paint() {
    btns().forEach(b => {
      const n = b.dataset.for === "board" ? boardCount() : count();
      const c = b.querySelector(".fcount");
      if (c) { c.textContent = n ? String(n) : ""; c.hidden = !n; }
      b.dataset.active = String(n);
    });
  }

  function sync() {
    const open = document.body.classList.contains("filters-open");
    btns().forEach(b => b.setAttribute("aria-expanded", open ? "true" : "false"));
  }

  document.addEventListener("click", e => {
    const b = e.target.closest && e.target.closest(".filtersbtn");
    if (!b) return;
    document.body.classList.toggle("filters-open");
    sync();
  });

  let queued = false;
  const later = () => { if (!queued) { queued = true; requestAnimationFrame(() => { queued = false; paint(); }); } };
  const mo = new MutationObserver(later);
  ["stack-seg", "stack-sort", "stack-group", "board-sort", "board-group"].forEach(id => {
    const el = document.getElementById(id);
    if (el) mo.observe(el, { childList: true, subtree: true, attributes: true, attributeFilter: ["class"] });
  });
  sync();
  paint();
})();
