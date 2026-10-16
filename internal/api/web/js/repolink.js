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
  let path;
  try { path = new URL(String(r.url || "")).pathname; } catch (e) { return null; }
  const segs = path.split("/").filter(Boolean);
  if (segs.length !== 2) return null;
  return { host, org, repo, slug: host + "/" + org + "/" + repo };
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
async function repoNewGo(id, rk, got) {
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
    if (seen.room || seen.hub) { await repoNewStart(id, rk, harness, headers, room); return; }
    if (typeof hubIsHub !== "undefined" && hubIsHub) {
      linkOpeningStep(id, "the hub is fetching " + rk.slug);
      await api("/_hub/git/init", { method: "POST", headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ url: "https://" + rk.slug }) });
    }
    linkOpeningStep(id, "cloning " + rk.slug);
    await api("/v1/new", { method: "POST", headers, body: JSON.stringify({ repo: rk.slug, prepare: true, progress: id }) });
    linkOpeningConfirm(id, "new repository from " + rk.host + ": its project settings and hooks will run.",
      () => repoNewStart(id, rk, harness, headers, room));
  } catch (e) {
    linkOpeningFail(id, (e && e.message) || String(e));
  }
}

// The worktree and the card, once the repo is somewhere this room can use it and somebody said so.
async function repoNewStart(id, rk, harness, headers, room) {
  try {
    const out = await api("/v1/new", { method: "POST", headers, body: JSON.stringify({
      repo: rk.slug, name: repoWorkBranch(), title: rk.org + "/" + rk.repo, harness, progress: id }) });
    linkOpeningEnd(id);
    rememberLaunchRoom(room, out.card);
    const warn = [out.fetch_error && "fetching origin failed, so the base is what was last fetched",
      out.hook_error && "the setup hook failed: " + out.hook_error].filter(Boolean).join(". ");
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
async function repoNewEdit(rk) {
  await openNewWork();
  const sel = document.getElementById("l-nw-repo");
  if (![...sel.options].some(o => o.value.toLowerCase() === rk.slug)) {
    sel.insertAdjacentHTML("afterbegin", `<option value="${esc(rk.slug)}">${esc(rk.slug)}</option>`);
  }
  sel.value = [...sel.options].find(o => o.value.toLowerCase() === rk.slug).value;
  const name = document.getElementById("l-nw-name");
  name.value = repoWorkBranch();
  name.focus();
  name.select();
}
