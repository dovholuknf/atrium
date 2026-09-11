// ── fixtures ────────────────────────────────────────────
//
// Terminals that come up with the daemon. Rows in a table, like runners,
// because that is what they are: a definition of what to start, edited one at
// a time and ordered.

let allFixtures = [];

async function renderFixtures() {
  const host = document.getElementById("fixture-list");
  if (!host) return;
  try { allFixtures = (await api("/v1/fixtures")).fixtures || []; } catch (e) { return; }

  if (!allFixtures.length) {
    setHTML(host, `<div class="panel"><div class="empty">
      nothing starts on its own yet. add one for the session you open every morning.
    </div></div>`);
    return;
  }

  setHTML(host, `<div class="panel">` + allFixtures.map((f, i) => `
    <div class="row line${f.last_error ? " broke" : ""}">
      <span class="chip ${f.enabled ? "accent" : ""}">${f.enabled ? "on" : "off"}</span>
      <span class="ord">${i + 1}</span>
      <span class="tool">${esc(f.label || repoLeaf(f.cwd) || f.harness)}</span>
      <code class="grow ell" title="${esc(f.cwd)}">${esc(f.cwd || "(the runner's own directory)")}</code>
      <span class="by">${esc(f.harness)}</span>
      ${f.resume ? `<span class="by">resumes</span>` : `<span class="by">fresh</span>`}
      ${f.theme ? `<span class="chip">${esc(f.theme)}</span>` : ""}
      <button onclick="moveFixture('${esc(f.id)}', -1)" ${i === 0 ? "disabled" : ""}
        title="start this one earlier">&#9650;</button>
      <button onclick="moveFixture('${esc(f.id)}', 1)"
        ${i === allFixtures.length - 1 ? "disabled" : ""}
        title="start this one later">&#9660;</button>
      <button onclick="startFixtureNow('${esc(f.id)}')"
        title="start it now, without restarting atrium">start</button>
      <button onclick="editFixture('${esc(f.id)}')">edit</button>
    </div>` +
    // Why the last start failed, under the row rather than in a tooltip. This
    // is the page the notification sends you to, so the reason has to be the
    // thing you see on arriving, not something else to go looking for.
    //
    // It stays up until a start works, so a fixture that has been broken since
    // Tuesday still says so on Friday, and says when it last tried.
    (f.last_error ? `<div class="row line why-broke">
      <span class="chip warn">did not start</span>
      <code class="grow ell" title="${esc(f.last_error)}">${esc(f.last_error)}</code>
      ${f.last_run_at ? `<span class="by">${esc(firstSeen(f.last_run_at))}</span>` : ""}
    </div>` : "")).join("") + `</div>`);
}

// Reordering by swapping sort values with the neighbor.
//
// The list is short and hand written, so this is two writes rather than a
// fractional rank scheme. Midpoint insertion earns its keep on a board with
// hundreds of cards, not on a list of five.
async function moveFixture(id, delta) {
  const i = allFixtures.findIndex(f => f.id === id);
  const j = i + delta;
  if (i < 0 || j < 0 || j >= allFixtures.length) return;

  const a = allFixtures[i], b = allFixtures[j];
  const at = a.sort, bt = b.sort;
  // Equal sorts fall back to creation order, which makes a swap a no-op. Give
  // them distinct ones based on position instead.
  a.sort = (at === bt) ? j : bt;
  b.sort = (at === bt) ? i : at;
  try {
    await api(`/v1/fixtures/${a.id}`, {
      method: "PUT", headers: { "Content-Type": "application/json" }, body: JSON.stringify(a)
    });
    await api(`/v1/fixtures/${b.id}`, {
      method: "PUT", headers: { "Content-Type": "application/json" }, body: JSON.stringify(b)
    });
  } catch (e) {
    toast("could not reorder", e.message);
  }
  renderFixtures();
}

// Opens the form. A null id is a new one.
function editFixture(id) {
  const f = allFixtures.find(x => x.id === id) || {
    id: "", label: "", harness: "", cwd: "", theme: "",
    // A new fixture resumes and starts, because that is what somebody adding
    // one means. Anyone who wants otherwise is one click away from it.
    resume: true, enabled: true,
    sort: allFixtures.length
  };
  const dlg = document.getElementById("fixture");
  dlg.dataset.editing = f.id;
  document.getElementById("f-heading").textContent = f.id ? "fixture" : "new fixture";

  const enabled = allHarnesses.filter(h => h.enabled);
  document.getElementById("f-harness").innerHTML = enabled.map(h =>
    `<option value="${esc(h.id)}"${h.id === f.harness ? " selected" : ""}>${esc(h.label)}</option>`
  ).join("");
  // Every theme, with an explicit entry for "let the project decide". A blank
  // first option rather than an empty select: the default is a real choice and
  // has to be selectable, not just what you get by not choosing.
  document.getElementById("f-theme").innerHTML = themeOptionsHTML(f.theme || "");

  document.getElementById("f-label").value = f.label || "";
  document.getElementById("f-cwd").value = f.cwd || "";
  document.getElementById("f-theme").value = f.theme || "";
  document.getElementById("f-resume").checked = !!f.resume;
  // Empty means latest, which is what an existing fixture with resume on
  // should always have been doing.
  document.getElementById("f-resume-mode").value =
    f.resume_mode === "card" ? "card" : "latest";
  document.getElementById("f-enabled").checked = !!f.enabled;
  document.getElementById("f-delete").hidden = !f.id;

  if (!enabled.length) {
    tellUser("atrium", "no runner is enabled yet. turn one on above first.");
    return;
  }
  dlg.showModal();
}

async function saveFixture() {
  const dlg = document.getElementById("fixture");
  const id = dlg.dataset.editing || "";
  const known = allFixtures.find(x => x.id === id);
  const body = {
    id,
    label: document.getElementById("f-label").value.trim(),
    harness: document.getElementById("f-harness").value,
    cwd: document.getElementById("f-cwd").value.trim(),
    theme: document.getElementById("f-theme").value.trim(),
    resume: document.getElementById("f-resume").checked,
    resume_mode: document.getElementById("f-resume-mode").value,
    enabled: document.getElementById("f-enabled").checked,
    // Keeps its place in the order, or goes last when it is new.
    sort: known ? known.sort : allFixtures.length,
    // Carried through, or saving would forget which card this starts onto and
    // the next start would open a second one beside it.
    task_id: known ? known.task_id : ""
  };
  let saved;
  try {
    saved = await api(id ? `/v1/fixtures/${id}` : "/v1/fixtures", {
      method: id ? "PUT" : "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body)
    });
  } catch (e) {
    tellUser("atrium", e.message);
    return;
  }
  dlg.close();
  renderFixtures();

  // A fixture is a terminal that comes up with the daemon, which means a new
  // one does nothing until the next restart. Nobody sets one up in order to
  // wait for that: they set it up because they want the terminal. So it offers
  // to start it, once, at the moment it was asked for.
  //
  // Only on create, and only when enabled. Editing an existing fixture is
  // usually changing a detail on something already running, and asking then
  // would be offering a second copy.
  if (id || !body.enabled) return;
  const newID = (saved && saved.id) || "";
  if (!newID) return;
  if (!await confirmUser("start it now?",
    "It comes up with atrium from now on. Starting it now means you do not have to " +
    "restart the daemon to get the terminal you just asked for.",
    "start it", "fixture-start-now")) return;
  startFixtureNow(newID);
}

async function deleteFixture() {
  const id = document.getElementById("fixture").dataset.editing;
  if (!id) return;
  if (!await confirmUser("delete this fixture?",
    "It stops coming up with atrium. The card it started and anything running on it are " +
    "left alone.", "delete it")) return;
  try {
    await api(`/v1/fixtures/${id}`, { method: "DELETE" });
  } catch (e) {
    tellUser("atrium", e.message);
    return;
  }
  document.getElementById("fixture").close();
  renderFixtures();
}

async function startFixtureNow(id) {
  try {
    await api(`/v1/fixtures/${id}/start`, { method: "POST" });
    toast("starting", "it will appear in terminals");
  } catch (e) {
    toast("could not start it", e.message);
  }
  refresh();
}

// Runners this machine has that atrium is not set up to use. Offered rather
// than added: what to run is a decision, and a row that appeared on its own is
// a row you did not agree to.
async function renderDiscovered() {
  const host = document.getElementById("discovered");
  if (!host) return;
  let found = [];
  try { found = (await api("/v1/harnesses/discover")).candidates || []; } catch (e) { return; }
  if (!found.length) { host.innerHTML = ""; return; }

  host.innerHTML = `<div class="panel">
    <div class="empty" style="text-align:left">
      found on this machine and not set up yet
    </div>` + found.map(c => `
    <div class="row line">
      <span class="tool">${esc(c.label)}</span>
      <code class="grow ell" title="${esc(c.found)}">${esc(c.found)}</code>
      <button class="go" onclick='addDiscovered(${JSON.stringify(c).replace(/'/g, "&#39;")})'
        >set it up</button>
    </div>`).join("") + `</div>`;
}

// Which runner's hooks file a row belongs to, or "" when atrium has none for
// it. Matched on the command rather than the row id, since the id is whatever
// you typed when you made the row.
function hookTargetFor(h) {
  const cmd = (h.cmd || "").toLowerCase().replace(/\\/g, "/");
  const leaf = cmd.split("/").pop().replace(/\.(exe|cmd|bat|ps1)$/, "");
  if (leaf === "claude") return "claude";
  if (leaf === "codex") return "codex";
  return "";
}

// What a runner with no hooks of its own can tell atrium, which is nothing.
//
// Written on the row rather than left blank. The table offers four runners as
// equals and two of them cannot report a thing: no activity, no session
// starting or ending, no resume id. A blank space there reads as "wired" to
// anyone who has not read the notes field, and a card that never leaves
// `running` reads as atrium being broken rather than as the runner having
// nothing to say.
const REPORTS_NOTHING =
  "this runner has no hook system, so atrium learns nothing from it: no activity, " +
  "no session starting or ending, and no id to resume from. its card shows the " +
  "terminal, the permission gate and whatever you typed on the card itself.";

// Not wiring the hooks is a legitimate choice, so the reminder can be turned
// off. It goes through the same store as every other "do not ask me again", so
// the gear lists it and the one button there brings all of them back.
const HOOK_NAG = "hooks-nag";

// The way in, on the row it belongs with. Counts rather than a bare link,
// because "3 hooks missing" is the whole reason to open it.
//
// Turned off, the button stays and only the count and the color go. Removing
// it would take away the way back to the dialog that turns the reminder on
// again.
function hooksChip(h) {
  const target = hookTargetFor(h);
  if (!target) return `<span class="by missing" title="${esc(REPORTS_NOTHING)}"
    >reports nothing</span>`;
  const rep = target === "codex" ? codexHooks : hookReport;
  // A row atrium could wire and has not looked at yet. The button still opens
  // the dialog, which is where the answer is; a count nobody fetched would be
  // a number made up on the row.
  if (!rep) return `<button onclick="openHooks()" title="${esc(target)} hooks">hooks</button>`;
  if (rep.refused) {
    return `<button onclick="openHooks()"
      title="${esc(rep.refused)}">hooks: cannot wire</button>`;
  }
  const n = skippedConfirms()[HOOK_NAG] ? 0 : rep.missing;
  return `<button class="${n ? "go" : ""}" onclick="openHooks()"
    title="${n ? "atrium cannot see what these sessions are doing" : esc(target) + " hooks"}"
    >hooks${n ? `: ${n} missing` : ""}</button>`;
}

async function openHooks() {
  document.getElementById("hooks").showModal();
  await loadHooks();
  renderHooks();
}

// Which hooks are wired, and the offer to wire them.
//
// A feature nobody installs is a feature that does not exist. Activity,
// subagent counts and reaching an idle session are all built and all inert
// until these land in settings.json, and the only place that was written down
// was a documentation page.
// Fetched separately from painting, because the claude row's chip needs the
// count and that row is drawn first.
async function loadHooks() {
  const before = hookReport ? hookReport.missing : null;
  try { hookReport = await api("/v1/hooks"); } catch (e) { return; }

  // Codex keeps its hooks in its own file, in the same format. Loaded
  // separately and only when codex is a runner this machine uses: asking about
  // a tool that is not installed would report a whole section missing and
  // offer to fix something nobody asked for.
  codexHooks = null;
  if ((allHarnesses || []).some(h => h.id === "codex" && h.enabled)) {
    try { codexHooks = await api("/v1/hooks?runner=codex"); } catch (e) { codexHooks = null; }
  }

  // Nothing left to do means the dialog has nothing left to say. Only on the
  // way to zero: opening it when everything is already wired is somebody
  // checking, and closing it under them would be answering a question they
  // did not ask.
  // The steps dialog drops what has been done and closes when it empties, so
  // opening it for one hook and running that command puts you straight back on
  // the list with the row now reading wired.
  paintHookSteps();

  if (before !== null && before > 0 && hookReport.missing === 0) {
    const dlg = document.getElementById("hooks");
    if (dlg.open) dlg.close();
    toast("all hooks wired", "sessions started from now on report what they are doing");
  }
}

// One hook, as a row. Shared by both runners because the format is shared and
// so is every rule about what a row means: wired, pointing elsewhere, off on
// purpose.
//
// `runner` is empty for claude, which keeps the manual-steps dialog pointed at
// the one report it knows how to read.
function hookRow(h, runner) {
  const done = h.installed && !h.stale;
  // BOTH PATHS, in the tooltip. `points elsewhere` names neither the entry's
  // path nor the daemon's, which leaves opening settings.json and comparing by
  // eye as the only way to act on it.
  const drift = h.stale
    ? `this entry runs:\n${h.found || "(nothing)"}\n\natrium is running:\n${h.want || ""}`
    : (h.found || "");
  const state = h.stale
    ? `<span class="by missing" title="${esc(drift)}">points elsewhere</span>`
    : done
      ? `<span class="by found" title="${esc(h.found || "")}">wired</span>`
      : `<span class="by missing">not wired</span>`;
  // Each row decides for itself. Wanting the tool events and not the subagent
  // count is a reasonable thing to want, and one button for all of them made
  // that impossible to say.
  const arg = runner ? `,'${esc(runner)}'` : "";
  const act = done ? "" : `
    <button class="go" onclick="installHooks(['${esc(h.event)}']${arg})"
      title="write this one entry">wire it</button>` +
    // The manual steps read the claude report, so they are offered only there
    // rather than showing the wrong command for the other runner.
    (runner ? "" : `
    <button onclick="showHookSteps('${esc(h.event)}')"
      title="the command to run yourself">manually</button>`);
  // An optional hook is off on purpose rather than missing, so it does not
  // read as something to fix. What it does is said next to it in full: this is
  // the one hook whose answer changes what a session does, and nobody should
  // turn that on from a one-line summary.
  const state2 = h.optional && !h.installed
    ? `<span class="by" title="offered, not installed by default">off</span>`
    : state;
  const warn = h.warn
    ? `<div class="row line"><span class="hintline warn">${esc(h.warn)}</span></div>`
    : "";
  return `<div class="row line${h.optional ? " optional" : ""}">
    <span class="tool">${esc(h.hook)}</span>
    <span class="grow ell" title="${esc(h.why)}">${esc(h.why)}</span>
    ${state2}
    ${act}
  </div>${warn}`;
}

function renderHooks() {
  const host = document.getElementById("hook-status");
  const rep = hookReport;
  if (!host || !rep) return;

  const rows = (rep.hooks || []).map(h => hookRow(h, "")).join("");

  // The file itself is worth showing. Atrium picks the one in the home
  // directory, and being told which file was changed is the difference
  // between trusting the button and going to look.
  const where = `<div class="row line copypath">
      <code>${esc(rep.path)}</code>
      <button onclick='copyText(this, ${JSON.stringify(rep.path).replace(/'/g, "&#39;")})'
        title="copy this path">copy</button>
      ${rep.exists ? "" : `<span class="by">does not exist yet</span>`}
    </div>`;

  if (rep.unreadable) {
    setHTML(host, `<div class="panel">${where}${rows}
      <div class="row line"><span class="by missing">that file is not valid json, so atrium
      will not write to it. fix it and reload.</span></div></div>`);
    return;
  }

  const quiet = !!skippedConfirms()[HOOK_NAG];
  // The rows do them one at a time. These do the lot, which is what you want
  // the first time and never again.
  const action = rep.missing === 0
    ? `<span class="by found">all wired. sessions started from now on report what they are doing.</span>`
    : `<button class="go" onclick="installHooks()">do all ${rep.missing} for me</button>
       <button onclick="showHookSteps()">let me do them all manually</button>`;

  // The reminder is separate from the wiring, and stays offerable once it is
  // off: not wiring these is a choice, and so is changing your mind.
  const nag = rep.missing === 0 ? "" : quiet
    ? `<button onclick="setHookNag(false)"
         title="show the count on the claude row again">remind me again</button>`
    : `<button onclick="setHookNag(true)"
         title="keep the button, drop the count and the color">stop reminding me</button>`;

  setHTML(host, `<div class="panel">${where}${rows}
    <div class="row line">${nag}<span class="grow"></span>${action}</div></div>` +
    codexPanel());
}

// Codex's hooks, in codex's own file.
//
// Its own panel rather than mixed in, because they are two files and wiring
// one says nothing about the other. Drawn only when codex is a runner this
// machine uses: a section reporting eight missing hooks for a tool that is not
// installed is a problem invented by the board.
//
// The rows reuse hookRow, since the format is shared and so is every rule
// about what a row means.
function codexPanel() {
  const rep = codexHooks;
  if (!rep) return "";
  const rows = (rep.hooks || []).map(h => hookRow(h, "codex")).join("");
  const where = `<div class="row line copypath">
      <code>${esc(rep.path)}</code>
      <button onclick='copyText(this, ${JSON.stringify(rep.path).replace(/'/g, "&#39;")})'
        title="copy this path">copy</button>
      ${rep.exists ? "" : `<span class="by">does not exist yet</span>`}
    </div>`;
  // Writing the file is not enough for codex, and saying so is the difference
  // between a button that worked and a button that appeared to.
  const trust = rep.trust
    ? `<div class="row line"><span class="hintline warn">${esc(rep.trust)}</span></div>`
    : "";
  // A runner atrium cannot point at this binary at all. Said here, with the
  // reason, instead of leaving a button that fails the moment it is pressed.
  // Codex reads the first word of a hook command as the program and strips no
  // quotes, so a path with a space in it has no spelling that works.
  const action = rep.refused
    ? `<span class="by missing" title="${esc(rep.refused)}">cannot be wired here</span>`
    : rep.missing === 0
      ? `<span class="by found">all wired.</span>`
      : `<button class="go" onclick="installHooks(null, 'codex')"
           >do all ${rep.missing} for me</button>`;
  const refused = rep.refused
    ? `<div class="row line"><span class="hintline warn">${esc(rep.refused)}</span></div>`
    : "";
  return `<h3 class="s-section">codex</h3>
    <div class="panel">${where}${rows}${refused}${trust}
      <div class="row line"><span class="grow"></span>${action}</div></div>`;
}

let hookReport = null;
// Codex's own hooks, in its own file. Null when codex is not a runner this
// machine uses, which is not the same as none installed.
let codexHooks = null;

// Claude Code's name for the hook that carries one atrium event, for saying
// which one a button is about.
function hookNameFor(event, rep) {
  const r = rep || hookReport;
  const h = (r && r.hooks || []).find(x => x.event === event);
  return h ? h.hook : event;
}

// The line that would go into settings.json for one event, which is what
// claude code will actually run.
function installedCommandFor(event, rep) {
  const r = rep || hookReport;
  const h = (r && r.hooks || []).find(x => x.event === event);
  return h ? h.want : "";
}

// Writing it. Confirmed first, and never skippable: this edits a file atrium
// does not own, and a settings.json that comes back wrong breaks every claude
// session on the machine.
async function installHooks(events, runner) {
  // Which report this is about. Codex keeps its own, in its own file.
  const rep = runner === "codex" ? codexHooks : hookReport;
  if (!rep) return;
  const label = runner === "codex" ? "Codex" : "Claude Code";
  const n = events ? events.length : rep.missing;
  const which = events
    ? `the <b>${esc(hookNameFor(events[0], rep))}</b> entry`
    : `${n} entr${n === 1 ? "y" : "ies"}`;
  // No path here: it is already on screen in the dialog behind this one, and
  // `.asktext code` is a block, so an inline one breaks the sentence in half.
  const hook = events ? hookNameFor(events[0], rep) : "";
  const body = events
    ? `${label} will run this on every <b>${esc(hook)}</b>:` +
      `<code>${esc(installedCommandFor(events[0], rep))}</code>` +
      "It joins anything already registered under that hook rather than replacing it."
    : `Atrium registers ${which}, one per hook, each running this binary with the ` +
      "name of the event. Anything already registered is left alone.";

  if (!await confirmUser(events ? `wire ${hook}?` : "wire these hooks?",
    body +
    "<br><br>A copy of your settings is kept first. Sessions already running keep their old " +
    "settings until they are restarted.",
    n === 1 ? "wire it" : "wire them")) return;
  try {
    const res = await api("/v1/hooks/install", {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ events: events || [], runner: runner || "" })
    });
    // Codex will not run a hook it has not been shown, so saying "wired" and
    // stopping would be a button that reports success and changes nothing.
    const next = runner === "codex" && codexHooks && codexHooks.trust
      ? " " + codexHooks.trust
      : "";
    toast("hooks wired", (res.backup
      ? "the old settings were copied to " + res.backup
      : "the file was created") + next);
  } catch (e) {
    toast("nothing was changed", e.message);
  }
  // The dialog and the chip behind it both read the same report.
  await loadHooks();
  renderHooks();
  renderRunners();
}

// The manual route: `hook install` commands to run, NOT the hook command that
// belongs inside settings.json. In a block with a copy button, the two look
// alike, and running the wrong one posts a single activity event and changes
// nothing.
//
// One event named shows only that step, for a row's own "manually".
function showHookSteps(only) {
  stepsFilter = only || "";
  document.getElementById("steps").showModal();
  paintHookSteps();
}

// Which hook the open steps dialog is about, empty for all of them. Kept so
// the dialog can be redrawn when settings.json changes underneath it.
let stepsFilter = "";

// Redraws the steps, dropping the ones already done.
//
// A command you have run is not a step any more. Leaving it on the list means
// working out for yourself which of five you still owe, which is the job the
// list was supposed to be doing. When the last one goes the dialog has nothing
// left to say, so it closes.
function paintHookSteps() {
  const dlg = document.getElementById("steps");
  const rep = hookReport;
  if (!dlg.open || !rep) return;

  const wanted = (rep.hooks || [])
    .filter(h => !stepsFilter || h.event === stepsFilter)
    .filter(h => !h.installed || h.stale);

  if (!wanted.length) {
    dlg.close();
    return;
  }
  renderHookSteps(rep, wanted);
}

function renderHookSteps(rep, wanted) {
  const only = stepsFilter;

  const steps = wanted.map(h => {
    const cmd = installCommand(rep, h.event);
    return `
    <div class="step">
      <div class="steptop">
        <b>${esc(h.hook)}</b>
        <span class="by">${esc(h.why)}</span>
        <span class="grow"></span>
        <button onclick='copyText(this, ${JSON.stringify(cmd).replace(/'/g, "&#39;")})'
          title="copy this command">copy</button>
      </div>
      <pre class="code">${esc(cmd)}</pre>
    </div>`;
  }).join("");

  // All of them at once, for anyone who would rather run one line.
  const all = only ? "" : `
    <div class="step">
      <div class="steptop">
        <b>all of them</b>
        <span class="by">one command, same result as the button</span>
        <span class="grow"></span>
        <button onclick='copyText(this, ${JSON.stringify(installCommand(rep, "")).replace(/'/g, "&#39;")})'
          title="copy this command">copy</button>
      </div>
      <pre class="code">${esc(installCommand(rep, ""))}</pre>
    </div>`;

  document.getElementById("steps-path").textContent = rep.path;
  document.getElementById("steps-copy").onclick = e => copyText(e.target, rep.path);
  setHTML(document.getElementById("steps-list"), all + steps);
}

// The command that installs one hook, or all of them when event is empty.
//
// Built from the same absolute path the entries themselves carry, which is the
// binary this daemon is running. Typing `atrium` would depend on a PATH atrium
// has no say over, and the whole point of the path being absolute is that it
// works wherever it is pasted.
function installCommand(rep, event) {
  const h = (rep.hooks || [])[0];
  // `want` is `<exe> hook --event x`, so everything before " hook " is the
  // binary, quotes and all.
  const exe = h ? h.want.split(" hook ")[0] : "atrium";
  return exe + " hook install" + (event ? " --event " + event : "");
}

// Copies one command and says so on the button that was pressed. A toast for
// this would be a notification about a click you just made.
async function copyText(btn, text) {
  try {
    await navigator.clipboard.writeText(text);
    btn.textContent = "copied";
  } catch (e) {
    btn.textContent = "could not copy";
  }
  setTimeout(() => { btn.textContent = "copy"; }, 1400);
}

// Opens the runner form pre-filled from what was found. The command and the
// path are known; anything that was not is left empty with the reason shown,
// since a guessed flag produces a runner that fails on first use.
function addDiscovered(c) {
  editHarness("");
  document.getElementById("h-id").value = c.id;
  document.getElementById("h-id").disabled = false;
  document.getElementById("h-label").value = c.label;
  document.getElementById("h-cmd").value = c.cmd;
  document.getElementById("h-args").value = (c.args || []).join("\n");
  document.getElementById("h-resume").value = (c.resume_args || []).join("\n");
  document.getElementById("h-exit").value = (c.exit_keys || []).join("\n");
  document.getElementById("h-prepare").value = "";
  document.getElementById("h-notes").value = c.confirm || "";
  if (c.confirm) toast("needs confirming", c.confirm);
}

function linesToList(v) {
  return (v || "").split("\n").map(s => s.trim()).filter(Boolean);
}

function editHarness(id) {
  const h = allHarnesses.find(x => x.id === id) || {
    id: "", label: "", cmd: "", args: [], resume_args: [], cwd: "", env: {},
    launch_mode: "window", rules_source: "", notes: "", enabled: false
  };
  document.getElementById("h-heading").textContent = id ? "edit " + h.label : "add a runner";
  document.getElementById("h-id").value = h.id;
  document.getElementById("h-id").disabled = !!id;
  document.getElementById("h-label").value = h.label || "";
  document.getElementById("h-cmd").value = h.cmd || "";
  document.getElementById("h-args").value = (h.args || []).join("\n");
  document.getElementById("h-resume").value = (h.resume_args || []).join("\n");
  document.getElementById("h-exit").value = (h.exit_keys || []).join("\n");
  document.getElementById("h-prepare").value = h.prepare || "";
  document.getElementById("h-cwd").value = h.cwd || "";
  document.getElementById("h-env").value =
    Object.entries(h.env || {}).map(([k, v]) => `${k}=${v}`).join("\n");
  document.getElementById("h-rules").value = h.rules_source || "";
  document.getElementById("h-notes").value = h.notes || "";
  document.querySelectorAll("#h-mode button").forEach(b =>
    b.classList.toggle("on", b.dataset.v === (h.launch_mode || "window")));
  document.getElementById("h-delete").style.display = id ? "" : "none";
  document.getElementById("harness").dataset.editing = h.id || "";
  document.getElementById("harness").dataset.enabled = h.enabled ? "1" : "0";
  document.getElementById("harness").showModal();
}

document.querySelectorAll("#h-mode button").forEach(b => b.addEventListener("click", () => {
  document.querySelectorAll("#h-mode button").forEach(x => x.classList.toggle("on", x === b));
}));

function harnessFromForm() {
  const env = {};
  linesToList(document.getElementById("h-env").value).forEach(line => {
    const i = line.indexOf("=");
    if (i > 0) env[line.slice(0, i).trim()] = line.slice(i + 1).trim();
  });
  const dlg = document.getElementById("harness");
  return {
    id: document.getElementById("h-id").value.trim(),
    label: document.getElementById("h-label").value.trim(),
    enabled: dlg.dataset.enabled === "1",
    cmd: document.getElementById("h-cmd").value.trim(),
    args: linesToList(document.getElementById("h-args").value),
    resume_args: linesToList(document.getElementById("h-resume").value),
    exit_keys: linesToList(document.getElementById("h-exit").value),
    prepare: document.getElementById("h-prepare").value.trim(),
    cwd: document.getElementById("h-cwd").value.trim(),
    env,
    launch_mode: document.querySelector("#h-mode button.on").dataset.v,
    rules_source: document.getElementById("h-rules").value,
    notes: document.getElementById("h-notes").value.trim()
  };
}

async function saveHarness() {
  const h = harnessFromForm();
  if (!h.id) { tellUser("atrium", "give it an id"); return; }
  try {
    await api(`/v1/harnesses/${encodeURIComponent(h.id)}`, {
      method: "PUT", headers: { "Content-Type": "application/json" }, body: JSON.stringify(h)
    });
  } catch (e) { tellUser("atrium", e.message); return; }
  document.getElementById("harness").close();
  refresh();
}

async function deleteHarness() {
  const id = document.getElementById("harness").dataset.editing;
  if (!id) return;
  if (!await confirmUser("delete this runner?",
    `<b>${esc(id)}</b> is removed from the list. Anything it already started keeps running.`,
    "delete it")) return;
  try { await api(`/v1/harnesses/${encodeURIComponent(id)}`, { method: "DELETE" }); }
  catch (e) { tellUser("atrium", e.message); return; }
  document.getElementById("harness").close();
  refresh();
}

async function toggleHarness(id) {
  const h = allHarnesses.find(x => x.id === id);
  if (!h) return;
  try {
    await api(`/v1/harnesses/${encodeURIComponent(id)}`, {
      method: "PUT", headers: { "Content-Type": "application/json" },
      body: JSON.stringify(Object.assign({}, h, { enabled: !h.enabled }))
    });
  } catch (e) { tellUser("atrium", e.message); }
  refresh();
}

// Called with an id from the runners tab, or with nothing from the header's
// new agent button, in which case you pick the runner here.
// `ontoTask` starts onto a card that already exists, which is what resuming a
// finished card means. Without it the runner would come back as a second card
// and the history would be split across two.
// Start an offered card: the launch dialog with everything the source already
// knew filled in.
//
// The card is the target rather than a template, so pressing start does not
// make a second card. The item keeps its identifier, its link and its history,
// and the session that runs attaches to the card that says what it is for.
async function startOffered(id) {
  const t = (lastTasks || []).find(x => x.id === id);
  if (!t) return;
  await openLaunch(t.runner || null, "", t.worktree, t.id, {
    title: t.display_title || t.title || "",
    why: t.why || "",
    prompt: t.prompt || "",
    tags: t.tags || [],
    // The link the source raised it with, put in the box ready to recognise.
    // An intake item usually has no directory, and the recogniser is what
    // turns its url into one.
    url: t.url || ""
  });
}

// `where` is the destination for whatever this starts: "here" for the
// terminals tab, "window" for its own window. Held on the dialog rather than
// passed to the submit, because the decision is made in the menu that opened
// the form and answered by the code that closes it.
let launchWhere = "here";

// What the last recognised url worked out, held for `doLaunch`.
//
// The fields in it are the ones the dialog has no box for: the repo, the org,
// the host, the branch, the window and the theme. They are not hidden inputs
// because they are not things to type. They are what a launcher knew, and
// atrium derives none of them.
//
// CLEARED EVERY TIME THE DIALOG OPENS. Left over, a stale resolution would put
// the previous card's repo and branch onto this one, and there is no field on
// screen where that would be visible enough to correct.
let launchResolved = null;

function clearResolved(url) {
  launchResolved = null;
  const box = document.getElementById("l-url");
  if (box) box.value = url || "";
  const note = document.getElementById("l-link-note");
  if (note) {
    note.textContent = "Optional. A recogniser turns it into the fields below, " +
      "and nothing starts until you press start.";
    note.classList.remove("warn");
  }
}

// Paste a link, fill the form in.
//
// IT STARTS NOTHING. What comes back is text in boxes, which is the whole
// separation the design rests on: recognising is not deciding, and the button
// that decides is the one that was already there.
async function recogniseLink() {
  const url = document.getElementById("l-url").value.trim();
  const note = document.getElementById("l-link-note");
  const btn = document.getElementById("l-recognise");
  if (!url) return;
  btn.disabled = true;
  note.classList.remove("warn");
  note.textContent = "recognising...";
  let got;
  try {
    got = await api("/v1/recognise", {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ url })
    });
  } catch (e) {
    btn.disabled = false;
    note.classList.add("warn");
    // A url nothing matches is a 404 and reads as one. Not a failure: this
    // atrium has no row for that shape yet, and the fix is to write one.
    note.textContent = /404|no recogniser/i.test(e.message)
      ? "nothing here knows what that is yet. add a row for it under runners, recognisers."
      : e.message;
    return;
  }
  btn.disabled = false;
  launchResolved = got;

  // Only over what the recogniser had something to say about. A directory or a
  // title already typed is not overwritten by an empty template, because
  // clearing what somebody just typed is the one thing that makes a form feel
  // hostile.
  const put = (id, v) => { if (v) document.getElementById(id).value = v; };
  put("l-cwd", got.cwd);
  put("l-title", got.title);
  put("l-prompt", got.prompt);
  put("l-tags", (got.tags || []).join(", "));

  const bits = [got.label || got.recogniser];
  if (got.fetch_error) bits.push("the fetch failed, so anything it would have added " +
    "is missing: " + got.fetch_error);
  if (got.problem) bits.push(got.problem);
  note.textContent = bits.join(". ");
  // A missing worktree, a hole in a template or a broken fetch all read the
  // same way here: something to go and do before pressing start.
  note.classList.toggle("warn", !!(got.problem || got.fetch_error));
}

async function openLaunch(id, resume, cwd, ontoTask, prefill, where) {
  launchWhere = where === "window" ? "window" : "here";
  if (!allHarnesses.length) {
    try { allHarnesses = (await api("/v1/harnesses")).harnesses || []; } catch (e) {}
  }
  const enabled = allHarnesses.filter(h => h.enabled);
  if (!enabled.length) {
    tellUser("atrium", "no runner is enabled yet. turn one on in the runners tab.");
    goRunners("runners");
    return;
  }
  const picking = !id;
  const h = allHarnesses.find(x => x.id === id) || enabled[0];

  document.getElementById("l-pick-field").hidden = !picking;
  document.getElementById("l-harness").innerHTML = enabled.map(x =>
    `<option value="${esc(x.id)}"${x.id === h.id ? " selected" : ""}>${esc(x.label)}</option>`).join("");

  launchTarget = { harness: h.id, resume: resume || "", task_id: ontoTask || "" };
  document.getElementById("l-heading").textContent =
    resume ? "resume " + (h.label || h.id) : picking ? "new agent" : "start " + (h.label || h.id);
  const pre = prefill || {};
  document.getElementById("l-cwd").value = cwd || h.cwd || "";
  document.getElementById("l-title").value = pre.title || "";
  document.getElementById("l-why").value = pre.why || "";
  document.getElementById("l-prompt").value = pre.prompt || "";
  document.getElementById("l-tags").value = (pre.tags || []).join(", ");

  // Every dialog starts with nothing recognised. Left over from the last one,
  // a stale resolution would put the previous card's repo, branch and window
  // onto this one, and none of those are fields you can see to correct.
  clearResolved(pre.url || "");
  // Resuming is the one case with nothing to recognise: that conversation
  // already knows what it is for, and the directory it is in is not a choice.
  //
  // An offered card is the OPPOSITE case and the third place the design names
  // a url gets pasted. An intake item arrives carrying a link and usually no
  // directory, so its url is put in the box already, and one press turns "here
  // is a thing" into "here is a card ready to start".
  document.getElementById("l-link-field").hidden = !!resume;

  // A resumed conversation already has its instruction, and the daemon refuses
  // both. Hidden rather than disabled, so there is nothing to wonder about.
  document.getElementById("l-prompt-field").hidden = !!resume;

  // Resuming is opt-out. Where there is a conversation to pick up, continuing
  // it is nearly always the intent, and starting fresh by accident discards
  // everything the session worked out.
  const rf = document.getElementById("l-resume-field");
  rf.hidden = !resume;
  if (resume) {
    document.getElementById("l-resume-on").checked = true;
    document.getElementById("l-resume-note").textContent =
      "unchecked, this starts a fresh conversation in the same directory.";
  }

  renderRecentDirs();
  document.getElementById("launch").showModal();
  document.getElementById("l-cwd").focus();
}

// Directories that already have cards, most recently active first, so a launch
// into somewhere used before is one click and browsing is left for new ones.
async function renderRecentDirs() {
  let tasks = [];
  try { tasks = (await api("/v1/tasks")).tasks || []; } catch (e) { return; }
  tasks.sort((a, b) => (a.idle_seconds || 0) - (b.idle_seconds || 0));

  const seen = new Set();
  const dirs = [];
  for (const t of tasks) {
    const d = t.worktree;
    if (!d || seen.has(d)) continue;
    seen.add(d);
    dirs.push(d);
    if (dirs.length >= 6) break;
  }
  const host = document.getElementById("l-recent");
  // The whole path, not the leaf. Three worktrees of the same repo all end in
  // the same word, so a row of leaf names is a row of identical buttons.
  host.innerHTML = dirs.map(d =>
    `<button class="quickdir" data-path="${esc(d)}" title="${esc(d)}">${esc(d)}</button>`).join("");
  // Same as the browser: HTML escaping does nothing about an apostrophe inside
  // a JavaScript string literal.
  host.querySelectorAll(".quickdir").forEach(b => b.onclick = () => {
    document.getElementById("l-cwd").value = b.dataset.path;
  });
}

async function doLaunch() {
  if (!launchTarget) return;
  const picker = document.getElementById("l-harness");
  const resumeOff = !document.getElementById("l-resume-field").hidden &&
    !document.getElementById("l-resume-on").checked;
  const body = Object.assign({}, launchTarget, {
    harness: document.getElementById("l-pick-field").hidden ? launchTarget.harness : picker.value,
    // No resume id means no resume arguments, so the daemon starts fresh.
    resume: resumeOff ? "" : launchTarget.resume,
    cwd: document.getElementById("l-cwd").value.trim(),
    title: document.getElementById("l-title").value.trim(),
    why: document.getElementById("l-why").value.trim(),
    // Split on commas and never on spaces, the same rule everywhere else: a
    // tag with a space in it is one tag.
    tags: document.getElementById("l-tags").value
      .split(",").map(x => x.trim()).filter(Boolean),
    // Empty while resuming, since the field is hidden and the daemon refuses
    // a prompt and a resume together.
    prompt: resumeOff || !launchTarget.resume
      ? document.getElementById("l-prompt").value.trim() : ""
  });
  // WHAT THE RECOGNISER KNEW, sent only when there was one. The daemon never
  // overwrites a card field with an empty value, so a plain launch says
  // nothing about the repo rather than blanking one a session reported for
  // itself.
  if (launchResolved) {
    Object.assign(body, {
      repo: launchResolved.repo || "",
      org: launchResolved.org || "",
      host: launchResolved.host || "",
      branch: launchResolved.branch || "",
      window: launchResolved.window || "",
      theme: launchResolved.theme || "",
      source_kind: launchResolved.kind || "",
      source_url: launchResolved.url || ""
    });
  }
  let task;
  try {
    task = await api("/v1/launch", {
      method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body)
    });
  } catch (e) { tellUser("could not start it", e.message); return; }
  document.getElementById("launch").close();

  // Straight to the terminal you just started. Landing on the board instead
  // means finding the new card and clicking attach, which is two steps to get
  // where you were already going.
  if (task && task.supervised) {
    if (launchWhere === "window") {
      // Its own window, which is where you said to put it. The board does not
      // attach first: it would open the pane, then detach it a moment later,
      // and the flicker reads as something going wrong.
      popOutTask(task.id);
      return;
    }
    switchView("terms");
    openTerm(task);
    return;
  }
  switchView("board");
}

// The page is a flex column that fills the window, so nothing measures the
// header any more. What is left is telling the terminal to re-fit, since
// xterm.js sizes itself in characters and only finds out its box changed if
// something says so.
addEventListener("resize", onTermResize);
// The header changes height when a badge appears or the nav wraps, neither of
// which fires a window resize, and both of which change the terminal's box.
if (window.ResizeObserver) {
  const header = document.querySelector("header");
  if (header) {
    new ResizeObserver(() => onTermResize()).observe(header);
  }
  // The pane itself, so switching views or opening a permission strip inside
  // it re-fits without waiting for a window resize that will never come.
  const screen = document.getElementById("t-screen");
  if (screen) new ResizeObserver(() => onTermResize()).observe(screen);
}

