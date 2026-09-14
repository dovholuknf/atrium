// ── projects ────────────────────────────────────────────
// Every repository the daemon can see, and the worktrees that already exist
// for each, so starting work does not mean leaving the board to make a
// directory and browsing back to it.
//
// The daemon scans, for the same reason it browses: the repositories are on
// the machine atrium runs on, which is not the machine this page is open on.
// And the daemon does not MAKE the worktree, it runs the command that does.
// See internal/api/projects.go.
const projDlg = document.getElementById("projects");
let projData = { projects: [], roots: [], depth: 2, worktree_command: "" };
// Which repository is open, by path. One at a time: the list is long and a row
// expanded three deep is a list you scroll past rather than read.
let projOpen_ = "";
let projInto_ = "l-cwd";
let projBusy_ = false;

// Escape closes it, from wherever the caret is, and stops there. Same
// reasoning as the directory picker: this opens over the launch form, and one
// press must not close both.
document.addEventListener("keydown", e => {
  if (e.key !== "Escape" || !projDlg.open) return;
  e.preventDefault();
  e.stopPropagation();
  projDlg.close();
}, true);

async function openProjects(fieldID) {
  projInto_ = fieldID || "l-cwd";
  projOpen_ = "";
  const list = document.getElementById("p-list");
  if (list) list.innerHTML = `<div class="empty">looking for repositories…</div>`;
  projDlg.showModal();
  await loadProjects();
}

async function loadProjects() {
  try {
    projData = await api("/v1/projects");
  } catch (e) {
    const list = document.getElementById("p-list");
    if (list) list.innerHTML = `<div class="empty">${esc(e.message)}</div>`;
    return;
  }
  const crumb = document.getElementById("p-where");
  if (crumb) {
    const n = (projData.projects || []).length;
    crumb.textContent = n + (n === 1 ? " repository" : " repositories") +
      " in " + (projData.roots || []).length + " place(s), " +
      projData.depth + " level(s) down" +
      (projData.truncated ? " — stopped early, there are more" : "");
  }
  drawProjects();
}

// The filter is applied here rather than by hiding rows, so an org with
// nothing matching takes no space at all.
function projectsMatching() {
  const q = (document.getElementById("p-filter").value || "").trim().toLowerCase();
  const all = projData.projects || [];
  if (!q) return all;
  return all.filter(p =>
    (p.name + " " + p.group + " " + p.path).toLowerCase().includes(q) ||
    (p.worktrees || []).some(w => w.branch.toLowerCase().includes(q)));
}

function drawProjects() {
  const list = document.getElementById("p-list");
  if (!list) return;
  const rows = projectsMatching();
  if (!rows.length) {
    list.innerHTML = `<div class="empty">no repositories here. settings, this machine,
      the picker may open, is where the scan starts.</div>`;
    return;
  }
  list.innerHTML = rows.map(p => projectRow(p)).join("");
  // Every path travels in an attribute and comes back through the DOM, never
  // through an inline handler: a repository called `it's mine` would end a
  // quoted string literal, and HTML escaping is not JavaScript escaping.
  list.querySelectorAll(".proj-head").forEach(b =>
    b.onclick = () => { projOpen_ = projOpen_ === b.dataset.path ? "" : b.dataset.path; drawProjects(); });
  list.querySelectorAll(".proj-use").forEach(b =>
    b.onclick = e => { e.stopPropagation(); useProjectPath(b.dataset.path); });
  list.querySelectorAll(".proj-make").forEach(b =>
    b.onclick = () => makeWorktree(b.dataset.path));
  const box = list.querySelector(".proj-branch");
  if (box) {
    box.focus();
    box.onkeydown = e => {
      if (e.key !== "Enter") return;
      e.preventDefault();
      makeWorktree(box.dataset.path);
    };
  }
}

function projectRow(p) {
  const open = projOpen_ === p.path;
  const wts = p.worktrees || [];
  const count = wts.length
    ? wts.length + (wts.length === 1 ? " worktree" : " worktrees")
    : "no worktrees";
  return `
    <div class="proj ${open ? "open" : ""}">
      <button class="dir repo proj-head" data-path="${esc(p.path)}" title="${esc(p.path)}">
        <span class="ic">${open ? "&#9662;" : "&#9656;"}</span>
        <span class="nm">${esc(p.group)}/${esc(p.name)}</span>
        <span class="proj-count">${esc(count)}</span>
      </button>
      ${open ? `
      <div class="proj-body">
        <button class="dir proj-use" data-path="${esc(p.path)}" title="${esc(p.path)}">
          <span class="ic">&#9679;</span>
          <span class="nm">the checkout itself</span>
          <span class="proj-path">${esc(p.path)}</span>
        </button>
        ${wts.map(w => `
          <button class="dir proj-use" data-path="${esc(w.path)}" title="${esc(w.path)}">
            <span class="ic">&#9656;</span>
            <span class="nm">${esc(w.branch)}</span>
            <span class="proj-path">${esc(w.path)}</span>
          </button>`).join("")}
        ${projData.worktree_command ? `
        <div class="proj-new">
          <input class="proj-branch" spellcheck="false" data-path="${esc(p.path)}"
            placeholder="a branch name, and atrium runs the command below">
          <button class="go proj-make" data-path="${esc(p.path)}">make a worktree</button>
        </div>
        <div class="proj-cmd">Runs <code>${esc(projData.worktree_command)}</code> in
          ${esc(p.path)}, on the machine atrium is on. A branch that already has a worktree
          takes you there instead.</div>` : `
        <div class="proj-cmd">No worktree command is set, so atrium can only take you to
          one that exists. Settings, this machine, make a worktree with.</div>`}
      </div>` : ""}
    </div>`;
}

function useProjectPath(path) {
  const into = document.getElementById(projInto_) || document.getElementById("l-cwd");
  if (into) into.value = path;
  projDlg.close();
}

// THE COMMON CASE IS THAT IT ALREADY EXISTS, and the daemon answers that with
// the directory rather than an error, so both answers land here the same way:
// the field gets a path and the dialog closes.
async function makeWorktree(repo) {
  if (projBusy_) return;
  const box = document.querySelector(".proj-branch");
  const branch = ((box && box.value) || "").trim();
  if (!branch) { toast("no branch", "type a branch name first"); return; }
  projBusy_ = true;
  const btn = document.querySelector(".proj-make");
  if (btn) { btn.disabled = true; btn.textContent = "making…"; }
  let made;
  try {
    made = await api("/v1/projects/worktree", {
      method: "POST",
      body: JSON.stringify({ repo: repo, branch: branch })
    });
  } catch (e) {
    projBusy_ = false;
    if (btn) { btn.disabled = false; btn.textContent = "make a worktree"; }
    tellUser("no worktree", e.message);
    return;
  }
  projBusy_ = false;
  toast(made.existed ? "already there" : "worktree made", made.path);
  useProjectPath(made.path);
}
