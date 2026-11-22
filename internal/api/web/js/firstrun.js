// First run: the connect panel, the getting-started checklist, launch in place, adopting a session and the harden
// wizard. Everything here reads the real API. boot.js stays last, so this file may only touch what is already loaded.
(function () {
  "use strict";
  const $ = id => document.getElementById(id);
  const help = tip => `<span class="help" tabindex="0" data-tip="${esc(tip)}">?</span>`;
  const K = {
    seen: "atrium.fr.seen", joined: "atrium.fr.joined", hidden: "atrium.fr.hidden", launched: "atrium.fr.launched",
    adopted: "atrium.fr.adopted", hardened: "atrium.fr.hardened", nudged: "atrium.fr.nudged", undo: "atrium.fr.undo",
  };
  const get = k => { try { return localStorage.getItem(k); } catch (e) { return null; } };
  const put = (k, v) => { try { if (v == null) localStorage.removeItem(k); else localStorage.setItem(k, v); } catch (e) {} };
  const board = () => typeof hubIsHub !== "undefined" && hubIsHub;
  const rooms = () => (typeof hubRooms === "undefined" ? [] : hubRooms);
  const cards = () => (typeof lastTasks === "undefined" || !tasksLoaded ? null : lastTasks);
  const slash = p => String(p || "").replace(/\\/g, "/").replace(/(.)\/+$/, "$1");
  const leaf = p => slash(p).split("/").filter(Boolean).pop() || p;
  const say = (t, b) => { try { toast(t, b); } catch (e) {} };

  let facts = null;
  async function loadFacts() {
    if (facts) return facts;
    try { facts = await api("/_hub/firstrun"); } catch (e) { facts = {}; }
    return facts;
  }
  const human = () => (facts && facts.user) || "you";

  // Folders worth offering: where cards have worked, and where Claude sessions already live on this machine.
  let folders = [];
  let found = [];
  async function loadFolders() {
    const out = [], seen = new Set();
    const add = (p, git) => { p = slash(p); if (p && !seen.has(p)) { seen.add(p); out.push({ path: p, git }); } };
    try { ((await api("/v1/checkouts")).checkouts || []).forEach(c => add(c.root || c.path, true)); } catch (e) {}
    try { found = (await api("/v1/sessions/found")).sessions || []; } catch (e) { found = []; }
    found.forEach(s => add(s.cwd, null));
    folders = out;
    return out;
  }
  const home = p => String(p || "").replace(/^[A-Za-z]:\/Users\/[^/]+|^\/Users\/[^/]+|^\/home\/[^/]+/, "~");

  // ── what is known, and which pane that calls for ───────────────────────────
  function everConnected() {
    return typeof hubInventory !== "undefined" && hubInventory.some(r => r.transport !== "local" && (r.attached || r.first_seen));
  }
  function stage() {
    if (!board()) return "";
    if (!rooms().length) return everConnected() ? "" : "connect";
    if (get(K.joined) === "1") return "joined";
    const c = cards();
    if (c && c.length === 0 && get(K.hidden) !== "1") return "list";
    return "";
  }
  const doneFlags = () => ({
    hub: true, room: rooms().length > 0, launch: get(K.launched) === "1", adopt: get(K.adopted) === "1", harden: get(K.hardened) === "1",
  });
  function steps() {
    const d = doneFlags(), r = rooms()[0];
    return [
      { id: "hub", title: "the hub is running", sub: location.host, done: true,
        tip: "atrium run started the hub on this machine. It serves this board and holds nothing else: the agents run on rooms." },
      { id: "room", title: d.room ? "this machine is a room" : "connect this machine as a room", sub: d.room && r ? r.name + " as " + human() : "", done: d.room,
        tip: "A room is a machine that runs your agents and dials the hub. Your own laptop is the usual first one." },
      { id: "launch", title: "start your first agent in your own folder", sub: "", done: d.launch, act: "start an agent", key: "launch",
        tip: "It starts in the folder as it is and edits your files directly: no copy and no branch. Pick in a copy in the dialog if you would rather keep your tree clean." },
      { id: "adopt", title: "bring in a session you already have", sub: d.adopt ? "adopted" : "", done: d.adopt, act: "adopt a session", key: "adopt",
        tip: "Claude sessions already on this machine can be taken over: atrium ends them where they run now and resumes them in a terminal it holds." },
      { id: "harden", title: "harden this setup", sub: d.harden ? "applied" : "", done: d.harden, act: "harden", key: "harden", optional: true,
        tip: "Optional. Permissions, which folders agents may send files to, the guard hook and who can open this board." },
    ];
  }
  const count = () => { const s = steps(); return [s.filter(x => x.done).length, s.length]; };

  // ── the panes ──────────────────────────────────────────────────────────────
  const pane = document.createElement("div");
  pane.id = "fr-pane";
  pane.hidden = true;
  const tp = $("term-pane");
  if (tp) tp.appendChild(pane);

  function checklistHTML(inDialog) {
    const [n, t] = count();
    const rows = steps().map(s => `
      <div class="fr-step${s.done ? " done" : ""}" data-step="${s.id}">
        <span class="fr-tick" aria-hidden="true">${s.done ? "&#10003;" : ""}</span>
        <span class="fr-st">
          <span class="fr-t">${esc(s.title)}${help(s.tip)}${s.optional ? `<span class="chip">optional</span>` : ""}</span>
          ${s.sub ? `<code class="fr-sub">${esc(s.sub)}</code>` : ""}
        </span>
        ${s.act ? `<button type="button" class="${s.done ? "" : "go"}" data-act="${s.key}">${s.done && s.key !== "harden" ? "again" : esc(s.act)}</button>` : ""}
      </div>`).join("");
    return `<div class="fr-card">
      <div class="fr-head">
        <div><div class="fr-eyebrow">getting started</div><h2>${esc(human())}, your board is empty</h2></div>
        <span class="fr-count">${n} of ${t}</span>
      </div>
      <div class="fr-bar"><i style="width:${Math.round(100 * n / t)}%"></i></div>
      <div class="fr-steps">${rows}</div>
      <div class="fr-foot">
        <button type="button" class="linkbtn" data-act="hide">${inDialog ? "close" : "hide this list"}</button>
        <span class="grow"></span>
        <button type="button" class="linkbtn" data-act="skip-all">skip setup</button>
      </div>
    </div>`;
  }

  function connectHTML() {
    return `<div class="fr-card">
      <div class="fr-head"><div><div class="fr-eyebrow">welcome to atrium</div><h2>connect this machine</h2></div></div>
      <div class="fr-steps">
        <div class="fr-step done"><span class="fr-tick">&#10003;</span><span class="fr-st"><span class="fr-t">the hub is running${help("atrium run started the hub on this machine. It serves this board and holds nothing else: the agents run on rooms.")}</span><code class="fr-sub">${esc(location.host)}</code></span></div>
        <div class="fr-step"><span class="fr-tick"></span><span class="fr-st"><span class="fr-t">connect a room${help("A room is a machine that runs your agents and dials the hub. Your own laptop is the usual first one.")}</span></span></div>
      </div>
      <div class="fr-form">
        <div class="field"><label class="eyebrow" for="fr-room">room name${help("The name is the key. The hub lists the room by it, and cards show it once there is more than one room.")}</label>
          <input type="text" id="fr-room" spellcheck="false" value="${esc((facts && facts.room) || "")}"></div>
        <div class="field"><label class="eyebrow" for="fr-human">joins as${help("The room acts as the account atrium runs under: sessions it starts are that account's, and every approval you give is recorded under this name.")}</label>
          <input type="text" id="fr-human" spellcheck="false" readonly value="${esc(human())}"></div>
      </div>
      <div class="fr-cmd" id="fr-cmd" hidden>
        <label class="eyebrow">run this on the other machine${help("The hub mints a one-time string for a room name. It is good once, for an hour, and for that name only.")}</label>
        <div class="copyrow"><input type="text" id="fr-cmdtext" readonly spellcheck="false"><button type="button" class="go" data-act="copy">copy</button></div>
        <div class="fr-waiting"><span class="spin"></span><span>waiting for it to check in</span></div>
      </div>
      <div class="fr-note warn" id="fr-err" hidden></div>
      <div class="fr-foot">
        <button type="button" class="linkbtn" data-act="other">join another machine instead</button>
        <span class="grow"></span>
        <button type="button" class="go big" data-act="join">join this machine</button>
      </div>
    </div>`;
  }

  let facts2 = null;
  async function joinedFacts() {
    const out = { machine: "", agent: "", agentOK: false, hooks: "", hooksOK: false, folders: 0 };
    const r = rooms()[0];
    out.machine = r ? (r.name + (r.os ? " &middot; " + esc(r.os) : "")) : "";
    try {
      const hs = ((await api("/v1/harnesses")).harnesses || []).filter(h => h.enabled && h.found);
      if (hs.length) { out.agent = hs[0].id + " at " + hs[0].found; out.agentOK = true; }
    } catch (e) {}
    try {
      const rep = await api("/v1/hooks");
      out.hooks = rep.path || "";
      out.hooksOK = rep.missing === 0;
      out.hookMissing = rep.missing;
    } catch (e) {}
    out.folders = (await loadFolders()).length;
    return out;
  }
  function joinedHTML(f) {
    const r = rooms()[0] || {};
    return `<div class="fr-card">
      <div class="fr-head"><div><div class="fr-eyebrow">connected</div><h2>${esc(r.name || "")} joined as ${esc(human())}</h2></div><span class="chip ok">attached</span></div>
      <div class="fr-found">
        <div class="fr-frow"><span class="fr-k">machine${help("What the room reported about itself when it checked in.")}</span><code>${f.machine}</code></div>
        <div class="fr-frow"><span class="fr-k">agent found${help("atrium looks for the agent programs on the room's PATH. A missing one can be added under runners.")}</span><code>${f.agentOK ? esc(f.agent) : "no agent program found on this machine's PATH. install claude, then add it under runners"}</code>${f.agentOK ? `<span class="chip ok">ready</span>` : `<span class="chip warn">missing</span>`}</span></div>
        <div class="fr-frow"><span class="fr-k">hooks${help("atrium reports what a session is doing through hooks in the agent's own settings.")}</span><code>${esc(f.hooks || "not read")}</code>${f.hooksOK ? `<span class="chip ok">installed</span>` : `<span class="chip warn">${f.hookMissing ? f.hookMissing + " missing" : "unknown"}</span>`}</div>
        <div class="fr-frow"><span class="fr-k">folders found${help("Folders cards have worked in and folders Claude sessions already live in. They are offered when you start an agent.")}</span><code>${f.folders ? f.folders + " on this machine" : "none yet. type a folder when you start an agent"}</code></div>
      </div>
      <div class="fr-foot">
        <span class="grow"></span>
        <button type="button" class="go big" data-act="continue">continue to the board</button>
      </div>
    </div>`;
  }

  let painted = "", lastStage = "";
  async function paint() {
    const st = stage();
    const key = st + "|" + count().join() + "|" + rooms().map(r => r.name).join() + "|" + get(K.hidden);
    if (st) put(K.seen, "1");
    if (st === "connect") put(K.joined, "1");
    if (key !== painted) {
      painted = key;
      if (st === "connect") { await loadFacts(); pane.innerHTML = connectHTML(); }
      else if (st === "joined") {
        await loadFacts();
        pane.innerHTML = joinedHTML(await joinedFacts());
      } else if (st === "list") { await loadFacts(); pane.innerHTML = checklistHTML(false); }
    }
    pane.dataset.stage = st;
    pane.hidden = !st;
    if (st && st !== lastStage) { try { switchView("terms"); } catch (e) {} }
    lastStage = st;
    const pill = $("fr-pill");
    if (pill) {
      const [n, t] = count();
      pill.hidden = !(board() && get(K.seen) === "1" && !st && rooms().length > 0 && n < t);
      pill.querySelector(".n").textContent = n + "/" + t;
    }
    const list = $("fr-list");
    if (list && list.open) list.querySelector(".fr-body").innerHTML = checklistHTML(true);
  }
  window.frPaint = () => { paint().catch(() => {}); };
  window.frCardsMoved = () => {
    const c = cards();
    if (c && c.length && get(K.launched) !== "1") put(K.launched, "1");
    paint().catch(() => {});
    nudgeCheck(c || []);
  };

  // ── the header pill, and the dialog it opens ───────────────────────────────
  (function () {
    const pill = document.createElement("button");
    pill.id = "fr-pill"; pill.type = "button"; pill.hidden = true;
    pill.innerHTML = `<span class="fr-ring"></span>getting started <b class="n"></b>`;
    pill.onclick = () => openList();
    const before = $("working");
    if (before && before.parentNode) before.parentNode.insertBefore(pill, before);
    const dlg = document.createElement("dialog");
    dlg.id = "fr-list";
    dlg.innerHTML = `<div class="dlg-head"><div class="grow"><div class="dlg-eyebrow">atrium</div><h2>getting started</h2></div><button type="button" class="x" id="fr-list-x" aria-label="close">&times;</button></div><div class="dlg-body fr-body"></div>`;
    document.body.appendChild(dlg);
    dlg.querySelector("#fr-list-x").onclick = () => dlg.close();
  })();
  function openList() { const l = $("fr-list"); l.querySelector(".fr-body").innerHTML = checklistHTML(true); if (!l.open) l.showModal(); }
  window.openChecklist = openList;
  function closeList() { const l = $("fr-list"); if (l && l.open) l.close(); }

  // ── joining ────────────────────────────────────────────────────────────────
  let watching = 0;
  function watchForRoom() {
    clearInterval(watching);
    let tries = 0;
    watching = setInterval(async () => {
      tries++;
      try { hubRead = 0; await loadHubRooms(); } catch (e) {}
      if (rooms().length) {
        clearInterval(watching);
        try { refresh(); } catch (e) {}
        paint().catch(() => {});
      } else if (tries >= 60) {
        clearInterval(watching);
        fail("the room has not checked in after a minute. look at the hub's output for why, then try again");
        const b = pane.querySelector('[data-act="join"]');
        if (b) { b.disabled = false; b.textContent = "join this machine"; }
      }
    }, 1500);
  }
  function fail(msg) { const e = $("fr-err"); if (e) { e.textContent = msg; e.hidden = !msg; } }
  const roomName = () => (($("fr-room") || {}).value || "").trim();

  async function join(b) {
    const name = roomName();
    if (!name) { fail("a room needs a name. type one, such as the name of this machine"); $("fr-room").focus(); return; }
    fail("");
    b.disabled = true; b.textContent = "connecting...";
    try {
      await api("/_hub/firstrun/room", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ name }) });
    } catch (e) {
      fail(e.message);
      b.disabled = false; b.textContent = "join this machine";
      return;
    }
    watchForRoom();
  }
  async function other(b) {
    const c = $("fr-cmd");
    if (!c.hidden) { c.hidden = true; b.textContent = "join another machine instead"; clearInterval(watching); return; }
    const name = roomName();
    if (!name) { fail("a room needs a name. type the other machine's name first"); $("fr-room").focus(); return; }
    fail("");
    try {
      const out = await api("/_hub/firstrun/join-string", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ name }) });
      $("fr-cmdtext").value = "atrium room join " + out.line;
    } catch (e) { fail(e.message); return; }
    c.hidden = false; b.textContent = "hide the command";
    watchForRoom();
  }

  // ── every button in the panes ──────────────────────────────────────────────
  document.addEventListener("click", e => {
    const b = e.target.closest("[data-act]");
    if (!b || !(b.closest("#fr-pane") || b.closest("#fr-list"))) return;
    act(b.dataset.act, b);
  });
  function act(a, b) {
    if (a === "join") join(b);
    else if (a === "other") other(b);
    else if (a === "copy") {
      try { navigator.clipboard.writeText($("fr-cmdtext").value).catch(() => {}); } catch (e) {}
      b.textContent = "copied"; setTimeout(() => { b.textContent = "copy"; }, 1200);
    } else if (a === "continue") { put(K.joined, null); painted = ""; paint().catch(() => {}); }
    else if (a === "launch") { closeList(); openNewAgent(false); }
    else if (a === "adopt") { closeList(); openAdopt(); }
    else if (a === "harden") { closeList(); openHarden(); }
    else if (a === "hide" || a === "skip-all") {
      put(K.hidden, "1"); closeList(); painted = ""; paint().catch(() => {});
      if (a === "hide") say("list hidden", "reopen it from getting started in the header");
    }
  }

  // ── the nudge after the first card goes idle ───────────────────────────────
  function nudgeCheck(c) {
    if (get(K.nudged) === "1" || get(K.hardened) === "1" || get(K.seen) !== "1" || $("fr-nudge")) return;
    const t = c.find(x => x.status === "needs-input" || x.status === "idle");
    if (!t) return;
    put(K.nudged, "1");
    const n = document.createElement("div");
    n.className = "fr-nudge"; n.id = "fr-nudge";
    n.innerHTML = `<div><b>${esc(t.display_title || "your agent")} is idle</b><span>its first turn is done</span></div>
      <button type="button" class="go" data-nudge="harden">harden this setup</button>
      <button type="button" data-nudge="later">not now</button>`;
    document.body.appendChild(n);
    n.onclick = e => {
      const k = e.target.dataset.nudge; if (!k) return;
      n.remove();
      if (k === "harden") openHarden();
    };
  }

  // ── launch in place: the real new-agent dialog, changed where first run needs it ──
  let runIn = "place";
  const origOpen = window.openNewAgent;
  window.openNewAgent = async function (tw, prefill, clone) {
    await origOpen(tw, prefill, clone);
    if (tw || clone || !naS) return;
    decorateNewAgent();
    const first = !cards() || cards().length === 0;
    if (first && !prefill && !naS.ref) { naS.kind = "folder"; runIn = "place"; fillFolder(); }
    naRender();
    (naS.kind === "folder" ? naEl("na-cwd") : naEl("na-repo")).focus();
    // Not awaited: whoever opened the dialog sets its groups the moment this returns.
    loadFolders().then(() => { if (naS && naEl("na").open) { if (first) fillFolder(); paintRun(); } });
  };
  function fillFolder() {
    if (naS.kind !== "folder" || naEl("na-cwd").value) return;
    const start = naRead(NA_START_KEY, "") || (folders[0] && folders[0].path) || "";
    if (!start) return;
    naEl("na-cwd").value = slash(start);
    naEl("na-cwd").dispatchEvent(new Event("input", { bubbles: true }));
  }
  const origRender = window.naRender;
  window.naRender = function () { origRender(); if ($("fr-run")) paintRun(); };
  const origLaunch = window.naLaunch;
  window.naLaunch = async function (tw) {
    if (tw || !naS || naS.kind !== "folder" || runIn !== "copy") return origLaunch(tw);
    const path = naFolder(), title = naTitle();
    if (!path) return origLaunch(tw);
    Object.assign(naS, { kind: "repo", title, titleTouched: true });
    naSetRepo(path, null);
    try { return await origLaunch(tw); }
    finally { if (naS && naEl("na").open) { naS.kind = "folder"; naRender(); } }
  };
  function decorateNewAgent() {
    if ($("fr-run")) return;
    const two = document.querySelector("#na .two");
    const row = document.createElement("div");
    row.className = "field notw"; row.id = "fr-run-row";
    row.innerHTML = `<label class="eyebrow">where it works${help("In place: the agent edits the files in this folder directly, with no copy and no branch. In a copy: atrium makes a worktree under the card root on a new branch, and your folder is left alone. A copy needs a git repository.")}
      <span class="kindseg" id="fr-run"><button type="button" data-v="place" class="on">in place</button><button type="button" data-v="copy">in a copy</button></span></label>`;
    const chips = document.createElement("div"); chips.className = "fr-folders"; chips.id = "fr-folders";
    $("na-resolved").parentNode.insertBefore(chips, $("na-resolved"));
    two.parentNode.insertBefore(row, $("na-conf"));
    $("fr-run").addEventListener("click", e => { const v = e.target.dataset.v; if (v && !e.target.disabled) { runIn = v; naRender(); } });
    $("fr-folders").addEventListener("click", e => {
      const b = e.target.closest("button[data-path]"); if (!b) return;
      naS.kind = "folder"; $("na-cwd").value = b.dataset.path; $("na-cwd").dispatchEvent(new Event("input", { bubbles: true })); naRender();
    });
    const foot = document.querySelector("#na .dlg-foot");
    const adopt = document.createElement("button");
    adopt.type = "button"; adopt.className = "linkbtn"; adopt.id = "fr-na-adopt"; adopt.textContent = "adopt a session instead";
    adopt.onclick = () => { $("na").close(); openAdopt(); };
    foot.insertBefore(adopt, foot.firstChild);
  }
  function paintRun() {
    const fold = naS && naS.kind === "folder";
    const row = $("fr-run-row"); if (!row) return;
    row.hidden = !fold; $("fr-folders").hidden = !fold || !folders.length;
    const cwd = slash(($("na-cwd").value || "").trim());
    const f = folders.find(x => x.path === cwd);
    const nogit = !!f && f.git === false;
    if (nogit && runIn === "copy") runIn = "place";
    $("fr-run").querySelectorAll("button").forEach(b => {
      b.classList.toggle("on", b.dataset.v === runIn);
      b.disabled = b.dataset.v === "copy" && nogit;
    });
    $("fr-folders").innerHTML = folders.map(x => `<button type="button" class="fr-fchip${x.path === cwd ? " on" : ""}" data-path="${esc(x.path)}">${esc(home(x.path))}${x.git === false ? `<span class="chip">no git</span>` : ""}</button>`).join("");
    if (fold) $("na-go").firstElementChild.textContent = "launch in " + (cwd ? leaf(cwd) : "folder");
  }
  document.addEventListener("input", e => { if (e.target.id === "na-cwd") setTimeout(paintRun, 0); });

  // ── adopt an existing session ──────────────────────────────────────────────
  const adopt = document.createElement("dialog");
  adopt.id = "fr-adopt";
  document.body.appendChild(adopt);
  let pick = "", sessions = [], adoptMsg = "";
  const taken = () => new Set((cards() || []).map(t => t.resume_id).filter(Boolean));
  const ago = at => {
    const s = Math.max(0, (Date.now() - Date.parse(at)) / 1000);
    return s < 90 ? "just now" : s < 5400 ? Math.round(s / 60) + " min ago" : s < 129600 ? Math.round(s / 3600) + " h ago" : Math.round(s / 86400) + " d ago";
  };
  function adoptHTML() {
    const list = sessions.filter(s => !taken().has(s.id));
    const where = (rooms()[0] || {}).name || "this machine";
    return `<div class="dlg-head"><div class="grow"><div class="dlg-eyebrow">sessions on ${esc(where)}</div><h2>adopt a session${help("Adopting ends the session where it runs now and resumes it in a terminal atrium holds, in the same folder, with its history.")}</h2></div>
      <button type="button" class="x" data-ad="close" aria-label="close">&times;</button></div>
      <div class="dlg-body">
        ${list.length ? `<div class="fr-sessions" role="radiogroup">${list.map(s => `
          <button type="button" class="fr-sess${pick === s.id ? " on" : ""}" role="radio" aria-checked="${pick === s.id}" data-id="${esc(s.id)}">
            <span class="fr-radio"></span>
            <span class="fr-sl"><b>${esc(s.title || leaf(s.cwd))}</b><code>${esc(home(s.cwd))}</code></span>
            <span class="fr-sm">${esc(ago(s.at))}</span>
          </button>`).join("")}</div>`
        : `<div class="empty">no sessions found on this machine. claude keeps them under ~/.claude/projects. scan a folder below if yours lives elsewhere</div>`}
        <div class="field fr-scan"><label class="eyebrow" for="fr-scan-in">also look in${help("A folder atrium has not searched. It reads the agent's own session files there and changes nothing.")}</label>
          <div class="copyrow"><input type="text" id="fr-scan-in" spellcheck="false" placeholder="~/src/another-project"><button type="button" data-ad="scan">scan</button></div></div>
        ${adoptMsg ? `<div class="fr-note warn">${esc(adoptMsg)}</div>` : ""}
      </div>
      <div class="dlg-foot"><span class="grow"></span><button type="button" data-ad="close">cancel</button>
        <button type="button" class="go big" data-ad="go"${pick ? "" : " disabled"}>adopt</button></div>`;
  }
  window.openAdopt = async function () {
    pick = ""; adoptMsg = ""; sessions = [];
    try { sessions = (await api("/v1/sessions/found")).sessions || []; } catch (e) { adoptMsg = "could not read the sessions on this machine: " + e.message; }
    adopt.innerHTML = adoptHTML();
    if (!adopt.open) adopt.showModal();
  };
  adopt.addEventListener("click", async e => {
    const s = e.target.closest(".fr-sess");
    if (s) { pick = s.dataset.id; adopt.innerHTML = adoptHTML(); return; }
    const b = e.target.closest("[data-ad]"); if (!b) return;
    if (b.dataset.ad === "close") adopt.close();
    else if (b.dataset.ad === "scan") {
      const v = $("fr-scan-in").value.trim();
      if (!v) { $("fr-scan-in").focus(); return; }
      try {
        const more = (await api("/v1/sessions/found?dir=" + encodeURIComponent(v))).sessions || [];
        const ids = new Set(sessions.map(x => x.id));
        sessions = sessions.concat(more.filter(x => !ids.has(x.id)));
        adoptMsg = more.length ? "" : "no Claude sessions in " + v + ". open the folder in claude once, then scan again";
        if (more.length) pick = more[0].id;
      } catch (err) { adoptMsg = err.message; }
      adopt.innerHTML = adoptHTML();
    } else if (b.dataset.ad === "go") doAdopt(b);
  });
  async function doAdopt(b) {
    const s = sessions.find(x => x.id === pick); if (!s) return;
    b.disabled = true; b.textContent = "adopting...";
    let task;
    try {
      task = await api("/v1/launch", { method: "POST", headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ cwd: s.cwd, resume: s.id, title: s.title || leaf(s.cwd) }) });
    } catch (err) {
      adoptMsg = "could not adopt it: " + err.message + ". make sure the folder still exists, then try again";
      adopt.innerHTML = adoptHTML();
      return;
    }
    put(K.adopted, "1"); put(K.launched, "1");
    adopt.close();
    try { cardLaunched(task); refresh(); } catch (err) {}
    painted = ""; paint().catch(() => {});
    try { if (task.supervised) { switchView("terms"); openTerm(task); } } catch (err) {}
    say("adopted", s.title + " is now a card, resumed in " + home(s.cwd));
  }

  // ── the harden wizard: every step writes something the daemon really keeps ───
  const hd = document.createElement("dialog");
  hd.id = "fr-harden";
  document.body.appendChild(hd);
  const STEPS = ["permissions", "folders", "guard", "reach", "review"];
  const TITLES = ["permissions", "open folders", "the guard", "who can reach the board", "review"];
  let H;
  const PERMS = [
    { v: "ask", t: "ask me every time", tip: "Every tool call waits for you. Nothing runs until you approve it, which is the safest and the slowest." },
    { v: "risky", t: "ask for risky calls only", tip: "Reads and searches in your project folders go through. Anything that writes, deletes, runs a shell or reaches the network waits for you. This is what most people want." },
    { v: "auto", t: "allow everything", tip: "Nothing waits. A session can run any command as you. Only for a throwaway machine." },
  ];
  const REACH = [
    { v: "local", t: "this machine only", tip: "The board listens on 127.0.0.1. Nothing off this machine can open it." },
    { v: "net", t: "my overlay network", tip: "Opens the access screen, where a zrok or OpenZiti share is added so a phone or a second laptop can open the board." },
  ];
  async function hInit() {
    H = { step: 0, applying: false, applied: false, err: "", perm: "risky", folders: {}, extraFolders: [], hook: true, reach: "local",
      was: { auto: false, open: "", hook: false } };
    try {
      const s = await api("/v1/settings");
      H.was.auto = !!s.global_auto;
      H.was.open = String(s.open_paths || "");
    } catch (e) { H.err = "could not read the current settings: " + e.message; }
    try { const rep = await api("/v1/hooks"); H.was.hook = rep.missing === 0; H.hook = rep.missing !== 0; } catch (e) {}
    const open = H.was.open.split("\n").map(x => x.trim()).filter(Boolean);
    open.forEach(p => { H.folders[p] = true; });
    await loadFolders();
    folders.forEach(f => { if (!(f.path in H.folders)) H.folders[f.path] = false; });
    H.perm = H.was.auto ? "auto" : "risky";
  }
  const permName = v => PERMS.find(x => x.v === v).t;
  function stepBody() {
    const s = STEPS[H.step];
    if (s === "permissions") return `<div class="field"><label class="eyebrow">when an agent asks to run something${help("This is the permission gate. atrium holds a tool call until you answer, and this sets the default for every card. Standing rules under perms still win.")}</label>
      <div class="fr-opts" role="radiogroup">${PERMS.map(o => `<button type="button" class="fr-opt${H.perm === o.v ? " on" : ""}" role="radio" aria-checked="${H.perm === o.v}" data-perm="${o.v}"><span class="fr-radio"></span><span class="fr-ot">${o.t}</span>${help(o.tip)}${o.v === "risky" ? `<span class="chip ok">suggested</span>` : ""}${o.v === "auto" ? `<span class="chip warn">not advised</span>` : ""}</button>`).join("")}</div></div>`;
    if (s === "folders") return `<div class="field"><label class="eyebrow">folders agents may send files to${help("Files leave a card only to the card itself, an atrium path or a folder on this list. Anything else is refused, whatever the agent was asked.")}</label>
      <div class="fr-opts">${Object.keys(H.folders).length ? Object.keys(H.folders).map(f => `<button type="button" class="fr-opt${H.folders[f] ? " on" : ""}" role="checkbox" aria-checked="${H.folders[f]}" data-folder="${esc(f)}"><span class="fr-box"></span><code class="fr-ot">${esc(home(f))}</code></button>`).join("") : `<div class="empty">no folders to offer yet. add one below, or leave it empty to keep files in the card only</div>`}</div>
      <div class="copyrow fr-addf"><input type="text" id="fr-addf" spellcheck="false" placeholder="~/another/folder"><button type="button" data-h="addf">add</button></div></div>`;
    if (s === "guard") return `<div class="field"><label class="eyebrow">what the guard stops${help("The guard is a hook in the agent's settings. It runs before every tool call. A hook never fails a session: if atrium is down the call goes through to the permission gate.")}</label>
      <div class="fr-opts"><button type="button" class="fr-opt${H.hook ? " on" : ""}" role="checkbox" aria-checked="${H.hook}" data-gd="hook"${H.was.hook ? " disabled" : ""}><span class="fr-box"></span><span class="fr-ot">${H.was.hook ? "the guard hook is already installed for claude" : "install the guard hook for claude"}</span>${help("Adds atrium's hook entries to ~/.claude/settings.json and keeps a backup. Your own hooks are left alone.")}</button></div>
      <div class="fr-note">the git and secrets guard is switched on with ATRIUM_GUARD in the environment atrium runs in. It is not a setting here.</div></div>`;
    if (s === "reach") return `<div class="field"><label class="eyebrow">who can open this board${help("Anyone who can open the board can approve a tool call, so reach and sign-in go together.")}</label>
      <div class="fr-opts" role="radiogroup">${REACH.map(o => `<button type="button" class="fr-opt${H.reach === o.v ? " on" : ""}" role="radio" aria-checked="${H.reach === o.v}" data-reach="${o.v}"><span class="fr-radio"></span><span class="fr-ot">${o.t}</span>${help(o.tip)}</button>`).join("")}</div></div>`;
    return `<div class="fr-review">${review().map(r => `<div class="fr-rrow"><span class="fr-k">${r[0]}</span><span class="fr-was">${esc(r[1])}</span><span class="fr-arrow">&rarr;</span><b>${esc(r[2])}</b></div>`).join("")}</div>${H.err ? `<div class="fr-note warn">${esc(H.err)}</div>` : ""}`;
  }
  const chosen = () => Object.keys(H.folders).filter(f => H.folders[f]);
  function review() {
    const was = H.was.open.split("\n").map(x => x.trim()).filter(Boolean);
    return [
      ["permissions", H.was.auto ? permName("auto") : permName("ask"), permName(H.perm)],
      ["open folders", was.length ? was.map(home).join(", ") : "the card only", chosen().length ? chosen().map(home).join(", ") : "the card only"],
      ["guard hook", H.was.hook ? "installed" : "not installed", H.hook || H.was.hook ? "installed for claude" : "not installed"],
      ["board reach", "this machine only", REACH.find(x => x.v === H.reach).t],
    ];
  }
  function hdrender() {
    if (H.applied) {
      hd.innerHTML = `<div class="dlg-head"><div class="grow"><div class="dlg-eyebrow">harden this setup</div><h2>setup hardened</h2></div><button type="button" class="x" data-h="close" aria-label="close">&times;</button></div>
        <div class="dlg-body"><div class="fr-review">${review().map(r => `<div class="fr-rrow"><span class="fr-tick sm">&#10003;</span><span class="fr-k">${r[0]}</span><b>${esc(r[2])}</b></div>`).join("")}</div></div>
        <div class="dlg-foot"><button type="button" class="linkbtn" data-h="undo">undo all of it</button><span class="grow"></span><button type="button" class="go big" data-h="close">done</button></div>`;
      return;
    }
    if (H.applying) {
      hd.innerHTML = `<div class="dlg-head"><div class="grow"><div class="dlg-eyebrow">harden this setup</div><h2>applying</h2></div></div>
        <div class="dlg-body"><div class="fr-review">${review().map((r, i) => `<div class="fr-rrow" id="fr-ap${i}"><span class="fr-tick sm"></span><span class="fr-k">${r[0]}</span><b>${esc(r[2])}</b></div>`).join("")}</div></div>
        <div class="dlg-foot"><span class="grow"></span><button type="button" class="go big" disabled>applying...</button></div>`;
      return;
    }
    hd.innerHTML = `<div class="dlg-head"><div class="grow"><div class="dlg-eyebrow">harden this setup &middot; step ${H.step + 1} of ${STEPS.length}</div><h2>${TITLES[H.step]}</h2></div>
        <button type="button" class="x" data-h="close" aria-label="close">&times;</button></div>
      <div class="fr-steps-bar">${STEPS.map((s, i) => `<button type="button" class="fr-dot${i === H.step ? " on" : ""}${i < H.step ? " past" : ""}" data-h="goto" data-i="${i}"${i > H.step ? " disabled" : ""}>${i + 1}<span>${s}</span></button>`).join("")}</div>
      <div class="dlg-body">${stepBody()}</div>
      <div class="dlg-foot"><button type="button" class="linkbtn" data-h="close">skip for now</button><span class="grow"></span>
        <button type="button" data-h="back"${H.step === 0 ? " disabled" : ""}>back</button>
        <button type="button" class="go big" data-h="${H.step === STEPS.length - 1 ? "apply" : "next"}">${H.step === STEPS.length - 1 ? "apply" : "next"}</button></div>`;
  }
  window.openHarden = async function () {
    const n = $("fr-nudge"); if (n) n.remove();
    await hInit(); hdrender(); if (!hd.open) hd.showModal();
  };
  const post = (path, body) => api(path, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) });
  async function applyAll() {
    const undo = { auto: H.was.auto, open: H.was.open, hook: H.was.hook, rules: [] };
    const tick = i => { const row = $("fr-ap" + i); if (row) row.querySelector(".fr-tick").innerHTML = "&#10003;"; };
    await post("/v1/settings", { global_auto: H.perm === "auto", global_auto_minutes: 0 });
    if (H.perm === "risky") {
      for (const f of folders.map(x => x.path)) {
        for (const tool of ["Read", "Glob", "Grep"]) {
          try { const r = await post("/v1/rules", { tool, prefix: f, kind: "path", decision: "approve", reason: "first-run setup: reads in a project folder" }); if (r && r.id) undo.rules.push(r.id); } catch (e) {}
        }
      }
    }
    tick(0);
    await post("/v1/settings", { open_paths: chosen().join("\n") });
    tick(1);
    if (H.hook && !H.was.hook) await post("/v1/hooks/install", {});
    tick(2);
    tick(3);
    put(K.undo, JSON.stringify(undo));
  }
  async function undoAll() {
    let u = {};
    try { u = JSON.parse(get(K.undo) || "{}"); } catch (e) {}
    await post("/v1/settings", { global_auto: !!u.auto, global_auto_minutes: 0 });
    await post("/v1/settings", { open_paths: u.open || "" });
    for (const id of u.rules || []) { try { await api("/v1/rules/" + encodeURIComponent(id), { method: "DELETE" }); } catch (e) {} }
  }
  hd.addEventListener("click", async e => {
    const t = e.target;
    const o = t.closest("[data-perm],[data-folder],[data-gd],[data-reach]");
    if (o && !o.disabled && !t.closest(".help")) {
      if (o.dataset.perm) H.perm = o.dataset.perm;
      else if (o.dataset.folder) H.folders[o.dataset.folder] = !H.folders[o.dataset.folder];
      else if (o.dataset.gd) H.hook = !H.hook;
      else if (o.dataset.reach) H.reach = o.dataset.reach;
      hdrender(); return;
    }
    const b = t.closest("[data-h]"); if (!b || b.disabled) return;
    const k = b.dataset.h;
    if (k === "close") { hd.close(); return; }
    if (k === "next") { H.step++; hdrender(); }
    else if (k === "back") { H.step--; hdrender(); }
    else if (k === "goto") { H.step = +b.dataset.i; hdrender(); }
    else if (k === "addf") {
      const v = $("fr-addf").value.trim(); if (!v) { $("fr-addf").focus(); return; }
      H.folders[slash(v)] = true; hdrender();
    } else if (k === "apply") {
      H.applying = true; H.err = ""; hdrender();
      try { await applyAll(); } catch (err) {
        H.applying = false; H.step = STEPS.length - 1;
        H.err = "could not apply it: " + err.message + ". nothing after that step was changed, so apply again or undo from the checklist";
        hdrender(); return;
      }
      H.applying = false; H.applied = true; put(K.hardened, "1");
      hdrender(); painted = ""; paint().catch(() => {});
      if (H.reach === "net") {
        hd.close();
        try { $("gear").click(); showSettingsPane("accessing atrium"); } catch (err) {}
      }
    } else if (k === "undo") {
      try { await undoAll(); } catch (err) { say("could not undo it", err.message + ". set the values under settings"); return; }
      put(K.hardened, null); put(K.undo, null);
      hd.close(); painted = ""; paint().catch(() => {});
      say("undone", "permissions and open folders are back as they were. the guard hook stays installed: remove it under hooks");
    }
  });
})();
