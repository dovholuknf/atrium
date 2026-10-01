// A reply is model output, and some of it echoes web content, so it is rendered SAFELY.
//
// Every character is escaped before any markup is added, raw HTML is never passed through, and a link survives only
// as http or https, the same allow list `SafeURL` in internal/store/intake.go applies to intake links. Code goes in
// monospace blocks that scroll sideways inside themselves, with a copy button.
//
// FILES OF THE CARD. An absolute path under the card's directory (`ctx.worktree`) is a tap to open or download it, and an
// image one is a thumbnail that opens larger on a tap. Both go through the card's own download endpoint, which
// internal/safepath bounds on the daemon, so a refusal there shows "not available" and never a broken image. A path
// outside the card is plain text. A remote image is never loaded: it is shown as a link. Nothing here copies an
// attribute out of the reply, the data attributes hold escaped values the page's own code reads back.
(function () {
  "use strict";

  const ENT = { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" };
  const esc = s => String(s).replace(/[&<>"']/g, c => ENT[c]);
  const unesc = s => String(s).replace(/&amp;/g, "&").replace(/&lt;/g, "<").replace(/&gt;/g, ">")
    .replace(/&quot;/g, '"').replace(/&#39;/g, "'");

  // The JS side of intake's SafeURL. An allow list, because the set of schemes a browser acts on is not one to enumerate.
  function safeURL(raw) {
    const v = String(raw || "").trim();
    if (!v) return "";
    let u;
    try { u = new URL(v); } catch (e) { return ""; }
    return u.protocol === "http:" || u.protocol === "https:" ? v : "";
  }

  // A link to a hub document, `/d/<slug>` or `/d/<slug>@<n>`, and nothing looser. It is a same-origin link the page opens in
  // its own documents view (js/docs.js), so it carries no target and no scheme.
  const DOC = /^\/d\/[a-z0-9-]{1,60}(@[1-9][0-9]*)?$/;

  // Inline markup on one run of text. Code spans and links are lifted out first, as placeholders, so emphasis never
  // reaches into them.
  const IMG = /\.(png|jpe?g|gif|webp)$/i;

  // A path as the page compares it: forward slashes only.
  const slashed = p => String(p || "").replace(/\\/g, "/");
  const DRIVE = /^[A-Za-z]:\//;
  const isAbs = p => p[0] === "/" || DRIVE.test(p);

  // Whether a path names something inside the card's directory. Absolute and under it, or relative with no way out.
  // A Windows worktree is `D:/...` and compares without regard to case. A single letter and a colon then a slash is a
  // drive, anything else before a colon is a scheme and is refused.
  function inside(path, ctx) {
    const wt = ctx && ctx.worktree ? slashed(ctx.worktree).replace(/\/+$/, "") : "";
    const p = slashed(path);
    if (!p || /[\u0000-\u001f]/.test(p)) return false;
    if (isAbs(p)) {
      if (!wt || p.length <= wt.length + 1) return false;
      const fold = DRIVE.test(wt) ? x => x.toLowerCase() : x => x;
      if (DRIVE.test(wt) !== DRIVE.test(p) || fold(p).indexOf(fold(wt) + "/") !== 0) return false;
    } else if (/^[a-z][a-z0-9+.-]*:/i.test(p)) return false;
    return !p.split("/").some(seg => seg === ".." || seg === ".");
  }

  const base = p => String(p).replace(/\/+$/, "").split("/").pop();

  // The node that stands for a card file. Hydrated by `hydrate`, which fetches an image and leaves a file for a tap.
  function fileNode(path, ctx, label, asImage) {
    path = slashed(path);
    const a = esc(path), id = esc(ctx.id || "");
    if (asImage && IMG.test(path)) {
      return '<button type="button" class="md-img" data-card="' + id + '" data-path="' + a + '" aria-label="' + esc(label || base(path)) +
        '"><span class="md-img-note">' + esc(label || base(path)) + "</span></button>";
    }
    return '<button type="button" class="md-file" data-card="' + id + '" data-path="' + a + '">' + esc(label || base(path)) + "</button>";
  }

  function inline(text, ctx) {
    ctx = ctx || {};
    const hold = [];
    const stash = html => { hold.push(html); return "\u0000" + (hold.length - 1) + "\u0000"; };
    let s = String(text).replace(/\u0000/g, "");
    s = s.replace(/`([^`\n]+)`/g, (m, c) =>
      stash(inside(c, ctx) && isAbs(slashed(c)) ? fileNode(c, ctx, c, true) : "<code>" + esc(c) + "</code>"));
    s = esc(s);
    // An image: a file of the card is a thumbnail, a remote one is a link and is not loaded, anything else is its alt text.
    s = s.replace(/!\[([^\]\n]*)\]\(([^)\s]+)\)/g, (m, alt, url) => {
      const u = unesc(url);
      if (inside(u, ctx) && IMG.test(u)) return stash(fileNode(u, ctx, unesc(alt), true));
      const ok = safeURL(u);
      if (ok) return stash('<a href="' + esc(ok) + '" target="_blank" rel="noopener noreferrer">' + (alt || "image") + " (image, not loaded)</a>");
      return alt;
    });
    s = s.replace(/\[([^\]\n]+)\]\(([^)\s]+)\)/g, (m, label, url) => {
      const u = unesc(url);
      if (DOC.test(u)) return stash('<a href="' + esc(u) + '" class="md-doc">' + label + "</a>");
      if (inside(u, ctx)) return stash(fileNode(u, ctx, unesc(label), false));
      const ok = safeURL(u);
      if (!ok) return label;
      return stash('<a href="' + esc(ok) + '" target="_blank" rel="noopener noreferrer">' + label + "</a>");
    });
    // A bare absolute path in the text. Only one under the card is a control, the rest stays text.
    s = s.replace(/(^|[\s(])((?:\/|[A-Za-z]:[\\/])[\w.@+~\/\\-]+)/g, (m, lead, path) => {
      const raw = unesc(path), tail = /[.,;:)]+$/.exec(raw), p = tail ? raw.slice(0, -tail[0].length) : raw;
      if (DOC.test(p)) return lead + stash('<a href="' + esc(p) + '" class="md-doc">' + esc(p) + "</a>") + esc(tail ? tail[0] : "");
      if (!inside(p, ctx)) return m;
      return lead + stash(fileNode(p, ctx, p, true)) + esc(tail ? tail[0] : "");
    });
    s = s.replace(/\*\*([^*\n]+)\*\*/g, "<strong>$1</strong>")
      .replace(/(^|[^\w*])\*([^*\n]+)\*(?![\w*])/g, "$1<em>$2</em>")
      .replace(/(^|[^\w])_([^_\n]+)_(?![\w])/g, "$1<em>$2</em>")
      .replace(/~~([^~\n]+)~~/g, "<del>$1</del>");
    return s.replace(/\u0000(\d+)\u0000/g, (m, i) => hold[+i]);
  }

  // Cells of one table row. A pipe inside a code span or escaped with a backslash belongs to the cell.
  function cells(line) {
    let s = line.trim();
    if (s[0] === "|") s = s.slice(1);
    if (s.endsWith("|") && !s.endsWith("\\|")) s = s.slice(0, -1);
    const out = [];
    let cur = "", tick = false;
    for (let k = 0; k < s.length; k++) {
      const c = s[k];
      if (c === "\\" && s[k + 1] === "|") { cur += "|"; k++; continue; }
      if (c === "`") tick = !tick;
      if (c === "|" && !tick) { out.push(cur.trim()); cur = ""; continue; }
      cur += c;
    }
    out.push(cur.trim());
    return out;
  }

  const DELIM = /^\s*\|?\s*:?-+:?\s*(\|\s*:?-+:?\s*)*\|?\s*$/;

  // A header row, a delimiter row and the rows under it, as a table in a box that scrolls sideways when it must.
  // Alignment colons are kept, and a short row is padded to the header's width.
  function table(head, delim, rows, ctx) {
    const n = head.length;
    const al = delim.map(d => /^:-+:$/.test(d) ? "center" : /-:$/.test(d) ? "right" : /^:-/.test(d) ? "left" : "");
    const cell = (tag, t, k) => "<" + tag + (al[k] ? ' style="text-align:' + al[k] + '"' : "") + ">" + inline(t, ctx) + "</" + tag + ">";
    const row = (tag, r) => "<tr>" + Array.from({ length: n }, (x, k) => cell(tag, r[k] || "", k)).join("") + "</tr>";
    return '<div class="tbl"><table><thead>' + row("th", head) + "</thead><tbody>" + rows.map(r => row("td", r)).join("") +
      "</tbody></table></div>";
  }

  function render(src, ctx) {
    ctx = ctx || {};
    const lines = String(src == null ? "" : src).replace(/\r\n?/g, "\n").split("\n");
    const out = [];
    let i = 0;
    let para = [];
    const flush = () => {
      if (para.length) out.push("<p>" + inline(para.join(" "), ctx) + "</p>");
      para = [];
    };
    while (i < lines.length) {
      const line = lines[i];
      const fence = /^\s*(```+|~~~+)\s*([\w+#.-]*)\s*$/.exec(line);
      if (fence) {
        flush();
        const mark = fence[1][0];
        const body = [];
        i++;
        while (i < lines.length && !new RegExp("^\\s*" + mark + "{3,}\\s*$").test(lines[i])) body.push(lines[i++]);
        i++; // the closing fence, or the end of a reply cut in the middle of one
        out.push('<div class="codebox"><button type="button" class="code-copy" aria-label="copy the code">copy</button>' +
          '<pre class="code"' + (fence[2] ? ' data-lang="' + esc(fence[2]) + '"' : "") + "><code>" +
          esc(body.join("\n")) + "</code></pre></div>");
        continue;
      }
      if (/^\s*$/.test(line)) { flush(); i++; continue; }
      if (line.includes("|") && i + 1 < lines.length && DELIM.test(lines[i + 1]) && lines[i + 1].includes("-") &&
          cells(lines[i + 1]).length === cells(line).length) {
        flush();
        const head = cells(line), delim = cells(lines[i + 1]);
        const rows = [];
        i += 2;
        while (i < lines.length && lines[i].includes("|") && !/^\s*$/.test(lines[i])) rows.push(cells(lines[i++]));
        out.push(table(head, delim, rows, ctx));
        continue;
      }
      const h = /^(#{1,6})\s+(.*?)\s*#*\s*$/.exec(line);
      if (h) {
        flush();
        const n = Math.min(h[1].length + 2, 5); // a reply's h1 is not the page's
        out.push("<h" + n + ">" + inline(h[2], ctx) + "</h" + n + ">");
        i++;
        continue;
      }
      if (/^\s*([-*_])(\s*\1){2,}\s*$/.test(line)) { flush(); out.push("<hr>"); i++; continue; }
      if (/^\s*>/.test(line)) {
        flush();
        const q = [];
        while (i < lines.length && /^\s*>/.test(lines[i])) q.push(lines[i++].replace(/^\s*>\s?/, ""));
        out.push("<blockquote>" + render(q.join("\n"), ctx) + "</blockquote>");
        continue;
      }
      const li = /^(\s*)([-*+]|\d+[.)])\s+(.*)$/.exec(line);
      if (li) {
        flush();
        const ordered = /\d/.test(li[2]);
        const items = [];
        while (i < lines.length) {
          const m = /^(\s*)([-*+]|\d+[.)])\s+(.*)$/.exec(lines[i]);
          if (m && /\d/.test(m[2]) === ordered) { items.push(m[3]); i++; continue; }
          // A wrapped continuation of the item above it.
          if (items.length && /^\s{2,}\S/.test(lines[i]) && !/^\s*([-*+]|\d+[.)])\s/.test(lines[i])) {
            items[items.length - 1] += " " + lines[i].trim();
            i++;
            continue;
          }
          break;
        }
        const tag = ordered ? "ol" : "ul";
        out.push("<" + tag + ">" + items.map(x => "<li>" + inline(x, ctx) + "</li>").join("") + "</" + tag + ">");
        continue;
      }
      para.push(line.trim());
      i++;
    }
    flush();
    return out.join("");
  }

  // ── the page's side: thumbnails, files, copy ─────────────────────────────
  const TYPE = { png: "image/png", jpg: "image/jpeg", jpeg: "image/jpeg", gif: "image/gif", webp: "image/webp" };

  // The card's file, through its own download endpoint. The room header is the one the rest of the page sends.
  async function getFile(card, path) {
    const headers = {};
    try { const room = localStorage.getItem("atrium.room"); if (room) headers["X-Atrium-Room"] = room; } catch (e) {}
    const r = await fetch("/v1/tasks/" + encodeURIComponent(card) + "/files?path=" + encodeURIComponent(path), { headers });
    if (!r.ok) throw new Error("HTTP " + r.status);
    return r.blob();
  }

  function unavailable(el) {
    el.dataset.state = "gone";
    el.className = "md-gone";
    el.removeAttribute("type");
    el.textContent = "not available";
  }

  // Object URLs by card and path, so a redraw does not fetch again, and revoked when the card closes or the cache is full.
  const cache = new Map();
  const CACHE_MAX = 40;
  function cacheDrop(key) {
    const v = cache.get(key);
    cache.delete(key);
    if (v && v.url) URL.revokeObjectURL(v.url);
  }
  // Oldest first, and never one still being fetched or one a drawn thumbnail shows, since revoking either breaks it.
  function evict() {
    for (const [key, v] of cache) {
      if (cache.size <= CACHE_MAX) return;
      const shown = v.url && [...document.querySelectorAll(".md-img img")].some(i => i.src === v.url);
      if (v.settled && !shown) cacheDrop(key);
    }
  }
  function release() { [...cache.keys()].forEach(cacheDrop); }

  // The picture of a card file, once. Resolves { url } or { url: "" } when it is not available.
  function picture(card, path) {
    const key = card + "\u0000" + path;
    let v = cache.get(key);
    if (v) { cache.delete(key); cache.set(key, v); return v.ready; }
    const ext = (/\.(\w+)$/.exec(path) || [])[1] || "";
    v = { url: "", ready: null, settled: false };
    v.ready = getFile(card, path).then(blob => {
      v.url = URL.createObjectURL(new Blob([blob], { type: TYPE[ext.toLowerCase()] || "application/octet-stream" }));
      v.settled = true;
      return v;
    }).catch(() => { v.settled = true; return v; });
    cache.set(key, v);
    evict();
    return v.ready;
  }

  // Fills every thumbnail not yet drawn. The bytes are typed by the file's extension and not by the server, so a file
  // that is not the picture it says it is shows nothing.
  function hydrate(root) {
    (root || document).querySelectorAll(".md-img:not([data-state])").forEach(el => {
      el.dataset.state = "loading";
      picture(el.dataset.card, el.dataset.path).then(v => {
        if (!v.url || !el.isConnected) { if (!v.url) unavailable(el); return; }
        const img = document.createElement("img");
        img.alt = el.getAttribute("aria-label") || "";
        img.onerror = () => unavailable(el);
        img.onload = () => { el.dataset.state = "ready"; };
        img.src = v.url;
        el.textContent = "";
        el.appendChild(img);
      });
    });
  }

  function enlarge(src, alt) {
    const box = document.createElement("div");
    box.className = "md-lightbox";
    const img = document.createElement("img");
    img.src = src;
    img.alt = alt || "";
    box.appendChild(img);
    box.addEventListener("click", () => box.remove());
    document.body.appendChild(box);
  }

  async function saveFile(el) {
    try {
      const blob = await getFile(el.dataset.card, el.dataset.path);
      const a = document.createElement("a");
      a.href = URL.createObjectURL(blob);
      a.download = base(el.dataset.path);
      document.body.appendChild(a);
      a.click();
      a.remove();
      setTimeout(() => URL.revokeObjectURL(a.href), 10000);
    } catch (e) { unavailable(el); }
  }

  // The clipboard API needs a secure page, so on plain http the code is copied through a selected textarea. When that
  // is refused as well the code is left selected and the button says so.
  function copy(btn) {
    const pre = btn.parentNode.querySelector("pre");
    const text = pre ? pre.textContent : "";
    const say = t => { btn.textContent = t; setTimeout(() => { btn.textContent = "copy"; }, 1800); };
    const fallback = () => {
      const ta = document.createElement("textarea");
      ta.value = text;
      ta.setAttribute("readonly", "");
      ta.style.cssText = "position:fixed;top:0;left:0;opacity:0";
      document.body.appendChild(ta);
      ta.select();
      let ok = false;
      try { ok = document.execCommand("copy"); } catch (e) {}
      ta.remove();
      if (ok) { say("copied"); return; }
      if (pre) { const r = document.createRange(); r.selectNodeContents(pre); const sel = getSelection(); sel.removeAllRanges(); sel.addRange(r); }
      say("select and copy");
    };
    if (navigator.clipboard && navigator.clipboard.writeText) navigator.clipboard.writeText(text).then(() => say("copied"), fallback);
    else fallback();
  }

  document.addEventListener("click", e => {
    const t = e.target.closest && e.target.closest(".md-img, .md-file, .code-copy");
    if (!t) return;
    if (t.classList.contains("code-copy")) copy(t);
    else if (t.classList.contains("md-file")) { if (window.mViewer) window.mViewer.open(t.dataset.card, t.dataset.path); else saveFile(t); }
    else if (t.dataset.state === "ready") { const i = t.querySelector("img"); if (i) enlarge(i.src, i.alt); }
  });
  // A reply is drawn by innerHTML, so a thumbnail is filled when it arrives. Only where replies are drawn.
  if (window.MutationObserver) {
    ["m-replies", "m-recap"].forEach(id => {
      const host = document.getElementById(id);
      if (host) new MutationObserver(() => hydrate(host)).observe(host, { childList: true, subtree: true });
    });
  }

  window.mMd = { render, safeURL, inline, hydrate, release, saveBlob: (blob, name) => { const a = document.createElement("a"); a.href = URL.createObjectURL(blob); a.download = name; document.body.appendChild(a); a.click(); a.remove(); setTimeout(() => URL.revokeObjectURL(a.href), 10000); } };
})();
