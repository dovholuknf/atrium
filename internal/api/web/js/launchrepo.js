// ── the launch dialog's `repo` list ─────────────────────
//
// docs/changes/launch-repo-picker.md. `repo` opens a panel INSIDE the launch dialog, never a second modal over it:
//
//   this room's checkouts   GET /v1/checkouts, which the room builds from the directories of its own cards (live,
//                           finished and history), each resolved to its repository and worktree by reading .git, newest
//                           use first. Picking one fills the working directory.
//   the hub's repos         GET /_hub/git/repos, the same data as the repos tab (js/hubrepos.js). One this room has a
//                           list, deduplicated by host/org/repo with the checkouts: ONE LIST, one row per repository,
//                           the host/org/repo slug and nothing else (a checkout with no slug, its folder name).
//
// PICKING A ROW FILLS THE DIRECTORY, whatever the row is (docs/changes/repo-picker-just-pick.md):
//   a repo the room has        its checkout.
//   a repo only the hub has    POST /v1/checkouts/clone makes the clone on that room from the hub's copy, never a forge,
//                              into <scm root>/<host>/<org>/<repo>.
//   a repo neither has         typed in the filter as host/org/repo or a URL. POST /_hub/git/init makes the hub clone it
//                              from the forge first, then the room clones from the hub. Only the hub talks to a forge.
// A failure of either clone is a line in the dialog.
//
// THE ROOM SELECT DECIDES WHICH ROOM. Both the list and a clone go to the room the dialog's room control names, in
// `X-Atrium-Room`, and changing the control repaints the list.
//
// FAST. Nothing here waits on a disk or a process: the room answers /v1/checkouts from memory and the hub answers its
// repos from a snapshot. The last answers are kept per room and drawn at once on the next open, and the read that
// follows repaints only when it changed. A filter box narrows what is drawn and never reads again.
//
// NOTHING IN THIS FILE ASKS FOR PROVIDERS. A provider is still how a PR or branch worktree finds its repository, but
// the launch dialog no longer needs one to offer a list.

// What was last read, by room name ("" for a board with no hub), so reopening paints before the network answers.
const launchRepoMemo = {};

// The room the launch dialog is aimed at, "" when it asks nobody.
function launchRepoRoom() {
  const field = document.getElementById("l-room-field");
  return field && !field.hidden ? document.getElementById("l-room").value || "" : "";
}

function launchRepoHeaders(room, extra) {
  const h = Object.assign({}, extra || {});
  if (room) h["X-Atrium-Room"] = room;
  return h;
}

function newWorkSay(text, warn) {
  const n = document.getElementById("l-nw-note");
  n.classList.toggle("warn", !!warn);
  n.textContent = text || "";
}

function toggleNewWork() {
  const box = document.getElementById("l-nw");
  if (!box.hidden) { box.hidden = true; return; }
  return openNewWork();
}

// One repository is one row. A hub repo and a checkout name the same one when their host/org/repo agree, and "github"
// and "github.com" are the same host (the room writes the first, the hub's list the second in places).
const launchRepoSlug = r => [r.host, r.owner, r.repo].join("/").toLowerCase().replace(/^github\.com\//, "github/");
const launchRepoKey = s => (s || "").toLowerCase().replace(/^github\.com\//, "github/");

// The rows to draw, one per repository: a repo this room has a checkout of uses that checkout (the main one rather than
// a linked worktree, else the newest), then the hub's repos this room has none of. A checkout with no forge name is its
// own row, keyed by where it lives.
function launchRepoRows(memo) {
  const rows = [];
  const at = new Map();
  (memo.checkouts || []).forEach(c => {
    const key = launchRepoKey(c.slug) || "path:" + (c.root || c.path);
    const i = at.get(key);
    if (i === undefined) { at.set(key, rows.length); rows.push({ key, name: c.slug || c.repo, tip: c.slug || c.path, path: c.path, linked: !!c.linked }); return; }
    if (rows[i].linked && !c.linked) Object.assign(rows[i], { path: c.path, linked: false });
  });
  (memo.hub || []).forEach(r => {
    const key = launchRepoSlug(r);
    if (!at.has(key)) { at.set(key, rows.length); rows.push({ key, name: r.host + "/" + r.owner + "/" + r.repo, tip: r.host + "/" + r.owner + "/" + r.repo, slug: r.host + "/" + r.owner + "/" + r.repo }); }
  });
  return rows;
}

// What the operator typed, read as a repository the hub may not have yet: host/org/repo or a URL. Null when it is
// neither. The server checks it again, this only decides whether to offer the row.
// Every form a person copies is read by the one parser, `parseRepoText` in js/repolink.js.
function launchRepoTyped(q) {
  return parseRepoText(q, false);
}

// The `from` box takes the branch a pasted /tree/<branch> link names, and gives it back when the text stops naming one.
// A `from` the operator typed is never touched.
function syncTypedFrom(typed) {
  const from = document.getElementById("l-nw-from");
  if (typed && typed.branch && (!from.value || from.dataset.auto === "1")) {
    from.value = typed.branch;
    from.dataset.auto = "1";
  } else if (from.dataset.auto === "1" && !(typed && typed.branch)) {
    from.value = "";
    from.dataset.auto = "";
  }
}
document.getElementById("l-nw-from").addEventListener("input", e => { e.target.dataset.auto = ""; });

function launchRepoMatches(row, q) {
  return [row.name, row.tip].join(" ").toLowerCase().includes(q);
}

function paintLaunchRepos() {
  const list = document.getElementById("l-nw-list");
  const memo = launchRepoMemo[launchRepoRoom()];
  const raw = (document.getElementById("l-nw-filter").value || "").trim();
  const typed = launchRepoTyped(raw);
  syncTypedFrom(typed);
  if (!memo) { list.innerHTML = `<div class="empty">reading…</div>`; syncNewWorkGo(); return; }
  // A link narrows by the repository it names, not by its own text.
  const q = (typed ? typed.slug : raw).toLowerCase();
  const all = launchRepoRows(memo);
  const rows = q ? all.filter(r => launchRepoMatches(r, q)) : all;
  // A repository nobody here has: the hub clones it from the forge first, then the room clones from the hub.
  if (typed && !all.some(r => r.key === typed.key)) rows.push({ key: typed.key, name: typed.slug, tip: typed.url, typed });
  if (!rows.length) {
    list.innerHTML = `<div class="empty">${all.length ? "nothing matches that. type host/org/repo to fetch one the hub lacks." :
      "no repositories yet. type host/org/repo, or <b>browse</b> to point a card at any directory."}</div>`;
  } else {
    list.innerHTML = rows.map((row, i) => `<div class="pvrepo nw-pick" role="button" tabindex="0" data-i="${i}" data-tip="${esc(row.tip)}">
        <span class="nm">${esc(row.name)}</span>
      </div>`).join("");
    list.querySelectorAll(".nw-pick").forEach(e => {
      const go = () => pickLaunchRepo(rows[+e.dataset.i], e);
      e.onclick = go;
      e.onkeydown = ev => { if (ev.key === "Enter" || ev.key === " ") { ev.preventDefault(); go(); } };
    });
  }
  paintNewWorkRepos(memo);
}
// The `start new work` form's repository box: every repo with a forge name, as host/org/repo.
function paintNewWorkRepos(memo) {
  const sel = document.getElementById("l-nw-repo");
  const want = sel.value;
  const names = new Set();
  const add = slug => { if (slug) names.add(slug.replace(/^github\//, "github.com/")); };
  (memo.checkouts || []).forEach(c => add(c.slug));
  (memo.hub || []).forEach(r => add(r.host + "/" + r.owner + "/" + r.repo));
  const opts = [...names].sort();
  // What was typed names a repo: the box offers it, selected, whether or not anybody here holds it. A repo nobody holds
  // says it will be fetched, and `start new work` does that first (fetchTypedRepo).
  const typed = launchRepoTyped(document.getElementById("l-nw-filter").value);
  const known = typed && opts.find(v => launchRepoKey(v) === typed.key);
  const fetchIt = typed && !known;
  sel.innerHTML = (fetchIt ? `<option value="${esc(typed.slug)}" data-fetch="1">${esc(typed.slug)} (will be fetched)</option>` : "") +
    (opts.length
      ? opts.map(v => `<option value="${esc(v)}">${esc(v)}</option>`).join("")
      : fetchIt ? "" : `<option value="">no repositories yet</option>`);
  if (typed) sel.value = known || typed.slug;
  else if (want && opts.includes(want)) sel.value = want;
  syncNewWorkGo();
}

// `start new work` waits for the box to say what was typed: with a repo named in the filter, the selection is that repo.
function syncNewWorkGo() {
  const typed = launchRepoTyped(document.getElementById("l-nw-filter").value);
  const sel = document.getElementById("l-nw-repo");
  const go = document.getElementById("l-nw-go");
  const off = !!typed && launchRepoKey(sel.value) !== typed.key;
  go.disabled = off;
  if (off) go.setAttribute("data-tip", "the repository box is not " + typed.slug); else go.removeAttribute("data-tip");
}
document.getElementById("l-nw-repo").addEventListener("change", syncNewWorkGo);

// A link pasted into the working directory box is not a directory: it moves to the repo panel's filter, as typed there.
// Typing a path one character at a time never trips this, only a paste or a fill (an `input` of more than one character).
document.getElementById("l-cwd").addEventListener("input", e => {
  const cwd = e.target;
  if (e.inputType === "insertText" && (e.data || "").length <= 1) return;
  const text = cwd.value.trim();
  if (!parseRepoText(text, true)) return;
  cwd.value = "";
  openNewWork(text);
});

// Filling the directory is the whole point: the panel closes and the dialog goes on.
function useLaunchRepoPath(path) {
  const into = document.getElementById("l-cwd");
  into.value = path;
  document.getElementById("l-nw").hidden = true;
  into.focus();
}

// Picking a row fills the directory. A repo the room has is its checkout. One it lacks is cloned from the hub first, and
// one the hub lacks is cloned onto the hub from the forge before that: only the hub talks to a forge. A failure is a
// line in the dialog, which stays as it was.
async function pickLaunchRepo(row, el) {
  if (!row) return;
  if (row.path) { useLaunchRepoPath(row.path); return; }
  if (el.classList.contains("busy")) return;
  el.classList.add("busy");
  const nm = el.querySelector(".nm");
  const was = nm.textContent;
  nm.textContent = was + " …";
  try {
    await cloneFromHub(row.typed ? row.typed : row.slug, !!row.typed);
  } finally {
    el.classList.remove("busy");
    nm.textContent = was;
  }
}
async function cloneFromHub(slug, hubLacks) {
  newWorkSay("");
  const room = launchRepoRoom();
  const say = (what, e) => newWorkSay("could not " + what + ": " + e.message, true);
  if (hubLacks) {
    const t = slug;
    newWorkSay("the hub is fetching " + t.slug + "…");
    try {
      await api("/_hub/git/init", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ url: t.url }) });
    } catch (e) {
      say("fetch " + t.slug + " onto the hub", e);
      return;
    }
    slug = t.host + "/" + t.owner + "/" + t.repo;
  }
  let out;
  try {
    out = await api("/v1/checkouts/clone", {
      method: "POST", headers: launchRepoHeaders(room, { "Content-Type": "application/json" }),
      body: JSON.stringify({ repo: slug })
    });
  } catch (e) {
    // In the dialog, where the list is. The dialog stays as it was.
    say("clone " + slug, e);
    return;
  }
  newWorkSay("");
  delete launchRepoMemo[room];
  toast(out.state === "existing" ? "already there" : "cloned from the hub", out.path);
  useLaunchRepoPath(out.path);
}
// A repo nobody here holds, before `start new work` can branch it: the hub fetches it from the forge (the same
// /_hub/git/init as cloneFromHub), then the room clones it from the hub with no worktree and no card (`prepare`).
// False, with the reason in the dialog, when either refused.
async function fetchTypedRepo(t, room) {
  try {
    if (typeof hubIsHub !== "undefined" && hubIsHub) {
      newWorkSay("the hub is fetching " + t.slug + "…");
      await api("/_hub/git/init", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ url: t.url }) });
    }
    newWorkSay("cloning " + t.slug + "…");
    await api("/v1/new", { method: "POST", headers: launchRepoHeaders(room, { "Content-Type": "application/json" }),
      body: JSON.stringify({ repo: t.slug, prepare: true }) });
    delete launchRepoMemo[room];
    newWorkSay("");
    return true;
  } catch (e) {
    newWorkSay("could not fetch " + t.slug + ": " + e.message, true);
    return false;
  }
}
// Reads both lists for one room. Either may fail on its own: a board with no hub has no hub list, a room built before
// the list existed has no checkouts, and the other still draws.
async function loadLaunchRepos(room) {
  const [co, hub] = await Promise.all([
    api("/v1/checkouts", { headers: launchRepoHeaders(room) }).then(b => b.checkouts || []).catch(() => null),
    typeof hubIsHub !== "undefined" && hubIsHub
      ? fetch("/_hub/git/repos").then(r => r.ok ? r.json() : null).then(b => (b && b.repos) || null).catch(() => null)
      : Promise.resolve(null)
  ]);
  const prev = launchRepoMemo[room] || {};
  const next = { checkouts: co || prev.checkouts || [], hub: hub || prev.hub || [] };
  const changed = JSON.stringify(next) !== JSON.stringify(prev);
  launchRepoMemo[room] = next;
  return changed;
}

async function openNewWork(typedText) {
  const box = document.getElementById("l-nw");
  box.hidden = false;
  newWorkSay("");
  const filter = document.getElementById("l-nw-filter");
  filter.value = typedText || "";
  paintLaunchRepos();
  filter.focus();
  const room = launchRepoRoom();
  // The list the room last gave is on screen already, so only an answer that differs repaints.
  if (await loadLaunchRepos(room) && !box.hidden && room === launchRepoRoom()) paintLaunchRepos();
}

// The room control moved: the list is that room's.
document.getElementById("l-room").addEventListener("change", () => {
  if (document.getElementById("l-nw").hidden) return;
  openNewWork();
});

async function newWorkGo() {
  const btn = document.getElementById("l-nw-go");
  const repo = document.getElementById("l-nw-repo").value;
  const selected = document.getElementById("l-nw-repo").selectedOptions[0] || { dataset: {} };
  // Left empty, the name is made for it: paste, start, and a session needs nothing else.
  const nameBox = document.getElementById("l-nw-name");
  if (!nameBox.value.trim()) nameBox.value = repoWorkBranch();
  const name = nameBox.value.trim();
  if (!repo) { newWorkSay("pick a repository first", true); return; }
  const typed = launchRepoTyped(document.getElementById("l-nw-filter").value);
  if (typed && launchRepoKey(repo) !== typed.key) { newWorkSay("the repository box is not " + typed.slug, true); return; }
  return busyWhile(btn, async () => {
    newWorkSay("");
    const room = launchRepoRoom();
    const headers = launchRepoHeaders(room, { "Content-Type": "application/json" });
    if (selected.dataset.fetch && !(await fetchTypedRepo(typed || launchRepoTyped(repo), room))) return;
    const picker = document.getElementById("l-harness");
    let out;
    try {
      out = await api("/v1/new", {
        method: "POST", headers, body: JSON.stringify({
          repo, name,
          from: document.getElementById("l-nw-from").value.trim(),
          prompt: document.getElementById("l-prompt").value.trim(),
          title: document.getElementById("l-title").value.trim(),
          why: document.getElementById("l-why").value.trim(),
          harness: document.getElementById("l-pick-field").hidden ? (launchTarget && launchTarget.harness) : picker.value,
          model: document.getElementById("l-model").value.trim(),
          effort: document.getElementById("l-effort").value.trim()
        })
      });
    } catch (e) {
      // In the dialog, with the form still filled in.
      newWorkSay(e.message, true);
      return;
    }
    document.getElementById("launch").close();
    if (typeof rememberLaunchRoom === "function") rememberLaunchRoom(room, out.card);
    const warn = [out.fetch_error && "fetching origin failed, so the base is what was last fetched",
      out.hook_error && "the setup hook failed: " + out.hook_error, out.hook_warning].filter(Boolean).join(". ");
    toast(out.existed && out.card && !out.created ? "already there" : "new work", warn || out.worktree);
    const card = out.card ? await api("/v1/tasks/" + encodeURIComponent(out.card)) : null;
    if (typeof refresh === "function") refresh();
    if (!card || !card.supervised) { switchView("board"); return; }
    if (launchWhere === "window") { popOutTask(card.id); return; }
    switchView("terms");
    openTerm(card);
  }, "starting…");
}
