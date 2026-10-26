// ── a pasted repository link starts new work ────────────────────────────────
//
// docs/changes/f-paste-repo-new-worktree.md. A link to a repository itself (github.com/org/repo, with a trailing slash
// or `.git`) is not a pull request, an issue or a branch, so the open verb takes no card for it (`pastedOpenKind` says
// ""). It is read here as `gwt new`: a worktree off the repo's default branch, on a new `claude/work-<yymmdd>-<hhmm>`
// branch, with a card from the keypress (js/link-opening.js) naming the step. The work is `POST /v1/new`, the call the
// launch dialog's "start new work" makes, so nothing is made twice.
//
// ENTER STARTS AT ONCE ONLY FOR A REPO SOMEONE HERE ALREADY HOLDS. A worktree's own `.claude/settings.json` hooks and MCP
// servers run once a runner starts in it, so one keypress does not start a repo nobody here has seen. When neither the
// room nor the hub holds it, the hub fetches it from the forge, the room clones it (`/v1/new` with `prepare`, which
// makes no worktree and no card), and the card then stops and asks: start or cancel. Shift-Enter opens "start new work"
// with the repo and a branch name filled in.

// The branch a pasted repo starts on. `claude/` is required: a launch outside claude/* is refused (agentCheckoutRefusal).
function repoWorkBranch(now) {
  const d = now || new Date();
  const two = n => String(n).padStart(2, "0");
  return "claude/work-" + two(d.getFullYear() % 100) + two(d.getMonth() + 1) + two(d.getDate()) + "-" +
    two(d.getHours()) + two(d.getMinutes());
}

// The repository a recognised link IS, or null when it is anything else: a link that names a pull request, a number or a
// branch is not, and neither is a path under the repo (`/tree/main`, `/issues`), which a loose "any repo on this host"
// row captures all the same. The text is read for that last part, and only to refuse. { host, org, repo, slug }.
function pastedRepoKind(resolved) {
  const r = resolved || (typeof launchResolved !== "undefined" ? launchResolved : null);
  if (!r) return null;
  const v = r.vars || {};
  if ((r.tags || []).includes("pull-request")) return null;
  const host = String(v.host || r.host || "").trim().toLowerCase();
  const org = String(v.org || "").trim();
  const repo = String(v.repo || "").trim().replace(/\.git$/i, "");
  if (!host || !org || !repo || Number(v.num) > 0) return null;
  const branch = String(r.branch || "").trim();
  if (branch !== "" && !branch.includes("{")) return null;
  const link = parseRepoText(r.url, true);
  if (!link || link.rest) return null;
  return { host, org, repo, slug: host + "/" + org + "/" + repo };
}

// THE ONE PARSER for text that names a repository, shared by the repo filter and the working directory box
// (js/launchrepo.js) and by `pastedRepoKind` above. Everything a person copies reduces to host/org/repo:
//   https://host/org/repo, with or without `.git` or a trailing slash, and with a page under it (`/tree/<branch>/<path>`,
//   `/blob/...`, `/pull/N`, `/issues`, gitlab's `/-/...`, bitbucket's `/src/<branch>`)
//   git@host:org/repo(.git)   ssh://git@host(:port)/org/repo(.git)   git://host/org/repo
//   host/org/repo             only when `strict` is off, and only when the host has a dot (or is "github")
// Null when the text is none of those. `strict` demands a scheme or `git@`, which the directory box needs so a path is
// never read as a repo. { host, owner, org, repo, slug, key, url, branch, rest }: `branch` is the one a `/tree/<branch>`
// link names, `rest` is true when the link went on to a page under the repo. A gitlab subgroup is part of `owner`, told
// apart from a page by the `/-/` gitlab puts before one, or by `.git` ending the path.
const REPO_PAGE_WORDS = ["tree", "blob", "src", "raw", "commit", "commits", "pull", "pulls", "pull-requests", "merge_requests",
  "issues", "branches", "tags", "releases", "compare", "actions", "wiki", "pipelines", "blame", "history", "-"];
const REPO_BRANCH_WORDS = ["tree", "blob", "src", "raw", "commits", "blame", "history"];

function parseRepoText(text, strict) {
  const s = String(text || "").trim();
  if (!s || /\s/.test(s)) return null;
  let host, path, m;
  if ((m = s.match(/^(?:https?|ssh|git|git\+ssh):\/\/(?:[^@\/]*@)?([^\/:?#]+)(?::\d+)?\/(.*)$/i))) {
    host = m[1]; path = m[2];
  } else if ((m = s.match(/^[\w.-]+@([^\/:\s]+):\/?(.*)$/))) {
    host = m[1]; path = m[2];
  } else if (!strict && s.includes("/")) {
    host = s.slice(0, s.indexOf("/")); path = s.slice(s.indexOf("/") + 1);
    if (!host.includes(".") && host.toLowerCase() !== "github") return null;
  } else {
    return null;
  }
  host = host.toLowerCase().replace(/^www\./, "");
  if (host === "github") host = "github.com";
  if (!/^[a-z0-9.-]+$/.test(host)) return null;
  const segs = path.replace(/[?#].*$/, "").split("/").filter(Boolean);
  const gitEnd = segs.length > 0 && /\.git$/i.test(segs[segs.length - 1]);
  let at = segs.length;
  if (!gitEnd) {
    const dash = segs.indexOf("-");
    const page = segs.findIndex((x, i) => i >= 2 && REPO_PAGE_WORDS.includes(x));
    at = dash >= 2 ? dash : page >= 0 ? page : Math.min(segs.length, 2);
  }
  const named = segs.slice(0, at);
  if (named.length < 2) return null;
  named[named.length - 1] = named[named.length - 1].replace(/\.git$/i, "");
  if (named.some(x => !/^[\w.~+-]+$/.test(x))) return null;
  const owner = named.slice(0, -1).join("/");
  const repo = named[named.length - 1];
  let branch = "";
  let tail = segs.slice(at);
  if (tail[0] === "-") tail = tail.slice(1);
  if (REPO_BRANCH_WORDS.includes(tail[0]) && tail[1]) {
    try { branch = decodeURIComponent(tail[1]); } catch (e) { branch = tail[1]; }
  }
  const slug = host + "/" + owner + "/" + repo;
  return { host, owner, org: owner, repo, slug, key: launchRepoKey(slug), url: "https://" + slug, branch, rest: tail.length > 0 };
}

// Whether the room and the hub already hold the repo. A question that fails answers "no", which asks for a confirm.
async function repoSeenBy(rk, room) {
  const headers = room ? { "X-Atrium-Room": room } : {};
  const mine = api("/v1/scm/has?repo=" + encodeURIComponent(rk.slug), { headers }).then(x => !!(x && x.has), () => false);
  const hubs = typeof hubIsHub !== "undefined" && hubIsHub
    ? fetch("/_hub/git/repos").then(r => r.ok ? r.json() : null).then(b =>
      ((b && b.repos) || []).some(r => launchRepoSlug(r) === launchRepoKey(rk.slug))).catch(() => false)
    : Promise.resolve(false);
  const [inRoom, inHub] = await Promise.all([mine, hubs]);
  return { room: inRoom, hub: inHub };
}

// Enter on a repo link, with the card already drawn under `id`.
async function repoNewGo(id, rk, got, name) {
  linkOpeningName(id, rk.org + "/" + rk.repo);
  const picker = document.getElementById("l-harness");
  const harness = (document.getElementById("l-pick-field").hidden ? launchTarget && launchTarget.harness : picker.value) || "";
  const launch = document.getElementById("launch");
  if (launch && launch.open) launch.close();
  try {
    const room = await linkRoomFor(got);
    const headers = { "Content-Type": "application/json" };
    if (room) headers["X-Atrium-Room"] = room;
    linkOpeningStep(id, "finding " + rk.slug);
    const seen = await repoSeenBy(rk, room);
    if (seen.room || seen.hub) { await repoNewStart(id, rk, harness, headers, room, name); return; }
    if (typeof hubIsHub !== "undefined" && hubIsHub) {
      linkOpeningStep(id, "the hub is fetching " + rk.slug);
      await api("/_hub/git/init", { method: "POST", headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ url: "https://" + rk.slug }) });
    }
    linkOpeningStep(id, "cloning " + rk.slug);
    await api("/v1/new", { method: "POST", headers, body: JSON.stringify({ repo: rk.slug, prepare: true, progress: id }) });
    linkOpeningConfirm(id, "new repository from " + rk.host + ": its project settings and hooks will run.",
      () => repoNewStart(id, rk, harness, headers, room, name));
  } catch (e) {
    linkOpeningFail(id, (e && e.message) || String(e));
  }
}

// The worktree and the card, once the repo is somewhere this room can use it and somebody said so.
async function repoNewStart(id, rk, harness, headers, room, name) {
  try {
    const out = await api("/v1/new", { method: "POST", headers, body: JSON.stringify({
      repo: rk.slug, name: name || repoWorkBranch(), title: rk.org + "/" + rk.repo, harness, progress: id }) });
    linkOpeningEnd(id);
    rememberLaunchRoom(room, out.card);
    const warn = [out.fetch_error && "fetching origin failed, so the base is what was last fetched",
      out.hook_error && "the setup hook failed: " + out.hook_error, out.hook_warning].filter(Boolean).join(". ");
    toast(out.existed && out.card && !out.created ? "already there" : "new work", warn || out.worktree);
    const card = out.card ? await api("/v1/tasks/" + encodeURIComponent(out.card)) : null;
    if (typeof refresh === "function") refresh();
    if (!card || !card.supervised) { switchView("board"); return; }
    switchView("terms");
    openTerm(card);
  } catch (e) {
    linkOpeningFail(id, (e && e.message) || String(e));
  }
}

// Shift-Enter: the launch dialog is open, and "start new work" is filled in with this repo and a branch name.
async function repoNewEdit(rk, name) {
  await openNewWork();
  const sel = document.getElementById("l-nw-repo");
  if (![...sel.options].some(o => o.value.toLowerCase() === rk.slug)) {
    sel.insertAdjacentHTML("afterbegin", `<option value="${esc(rk.slug)}">${esc(rk.slug)}</option>`);
  }
  sel.value = [...sel.options].find(o => o.value.toLowerCase() === rk.slug).value;
  const box = document.getElementById("l-nw-name");
  box.value = name || repoWorkBranch();
  box.focus();
  box.select();
}

// THE NAME FIRST. Enter on a repo link in the switcher asks for the worktree name, `claude/` fixed and the rest
// editable, before anything starts. Enter starts it, shift-Enter is "start new work" filled in, Esc is the list again.
let repoNaming = null;

function repoNameAsk(url, rk) {
  repoNaming = { url, rk };
  repoNameShow(true);
  document.getElementById("sw-nmkey").textContent = comboName(switchKey());
  const box = document.getElementById("sw-nm");
  box.value = repoWorkBranch().slice("claude/".length);
  paintSwitcher();
  box.focus();
  box.select();
}

function repoNameShow(on) {
  document.getElementById("sw-q").hidden = on;
  document.getElementById("sw-name").hidden = !on;
  document.getElementById("sw-foot").hidden = on;
  document.getElementById("sw-nmfoot").hidden = !on;
}

function repoNameEnd() {
  if (!repoNaming) return;
  repoNaming = null;
  repoNameShow(false);
}

function repoNameBranch() {
  return "claude/" + (document.getElementById("sw-nm").value.trim() || repoWorkBranch().slice("claude/".length));
}

function repoNamePaint(host) {
  const branch = repoNameBranch();
  const folder = "<scm>/worktrees/" + repoNaming.rk.slug + "/" + branch.replace(/\//g, "-");
  setHTML(host, `<div class="swrow swopen on seen" data-kind="repo">${quickPasteRow(repoNaming.url).html}</div>
    <div class="swnmhint"><b>branch</b><code>${esc(branch)}</code><b>off</b><code>origin's default branch</code>
      <b>folder</b><code class="swnmdir">&lrm;${esc(folder)}&lrm;</code></div>`);
}

(function wireRepoName() {
  const box = document.getElementById("sw-nm");
  if (!box) return;
  box.addEventListener("input", () => { if (repoNaming) paintSwitcher(); });
  box.addEventListener("keydown", e => {
    if (!repoNaming) return;
    if (e.key === "Enter") {
      e.preventDefault();
      quickPasteGo(repoNaming.url, e.shiftKey, repoNameBranch());
    } else if (e.key === "Escape") {
      // Kept from the dialog, whose Escape would close the whole bar.
      e.preventDefault();
      repoNameEnd();
      paintSwitcher();
      document.getElementById("sw-q").focus();
    }
  });
  switcherDlg().addEventListener("close", repoNameEnd);
})();
