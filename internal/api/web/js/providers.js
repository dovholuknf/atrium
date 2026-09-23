// ── providers ───────────────────────────────────────────
//
// Where your repositories live, told to atrium once rather than rediscovered.
//
// This replaces a `projects` button that SCANNED for anything holding a `.git`
// two levels down and then shelled out to a command template to make a
// worktree. That inferred a layout from whatever directories happened to exist,
// knew nothing about what an org was, and ran a command atrium could not see
// inside. A provider is told the layout, so the path is computed rather than
// found and the make is `git worktree add` with no shell anywhere near it.
//
// THE BOARD RULE THIS FILE KEEPS THROUGHOUT: every path and name travels in a
// `data-` attribute and comes back through the DOM, never through an inline
// handler. A repository called `it's mine` ends a quoted string literal, and
// HTML escaping is not JavaScript escaping.

let allProviders = [];
// Repository rows per provider, fetched when a provider's list is opened
// rather than with the page. A provider with four hundred rows is four hundred
// rows nobody asked for until they did.
const providerRepos = {};
// Which provider's repositories are open in the pane, by name. One at a time:
// three lists of two hundred is a page you scroll past rather than read.
let providerOpen = "";

async function renderProviders() {
  const host = document.getElementById("provider-list");
  if (!host) return;
  try { allProviders = (await api("/v1/providers")).providers || []; } catch (e) { return; }

  if (!allProviders.length) {
    setHTML(host, `<div class="panel"><div class="empty">
      nothing describes where your repositories live yet. a provider is a name, a root folder
      and the layout under it, and defining one adopts every checkout already there.
    </div></div>`);
    return;
  }

  setHTML(host, `<div class="panel">` + allProviders.map(p => providerRow(p)).join("") + `</div>`);

  host.querySelectorAll(".pv-edit").forEach(b =>
    b.onclick = () => editProvider(b.dataset.name));
  host.querySelectorAll(".pv-open").forEach(b =>
    b.onclick = () => {
      providerOpen = providerOpen === b.dataset.name ? "" : b.dataset.name;
      if (providerOpen) loadProviderRepos(providerOpen).then(renderProviders);
      else renderProviders();
    });
  host.querySelectorAll(".pv-look").forEach(b =>
    b.onclick = () => discoverProvider(b.dataset.name));
  // `use` FROM THE PANE OPENS THE LAUNCH FORM, rather than filling a field on
  // a dialog nobody has open. Same button word as in the picker because it is
  // the same intent, and the two differ only in where you already were.
  host.querySelectorAll(".pv-use").forEach(b =>
    b.onclick = () => openLaunch(null, false, b.dataset.path));
  host.querySelectorAll(".pv-hide").forEach(b =>
    b.onclick = () => hideRepo(b.dataset.name, b.dataset.org, b.dataset.repo, b.dataset.on === "1"));
  host.querySelectorAll(".pv-forget").forEach(b =>
    b.onclick = () => forgetRepo(b.dataset.name, b.dataset.org, b.dataset.repo));
  const filter = host.querySelector(".pv-filter");
  if (filter) filter.oninput = () => renderProviders();
}

function providerRow(p) {
  const open = providerOpen === p.name;
  const rows = providerRepos[p.name] || null;
  const count = rows ? rows.length + (rows.length === 1 ? " repository" : " repositories") : "";
  return `
    <div class="row line">
      ${switchChip("provider", p.name, "", p.enabled)}
      <span class="tool">${esc(p.name)}</span>
      <code class="grow ell" title="${esc(p.root)}">${esc(p.root)}</code>
      ${p.worktrees ? `<span class="chip" title="${
        esc("worktrees go under " + p.worktree_root)}">worktrees</span>` : ""}
      ${p.last_error ? `<span class="chip warn" title="${esc(p.last_error)}">${
        esc(shortly(p.last_error))}</span>` : ""}
      <span class="by">${esc(count)}</span>
      <button class="pv-open" data-name="${esc(p.name)}">${open ? "hide" : "repositories"}</button>
      <button class="pv-look" data-name="${esc(p.name)}">look again</button>
      <button class="pv-edit" data-name="${esc(p.name)}">edit</button>
    </div>
    ${open ? providerRepoList(p, rows) : ""}`;
}

// The repository list under a provider.
//
// A row whose directory is missing is drawn dim and KEEPS ITS BUTTONS. It is
// not a mistake: the row is what somebody declared, and the directory is what
// happens to be plugged in. Forgetting one is deliberate and discovery never
// does it, so an unplugged drive greys the list rather than erasing it.
function providerRepoList(p, rows) {
  if (rows === null) return `<div class="empty">reading…</div>`;
  const q = ((document.querySelector(".pv-filter") || {}).value || "").trim().toLowerCase();
  const shown = q
    ? rows.filter(r => (r.org + "/" + r.repo + " " + r.path).toLowerCase().includes(q))
    : rows;
  return `
    <div class="pvrepos">
      <input class="pv-filter" spellcheck="false" placeholder="filter" value="${esc(q)}">
      ${shown.length ? shown.map(r => `
        <div class="pvrepo ${r.present ? "" : "gone"} ${r.hidden ? "hid" : ""}">
          <span class="nm">${esc(r.org || "(no org)")}/${esc(r.repo)}</span>
          <code class="grow ell" title="${esc(r.path)}">${esc(r.path)}</code>
          ${r.present ? "" : `<span class="chip warn">not on disk</span>`}
          ${r.hidden ? `<span class="chip">hidden</span>` : ""}
          <button class="pv-use" data-path="${esc(r.path)}">use</button>
          <button class="pv-hide" data-name="${esc(p.name)}" data-org="${esc(r.org)}"
            data-repo="${esc(r.repo)}" data-on="${r.hidden ? "0" : "1"}">${
            r.hidden ? "unhide" : "hide"}</button>
          <button class="no pv-forget" data-name="${esc(p.name)}" data-org="${esc(r.org)}"
            data-repo="${esc(r.repo)}">forget</button>
        </div>`).join("") : `<div class="empty">nothing matches</div>`}
    </div>`;
}

function shortly(s) {
  s = String(s || "");
  return s.length > 40 ? s.slice(0, 38) + "…" : s;
}

async function loadProviderRepos(name) {
  try {
    providerRepos[name] = (await api(
      "/v1/providers/" + encodeURIComponent(name) + "/repos")).repos || [];
  } catch (e) {
    providerRepos[name] = [];
  }
  return providerRepos[name];
}

async function discoverProvider(name) {
  let d;
  try {
    d = await api("/v1/providers/" + encodeURIComponent(name) + "/discover", { method: "POST" });
  } catch (e) { tellUser("could not look", e.message); return; }
  // A run that could not read the root is not a failure of the request, so it
  // arrives here as an ordinary answer carrying its problem.
  if (d.error) tellUser("nothing adopted", d.error);
  else toast(name, d.summary);
  delete providerRepos[name];
  if (providerOpen === name) await loadProviderRepos(name);
  renderProviders();
}

async function hideRepo(name, org, repo, on) {
  const q = "?org=" + encodeURIComponent(org) + "&repo=" + encodeURIComponent(repo) +
    "&hide=" + (on ? "1" : "0");
  try {
    await api("/v1/providers/" + encodeURIComponent(name) + "/repos" + q, { method: "DELETE" });
  } catch (e) { tellUser("could not do that", e.message); return; }
  await loadProviderRepos(name);
  renderProviders();
}

async function forgetRepo(name, org, repo) {
  if (!await confirmUser("forget " + (org ? org + "/" : "") + repo + "?",
    "The row goes. Nothing on disk is touched and no card changes. A later look will adopt it " +
    "again if the directory is there, so use hide instead if you want it to stay gone.",
    "forget it")) return;
  const q = "?org=" + encodeURIComponent(org) + "&repo=" + encodeURIComponent(repo);
  try {
    await api("/v1/providers/" + encodeURIComponent(name) + "/repos" + q, { method: "DELETE" });
  } catch (e) { tellUser("could not forget it", e.message); return; }
  await loadProviderRepos(name);
  renderProviders();
}

// ── the provider dialog ─────────────────────────────────

function editProvider(name) {
  const p = allProviders.find(x => x.name === name) || {
    name: "", kind: "git", root: "", worktrees: false, worktree_root: "",
    host: "", enabled: true, exclude: "", max_repos: 0
  };
  const dlg = document.getElementById("provider");
  dlg.dataset.editing = name || "";
  document.getElementById("pv-heading").textContent = name ? "provider: " + name : "a provider";

  const nameBox = document.getElementById("pv-name");
  nameBox.value = p.name;
  // THE NAME IS THE KEY, so it is disabled once the row exists, and the reason
  // sits on the field rather than being left to be discovered. A rename would
  // be a delete plus an add, and anything holding the old string would be
  // silently orphaned.
  nameBox.disabled = !!name;
  document.getElementById("pv-name-said").textContent = name
    ? "This is the name everything refers to it by, so it cannot be changed. To rename, make a " +
      "new provider and delete this one."
    : "Whatever you want to call it. This is the name everything else will refer to it by, so it " +
      "cannot be changed once saved.";

  document.getElementById("pv-kind").value = p.kind || "git";
  document.getElementById("pv-root").value = p.root || "";
  document.getElementById("pv-worktrees").checked = !!p.worktrees;
  document.getElementById("pv-worktree-root").value = p.worktree_root || "";
  document.getElementById("pv-host").value = p.host || "";
  document.getElementById("pv-exclude").value = p.exclude || "";
  document.getElementById("pv-max").value = p.max_repos ? String(p.max_repos) : "";
  document.getElementById("pv-enabled").checked = p.enabled !== false;
  document.getElementById("pv-delete").hidden = !name;
  document.getElementById("pv-discover").hidden = !name;

  const scan = document.getElementById("pv-scan-field");
  scan.hidden = !p.last_scan && !p.last_error;
  document.getElementById("pv-scan").textContent = p.last_error || p.last_scan || "";

  document.getElementById("pv-blockers").hidden = true;
  paintProviderForm();
  dlg.showModal();
  if (!name) nameBox.focus();
}

// The worktree folder only exists while the toggle is on. Shown and hidden
// rather than disabled, because an empty field for a thing that is off is a
// question nobody was asked.
function paintProviderForm() {
  document.getElementById("pv-wt-field").hidden =
    !document.getElementById("pv-worktrees").checked;
}

async function saveProvider() {
  const dlg = document.getElementById("provider");
  const editing = dlg.dataset.editing;
  const name = editing || document.getElementById("pv-name").value.trim();
  if (!name) { tellUser("no name", "a provider needs a name to refer to it by"); return; }

  const body = {
    kind: document.getElementById("pv-kind").value,
    root: document.getElementById("pv-root").value.trim(),
    worktrees: document.getElementById("pv-worktrees").checked,
    worktree_root: document.getElementById("pv-worktree-root").value.trim(),
    host: document.getElementById("pv-host").value.trim(),
    exclude: document.getElementById("pv-exclude").value,
    max_repos: Number(document.getElementById("pv-max").value) || 0,
    enabled: document.getElementById("pv-enabled").checked
  };
  let saved;
  try {
    saved = await api("/v1/providers/" + encodeURIComponent(name), {
      method: "PUT", body: JSON.stringify(body)
    });
  } catch (e) {
    // THE TOGGLE CAME BACK ON, because the save was refused whole and the row
    // on the daemon still has it on. Leaving the box unticked would show a
    // state that is not the one stored.
    if (e.status === 409 && e.body && e.body.blockers) {
      document.getElementById("pv-worktrees").checked = true;
      paintProviderForm();
      showBlockers(e.body.error, e.body.blockers);
      return;
    }
    tellUser("could not save it", e.message);
    return;
  }
  dlg.close();
  // A new provider adopts what is already there, which is the whole point of
  // defining one. Done on save rather than behind a second button nobody would
  // know to press.
  if (!editing) await discoverProvider(saved.name);
  else renderProviders();
}

// The refusal, rendered under the field it is about rather than in a toast that
// goes away while you are reading it.
function showBlockers(message, blockers) {
  const box = document.getElementById("pv-blockers");
  box.hidden = false;
  box.textContent = message || ((blockers || []).length
    ? "that folder is not empty"
    : "");
}

async function checkWorktreeRoot() {
  const name = document.getElementById("provider").dataset.editing;
  if (!name) {
    tellUser("save it first", "there is nothing to check until the provider exists");
    return;
  }
  let out;
  try {
    out = await api("/v1/providers/" + encodeURIComponent(name) + "/check-worktrees",
      { method: "POST" });
  } catch (e) { tellUser("could not check", e.message); return; }
  const box = document.getElementById("pv-blockers");
  box.hidden = false;
  box.textContent = out.clear
    ? "that folder is empty, so worktree support can be turned off."
    : (out.error || "that folder is not empty.");
}

async function deleteProvider() {
  const name = document.getElementById("provider").dataset.editing;
  if (!name) return;
  if (!await confirmUser("delete the " + name + " provider?",
    "Its repository rows go with it. Nothing on disk is touched and NO CARD CHANGES: a card's " +
    "directory is what you typed, and it outlives anything describing it.",
    "delete it")) return;
  try {
    await api("/v1/providers/" + encodeURIComponent(name), { method: "DELETE" });
  } catch (e) {
    if (e.status === 409 && e.body && e.body.error) {
      showBlockers(e.body.error, e.body.blockers);
      return;
    }
    tellUser("could not delete it", e.message);
    return;
  }
  document.getElementById("provider").close();
  delete providerRepos[name];
  renderProviders();
}

// ── picking a repository for the launch form ────────────

const pickDlg = document.getElementById("pickrepo");
const whereDlg = document.getElementById("pickwhere");
let pickInto = "l-cwd";
let pickRows = [];
let pickWhere = null;

// Escape closes whichever of these is open and stops there. Same reasoning as
// the directory picker: these open over the launch form, and one press must not
// close both.
document.addEventListener("keydown", e => {
  if (e.key !== "Escape") return;
  const dlg = whereDlg.open ? whereDlg : (pickDlg.open ? pickDlg : null);
  if (!dlg) return;
  e.preventDefault();
  e.stopPropagation();
  dlg.close();
}, true);

async function openPickRepo(fieldID) {
  pickInto = fieldID || "l-cwd";
  const sel = document.getElementById("pk-provider");
  try { allProviders = (await api("/v1/providers")).providers || []; } catch (e) {}
  const usable = allProviders.filter(p => p.enabled);
  if (!usable.length) {
    tellUser("no providers yet",
      "Nothing describes where your repositories live. Open runners, providers, and add one. " +
      "Until then, browse still points a card at any directory.");
    return;
  }
  sel.innerHTML = usable.map(p =>
    `<option value="${esc(p.name)}">${esc(p.name)}</option>`).join("");
  document.getElementById("pk-filter").value = "";
  pickDlg.showModal();
  await loadPickRepos();
}

async function loadPickRepos() {
  const name = document.getElementById("pk-provider").value;
  const list = document.getElementById("pk-list");
  list.innerHTML = `<div class="empty">reading…</div>`;
  let out;
  try {
    out = await api("/v1/providers/" + encodeURIComponent(name) + "/repos");
  } catch (e) {
    list.innerHTML = `<div class="empty">${esc(e.message)}</div>`;
    return;
  }
  // A hidden row is a durable no, so it is not offered here either.
  pickRows = (out.repos || []).filter(r => !r.hidden);
  document.getElementById("pk-orgs").innerHTML =
    (out.orgs || []).map(o => `<option value="${esc(o)}">`).join("");
  paintPickList();
}

function paintPickList() {
  const list = document.getElementById("pk-list");
  const q = (document.getElementById("pk-filter").value || "").trim().toLowerCase();
  const rows = q
    ? pickRows.filter(r => (r.org + "/" + r.repo).toLowerCase().includes(q))
    : pickRows;
  if (!rows.length) {
    list.innerHTML = `<div class="empty">nothing here. name one below, or press look again on
      the provider to adopt what is on disk.</div>`;
    return;
  }
  list.innerHTML = rows.map(r => `
    <div class="pvrepo ${r.present ? "" : "gone"}">
      <span class="nm">${esc(r.org || "(no org)")}/${esc(r.repo)}</span>
      <code class="grow ell" title="${esc(r.path)}">${esc(r.path)}</code>
      ${r.present ? "" : `<span class="chip warn">not on disk</span>`}
      <button class="go pk-where" data-org="${esc(r.org)}" data-repo="${esc(r.repo)}"
        data-path="${esc(r.path)}">choose</button>
    </div>`).join("");
  list.querySelectorAll(".pk-where").forEach(b =>
    b.onclick = () => openWhere(b.dataset.org, b.dataset.repo, b.dataset.path));
}

// Typing an org and a repo that name no row is how a repository is ADDED. The
// path follows from the provider's root, and the directory not being there yet
// is allowed: atrium does not clone, and a directory you are about to make is a
// legitimate thing to point a card at.
async function addPickRepo() {
  const name = document.getElementById("pk-provider").value;
  const org = document.getElementById("pk-org").value.trim();
  const repo = document.getElementById("pk-repo").value.trim();
  if (!repo) { tellUser("no repository", "type a repository name"); return; }
  let saved;
  try {
    saved = await api("/v1/providers/" + encodeURIComponent(name) + "/repos", {
      method: "PUT", body: JSON.stringify({ org: org, repo: repo })
    });
  } catch (e) { tellUser("could not add it", e.message); return; }
  document.getElementById("pk-org").value = "";
  document.getElementById("pk-repo").value = "";
  await loadPickRepos();
  openWhere(saved.org, saved.repo, saved.path);
}

// The checkout, and every worktree that already exists for it, and a box to
// make one that does not. Its own step because a repository with six worktrees
// is six answers to one question.
async function openWhere(org, repo, path) {
  const name = document.getElementById("pk-provider").value;
  const p = allProviders.find(x => x.name === name) || {};
  pickWhere = { provider: name, org: org, repo: repo, path: path };
  document.getElementById("pw-heading").textContent = (org ? org + "/" : "") + repo;
  document.getElementById("pw-said").textContent = "";
  document.getElementById("pw-branch").value = "";
  document.getElementById("pw-make-field").hidden = !p.worktrees;
  paintWhere([]);
  whereDlg.showModal();

  // The worktrees a repository already has come from git, so this is the one
  // part of the picker that waits on a process. Drawn empty first, so the
  // checkout is pickable immediately.
  let out = { worktrees: [] };
  try {
    out = await api("/v1/providers/" + encodeURIComponent(name) + "/worktrees?org=" +
      encodeURIComponent(org) + "&repo=" + encodeURIComponent(repo));
  } catch (e) {}
  if (whereDlg.open) paintWhere(out.worktrees || []);
}

function paintWhere(worktrees) {
  const list = document.getElementById("pw-list");
  list.innerHTML = `
    <div class="pvrepo">
      <span class="nm">the checkout itself</span>
      <code class="grow ell" title="${esc(pickWhere.path)}">${esc(pickWhere.path)}</code>
      <button class="go pw-use" data-path="${esc(pickWhere.path)}">use</button>
    </div>
    ${worktrees.map(w => `
      <div class="pvrepo">
        <span class="nm">${esc(w.branch)}</span>
        <code class="grow ell" title="${esc(w.path)}">${esc(w.path)}</code>
        <button class="pw-use" data-path="${esc(w.path)}">use</button>
      </div>`).join("")}`;
  list.querySelectorAll(".pw-use").forEach(b =>
    b.onclick = () => useRepoPath(b.dataset.path));
}

// `git worktree add` and nothing else. It makes a directory and does NOT start
// a session, which is the defect in the command template this replaced: its
// default also launched, so make then start started two.
async function makeWorktree() {
  if (!pickWhere) return;
  const branch = document.getElementById("pw-branch").value.trim();
  if (!branch) { tellUser("no branch", "type a branch name first"); return; }
  const said = document.getElementById("pw-said");
  said.textContent = "making…";
  let made;
  try {
    made = await api("/v1/providers/" + encodeURIComponent(pickWhere.provider) + "/worktree", {
      method: "POST",
      body: JSON.stringify({ org: pickWhere.org, repo: pickWhere.repo, branch: branch })
    });
  } catch (e) {
    said.textContent = "";
    tellUser("no worktree", e.message);
    return;
  }
  said.textContent = "";
  // Already there is the answer, not an error: pressing make twice takes you to
  // the worktree rather than failing.
  toast(made.existed ? "already there" : (made.created_branch
    ? "worktree and branch made" : "worktree made"), made.path);
  useRepoPath(made.path);
}

// Filling the field and closing both dialogs is the whole point of this flow.
function useRepoPath(path) {
  const into = document.getElementById(pickInto) || document.getElementById("l-cwd");
  if (into) into.value = path;
  if (whereDlg.open) whereDlg.close();
  if (pickDlg.open) pickDlg.close();
}
