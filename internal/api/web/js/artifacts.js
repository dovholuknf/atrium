// What this session made, as a slide-out beside the terminal: the files view's list, ticks, sort, keys and viewer, over the
// card's artifact list (GET /v1/tasks/{id}/artifacts). Files open through the same read.html frame, the same editor and
// the same download routes as the files drawer in js/notes-files.js, so containment stays that view's own.

const ART_KINDS = [
  { k: "file", label: "files" }, { k: "report", label: "reports" }, { k: "pub", label: "published" },
  { k: "img", label: "images" }, { k: "bin", label: "builds" }, { k: "git", label: "git" }, { k: "link", label: "links" }
];
const ART_FOLD_KEY = "atrium.arts.collapsed";
const ART_GROUP_KEY = "atrium.arts.group";

const arts = { id: "", open: false, list: [], shown: [], sort: { key: "when", desc: true }, only: "", q: "", cur: 0,
  viewing: null, raw: false, picked: new Set(), group: localStorage.getItem(ART_GROUP_KEY) !== "0", timer: 0,
  collapsed: new Set() };
try { arts.collapsed = new Set(JSON.parse(localStorage.getItem(ART_FOLD_KEY) || "[]")); } catch (e) {}

const artHead = x => !!(x && x.head);
const artRows = () => arts.shown.filter(x => !artHead(x));
const artMd = a => /\.(md|markdown)$/i.test(a.name) && a.tag !== "gone";
const artOnDisk = a => !!a.path && a.tag !== "gone" && (a.kind === "file" || a.kind === "report" || a.kind === "img" || a.kind === "bin");
const artEditable = a => artOnDisk(a) && (a.kind === "file" || a.kind === "report") && editableHere(a);

const ART_ICONS = {
  back: '<path d="M13.5 8H2.5M7 3.5 2.5 8 7 12.5"/>', prev: '<path d="M10 3 5 8l5 5"/>', next: '<path d="M6 3l5 5-5 5"/>',
  edit: '<path d="M10.8 2.6a1.5 1.5 0 0 1 2.1 2.1L5.6 12 2.6 13.4 4 10.4Z"/><path d="M9.6 3.8l2.6 2.6"/>',
  tab: '<path d="M9.5 2.5h4v4M13.5 2.5 7.5 8.5"/><path d="M12 9.5v3a1 1 0 0 1-1 1H3.5a1 1 0 0 1-1-1V5a1 1 0 0 1 1-1h3"/>',
  get: '<path d="M8 2.5v8M4.5 7 8 10.5 11.5 7"/><path d="M2.5 11.5v1a1 1 0 0 0 1 1h9a1 1 0 0 0 1-1v-1"/>',
  copy: '<rect x="5.5" y="5.5" width="8" height="8" rx="1.2"/><path d="M10.5 5.5V3.5a1 1 0 0 0-1-1h-6a1 1 0 0 0-1 1v6a1 1 0 0 0 1 1h2"/>',
  walk: '<circle cx="4" cy="4" r="1.6"/><circle cx="12" cy="12" r="1.6"/><path d="M4 5.6V9a3 3 0 0 0 3 3h3.4M12 10.4V7a3 3 0 0 0-3-3H5.6"/>',
  close: '<path d="M4 4l8 8M12 4l-8 8"/>', group: '<path d="M2.5 3.5h11M2.5 8h11M2.5 12.5h11"/><path d="M1 3.5h.01M1 8h.01M1 12.5h.01" stroke-width="2.4"/>',
  unpick: '<rect x="2.5" y="2.5" width="11" height="11" rx="1.5"/><path d="M5.5 8h5"/>',
  globe: '<circle cx="8" cy="8" r="6"/><path d="M2 8h12M8 2c1.8 1.7 2.6 3.7 2.6 6S9.8 12.3 8 14c-1.8-1.7-2.6-3.7-2.6-6S6.2 3.7 8 2Z"/>',
  rendered: '<path d="M1.5 8s2.4-4.5 6.5-4.5S14.5 8 14.5 8 12.1 12.5 8 12.5 1.5 8 1.5 8Z"/><circle cx="8" cy="8" r="1.9"/>',
  raw: '<path d="M5.5 4.5 2 8l3.5 3.5M10.5 4.5 14 8l-3.5 3.5"/>', caret: '<path d="M4 6l4 4 4-4"/>'
};
const artIc = k => `<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${ART_ICONS[k]}</svg>`;
const artBtn = (k, tip, attrs, word) =>
  `<button type="button" class="ab${word ? " w" : ""}" aria-label="${esc(word || tip)}" data-tip="${esc(tip)}" ${attrs || ""}>${artIc(k)}${word ? esc(word) : ""}</button>`;

// The server's rows as the list draws them. A publish of a file the card also lists rides on that file's row.
function artItems(rows, root) {
  const out = rows.map(r => {
    let x = {};
    try { x = JSON.parse(r.extra || "{}") || {}; } catch (e) {}
    const name = r.label || String(r.ref).split(/[\\/]/).pop() || r.ref;
    const a = { kind: r.kind, name, path: r.kind === "link" ? "" : r.ref, href: r.kind === "link" || r.kind === "pub" ? x.href || r.ref : "",
      at: Date.parse(r.last_at) || 0, size: r.bytes || null, where: x.where || "", from: x.from || "", sha: x.sha || "", ref: x.ref || "" };
    if (x.change === "new" || x.change === "edited") { a.tag = x.change; a.add = x.add || 0; a.del = x.del || 0; }
    if (x.change === "deleted") a.tag = "gone";
    a.abs = a.path && !/^([a-z]:)?\//i.test(a.path) && root ? root.replace(/\/+$/, "") + "/" + a.path : a.path;
    return a;
  });
  return out.filter(p => {
    if (p.kind !== "pub" || !p.from) return true;
    const src = out.find(a => a.path === p.from && a.kind !== "pub");
    if (!src) return true;
    src.pub = { href: p.href, where: p.where, at: p.at };
    return false;
  });
}

function artWhere(a) {
  if (a.kind === "git") return [a.sha, a.where].filter(Boolean).join(" on ");
  if (a.kind === "link") return a.href.replace(/^https?:\/\//, "");
  if (a.where) return a.where;
  const p = a.path || "";
  return p.slice(0, p.length - a.name.length).replace(/\/$/, "") || "the card's folder";
}

function artBadge(a) {
  const pub = a.pub ? `<span class="tag pubd" data-tip="${esc("published to " + a.pub.where + " " + firstSeen(new Date(a.pub.at).toISOString()) + ", " + a.pub.href)}">published</span>` : "";
  if (a.tag === "new") return pub + `<span class="tag new">new</span>${a.add ? `<span class="delta"><span class="p">+${a.add}</span></span>` : ""}`;
  if (a.tag === "edited") return pub + `<span class="delta"><span class="p">+${a.add}</span> <span class="m">&minus;${a.del}</span></span>`;
  if (a.tag === "gone") return pub + `<span class="tag gone">deleted</span>`;
  return pub;
}

function artIcon(a) {
  if (a.kind === "file" || a.kind === "img") return fileIcon({ name: a.name });
  const svg = d => `<svg class="ficon ${a.kind}" viewBox="0 0 16 16" width="13" height="13" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linejoin="round" stroke-linecap="round">${d}</svg>`;
  if (a.kind === "report") return fileIcon({ name: a.name }).replace('class="ficon', 'class="ficon rep');
  if (a.kind === "pub") return svg(ART_ICONS.globe);
  if (a.kind === "bin") return svg('<path d="M8 1.8 13.6 5v6L8 14.2 2.4 11V5Z"/><path d="M2.4 5 8 8.2 13.6 5M8 8.2v6"/>');
  if (a.kind === "git") return svg(ART_ICONS.walk);
  return svg('<path d="M6.6 9.4a2.6 2.6 0 0 0 3.7 0l2.4-2.4a2.6 2.6 0 0 0-3.7-3.7l-.7.7"/><path d="M9.4 6.6a2.6 2.6 0 0 0-3.7 0L3.3 9a2.6 2.6 0 0 0 3.7 3.7l.7-.7"/>');
}

// One list of actions per kind, drawn the same way on a row and in the file header.
function artActs(a) {
  if (a.tag === "gone") return [];
  if (a.kind === "link") return [["tab", "open it in a new tab", "open"], ["copy", "copy the link", "copy"]];
  if (a.kind === "git") return [["walk", "walk its changes beside the terminal", "walk"], ["copy", "copy the sha", "copy"]];
  if (a.kind === "pub") return [["tab", "the published copy, in a new tab", "open"], ["copy", "copy the link it was published at", "copy"]];
  const out = [];
  if (artEditable(a)) out.push(["edit", "change it here, in this browser", "edit"]);
  if (a.kind !== "bin") out.push(["tab", "read it in its own browser tab", "tab"]);
  if (a.pub) out.push(["globe", "the published copy on " + a.pub.where + ", in a new tab", "pubopen"]);
  out.push(["get", "save it to this computer", "get"]);
  if (a.kind === "bin") out.push(["copy", "copy its full path", "copy"]);
  return out;
}
const artActButtons = a => artActs(a).map(([k, tip, act]) => artBtn(k, tip, `data-act="${act}"`)).join("");

const ART_SORTS = {
  name: (x, y) => x.name.localeCompare(y.name), where: (x, y) => artWhere(x).localeCompare(artWhere(y)),
  size: (x, y) => (x.size || 0) - (y.size || 0), when: (x, y) => x.at - y.at
};

function artShownRows() {
  const q = arts.q.toLowerCase();
  const rows = arts.list.filter(a => (!arts.only || a.kind === arts.only) && (!q || (a.name + " " + (a.path || a.href || "")).toLowerCase().includes(q)));
  const f = ART_SORTS[arts.sort.key];
  rows.sort((x, y) => (arts.sort.desc ? -1 : 1) * f(x, y));
  if (arts.group) rows.sort((x, y) => ART_KINDS.findIndex(k => k.k === x.kind) - ART_KINDS.findIndex(k => k.k === y.kind));
  return rows;
}

function artSetCollapsed(k, on) {
  if (on) arts.collapsed.add(k); else arts.collapsed.delete(k);
  try { localStorage.setItem(ART_FOLD_KEY, JSON.stringify([...arts.collapsed])); } catch (e) {}
}

function artPaintKinds() {
  const all = arts.list;
  const n = k => all.filter(a => a.kind === k).length;
  const kinds = document.getElementById("t-arts-kinds");
  setHTML(kinds, `<button type="button" data-only=""${!arts.only ? ' class="on"' : ""}>all<i class="pillcount">${all.length}</i></button>` +
    ART_KINDS.filter(k => n(k.k)).map(k => `<button type="button" data-only="${k.k}"${arts.only === k.k ? ' class="on"' : ""}>${k.label}<i class="pillcount">${n(k.k)}</i></button>`).join(""));
  kinds.querySelectorAll("button").forEach(b => b.onclick = () => { arts.only = b.dataset.only; arts.cur = 0; artPaint(); });
  const count = all.length ? String(all.length) : "";
  document.getElementById("t-arts-count").textContent = count;
  document.getElementById("t-arts-n").textContent = count;
  document.getElementById("t-arts-group").setAttribute("aria-pressed", String(arts.group));
}

function artHeadRow() {
  const b = (k, label, right) => `<button type="button" class="fsort${right ? " r" : ""}${arts.sort.key === k ? " on" : ""}"
    data-sort="${k}" data-tip="sort by ${label}">${label}${arts.sort.key === k ? `<i>${arts.sort.desc ? "&#9662;" : "&#9652;"}</i>` : ""}</button>`;
  return `<div class="frow fhead"><input type="checkbox" class="fpickall" aria-label="pick every one shown" data-tip="pick every one shown">${
    b("name", "name")}${b("where", "where")}${b("size", "size", true)}${b("when", "when", true)}<span class="hacts"></span></div>`;
}

function artPaintBulk() {
  const host = document.getElementById("t-arts-bulk");
  const picked = arts.list.filter(a => arts.picked.has(a));
  if (!picked.length) { setHTML(host, ""); return; }
  const n = picked.length;
  setHTML(host, artBtn("get", picked.map(a => a.path).join("\n"), 'data-bulk="get"', n === 1 ? "download" : `download ${n} as a zip`) +
    artBtn("copy", "copy their paths, one a line", 'data-bulk="copy"', n === 1 ? "copy path" : `copy ${n} paths`) +
    artBtn("unpick", "untick them all", 'data-bulk="none"'));
  host.querySelectorAll("[data-bulk]").forEach(b => b.onclick = () => {
    if (b.dataset.bulk === "none") { arts.picked.clear(); artPaint(); return; }
    if (b.dataset.bulk === "copy") { artCopy(picked.map(a => a.abs).join("\n"), b); return; }
    artDownload(picked);
  });
}

function artDownload(list) {
  const url = list.length === 1 ? `/v1/tasks/${arts.id}/files?path=${encodeURIComponent(list[0].path)}`
    : `/v1/tasks/${arts.id}/files/zip?` + list.map(a => "path=" + encodeURIComponent(a.path)).join("&");
  const a = document.createElement("a");
  a.href = url;
  a.download = "";
  a.click();
}

function artPaint() {
  artPaintKinds();
  artPaintBulk();
  const host = document.getElementById("t-arts-list");
  const panel = document.getElementById("t-arts-panel");
  panel.classList.toggle("empty", !arts.list.length);
  if (!arts.list.length) {
    arts.shown = [];
    setHTML(host, `<div class="fstate"><span class="big">nothing made yet</span></div>`);
    document.getElementById("t-arts-qn").textContent = "";
    return;
  }
  const rows = artShownRows();
  document.getElementById("t-arts-qn").textContent = arts.q ? `${rows.length} of ${arts.list.length}` : "";
  if (!rows.length) {
    arts.shown = [];
    setHTML(host, artHeadRow() + `<div class="fstate">nothing here is called “${esc(arts.q)}”<span class="facts-row">${
      artBtn("close", "clear the filter", "data-clearq", "clear the filter")}</span></div>`);
    host.querySelector("[data-clearq]").onclick = () => { artSetQ(""); artPaint(); };
    artWireHead(host, []);
    return;
  }
  const items = [];
  let last = null;
  rows.forEach(a => {
    if (arts.group && a.kind !== last) { items.push({ head: a.kind, n: rows.filter(r => r.kind === a.kind).length }); last = a.kind; }
    if (!arts.group || arts.q || !arts.collapsed.has(a.kind)) items.push(a);
  });
  arts.shown = items;
  if (arts.cur >= items.length) arts.cur = items.length - 1;
  let html = artHeadRow();
  items.forEach((a, i) => {
    if (artHead(a)) {
      const k = ART_KINDS.find(x => x.k === a.head);
      const shut = arts.collapsed.has(a.head) && !arts.q;
      html += `<div class="frow agroup${shut ? " shut" : ""}${i === arts.cur ? " cur" : ""}" role="option" aria-expanded="${!shut}" data-i="${i}"
        data-tip="${shut ? "show" : "hide"} the ${k.label} (Enter or ${shut ? "→" : "←"})">${artIc("caret")}${k.label}<span class="n">${a.n}</span></div>`;
      return;
    }
    html += `<div class="frow${i === arts.cur ? " cur" : ""}${arts.viewing === a ? " open" : ""}${a.tag === "gone" ? " gone" : ""}" role="option" data-i="${i}">
      <input type="checkbox" class="fpick" tabindex="-1" aria-label="pick ${esc(a.name)}"${artOnDisk(a) ? "" : " disabled"}${arts.picked.has(a) ? " checked" : ""}>
      <span class="fname"${a.name.length > 34 ? ` data-tip="${esc(a.name)}"` : ""}>${artIcon(a)}<span class="fn">${esc(a.name)}</span>${artBadge(a)}</span>
      <span class="aw" data-tip="${esc(a.abs || a.href || artWhere(a))}"><bdi>${esc(artWhere(a))}</bdi></span>
      <span class="fsize">${esc(a.size == null ? "" : bytes(a.size))}</span>
      <span class="fwhen">${esc(firstSeen(new Date(a.at).toISOString()))}</span>
      <span class="facts">${artActButtons(a)}</span>
    </div>`;
  });
  setHTML(host, html);
  artWireHead(host, artRows());
  host.querySelectorAll(".frow[data-i]").forEach(el => {
    el.onclick = ev => {
      if (ev.target.closest(".fpick")) return;
      arts.cur = Number(el.dataset.i);
      const act = ev.target.closest("[data-act]");
      if (act) { artAct(arts.shown[arts.cur], act.dataset.act, act); return; }
      artOpenRow(arts.cur);
    };
  });
  host.querySelectorAll(".fpick").forEach(cb => {
    cb.onclick = ev => ev.stopPropagation();
    cb.onchange = () => { const a = arts.shown[Number(cb.closest(".frow").dataset.i)]; if (cb.checked) arts.picked.add(a); else arts.picked.delete(a); artPaint(); };
  });
  const cur = host.querySelector(".frow.cur");
  if (cur) cur.scrollIntoView({ block: "nearest" });
}

function artWireHead(host, rows) {
  host.querySelectorAll(".fsort").forEach(b => b.onclick = () => {
    const k = b.dataset.sort;
    arts.sort = arts.sort.key === k ? { key: k, desc: !arts.sort.desc } : { key: k, desc: k === "when" || k === "size" };
    artPaint();
  });
  const all = host.querySelector(".fpickall");
  if (!all) return;
  const can = rows.filter(artOnDisk);
  const n = can.filter(a => arts.picked.has(a)).length;
  all.checked = can.length > 0 && n === can.length;
  all.indeterminate = n > 0 && n < can.length;
  all.onchange = () => { can.forEach(a => { if (all.checked) arts.picked.add(a); else arts.picked.delete(a); }); artPaint(); };
}

function artSetQ(q) {
  arts.q = q;
  document.getElementById("t-arts-q").value = q;
}

function artCopy(text, el) {
  let ok = true;
  const tip = el.dataset.tip;
  (navigator.clipboard ? navigator.clipboard.writeText(text) : Promise.reject()).catch(() => { ok = false; }).then(() => {
    el.classList.toggle("done", ok);
    el.dataset.tip = ok ? "copied" : "could not copy";
    setTimeout(() => { el.classList.remove("done"); el.dataset.tip = tip; }, 1200);
  });
}

function artAct(a, act, el) {
  if (act === "copy") return artCopy(a.href || a.sha || a.abs || a.name, el);
  if (act === "pubopen") return void window.open(a.pub.href, "_blank", "noopener");
  if (act === "open") return void window.open(a.href, "_blank", "noopener");
  if (act === "tab") return openFileTabFor(arts.id, a.path);
  if (act === "get") return artDownload([a]);
  if (act === "walk") { artSetOpen(false); if (typeof toggleWalk === "function") toggleWalk(); return; }
  if (act === "edit") return artEdit(a);
}

// The editor is the files drawer's: this opens that drawer on the file and leaves the way a click on a path in the terminal does.
function artEdit(a) {
  artSetOpen(false);
  termFilesAuto = document.getElementById("t-files-panel").hidden;
  setTermFiles(true);
  const cut = a.path.lastIndexOf("/");
  loadFiles(cut > 0 ? a.path.slice(0, cut) : "", termFilesCtx());
  openEditor(arts.id, a.path);
}

function artToggleGroup(k, shut) {
  if (arts.q) return;
  artSetCollapsed(k, shut == null ? !arts.collapsed.has(k) : shut);
  if (arts.viewing && arts.collapsed.has(arts.viewing.kind)) artCloseView();
  artPaint();
  arts.cur = arts.shown.findIndex(x => artHead(x) && x.head === k);
  artPaint();
}

function artOpenRow(i) {
  const a = arts.shown[i];
  if (!a) return;
  arts.cur = i;
  if (artHead(a)) { artToggleGroup(a.head); return; }
  if (!artOnDisk(a)) {
    const first = artActs(a)[0];
    if (first) artAct(a, first[2], document.createElement("button"));
    return;
  }
  arts.viewing = a;
  document.getElementById("t-aview").hidden = false;
  document.getElementById("t-arts-panel").classList.add("viewing");
  document.getElementById("t-aview-frame").contentWindow.location.replace("/read.html?embed#" + encodeURIComponent(arts.id) + "/" + encodeURIComponent(a.path));
  artPaintViewBar();
  artPaint();
}

function artStep(by) {
  for (let i = arts.cur + by; i >= 0 && i < arts.shown.length; i += by) if (artOnDisk(arts.shown[i])) { artOpenRow(i); return; }
}

// The rendered page is the default for markdown. Raw clicks the frame's own Source button, which its header would hold.
function artApplyRaw() {
  const a = arts.viewing;
  if (!a || !artMd(a)) return;
  let doc = null;
  try { doc = document.getElementById("t-aview-frame").contentDocument; } catch (e) {}
  const btn = doc && doc.getElementById("raw-md"), text = doc && doc.getElementById("text");
  if (btn && !btn.hidden && text && text.hidden === arts.raw) btn.click();
}

function artPaintViewBar() {
  const a = arts.viewing;
  const bar = document.getElementById("t-aview-bar");
  const vis = artRows().filter(artOnDisk), at = vis.indexOf(a);
  const rel = a.path;
  const pop = `<span class="fpathpop">${[["absolute", a.abs], ["in the card", rel]].filter(([, p]) => p).map(([l, p]) =>
    `<span class="fpl">${l}</span><code>${esc(p)}</code>${artBtn("copy", "copy", `data-copy="${esc(p)}"`)}`).join("")}</span>`;
  const meta = [a.size != null ? bytes(a.size) : "", firstSeen(new Date(a.at).toISOString())].filter(Boolean).join(", ");
  const acts = artActs(a).map(([k, tip, act]) => artBtn(k, tip, `data-v="${act}"`)).join("");
  setHTML(bar, artBtn("back", "back to the list (Esc)", 'data-v="back"') +
    `<span class="awhere"><code tabindex="0">${esc(a.name)}</code>${pop}<span class="by">${esc(meta)}</span></span>` +
    `<span class="abgroup">${artBtn("prev", "previous (←)", `data-v="prev"${at <= 0 ? " disabled" : ""}`)}${
      artBtn("next", "next (→)", `data-v="next"${at >= vis.length - 1 ? " disabled" : ""}`)}</span>` +
    (artMd(a) ? `<span class="absep"></span><span class="abgroup">${artBtn("rendered", "drawn as a page", `data-v="rendered" aria-pressed="${!arts.raw}"`, "rendered")}${
      artBtn("raw", "the markdown as written", `data-v="raw" aria-pressed="${arts.raw}"`, "raw")}</span>` : "") +
    (acts ? `<span class="absep"></span><span class="abgroup">${acts}</span>` : ""));
  bar.querySelectorAll("[data-v]").forEach(b => b.onclick = () => {
    const v = b.dataset.v;
    if (v === "back") return artCloseView();
    if (v === "prev") return artStep(-1);
    if (v === "next") return artStep(1);
    if (v === "rendered" || v === "raw") { arts.raw = v === "raw"; artApplyRaw(); artPaintViewBar(); return; }
    artAct(a, v, b);
  });
  bar.querySelectorAll("[data-copy]").forEach(b => b.onclick = () => artCopy(b.dataset.copy, b));
}

function artCloseView() {
  if (!arts.viewing) return;
  arts.viewing = null;
  document.getElementById("t-aview").hidden = true;
  document.getElementById("t-aview-frame").contentWindow.location.replace("about:blank");
  document.getElementById("t-arts-panel").classList.remove("viewing");
  artPaint();
  document.getElementById("t-arts-list").focus();
}

async function artLoad() {
  const id = termTask && termTask.id;
  if (!id) return;
  let rows;
  try { rows = await api(`/v1/tasks/${id}/artifacts`); } catch (e) { return; }
  if (!termTask || termTask.id !== id) return;
  if (arts.id !== id) { arts.picked.clear(); arts.viewing = null; arts.cur = 0; arts.id = id; }
  const had = arts.list.map(a => a.kind + a.path + a.href + a.at).join("|");
  arts.list = artItems(Array.isArray(rows) ? rows : [], termTask.worktree || "");
  if (arts.list.length && arts.list.map(a => a.kind + a.path + a.href + a.at).join("|") === had && arts.open) return;
  arts.picked = new Set([...arts.picked].filter(a => arts.list.some(b => b.kind === a.kind && b.path === a.path)).map(a => arts.list.find(b => b.kind === a.kind && b.path === a.path)));
  if (arts.viewing) arts.viewing = arts.list.find(b => b.kind === arts.viewing.kind && b.path === arts.viewing.path) || null;
  artPaint();
}

function artSetOpen(on) {
  arts.open = on;
  const panel = document.getElementById("t-arts-panel");
  const bar = panel.parentNode.querySelector(".term-bar");
  if (bar) panel.style.setProperty("--arts-top", bar.offsetHeight + "px");
  panel.hidden = !on;
  document.getElementById("t-arts").setAttribute("aria-pressed", on ? "true" : "false");
  if (on) { artLoad(); setTimeout(() => document.getElementById("t-arts-list").focus(), 50); }
  else if (arts.viewing) artCloseView();
}

function toggleArts() {
  if (!arts.open && !termTask) return;
  artSetOpen(!arts.open);
}

// The count on the button follows the card on screen, and the open panel follows it as it works.
let artsBeat = 0;
setInterval(() => {
  if (!termTask) { if (arts.open) artSetOpen(false); return; }
  if (document.visibilityState !== "visible") return;
  artsBeat++;
  if (arts.open || artsBeat % 6 === 0) artLoad();
}, 5000);

(function wireArts() {
  const $ = id => document.getElementById(id);
  if (!$("t-arts-panel")) return;
  setHTML($("t-arts-group"), artIc("group"));
  setHTML($("t-arts-close"), artIc("close"));
  $("t-arts-close").onclick = () => artSetOpen(false);
  $("t-arts-group").onclick = () => { arts.group = !arts.group; try { localStorage.setItem(ART_GROUP_KEY, arts.group ? "1" : "0"); } catch (e) {} artPaint(); };
  $("t-arts-q").oninput = e => { arts.q = e.target.value; arts.cur = 0; artPaint(); };
  $("t-arts-q").onkeydown = e => {
    if (e.key !== "ArrowDown" && e.key !== "Enter") return;
    e.preventDefault();
    $("t-arts-list").focus();
    if (e.key === "Enter") artOpenRow(Math.max(0, arts.cur));
  };
  $("t-aview-frame").addEventListener("load", () => {
    let doc = null;
    try { doc = $("t-aview-frame").contentDocument; } catch (e) {}
    if (!doc || !doc.body || !arts.viewing) return;
    paintFrameTheme(doc);
    doc.addEventListener("keydown", artKey);
    artApplyRaw();
  });
})();

function artKey(e) {
  if (!arts.open) return;
  const t = e.target;
  if (e.key === "Escape") {
    e.preventDefault();
    if (arts.viewing) artCloseView();
    else if (arts.q) { artSetQ(""); artPaint(); document.getElementById("t-arts-list").focus(); }
    else { artSetOpen(false); if (typeof term !== "undefined" && term) term.focus(); }
    return;
  }
  const inFrame = t && t.ownerDocument && t.ownerDocument !== document;
  if (!inFrame && !(t && document.getElementById("t-arts-panel").contains(t))) return;
  if (t && /^(INPUT|TEXTAREA|SELECT)$/.test(t.tagName) && t.type !== "checkbox") return;
  if (e.ctrlKey || e.metaKey || e.altKey) return;
  if (arts.viewing && (e.key === "ArrowLeft" || e.key === "ArrowRight")) { e.preventDefault(); artStep(e.key === "ArrowLeft" ? -1 : 1); return; }
  if (!arts.viewing && arts.group && (e.key === "ArrowLeft" || e.key === "ArrowRight")) {
    e.preventDefault();
    const x = arts.shown[arts.cur];
    if (x) artToggleGroup(artHead(x) ? x.head : x.kind, e.key === "ArrowLeft");
    return;
  }
  const page = Math.max(1, Math.floor(document.getElementById("t-arts-list").clientHeight / 30) - 1);
  const go = to => {
    arts.cur = Math.max(0, Math.min(arts.shown.length - 1, to));
    if (arts.viewing && artOnDisk(arts.shown[arts.cur])) artOpenRow(arts.cur); else artPaint();
  };
  switch (e.key) {
    case "ArrowDown": e.preventDefault(); go(arts.cur + 1); break;
    case "ArrowUp": e.preventDefault(); go(arts.cur - 1); break;
    case "PageDown": e.preventDefault(); go(arts.cur + page); break;
    case "PageUp": e.preventDefault(); go(arts.cur - page); break;
    case "Home": e.preventDefault(); go(0); break;
    case "End": e.preventDefault(); go(arts.shown.length - 1); break;
    case "Enter": e.preventDefault(); artOpenRow(arts.cur); break;
    case " ": {
      e.preventDefault();
      const a = arts.shown[arts.cur];
      if (artHead(a)) { artToggleGroup(a.head); break; }
      if (a && artOnDisk(a)) { if (arts.picked.has(a)) arts.picked.delete(a); else arts.picked.add(a); artPaint(); }
      break;
    }
    case "/": e.preventDefault(); document.getElementById("t-arts-q").focus(); document.getElementById("t-arts-q").select(); break;
  }
}
document.addEventListener("keydown", artKey);
