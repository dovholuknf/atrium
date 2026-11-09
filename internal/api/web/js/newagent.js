// ── the new agent dialog ─────────────────────
//
// WHAT FIRST, HOW ON ONE LINE. The repo, the name and the groups come first, the room, runner, model, effort and setup
// fold into one summary line whose defaults are the last ones saved. A card always lives under the room's card root:
//   a repo                 POST /v1/launch with repo host/org/repo, which makes a worktree home on `branch`.
//   a pull request, issue  POST /v1/open, as a pasted link always has: the room names the branch.
//   no repo                POST /v1/launch with home, a scratch home.
//   quick throwaway        POST /v1/launch with throwaway, a home that goes when the session ends. `keep` on its card
//                          keeps it where it is (naKeepOpen).
// A branch that is already there is asked about before the launch (GET /v1/branches/check). The `launch` dialog in
// js/fixtures.js still serves resume, the switcher's links and starting onto a card.

const NA_HOW_KEY = "atrium.newagent.how";
const NA_GROUPS_KEY = "atrium.newagent.groups";
const NA_RECENT_GROUPS_KEY = "atrium.newagent.recentgroups";
const NA_EFFORTS = ["", "low", "medium", "high", "max"];
const NA_SETUPS = [["full", "full setup"], ["lean", "lean"], ["superlean", "superlean"]];

let naS = null;
let naCheckSeq = 0;
let naCheckTimer = 0;

const naEl = id => document.getElementById(id);
function naRead(key, dflt) {
  try { return JSON.parse(localStorage.getItem(key) || "null") || dflt; } catch (e) { return dflt; }
}
function naWrite(key, v) {
  try { localStorage.setItem(key, JSON.stringify(v)); } catch (e) {}
}
function naSlug(s, n) {
  const full = String(s || "").toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-+|-+$/g, "");
  if (full.length <= n) return full;
  const cut = full.slice(0, n + 1), at = cut.lastIndexOf("-");
  return (at > n / 2 ? cut.slice(0, at) : cut.slice(0, n)).replace(/-+$/, "");
}
const naFirstLine = s => (String(s || "").split("\n")[0] || "").trim();
const naClip = (s, n) => s.length > n ? s.slice(0, n - 1).trimEnd() + "…" : s;
// What a repo is called on screen: owner/repo on github, host/owner/repo elsewhere, a folder's name for a path.
function naShort(ref) {
  if (/^[a-z]:[\\/]|^\//i.test(ref)) return ref.replace(/[\\/]+$/, "").split(/[\\/]/).pop();
  return ref.replace(/^github(\.com)?\//i, "");
}

// The rooms a launch can choose between: only on the hub's all-rooms view with two or more. Otherwise the launch goes
// where the board is, and the summary names it.
function naRooms() {
  const rooms = (typeof hubRooms === "undefined" ? [] : hubRooms) || [];
  const scoped = typeof roomNow === "function" ? roomNow() : "";
  return typeof hubIsHub !== "undefined" && hubIsHub && !scoped && rooms.length > 1 ? rooms.map(r => r.name) : [];
}
function naRoomName() {
  const scoped = typeof roomNow === "function" ? roomNow() : "";
  const rooms = (typeof hubRooms === "undefined" ? [] : hubRooms) || [];
  return naS.how.room || scoped || (rooms.length === 1 ? rooms[0].name : "") || "this room";
}
function naHarness() {
  const all = launchableHarnesses(naS.how.room || "");
  return all.find(h => h.id === naS.how.harness) || all[0] || null;
}
const naCanModel = h => !!(h && ((h.model_args || []).length || (h.model_env || "").trim()));
const naCanEffort = h => !!(h && ((h.effort_args || []).length || (h.effort_env || "").trim()));
const naIsClaude = h => !!(h && hookTargetFor(h) === "claude");

// The saved default, with a room or runner that is gone replaced by what is there now.
function naDefaultHow() {
  const saved = naRead(NA_HOW_KEY, {});
  const rooms = naRooms();
  const last = typeof lastLaunchRoom === "function" ? lastLaunchRoom() : "";
  const room = rooms.includes(saved.room) ? saved.room : rooms.includes(last) ? last : rooms[0] || "";
  const usable = launchableHarnesses(room);
  const h = usable.find(x => x.id === saved.harness) || usable[0];
  return { room, harness: h ? h.id : "", model: saved.model || "", effort: saved.effort || "", setup: saved.setup || "full" };
}
function naSameAsDefault() {
  const d = naDefaultHow(), h = naS.how;
  return ["room", "harness", "model", "effort", "setup"].every(k => (d[k] || "") === (h[k] || ""));
}

// ── what was typed or pasted ─────────────────────
// {ref, link} or null. ref is host/owner/repo (owner/repo means github), link is a pull request, issue or branch.
function naParse(text) {
  const t = String(text || "").trim();
  if (!t) return null;
  let m = t.match(/^([\w.-]+)\/([\w.-]+)#(\d+)$/);
  if (m) return { ref: "github.com/" + m[1] + "/" + m[2], link: { kind: "issue", num: +m[3], url: "https://github.com/" + m[1] + "/" + m[2] + "/issues/" + m[3] } };
  if (/^[\w.-]+\/[\w.-]+$/.test(t) && !/^github(\.com)?\//i.test(t)) return { ref: "github.com/" + t, link: null };
  const p = parseRepoText(t, false);
  if (!p) return null;
  const ref = p.host + "/" + p.owner + "/" + p.repo;
  m = t.match(/\/(pull|pulls|issues)\/(\d+)/);
  if (m) return { ref, link: { kind: m[1] === "issues" ? "issue" : "pr", num: +m[2], url: t } };
  if (p.branch) return { ref, link: { kind: "branch", head: p.branch, url: t } };
  return { ref, link: null };
}

// ── derived values ─────────────────────
const naOpens = () => !!(naS.link && naS.link.num);
function naAutoTitle() {
  const what = naClip(naFirstLine(naS.what), 64);
  if (!naS.ref) return what;
  const r = naS.link, short = naShort(naS.ref);
  if (r && r.num) return r.title || (short + "#" + r.num + (what ? " " + what : ""));
  if (r && r.kind === "branch") return short + "@" + r.head + (what ? " " + what : "");
  return what ? short + " " + what : short;
}
const naTitle = () => naS.titleTouched ? naS.title : naAutoTitle();
const naStripRef = t => t.replace(/^\S+\/\S+?(?:#\d+|@\S+)?(\s+|$)/, "").trim();
function naAutoBranch() {
  if (!naS.ref || naOpens()) return "";
  if (naS.link && naS.link.kind === "branch") return naS.link.head;
  const named = naS.titleTouched ? naStripRef(naS.title) : naFirstLine(naS.what);
  return "claude/" + (naSlug(named, 38) || "work");
}
const naWantedBranch = () => naS.branchTouched ? naS.branch.trim() : naAutoBranch();
// The branch the launch goes on: a free name when the wanted one is held by another worktree, or exists and the choice
// is a new branch.
function naBranch() {
  const want = naWantedBranch(), c = naS.check;
  if (!c || c.branch !== want) return want;
  if (c.held_at || (c.exists && naS.brChoice !== "reuse")) return c.free || want;
  return want;
}

// ── the repo list ─────────────────────
function naRows() {
  const memo = launchRepoMemo[naS.how.room || ""];
  if (!memo) return null;
  return launchRepoRows(memo).map(r => {
    const ref = r.slug || (r.name && r.name.split("/").length === 3 ? r.name.replace(/^github\//i, "github.com/") : r.path);
    return { ref, name: naShort(ref || r.name), here: !r.slug, tip: r.tip };
  }).filter(r => r.ref);
}
function naRowOf(ref) {
  const rows = naRows() || [];
  const k = launchRepoKey(ref);
  return rows.find(r => launchRepoKey(r.ref) === k) || null;
}
function naListItems() {
  const raw = naEl("na-repo").value.trim(), q = raw.toLowerCase();
  const p = naParse(raw);
  const rows = naRows();
  if (!rows) return [{ empty: "reading the repos…" }];
  const match = rows.filter(r => !q || (p && launchRepoKey(r.ref) === launchRepoKey(p.ref)) ||
    (r.name + " " + r.tip).toLowerCase().includes(q));
  const here = match.filter(r => r.here), hub = match.filter(r => !r.here);
  const items = [];
  if (p && !rows.some(r => launchRepoKey(r.ref) === launchRepoKey(p.ref))) items.push({ ref: p.ref, name: naShort(p.ref), meta: "fetch it" });
  if (here.length) items.push({ sec: q ? "repos" : "recent" }, ...here.map(r => ({ ref: r.ref, name: r.name, meta: "" })));
  if (hub.length) items.push({ sec: "on the hub, not here" }, ...hub.map(r => ({ ref: r.ref, name: r.name, meta: "clone" })));
  if (!items.length) items.push({ empty: rows.length ? "nothing matches. type owner/repo or paste a link." : "no repos yet. type owner/repo or paste a link." });
  return items;
}
function naHL(name, q) {
  const i = q ? name.toLowerCase().indexOf(q.toLowerCase()) : -1;
  if (i < 0) return esc(name);
  return esc(name.slice(0, i)) + "<b>" + esc(name.slice(i, i + q.length)) + "</b>" + esc(name.slice(i + q.length));
}
function naPaintList() {
  const el = naEl("na-repo-list");
  el.hidden = !naS.listOpen;
  naEl("na-repo").setAttribute("aria-expanded", String(naS.listOpen));
  naSyncPop();
  if (!naS.listOpen) return;
  const q = naEl("na-repo").value.trim();
  const items = naListItems();
  const picks = items.filter(i => i.ref);
  naS.listIdx = Math.max(0, Math.min(naS.listIdx, picks.length - 1));
  let n = -1;
  el.innerHTML = items.map(i => {
    if (i.empty) return `<div class="empty">${esc(i.empty)}</div>`;
    if (i.sec) return `<div class="sec">${esc(i.sec)}</div>`;
    n++;
    return `<div class="cb-opt ${n === naS.listIdx ? "on" : ""}" data-n="${n}"><span class="nm">${naHL(i.name, q)}</span><span class="meta">${esc(i.meta)}</span></div>`;
  }).join("");
  el.querySelectorAll(".cb-opt").forEach(o => o.onmousedown = e => { e.preventDefault(); naPick(picks[+o.dataset.n]); });
  const on = el.querySelector(".cb-opt.on");
  if (on) on.scrollIntoView({ block: "nearest" });
}
function naPick(it) {
  naS.listOpen = false;
  if (!it) { naRender(); return; }
  const p = naParse(naEl("na-repo").value);
  naSetRepo(it.ref, p && launchRepoKey(p.ref) === launchRepoKey(it.ref) ? p.link : null);
  naEl("na-what").focus();
}

function naSetRepo(ref, link) {
  naS.ref = ref || "";
  naS.link = link ? Object.assign({}, link) : null;
  naS.brChoice = link && link.kind === "branch" ? "reuse" : "new";
  naEl("na-repo").value = ref ? naShort(ref) + (link && link.num ? "#" + link.num : link && link.head ? "@" + link.head : "") : "";
  if (naS.link && naS.link.num) naRecognise(naS.link);
  naRender();
}

// A pull request or issue is read for its title, the same /v1/recognise a pasted link always asks.
async function naRecognise(link) {
  link.reading = true;
  try {
    const got = await api("/v1/recognise", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ url: link.url }) });
    link.title = (got && got.title) || "";
    link.label = (got && (got.label || got.recogniser)) || "";
  } catch (e) {
    link.problem = /404|no recogniser/i.test(e.message) ? "nothing here knows that link yet" : e.message;
  }
  link.reading = false;
  if (naS && naS.link === link) naRender();
}

// ── the branch check ─────────────────────
function naQueueCheck() {
  clearTimeout(naCheckTimer);
  const want = naWantedBranch();
  if (naS.tw || !naS.ref || naOpens() || !want) { naS.check = null; return; }
  if (naS.check && naS.check.branch === want && naS.check.ref === naS.ref) return;
  const seq = ++naCheckSeq, ref = naS.ref, room = naS.how.room;
  naCheckTimer = setTimeout(async () => {
    let got = null;
    try {
      got = await api("/v1/branches/check?repo=" + encodeURIComponent(ref) + "&branch=" + encodeURIComponent(want),
        { headers: launchRepoHeaders(room) });
    } catch (e) { got = null; }
    if (seq !== naCheckSeq || !naS) return;
    naS.check = got ? Object.assign(got, { ref }) : null;
    naRender();
  }, 250);
}
function naAgo(iso) {
  const s = (Date.now() - Date.parse(iso)) / 1000;
  if (!(s >= 0)) return "";
  if (s < 3600) return Math.max(1, Math.round(s / 60)) + " min ago";
  if (s < 86400) return Math.round(s / 3600) + " hours ago";
  return Math.round(s / 86400) + " days ago";
}
function naHolder(id) {
  const bare = String(id || "").replace(/^.*~/, "");
  return bare ? (lastTasks || []).find(t => String(t.id).replace(/^.*~/, "") === bare) : null;
}
function naPaintConflicts() {
  const el = naEl("na-conf");
  const c = naS.check, want = naWantedBranch();
  if (!c || c.branch !== want || naS.tw) { el.innerHTML = ""; return; }
  if (c.held_at) {
    const t = naHolder(c.held_by);
    const who = t ? `the card <b>${esc(t.display_title || t.title || t.id)}</b>` : `<code>${esc(c.held_at)}</code>`;
    el.innerHTML = `<div class="cf"><div class="msg"><code>${esc(want)}</code> is checked out by ${who}. git keeps a branch in one worktree at a time, so this card gets <code>${esc(c.free)}</code>.</div>` +
      (t ? `<div class="act"><button type="button" class="linkbtn" id="na-open-holder">open that card instead</button></div>` : "") + `</div>`;
    const b = naEl("na-open-holder");
    if (b) b.onclick = () => { naEl("na").close(); switchView("terms"); openTerm(t); };
    return;
  }
  if (c.exists) {
    const last = c.last ? ", the last one " + naAgo(c.last) : "";
    el.innerHTML = `<div class="cf"><div class="msg">the branch <code>${esc(want)}</code> already exists: ${c.ahead} commits past its base${esc(last)}.</div>` +
      `<div class="act"><div class="seg"><button type="button" data-v="new" class="${naS.brChoice !== "reuse" ? "on" : ""}">new branch <code>${esc(c.free)}</code></button>` +
      `<button type="button" data-v="reuse" class="${naS.brChoice === "reuse" ? "on" : ""}">continue on <code>${esc(want)}</code></button></div></div></div>`;
    el.querySelectorAll(".seg button").forEach(b => b.onclick = () => { naS.brChoice = b.dataset.v; naRender(); });
    return;
  }
  el.innerHTML = "";
}

// ── groups ─────────────────────
// The groups a card can go in: the ones picked, the ones used lately, the board's own named groups, and every tag a card
// carries that is not atrium's (those have a colon).
function naGroupChoices(sel) {
  const p = typeof groupingPrefs === "function" ? groupingPrefs() : {};
  const named = p.mode === "custom" && Array.isArray(p.groups) ? p.groups : [];
  const tags = [];
  (lastTasks || []).forEach(t => (t.tags || []).forEach(g => { if (!String(g).includes(":")) tags.push(g); }));
  return [...new Set([...sel, ...naRead(NA_RECENT_GROUPS_KEY, []), ...named, ...tags])];
}
function naGroupCount(g) { return (lastTasks || []).filter(t => (t.tags || []).includes(g)).length; }
function naSyncPop() {
  document.querySelectorAll("#na .dlg-body, #na-keep .dlg-body").forEach(b =>
    b.classList.toggle("popped", !!b.querySelector('.gpick[data-open="1"], #na-repo-list:not([hidden])')));
}
function naPaintGroups(root, sel, set) {
  root._paint = () => naPaintGroups(root, sel, set);
  const open = root.dataset.open === "1";
  const all = naGroupChoices(sel);
  root.innerHTML =
    `<button type="button" class="gp-field" aria-expanded="${open}" aria-haspopup="listbox">` +
      (sel.length ? sel.map(g => `<span class="chip accent">${esc(g)}</span>`).join("") : `<span class="ph">no group</span>`) +
      `<span class="caret">▾</span></button>` +
    `<button type="button" class="gp-add" title="a new group">+</button>` +
    (open ? `<div class="gp-list" role="listbox">` + (all.length ? all.map(g =>
      `<label class="check"><input type="checkbox" data-g="${esc(g)}" ${sel.includes(g) ? "checked" : ""}><span>${esc(g)}</span>` +
      `<span class="n">${naGroupCount(g) || ""}</span></label>`).join("") : `<div class="empty">no groups yet. + makes one.</div>`) + `</div>` : "");
  naSyncPop();
  root.querySelector(".gp-field").onclick = () => {
    root.dataset.open = open ? "" : "1";
    root._paint();
    const c = !open && root.querySelector(".gp-list input");
    if (c) c.focus();
  };
  root.querySelectorAll(".gp-list input").forEach(c => c.onchange = () => {
    const g = c.dataset.g;
    set(c.checked ? [...sel, g] : sel.filter(x => x !== g));
    const again = root.querySelector(`.gp-list input[data-g="${CSS.escape(g)}"]`);
    if (again) again.focus();
  });
  root.querySelector(".gp-add").onclick = () => {
    root.dataset.open = "";
    const i = document.createElement("input");
    i.type = "text"; i.placeholder = "new group"; i.className = "gp-new";
    const list = root.querySelector(".gp-list");
    if (list) list.remove();
    naSyncPop();
    root.querySelector(".gp-add").replaceWith(i);
    i.focus();
    let done = false;
    const finish = keep => {
      if (done) return;
      done = true;
      const v = i.value.trim();
      if (keep && v) set(sel.includes(v) ? sel : [...sel, v]);
      else root._paint();
    };
    i.onkeydown = e => {
      if (e.key === "Enter") { e.preventDefault(); e.stopPropagation(); finish(true); }
      if (e.key === "Escape") { e.preventDefault(); e.stopPropagation(); finish(false); }
    };
    i.onblur = () => finish(true);
  };
  root.onkeydown = e => {
    if (e.key === "Escape" && root.dataset.open === "1") {
      e.preventDefault(); e.stopPropagation();
      root.dataset.open = "";
      root._paint();
      root.querySelector(".gp-field").focus();
    }
  };
}
document.addEventListener("mousedown", e => {
  document.querySelectorAll('#na .gpick[data-open="1"], #na-keep .gpick[data-open="1"]').forEach(r => {
    if (!r.contains(e.target)) { r.dataset.open = ""; r._paint(); }
  });
});

// ── how it runs ─────────────────────
function naSummary() {
  const h = naHarness(), how = naS.how;
  if (!h) return "<b>" + esc(naRoomName()) + "</b> · no runner here";
  const parts = ["<b>" + esc(naRoomName()) + "</b>", esc(h.label || h.id)];
  if (naCanModel(h) && how.model) parts.push(esc(naModelLabel(how.model)));
  if (naCanEffort(h) && how.effort) parts.push(esc(how.effort) + " effort");
  if (naIsClaude(h)) parts.push(esc((NA_SETUPS.find(s => s[0] === how.setup) || NA_SETUPS[0])[1]));
  return parts.join(" · ") + (naSameAsDefault() ? "" : ' <span class="ch">• changed</span>');
}
function naModelLabel(id) {
  const m = (modelPicker || []).find(x => (x.ID || x.model) === id);
  return m ? (m.Label || m.label || id) : id;
}
function naPaintHow() {
  const el = naEl("na-how"), how = naS.how, h = naHarness();
  const rows = [];
  const seg = (k, opts) => `<div class="seg" data-k="${k}">` + opts.map(([v, label, sub]) =>
    `<button type="button" data-v="${esc(v)}" class="${(how[k] || "") === v ? "on" : ""}">${esc(label)}${sub ? `<span class="sub">${esc(sub)}</span>` : ""}</button>`).join("") + `</div>`;
  const rooms = naRooms();
  if (rooms.length) rows.push(["room", seg("room", rooms.map(r => [r, r]))]);
  const usable = launchableHarnesses(how.room || "");
  rows.push(["runner", usable.length ? seg("harness", usable.map(x => [x.id, x.label || x.id]))
    : `<span class="none">no runner on ${esc(naRoomName())} is turned on and installed</span>`]);
  if (naCanModel(h)) rows.push(["model", seg("model", [["", "default"]].concat((modelPicker || []).map(m => [m.ID || m.model, m.Label || m.label || m.ID || m.model])))]);
  if (naCanEffort(h)) rows.push(["effort", seg("effort", NA_EFFORTS.map(v => [v, v || "default"]))]);
  if (naIsClaude(h)) rows.push(["setup", seg("setup", NA_SETUPS)]);
  const same = naSameAsDefault();
  const save = `<div class="savebar">` + (naS.savedFlash ? `<span class="ok">✓ saved: every new agent starts like this</span>`
    : same ? "" : `<span>changed for this launch only</span>`) +
    `<button type="button" id="na-save" ${same ? "disabled" : ""}>save as default</button></div>`;
  el.innerHTML = rows.map(([k, body], i) => `<span class="k">${k}</span><div class="rowend">${body}${i === rows.length - 1 ? save : ""}</div>`).join("");
  el.querySelectorAll(".seg").forEach(sg => sg.querySelectorAll("button").forEach(b => b.onclick = () => {
    naS.how[sg.dataset.k] = b.dataset.v;
    if (sg.dataset.k === "room") {
      const u = launchableHarnesses(b.dataset.v);
      if (!u.some(x => x.id === naS.how.harness)) naS.how.harness = u[0] ? u[0].id : "";
      naS.check = null;
      naLoadRepos();
    }
    naS.savedFlash = false;
    naRender();
  }));
  naEl("na-save").onclick = () => {
    naWrite(NA_HOW_KEY, Object.assign({}, naS.how));
    naS.savedFlash = true;
    naRender();
  };
}

// ── paint ─────────────────────
function naRender() {
  if (!naS) return;
  const d = naEl("na");
  d.classList.toggle("tw-mode", naS.tw);
  naEl("na-h").textContent = naS.tw ? "⚡ quick throwaway" : "new agent";
  naEl("na-repo-clr").hidden = !naEl("na-repo").value;

  const res = naEl("na-resolved"), r = naS.link, room = esc(naRoomName());
  const row = naS.ref ? naRowOf(naS.ref) : null;
  if (r && r.num) {
    const k = r.kind === "pr" ? "pr" : "issue";
    res.innerHTML = r.reading ? `<span class="spin"></span><span class="txt">reading ${esc(naShort(naS.ref))}#${r.num}…</span>`
      : `<span class="kind ${k}">${k} #${r.num}</span><span class="txt">${esc(r.title || r.problem || "")}${r.title ? " · " : ""}${r.title ? "the room names its branch" : ""}</span>`;
  } else if (r && r.kind === "branch") {
    res.innerHTML = `<span class="kind branch">branch</span><span class="txt">continues <b>${esc(r.head)}</b></span>`;
  } else if (naS.ref && !naRows()) {
    res.innerHTML = "";
  } else if (naS.ref && !row) {
    res.innerHTML = `<span class="kind clone">fetch</span><span class="txt">not on the hub yet: fetched from ${esc(naS.ref.split("/")[0])} first</span>`;
  } else if (naS.ref && !row.here) {
    res.innerHTML = `<span class="kind clone">clone</span><span class="txt">on the hub, not on ${room}: cloned first</span>`;
  } else {
    res.innerHTML = naS.ref ? `<span class="txt" style="color:var(--dim)">a new worktree under the card root</span>` : "";
  }

  naPaintGroups(naEl("na-groups"), naS.groups, gs => { naS.groups = gs; naRender(); });

  const t = naTitle();
  if (document.activeElement !== naEl("na-title")) naEl("na-title").value = t;
  naQueueCheck();
  const br = naBranch();
  if (document.activeElement !== naEl("na-branch")) naEl("na-branch").value = br;
  naEl("na-brline").style.visibility = naS.ref && !naOpens() ? "" : "hidden";
  naEl("na-brline").classList.toggle("moved", br !== naWantedBranch());
  naPaintConflicts();
  naEl("na-need").hidden = !!t.trim();
  if (t.trim()) naEl("na-title").classList.remove("missing");
  naEl("na-reset").hidden = !(naS.titleTouched || naS.branchTouched) || !naS.ref;

  naEl("na-sum").innerHTML = naSummary();
  naEl("na-change").textContent = naS.howOpen ? "close" : "change";
  naEl("na-change").setAttribute("aria-expanded", String(naS.howOpen));
  naEl("na-how").hidden = !naS.howOpen;
  if (naS.howOpen) naPaintHow();
  naEl("na-go").firstElementChild.textContent = naS.tw ? "start throwaway" : "launch";
  naEl("na-go").disabled = !naHarness();
  naPaintList();
}
function naSay(text, warn) {
  const n = naEl("na-note");
  n.textContent = text || "";
  n.classList.toggle("warn", !!warn);
}

async function naLoadRepos() {
  const room = naS.how.room || "";
  if (await loadLaunchRepos(room) && naS && naEl("na").open && (naS.how.room || "") === room) naRender();
}

// ── open ─────────────────────
async function openNewAgent(tw, prefill) {
  if (!allHarnesses.length) await loadHarnesses();
  if (!allHarnesses.some(h => h.enabled)) { openLaunch(); return; }
  naS = {
    tw: !!tw, ref: "", link: null, what: "", title: "", titleTouched: false, branch: "", branchTouched: false,
    groups: naRead(NA_GROUPS_KEY, []), how: naDefaultHow(), howOpen: false, savedFlash: false,
    listOpen: false, listIdx: 0, brChoice: "new", check: null
  };
  naEl("na-what").value = "";
  naEl("na-repo").value = "";
  naEl("na-title").classList.remove("missing");
  naEl("na-groups").dataset.open = "";
  naSay("");
  naGrowWhat();
  naEl("na").showModal();
  naRender();
  naLoadRepos();
  const p = prefill ? naParse(prefill) : null;
  if (p) naSetRepo(p.ref, p.link);
  if (p || tw) naEl("na-what").focus();
  else naEl("na-repo").focus();
}

naEl("na-repo").addEventListener("focus", () => { naS.listOpen = true; naS.listIdx = 0; naRender(); });
naEl("na-repo").addEventListener("blur", () => {
  setTimeout(() => {
    if (!naS || !naS.listOpen) return;
    naS.listOpen = false;
    const p = naParse(naEl("na-repo").value);
    if (p && (p.ref !== naS.ref || JSON.stringify(p.link) !== JSON.stringify(naS.link && { kind: naS.link.kind, num: naS.link.num, url: naS.link.url, head: naS.link.head }))) naSetRepo(p.ref, p.link);
    else if (!naEl("na-repo").value.trim()) { naS.ref = ""; naS.link = null; }
    naRender();
  }, 0);
});
naEl("na-repo").addEventListener("input", () => {
  const p = naParse(naEl("na-repo").value);
  if (p && p.link) { naS.listOpen = false; naSetRepo(p.ref, p.link); naEl("na-what").focus(); return; }
  naS.listOpen = true;
  naS.listIdx = 0;
  naRender();
});
naEl("na-repo").addEventListener("keydown", e => {
  const picks = naListItems().filter(i => i.ref);
  if (e.key === "ArrowDown") { e.preventDefault(); naS.listOpen = true; naS.listIdx = Math.min(naS.listIdx + 1, picks.length - 1); naPaintList(); }
  else if (e.key === "ArrowUp") { e.preventDefault(); naS.listIdx = Math.max(naS.listIdx - 1, 0); naPaintList(); }
  else if (e.key === "Enter" && naS.listOpen && !(e.ctrlKey || e.metaKey)) { e.preventDefault(); e.stopPropagation(); naPick(picks[naS.listIdx]); }
  else if (e.key === "Escape" && naS.listOpen) { e.preventDefault(); e.stopPropagation(); naS.listOpen = false; naRender(); }
});
naEl("na-repo-clr").onclick = () => { naS.ref = ""; naS.link = null; naS.check = null; naEl("na-repo").value = ""; naEl("na-repo").focus(); };

// A link pasted into the what box fills the repo, and leaves the box empty when the link was all it held.
naEl("na-what").addEventListener("input", () => {
  const v = naEl("na-what").value, url = (v.match(/https?:\/\/\S+/i) || [])[0];
  const p = url && !naS.tw ? naParse(url) : null;
  if (p && (p.ref !== naS.ref || (p.link && (!naS.link || p.link.url !== naS.link.url)))) {
    if (v.trim() === url) naEl("na-what").value = "";
    naSetRepo(p.ref, p.link);
  }
  naS.what = naEl("na-what").value;
  naGrowWhat();
  naRender();
});
function naGrowWhat() {
  const w = naEl("na-what");
  w.style.height = "36px";
  if (w.scrollHeight > w.clientHeight) w.style.height = Math.min(w.scrollHeight + 2, 132) + "px";
  w.style.overflowY = w.scrollHeight > 132 ? "auto" : "hidden";
}
naEl("na-title").addEventListener("input", () => { naS.title = naEl("na-title").value; naS.titleTouched = true; naRender(); });
naEl("na-branch").addEventListener("input", () => { naS.branch = naEl("na-branch").value; naS.branchTouched = true; naRender(); });
naEl("na-reset").onclick = () => { naS.titleTouched = naS.branchTouched = false; naRender(); };
naEl("na-change").onclick = () => { naS.howOpen = !naS.howOpen; naRender(); };
naEl("na-x").onclick = () => naEl("na").close();
naEl("na-full").onclick = () => { naS.tw = false; naRender(); naEl("na-repo").focus(); };
naEl("na-go").onclick = () => naLaunch(naS.tw);
naEl("na-tw").onclick = () => naLaunch(true);
naEl("na").addEventListener("keydown", e => {
  if (e.key !== "Enter" || e.isComposing) return;
  const t = e.target;
  if (t.id === "na-what" && e.shiftKey) return;
  if (t.tagName === "BUTTON" || (t.closest && t.closest(".gpick"))) return;
  e.preventDefault();
  naLaunch(e.ctrlKey || e.metaKey ? true : naS.tw);
});

// ── launch ─────────────────────
function naBody(h) {
  const how = naS.how, body = { harness: h.id, prompt: naEl("na-what").value.trim() };
  if (naCanModel(h) && how.model) body.model = how.model;
  if (naCanEffort(h) && how.effort) body.effort = how.effort;
  if (naIsClaude(h) && how.setup === "lean") body.lean = true;
  if (naIsClaude(h) && how.setup === "superlean") body.superlean = true;
  return body;
}

async function naLaunch(tw) {
  const h = naHarness();
  if (!h) return;
  const title = naTitle().trim();
  if (!tw && !title) {
    naEl("na-title").classList.add("missing");
    naEl("na-title").focus();
    return;
  }
  naS.tw = !!tw;
  const room = naS.how.room || "";
  const headers = launchRepoHeaders(room, { "Content-Type": "application/json" });
  const body = naBody(h);
  return busyWhile("na-go", async () => {
    naSay("");
    let task;
    try {
      if (tw) {
        const what = naFirstLine(body.prompt);
        task = await api("/v1/launch", { method: "POST", headers,
          body: JSON.stringify(Object.assign(body, { throwaway: true, cwd: "", title: naClip(what, 70) || "throwaway" })) });
      } else if (naOpens()) {
        const out = await api("/v1/open", { method: "POST", headers, body: JSON.stringify({
          url: naS.link.url, title, prompt: body.prompt, harness: h.id, model: body.model || "", effort: body.effort || "", pick: !!room }) });
        if (out.card && naS.groups.length) await naTag(out.card, naS.groups);
        task = out.card ? await api("/v1/tasks/" + encodeURIComponent(out.card)) : null;
      } else {
        if (naS.ref && !naRowOf(naS.ref) && typeof hubIsHub !== "undefined" && hubIsHub && !/^[a-z]:[\\/]|^\//i.test(naS.ref)) {
          naSay("the hub is fetching " + naShort(naS.ref) + "…");
          await api("/_hub/git/init", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ url: "https://" + naS.ref }) });
          naSay("");
        }
        Object.assign(body, { cwd: "", title, tags: naS.groups.slice() });
        if (naS.ref) {
          const c = naS.check, br = naBranch();
          Object.assign(body, { repo: naS.ref, branch: br });
          if (naS.link && naS.link.kind === "branch" && br === naS.link.head && !(c && c.exists)) body.base = "origin/" + br;
        } else {
          body.home = true;
        }
        task = await api("/v1/launch", { method: "POST", headers, body: JSON.stringify(body) });
      }
    } catch (e) {
      naSay(e.message, true);
      return;
    }
    if (!tw) {
      naWrite(NA_GROUPS_KEY, naS.groups);
      naWrite(NA_RECENT_GROUPS_KEY, [...new Set([...naS.groups, ...naRead(NA_RECENT_GROUPS_KEY, [])])].slice(0, 12));
    }
    naEl("na").close();
    if (typeof rememberLaunchRoom === "function") rememberLaunchRoom(room, task && task.id);
    if (!task) { switchView("board"); return; }
    cardLaunched(task);
    if (typeof refresh === "function") refresh();
    if (task.supervised) { switchView("terms"); openTerm(task); return; }
    switchView("board");
  }, "starting…");
}

// A card a link opened takes the groups after it exists: the open verb makes it and knows nothing of groups.
async function naTag(id, groups) {
  const t = await api("/v1/tasks/" + encodeURIComponent(id)).catch(() => null);
  const tags = [...new Set([...((t && t.tags) || []), ...groups])];
  await api("/v1/tasks/" + encodeURIComponent(id), { method: "PATCH", headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ tags }) }).catch(e => toast("could not put it in its groups", e.message));
}

// ── keep a throwaway ─────────────────────
// Its name and groups, and its home stays where it is: POST /promote with no path keeps a home under the card root.
let naKeepId = "", naKeepGroups = [];
function naKeepOpen(id, t) {
  naKeepId = id;
  naKeepGroups = naRead(NA_GROUPS_KEY, []);
  naEl("nk-h").textContent = (t && (t.display_title || t.title)) || id;
  naEl("nk-title").value = (t && t.title) || "";
  naEl("nk-groups").dataset.open = "";
  const paint = () => naPaintGroups(naEl("nk-groups"), naKeepGroups, gs => { naKeepGroups = gs; paint(); });
  paint();
  naEl("na-keep").showModal();
  naEl("nk-title").focus();
}
naEl("nk-x").onclick = () => naEl("na-keep").close();
naEl("nk-go").onclick = () => busyWhile("nk-go", async () => {
  const id = naKeepId, title = naEl("nk-title").value.trim();
  let res;
  try {
    res = await api(`/v1/tasks/${id}/promote`, { method: "POST", headers: { "Content-Type": "application/json" }, body: "{}" });
    const t = await api("/v1/tasks/" + encodeURIComponent(id));
    const patch = { tags: [...new Set([...(t.tags || []), ...naKeepGroups])] };
    if (title) patch.overrides = { title };
    await api("/v1/tasks/" + encodeURIComponent(id), { method: "PATCH", headers: { "Content-Type": "application/json" }, body: JSON.stringify(patch) });
  } catch (e) {
    toast("could not keep it", e.message);
    return;
  }
  naEl("na-keep").close();
  toast(res.when === "now" ? "kept" : "kept when the session ends", naKeepGroups.length ? "in " + naKeepGroups.join(", ") : res.to);
  if (typeof refresh === "function") refresh();
}, "keeping…");
naEl("na-keep").addEventListener("keydown", e => {
  if (e.key === "Enter" && e.target.id === "nk-title") { e.preventDefault(); naEl("nk-go").click(); }
});

// The chip a throwaway wears on its card: one click to keep it.
function naKeepChip(t) {
  if (!t || !t.throwaway || isMinion(t)) return "";
  if (t.promote_to) return `<span class="chip" data-tip="kept: it stays when the session ends">kept</span>`;
  return `<span class="chip warn" data-tip="a throwaway: gone when the session ends. click to keep it"
    onclick="event.stopPropagation();naKeepFromCard('${t.id}')">throwaway · keep</span>`;
}
function naKeepFromCard(id) {
  const t = (lastTasks || []).find(x => x.id === id);
  if (t) promoteCard(id, t);
}
