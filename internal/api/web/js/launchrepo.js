// ── the launch dialog's `repo` list ─────────────────────
//
// docs/changes/launch-repo-picker.md. `repo` opens a panel INSIDE the launch dialog, never a second modal over it:
//
//   this room's checkouts   GET /v1/checkouts, which the room builds from the directories of its own cards (live,
//                           finished and history), each resolved to its repository and worktree by reading .git, newest
//                           use first. Picking one fills the working directory.
//   the hub's repos         GET /_hub/git/repos, the same data as the repos tab (js/hubrepos.js). One this room has a
//                           checkout of is not listed twice: its checkout row says it is on the hub. One it has none of
//                           offers `clone from hub`: POST /v1/checkouts/clone makes the clone on that room from the
//                           hub's copy (never a forge), into <scm root>/<host>/<org>/<repo>, and fills the directory.
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

// A hub repo and a checkout name the same repository when their host/org/repo agree.
const launchRepoSlug = r => [r.host, r.owner, r.repo].join("/").toLowerCase();

// The rows to draw: checkouts first, newest use first, then the hub's repos this room has no checkout of.
function launchRepoRows(memo) {
  const have = new Set((memo.checkouts || []).map(c => (c.slug || "").toLowerCase()).filter(Boolean));
  const onHub = new Set((memo.hub || []).map(launchRepoSlug));
  const rows = (memo.checkouts || []).map(c => ({ kind: "checkout", c, onHub: !!c.slug && onHub.has(c.slug.toLowerCase()) }));
  (memo.hub || []).forEach(r => {
    if (!have.has(launchRepoSlug(r))) rows.push({ kind: "hub", slug: r.host + "/" + r.owner + "/" + r.repo, r });
  });
  return rows;
}

function launchRepoMatches(row, q) {
  const text = row.kind === "checkout"
    ? [row.c.repo, row.c.slug, row.c.path, row.c.branch].join(" ")
    : row.slug;
  return text.toLowerCase().includes(q);
}

function paintLaunchRepos() {
  const list = document.getElementById("l-nw-list");
  const memo = launchRepoMemo[launchRepoRoom()];
  if (!memo) { list.innerHTML = `<div class="empty">reading…</div>`; return; }
  const q = (document.getElementById("l-nw-filter").value || "").trim().toLowerCase();
  const all = launchRepoRows(memo);
  const rows = q ? all.filter(r => launchRepoMatches(r, q)) : all;
  if (!rows.length) {
    list.innerHTML = `<div class="empty">${all.length ? "nothing matches that." :
      "no repositories yet. <b>browse</b> still points a card at any directory."}</div>`;
  } else {
    list.innerHTML = rows.map(row => {
      if (row.kind === "hub") {
        return `<div class="pvrepo">
          <span class="nm">${esc(row.slug)}</span>
          <span class="grow"></span>
          <span class="chip">on the hub</span>
          <button class="go nw-clone" data-repo="${esc(row.slug)}">clone from hub</button>
        </div>`;
      }
      const c = row.c;
      return `<div class="pvrepo">
        <span class="nm">${esc(c.repo)}</span>
        <code class="grow ell" data-tip="${esc(c.path)}">${esc(c.path)}</code>
        ${c.branch ? `<span class="chip">${esc(c.branch)}</span>` : ""}
        ${c.linked ? `<span class="chip">worktree</span>` : ""}
        ${row.onHub ? `<span class="chip" data-tip="the hub has this repository">hub</span>` : ""}
        <button class="go nw-use" data-path="${esc(c.path)}">use</button>
      </div>`;
    }).join("");
  }
  list.querySelectorAll(".nw-use").forEach(b => b.onclick = () => useLaunchRepoPath(b.dataset.path));
  list.querySelectorAll(".nw-clone").forEach(b => b.onclick = () => busyWhile(b, () => cloneFromHub(b.dataset.repo), "cloning…"));
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
  sel.innerHTML = opts.length
    ? opts.map(v => `<option value="${esc(v)}">${esc(v)}</option>`).join("")
    : `<option value="">no repositories yet</option>`;
  if (want && opts.includes(want)) sel.value = want;
}

// Filling the directory is the whole point: the panel closes and the dialog goes on.
function useLaunchRepoPath(path) {
  const into = document.getElementById("l-cwd");
  into.value = path;
  document.getElementById("l-nw").hidden = true;
  into.focus();
}

async function cloneFromHub(slug) {
  newWorkSay("");
  const room = launchRepoRoom();
  let out;
  try {
    out = await api("/v1/checkouts/clone", {
      method: "POST", headers: launchRepoHeaders(room, { "Content-Type": "application/json" }),
      body: JSON.stringify({ repo: slug })
    });
  } catch (e) {
    // In the dialog, where the list is. The dialog stays as it was.
    newWorkSay("could not clone " + slug + ": " + e.message, true);
    return;
  }
  delete launchRepoMemo[room];
  toast(out.state === "existing" ? "already there" : "cloned from the hub", out.path);
  useLaunchRepoPath(out.path);
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

async function openNewWork() {
  const box = document.getElementById("l-nw");
  box.hidden = false;
  newWorkSay("");
  const filter = document.getElementById("l-nw-filter");
  filter.value = "";
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
  const name = document.getElementById("l-nw-name").value.trim();
  if (!repo) { newWorkSay("pick a repository first", true); return; }
  if (!name) { newWorkSay("name the work: it is the branch and the folder", true); return; }
  return busyWhile(btn, async () => {
    newWorkSay("");
    const room = launchRepoRoom();
    const headers = launchRepoHeaders(room, { "Content-Type": "application/json" });
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
      out.hook_error && "the setup hook failed: " + out.hook_error].filter(Boolean).join(". ");
    toast(out.existed && out.card && !out.created ? "already there" : "new work", warn || out.worktree);
    const card = out.card ? await api("/v1/tasks/" + encodeURIComponent(out.card)) : null;
    if (typeof refresh === "function") refresh();
    if (!card || !card.supervised) { switchView("board"); return; }
    if (launchWhere === "window") { popOutTask(card.id); return; }
    switchView("terms");
    openTerm(card);
  }, "starting…");
}
