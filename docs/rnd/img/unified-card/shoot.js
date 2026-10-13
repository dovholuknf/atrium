#!/usr/bin/env node
// Before and after mockups for docs/rnd/unified-card.md.
//
//   NODE_PATH=<a node_modules with playwright> node docs/rnd/img/unified-card/shoot.js
//
// Static HTML, written next to this script, styled by the board's real stylesheets (linen skin) and, for the
// "after" pages, proposed.css on top. The "before" markup copies what cardHTML (board.js), stackRow (stack.js),
// termRow (terminal-list.js) and rowHTML (m/js/home.js) draw today. The "after" pages draw every view from one
// function, `uc`, which is the point. PNGs land in docs/rnd/img/.

const fs = require("fs");
const path = require("path");
let chromium;
try { ({ chromium } = require("@playwright/test")); } catch (e) { ({ chromium } = require("playwright")); }

const here = __dirname;
const web = "../../../../internal/api/web";
const outDir = path.join(here, "..");

const CSS = ["tokens", "chrome", "cards", "notify", "rows", "terminal", "sharing", "themes"]
  .map(n => `<link rel="stylesheet" href="${web}/css/${n}.css">`).join("\n");
const esc = s => String(s).replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;");

const CLAUDE = `<path d="M12 3v18M4.5 6.5l15 11M19.5 6.5l-15 11M3 12h18" stroke="currentColor" stroke-width="2.4" stroke-linecap="round"/>`;
const rmark = (working) => `<span class="rmark${working ? " working" : ""}" data-runner="claude"><svg viewBox="0 0 24 24" aria-hidden="true">${CLAUDE}</svg></span>`;
const chip = (text, cls = "") => `<span class="chip ${cls}">${text}</span>`;

// state: waiting | working | live | off | unreach
const C = [
  { t: "interview: gwt prune ownership", g: "dovholuknf/atrium", st: "waiting", status: "ready", age: "1h11m", ask: "Q1 of 4: does prune take worktrees atrium did not make?", why: "clint: resurrect interview cards lost in the 13:44 outage.", path: "D:/worktrees/github/dovholuknf/atrium/interview-gwt-prune-ownership", room: "claude-sg4", alias: "@interview-gwt-prune", model: "opus", q: 4, pin: true },
  { t: "interview: close workflow", g: "dovholuknf/atrium", st: "waiting", status: "ready", age: "51m", why: "interview Clint on the open choices of docs/rnd/close-workflow.md", path: "/Users/claude/git/github/dovholuknf/atrium-worktrees/interview-close-workflow", room: "m1mini", unseen: true, agent: true },
  { t: "pr-ziti-4397", g: "openziti/ziti", st: "waiting", status: "needs permission", age: "2m", ask: "wants to run git push hub claude/pr-ziti-4397", path: "D:/worktrees/github/openziti/ziti/pr-4397", room: "claude-sg4", alias: "@pr-ziti-4397" },
  { t: "m-doc-overhaul: full atrium doc overhaul", g: "dovholuknf/atrium", st: "working", status: "Bash", age: "40s", why: "clint 2026-10-07: start a card up to do a full doc overhaul", path: "D:/worktrees/github/dovholuknf/atrium/m-doc-overhaul", room: "claude-sg4", alias: "@m-doc-overhaul", model: "opus", tags: ["atrium:docs"] },
  { t: "t-ci-green-for-good: CI on main green on Windows and Linux", g: "dovholuknf/atrium", st: "working", status: "thinking", age: "3m", why: "clint: CI on main is still failing, replace the CI worker", path: "D:/worktrees/github/dovholuknf/atrium/t-ci-green-for-good", room: "claude-sg4", alias: "@t-ci-green-for-good", model: "sonnet" },
  { t: "ziti-sdk-c: opt-in e2ee FIPS transition mode", g: "openziti/ziti-sdk-c", st: "live", status: "running", age: "8m", why: "clint wants FIPS-mode SDKs to interoperate with pre-FIPS peers", path: "D:/worktrees/github/openziti/ziti-sdk-c/e2ee-fips-transition", room: "claude-sg4" },
  { t: "clintify: own the clint style-model experiment", g: "dotfiles/clintify", st: "off", status: "stopped", age: "4h", why: "clint asked dotfiles-41800 to move the whole clintify experiment into its own session", path: "D:/git/dotfiles/clintify", room: "claude-sg4", alias: "@clintify" },
  { t: "dotagents: refresh stale atrium AGENTS.md files", g: "dovholuknf/dotagents", st: "off", status: "stopped", age: "6h", why: "clint asked: doc-overhaul notes for dotagents were sent to dotfiles by mistake", path: "D:/git/dotagents", room: "claude-sg4" },
  { t: "u-resume-menu-missing", g: "dovholuknf/atrium", st: "off", status: "shelved", age: "1d", why: "clint 2026-10-07 16:19: where is my resume? Critical: he cannot resume dead cards", path: "D:/worktrees/github/dovholuknf/atrium/u-resume-menu-missing", room: "claude-sg4", pin: true },
];
const DONE = [
  ["ziti/discourse-6087", "2h", true], ["card-audit: every card, keep or ditch", "3h", true], ["win32crypto-e2e", "5h", true],
  ["main:dotagents", "7h", true], ["posture-check-in-native", "9h", false], ["docpreview/aug-revisions", "11h", false],
  ["fips-support-take-2", "14h", true], ["desktop-edge-win/promote-2.11.3.1", "18h", false],
].map(([t, age, pin]) => ({ t, age, pin, st: "off", status: "done", g: "", path: "", room: "claude-sg4" }));

// ── before: today's markup ───────────────────────────────────────────────────
const HUE = { "dovholuknf/atrium": 140, "openziti/ziti": 25, "openziti/ziti-sdk-c": 25, "dotfiles/clintify": 285, "dovholuknf/dotagents": 195 };
function oldCard(c) {
  const w = c.st === "waiting";
  const live = c.st === "working" ? `<span class="chip live ${c.status === "Bash" ? "tool" : "thinking"}">${c.status === "Bash" ? "running Bash 40s" : "thinking 3m"}</span>` : "";
  const word = w ? c.status : c.status === "stopped" ? "ready" : c.st === "working" || c.st === "live" ? "running" : c.status;
  return `<div class="card ${w ? "waiting" : ""}"><div class="card-line">
    <div class="title">${w ? '<span class="pulse"></span>' : ""}${c.pin ? '<span class="pin on">&#9733;</span>' : ""}${rmark(false)}${esc(c.t)}</div>
    <div class="chips">${c.alias ? chip(esc(c.alias) + " &#10697;") : ""}${c.model ? chip(c.model, "accent") : ""}${c.room ? chip(c.room, "accent") : ""}${(c.tags || []).map(x => chip(x)).join("")}${live}${c.q ? chip("? " + c.q, "warn questions") : ""}${c.unseen ? chip("&#9679;", "unseen") : ""}${w ? chip(esc(c.status) + " " + c.age, "warn") : chip(word + " " + c.age)}${chip("attach", "attach")}</div>
  </div>${c.why ? `<div class="why">${esc(c.why)}</div>` : ""}</div>`;
}
function oldGroup(name, cards) {
  return `<details class="cardgroup project" open style="--ghue:${HUE[name] || 210}"><summary><span class="gname">${esc(name)}</span><span class="gn">${cards.length}</span></summary>${cards.map(oldCard).join("")}</details>`;
}
function grouped(list, fn) {
  const by = new Map();
  list.forEach(c => { if (!by.has(c.g)) by.set(c.g, []); by.get(c.g).push(c); });
  return [...by.keys()].sort().map(k => fn(k, by.get(k))).join("");
}
function oldCol(label, n, body, weight) {
  return `<section class="col" style="flex-grow:${weight}"><div class="col-head"><span><span class="fold">&#9662;</span>${label}</span><span class="help">?</span><span class="n">${n}</span></div><div class="cards">${body}</div></section>`;
}
function beforeBoard() {
  // Today: columns are stored statuses. A card whose turn ended sits in READY whether or not its runner is alive.
  const ready = C.filter(c => c.status === "ready" || c.status === "stopped");
  const perm = C.filter(c => c.status === "needs permission");
  const running = C.filter(c => c.st === "working" || c.st === "live");
  const pins = DONE.filter(c => c.pin), rest = DONE.filter(c => !c.pin);
  const done = `<details class="cardgroup" open><summary><span class="gname">done</span><span class="gn">111</span></summary>
    <details class="cardgroup project pins" open style="--ghue:41"><summary><span class="gname">&#9733; pinned</span><span class="gn">17</span></summary>${pins.map(oldCard).join("")}</details>${rest.map(oldCard).join("")}</details>`;
  return page("before-board", `<main style="height:100vh"><div class="board" style="height:100%;padding:10px 14px">
    ${oldCol("needs permission", perm.length, grouped(perm, oldGroup), 1)}
    ${oldCol("ready", 39, grouped(ready, oldGroup), 4)}
    ${oldCol("running", 0, '<div class="empty">empty</div>', 1).replace('class="col"', 'class="col vacant"')}
    ${oldCol("finished", 111, done, 4)}
  </div></main>`, 1600, 820);
}
function beforeStack() {
  const rows = C.map(c => {
    const w = c.st === "waiting";
    return `<div class="row stackrow ${w ? "attn" : ""}"><span class="pin ${c.pin ? "on" : ""}">${c.pin ? "&#9733;" : "&#9734;"}</span>${rmark(false)}
      <div class="wait ${w ? "" : "quiet"}">${c.age}</div>
      <div class="who"><b>${w ? '<span class="pulse"></span>' : ""}${esc(c.t)}</b><span>${esc(c.path)}</span>${c.why ? `<span class="why">${esc(c.why)}</span>` : ""}</div>
      <div class="chips">${c.st === "working" ? `<span class="chip live thinking">thinking</span>` : ""}${c.q ? chip("? " + c.q, "warn questions") : ""}${chip(w ? c.status : c.st === "off" ? (c.status === "shelved" ? "shelved" : "ready") : "running", w ? "warn" : "accent")}${chip("attach", "attach")}</div>
      <i class="since">today 14:02</i></div>`;
  }).join("");
  return page("before-stack", `<div class="stack" style="padding:6px 14px">${rows}</div>`, 1280, 900);
}
const WORN = "--card-0:#1d5a3b;--card-1:#1d5a3b;--head:#e9fff0;--body:#cfeedd;--label:#cfeedd;--dim:#9fd0b4;--dimmest:#86b89c;--warn:#e6d36b;--warn-bg:rgba(230,211,107,.14);--warn-stroke:rgba(230,211,107,.4);--teal:#7fe3d0;--teal-rgb:127,227,208;--chip:rgba(255,255,255,.1);--stroke:rgba(255,255,255,.2)";
function oldTab(c, on) {
  const cold = c.st === "off";
  return `<div class="card tab worn ${on ? "on" : ""} ${cold ? "cold" : ""}" style="${WORN};--tabc:#e9fff0;--tabbg:#1d5a3b">
    <div class="card-line"><div class="title"><span class="pin ${c.pin ? "on" : ""}">${c.pin ? "&#9733;" : "&#9734;"}</span>${rmark(c.st === "working")}<span class="tstack"><span class="tname">${esc(c.t)}</span><span class="tpath">${esc(c.path)}</span></span></div>
    <div class="chips">${c.unseen ? chip("&#9679;", "unseen") : ""}${c.q ? chip("? " + c.q, "warn questions") : ""}<span class="chip keepalive cache warm" style="width:14px;height:14px;border-radius:50%"></span>${chip(c.room, "room")}</div></div></div>`;
}
function beforeTerms() {
  const list = [C[1], C[0], C[3], C[2], C[5], C[8]];
  const head = (n, k) => `<div style="font:600 11px var(--mono);color:var(--label);padding:8px 4px 4px;letter-spacing:.06em">&#9662; ${n} <span style="color:var(--dimmest)">${k}</span></div>`;
  return page("before-terminals", `<div class="term-list" style="--termw:420px;width:420px;height:600px;margin:8px"><div class="termscroll">
    ${head("ATRIUM:INTERVIEW", 2)}${oldTab(list[0], true)}${oldTab(list[1])}
    ${head("WORK", 4)}${list.slice(2).map(c => oldTab(c)).join("")}</div></div>`, 440, 620);
}
const ICON = {
  permission: '<svg viewBox="0 0 24 24"><path d="M12 3l7 3v5c0 4.5-3 8-7 10-4-2-7-5.5-7-10V6z"/><path d="M9.5 12l2 2 3.5-4"/></svg>',
  questions: '<svg viewBox="0 0 24 24"><path d="M9.2 9a3 3 0 115 2c-.9.7-2.2 1.3-2.2 3"/><circle cx="12" cy="17.6" r=".6"/></svg>',
  ready: '<svg viewBox="0 0 24 24"><path d="M6 12h11M13 7l5 5-5 5"/></svg>',
  unseen: '<svg viewBox="0 0 24 24"><circle cx="12" cy="12" r="4.5" class="fill"/></svg>',
  working: '<svg viewBox="0 0 24 24"><path d="M12 4a8 8 0 108 8"/></svg>',
  idle: '<svg viewBox="0 0 24 24"><circle cx="12" cy="12" r="3" class="fill"/></svg>',
};
function phonePage(name, inner, extra = []) {
  return page(name, `<div class="app"><main class="home"><div class="list" id="m-list" style="padding:12px 14px">${inner}</div></main></div>`, 412, 860, "m",
    [web + "/m/m.css", "phone-mark.css", ...extra], ["tokens", "themes"]);
}
function beforePhone() {
  const row = (c, kind, why) => `<div class="rw"><div class="rw-in"><button type="button" class="row k-${kind}"><span class="glyph">${ICON[kind]}</span><span class="body"><span class="name"><b>${esc(c.t)}</b></span><span class="why">${esc(why)}</span></span><span class="meta"><span class="age">${c.age}</span><span class="room">${c.room}</span></span></button></div></div>`;
  return phonePage("before-phone", `<h2 class="grp">Working <em>2</em></h2>${row(C[3], "working", "running Bash")}${row(C[5], "working", "running")}
    <h2 class="grp">Waiting <em>3</em></h2>${row(C[2], "permission", "wants to run git push hub claude/pr-ziti-4397")}${row(C[0], "questions", "asks 4 questions")}${row(C[1], "ready", "waiting for you")}
    <h2 class="grp">Idle <em>3</em></h2>${row(C[6], "idle", "ready")}${row(C[8], "idle", "shelved")}${row(DONE[0], "idle", "done")}`);
}

// ── after: one card, every view ──────────────────────────────────────────────
const WORD = { waiting: null, working: null, live: "running", off: null };
function uc(c, opt = {}) {
  const state = c.st === "waiting" ? c.status : c.st === "working" ? c.status : c.st === "live" ? "running" : c.status;
  const chips = [];
  if (opt.stateLow) chips.push(`<span class="chip uc-state">${esc(state)} ${c.age}</span>`);
  if (c.q) chips.push(chip("? " + c.q, "warn questions"));
  if (c.unseen) chips.push(chip("&#9679; unread", "unseen"));
  if (opt.detail !== false) {
    if (c.alias) chips.push(chip(esc(c.alias)));
    if (c.model) chips.push(chip(c.model));
    (c.tags || []).forEach(x => chips.push(chip(x)));
    if (c.agent) chips.push(chip("agent"));
    if (opt.room && c.room) chips.push(chip(c.room, "room"));
  }
  if (c.st === "off" && c.status !== "done") chips.push(chip("resume", "attach uc-act"));
  else if (c.st !== "off") chips.push(chip("attach", "attach uc-act"));
  const side = opt.side ? `<div class="uc-side">${opt.side(c)}</div>` : "";
  return `<div class="uc s-${c.st}${opt.cls || ""}"${opt.style ? ` style="${opt.style}"` : ""}>
    <span class="uc-mark">${rmark(c.st === "working")}</span>
    <div class="uc-title">${c.pin ? '<span class="star">&#9733;</span>' : ""}${esc(c.t)}</div>
    ${opt.stateLow ? "" : `<span class="chip uc-state">${esc(state)} ${c.age}</span>`}${side}
    ${opt.path && c.path ? `<div class="uc-path">${esc(c.path)}</div>` : ""}
    ${c.ask ? `<div class="uc-ask">${esc(c.ask)}</div>` : ""}
    ${chips.length ? `<div class="uc-chips">${chips.join("")}</div>` : ""}
    ${c.why && opt.why !== false ? `<div class="uc-why">${esc(c.why)}</div>` : ""}
  </div>`;
}
function ugroup(name, cards, opt) {
  return `<div class="ugroup"><div class="uh">${esc(name)} <span class="n">${cards.length}</span></div>${cards.map(c => uc(c, opt)).join("")}</div>`;
}
function ucol(label, n, body, cls = "", head = "") {
  return `<section class="col ${cls}"><div class="col-head ${head}"><span><span class="fold">&#9662;</span>${label}</span><span class="help">?</span><span class="n">${n}</span></div><div class="cards">${body}</div></section>`;
}
function afterBoard() {
  const waiting = C.filter(c => c.st === "waiting");
  const running = C.filter(c => c.st === "working" || c.st === "live");
  const stopped = C.filter(c => c.st === "off");
  const opt = { room: true };
  const done = DONE.slice(0, 6).map(c => uc(c, { detail: false })).join("") +
    `<a class="umore"><b>105 more</b> finished, 17 of them pinned &middot; open in the stack</a>`;
  return page("after-board", `<div class="ub-sum"><span><b>6</b> running:</span><span class="w"><b>3</b> need you</span><span><b>3</b> working</span><span style="margin-left:auto;font-weight:400;color:var(--dim)">3 stopped &middot; 111 done</span></div>
    <div class="ub">
    ${ucol("needs you", waiting.length, grouped(waiting, (k, v) => ugroup(k, v, opt)), "", "needs")}
    ${ucol("working", running.length, grouped(running, (k, v) => ugroup(k, v, opt)))}
    ${ucol("stopped", stopped.length, stopped.map(c => uc(c, { room: true })).join(""), "stopped")}
    ${ucol("done", 111, done, "done")}
  </div>`, 1600, 820, "", ["proposed.css"]);
}
function afterStack() {
  const rows = [...C].map(c => `<div class="urow s-${c.st}"><div class="big">${c.age}</div>${uc(c, { path: true, room: true })}<div class="since">today 14:02</div></div>`).join("");
  return page("after-stack", `<div class="us">${rows}</div>`, 1280, 900, "", ["proposed.css"]);
}
const WORN2 = "--wbg:#1d5a3b;--whead:#e9fff0;--wdim:#9fd0b4";
function afterTerms() {
  const side = c => `${c.st !== "off" ? '<span class="chip keepalive cache warm" style="width:14px;height:14px;border-radius:50%"></span>' : ""}${chip(c.room, "room")}`;
  const o = (c, on) => uc(c, { path: true, why: false, detail: false, side, stateLow: true, cls: " worn" + (on ? " on" : ""), style: WORN2 });
  return page("after-terminals", `<div class="ut">
    <div class="uh">&#9662; atrium:interview <span class="n">2</span></div>${o(C[1], true)}${o(C[0])}
    <div class="uh">&#9662; work <span class="n">4</span></div>${o(C[3])}${o(C[2])}${o(C[5])}${o(C[8])}</div>`, 440, 600, "", ["proposed.css"]);
}
function afterPhone() {
  const row = (c, why) => `<div class="rw"><div class="rw-in"><button type="button" class="row s-${c.st}"><span class="glyph">${rmark(c.st === "working")}</span><span class="body"><span class="name"><b>${esc(c.t)}</b></span><span class="why">${esc(why)}</span></span><span class="meta"><span class="age">${c.age}</span><span class="room">${c.room}</span></span></button></div></div>`;
  return phonePage("after-phone", `<h2 class="grp">Needs you <em>3</em></h2>${row(C[2], "wants to run git push hub claude/pr-ziti-4397")}${row(C[0], "asks 4 questions")}${row(C[1], "ready, finished 51m ago, not read")}
    <h2 class="grp">Working <em>2</em></h2>${row(C[3], "Bash 40s")}${row(C[5], "running, quiet 8m")}
    <h2 class="grp">Stopped <em>2</em></h2>${row(C[6], "stopped 4h ago. tap to resume")}${row(C[8], "shelved")}
    <h2 class="grp">Done <em>111</em></h2>`, ["proposed.css"]);
}

function page(name, body, w, h, bodyClass = "", extra = [], only = null) {
  const css = only ? only.map(n => `<link rel="stylesheet" href="${web}/css/${n}.css">`).join("\n") : CSS;
  const html = `<!doctype html><html data-skin="linen"><head><meta charset="utf-8">${css}
${extra.map(x => `<link rel="stylesheet" href="${x}">`).join("\n")}
<style>html,body{height:auto;min-height:100%;overflow:visible} body{display:block}
.rmark.working::after{background-image:url("${web}/working.gif")}</style></head>
<body class="${bodyClass}">${body}</body></html>`;
  fs.writeFileSync(path.join(here, name + ".html"), html);
  return { name, w, h };
}

(async () => {
  const shots = [beforeBoard(), afterBoard(), beforeStack(), afterStack(), beforeTerms(), afterTerms(), beforePhone(), afterPhone()];
  const browser = await chromium.launch();
  for (const s of shots) {
    const p = await browser.newPage({ viewport: { width: s.w, height: s.h }, deviceScaleFactor: 1 });
    await p.goto("file://" + path.join(here, s.name + ".html"));
    await p.waitForTimeout(150);
    await p.screenshot({ path: path.join(outDir, "unified-card-" + s.name + ".png") });
    await p.close();
  }
  await browser.close();
  console.log("wrote " + shots.length + " shots to " + outDir);
})();
