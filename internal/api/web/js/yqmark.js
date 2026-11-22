// YOUR QUEUE, MARK UP: on a TRY or APPROVE ask the operator draws arrows, circles and boxes on the agent's page and writes a
// note on each, then sends the batch to the agent. A mark carries where it points as a selector and text from the page
// plus page pixels, so the agent can act on it without a screenshot. The frame is same-origin, which lets us read it.
const yqm = { on: false, tool: "arrow", cur: -1, marks: {}, draft: null, hide: 0 };
const YQM_KIND = { arrow: "arrow", circle: "circle", box: "box" };

function yqmKey() {
  const a = yqView.ask;
  return a ? a.key + "|" + yqView.page : "";
}

function yqmList() {
  const k = yqmKey();
  return k ? (yqm.marks[k] = yqm.marks[k] || []) : [];
}

function yqmFrame() {
  return document.getElementById("yq-frame");
}

function yqmDoc() {
  const f = yqmFrame();
  try { return f && f.contentDocument && f.contentWindow ? { doc: f.contentDocument, win: f.contentWindow } : null; } catch (e) { return null; }
}

function yqmScroll(d) {
  const s = d && (d.doc.scrollingElement || d.doc.documentElement);
  return { x: s ? s.scrollLeft : 0, y: s ? s.scrollTop : 0 };
}

// A selector that finds the element again: its id when it has one, else tag and position up to the nearest id.
function yqmSelector(el) {
  const parts = [];
  while (el && el.nodeType === 1 && el.tagName !== "BODY" && el.tagName !== "HTML") {
    let s = el.tagName.toLowerCase();
    if (el.id) { parts.unshift("#" + CSS.escape(el.id)); break; }
    const same = [...el.parentNode.children].filter(c => c.tagName === el.tagName);
    if (same.length > 1) s += ":nth-of-type(" + (same.indexOf(el) + 1) + ")";
    parts.unshift(s);
    el = el.parentElement;
  }
  return parts.join(" > ") || "body";
}

// What is under one page point: the element's selector and the words in it.
function yqmAt(d, x, y) {
  if (!d) return { sel: "", text: "" };
  const s = yqmScroll(d);
  const el = d.doc.elementFromPoint(x - s.x, y - s.y);
  if (!el) return { sel: "", text: "" };
  return { sel: yqmSelector(el), text: String(el.textContent || "").replace(/\s+/g, " ").trim().slice(0, 60) };
}

// The elements a circle or a box covers, sampled at its centre and around it.
function yqmHits(d, c) {
  const out = [], seen = {};
  const pts = [[0, 0], [-.6, 0], [.6, 0], [0, -.6], [0, .6], [-.45, -.45], [.45, -.45], [-.45, .45], [.45, .45]];
  for (const [dx, dy] of pts) {
    const h = yqmAt(d, c.x + dx * c.rx, c.y + dy * c.ry);
    if (h.sel && !seen[h.sel]) { seen[h.sel] = 1; out.push(h); }
  }
  const real = out.filter(h => h.sel !== "body");
  return (real.length ? real : out).slice(0, 4);
}

function yqmWhere(h) {
  return h && h.sel ? "`" + h.sel + "`" + (h.text ? ' "' + h.text + '"' : "") : "the page";
}

// The batch as the agent reads it. Pure: the marks hold everything it prints.
function yqmCompose(page, marks, general) {
  const lines = ["Marks on " + (page || "the page") + " (" + marks.length + "). x and y are page pixels from its top left."];
  marks.forEach((m, i) => {
    let head;
    if (m.kind === "arrow") head = "ARROW from (" + m.x1 + "," + m.y1 + ") on " + yqmWhere(m.from) + " to (" + m.x2 + "," + m.y2 + "), pointing at " + yqmWhere(m.at);
    else if (m.kind === "circle") head = "CIRCLE centre (" + m.x + "," + m.y + ") radius " + m.rx + "x" + m.ry + ", around " + (m.hits.length ? m.hits.map(yqmWhere).join(", ") : "the page");
    else head = "BOX at (" + (m.x - m.rx) + "," + (m.y - m.ry) + ") size " + m.rx * 2 + "x" + m.ry * 2 + ", around " + (m.hits.length ? m.hits.map(yqmWhere).join(", ") : "the page");
    lines.push((i + 1) + ". " + head + (m.text ? "\n   " + m.text.replace(/\n/g, "\n   ") : ""));
  });
  if (general) lines.push(general);
  return lines.join("\n");
}

// Where a mark's note opens, in the frame's own pixels: at the arrow's head, under a circle or a box.
function yqmAnchor(m, s) {
  if (m.kind === "arrow") return { x: m.x2 - s.x, y: m.y2 - s.y + 10 };
  return { x: m.x - m.rx - s.x, y: m.y + m.ry - s.y + 8 };
}

function yqmShape(m, i, s) {
  const on = i === yqm.cur ? " on" : "";
  const badge = (x, y) => `<g class="yqm-b${on}"><circle cx="${x}" cy="${y}" r="10"/><text x="${x}" y="${y + 4}" text-anchor="middle">${i + 1}</text></g>`;
  const hit = i < 0 ? "" : ` data-i="${i}"`;
  if (m.kind === "arrow") {
    const x1 = m.x1 - s.x, y1 = m.y1 - s.y, x2 = m.x2 - s.x, y2 = m.y2 - s.y;
    const a = Math.atan2(y2 - y1, x2 - x1), h = 14;
    const p = [[x2, y2], [x2 - h * Math.cos(a - .45), y2 - h * Math.sin(a - .45)], [x2 - h * Math.cos(a + .45), y2 - h * Math.sin(a + .45)]];
    return `<g class="yqm-s${on}"${hit}><line class="hit" x1="${x1}" y1="${y1}" x2="${x2}" y2="${y2}"/><line x1="${x1}" y1="${y1}" x2="${x2}" y2="${y2}"/><polygon points="${p.map(q => q.join(",")).join(" ")}"/></g>` + (i < 0 ? "" : badge(x1, y1));
  }
  const cx = m.x - s.x, cy = m.y - s.y;
  const shape = m.kind === "circle"
    ? `<ellipse cx="${cx}" cy="${cy}" rx="${m.rx}" ry="${m.ry}"/>`
    : `<rect x="${cx - m.rx}" y="${cy - m.ry}" width="${m.rx * 2}" height="${m.ry * 2}" rx="2"/>`;
  return `<g class="yqm-s${on}"${hit}>${shape.replace("/>", ' class="hit"/>')}${shape}</g>` + (i < 0 ? "" : badge(cx - m.rx, cy - m.ry));
}

function yqmDraftMark(dr) {
  const c = yqmCircle(dr);
  return dr.kind === "arrow" ? { kind: "arrow", x1: dr.x1, y1: dr.y1, x2: dr.x2, y2: dr.y2 } : Object.assign({ kind: dr.kind }, c);
}

function yqmCircle(dr) {
  return { x: Math.round((dr.x1 + dr.x2) / 2), y: Math.round((dr.y1 + dr.y2) / 2), rx: Math.max(6, Math.round(Math.abs(dr.x2 - dr.x1) / 2)), ry: Math.max(6, Math.round(Math.abs(dr.y2 - dr.y1) / 2)) };
}

function yqmPaintOverlay() {
  const ov = document.getElementById("yqm-ov");
  if (!ov) return;
  const s = yqmScroll(yqmDoc());
  ov.innerHTML = yqmList().map((m, i) => yqmShape(m, i, s)).join("") + (yqm.draft ? yqmShape(yqmDraftMark(yqm.draft), -1, { x: 0, y: 0 }) : "");
  const pop = document.getElementById("yqm-pop");
  if (pop && !pop.hidden) yqmPlacePop();
}

function yqmPlacePop() {
  const pop = document.getElementById("yqm-pop"), m = yqmList()[yqm.cur], wrap = document.getElementById("yqm-wrap");
  if (!pop || !m) return;
  const a = yqmAnchor(m, yqmScroll(yqmDoc()));
  pop.style.left = Math.max(4, Math.min(a.x, wrap.clientWidth - pop.offsetWidth - 4)) + "px";
  pop.style.top = Math.max(4, Math.min(a.y, wrap.clientHeight - pop.offsetHeight - 4)) + "px";
}

// The note beside its mark: shown on hover and right after drawing, editable in place.
function yqmShowPop(i, focus) {
  const pop = document.getElementById("yqm-pop"), m = yqmList()[i];
  clearTimeout(yqm.hide);
  if (!pop || !m) return;
  const same = !pop.hidden && yqm.cur === i;
  yqm.cur = i;
  if (same) { if (focus) pop.querySelector("textarea").focus(); return; }
  pop.innerHTML = `<div class="yqm-ph"><span class="yqm-n">${i + 1}</span><span>${YQM_KIND[m.kind]}</span>
    <button class="yqm-x" data-yq-do="mk-del" data-i="${i}" aria-label="remove mark ${i + 1}">&times;</button></div>
    <textarea class="yq-reply" rows="2" data-yqm-text="${i}" placeholder="Note on this ${YQM_KIND[m.kind]} (optional)">${esc(m.text)}</textarea>`;
  pop.hidden = false;
  yqmPlacePop();
  yqmPaintOverlay();
  if (focus) { const box = pop.querySelector("textarea"); if (box) box.focus(); }
}

function yqmHidePop(now) {
  clearTimeout(yqm.hide);
  const go = () => {
    const pop = document.getElementById("yqm-pop");
    if (!pop || pop.contains(document.activeElement)) return;
    pop.hidden = true;
    yqm.cur = -1;
    yqmPaintOverlay();
  };
  if (now) go(); else yqm.hide = setTimeout(go, 250);
}

function yqmPaintChrome() {
  const wrap = document.getElementById("yqm-wrap");
  if (!wrap) return;
  wrap.classList.toggle("mark", yqm.on);
  wrap.dataset.tool = yqm.tool;
  const tog = document.querySelector('[data-yq-do="mk-toggle"]');
  if (tog) { tog.classList.toggle("on", yqm.on); tog.textContent = yqm.on ? "Done marking" : "Mark up the page"; }
  document.querySelectorAll('[data-yq-do="mk-tool"]').forEach(b => b.classList.toggle("on", b.dataset.v === yqm.tool));
  const tools = document.getElementById("yqm-tools");
  if (tools) tools.hidden = !yqm.on;
  const n = yqmList().length;
  const send = document.getElementById("yqm-send");
  if (send) { send.hidden = !n; send.textContent = "Send " + n + " mark" + (n === 1 ? "" : "s") + " to the agent"; }
}

function yqmPaint() {
  const ov = document.getElementById("yqm-ov");
  if (!ov) return;
  yqmPaintChrome();
  yqmPaintOverlay();
  if (ov.__yqm) return;
  ov.__yqm = true;
  yqmWire(ov);
  const f = yqmFrame();
  const hook = () => {
    const d = yqmDoc();
    if (d) d.win.addEventListener("scroll", yqmPaintOverlay);
    yqmPaintOverlay();
  };
  f.addEventListener("load", hook);
  hook();
  if (typeof ResizeObserver === "function") new ResizeObserver(yqmPaintOverlay).observe(f);
  const pop = document.getElementById("yqm-pop");
  pop.addEventListener("pointerenter", () => clearTimeout(yqm.hide));
  pop.addEventListener("pointerleave", () => yqmHidePop());
  pop.addEventListener("focusout", () => yqmHidePop());
}

function yqmPoint(ov, e) {
  const r = ov.getBoundingClientRect(), s = yqmScroll(yqmDoc());
  return { x: Math.round(e.clientX - r.left + s.x), y: Math.round(e.clientY - r.top + s.y) };
}

function yqmWire(ov) {
  ov.addEventListener("pointerover", e => {
    const g = e.target.closest && e.target.closest(".yqm-s");
    if (g && !yqm.draft) yqmShowPop(Number(g.dataset.i), false);
  });
  ov.addEventListener("pointerout", e => {
    if (e.target.closest && e.target.closest(".yqm-s") && !yqm.draft) yqmHidePop();
  });
  ov.addEventListener("pointerdown", e => {
    if (!yqm.on || e.button) return;
    const p = yqmPoint(ov, e);
    yqm.draft = { kind: yqm.tool, x1: p.x, y1: p.y, x2: p.x, y2: p.y, id: e.pointerId };
    yqmHidePop(true);
    ov.setPointerCapture(e.pointerId);
  });
  ov.addEventListener("pointermove", e => {
    const dr = yqm.draft;
    if (!dr || dr.id !== e.pointerId) return;
    const p = yqmPoint(ov, e);
    dr.x2 = p.x;
    dr.y2 = p.y;
    yqmPaintOverlay();
  });
  const end = e => {
    const dr = yqm.draft;
    if (!dr || dr.id !== e.pointerId) return;
    yqm.draft = null;
    if (ov.hasPointerCapture && ov.hasPointerCapture(e.pointerId)) ov.releasePointerCapture(e.pointerId);
    if (e.type === "pointercancel") return yqmPaintOverlay();
    yqmCommit(dr);
  };
  ov.addEventListener("pointerup", end);
  ov.addEventListener("pointercancel", end);
}

function yqmCommit(dr) {
  if (Math.hypot(dr.x2 - dr.x1, dr.y2 - dr.y1) < 12) return yqmPaintOverlay();
  const d = yqmDoc();
  let m;
  if (dr.kind === "arrow") m = { kind: "arrow", x1: dr.x1, y1: dr.y1, x2: dr.x2, y2: dr.y2, from: yqmAt(d, dr.x1, dr.y1), at: yqmAt(d, dr.x2, dr.y2), text: "" };
  else { const c = yqmCircle(dr); m = Object.assign({ kind: dr.kind, hits: yqmHits(d, c), text: "" }, c); }
  const list = yqmList();
  list.push(m);
  yqmPaintChrome();
  yqmShowPop(list.length - 1, true);
}

async function yqmClick(b, d) {
  if (d === "mk-toggle") { yqm.on = !yqm.on; return yqmPaintChrome(); }
  if (d === "mk-tool") { yqm.tool = b.dataset.v; return yqmPaintChrome(); }
  if (d === "mk-del") {
    yqmList().splice(Number(b.dataset.i), 1);
    yqmHidePop(true);
    yqmPaintChrome();
    return yqmPaintOverlay();
  }
  if (d === "mk-send") {
    const t = yqCard(), a = yqView.ask, marks = yqmList();
    if (!t || !marks.length) return;
    const page = a.pages && a.pages[yqView.page] ? a.pages[yqView.page] : "";
    if (await yqSay(t.id, yqmCompose(page, marks, yqBoxVal("p:" + t.id)), marks.length + " mark" + (marks.length === 1 ? "" : "s") + " sent")) {
      yqm.marks[yqmKey()] = [];
      yqm.cur = -1;
      yqm.on = false;
      yqClearBox("p:" + t.id);
      yqPaintAsk(true);
    }
  }
}

document.addEventListener("input", e => {
  const i = e.target.dataset && e.target.dataset.yqmText;
  if (i !== undefined && yqmList()[Number(i)]) yqmList()[Number(i)].text = e.target.value;
});
