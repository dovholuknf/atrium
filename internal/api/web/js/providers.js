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
  if (typeof roomUnnamed === "function" && roomUnnamed()) return;
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
      <code class="grow ell" data-tip="${esc(p.root)}">${esc(p.root)}</code>
      ${p.worktrees ? `<span class="chip" data-tip="${
        esc("worktrees go under " + p.worktree_root)}">worktrees</span>` : ""}
      ${p.last_error ? `<span class="chip warn" data-tip="${esc(p.last_error)}">${
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
          <code class="grow ell" data-tip="${esc(r.path)}">${esc(r.path)}</code>
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
