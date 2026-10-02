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

const hubRepos = { repos: [], loaded: false, note: "" };

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

function hubReposBranch(b) {
  const title = hubReposCardTitle(b.room, b.card);
  const who = (b.room ? esc(b.room) : "") + (b.card ? " " + esc(b.card) : "");
  return '<div class="hr-branch">' +
    '<span class="hr-name">' + esc(b.name) + "</span>" +
    '<span class="hr-sha">' + esc(hubReposSha(b.sha)) + "</span>" +
    '<span class="hr-who">' + who + (title ? ' <span class="hr-title">' + esc(title) + "</span>" : "") + "</span>" +
    '<span class="hr-when">' + esc(hubReposWhen(b.at)) + "</span>" +
    (b.released ? '<span class="hr-released">released</span>' : "") +
    "</div>";
}

function hubReposUrlRow(label, value, id) {
  return '<div class="hr-url"><span class="hr-urllabel">' + label + "</span>" +
    '<code class="hr-urlval">' + esc(value) + "</code>" +
    '<button type="button" class="hr-copy" data-copy="' + esc(value) + '" data-id="' + esc(id) + '">copy</button></div>';
}

function hubReposRow(r) {
  const name = (r.host && r.host !== "github" ? r.host + "/" : "") + r.owner + "/" + r.repo;
  const main = r.main && r.main.at
    ? "main " + hubReposSha(r.main.sha) + " " + hubReposWhen(r.main.at)
    : "empty";
  const http = r.path ? location.origin + r.path : "";
  const branches = r.branches || [];
  return '<div class="hr-repo" data-repo="' + esc(name) + '">' +
    '<div class="hr-head"><span class="hr-repoName">' + esc(name) + '</span><span class="hr-main">' + esc(main) + "</span></div>" +
    (r.url ? hubReposUrlRow("clone", r.url, name + "#ssh") : "") +
    (http ? hubReposUrlRow("http", http, name + "#http") : "") +
    branches.map(hubReposBranch).join("") +
    "</div>";
}

function hubReposPaint() {
  const list = document.getElementById("hubrepos-list");
  if (!list) return;
  if (hubRepos.note) { list.innerHTML = '<p class="pane-lead">' + esc(hubRepos.note) + "</p>"; return; }
  if (!hubRepos.repos.length) {
    list.innerHTML = '<p class="pane-lead">No repos on the hub yet. Run <code>atrium git setup</code> to point a room at it.</p>';
    return;
  }
  list.innerHTML = hubRepos.repos.map(hubReposRow).join("");
}

async function loadHubRepos() {
  const fetcher = typeof plainFetch === "function" ? plainFetch : window.fetch.bind(window);
  try {
    const res = await fetcher("/_hub/git/repos");
    if (!res.ok) throw new Error(String(res.status));
    const body = await res.json();
    hubRepos.repos = (body && body.repos) || [];
    hubRepos.note = "";
    hubRepos.loaded = true;
  } catch (e) {
    hubRepos.note = "The hub's repos are not answering right now. This hub may be too old to list them.";
  }
  hubReposPaint();
}

document.addEventListener("DOMContentLoaded", () => {
  const list = document.getElementById("hubrepos-list");
  if (list) list.addEventListener("click", e => {
    const b = e.target.closest(".hr-copy");
    if (b) copyText(b, b.dataset.copy);
  });
  const refresh = document.getElementById("hubrepos-refresh");
  if (refresh) refresh.addEventListener("click", loadHubRepos);
});
