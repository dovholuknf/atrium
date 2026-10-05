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
// folder to the same card: another .md opens in this page, anything else at the
// view route. Remote images are not loaded.

// `mdEscape` is the renderer's answer for raw HTML.
function mdEscape(s) {
  return String(s == null ? "" : s)
    .replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;");
}

// Where a card's file is streamed from.
function viewURL(id, path) {
  return "/v1/tasks/" + encodeURIComponent(id) + "/files/view?path=" + encodeURIComponent(path);
}

function isMarkdown(path) { return /\.(md|markdown)$/i.test(path); }

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
  // An INERT document, not a div in this page: an element built in the live
  // document starts loading its image the moment it has a src, which would fetch
  // a remote picture before it is rewritten away.
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
      a.setAttribute("href", isMarkdown(to) ? "/read.html#" + encodeURIComponent(id) + "/" + encodeURIComponent(to) : viewURL(id, to));
      a.target = "_blank"; a.rel = "noopener noreferrer";
    }
  }
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
    im.setAttribute("src", viewURL(id, resolveRel(path, decodeURIComponentSafe(ref.split(/[?#]/)[0]))));
  }
  return box.innerHTML;
}

function decodeURIComponentSafe(s) { try { return decodeURIComponent(s); } catch (e) { return s; } }

function readMain() {
  // `id/path`, where the id cannot contain a slash and a path can, so the
  // first one separates them and the rest belongs to the file.
  const frag = decodeURIComponent(location.hash.slice(1));
  const cut = frag.indexOf("/");
  const id = cut < 0 ? "" : decodeURIComponent(frag.slice(0, cut));
  const path = cut < 0 ? "" : decodeURIComponent(frag.slice(cut + 1));

  const where = document.getElementById("where");
  const what = document.getElementById("what");
  const text = document.getElementById("text");
  const md = document.getElementById("md");

  if (!id || !path) {
    where.textContent = "nothing to read";
    text.className = "oops";
    text.textContent = "This address names no file.";
    return;
  }
  where.textContent = path;
  document.title = path.split("/").pop() + " — atrium";
  fetch(viewURL(id, path))
    .then(async r => {
      if (!r.ok) throw new Error((await r.text()) || r.statusText);
      return r.text();
    })
    .then(body => {
      const lines = body.split("\n").length;
      what.textContent = lines + (lines === 1 ? " line" : " lines");
      if (isMarkdown(path)) {
        text.hidden = true;
        md.hidden = false;
        // The sanitised string, and the only innerHTML on this page.
        md.innerHTML = renderMarkdown(body, id, path);
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

if (document.getElementById("md")) {
  readMain();
  addEventListener("hashchange", () => location.reload());
}
