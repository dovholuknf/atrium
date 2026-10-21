// ── the review view ──────────────────────────────────────────────────────
//
// A read-only view of an atrium review (kind: review) beside chat and terminal in the chat tab. A card tagged
// `atrium:review` is asked for GET /v1/tasks/<id>/atrium-review, and a 404 means it has no review yet. Stage 1 has no
// buttons: the human answers in chat. The footer is the two lines the agent wrote, verbatim.

const REVIEW_TAG = "atrium:review";
const REVIEW_POLL_MS = 3000;
let reviewData = null;
let reviewOpen = false;
let reviewCur = 0;
let reviewTimer = 0;
let reviewFor = "";

function reviewEl(tag, cls, text) {
  const e = document.createElement(tag);
  if (cls) e.className = cls;
  if (text !== undefined) e.textContent = text;
  return e;
}

function reviewIsCard(id) {
  const t = chatCard(id);
  return !!(t && (t.tags || []).includes(REVIEW_TAG));
}

function reviewStop() {
  if (reviewTimer) clearTimeout(reviewTimer);
  reviewTimer = 0;
}

async function reviewLoad() {
  const id = reviewFor;
  if (!id) return;
  let data = null;
  try { data = await api("/v1/tasks/" + encodeURIComponent(id) + "/atrium-review"); } catch (e) {}
  if (id !== reviewFor) return;
  reviewData = data && Array.isArray(data.findings) ? data : null;
  const btn = document.getElementById("chat-review");
  if (btn) btn.hidden = !reviewData;
  if (!reviewData && reviewOpen) reviewShow(false);
  if (reviewOpen) reviewDraw();
  reviewStop();
  if (chatLent) reviewTimer = setTimeout(reviewLoad, REVIEW_POLL_MS);
}

// Called at the end of chatSelect.
function reviewSelected(id) {
  reviewStop();
  reviewFor = id && reviewIsCard(id) ? id : "";
  reviewData = null;
  const btn = document.getElementById("chat-review");
  if (btn) btn.hidden = true;
  if (reviewOpen) reviewShow(false);
  if (reviewFor) reviewLoad();
}

function reviewShow(on) {
  reviewOpen = on;
  const pane = document.getElementById("chat-review-pane");
  const host = document.getElementById("chat-frame-host");
  const btn = document.getElementById("chat-review");
  if (pane) pane.hidden = !on;
  if (host) host.hidden = on;
  if (btn) btn.classList.toggle("on", on);
  if (on) reviewDraw();
}

function reviewToggle() { reviewShow(!reviewOpen); }

function reviewStateClass(s) {
  if (s === "applied" || s === "kept") return "ok";
  if (s === "withdrawn" || s === "refused") return "no";
  return "";
}

function reviewDiffLines(diff) {
  const out = [];
  for (const line of String(diff || "").split("\n")) {
    if (line === "" && out.length === 0) continue;
    let cls = "";
    if (line.startsWith("@@")) cls = "h";
    else if (line.startsWith("+") && !line.startsWith("+++")) cls = "a";
    else if (line.startsWith("-") && !line.startsWith("---")) cls = "d";
    else if (line.startsWith("+++") || line.startsWith("---")) cls = "h";
    out.push(reviewEl("div", cls, line));
  }
  return out;
}

function reviewDraw() {
  const rail = document.getElementById("review-rail");
  const main = document.getElementById("review-main");
  const meta = document.getElementById("review-meta");
  if (!rail || !main || !reviewData) return;
  const d = reviewData;
  const fs = d.findings;
  meta.textContent = ["against " + (d.base || "base"), (d.files || []).length + " files",
    fs.length + " findings"].join(" · ");
  if (!fs.some(f => f.num === reviewCur)) reviewCur = fs.length ? fs[0].num : 0;
  rail.replaceChildren();
  const groups = [["batch", "Applied for veto"], ["", "One at a time"]];
  for (const [g, label] of groups) {
    const rows = fs.filter(f => (f.group === "batch") === (g === "batch"));
    if (!rows.length) continue;
    rail.append(reviewEl("div", "rv-h", label + " (" + rows.length + ")"));
    for (const f of rows) {
      const row = reviewEl("div", "rv-f" + (f.num === reviewCur ? " cur" : ""));
      row.dataset.num = String(f.num);
      row.append(reviewEl("span", "rv-cat", f.cat || ""), reviewEl("span", "rv-t", f.title || ""),
        reviewEl("span", "rv-st " + reviewStateClass(f.state), f.state || ""));
      row.addEventListener("click", () => { reviewCur = f.num; reviewDraw(); });
      rail.append(row);
    }
  }
  const f = fs.find(x => x.num === reviewCur);
  main.replaceChildren();
  if (!f) {
    main.append(reviewEl("p", "chat-empty", "No findings yet."));
    return;
  }
  const site = reviewEl("div", "rv-site");
  site.append(reviewEl("span", "rv-loc", f.site || ""),
    reviewEl("span", "", [f.kind, f.origin, f.lines_vs_base ? f.lines_vs_base + " lines vs base" : ""].filter(Boolean).join(" · ")));
  const diff = reviewEl("pre", "rv-diff");
  diff.append(...reviewDiffLines(f.diff));
  main.append(site, diff);
  if (f.reason) main.append(reviewEl("div", "rv-note", f.reason));
  if (f.note) main.append(reviewEl("div", "rv-note", f.note));
  main.append(reviewEl("pre", "rv-foot", f.footer || ""));
}
