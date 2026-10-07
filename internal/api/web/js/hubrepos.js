// The hub's repos: what rooms have pushed to the hub, and the clone URL of each.
//
// docs/fabric/hub-forge-design.md sections 2, 3.2 and 7 (u-new-hub-repos-list). A list, not a code browser: each repo
// shows its `main`, the branches rooms pushed (room, card, when) and a clone URL to copy.
//
// THREE VIEWS OF ONE SET OF DATA, chosen with the switcher in the tab head and remembered per browser (a fourth, `requests`,
// is not the repos at all but the change requests between rooms, drawn by js/changereq.js in the same area):
//   shelf   (the default)  a grid of repo cards: identicon tile, branch count, main sha, a heat strip of pushes
//   ledger                 a repo list and the selected repo as a hero, with its branches on a vertical rail
//   feed                   every push of every repo, newest first, with the repos as a rail of filters
// They share every helper below (identicon, heat strip, ages, who pushed, copy, the card-title lookup) and ONE empty
// state, `hubReposHero`: a hub nothing has been pushed to reads as an invitation, in all three.
//
// HUB-ONLY, like the audit tab: it reveals itself once the hub probe finds a hub (`paintHubReposTab`, called from
// rooms.js). READ WHEN YOU GO THERE and again on the refresh button. No stream event and no timer.
//
// THE CARD TITLE IS NOT IN THE ANSWER. It is looked up from the board's own card list by room and card id, and a card
// that is gone shows just its room and id.
//
// THE VIEW IS A PER-BROWSER PREFERENCE, `atrium.reposView`, kept the way the growler switch is: a localStorage key,
// every read and write inside try/catch, and a `storage` listener so another open window follows. An unknown value is
// Shelf. Which repo the ledger has open, the feed's filter, an ssh/http choice and an expanded card are this window's
// and not remembered.

const hubRepos = { repos: [], loaded: false, note: "", inflight: false, mem: "", sel: null, filter: "", mode: {}, open: {} };

const HR_VIEW_KEY = "atrium.reposView";
const HR_VIEWS = ["shelf", "ledger", "feed", "requests"];

function hubReposView() {
  let v = "";
  try { v = localStorage.getItem(HR_VIEW_KEY) || ""; } catch (e) {}
  if (!HR_VIEWS.includes(v)) v = HR_VIEWS.includes(hubRepos.mem) ? hubRepos.mem : "shelf";
  return v;
}

function setHubReposView(v) {
  if (!HR_VIEWS.includes(v)) v = "shelf";
  hubRepos.mem = v;
  try { localStorage.setItem(HR_VIEW_KEY, v); } catch (e) {}
  // Change requests are read when you go there (js/changereq.js), and the paint below shows "reading" until they arrive.
  if (v === "requests" && typeof cr !== "undefined" && !cr.loaded && !cr.inflight) crLoad();
  hubReposPaint();
}

// THE SORT IS A PER-BROWSER PREFERENCE TOO, `atrium.reposSort`. name is the hub's own order, a to z, pushed is the newest push of main or any branch
// first (a repo nothing was pushed to last), added is the newest repo the hub took in first. An unknown value is name.
const HR_SORT_KEY = "atrium.reposSort";
const HR_SORTS = ["name", "pushed", "added"];

function hubReposSort() {
  let v = "";
  try { v = localStorage.getItem(HR_SORT_KEY) || ""; } catch (e) {}
  return HR_SORTS.includes(v) ? v : "name";
}

function setHubReposSort(v) {
  if (!HR_SORTS.includes(v)) v = "name";
  try { localStorage.setItem(HR_SORT_KEY, v); } catch (e) {}
  hubReposPaint();
}

// A copy in the chosen order. Ties and missing times fall back to the name, so the order is the same on every poll.
function hubReposSorted(repos, sort) {
  const id = r => (r.host || "") + "/" + r.owner + "/" + r.repo;
  const name = (a, b) => id(a).localeCompare(id(b));
  // The hub already answers by name, so that choice leaves its order alone.
  if (sort === "name") return repos.slice();
  const at = iso => { const t = iso ? Date.parse(iso) : NaN; return isNaN(t) ? -Infinity : t; };
  const pushed = r => { const l = hubReposStats(r).last; return l == null ? -Infinity : -l; };
  const key = sort === "pushed" ? pushed : sort === "added" ? r => at(r.created) : null;
  return repos.slice().sort((a, b) => {
    const x = key(a), y = key(b);
    return x === y ? name(a, b) : y > x ? 1 : -1;
  });
}

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

// ---- the small shared pieces ---------------------------------------------------------------------------------------

const hubReposSha = s => String(s || "").slice(0, 7);
const hubReposFull = r => (r.host && r.host !== "github" ? r.host + "/" : "") + r.owner + "/" + r.repo;

function hubReposHash(s) {
  let h = 2166136261 >>> 0;
  for (const ch of String(s)) h = Math.imul(h ^ ch.charCodeAt(0), 16777619) >>> 0;
  return h;
}
// One of five accents, from the name, so a repo and a room keep their colour wherever they appear.
const hubReposAccent = s => "hr-a" + (hubReposHash(s) % 5);

function hubReposAgeMs(iso) {
  const t = iso ? new Date(iso).getTime() : NaN;
  return isNaN(t) ? null : Math.max(0, Date.now() - t);
}
function hubReposShort(ms) {
  const s = ms / 1000;
  if (s < 45) return "just now";
  if (s < 3600) return Math.round(s / 60) + "m ago";
  if (s < 86400) return Math.round(s / 3600) + "h ago";
  if (s < 86400 * 14) return Math.round(s / 86400) + "d ago";
  return Math.round(s / 86400 / 7) + "w ago";
}
function hubReposWhen(iso) {
  const ms = hubReposAgeMs(iso);
  return ms == null ? "" : hubReposShort(ms);
}
function hubReposExact(iso) {
  const d = iso ? new Date(iso) : null;
  return d && !isNaN(d) ? d.toLocaleString() : "";
}
function hubReposAge(iso, cls) {
  const w = hubReposWhen(iso);
  return w ? '<time class="' + cls + '" datetime="' + esc(iso) + '" title="' + esc(hubReposExact(iso)) + '">' + esc(w) + "</time>" : "";
}

const HR_STATES = { active: "Active", quiet: "Quiet", stale: "Stale", empty: "Empty" };

function hubReposStats(r) {
  const branches = r.branches || [];
  const hasMain = !!(r.main && r.main.at);
  const ages = branches.map(b => hubReposAgeMs(b.at)).concat(hasMain ? [hubReposAgeMs(r.main.at)] : []).filter(x => x != null);
  const last = ages.length ? Math.min(...ages) : null;
  const state = last == null ? "empty" : last < 864e5 ? "active" : last < 7 * 864e5 ? "quiet" : "stale";
  return { branches, hasMain, last, state, rooms: [...new Set(branches.map(b => b.room).filter(Boolean))] };
}
const hubReposPushed = r => { const s = hubReposStats(r); return s.hasMain || s.branches.length > 0; };

function hubReposUrl(r, mode) {
  return mode === "http" && r.path ? location.origin + r.path : r.url || "";
}
const hubReposMode = r => (hubRepos.mode[hubReposFull(r)] === "http" && r.path ? "http" : "ssh");

const HR_SVG = {
  branch: '<svg class="hr-glyph" viewBox="0 0 16 16" width="14" height="14" aria-hidden="true" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"><circle cx="4.5" cy="3.5" r="1.7"/><circle cx="4.5" cy="12.5" r="1.7"/><circle cx="11.5" cy="5.5" r="1.7"/><path d="M4.5 5.2v5.6M11.5 7.2c0 2.6-3.4 2.2-6.2 4"/></svg>',
  lock: '<svg viewBox="0 0 16 16" width="12" height="12" aria-hidden="true" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><rect x="3.5" y="7" width="9" height="6.5" rx="1.5"/><path d="M5.5 7V5a2.5 2.5 0 0 1 5 0v2"/></svg>',
  copy: '<svg viewBox="0 0 16 16" width="14" height="14" aria-hidden="true" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linejoin="round"><rect x="5.5" y="5.5" width="8" height="8" rx="1.5"/><path d="M10.5 5.5v-2a1 1 0 0 0-1-1h-6a1 1 0 0 0-1 1v6a1 1 0 0 0 1 1h2"/></svg>',
  chev: '<svg viewBox="0 0 16 16" width="12" height="12" aria-hidden="true" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M6 3l5 5-5 5"/></svg>',
  up: '<svg viewBox="0 0 16 16" width="15" height="15" aria-hidden="true" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M8 13V3m0 0L3.5 7.5M8 3l4.5 4.5"/></svg>',
  all: '<svg viewBox="0 0 16 16" width="16" height="16" aria-hidden="true" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round"><path d="M2.5 4h11M2.5 8h11M2.5 12h7"/></svg>',
  art: '<svg class="hr-art" viewBox="0 0 520 240" aria-hidden="true" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">' +
    '<defs><linearGradient id="hr-g1" x1="0" y1="0" x2="1" y2="1"><stop offset="0" stop-color="var(--teal)" stop-opacity=".28"/><stop offset="1" stop-color="var(--blue)" stop-opacity=".10"/></linearGradient></defs>' +
    '<ellipse cx="260" cy="214" rx="220" ry="12" fill="var(--stroke)" opacity=".18" stroke="none"/>' +
    '<rect x="34" y="84" width="140" height="96" rx="12" fill="var(--card-0)"/><path d="M34 108h140" opacity=".5"/>' +
    '<circle cx="50" cy="96" r="3" fill="var(--danger)" stroke="none"/><circle cx="62" cy="96" r="3" fill="var(--warn)" stroke="none"/><circle cx="74" cy="96" r="3" fill="var(--teal)" stroke="none"/>' +
    '<path d="M52 128h38M52 142h70M52 156h24" stroke="var(--teal)" opacity=".9"/><path d="M96 156h6" stroke="var(--blue)"/><path d="M62 180v14h50v-14" opacity=".5"/>' +
    '<path d="M184 128C230 70 290 70 336 118" stroke="var(--teal)" stroke-dasharray="2 9" stroke-width="3"/><path d="M327 104l12 16-19 2" stroke="var(--teal)" stroke-width="3"/>' +
    '<circle cx="226" cy="84" r="9" fill="var(--bg-1)" stroke="var(--teal)"/><circle cx="266" cy="78" r="6" fill="var(--teal)" stroke="none" opacity=".85"/><circle cx="302" cy="92" r="4" fill="var(--teal)" stroke="none" opacity=".5"/>' +
    '<rect x="346" y="40" width="140" height="150" rx="14" fill="url(#hr-g1)"/><rect x="346" y="40" width="140" height="150" rx="14"/>' +
    '<rect x="362" y="58" width="108" height="28" rx="7" fill="var(--bg-1)" opacity=".7"/><rect x="362" y="96" width="108" height="28" rx="7" fill="var(--bg-1)" opacity=".7" stroke-dasharray="3 6"/><rect x="362" y="134" width="108" height="28" rx="7" fill="var(--bg-1)" opacity=".7" stroke-dasharray="3 6"/>' +
    '<circle cx="378" cy="72" r="4" fill="var(--teal)" stroke="none"/><path d="M390 72h56" opacity=".5"/><path d="M378 110h40M378 148h30" opacity=".3"/><path d="M396 190v18h40v-18" opacity=".5"/>' +
    '<path d="M410 22l3 7 7 3-7 3-3 7-3-7-7-3 7-3z" fill="var(--warn)" stroke="none" opacity=".9"/><path d="M118 40l2 5 5 2-5 2-2 5-2-5-5-2 5-2z" fill="var(--blue)" stroke="none" opacity=".8"/></svg>'
};

// A five-by-five mirrored pattern from the repo's name: the same repo is the same tile in every view.
function hubReposIdenticon(seed) {
  const h = hubReposHash(seed), h2 = hubReposHash(seed + "x");
  let cells = "";
  for (let y = 0; y < 5; y++) for (let x = 0; x < 3; x++) {
    if (((h >> (y * 3 + x)) & 1) || ((h2 >> (y * 3 + x)) & 3) === 0) {
      cells += '<rect x="' + x + '" y="' + y + '" width="1" height="1" rx=".18"/>';
      if (x < 2) cells += '<rect x="' + (4 - x) + '" y="' + y + '" width="1" height="1" rx=".18"/>';
    }
  }
  return '<svg viewBox="-1 -1 7 7" aria-hidden="true" fill="currentColor">' + cells + "</svg>";
}
const hubReposTile = (r, px) => '<span class="hr-tile ' + hubReposAccent(hubReposFull(r)) + '" style="--s:' + px + 'px">' + hubReposIdenticon(hubReposFull(r)) + "</span>";
const hubReposAvatar = (room, px) => '<span class="hr-av ' + hubReposAccent("room" + room) + '" style="--s:' + px + 'px" title="' + esc(room) + '">' + esc(String(room || "?").slice(0, 2)) + "</span>";

// Pushes per day, oldest on the left, as bars: a repo with a dozen branches this week looks busy and a stale pile looks dead.
function hubReposSpark(isos, days) {
  const bins = Array(days).fill(0);
  isos.forEach(iso => { const ms = hubReposAgeMs(iso); const d = ms == null ? days : Math.floor(ms / 864e5); if (d < days) bins[days - 1 - d]++; });
  const mx = Math.max(1, ...bins);
  return '<span class="hr-spark" role="img" aria-label="pushes in the last ' + days + ' days">' +
    bins.map(n => '<i class="' + (n ? "on" : "") + '" style="height:' + (n ? Math.round(18 + 82 * n / mx) : 10) + '%"></i>').join("") + "</span>";
}
const hubReposPushTimes = r => (r.branches || []).map(b => b.at).concat(r.main && r.main.at ? [r.main.at] : []);

const hubReposBadge = state => '<span class="hr-state st-' + state + '"><i></i>' + HR_STATES[state] + "</span>";
const hubReposHost = h => '<span class="hr-host ' + (h === "github" || !h ? "gh" : "other") + '">' + esc(h || "github") + "</span>";

function hubReposCopyBtn(value, id, label, text) {
  return '<button type="button" class="hr-copy" data-copy="' + esc(value) + '" data-id="' + esc(id) + '" aria-label="' + esc(label) + '">' +
    HR_SVG.copy + '<span class="hr-copylabel">' + esc(text || "copy") + "</span></button>";
}

// ssh | http, when the hub serves http for this repo. One choice per repo, shared by every place its URL shows.
function hubReposSeg(r) {
  if (!r.path) return "";
  const mode = hubReposMode(r), full = hubReposFull(r);
  const b = m => '<button type="button" data-act="mode" data-repo="' + esc(full) + '" data-mode="' + m + '" aria-pressed="' + (mode === m) + '">' + m + "</button>";
  return '<span class="hr-seg" role="group" aria-label="clone over">' + b("ssh") + b("http") + "</span>";
}

// THE CLONE URL: one pill, one copy button, inside its own row.
function hubReposCloneBar(r) {
  const full = hubReposFull(r), mode = hubReposMode(r), url = hubReposUrl(r, mode);
  if (!url) return "";
  return '<div class="hr-clone">' + hubReposSeg(r) + '<code class="hr-urlval" title="' + esc(url) + '">' + esc(url) + "</code>" +
    hubReposCopyBtn(url, full + "#" + mode, "copy the " + mode + " clone URL for " + full) + "</div>";
}

// Who pushed a branch, and what that card is called if the board still knows it.
function hubReposWho(b) {
  return { room: b.room || "", card: b.card || "", title: hubReposCardTitle(b.room, b.card) };
}
const hubReposWhoLine = b => { const w = hubReposWho(b); return esc((w.room + (w.card ? " " + w.card : "")).trim()) + (w.title ? " · " + esc(w.title) : ""); };

// By the push time itself, not by an age measured twice, so two pushes with the same timestamp keep their order.
const hubReposTime = iso => { const t = iso ? new Date(iso).getTime() : NaN; return isNaN(t) ? 0 : t; };
const hubReposByNewest = branches => branches.slice().sort((a, b) => hubReposTime(b.at) - hubReposTime(a.at));

// ---- the one empty state -------------------------------------------------------------------------------------------

function hubReposSteps(r) {
  const url = hubReposUrl(r, hubReposMode(r));
  return '<ol class="hr-steps">' +
    '<li><b class="hr-n">1</b><div><h4>Add the hub as a remote</h4><code>git remote add hub ' + esc(url || "<url>") + "</code></div></li>" +
    '<li><b class="hr-n">2</b><div><h4>Push a branch</h4><code>git push hub &lt;branch&gt;</code></div></li>' +
    '<li><b class="hr-n">3</b><div><h4>Watch it land here</h4><p>Its room, card and age appear the moment it arrives.</p></div></li></ol>';
}

// Nothing pushed yet. `r` is the repo to invite a push to, or null when the hub has no repos at all.
function hubReposHero(r) {
  if (!r) {
    return '<section class="hr-hero"><div class="hr-halo">' + HR_SVG.art + "</div>" +
      "<h2>Point a room at your <em>hub</em>.</h2>" +
      '<p class="hr-lead">No repos on the hub yet. Run <code>atrium git setup</code> in a room and its repo appears here the moment it is pushed.</p>' +
      '<div class="hr-herorow"><button type="button" class="hr-copy hr-go" data-copy="atrium git setup" data-id="setup" aria-label="copy atrium git setup">' +
      HR_SVG.copy + '<span class="hr-copylabel">Copy atrium git setup</span></button></div></section>';
  }
  const full = hubReposFull(r), url = hubReposUrl(r, hubReposMode(r));
  const cmd = "git remote add hub " + url + "\ngit push hub <branch>";
  return '<section class="hr-hero ' + hubReposAccent(full) + '"><div class="hr-halo">' + HR_SVG.art + "</div>" +
    '<div class="hr-repoid">' + hubReposTile(r, 40) + "<span>" + esc(r.owner) + "/<b>" + esc(r.repo) + "</b></span></div>" +
    "<h2>Your hub is ready for <em>its first push.</em></h2>" +
    '<p class="hr-lead">Nothing pushed yet. Add it as a remote, push a branch, and it shows up here with the room and card that sent it.</p>' +
    '<div class="hr-herorow"><button type="button" class="hr-copy hr-go" data-copy="' + esc(cmd) + '" data-id="' + esc(full) + '#howto" aria-label="copy the git commands for ' + esc(full) + '">' +
    HR_SVG.copy + '<span class="hr-copylabel">Copy the clone command</span></button>' + hubReposSeg(r) + "</div>" +
    hubReposSteps(r) + "</section>";
}

// ---- SHELF ---------------------------------------------------------------------------------------------------------

function hubReposBranchRow(b) {
  return '<div class="hr-branch' + (b.released ? " released" : "") + '">' + hubReposAvatar(b.room, 28) +
    '<span class="hr-bmain"><span class="hr-name" title="' + esc(b.name) + '">' + esc(b.name) + "</span>" +
    '<span class="hr-who">' + hubReposWhoLine(b) + "</span></span>" +
    (b.released ? '<span class="hr-released" title="released">' + HR_SVG.lock + "released</span>" : "") +
    hubReposAge(b.at, "hr-when") + "</div>";
}

function hubReposCard(r) {
  const full = hubReposFull(r), st = hubReposStats(r), n = st.branches.length;
  const head = '<div class="hr-cardhead">' + hubReposTile(r, 68) + '<div class="hr-id"><div class="hr-owner">' + esc((r.host && r.host !== "github" ? r.host + "/" : "") + r.owner) + '</div><h3 class="hr-repoName">' + esc(r.repo) + "</h3>" +
    '<div class="hr-badges">' + hubReposBadge(st.state) + hubReposHost(r.host) + "</div></div></div>";
  if (st.state === "empty") {
    return '<article class="hr-repo is-empty ' + hubReposAccent(full) + '" data-repo="' + esc(full) + '">' + head +
      '<div class="hr-invite"><b>Nothing pushed yet</b>Add the hub as a remote and push a branch. It lands here.</div>' +
      '<div class="hr-howto"><pre class="hr-cmd">git remote add hub ' + esc(r.url || "<url>") + "\ngit push hub &lt;branch&gt;</pre>" +
      hubReposCopyBtn("git remote add hub " + (r.url || "<url>") + "\ngit push hub <branch>", full + "#howto", "copy the git commands for " + full) + "</div>" +
      hubReposCloneBar(r) + "</article>";
  }
  const sorted = hubReposByNewest(st.branches), open = !!hubRepos.open[full];
  const shown = open ? sorted : sorted.slice(0, 3);
  return '<article class="hr-repo ' + (n ? "full" : "main") + " " + hubReposAccent(full) + '" data-repo="' + esc(full) + '">' + head +
    '<div class="hr-nums"><div class="hr-count-big"><b>' + n + "</b><span>" + (n === 1 ? "branch" : "branches") + "</span></div>" +
    (st.hasMain ? '<div class="hr-main"><small>main</small><code class="hr-mainsha">' + esc(hubReposSha(r.main.sha)) + "</code>" + hubReposAge(r.main.at, "hr-mainage") + "</div>" : "") + "</div>" +
    '<div class="hr-heat">' + hubReposSpark(hubReposPushTimes(r), 28) + "<div><span>4 weeks ago</span><span>today</span></div></div>" +
    (shown.length ? '<div class="hr-branches">' + shown.map(hubReposBranchRow).join("") + "</div>" : "") +
    (sorted.length > 3 ? '<button type="button" class="hr-more" data-act="more" data-repo="' + esc(full) + '" aria-expanded="' + open + '">' +
      (open ? "show fewer" : (sorted.length - 3) + " more " + (sorted.length - 3 === 1 ? "branch" : "branches")) + HR_SVG.chev + "</button>" : "") +
    hubReposCloneBar(r) + "</article>";
}

function hubReposHeatAll(repos) {
  return hubReposSpark(repos.flatMap(r => (r.branches || []).map(b => b.at)), 28);
}

function hubReposBig(n, label, cls) { return '<div class="hr-big ' + cls + '"><b>' + n + "</b><span>" + label + "</span></div>"; }

function hubReposSummary(repos, title, lead) {
  const all = repos.flatMap(r => r.branches || []);
  const today = all.filter(b => (hubReposAgeMs(b.at) ?? 1e15) < 864e5).length;
  const rooms = new Set(all.map(b => b.room).filter(Boolean)).size;
  return '<section class="hr-summary"><div class="hr-sumtitle"><h2>' + title + "</h2><p>" + lead + "</p></div>" +
    hubReposBig(repos.length, repos.length === 1 ? "repo" : "repos", "t") + hubReposBig(all.length, "branches", "p") +
    hubReposBig(today, "pushed today", "w") + hubReposBig(rooms, rooms === 1 ? "room" : "rooms", "") + hubReposHeatAll(repos) + "</section>";
}

function hubReposShelf(repos) {
  if (repos.length === 1 && !hubReposPushed(repos[0])) return hubReposHero(repos[0]);
  return hubReposSummary(repos, "Repos on the hub", "Everything your rooms pushed.") + '<section class="hr-shelf">' + repos.map(hubReposCard).join("") + "</section>";
}

// ---- timeline pieces shared by ledger and feed ---------------------------------------------------------------------

function hubReposEvents(repos) {
  const ev = [];
  repos.forEach(r => {
    (r.branches || []).forEach(b => ev.push({ r, b, ms: hubReposAgeMs(b.at) ?? 1e15, t: hubReposTime(b.at) }));
    if (r.main && r.main.at) ev.push({ r, b: null, ms: hubReposAgeMs(r.main.at) ?? 1e15, t: hubReposTime(r.main.at) });
  });
  return ev.sort((a, b) => b.t - a.t);
}
const hubReposDay = ms => ms < 864e5 ? "Today" : ms < 2 * 864e5 ? "Yesterday" : ms < 7 * 864e5 ? "This week" : "Earlier";

// Items under day headings. `item` draws one event.
function hubReposGrouped(events, item) {
  let last = "", out = "";
  events.forEach(e => {
    const d = hubReposDay(e.ms);
    if (d !== last) { out += '<div class="hr-day">' + d + " <b>" + events.filter(x => hubReposDay(x.ms) === d).length + "</b></div>"; last = d; }
    out += item(e);
  });
  return out;
}

function hubReposRoomTally(repos) {
  const by = {};
  repos.forEach(r => (r.branches || []).forEach(b => { if (b.room) by[b.room] = (by[b.room] || 0) + 1; }));
  return by;
}

// ---- LEDGER --------------------------------------------------------------------------------------------------------

function hubReposLedgerItem(r, sel) {
  const full = hubReposFull(r), st = hubReposStats(r);
  return '<button type="button" class="hr-li ' + hubReposAccent(full) + (sel ? " sel" : "") + '" data-act="sel" data-repo="' + esc(full) + '"' + (sel ? ' aria-current="true"' : "") + ">" +
    hubReposTile(r, 46) + '<span class="hr-lit"><span class="hr-lio">' + esc(r.owner) + '</span><span class="hr-lin">' + esc(r.repo) + "</span>" +
    '<span class="hr-lim">' + (st.last == null ? "nothing pushed" : "main " + esc(hubReposSha(r.main && r.main.sha)) + " · " + esc(hubReposShort(st.last))) + "</span></span>" +
    '<span class="hr-lic">' + (st.last == null ? "–" : st.branches.length) + '<small class="st-' + st.state + '"><i></i>' + st.state + "</small></span></button>";
}

function hubReposRailEvent(e) {
  const b = e.b, fresh = e.ms < 3600e3;
  if (!b) {
    return '<div class="hr-ev main"><span class="hr-av hr-a0" style="--s:38px">' + HR_SVG.up + '</span><div class="hr-evbody"><span class="hr-name">main</span><div class="hr-evsub">moved to ' + esc(hubReposSha(e.r.main.sha)) + "</div></div>" +
      '<div class="hr-evwhen">' + hubReposAge(e.r.main.at, "hr-when") + "</div></div>";
  }
  return '<div class="hr-ev' + (b.released ? " released" : "") + (fresh ? " fresh" : "") + " " + hubReposAccent("room" + b.room) + '">' + hubReposAvatar(b.room, 38) +
    '<div class="hr-evbody"><span class="hr-name" title="' + esc(b.name) + '">' + esc(b.name) + '</span><div class="hr-evsub"><span class="hr-chip">' +
    esc((b.room + (b.card ? " " + b.card : "")).trim()) + "</span>" + (hubReposWho(b).title ? '<span class="hr-title">' + esc(hubReposWho(b).title) + "</span>" : "") + "</div></div>" +
    '<div class="hr-evwhen">' + hubReposAge(b.at, "hr-when") + (b.released ? '<span class="hr-released">' + HR_SVG.lock + "released</span>" : '<code class="hr-sha">' + esc(hubReposSha(b.sha)) + "</code>") + "</div>" +
    (typeof crBranchBit === "function" ? crBranchBit(e.r, b) : "") + "</div>";
}

function hubReposTerminal(r) {
  const full = hubReposFull(r), mode = hubReposMode(r), url = hubReposUrl(r, mode);
  if (!url) return "";
  return '<div class="hr-term"><div class="hr-termbar"><span class="hr-dots"><i></i><i></i><i></i></span>' + hubReposSeg(r) +
    hubReposCopyBtn("git clone " + url, full + "#" + mode, "copy the " + mode + " clone command for " + full, "Copy").replace('class="hr-copy"', 'class="hr-copy hr-go"') + "</div>" +
    '<pre class="hr-termcmd"><span class="hr-prompt">$ </span>git clone ' + esc(url) + "</pre></div>";
}

function hubReposLedgerDetail(r) {
  const full = hubReposFull(r), st = hubReposStats(r);
  if (st.state === "empty") return hubReposHero(r);
  const sorted = hubReposByNewest(st.branches), by = hubReposRoomTally([r]), mx = Math.max(1, ...Object.values(by));
  const rail = hubReposGrouped(hubReposEvents([Object.assign({}, r, { main: { at: null } })]), hubReposRailEvent);
  return '<div class="hr-dcrumb">' + esc((r.host && r.host !== "github" ? r.host + "/" : "") + r.owner) + ' <span>/</span></div>' +
    '<div class="hr-dh">' + hubReposTile(r, 84) + '<h2 class="hr-dname">' + esc(r.repo) + "</h2></div>" +
    '<div class="hr-facts"><div class="hr-fact ac"><b>' + sorted.length + "</b><span>" + (sorted.length === 1 ? "branch" : "branches") + "</span></div>" +
    (st.hasMain ? '<div class="hr-fact"><b class="mono">' + esc(hubReposSha(r.main.sha)) + "</b><span>main · " + esc(hubReposShort(hubReposAgeMs(r.main.at) ?? 0)) + "</span></div>" : "") +
    '<div class="hr-fact"><b>' + st.rooms.length + "</b><span>" + (st.rooms.length === 1 ? "room pushing" : "rooms pushing") + '</span></div><div class="hr-badges">' + hubReposBadge(st.state) + hubReposHost(r.host) + "</div></div>" +
    hubReposTerminal(r) +
    '<div class="hr-cols"><section><h3 class="hr-h">Branch timeline</h3>' + (sorted.length ? '<div class="hr-rail">' + rail + "</div>" : '<p class="hr-quiet">Only main so far. Branches appear here as rooms push them.</p>') + "</section>" +
    '<aside class="hr-side"><div class="hr-panel"><h3 class="hr-h">Pushes, last 4 weeks</h3>' + hubReposSpark(hubReposPushTimes(r), 28) + "</div>" +
    (Object.keys(by).length ? '<div class="hr-panel"><h3 class="hr-h">Who pushes</h3>' + Object.entries(by).map(([rm, n]) =>
      '<div class="hr-whorow ' + hubReposAccent("room" + rm) + '">' + hubReposAvatar(rm, 34) + "<b>" + esc(rm) + '</b><span class="hr-bar"><i style="width:' + Math.round(100 * n / mx) + '%"></i></span><span class="hr-ct">' + n + "</span></div>").join("") + "</div>" : "") +
    "</aside></div>";
}

function hubReposLedger(repos) {
  if (hubRepos.sel === null) hubRepos.sel = hubReposFull(repos[0]);
  const sel = hubRepos.sel === "" ? null : repos.find(r => hubReposFull(r) === hubRepos.sel) || repos[0];
  const detail = sel ? hubReposLedgerDetail(sel) :
    hubReposSummary(repos, "Live on the hub", "Every push from every room, newest first.") + '<div class="hr-feedcol">' + hubReposGrouped(hubReposEvents(repos), hubReposFeedItem) + "</div>";
  return '<div class="hr-ledger"><nav class="hr-list" aria-label="repos"><h3 class="hr-h">Repos <b>' + repos.length + "</b></h3>" +
    '<button type="button" class="hr-li hr-a0 all' + (sel ? "" : " sel") + '" data-act="sel" data-repo=""' + (sel ? "" : ' aria-current="true"') + '><span class="hr-tile hr-a0" style="--s:46px">' + HR_SVG.all + '</span><span class="hr-lit"><span class="hr-lin">All repos</span><span class="hr-lim">every push, newest first</span></span></button>' +
    repos.map(r => hubReposLedgerItem(r, sel === r)).join("") + '</nav><main class="hr-detail ' + (sel ? hubReposAccent(hubReposFull(sel)) : "hr-a0") + '">' + detail + "</main></div>";
}

// ---- FEED ----------------------------------------------------------------------------------------------------------

function hubReposRepoPill(r) {
  return '<span class="hr-pill ' + hubReposAccent(hubReposFull(r)) + '">' + hubReposTile(r, 22) + esc(hubReposFull(r)) + "</span>";
}

function hubReposFeedItem(e) {
  const b = e.b, r = e.r, old = e.ms > 6 * 864e5, hot = e.ms < 3600e3;
  if (!b) {
    return '<div class="hr-it main ' + hubReposAccent(hubReposFull(r)) + (hot ? " hot" : "") + '"><span class="hr-av" style="--s:46px">' + HR_SVG.up + '</span><div><div class="hr-l1"><b>main</b> moved in ' + hubReposRepoPill(r) + "</div>" +
      '<span class="hr-bn">' + esc(hubReposSha(r.main.sha)) + '</span></div><div class="hr-rt' + (old ? " old" : "") + '">' + hubReposAge(r.main.at, "hr-when") + "</div></div>";
  }
  const w = hubReposWho(b);
  return '<div class="hr-it ' + hubReposAccent("room" + b.room) + (b.released ? " released" : "") + (hot ? " hot" : "") + '">' + hubReposAvatar(b.room, 46) +
    '<div class="hr-itmain"><div class="hr-l1"><b>' + esc(b.room) + "</b> pushed to " + hubReposRepoPill(r) + '</div><span class="hr-bn" title="' + esc(b.name) + '">' + esc(b.name) + "</span>" +
    '<div class="hr-title">' + esc(w.card) + (w.title ? " · " + esc(w.title) : "") + '</div></div><div class="hr-rt' + (old ? " old" : "") + '">' + hubReposAge(b.at, "hr-when") +
    (b.released ? '<span class="hr-released">' + HR_SVG.lock + "released</span>" : '<code class="hr-sha">' + esc(hubReposSha(b.sha)) + "</code>") + "</div></div>";
}

function hubReposChip(r, on) {
  const full = hubReposFull(r), st = hubReposStats(r);
  return '<button type="button" class="hr-chipbtn ' + hubReposAccent(full) + (on ? " sel" : "") + '" data-act="filter" data-repo="' + esc(full) + '" aria-pressed="' + on + '">' + hubReposTile(r, 40) +
    '<span class="hr-ct2"><span class="hr-lio">' + esc(r.owner) + '</span><span class="hr-lin">' + esc(r.repo) + '</span></span><span class="hr-k"><i class="' + st.state + '"></i>' + st.branches.length + "</span></button>";
}

function hubReposFeed(repos) {
  const pushedAny = repos.some(hubReposPushed);
  const filterRepo = repos.find(r => hubReposFull(r) === hubRepos.filter) || null;
  const rail = '<div class="hr-rail2" role="group" aria-label="filter by repo"><button type="button" class="hr-chipbtn hr-a0 all' + (filterRepo ? "" : " sel") + '" data-act="filter" data-repo="" aria-pressed="' + !filterRepo + '"><span class="hr-lin">All repos</span><span class="hr-k">' + repos.length + "</span></button>" +
    repos.map(r => hubReposChip(r, r === filterRepo)).join("") + "</div>";
  if (!pushedAny) return rail + hubReposHero(repos[0]);
  const shown = filterRepo ? [filterRepo] : repos;
  const events = hubReposEvents(shown);
  let top;
  if (filterRepo) {
    const st = hubReposStats(filterRepo);
    top = '<section class="hr-rhead ' + hubReposAccent(hubReposFull(filterRepo)) + '">' + hubReposTile(filterRepo, 64) + '<div class="hr-id"><div class="hr-owner">' + esc(filterRepo.owner) + '</div><h2 class="hr-repoName">' + esc(filterRepo.repo) + "</h2>" +
      '<div class="hr-badges">' + hubReposBadge(st.state) + hubReposHost(filterRepo.host) + "</div></div>" + hubReposCloneBar(filterRepo) + "</section>";
  } else {
    top = hubReposSummary(repos, "Live on the hub", "Every push from every room, newest first.");
  }
  const by = hubReposRoomTally(shown);
  const clones = '<div class="hr-panel hr-clones"><h3 class="hr-h">Clone</h3>' + repos.map(r => {
    const full = hubReposFull(r), mode = hubReposMode(r), url = hubReposUrl(r, mode);
    return '<div class="hr-cr ' + hubReposAccent(full) + '">' + hubReposTile(r, 36) + '<div class="hr-crt"><span class="hr-crn">' + esc(full) + '</span><code class="hr-cru" title="' + esc(url) + '">' + esc(url) + "</code></div>" +
      hubReposCopyBtn(url, full + "#" + mode, "copy the " + mode + " clone URL for " + full) + "</div>";
  }).join("") + "</div>";
  const rooms = Object.keys(by).length ? '<div class="hr-panel"><h3 class="hr-h">Rooms</h3>' + Object.entries(by).map(([rm, n]) =>
    '<div class="hr-whorow ' + hubReposAccent("room" + rm) + '">' + hubReposAvatar(rm, 34) + "<b>" + esc(rm) + '</b><span class="hr-ct">' + n + "</span></div>").join("") + "</div>" : "";
  return rail + top + '<div class="hr-feedmain"><section class="hr-feedcol">' + hubReposGrouped(events, hubReposFeedItem) + '</section><aside class="hr-side">' + (filterRepo ? "" : clones) + rooms + "</aside></div>";
}

// ---- painting ------------------------------------------------------------------------------------------------------

function hubReposSyncSwitch() {
  const v = hubReposView();
  document.querySelectorAll("#hubrepos-views [data-hrview]").forEach(b => {
    const on = b.dataset.hrview === v;
    b.setAttribute("aria-checked", String(on));
    b.tabIndex = on ? 0 : -1;
    b.classList.toggle("on", on);
  });
}

function hubReposPaint() {
  const list = document.getElementById("hubrepos-list");
  if (!list) return;
  hubReposSyncSwitch();
  const sortBox = document.getElementById("hubrepos-sort");
  if (sortBox) {
    sortBox.value = hubReposSort();
    sortBox.hidden = hubReposView() === "requests";
  }
  const count = document.getElementById("hubrepos-count");
  const view = hubReposView();
  if (count) count.textContent = view === "requests" ? (cr.loaded ? String(crOpen().length) : "") : hubRepos.note || !hubRepos.loaded ? "" : String(hubRepos.repos.length);
  let html;
  if (view === "requests") html = crView();
  else if (hubRepos.note) html = '<p class="pane-lead hr-note" role="alert">' + esc(hubRepos.note) + "</p>";
  else if (!hubRepos.repos.length) html = hubReposHero(null);
  else {
    const sorted = hubReposSorted(hubRepos.repos, hubReposSort());
    html = view === "ledger" ? hubReposLedger(sorted) : view === "feed" ? hubReposFeed(sorted) : hubReposShelf(sorted);
  }
  // A control the keyboard is on survives the repaint, so toggling ssh | http or opening a repo does not drop focus.
  const a = document.activeElement;
  const keep = a && list.contains(a) && a.dataset.act ? { act: a.dataset.act, repo: a.dataset.repo, mode: a.dataset.mode } : null;
  list.dataset.view = view;
  list.innerHTML = '<div class="hr-root hr-view-' + view + '">' + html + "</div>";
  if (keep) {
    const q = [...list.querySelectorAll("[data-act]")].find(x => x.dataset.act === keep.act && x.dataset.repo === keep.repo && x.dataset.mode === keep.mode);
    if (q) q.focus({ preventScroll: true });
  }
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
  // The requests are read with the repos, so a branch row can say it has one. A failure here is the requests view's to show.
  if (typeof crLoad === "function") crLoad();
}

async function hubReposCopy(btn) {
  const label = btn.querySelector(".hr-copylabel");
  let ok = true;
  try { await navigator.clipboard.writeText(btn.dataset.copy); } catch (e) { ok = false; }
  clearTimeout(btn._hrTimer);
  btn.classList.toggle("done", ok);
  btn.classList.toggle("failed", !ok);
  if (label) {
    if (label.dataset.orig === undefined) label.dataset.orig = label.textContent;
    label.textContent = ok ? "copied" : "could not copy";
  }
  btn._hrTimer = setTimeout(() => {
    btn.classList.remove("done", "failed");
    if (label) label.textContent = label.dataset.orig;
  }, 1400);
}

function hubReposAct(el) {
  const act = el.dataset.act, repo = el.dataset.repo;
  if (act === "mode") hubRepos.mode[repo] = el.dataset.mode;
  else if (act === "more") hubRepos.open[repo] = !hubRepos.open[repo];
  else if (act === "sel") hubRepos.sel = repo;
  else if (act === "filter") hubRepos.filter = repo;
  else return;
  hubReposPaint();
}

// Arrow keys move through the switcher, which is one tab stop like any radio group.
function hubReposSwitchKey(e) {
  const keys = { ArrowRight: 1, ArrowDown: 1, ArrowLeft: -1, ArrowUp: -1 };
  let i = HR_VIEWS.indexOf(hubReposView());
  if (e.key in keys) i = (i + keys[e.key] + HR_VIEWS.length) % HR_VIEWS.length;
  else if (e.key === "Home") i = 0;
  else if (e.key === "End") i = HR_VIEWS.length - 1;
  else return;
  e.preventDefault();
  setHubReposView(HR_VIEWS[i]);
  const b = document.querySelector('#hubrepos-views [data-hrview="' + HR_VIEWS[i] + '"]');
  if (b) b.focus();
}

addEventListener("storage", e => { if (e.key === HR_VIEW_KEY || e.key === HR_SORT_KEY || e.key === null) hubReposPaint(); });

document.addEventListener("DOMContentLoaded", () => {
  const list = document.getElementById("hubrepos-list");
  if (list) list.addEventListener("click", e => {
    const c = e.target.closest(".hr-copy");
    if (c) { hubReposCopy(c); return; }
    const a = e.target.closest("[data-act]");
    if (a) hubReposAct(a);
  });
  const sw = document.getElementById("hubrepos-views");
  if (sw) {
    sw.addEventListener("click", e => { const b = e.target.closest("[data-hrview]"); if (b) setHubReposView(b.dataset.hrview); });
    sw.addEventListener("keydown", hubReposSwitchKey);
  }
  hubReposSyncSwitch();
  const sortBox = document.getElementById("hubrepos-sort");
  if (sortBox) sortBox.addEventListener("change", () => setHubReposSort(sortBox.value));
  const refresh = document.getElementById("hubrepos-refresh");
  if (refresh) refresh.addEventListener("click", loadHubRepos);
});
