// A drag that selects text never fires a click. Board-wide, so no handler has to know.
// Capture on document runs before inline onclick attributes and element listeners.
(function () {
  let down = null;
  document.addEventListener("pointerdown", e => { down = { x: e.clientX, y: e.clientY, slop: e.pointerType === "mouse" ? 4 : 10 }; }, true);
  document.addEventListener("click", e => {
    const t = e.target;
    if (!t || !t.closest || t.closest("input, textarea, select, [contenteditable], .xterm")) return;
    let drag = !!down && Math.hypot(e.clientX - down.x, e.clientY - down.y) > down.slop;
    if (!drag) {
      const sel = window.getSelection();
      // The selection must touch the target: a stale selection elsewhere must not eat a plain click.
      drag = !!sel && !sel.isCollapsed && sel.rangeCount > 0 && sel.getRangeAt(0).intersectsNode(t);
    }
    if (drag) { e.stopImmediatePropagation(); e.preventDefault(); }
  }, true);
})();
