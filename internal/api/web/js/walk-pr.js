// Layout B's comments, whole-PR thread and share: what hangs off the diff js/walk.js draws.

const walkPr = {
  taskId: "",
  prUrl: "",            // the pull request this card walks, as the daemon names it, for when the pulls list is not loaded
  s: null,              // the saved walk: { v, comments, thread, off, bottom, open }
  timer: 0,
  drafts: new Map(),    // textarea key -> text not sent yet, so a repaint keeps it
  editing: null,        // { file, line } of a new comment being written
  sel: "",              // id of the comment the walk stands on
  region: null
};

// One interface per forge. The walk asks `walkPrForge` and never branches on which it got.
const walkForges = {
  github: {
    label: "GitHub", ref: "#",
    anchor: async (f, c) => f.url + "/files#diff-" + await walkDigest("SHA-256", c.file) + "R" + c.line,
    cmd: (f, c, head) => "gh api repos/" + f.repo + "/pulls/" + f.num + "/comments -f commit_id=" + head + " -f path=" + c.file +
      " -F line=" + c.line + " -f side=RIGHT -f body=" + JSON.stringify(c.text)
  },
  gitlab: {
    label: "GitLab", ref: "!",
    anchor: async (f, c) => f.url + "/diffs#" + await walkDigest("SHA-1", c.file) + "_" + c.line + "_" + c.line,
    cmd: (f, c, head) => "glab api projects/" + encodeURIComponent(f.repo) + "/merge_requests/" + f.num +
      "/discussions -f 'position[position_type]=text' -f 'position[head_sha]=" + head + "' -f 'position[new_path]=" + c.file +
      "' -F 'position[new_line]=" + c.line + "' -f body=" + JSON.stringify(c.text)
  }
};

async function walkDigest(alg, s) {
  const buf = await crypto.subtle.digest(alg, new TextEncoder().encode(s));
  return [...new Uint8Array(buf)].map(b => b.toString(16).padStart(2, "0")).join("");
}

// The pull request this card walks, from its pulls row, or null for a bare branch.
function walkPrForge() {
  const row = walkPrOf(dock.taskId);
  const url = (row && row.url) || walkPr.prUrl || "";
  let m = /^(https?:\/\/github\.com\/([^/]+\/[^/]+))\/pull\/(\d+)/.exec(url);
  if (m) return { kind: "github", url: m[1] + "/pull/" + m[3], repo: m[2], num: m[3], f: walkForges.github };
  m = /^(https?:\/\/[^/]*gitlab[^/]*\/(.+?))\/-\/merge_requests\/(\d+)/.exec(url);
  if (m) return { kind: "gitlab", url: m[1] + "/-/merge_requests/" + m[3], repo: m[2], num: m[3], f: walkForges.gitlab };
  return null;
}

function walkPrBlank() { return { v: 1, comments: [], thread: [], off: [], bottom: "thread", open: false }; }

async function walkPrLoad(taskId) {
  if (walkPr.taskId === taskId && walkPr.s) return;
  walkPr.taskId = taskId; walkPr.s = null; walkPr.editing = null; walkPr.sel = ""; walkPr.drafts = new Map();
  let s = null;
  walkPr.prUrl = "";
  try { const r = await api(`/v1/tasks/${taskId}/walk`); s = r.state; walkPr.prUrl = r.pr_url || ""; } catch (e) { s = null; }
  if (walkPr.taskId !== taskId) return;
  walkPr.s = Object.assign(walkPrBlank(), s || {});
  walkPrSync();
}

// Takes in what the agent wrote to its file. A save still waiting goes first, so the walk it answers with has it.
async function walkPrSync() {
  const id = walkPr.taskId;
  if (!id || !walkPr.s) return;
  if (walkPr.timer) { clearTimeout(walkPr.timer); walkPr.timer = 0; try { await walkPrPut(id, walkPr.s); } catch (e) { return; } }
  let out;
  try { out = await api(`/v1/tasks/${id}/walk/agent`, { method: "POST" }); } catch (e) { return; }
  if (walkPr.taskId !== id || !out || !out.state || walkPr.timer) return;
  const at = (n) => (n.comments || []).reduce((k, c) => k + 1 + (c.turns || []).length, 0);
  if (at(out.state) === at(walkPr.s)) return;
  walkPr.s = Object.assign(walkPrBlank(), out.state);
  if (dock.open) dockPaint();
}

function walkPrPut(id, s) {
  return api(`/v1/tasks/${id}/walk`, { method: "PUT", headers: { "Content-Type": "application/json" }, body: JSON.stringify(s) });
}

function walkPrSave() {
  clearTimeout(walkPr.timer);
  const id = walkPr.taskId, s = walkPr.s;
  if (!id || !s) return;
  walkPr.timer = setTimeout(() => {
    walkPr.timer = 0;
    api(`/v1/tasks/${id}/walk`, { method: "PUT", headers: { "Content-Type": "application/json" }, body: JSON.stringify(s) })
      .catch(e => toast("the walk was not saved", (e && e.message) || ""));
  }, 400);
}

function walkPrComment(id) { return walkPr.s && walkPr.s.comments.find(c => c.id === id) || null; }
function walkPrAgentComments() { return walkPr.s ? walkPr.s.comments.filter(c => c.who === "agent") : []; }

// ── the bubbles ─────────────────────────────────────────

const walkPrHelp = (tip) => ` <span class="help" tabindex="0" data-tip="${esc(tip)}">?</span>`;
const walkPrMd = t => esc(t).replace(/`([^`]+)`/g, "<code>$1</code>").replace(/\n/g, "<br>");

function walkPrBubble(c) {
  const sel = walkPr.sel === c.id;
  const size = (c.w ? "width:" + c.w + ";" : "") + (c.h ? "height:" + c.h + ";" : "");
  const forge = walkPrForge();
  const turns = (c.turns || []).map(t => `<div class="wk-turn ${t.who === "agent" ? "rv" : "op"}"><div class="wk-av">${t.who === "agent" ? "ag" : "you"}</div>
    <div class="wk-bub"><div class="wk-said">${walkPrMd(t.text)}</div></div></div>`).join("");
  const foot = c.who === "agent" && (c.scope || c.summary)
    ? `<pre class="rv-foot">CHANGE SCOPE : ${esc(c.scope || "")}\nSUMMARY      : ${esc(c.summary || "")}</pre>` : "";
  const key = "reply:" + c.id;
  return `<div class="cmt${c.who === "you" ? " you" : ""}${sel ? " sel" : ""}" data-cid="${esc(c.id)}" style="${size}">
    <div class="chead"><b>${c.who === "you" ? "you" : "agent"}</b> &middot; ${esc(c.file.split("/").pop())}:${c.line}${
      c.post ? ` <span class="st">copied for ${esc(forge ? forge.f.label : "the forge")}</span>` : ""}${
      walkPrHelp("Enter sends it to the walk agent, with the walk's scope in front. Shift+Enter or Ctrl+Enter is a new line.")}</div>
    <div class="wk-said">${walkPrMd(c.text)}</div>${foot}${turns}
    <textarea data-wpk="${esc(key)}" data-morph-key="${esc(key)}" rows="2" spellcheck="true"
      placeholder="Ask the agent about this line">${esc(walkPr.drafts.get(key) || "")}</textarea>
    <div class="row2"><span class="grow"></span>${forge
      ? `<button type="button" class="wk-btn" data-wp="post" data-cid="${esc(c.id)}" data-tip="copies the comment, then opens line ${c.line} of the files page on ${esc(forge.f.label)} in a new tab, so you paste it there">Post to ${esc(forge.f.label)}</button>` : ""}
      <button type="button" class="wk-btn" data-wp="drop" data-cid="${esc(c.id)}" data-tip="delete this comment">Delete</button></div>
    ${c.post ? `<code class="gh" data-tip="the same thing from a terminal">${esc(c.post)}</code>` : ""}</div>`;
}

function walkPrEditor(file, line) {
  const key = "new:" + file + ":" + line;
  return `<div class="cmt you sel" data-new="1">
    <div class="chead"><b>you</b> &middot; new comment on line ${line}${walkPrHelp("Enter saves the comment. Shift+Enter or Ctrl+Enter is a new line.")}</div>
    <textarea data-wpk="${esc(key)}" data-morph-key="${esc(key)}" id="walk-new" spellcheck="true"
      placeholder="your own review comment on this line">${esc(walkPr.drafts.get(key) || "")}</textarea>
    <div class="row2"><span class="grow"></span><button type="button" class="wk-btn go" data-wp="save">Save</button>
      <button type="button" class="wk-btn" data-wp="cancel">Cancel</button></div></div>`;
}

// What follows a line of the diff: the comments anchored there, and the editor for a new one.
function walkPrAfterLine(path, n) {
  if (!walkPr.s || walkPr.taskId !== dock.taskId) return "";
  let h = walkPr.s.comments.filter(c => c.file === path && c.line === n).map(walkPrBubble).join("");
  if (walkPr.editing && walkPr.editing.file === path && walkPr.editing.line === n) h += walkPrEditor(path, n);
  return h;
}

function walkPrCommentCount(path) {
  return walkPr.s && walkPr.taskId === dock.taskId ? walkPr.s.comments.filter(c => c.file === path).length : 0;
}

// ── the walk's scope, and what is sent ─────────────────

function walkPrScope() {
  const it = dockCurrent();
  if (!it || it.kind !== "file") return [];
  const at = walkTenant.at && walkTenant.at.key === it.key ? walkTenant.at.li : -1;
  const lines = it.file.lines;
  const num = at >= 0 && lines[at] && lines[at].n ? ":" + lines[at].n : "";
  const out = [{ k: "file", v: it.path + num }];
  for (let i = at; i >= 0; i--) if (lines[i].t === "@") { out.push({ k: "hunk", v: lines[i].text.replace(/^(@@[^@]*@@).*$/, "$1") }); break; }
  const c = walkPrComment(walkPr.sel);
  if (c) out.push({ k: "comment", v: c.who + " comment " + c.id + " on " + c.file.split("/").pop() + ":" + c.line + ", " + JSON.stringify(c.text.slice(0, 60) + (c.text.length > 60 ? "..." : "")) });
  return out.map(x => ({ ...x, on: !walkPr.s.off.includes(x.k) }));
}

function walkPrScopeText() {
  const f = walkPrForge();
  const head = f ? f.repo + f.f.ref + f.num : (walkTenant.branch && walkTenant.branch.branch) || "branch";
  return "[walk scope] " + head + " " + walkPrScope().filter(x => x.on).map(x => x.k + " " + x.v).join("  ");
}

// One line into the card's terminal, the way walkAsk types: a single frame, then the Enter.
function walkPrSend(text, withScope) {
  if (typeof termSock === "undefined" || !termSock || termSock.readyState !== WebSocket.OPEN) {
    toast("the terminal is not connected", "there is nobody to ask yet");
    return false;
  }
  sendInput((withScope === false ? "" : walkPrScopeText() + " | ") + text.replace(/\s*\n\s*/g, " "));
  setTimeout(() => sendInput("\r"), 60);
  return true;
}

// ── the bottom: one bar, the whole-PR thread, or the terminal itself ──

function walkPrRegion() {
  if (walkPr.region) return walkPr.region;
  const home = document.querySelector(".term-body");
  if (!home) return null;
  const r = document.createElement("div");
  r.id = "walk-bottom"; r.className = "walk-bottom"; r.hidden = true;
  home.appendChild(r);
  r.addEventListener("click", walkPrClick);
  r.addEventListener("keydown", walkPrKeydown);
  r.addEventListener("input", walkPrInput);
  walkPr.region = r;
  return r;
}

function walkPrBottomHtml() {
  const s = walkPr.s, mode = s.bottom === "terminal" ? "terminal" : "thread";
  const open = mode === "thread" && s.open;
  const tip = walkPrHelp("For questions about the whole change, not tied to a line. Enter sends, Shift+Enter or Ctrl+Enter is a new line.");
  const last = s.thread.length ? s.thread[s.thread.length - 1].q.split("\n")[0] : "";
  const sw = `<span class="wb-sw" role="group" aria-label="what the bottom shows"><button type="button" data-wp="mode" data-mode="thread" aria-pressed="${mode === "thread"}"
    data-tip="the whole-PR thread with the walk agent">thread</button><button type="button" data-wp="mode" data-mode="terminal" aria-pressed="${mode === "terminal"}"
    data-tip="the agent's terminal, with a line to type in">terminal</button></span>`;
  const head = `<div class="wb-bar${open ? " open" : ""}"><button type="button" class="wb-toggle" data-wp="toggle" aria-expanded="${open}"
      data-tip="${open ? "collapse the thread" : "open the whole-PR thread with the walk agent"}">${
      mode === "terminal" ? "<b>the agent's terminal</b>"
        : open ? "<b>chat with the agent about the PR on the whole</b>" : "<b>click here to ask the agent about the whole PR</b>"}</button>${tip}<span class="wb-last">${
      open || mode === "terminal" ? "" : esc(last)}</span>${open || mode === "terminal" ? "" : `<span class="by">${s.thread.length} messages &#9652;</span>`}${sw}</div>`;
  if (!open) return head;
  const log = `<div class="wk-turn rv"><div class="wk-av">ag</div><div class="wk-bub"><div class="wk-said">Ask me anything about the whole change here.
    The walk's scope goes in front of what you type, and the answer comes in the terminal.</div></div></div>` +
    s.thread.map(x => `<div class="wk-turn op"><div class="wk-av">you</div><div class="wk-bub"><div class="wk-said">${walkPrMd(x.q)}</div>
      <div class="sctx">${esc(x.scope.replace(/^\[walk scope\] /, ""))}</div></div></div>`).join("");
  const chips = walkPrScope().map(x => `<button type="button" class="sc ${x.on ? "onc" : "offc"}" data-wp="chip" data-k="${x.k}" aria-pressed="${x.on}"
    data-tip="${x.on ? "sent with your message. click to leave it out" : "not sent with your message. click to send it again"}"><i>${x.on ? "&#10003;" : "&#9675;"}</i><b>${x.k}</b> ${esc(x.v)}</button>`).join("");
  return head + `<div class="wb-body"><div class="chatlog" id="walk-chatlog">${log}</div><div class="chatin"><div class="scopechips"><span>sent with it</span>${chips}</div>
    <div class="askrow"><textarea data-wpk="whole" data-morph-key="whole" rows="1" spellcheck="true" placeholder="Ask the agent about the whole PR">${esc(walkPr.drafts.get("whole") || "")}</textarea>
    <button type="button" class="wk-btn go" data-wp="whole-send">Send &#9166;</button></div></div></div>`;
}

// Called from `walkPlace`, so it runs whenever the drawer opens, closes, repaints or the layout moves.
function walkPrBottom() {
  const d = document.getElementById("walk-drawer");
  const r = walkPrRegion();
  if (!d || !r) return;
  const on = walkB() && !!walkPr.s && walkPr.taskId === dock.taskId;
  r.hidden = !on;
  if (!on) { d.style.bottom = ""; return; }
  const s = walkPr.s, open = s.bottom !== "terminal" && s.open;
  r.className = "walk-bottom " + (s.bottom === "terminal" ? "wb-term" : open ? "wb-thread" : "wb-bar1");
  setHTML(r, walkPrBottomHtml());
  d.style.bottom = r.className.includes("wb-bar1") ? r.offsetHeight + "px" : "32%";
  const log = document.getElementById("walk-chatlog");
  if (log && !log.dataset.seen) { log.scrollTop = log.scrollHeight; log.dataset.seen = "1"; }
}

// ── events ──────────────────────────────────────────────

function walkPrClick(e) {
  const b = e.target.closest("[data-wp]");
  const ln = e.target.closest(".wk-ln[data-ln]");
  const box = e.target.closest(".cmt[data-cid]");
  if (!b && !ln) {
    if (box && walkPr.sel !== box.dataset.cid) { walkPr.sel = box.dataset.cid; dockPaint(); }
    return;
  }
  if (!walkPr.s) return;
  const s = walkPr.s;
  if (ln) {
    const it = dockCurrent();
    const n = Number(ln.dataset.ln);
    if (!it || it.kind !== "file" || !n) return;
    if (s.comments.some(c => c.file === it.path && c.line === n)) return;
    walkPr.editing = { file: it.path, line: n };
    dockPaint();
    const t = document.getElementById("walk-new");
    if (t) t.focus();
    return;
  }
  const act = b.dataset.wp, c = walkPrComment(b.dataset.cid);
  if (act === "save") return walkPrSaveNew();
  if (act === "cancel") { walkPr.editing = null; dockPaint(); return; }
  if (act === "drop" && c) { s.comments.splice(s.comments.indexOf(c), 1); if (c.who === "agent") (s.gone = s.gone || []).push(c.id); walkPrSave(); dockPaint(); return; }
  if (act === "post" && c) return walkPrPost(c);
  if (act === "toggle") { s.open = !s.open; s.bottom = "thread"; walkPrSave(); walkPrBottom(); return; }
  if (act === "mode") {
    s.bottom = b.dataset.mode;
    if (s.bottom === "terminal" && typeof term !== "undefined" && term) setTimeout(() => term.focus(), 0);
    walkPrSave(); walkPrBottom(); return;
  }
  if (act === "chip") {
    const k = b.dataset.k, i = s.off.indexOf(k);
    if (i >= 0) s.off.splice(i, 1); else s.off.push(k);
    walkPrSave(); walkPrBottom(); return;
  }
  if (act === "whole-send") return walkPrSendWhole();
}

function walkPrSaveNew() {
  const ed = walkPr.editing, t = document.getElementById("walk-new");
  if (!ed || !t) return;
  const text = t.value.trim();
  walkPr.drafts.delete("new:" + ed.file + ":" + ed.line);
  walkPr.editing = null;
  if (text) {
    const c = { id: "c" + Date.now().toString(36), file: ed.file, line: ed.line, who: "you", text, turns: [], post: null };
    walkPr.s.comments.push(c);
    walkPr.sel = c.id;
    walkPrSave();
  }
  dockPaint();
}

function walkPrSendReply(t) {
  const c = walkPrComment(t.dataset.wpk.slice(6)), v = t.value.trim();
  if (!c || !v) return;
  walkPr.sel = c.id;
  if (!walkPrSend(v)) return;
  c.turns = (c.turns || []).concat({ who: "you", text: v });
  walkPr.drafts.delete(t.dataset.wpk);
  walkPrSave();
  dockPaint();
}

function walkPrSendWhole() {
  const t = document.querySelector('#walk-bottom [data-wpk="whole"]');
  const v = t && t.value.trim();
  if (!v || !walkPrSend(v)) return;
  walkPr.s.thread.push({ q: v, scope: walkPrScopeText(), at: Date.now() });
  walkPr.drafts.delete("whole");
  walkPrSave();
  walkPrBottom();
  const log = document.getElementById("walk-chatlog");
  if (log) log.scrollTop = log.scrollHeight;
}

// The copy and the tab both start inside the click, the only place a browser allows them.
async function walkPrPost(c) {
  const f = walkPrForge();
  if (!f) return;
  const w = window.open("", "_blank");
  await walkCopy(c.text, "comment copied, paste it on " + f.f.label);
  const head = ((walkTenant.branch && walkTenant.branch.head) || "").slice(0, 40);
  c.post = f.f.cmd(f, c, head);
  walkPrSave();
  dockPaint();
  try {
    const u = await f.f.anchor(f, c);
    if (w) w.location = u;
  } catch (e) { if (w) w.close(); }
}

function walkPrInput(e) {
  const t = e.target;
  if (t && t.dataset && t.dataset.wpk) walkPr.drafts.set(t.dataset.wpk, t.value);
}

// Enter sends and Shift+Enter is a new line, which a textarea makes by itself. Ctrl+Enter makes it by hand.
function walkPrKeydown(e) {
  const t = e.target;
  if (!t || t.tagName !== "TEXTAREA" || !t.dataset.wpk) return;
  if (e.key === "Escape" && t.id === "walk-new") { e.preventDefault(); e.stopPropagation(); walkPr.editing = null; dockPaint(); return; }
  if (e.key !== "Enter" || e.shiftKey || e.isComposing) { e.stopPropagation(); return; }
  e.preventDefault();
  e.stopPropagation();
  if (e.ctrlKey || e.metaKey) {
    t.setRangeText("\n", t.selectionStart, t.selectionEnd, "end");
    t.dispatchEvent(new Event("input", { bubbles: true }));
    return;
  }
  if (t.id === "walk-new") walkPrSaveNew();
  else if (t.dataset.wpk === "whole") walkPrSendWhole();
  else walkPrSendReply(t);
}

// [ and ] step through the agent's comments, wherever they are.
function walkPrStepComment(dir) {
  const all = walkPrAgentComments().sort((a, b) => a.file < b.file ? -1 : a.file > b.file ? 1 : a.line - b.line);
  walkPrSync();
  if (!all.length) return;
  const now = all.findIndex(c => c.id === walkPr.sel);
  const c = all[Math.max(0, Math.min(all.length - 1, (now < 0 ? (dir > 0 ? -1 : all.length) : now) + dir))];
  const it = dock.items.find(i => i.kind === "file" && i.path === c.file);
  if (!it) return;
  walkPr.sel = c.id;
  const li = it.file.lines.findIndex(l => l.n === c.line && l.t !== "-");
  walkTenant.at = li >= 0 ? { key: it.key, li } : null;
  if (dock.cur !== it.key) dockSelect(it.key); else dockPaint();
  const el = document.querySelector("#walk-item .cmt.sel");
  if (el) el.scrollIntoView({ block: "center" });
}

function walkPrCommentAt() {
  const all = walkPrAgentComments().sort((a, b) => a.file < b.file ? -1 : a.file > b.file ? 1 : a.line - b.line);
  const i = all.findIndex(c => c.id === walkPr.sel);
  return "agent comment " + (i < 0 ? "-" : i + 1) + " of " + all.length;
}

// A bubble's corner drag changes its width and height, which are kept with the comment.
document.addEventListener("pointerup", () => {
  if (!walkPr.s) return;
  let changed = false;
  document.querySelectorAll("#walk-item .cmt[data-cid]").forEach(n => {
    const c = walkPrComment(n.dataset.cid);
    if (c && ((n.style.width || "") !== (c.w || "") || (n.style.height || "") !== (c.h || ""))) {
      c.w = n.style.width; c.h = n.style.height; changed = true;
    }
  });
  if (changed) walkPrSave();
});

window.addEventListener("focus", () => { if (dock.open) walkPrSync(); });

(function walkPrWire() {
  const pf = document.getElementById("pulls-pop-walk");
  if (pf) pf.addEventListener("change", async () => {
    const f = pf.files[0];
    pf.value = "";
    if (f) walkPrImportText(await f.text());
  });
  const item = document.getElementById("walk-item");
  if (!item) return;
  item.addEventListener("click", walkPrClick);
  item.addEventListener("keydown", walkPrKeydown);
  item.addEventListener("input", walkPrInput);
})();

// ── share: walk.json out, walk.json in ───────────────────

function walkPrExport() {
  const f = walkPrForge(), b = walkTenant.branch || {}, s = walkPr.s;
  return {
    atrium: "walk", v: 1, exported: new Date().toISOString(),
    pr: { forge: f ? f.kind : "", url: f ? f.url : "", repo: f ? f.repo : "", num: f ? f.num : "", branch: b.branch || "", base: b.base || "", head: b.head || "" },
    comments: s.comments, thread: s.thread, off: s.off
  };
}

// What a walker starts with on a PR it has never seen: the comments and threads, as plain text.
function walkPrContext(doc) {
  const out = ["Somebody walked this PR and sent you their walk. Their comments and threads are below, anchored file:line. Use them as your context, and do not post anything."];
  (doc.comments || []).forEach(c => {
    out.push(`- ${c.who} comment at ${c.file}:${c.line}: ${c.text}`);
    (c.turns || []).forEach(t => out.push(`    ${t.who}: ${t.text}`));
  });
  (doc.thread || []).forEach(t => out.push(`- whole-PR question: ${t.q}`));
  return out.join("\n").slice(0, 8000);
}

function walkPrTake(doc) {
  const s = walkPr.s;
  let added = 0;
  (doc.comments || []).forEach(c => {
    if (!c || !c.file || !c.line || !c.text) return;
    if (s.comments.some(x => x.file === c.file && x.line === c.line && x.text === c.text)) return;
    s.comments.push(Object.assign({ turns: [], post: null }, c, { id: c.id && !walkPrComment(c.id) ? c.id : "c" + Date.now().toString(36) + added }));
    added++;
  });
  (doc.thread || []).forEach(t => { if (t && t.q && !s.thread.some(x => x.q === t.q && x.at === t.at)) s.thread.push(t); });
  walkPrSave();
  return added;
}

// Same PR: merged in. Another PR: opens it, and the walk becomes the new walker's context.
async function walkPrImportText(text) {
  let doc;
  try { doc = JSON.parse(text); } catch (e) { doc = null; }
  if (!doc || doc.atrium !== "walk" || !Array.isArray(doc.comments)) {
    toast("that is not a walk", "a walk.json has atrium: walk and a list of comments");
    return;
  }
  const here = dock.open && walkPr.s ? walkPrForge() : null;
  if (walkPr.s && dock.open && (!doc.pr || !doc.pr.url || (here && here.url === doc.pr.url))) {
    const n = walkPrTake(doc);
    toast("walk imported", n + " new comments, " + (doc.comments.length - n) + " already here");
    dockPaint();
    return;
  }
  if (!doc.pr || !doc.pr.url) { toast("no PR in that walk", "open the card it was made on, then import it"); return; }
  const id = typeof linkOpeningId === "function" ? linkOpeningId() : "";
  if (id) linkOpeningBegin(id, doc.pr.url);
  let out;
  try {
    out = await api("/v1/open", {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ progress: id, url: doc.pr.url, why: walkPrContext(doc) })
    });
  } catch (e) {
    if (id) linkOpeningFail(id, (e && e.message) || "could not open the PR");
    return;
  }
  if (id) linkOpeningEnd(id);
  if (!out || !out.card) { toast("the PR did not open", "no card was made for it"); return; }
  const state = Object.assign(walkPrBlank(), { comments: doc.comments, thread: doc.thread || [], off: doc.off || [] });
  try {
    await api(`/v1/tasks/${out.card}/walk`, { method: "PUT", headers: { "Content-Type": "application/json" }, body: JSON.stringify(state) });
  } catch (e) { toast("the walk was not kept", (e && e.message) || ""); }
  dock.want = out.card;
  await attachTask(out.card);
}

function walkShareToggle(on) {
  let pop = document.getElementById("walk-share-pop");
  if (!pop) {
    pop = document.createElement("div");
    pop.id = "walk-share-pop"; pop.className = "walk-share-pop"; pop.hidden = true;
    pop.innerHTML = `<p>Send this walk to another dev. It is one file, <code>walk.json</code>: your comments, their threads and the
      whole-PR thread, with the PR it is about. Opening it on any atrium shows the walk and starts a walker on that PR with all of it as context.</p>
      <div class="row2"><button type="button" data-wp="share-save">Download walk.json</button><button type="button" data-wp="share-copy">Copy it</button></div>
      <p>Open a walk somebody sent you, or your own saved one:</p>
      <div class="row2"><input type="file" id="walk-share-file" accept=".json,application/json" aria-label="a walk.json">
      <button type="button" data-wp="share-import">Import</button></div>
      <textarea id="walk-share-paste" rows="3" spellcheck="false" placeholder="or paste the walk.json here"></textarea>`;
    document.getElementById("walk-drawer").appendChild(pop);
    pop.addEventListener("click", walkShareClick);
  }
  pop.hidden = on === undefined ? !pop.hidden : !on;
}

async function walkShareClick(e) {
  const b = e.target.closest("[data-wp]");
  if (!b || !walkPr.s) return;
  const a = b.dataset.wp;
  if (a === "share-save" || a === "share-copy") {
    const text = JSON.stringify(walkPrExport(), null, 2);
    if (a === "share-copy") { await walkCopy(text, "walk.json copied"); return; }
    const link = document.createElement("a");
    link.href = URL.createObjectURL(new Blob([text], { type: "application/json" }));
    link.download = "walk.json";
    link.click();
    URL.revokeObjectURL(link.href);
    return;
  }
  if (a === "share-import") {
    const file = document.getElementById("walk-share-file").files[0];
    const paste = document.getElementById("walk-share-paste").value.trim();
    const text = file ? await file.text() : paste;
    if (!text) { toast("nothing to import", "choose a walk.json or paste one"); return; }
    walkShareToggle(false);
    walkPrImportText(text);
  }
}