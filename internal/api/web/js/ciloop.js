// The ci loop tab: per-project rules for what the push loop may do, its queue, a dry-run of a request, and every reason one
// is refused. From the spike spike-ci-loop (docs in its ci-loop-spike.md). Nothing is wired to the loop yet: the projects and
// requests below are sample data and every edit lives in this page only, which the tab says out loud.
(function () {
  "use strict";

  const VERBS = [
    { v: "push", tip: "One commit on origin/main taking every file that differs, force-push to origin/<branch>, open a PR to main with auto-merge unless it edits CI. Needs a PR text written first." },
    { v: "land", tip: "One commit on origin/main with the hub claude/main tree exactly, force-push to origin/<dev-branch>. Target must be dev-*." },
    { v: "release", tip: "Signs tag v<version> with the loop user key and pushes only that tag. Only when origin main has that VERSION, the tag is new and ci.yml passed on that sha. Starts release.yml." },
    { v: "delete-tag", tip: "Deletes an orphan release tag on origin. Only vMAJOR.MINOR.PATCH, and refused when a GitHub release exists for it." },
    { v: "close", tip: "Closes an open PR. Refused when the PR head is main, master or dev-*." },
    { v: "dispatch", tip: "Runs ci.yml on a ref." },
    { v: "rerun", tip: "Reruns only the failed jobs of one run id." },
    { v: "retrigger", tip: "Reruns in full each newest not-green finished run on a branch tip." },
    { v: "fetch", tip: "Fetches a branch from origin (default main) and bundles it back for the sender. Changes nothing on GitHub." }
  ];
  const VNAMES = VERBS.map(x => x.v);
  const DEF_VERBS0 = ["push", "land", "close", "dispatch", "rerun", "retrigger", "fetch"];

  const REFUSALS = {
    "verb-not-allowed": { n: "rule 1", t: "This project does not allow that verb", tip: "The verb is off for this project: it is not in the default and not allowed here, or the project denies it." },
    "protected-name": { n: "rule 2", t: "Never pushes to main or master", tip: "Branch names must be lowercase words and dashes, and never main or master." },
    "pr-text": { n: "rule 3", t: "A push to main needs the sender PR text", tip: "Write <request>.pr.md before the request: a title that says what changes (not a file, path or commit subject) and a description under it." },
    "revert-guard": { n: "rule 4", t: "Would put files back to older content", tip: "The branch is behind origin: pushing would undo what main changed since the merge-base. Rebase and send again." },
    "attribution": { n: "rule 5", t: "Agent attribution in a push or release", tip: "Real agent attribution (an author, trailer or co-author line) in the commits or files is refused. A bare word passes." },
    "ci-green": { n: "rule 6", t: "Release needs a green ci.yml on that sha", tip: "origin main head must have the VERSION, the tag must be new, and ci.yml must have succeeded on that exact sha." },
    "orphan-only": { n: "rule 7", t: "Only an orphan tag is deleted", tip: "A tag with a GitHub release is what people download, so it stays." },
    "close-protected": { n: "rule 8", t: "Never closes a main, master or dev-* PR", tip: "Only an open PR whose head is not main, master or dev-*." },
    "one-loop": { n: "rule 9", t: "One loop at a time (lock file)", tip: "A second loop refuses to start while the first pwsh is alive." }
  };

  const PROJECTS0 = [
    { id: "atrium", repo: "dovholuknf/atrium", mode: "override", ov: { release: "allow", "delete-tag": "allow" } },
    { id: "zrok-docs", repo: "dovholuknf/zrok-docs", mode: "inherit", ov: {} },
    { id: "ziti-sdk-go", repo: "openziti/ziti-sdk-go", mode: "override", ov: { push: "deny", land: "deny" } },
    { id: "homelab", repo: "dovholuknf/homelab", mode: "override", ov: { push: "deny", land: "deny", close: "deny" } },
    { id: "scratch", repo: "dovholuknf/scratch", mode: "override", ov: { land: "deny", retrigger: "deny" } },
    { id: "netfoundry-kb", repo: "netfoundry/kb", mode: "inherit", ov: {} }
  ];
  const REQS0 = [
    { id: "r-0412", age: 1, proj: "atrium", verb: "push", arg: "ci-loop-ui", by: "ci-loop-ui worker", res: "running", log: "rebase onto origin/main ok\nland-history ok, tree check ok\nforce-push origin/ci-loop-ui ..." },
    { id: "r-0411", age: 4, proj: "atrium", verb: "push", arg: "card-chat-fix", by: "card-chat-fix worker", res: "done", log: "one commit 9f3a1c2 on origin/main base\nforce-push origin/card-chat-fix ok\nPR #471 opened, auto-merge on\nbundle: out/r-0411.bundle" },
    { id: "r-0410", age: 19, proj: "atrium", verb: "push", arg: "rooms-tab", by: "rooms-tab worker", res: "refused", why: "pr-text", log: "no PR text: write inbox/r-0410.pr.md before the request (first line the title, then what changes for clint and why). Nothing pushed." },
    { id: "r-0409", age: 41, proj: "scratch", verb: "release", arg: "0.0.7", by: "orchestrator", res: "refused", why: "verb-not-allowed", log: "scratch does not allow release (not in the default). Nothing signed." },
    { id: "r-0408", age: 55, proj: "atrium", verb: "release", arg: "0.0.6", by: "orchestrator", res: "refused", why: "ci-green", log: "ci.yml has no successful run on origin/main for 4be1d7e: not releasing" },
    { id: "r-0407", age: 63, proj: "ziti-sdk-go", verb: "push", arg: "fix-timeout", by: "sdk worker", res: "refused", why: "verb-not-allowed", log: "ziti-sdk-go denies push. Nothing pushed." },
    { id: "r-0406", age: 90, proj: "atrium", verb: "rerun", arg: "1294857731", by: "ci watcher", res: "done", log: "gh run rerun 1294857731 --failed ok" },
    { id: "r-0405", age: 140, proj: "atrium", verb: "push", arg: "walk-annotate", by: "walk worker", res: "refused", why: "revert-guard", log: "refusing: walk-annotate would put 3 file(s) back to the content they had at the merge-base 4be1d7e, undoing what origin/main changed since. Rebase the branch onto origin/main and send it again: internal/api/web/css/walk.css, internal/api/web/js/walk.js, docs/agents.tests.md" },
    { id: "r-0404", age: 190, proj: "atrium", verb: "push", arg: "docs-sweep", by: "docs worker", res: "refused", why: "attribution", log: "not releasing: attribution is named in:\n  docs/working-here.md: Co-Authored-By trailer" },
    { id: "r-0403", age: 300, proj: "homelab", verb: "push", arg: "nas-cron", by: "homelab worker", res: "refused", why: "verb-not-allowed", log: "homelab denies push. Nothing pushed." },
    { id: "r-0402", age: 410, proj: "atrium", verb: "close", arg: "455", by: "orchestrator", res: "done", log: "gh pr close 455 ok" }
  ];
  const PENDING = [
    { id: "r-0413", age: 0, proj: "atrium", verb: "push", arg: "ci-loop-mock", by: "spike worker", res: "pending", log: "waiting for the next poll (5s)" },
    { id: "r-0414", age: 0, proj: "scratch", verb: "land", arg: "dev-try", by: "scratch worker", res: "pending", log: "waiting for the next poll (5s)" }
  ];

  const S = {
    view: "projects", running: true,
    defVerbs: DEF_VERBS0.slice(),
    projects: JSON.parse(JSON.stringify(PROJECTS0)),
    reqs: JSON.parse(JSON.stringify(REQS0)), filter: "all", open: null, sel: "atrium", confirm: "", add: null,
    sim: { proj: "atrium", verb: "push", arg: "ci-loop-ui", pr: "yes", out: null }
  };

  const root = () => document.getElementById("ciloop");
  const esc = s => String(s).replace(/[&<>"]/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c]));
  const help = t => '<span class="ci-help" tabindex="0" data-tip="' + esc(t) + '">?</span>';
  const fmtAge = m => m <= 0 ? "now" : m < 60 ? m + "m" : m < 1440 ? Math.floor(m / 60) + "h " + (m % 60) + "m" : Math.floor(m / 1440) + "d";

  function effective(p) {
    const verbs = VNAMES.filter(v => p.mode === "override" && p.ov[v] ? p.ov[v] === "allow" : S.defVerbs.includes(v));
    return { src: p.mode, verbs };
  }
  function evalReq(p, verb, arg, pr) {
    const e = effective(p);
    if (!e.verbs.includes(verb)) return { r: "refused", why: "verb-not-allowed", m: p.id + " does not allow " + verb + ". Nothing done." };
    if ((verb === "push" || verb === "land") && (arg === "main" || arg === "master")) return { r: "refused", why: "protected-name", m: "never pushes to: " + arg };
    if (verb === "push" && pr !== "yes") return { r: "refused", why: "pr-text", m: "no PR text: write inbox/<request>.pr.md first. Nothing pushed." };
    if (verb === "release" && arg === "0.0.6") return { r: "refused", why: "ci-green", m: "ci.yml has no successful run on origin/main head: not releasing" };
    if (verb === "close" && /^dev-/.test(arg)) return { r: "refused", why: "close-protected", m: "never closes a PR from " + arg };
    return { r: "done", m: verb + " " + arg + " accepted: would run and bundle the result" };
  }

  function head() {
    const pend = S.running ? 0 : PENDING.length;
    return '<div class="ci-status">' +
      '<span class="ci-pill ' + (S.running ? "ok" : "bad") + '">' + (S.running ? "loop running as clint, polled 3s ago" : "loop stopped: nothing is being pushed") + "</span>" +
      '<span class="ci-pill ' + (S.running ? "run" : "dim") + '">' + (S.running ? "polling every 5s" : "hub holds the queue") + "</span>" +
      '<span class="ci-pill ' + (pend ? "warn" : "dim") + '">' + (pend ? pend + " waiting for a loop" : S.reqs.length + " requests on file") + "</span>" +
      (S.confirm === "stop" ? '<span class="ci-ask">Stop the loop? Nothing is pushed until it starts again.</span><button class="danger" data-act="running" data-on="0">yes, stop</button><button data-act="confirm" data-c="">cancel</button>'
        : S.running ? '<button data-act="confirm" data-c="stop">stop loop</button>' : '<button class="primary" data-act="running" data-on="1">start loop</button>') + "</div>";
  }

  function projectsView() {
    const defChips = VERBS.map(x => '<button type="button" class="ci-vchip' + (S.defVerbs.includes(x.v) ? " on" : "") + '" aria-pressed="' + S.defVerbs.includes(x.v) +
      '" data-act="defverb" data-v="' + x.v + '">' + x.v + "</button>").join("");
    const rows = S.projects.filter(p => hit(p.id, p.repo)).map(p => {
      const e = effective(p), ro = p.mode === "inherit";
      const chips = VNAMES.map(v => {
        const set = !ro && p.ov[v], on = e.verbs.includes(v);
        return '<button type="button" class="ci-vchip' + (on ? " on" : "") + (set ? "" : " inherited") + (set === "deny" ? " denied" : set ? " allowed" : "") + (ro ? " ro" : "") +
          '" aria-pressed="' + on + '" data-tip="' + (ro ? "Inherited from the default. Switch to override to change it." : set ? (set === "deny" ? "Denied here. Click to inherit the default." : "Allowed here. Click to deny.") : "Inherited from the default. Click to allow here.") +
          '" data-act="verb" data-p="' + esc(p.id) + '" data-v="' + v + '"><span class="ci-g">' + (set === "deny" ? "×" : set ? "✓" : "") + "</span>" + v + "</button>";
      }).join("");
      const seg = ["inherit", "override"].map(m => '<button class="' + (p.mode === m ? "on" : "") + '" data-act="mode" data-p="' + esc(p.id) + '" data-m="' + m + '">' + m + "</button>").join("");
      return '<tr class="clk' + (S.sel === p.id ? " sel" : "") + '" data-act="sel" data-p="' + esc(p.id) + '">' +
        '<td class="ci-first"><div class="ci-proj">' + esc(p.id) + '</div><div class="ci-sub ci-mono">' + esc(p.repo) + "</div></td>" +
        '<td data-l="override"><div class="ci-seg">' + seg + "</div></td>" +
        '<td data-l="verbs"><div class="ci-verbs">' + chips + "</div></td></tr>";
    }).join("");
    return '<div class="ci-sec"><h3>Default for every project' + help("The verbs a project may use when it inherits. A new project starts here. Click a verb to turn it on or off for every project that inherits.") + "</h3>" +
      '<div class="ci-body"><div class="ci-verbs">' + defChips + "</div></div></div>" +
      '<div class="ci-sec"><h3>Projects' + help("Where the rules live: the hub store, edited here by the signed-in operator only. Agents file requests. They cannot read or change this page, the rules or the credentials, and neither can a card or a room.") +
      '<span class="ci-sp"></span><button data-act="addproj">add project</button></h3>' + addForm() + '<div class="ci-scroll">' +
      '<table class="ci-t ci-fix"><colgroup><col class="c-proj"><col class="c-rule"><col></colgroup><thead><tr><th>project</th><th>rule' + help("inherit takes the default verbs as they are. override lets this project allow or deny single verbs and inherit the rest.") +
      "</th><th>verbs allowed" + help("Dashed is inherited from the default. Green with a check is allowed on this project. Marked with an x is denied on this project. In override, click a verb to step through the three.") +
      '</th></tr></thead><tbody>' + rows + "</tbody></table></div></div>";
  }

  function addForm() {
    const a = S.add;
    if (!a) return "";
    return '<div class="ci-body ci-add"><div class="ci-fld"><label>name</label><input type="text" data-g="id" value="' + esc(a.id) + '" placeholder="my-project" size="18" autocomplete="off"></div>' +
      '<div class="ci-fld"><label>repo</label><input type="text" data-g="repo" value="' + esc(a.repo) + '" placeholder="owner/repo" size="26" autocomplete="off"></div>' +
      '<div class="ci-fld"><button class="primary" data-act="addok">add</button><button data-act="addno">cancel</button>' + (a.err ? '<span class="ci-bad">' + esc(a.err) + "</span>" : "") + "</div></div>";
  }

  function queueView() {
    const l = [...PENDING, ...S.reqs].filter(r => (S.filter === "all" || r.res === S.filter) && hit(r.id, r.proj, r.verb, r.arg, r.by));
    const rows = l.map(r => {
      const open = S.open === r.id;
      let detail = "";
      if (open) {
        const w = r.why && REFUSALS[r.why];
        detail = '<tr><td colspan="6" style="background:var(--lift)">' +
          (w ? '<div class="ci-why"><b>' + w.n + ": " + r.why + "</b><span>" + esc(w.t) + help(w.tip) + "</span></div>" : "") +
          '<pre class="ci-log">' + esc(r.log) + "</pre>" +
          (r.res === "refused" ? '<div class="ci-fld" style="margin:10px 0 0"><button data-act="resend" data-id="' + esc(r.id) + '">send again</button><button data-act="view" data-v="projects">open the project rules</button></div>' : "") +
          "</td></tr>";
      }
      return '<tr class="clk' + (open ? " sel" : "") + '" data-act="open" data-id="' + esc(r.id) + '">' +
        '<td class="ci-mono ci-first">' + esc(r.id) + '<span class="ci-res ' + r.res + '">' + r.res + "</span></td>" +
        '<td data-l="age" class="ci-sub">' + fmtAge(r.age) + "</td>" +
        '<td data-l="project" class="ci-proj">' + esc(r.proj) + '</td><td data-l="verb"><span class="ci-vb">' + r.verb + "</span></td>" +
        '<td data-l="argument" class="ci-mono">' + esc(r.arg) + '</td><td data-l="from" class="ci-sub">' + esc(r.by) + "</td></tr>" + detail;
    }).join("");
    const nref = S.reqs.filter(r => r.res === "refused").length;
    return '<div class="ci-sec"><h3>Requests<span class="ci-sp"></span><span class="ci-sub" style="text-transform:none;letter-spacing:0">' + nref + " refused of " + S.reqs.length + "</span></h3>" +
      (l.length ? '<div class="ci-scroll"><table class="ci-t"><thead><tr><th>id and result</th><th>age</th><th>project' + help("The request names its project. An agent cannot name one it is not working in: the hub fills it in from the card.") +
        '</th><th>verb</th><th>argument</th><th>from</th></tr></thead><tbody>' + rows + "</tbody></table></div>"
        : '<div class="ci-empty">No requests here.</div>') + "</div>";
  }

  function tryView() {
    const s = S.sim, p = S.projects.find(x => x.id === s.proj), o = s.out;
    const opts = (a, v) => a.map(x => "<option" + (x === v ? " selected" : "") + ">" + esc(x) + "</option>").join("");
    const res = o ? (o.r === "done"
      ? '<div class="ci-ok"><b>accepted</b>: ' + esc(o.m) + "</div>"
      : '<div class="ci-why"><b>' + REFUSALS[o.why].n + ": " + o.why + "</b><span>" + esc(REFUSALS[o.why].t) + help(REFUSALS[o.why].tip) + "<br>" + esc(o.m) + "</span></div>") : "";
    return '<div class="ci-sec" style="max-width:620px"><h3>Try a request' + help("Runs the same checks the loop runs, against the rules above, and sends nothing.") + '</h3><div class="ci-body">' +
      '<div class="ci-fld"><label>project</label><select data-f="proj">' + opts(S.projects.map(x => x.id), s.proj) + '</select><span class="ci-src">' + effective(p).src + "</span></div>" +
      '<div class="ci-fld"><label>verb</label><select data-f="verb">' + opts(VNAMES, s.verb) + "</select></div>" +
      '<div class="ci-fld"><label>argument</label><input type="text" data-f="arg" value="' + esc(s.arg) + '" size="22"></div>' +
      '<div class="ci-fld"><label>PR text' + help("push only: whether a .pr.md was written before the request") + '</label><select data-f="pr">' + opts(["yes", "no"], s.pr) + "</select></div>" +
      '<div class="ci-fld"><button class="primary" data-act="sim">check</button></div>' + res + "</div></div>";
  }

  function rulesView() {
    const cnt = k => S.reqs.filter(r => r.why === k).length;
    const rows = Object.entries(REFUSALS).filter(([k, w]) => hit(k, w.t, w.n)).map(([k, w]) => '<div class="ci-rule"><span class="ci-n">' + w.n + "</span><div><code>" + k + "</code> <span>" + esc(w.t) + "</span>" + help(w.tip) +
      '</div><span class="ci-hits">' + (cnt(k) || "no") + " refusal" + (cnt(k) === 1 ? "" : "s") + " on file</span></div>").join("");
    return '<div class="ci-sec"><h3>Every reason a request is refused' + help("Rule 1 is the per-project rule you set on the projects view. Rules 2 to 9 are the guards the loop always applies, for every project, and nothing here switches them off.") + "</h3>" + rows + "</div>";
  }

  const VIEWS_ROW = [["projects", "projects"], ["queue", "queue"], ["try", "try a request"], ["rules", "refusals"]];
  const query = () => (typeof fdSearchText === "function" ? fdSearchText("ciloop") : "");
  const hit = (...a) => { const q = query(); return !q || a.some(x => String(x).toLowerCase().includes(q)); };

  function paintDrawer() {
    const v = document.getElementById("ciloop-views"), s = document.getElementById("ciloop-show");
    if (v) v.innerHTML = VIEWS_ROW.map(([k, l]) => '<button type="button" class="' + (S.view === k ? "on" : "") + '" data-act="view" data-v="' + k + '">' + l + "</button>").join("");
    if (s) s.innerHTML = ["all", "pending", "running", "done", "refused"].map(k => '<button type="button" class="' + (S.filter === k ? "on" : "") + '" data-act="filter" data-f="' + k + '">' + k + "</button>").join("");
    if (typeof fdStatus !== "function") return;
    const label = VIEWS_ROW.find(x => x[0] === S.view)[1];
    const summary = S.view === "queue" ? "queue, " + (S.filter === "all" ? "every result" : S.filter) : label;
    fdStatus("ciloop", { summary, match: "" });
  }

  function paintCiLoop() {
    const el = document.getElementById("ciloop-body");
    if (!el) return;
    paintDrawer();
    el.innerHTML = '<p class="pane-lead">Sample data: not connected to the loop yet. Changes last until you reload.</p>' + head() +
      ({ projects: projectsView, queue: queueView, try: tryView, rules: rulesView })[S.view]();
  }

  function ciLoopClick(e) {
    const t = e.target.closest("[data-act]");
    if (!t || !root().contains(t)) return;
    const a = t.dataset.act, p = t.dataset.p && S.projects.find(x => x.id === t.dataset.p);
    if (a === "view") S.view = t.dataset.v;
    else if (a === "running") { S.running = t.dataset.on === "1"; S.confirm = ""; }
    else if (a === "confirm") S.confirm = t.dataset.c;
    else if (a === "defverb") { const i = S.defVerbs.indexOf(t.dataset.v); if (i < 0) S.defVerbs.push(t.dataset.v); else S.defVerbs.splice(i, 1); }
    else if (a === "sel") S.sel = t.dataset.p;
    else if (a === "mode") p.mode = t.dataset.m;
    else if (a === "verb") {
      if (p.mode !== "override") return;
      const v = t.dataset.v, next = { undefined: "allow", allow: "deny", deny: undefined }[p.ov[v]];
      if (next) p.ov[v] = next; else delete p.ov[v];
    }
    else if (a === "filter") S.filter = t.dataset.f;
    else if (a === "open") S.open = S.open === t.dataset.id ? null : t.dataset.id;
    else if (a === "resend") { const r = S.reqs.find(x => x.id === t.dataset.id); S.sim = { proj: r.proj, verb: r.verb, arg: r.arg, pr: "yes", out: null }; S.view = "try"; }
    else if (a === "sim") S.sim.out = evalReq(S.projects.find(x => x.id === S.sim.proj), S.sim.verb, S.sim.arg.trim(), S.sim.pr);
    else if (a === "addproj") S.add = S.add || { id: "", repo: "", err: "" };
    else if (a === "addno") S.add = null;
    else if (a === "addok") {
      const id = S.add.id.trim(), repo = S.add.repo.trim();
      if (!/^[a-z0-9]+(-[a-z0-9]+)*$/.test(id)) S.add.err = "Name: lowercase words and dashes.";
      else if (S.projects.some(x => x.id === id)) S.add.err = "That name is taken.";
      else if (!/^[\w.-]+\/[\w.-]+$/.test(repo)) S.add.err = "Repo looks like owner/repo.";
      else { S.projects.push({ id, repo, mode: "inherit", ov: {} }); S.add = null; }
    }
    paintCiLoop();
  }

  function ciLoopChange(e) {
    const g = e.target.dataset.g;
    if (g && S.add) { S.add[g] = e.target.value; S.add.err = ""; return; }
    const f = e.target.dataset.f;
    if (f && S.view === "try") { S.sim[f] = e.target.value; S.sim.out = null; if (e.target.tagName === "SELECT") paintCiLoop(); }
  }

  window.loadCiLoop = paintCiLoop;
  window.paintCiLoop = paintCiLoop;
  const el = root();
  if (el) { el.addEventListener("click", ciLoopClick); el.addEventListener("change", ciLoopChange); el.addEventListener("input", ciLoopChange); paintCiLoop(); }
})();
