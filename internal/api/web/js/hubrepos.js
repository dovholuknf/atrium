// The hub's repos: a list of what rooms have pushed to the hub, and the clone URL of each.
//
// docs/rnd/hub-forge-design.md sections 2, 3.2 and 7 (u-new-hub-repos-list). A list, not a code browser: each repo
// shows its `main`, the branches rooms pushed (room, card, when) and two URLs to copy.
//
// HUB-ONLY, like the audit tab: it reveals itself once the hub probe finds a hub (`paintHubReposTab`, called from
// rooms.js). READ WHEN YOU GO THERE and again on the refresh button. No stream event and no timer.
//
// THE CARD TITLE IS NOT IN THE ANSWER. It is looked up from the board's own card list by room and card id, and a card
// that is gone shows just its room and id.

const hubRepos = { repos: [], loaded: false, note: "", inflight: false };

function paintHubReposTab() {
  const tab = document.querySelector('.tab[data-view="hubrepos"]');
  if (!tab) return;
  tab.hidden = typeof hubIsHub === "undefined" ? true : !hubIsHub;
}

function hubReposCardTitle(room, card) {
  if (!card || typeof lastTasks === "undefined") return "";
  const t = lastTasks.find(x => x.id === card && (!x.room || !room || x.room === room));
  return t ? (t.display_title || t.title || "") : "";
}

const hubReposSha = s => String(s || "").slice(0, 7);

function hubReposWhen(iso) {
  const s = iso ? sinceSecs(iso) : null;
  return s == null ? "" : ago(s) + " ago";
}

const HR_SVG = {
  branch: '<svg class="hr-glyph" viewBox="0 0 16 16" width="14" height="14" aria-hidden="true" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"><circle cx="4.5" cy="3.5" r="1.7"/><circle cx="4.5" cy="12.5" r="1.7"/><circle cx="11.5" cy="5.5" r="1.7"/><path d="M4.5 5.2v5.6M11.5 7.2c0 2.6-3.4 2.2-6.2 4"/></svg>',
  lock: '<svg viewBox="0 0 16 16" width="11" height="11" aria-hidden="true" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><rect x="3.5" y="7" width="9" height="6.5" rx="1.5"/><path d="M5.5 7V5a2.5 2.5 0 0 1 5 0v2"/></svg>',
  copy: '<svg viewBox="0 0 16 16" width="13" height="13" aria-hidden="true" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round"><rect x="5.5" y="5.5" width="8" height="8" rx="1.5"/><path d="M10.5 5.5v-2a1 1 0 0 0-1-1h-6a1 1 0 0 0-1 1v6a1 1 0 0 0 1 1h2"/></svg>',
  empty: '<svg class="hr-art" viewBox="0 0 120 72" width="120" height="72" aria-hidden="true" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M10 52h100" opacity=".35"/><circle cx="22" cy="52" r="4"/><path d="M26 52h26" stroke-dasharray="2 5" opacity=".6"/><circle cx="60" cy="52" r="4" opacity=".6"/><path d="M64 52h20" stroke-dasharray="2 5" opacity=".4"/><circle cx="98" cy="52" r="4" opacity=".35"/><path d="M60 48V26m0 0-7 7m7-7 7 7" stroke="var(--teal)"/></svg>'
};

function hubReposWhen(iso) {
  const s = iso ? sinceSecs(iso) : null;
  return s == null ? "" : ago(s) + " ago";
}

function hubReposExact(iso) {
  const d = iso ? new Date(iso) : null;
  return d && !isNaN(d) ? d.toLocaleString() : "";
}

function hubReposAge(iso, cls) {
  const w = hubReposWhen(iso);
  return w ? '<time class="' + cls + '" datetime="' + esc(iso) + '" title="' + esc(hubReposExact(iso)) + '">' + esc(w) + "</time>" : "";
}

function hubReposBranch(b) {
  const title = hubReposCardTitle(b.room, b.card);
  const who = (b.room ? esc(b.room) : "") + (b.card ? " " + esc(b.card) : "");
  return '<div class="hr-branch' + (b.released ? " released" : "") + '">' + HR_SVG.branch +
    '<span class="hr-name" title="' + esc(b.name) + '">' + esc(b.name) + "</span>" +
    '<span class="hr-sha">' + esc(hubReposSha(b.sha)) + "</span>" +
    '<span class="hr-who">' + (who ? '<span class="hr-chip">' + who + "</span>" : "") +
    (title ? ' <span class="hr-title">' + esc(title) + "</span>" : "") + "</span>" +
    hubReposAge(b.at, "hr-when") +
    (b.released ? '<span class="hr-released" title="released">' + HR_SVG.lock + "released</span>" : "") +
    "</div>";
}

function hubReposCopyBtn(value, id, label) {
  return '<button type="button" class="hr-copy" data-copy="' + esc(value) + '" data-id="' + esc(id) + '" aria-label="' + esc(label) + '">' +
    HR_SVG.copy + '<span class="hr-copylabel">copy</span></button>';
}

function hubReposRow(r) {
  const full = (r.host && r.host !== "github" ? r.host + "/" : "") + r.owner + "/" + r.repo;
  const http = r.path ? location.origin + r.path : "";
  const branches = r.branches || [];
  const hasMain = !!(r.main && r.main.at);
  const times = [hasMain ? r.main.at : null].concat(branches.map(b => b.at)).filter(t => t && !isNaN(new Date(t)));
  const last = times.sort().pop() || "";
  const status = branches.length ? branches.length + (branches.length === 1 ? " branch" : " branches") : hasMain ? "main only" : "empty";
  const kind = branches.length ? "full" : hasMain ? "main" : "empty";
  let body = "";
  if (hasMain) {
    body += '<div class="hr-main"><span class="hr-mainlabel">main</span><span class="hr-mainsha">' + esc(hubReposSha(r.main.sha)) + "</span>" +
      hubReposAge(r.main.at, "hr-mainage") + "</div>";
  }
  if (branches.length) body += '<div class="hr-branches">' + branches.map(hubReposBranch).join("") + "</div>";
  if (!hasMain && !branches.length) {
    const cmd = "git remote add hub " + (r.url || "<url>") + "\ngit push hub <branch>";
    body += '<div class="hr-emptybox">' + HR_SVG.empty +
      '<div class="hr-emptytext"><b>Nothing pushed yet</b><span>Add the hub as a remote, then push a branch.</span></div>' +
      '<div class="hr-howto"><pre class="hr-cmd">git remote add hub ' + esc(r.url || "<url>") + "\ngit push hub &lt;branch&gt;</pre>" +
      hubReposCopyBtn(cmd, full + "#howto", "copy the git commands for " + full) + "</div></div>";
  }
  const [owner, repo] = [r.owner, r.repo];
  return '<article class="hr-repo ' + kind + '" data-repo="' + esc(full) + '">' +
    '<div class="hr-head"><h3 class="hr-title-line"><span class="hr-owner">' + esc((r.host && r.host !== "github" ? r.host + "/" : "") + owner) +
    '/</span><span class="hr-repoName">' + esc(repo) + "</span></h3>" +
    '<span class="hr-host">' + esc(r.host || "github") + "</span>" +
    '<span class="hr-pill ' + kind + '">' + esc(status) + "</span>" +
    (last ? hubReposAge(last, "hr-last").replace("<time ", "<time ").replace(">", ">last push ") : "") + "</div>" +
    (r.url ? '<div class="hr-url hero"><code class="hr-urlval">' + esc(r.url) + "</code>" + hubReposCopyBtn(r.url, full + "#ssh", "copy the clone URL for " + full) + "</div>" : "") +
    (http ? '<div class="hr-url"><span class="hr-urllabel">http</span><code class="hr-urlval">' + esc(http) + "</code>" + hubReposCopyBtn(http, full + "#http", "copy the http URL for " + full) + "</div>" : "") +
    body + "</article>";
}

function hubReposPaint() {
  const list = document.getElementById("hubrepos-list");
  if (!list) return;
  const count = document.getElementById("hubrepos-count");
  if (count) count.textContent = hubRepos.note || !hubRepos.loaded ? "" : String(hubRepos.repos.length);
  if (hubRepos.note) { list.innerHTML = '<p class="pane-lead hr-note" role="alert">' + esc(hubRepos.note) + "</p>"; return; }
  if (!hubRepos.repos.length) {
    list.innerHTML = '<div class="hr-none">' + HR_SVG.empty + '<p class="pane-lead">No repos on the hub yet. Run <code>atrium git setup</code> to point a room at it.</p></div>';
    return;
  }
  list.innerHTML = hubRepos.repos.map(hubReposRow).join("");
}

async function loadHubRepos() {
  if (hubRepos.inflight) return;
  hubRepos.inflight = true;
  const refresh = document.getElementById("hubrepos-refresh");
  if (refresh) refresh.disabled = true;
  try {
    const res = await fetch("/_hub/git/repos");
    if (!res.ok) throw new Error(String(res.status));
    const body = await res.json();
    hubRepos.repos = (body && body.repos) || [];
    hubRepos.note = "";
    hubRepos.loaded = true;
  } catch (e) {
    hubRepos.note = "The hub's repos are not answering right now. This hub may be too old to list them.";
  } finally {
    hubRepos.inflight = false;
    if (refresh) refresh.disabled = false;
  }
  hubReposPaint();
}

async function hubReposCopy(btn) {
  const label = btn.querySelector(".hr-copylabel");
  let ok = true;
  try { await navigator.clipboard.writeText(btn.dataset.copy); } catch (e) { ok = false; }
  clearTimeout(btn._hrTimer);
  btn.classList.toggle("done", ok);
  btn.classList.toggle("failed", !ok);
  if (label) label.textContent = ok ? "copied" : "could not copy";
  btn._hrTimer = setTimeout(() => {
    btn.classList.remove("done", "failed");
    if (label) label.textContent = "copy";
  }, 1400);
}

document.addEventListener("DOMContentLoaded", () => {
  const list = document.getElementById("hubrepos-list");
  if (list) list.addEventListener("click", e => {
    const b = e.target.closest(".hr-copy");
    if (b) hubReposCopy(b);
  });
  const refresh = document.getElementById("hubrepos-refresh");
  if (refresh) refresh.addEventListener("click", loadHubRepos);
});
