// The page in `read.html`: one card's file, in a tab, with markdown rendered.
//
// THE DOCUMENT IS AN AGENT'S, SO IT IS HOSTILE UNTIL SANITISED. Three layers, any
// one of which would be enough for the common cases and none of which is trusted
// alone:
//
//  1. Raw HTML in the markdown is ESCAPED by the renderer, so `<script>` is shown
//     as the characters it is. marked never hands it to the parser as markup.
//  2. DOMPurify takes out whatever is left that can run: script, event handlers,
//     `javascript:` and other non-web URLs, forms, frames.
//  3. The page's CSP has no inline script and no remote source, so a hole in 1 and 2
//     is still not script, and a remote image cannot phone anywhere.
//
// Links and images are rewritten AFTER sanitising, on the DOM, so what is
// rewritten is what is shown. A relative target resolves against the document's
// folder to the same card and opens in this viewer. Remote images are not loaded.

// `mdEscape` is the renderer's answer for raw HTML.
function mdEscape(s) {
  return String(s == null ? "" : s)
    .replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;");
}

// A page served by a public document share carries the snapshot's path and image map on its body, and asks nothing of /v1.
const SHARE = (() => {
  const b = typeof document !== "undefined" ? document.body : null;
  if (!b || b.dataset.source !== "share") return null;
  try { return JSON.parse(b.dataset.share || "{}"); } catch (e) { return {}; }
})();

// The images the last render drew, as card paths, which "Share publicly" sends so the room copies exactly those.
let readImages = [];

// Where a card's file is streamed from. In a shared page: the snapshot's own address for it, or "" when it is not in it.
function viewURL(id, path) {
  if (SHARE) {
    const imgs = SHARE.images || {};
    if (Object.prototype.hasOwnProperty.call(imgs, path)) return "/f/" + imgs[path];
    return path === SHARE.path ? "/doc.md" : "";
  }
  return "/v1/tasks/" + encodeURIComponent(id) + "/files/view?path=" + encodeURIComponent(path);
}

function isMarkdown(path) { return /\.(md|markdown)$/i.test(path); }

function isHTML(path) { return /\.html?$/i.test(path); }

// Beside the empty sandbox, so a rendered page cannot even ask the room for another of the card's files.
const HTML_CSP = "default-src 'none'; style-src 'unsafe-inline'; img-src data:; font-src data:";

function sandboxedHTML(body) {
  const meta = '<meta http-equiv="Content-Security-Policy" content="' + HTML_CSP + '">';
  // After the doctype, so a standards-mode page stays one.
  const m = /^﻿?\s*<!doctype[^>]*>/i.exec(body);
  return m ? m[0] + meta + body.slice(m[0].length) : meta + body;
}

// The source and the rendered page, one at a time, with `?render` in the address saying which.
function htmlMain(body) {
  const text = document.getElementById("text");
  const btn = document.getElementById("html-mode");
  // Made here rather than in the markup, with the sandbox set before it has anything to load.
  const frame = document.createElement("iframe");
  frame.id = "html";
  frame.className = "pdf page";
  frame.setAttribute("sandbox", "");
  frame.setAttribute("aria-label", "the page, rendered");
  frame.hidden = true;
  text.textContent = body;
  const show = render => {
    if (render && !frame.srcdoc) {
      frame.srcdoc = sandboxedHTML(body);
      text.after(frame);
    }
    frame.hidden = !render;
    text.hidden = render;
    btn.textContent = render ? "Show the source" : "Show it rendered";
    history.replaceState(null, "", (render ? "?render" : location.pathname) + location.hash);
  };
  btn.hidden = false;
  btn.onclick = () => show(frame.hidden);
  show(/(^|&)render(=|&|$)/.test(location.search.slice(1)));
}

// How the viewer shows a file, by its name. The image list is the view route's `viewImageTypes`.
function readKind(path) {
  if (/\.(png|jpe?g|gif|webp|bmp|ico|avif)$/i.test(path)) return "image";
  if (/\.svg$/i.test(path)) return "svg";
  if (/\.pdf$/i.test(path)) return "pdf";
  if (/\.json$/i.test(path)) return "json";
  if (/\.(tiff?|heic|zip|gz|tgz|bz2|xz|7z|rar|tar|exe|dll|so|dylib|bin|o|a|class|jar|wasm|mp3|mp4|m4a|wav|ogg|mov|avi|mkv|webm|woff2?|ttf|otf|eot|docx?|xlsx?|pptx?|db|sqlite3?)$/i.test(path)) return "binary";
  return "text";
}

// What `files/launch` opens with the default app. The same list as `launchTypes` in filelaunch.go.
function readLaunchable(path) { return /\.(png|jpe?g|gif|webp|bmp|ico|avif|svg|pdf|md|markdown|txt|log|csv|json|html?)$/i.test(path); }

// OPEN EXTERNALLY hands the file to the default app of the ROOM's machine, which is named, because over a share
// it is not this one.
function extMain(id, path) {
  const btn = document.getElementById("open-ext");
  const note = document.getElementById("ext-note");
  if (!btn || !readLaunchable(path)) return;
  const cut = id.indexOf("~");
  const room = cut > 0 ? id.slice(0, cut) : "";
  const on = room ? room + "'s machine" : "the room's machine";
  btn.hidden = false;
  btn.title = "Opens it with the default app on " + on + ", not in this browser.";
  btn.textContent = "Open externally, on " + (room || "the room's machine");
  btn.onclick = async () => {
    btn.disabled = true;
    let msg = "";
    try {
      // On the room's own machine with the handler installed, the desktop user's link. See `launchByLink` in notes-files.js.
      if (localStorage.getItem("atriumOpenLink") === "1" && /^(127\.0\.0\.1|localhost|\[::1\])$/.test(location.hostname)) {
        const l = await fetch("/v1/tasks/" + encodeURIComponent(id) + "/files/launch-link", {
          method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ path: path })
        });
        const j = l.ok ? await l.json() : null;
        if (j && j.url) {
          location.href = j.url;
          btn.disabled = false;
          note.textContent = "Asked your desktop to open it.";
          note.hidden = false;
          return;
        }
      }
      const r = await fetch("/v1/tasks/" + encodeURIComponent(id) + "/files/launch", {
        method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ path: path })
      });
      if (!r.ok) {
        let m = (await r.text()).trim();
        try { const j = JSON.parse(m); if (j && j.error) m = j.error; } catch (e) {}
        throw new Error(m || r.statusText);
      }
      msg = "Opened with the default app on " + on + ", not in this browser.";
    } catch (e) {
      msg = "Could not open it: " + e.message;
    }
    btn.disabled = false;
    note.textContent = msg;
    note.hidden = false;
  };
}

// A relative reference against the folder of `from`, as a card path. The result is
// never trusted: containment is the daemon's and a `..` past the top is refused there.
function resolveRel(from, ref) {
  const out = from.split("/").slice(0, -1);
  for (const seg of ref.split("/")) {
    if (seg === "" || seg === ".") continue;
    if (seg === "..") { out.pop(); continue; }
    out.push(seg);
  }
  return out.join("/");
}

// What the scheme of `ref` is, or "" for a relative one.
function refScheme(ref) {
  const m = /^\s*([a-z][a-z0-9+.-]*):/i.exec(ref.replace(/[\u0000- ]/g, ""));
  return m ? m[1].toLowerCase() : "";
}

// The sanitised HTML for a document. Pure: no DOM of the page is touched.
function renderMarkdown(text, id, path) {
  const renderer = new marked.Renderer();
  renderer.html = (h) => mdEscape(typeof h === "string" ? h : (h && (h.text || h.raw)) || "");
  const dirty = marked.parse(String(text), { gfm: true, breaks: false, renderer, async: false });
  const clean = DOMPurify.sanitize(dirty, {
    USE_PROFILES: { html: true },
    FORBID_TAGS: ["style", "form", "input", "button", "textarea", "select", "iframe", "object", "embed", "svg", "math", "link", "meta", "base"],
    FORBID_ATTR: ["style", "srcset", "formaction"],
    ALLOW_DATA_ATTR: false
  });
  // An inert document: an img in the live page fetches a remote picture before it is rewritten away.
  const box = new DOMParser().parseFromString("<!doctype html><body>" + clean, "text/html").body;
  for (const a of box.querySelectorAll("a[href]")) {
    const ref = a.getAttribute("href");
    const sc = refScheme(ref);
    if (sc === "http" || sc === "https" || sc === "mailto") {
      a.target = "_blank"; a.rel = "noopener noreferrer";
    } else if (sc !== "") {
      a.removeAttribute("href");
    } else if (ref.trim().startsWith("#")) {
      // An anchor inside this page's own address is not a card path.
      a.removeAttribute("href");
    } else {
      const [p] = ref.split("#");
      const to = resolveRel(path, decodeURIComponentSafe(p.split("?")[0]));
      if (SHARE) {
        const u = viewURL(id, to);
        if (u && to !== SHARE.path) {
          a.setAttribute("href", u);
          a.target = "_blank"; a.rel = "noopener noreferrer";
        } else {
          a.removeAttribute("href");
          a.title = "not shared";
        }
        continue;
      }
      a.setAttribute("href", "/read.html#" + encodeURIComponent(id) + "/" + encodeURIComponent(to));
      a.target = "_blank"; a.rel = "noopener noreferrer";
    }
  }
  readImages = [];
  for (const im of box.querySelectorAll("img")) {
    const ref = im.getAttribute("src") || "";
    const sc = refScheme(ref);
    if (sc === "data" && /^data:image\/(png|jpe?g|gif|webp)[;,]/i.test(ref.trim())) continue;
    if (sc !== "" || ref === "") {
      // Not fetched. The alt text stands in, so nothing is lost but the picture.
      const s = box.ownerDocument.createElement("span");
      s.textContent = "[image not loaded: " + (im.getAttribute("alt") || ref) + "]";
      im.replaceWith(s);
      continue;
    }
    const to = resolveRel(path, decodeURIComponentSafe(ref.split(/[?#]/)[0]));
    const u = viewURL(id, to);
    if (SHARE && !u) {
      const s = box.ownerDocument.createElement("span");
      s.textContent = "[image not shared: " + (im.getAttribute("alt") || ref) + "]";
      im.replaceWith(s);
      continue;
    }
    if (readImages.indexOf(to) < 0) readImages.push(to);
    im.setAttribute("src", u);
  }
  return box.innerHTML;
}

function decodeURIComponentSafe(s) { try { return decodeURIComponent(s); } catch (e) { return s; } }

function readMain() {
  // The id cannot hold a slash and a path can, so the first slash separates them.
  const frag = decodeURIComponent(location.hash.slice(1));
  const cut = frag.indexOf("/");
  const id = SHARE ? "" : cut < 0 ? "" : decodeURIComponent(frag.slice(0, cut));
  const path = SHARE ? String(SHARE.path || "") : cut < 0 ? "" : decodeURIComponent(frag.slice(cut + 1));

  const where = document.getElementById("where");
  const what = document.getElementById("what");
  const text = document.getElementById("text");
  const md = document.getElementById("md");

  if ((!SHARE && !id) || !path) {
    where.textContent = "nothing to read";
    text.className = "oops";
    text.textContent = "This address names no file.";
    return;
  }
  where.textContent = path;
  document.title = path.split("/").pop() + " — atrium";
  if (!SHARE) extMain(id, path);
  const kind = readKind(path);
  const oops = msg => { text.className = "oops"; text.textContent = msg; };
  if (kind === "image") {
    const pic = document.getElementById("pic");
    pic.alt = path;
    pic.onload = () => { what.textContent = pic.naturalWidth + " × " + pic.naturalHeight; };
    pic.onerror = () => { pic.hidden = true; text.hidden = false; oops("Could not show it as a picture."); };
    text.hidden = true;
    pic.hidden = false;
    pic.src = viewURL(id, path);
    return;
  }
  if (kind === "pdf") {
    text.hidden = true;
    const f = document.getElementById("pdf");
    f.hidden = false;
    f.src = viewURL(id, path);
    return;
  }
  if (kind === "binary") {
    oops("atrium shows pictures, PDFs and text, and this is none of them. ");
    const a = document.createElement("a");
    a.href = "/v1/tasks/" + encodeURIComponent(id) + "/files?path=" + encodeURIComponent(path);
    a.textContent = "Download it";
    text.appendChild(a);
    return;
  }
  fetch(viewURL(id, path))
    .then(async r => {
      if (!r.ok) throw new Error((await r.text()) || r.statusText);
      return r.text();
    })
    .then(body => {
      const lines = body.split("\n").length;
      what.textContent = lines + (lines === 1 ? " line" : " lines");
      if (kind === "svg") {
        // An image never runs the script an SVG can carry, and a blob keeps it off the view route's text type.
        const pic = document.getElementById("pic");
        pic.alt = path;
        pic.onerror = () => { pic.hidden = true; text.hidden = false; oops("Could not draw this SVG."); };
        text.hidden = true;
        pic.hidden = false;
        pic.src = URL.createObjectURL(new Blob([body], { type: "image/svg+xml" }));
      } else if (kind === "json") {
        let shown = body;
        try { shown = JSON.stringify(JSON.parse(body), null, 2); } catch (e) {}
        text.textContent = shown;
      } else if (isMarkdown(path)) {
        text.hidden = true;
        md.hidden = false;
        // The sanitised string, and the only innerHTML on this page.
        md.innerHTML = renderMarkdown(body, id, path);
        if (!SHARE) shareMain(id, path);
      } else if (isHTML(path) && !SHARE) {
        htmlMain(body);
      } else {
        // textContent, never innerHTML.
        text.textContent = body;
      }
    })
    .catch(e => {
      text.className = "oops";
      text.textContent = "Could not read it: " + e.message;
    });
}

// SHARE PUBLICLY. One click makes a snapshot of this document and the images just drawn, and shows the address.
function shareExpiry(iso) {
  const ms = Date.parse(iso) - Date.now();
  if (!(ms > 0)) return "expired";
  const days = Math.floor(ms / 86400000);
  if (days >= 1) return "expires in " + days + (days === 1 ? " day" : " days");
  const hours = Math.max(1, Math.floor(ms / 3600000));
  return "expires in " + hours + (hours === 1 ? " hour" : " hours");
}

function shareMain(id, path) {
  const btn = document.getElementById("share-doc");
  const box = document.getElementById("share-box");
  const err = document.getElementById("share-err");
  if (!btn || !box) return;
  const base = "/v1/tasks/" + encodeURIComponent(id) + "/docshare";
  let sharing = false;
  const fail = (msg) => { err.hidden = !msg; err.textContent = msg || ""; };
  const show = (s) => {
    document.getElementById("share-url").value = s.address;
    document.getElementById("share-note").textContent = "public, anyone with this link, " + shareExpiry(s.expires_at);
    box.hidden = false;
    btn.textContent = "Shared";
    btn.disabled = false;
    fail("");
  };
  const reasonOf = async (r) => {
    let m = "";
    try { m = (await r.text()).trim(); } catch (e) {}
    try { const j = JSON.parse(m); if (j && j.error) m = j.error; } catch (e) {}
    return m || r.statusText || "it did not work";
  };
  btn.hidden = false;
  btn.onclick = async () => {
    if (!box.hidden) { box.hidden = true; return; }
    if (sharing) return;
    if (btn.textContent === "Shared") { box.hidden = false; return; }
    sharing = true;
    btn.disabled = true;
    fail("");
    try {
      const r = await fetch(base, { method: "POST", headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ path: path, images: readImages.slice() }) });
      if (!r.ok) throw new Error(await reasonOf(r));
      show(await r.json());
    } catch (e) {
      btn.disabled = false;
      fail("Could not share it: " + e.message);
    } finally {
      sharing = false;
    }
  };
  document.getElementById("share-copy").onclick = () => {
    const inp = document.getElementById("share-url");
    const done = () => { document.getElementById("share-copy").textContent = "Copied"; };
    inp.select();
    if (navigator.clipboard && navigator.clipboard.writeText) navigator.clipboard.writeText(inp.value).then(done, () => {});
  };
  document.getElementById("share-stop").onclick = async () => {
    try {
      const r = await fetch(base + "?path=" + encodeURIComponent(path), { method: "DELETE" });
      if (!r.ok && r.status !== 404) throw new Error(await reasonOf(r));
      box.hidden = true;
      btn.textContent = "Share publicly";
      document.getElementById("share-copy").textContent = "Copy";
      fail("");
    } catch (e) {
      fail("Could not stop it: " + e.message);
    }
  };
  fetch(base + "?path=" + encodeURIComponent(path)).then(r => r.ok ? r.json() : null).then(s => {
    if (s && s.address) { show(s); box.hidden = true; }
  }).catch(() => {});
}

if (document.getElementById("md")) {
  readMain();
  addEventListener("hashchange", () => location.reload());
}

// TEXT SIZE, for the document only. Browser zoom is per origin, so zooming this page zooms the board too. This
// sets a CSS variable instead and keeps the number under a key of its own, which every read tab shares and the
// board never reads.
const READ_FONT_KEY = "atrium.readfont";
const READ_FONT_DEFAULT = 15;
const READ_FONT_MIN = 9;
const READ_FONT_MAX = 36;

function readFontStored() {
  try {
    const v = Number(localStorage.getItem(READ_FONT_KEY));
    if (v >= READ_FONT_MIN && v <= READ_FONT_MAX) return Math.round(v);
  } catch (e) {}
  return READ_FONT_DEFAULT;
}

function setReadFont(px, save) {
  const size = Math.max(READ_FONT_MIN, Math.min(READ_FONT_MAX, Math.round(px || 0)));
  document.documentElement.style.setProperty("--read-fs", size + "px");
  const val = document.getElementById("zoom-val");
  if (val) val.textContent = size + "px";
  if (save) { try { localStorage.setItem(READ_FONT_KEY, String(size)); } catch (e) {} }
  return size;
}

if (document.getElementById("zoom-up")) {
  let cur = setReadFont(readFontStored(), false);
  const step = (d) => { cur = setReadFont(d === 0 ? READ_FONT_DEFAULT : cur + d, true); };
  document.getElementById("zoom-up").addEventListener("click", () => step(1));
  document.getElementById("zoom-down").addEventListener("click", () => step(-1));
  document.getElementById("zoom-val").addEventListener("dblclick", () => step(0));
  addEventListener("keydown", e => {
    if (!(e.ctrlKey || e.metaKey) || e.altKey) return;
    const d = (e.key === "=" || e.key === "+") ? 1 : e.key === "-" ? -1 : e.key === "0" ? 0 : null;
    if (d === null) return;
    e.preventDefault();
    step(d);
  });
  // Another read tab changed it.
  addEventListener("storage", e => { if (e.key === READ_FONT_KEY) cur = setReadFont(readFontStored(), false); });
}

// READING WIDTH, for the document only, under a key of its own. `narrow` is the prose measure and the default.
const READ_WIDTH_KEY = "atrium.readwidth";
const READ_WIDTHS = ["narrow", "wide", "full"];

function readWidthStored() {
  try {
    const v = localStorage.getItem(READ_WIDTH_KEY);
    if (READ_WIDTHS.indexOf(v) >= 0) return v;
  } catch (e) {}
  return "narrow";
}

function setReadWidth(w, save) {
  if (READ_WIDTHS.indexOf(w) < 0) w = "narrow";
  document.documentElement.setAttribute("data-width", w);
  for (const b of document.querySelectorAll("#width button")) b.setAttribute("aria-pressed", String(b.dataset.width === w));
  if (save) { try { localStorage.setItem(READ_WIDTH_KEY, w); } catch (e) {} }
  return w;
}

if (document.getElementById("width")) {
  setReadWidth(readWidthStored(), false);
  for (const b of document.querySelectorAll("#width button")) b.addEventListener("click", () => setReadWidth(b.dataset.width, true));
  addEventListener("storage", e => { if (e.key === READ_WIDTH_KEY) setReadWidth(readWidthStored(), false); });
}

// SKIN. The page wears the board's, which is the daemon's `board_skin`, and `css/themes.css` is where every skin is
// defined, so nothing here knows a colour. The picker is for this page only: its choice is kept under a key of its
// own, wins over the board's, and "the board's skin" in the list puts the board's back. Before the daemon answers
// the page wears what the board last remembered in this browser, so it does not flash the default palette.
const READ_SKIN_KEY = "atrium.readskin";
const BOARD_SKIN_KEY = "atrium.skin";

function readSkinStored() {
  try { return localStorage.getItem(READ_SKIN_KEY) || ""; } catch (e) { return ""; }
}

// The board's skin as this browser last saw it, scoped to the room the way the board and the phone page scope it.
function boardSkinRemembered() {
  try {
    const room = localStorage.getItem("atrium.room") || "";
    return localStorage.getItem(BOARD_SKIN_KEY + (room ? "." + room : "")) || "";
  } catch (e) { return ""; }
}

// The first name in the daemon's list is the default, which has no rule of its own: it is `:root`.
function wearSkin(name, names) {
  if (name && (!names.length || (name !== names[0] && names.indexOf(name) >= 0))) {
    document.documentElement.setAttribute("data-skin", name);
  } else {
    document.documentElement.removeAttribute("data-skin");
  }
}

if (document.getElementById("skin")) {
  const sel = document.getElementById("skin");
  const early = readSkinStored() || boardSkinRemembered();
  if (early) document.documentElement.setAttribute("data-skin", early);
  let names = [], board = "";
  const paint = () => {
    const mine = readSkinStored();
    wearSkin(mine || board || boardSkinRemembered(), names);
    sel.value = mine && names.indexOf(mine) >= 0 ? mine : "";
  };
  sel.innerHTML = '<option value="">the board\'s skin</option>';
  sel.addEventListener("change", () => {
    try { sel.value ? localStorage.setItem(READ_SKIN_KEY, sel.value) : localStorage.removeItem(READ_SKIN_KEY); } catch (e) {}
    paint();
  });
  addEventListener("storage", e => { if (e.key === READ_SKIN_KEY) paint(); });
  fetch("/v1/settings").then(r => r.ok ? r.json() : null).then(s => {
    if (!s) return;
    names = Array.isArray(s.board_skins) ? s.board_skins : [];
    board = typeof s.board_skin === "string" ? s.board_skin : "";
    for (const n of names) {
      const o = document.createElement("option");
      o.value = n; o.textContent = n;
      sel.appendChild(o);
    }
    paint();
  }).catch(() => {});
}
