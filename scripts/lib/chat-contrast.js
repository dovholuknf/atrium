// Runs in the page: every text node's colour against what it sits on, composited down through its ancestors.
module.exports = function pairs() {
  const parse = s => { const m = s.match(/rgba?\(([^)]+)\)/); if (!m) return null; const p = m[1].split(/[ ,/]+/).filter(Boolean).map(Number); return { r: p[0], g: p[1], b: p[2], a: p.length > 3 ? p[3] : 1 }; };
  const over = (f, b) => ({ r: f.r * f.a + b.r * (1 - f.a), g: f.g * f.a + b.g * (1 - f.a), b: f.b * f.a + b.b * (1 - f.a), a: 1 });
  const lum = c => { const f = v => { v /= 255; return v <= .03928 ? v / 12.92 : Math.pow((v + .055) / 1.055, 2.4); }; return .2126 * f(c.r) + .7152 * f(c.g) + .0722 * f(c.b); };
  const ratio = (a, b) => { const x = lum(a), y = lum(b); return (Math.max(x, y) + .05) / (Math.min(x, y) + .05); };
  const root = parse(document.body.dataset.pane || "") || { r: 255, g: 255, b: 255, a: 1 };
  const behind = el => {
    const stack = [];
    for (let n = el; n && n.nodeType === 1; n = n.parentElement) {
      const cs = getComputedStyle(n);
      const c = parse(cs.backgroundColor);
      const g = /gradient/.test(cs.backgroundImage) && parse(cs.backgroundImage);
      if (c && c.a > 0) stack.push(c);
      if (g && g.a > 0) stack.push(g);
      if ((g && g.a === 1) || (c && c.a === 1 && !g)) break;
    }
    return stack.reverse().reduce((acc, c) => over(c, acc), root);
  };
  const out = [];
  const seen = new Set();
  const take = (el, name, fg) => {
    let a = fg.a;
    for (let n = el; n && n.nodeType === 1; n = n.parentElement) a *= Number(getComputedStyle(n).opacity);
    const bg = behind(el);
    const t = over({ r: fg.r, g: fg.g, b: fg.b, a }, bg);
    out.push({ name, ratio: ratio(t, bg) });
  };
  for (const el of document.querySelectorAll("#m-card *")) {
    const cs = getComputedStyle(el);
    if (cs.display === "none" || el.closest("[hidden]")) continue;
    const name = el.tagName.toLowerCase() + (el.className && typeof el.className === "string" ? "." + el.className.trim().split(/\s+/).join(".") : "");
    const own = Array.from(el.childNodes).some(n => n.nodeType === 3 && n.textContent.trim());
    const disabled = el.disabled === true && /q-send/.test(el.className);
    const key = name + (disabled ? "[disabled]" : "");
    if ((own || el.querySelector(":scope > svg")) && !seen.has(key)) {
      seen.add(key);
      const fg = parse(cs.color);
      if (fg) take(el, key, fg);
    }
    if (el.placeholder) take(el, name + "::placeholder", parse(getComputedStyle(el, "::placeholder").color));
  }
  return out;
};
