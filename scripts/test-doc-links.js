// A published document's address is a link of the board and its images are drawn: the terminal's /d/<slug> link, and the
// markdown view's ![alt](/d/<slug>) image, fetched from /_hub/docs and typed by its bytes.
const fs = require("fs");
const path = require("path");

let bad = 0;
function fail(msg) { console.error("FAIL: " + msg); bad++; }
const web = path.join(__dirname, "..", "internal", "api", "web");

const png = Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNkYAAAAAYAAjCB0C8AAAAASUVORK5CYII=", "base64");
const fetched = [];
const el = { dataset: { card: "", path: "/d/before-lab-png" }, isConnected: true, textContent: "", kids: [],
  getAttribute: () => "before", appendChild(c) { this.kids.push(c); } };
const win = {};
const doc = { addEventListener() {}, querySelectorAll: () => [el], createElement: () => ({}) };
const blobs = [];
const run = new Function("window", "document", "fetch", "URL", "Blob", fs.readFileSync(path.join(web, "m", "js", "md.js"), "utf8"));
run(win, doc, async u => { fetched.push(u); return { ok: true, arrayBuffer: async () => png.buffer.slice(png.byteOffset, png.byteOffset + png.length) }; },
  { createObjectURL: b => { blobs.push(b); return "blob:x"; }, revokeObjectURL() {} }, Blob);

const html = win.mMd.render("![before](/d/before-lab-png)\n\n![v](/d/a-b@2)", { id: "", worktree: "", noFiles: true });
if (!/class="md-img"[^>]*data-path="\/d\/before-lab-png"/.test(html)) fail("a published image ref was not an image node: " + html);
if (!/data-path="\/d\/a-b@2"/.test(html)) fail("a versioned image ref was not an image node: " + html);
if (/not loaded/.test(html)) fail("a published image ref was shown as not loaded: " + html);

(async () => {
  win.mMd.hydrate(null);
  await new Promise(r => setTimeout(r, 50)); // wait: the stub fetch resolves in a few microtasks
  if (fetched[0] !== "/_hub/docs/before-lab-png/raw") fail("the image was fetched from " + JSON.stringify(fetched));
  if (!blobs.length || blobs[0].type !== "image/png") fail("the image was not typed by its bytes: " + (blobs[0] && blobs[0].type));
  if (!el.kids.length || el.kids[0].src !== "blob:x") fail("the image was not drawn");

  const tl = fs.readFileSync(path.join(web, "js", "terminal-links.js"), "utf8");
  const mk = new Function("term", "linkTipHide", "openLinkReused", "location",
    tl.slice(tl.indexOf("function docLinkPath"), tl.indexOf("function linkTipShow")) +
    "; return { docLinkPath, docLink };");
  const opened = [];
  const t = mk(null, () => {}, u => opened.push(u), { origin: "http://hub:7778" });
  for (const ok of ["/d/orange-coral", "/d/x@3"]) if (!t.docLinkPath(ok)) fail(ok + " was not a doc path");
  for (const no of ["/d/work/x.md", "/d/", "/d/X", "/d/x@0", "/etc/d/x"]) if (t.docLinkPath(no)) fail(no + " was a doc path");
  t.docLink({}, "/d/orange-coral").activate({ preventDefault() {}, button: 0 });
  if (opened[0] !== "http://hub:7778/d/orange-coral") fail("the click opened " + JSON.stringify(opened));
  if (bad) process.exit(1);
  console.log("doc links ok");
})();
