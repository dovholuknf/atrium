// A reply is model output, and some of it echoes web content, so it is rendered SAFELY.
//
// Every character is escaped before any markup is added, raw HTML is never passed through, and a link survives only
// as http or https, the same allow list `SafeURL` in internal/store/intake.go applies to intake links. Code goes in
// monospace blocks that scroll sideways inside themselves.
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

  // Inline markup on one run of text. Code spans and links are lifted out first, as placeholders, so emphasis never
  // reaches into them.
  function inline(text) {
    const hold = [];
    const stash = html => { hold.push(html); return "\u0000" + (hold.length - 1) + "\u0000"; };
    let s = String(text).replace(/\u0000/g, "");
    s = s.replace(/`([^`\n]+)`/g, (m, c) => stash("<code>" + esc(c) + "</code>"));
    s = esc(s);
    s = s.replace(/\[([^\]\n]+)\]\(([^)\s]+)\)/g, (m, label, url) => {
      const ok = safeURL(unesc(url));
      if (!ok) return label;
      return stash('<a href="' + esc(ok) + '" target="_blank" rel="noopener noreferrer">' + label + "</a>");
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
  function table(head, delim, rows) {
    const n = head.length;
    const al = delim.map(d => /^:-+:$/.test(d) ? "center" : /-:$/.test(d) ? "right" : /^:-/.test(d) ? "left" : "");
    const cell = (tag, t, k) => "<" + tag + (al[k] ? ' style="text-align:' + al[k] + '"' : "") + ">" + inline(t) + "</" + tag + ">";
    const row = (tag, r) => "<tr>" + Array.from({ length: n }, (x, k) => cell(tag, r[k] || "", k)).join("") + "</tr>";
    return '<div class="tbl"><table><thead>' + row("th", head) + "</thead><tbody>" + rows.map(r => row("td", r)).join("") +
      "</tbody></table></div>";
  }

  function render(src) {
    const lines = String(src == null ? "" : src).replace(/\r\n?/g, "\n").split("\n");
    const out = [];
    let i = 0;
    let para = [];
    const flush = () => {
      if (para.length) out.push("<p>" + inline(para.join(" ")) + "</p>");
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
        out.push('<pre class="code"' + (fence[2] ? ' data-lang="' + esc(fence[2]) + '"' : "") + "><code>" +
          esc(body.join("\n")) + "</code></pre>");
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
        out.push(table(head, delim, rows));
        continue;
      }
      const h = /^(#{1,6})\s+(.*?)\s*#*\s*$/.exec(line);
      if (h) {
        flush();
        const n = Math.min(h[1].length + 2, 5); // a reply's h1 is not the page's
        out.push("<h" + n + ">" + inline(h[2]) + "</h" + n + ">");
        i++;
        continue;
      }
      if (/^\s*([-*_])(\s*\1){2,}\s*$/.test(line)) { flush(); out.push("<hr>"); i++; continue; }
      if (/^\s*>/.test(line)) {
        flush();
        const q = [];
        while (i < lines.length && /^\s*>/.test(lines[i])) q.push(lines[i++].replace(/^\s*>\s?/, ""));
        out.push("<blockquote>" + render(q.join("\n")) + "</blockquote>");
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
        out.push("<" + tag + ">" + items.map(x => "<li>" + inline(x) + "</li>").join("") + "</" + tag + ">");
        continue;
      }
      para.push(line.trim());
      i++;
    }
    flush();
    return out.join("");
  }

  window.mMd = { render, safeURL, inline };
})();
