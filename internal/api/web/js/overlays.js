let overlays = [];

async function loadOverlays() {
  try { overlays = (await api("/v1/overlays")).overlays || []; } catch (e) { return; }
}

// A SHARE THAT STOPS ON ITS OWN HAS TO SAY SO.
//
// The one state change in the overlay panel that nobody pressed a button for.
// Starting and stopping are visible in the panel because you were looking at
// it. A share ending by itself is not: the listener drops, `err` is set, and
// the gear is not open.
//
// It matters because the address is the thing you handed to somebody else. A
// dead public share is a link that has stopped working for a person who is not
// in the room, and they have no way to tell you except by telling you.
//
// Only the running-to-stopped edge, and only with a reason attached. Pressing
// `stop sharing` clears the error, so this cannot fire for something you did.
function noticeSharesThatDied(before, after) {
  // Nothing to compare against on the first event of a page's life. A board
  // that opens onto a share that stopped an hour ago should not announce it as
  // news: the panel says so, which is the right weight for old information.
  if (!before || !before.length) return;
  (after || []).forEach(now => {
    const was = before.find(o => o.kind === now.kind);
    if (!was || !was.running || now.running) return;
    if (!now.err) return;
    const what = now.label || now.kind;
    // NO `goTo`. Every other alert on the board goes to a VIEW, and the gear
    // is a dialog rather than one, so handing `settings` to `switchView` would
    // be a click that appears to do nothing. The message says where to look
    // instead, which is one instruction rather than a broken promise.
    alerting.notify(`${what} stopped sharing`, now.err, "", "", "overlay:" + now.kind, "", "");
    toast(`${what} stopped sharing`, now.err + ". open the gear, accessing atrium, to see why");
  });
}

// Atrium's configuration, out to a file and back.
//
// A DOWNLOAD RATHER THAN A FETCH, so the browser's own save dialog puts it
// where the operator wants it. The daemon already names it and dates it, and a
// file called `export` in a repository is one nobody can identify in six
// months.
//
// A refusal comes back as a 400 with a sentence, because the export stops when
// something in the configuration looks like a credential. That sentence is the
// whole product of the failure, so it is shown rather than summarised.
async function exportConfig() {
  const said = document.getElementById("config-said");
  said.className = "hintline";
  said.textContent = "asking…";
  try {
    // Fetched rather than navigated to, so a refusal can be read. A plain link
    // would show the operator a page of JSON error text in a new tab.
    const res = await fetch("/v1/config/export");
    if (!res.ok) {
      let why = await res.text();
      try { why = (JSON.parse(why) || {}).error || why; } catch (e) {}
      said.textContent = why;
      said.className = "hintline bad";
      return;
    }
    const blob = await res.blob();
    const named = (res.headers.get("Content-Disposition") || "").split("filename=")[1];
    const name = named ? named.replace(/"/g, "") : "atrium-config.json";
    const a = document.createElement("a");
    a.href = URL.createObjectURL(blob);
    a.download = name;
    a.click();
    URL.revokeObjectURL(a.href);
    said.textContent = "downloaded as " + name + " to this browser's downloads folder. no credentials are in it.";
    said.className = "hintline ok";
  } catch (e) {
    said.textContent = e.message;
    said.className = "hintline bad";
  }
}

// Reading one back. A DRY RUN FIRST, always.
//
// The list of what would change is the question somebody restoring a machine
// actually has, and answering it by doing it is not an answer. Applying is a
// second press, made after reading it.
async function importConfig(input) {
  const said = document.getElementById("config-said");
  const file = input.files && input.files[0];
  // Cleared so choosing the same file twice fires the event both times.
  input.value = "";
  if (!file) return;

  let text;
  try {
    text = await file.text();
  } catch (e) {
    said.textContent = e.message;
    said.className = "hintline bad";
    return;
  }

  let dry;
  try {
    dry = await api("/v1/config/import", {
      method: "POST", headers: { "Content-Type": "application/json" }, body: text
    });
  } catch (e) {
    said.textContent = e.message;
    said.className = "hintline bad";
    return;
  }

  const changes = dry.changes || [];
  const doing = changes.filter(c => c.action !== "keep");
  if (!doing.length) {
    tellUser("nothing to change", esc(dry.note || "this machine already matches that file."));
    said.textContent = "nothing to change.";
    said.className = "hintline";
    return;
  }
  const rows = changes.map(c =>
    `<div class="sharerow"><div class="grow"><b>${esc(c.action)}</b> ${esc(c.kind)}
       <code>${esc(c.name)}</code>${c.why ? `<span class="hintline">${esc(c.why)}</span>` : ""}
     </div></div>`).join("");

  const go = await askUser({
    title: "apply this configuration?",
    body: `${doing.length} thing(s) would change. Anything already set here is kept unless you
      choose to overwrite it.<br><br>${esc(dry.note || "")}${rows}`,
    choices: [
      { value: "safe", label: "add what is missing, keep what is here" },
      { value: "force", label: "overwrite what is already set too" }
    ],
    buttons: [{ label: "cancel", value: null }, { label: "apply", value: true, style: "go" }]
  });
  if (!go) return;

  try {
    const res = await api("/v1/config/import?apply=1" + (go === "force" ? "&force=1" : ""), {
      method: "POST", headers: { "Content-Type": "application/json" }, body: text
    });
    said.textContent = (res.changes || []).filter(c => c.action !== "keep").length +
      " applied. " + (res.note || "");
    said.className = "hintline ok";
    // Everything on this screen may have moved.
    loadOverlays();
    paintSettings();
    refresh();
  } catch (e) {
    said.textContent = e.message;
    said.className = "hintline bad";
  }
}

function setHookNag(on) {
  skipConfirm(HOOK_NAG, on);
  renderHooks();
  renderRunners();
}

// `view` is the tab a strip of pills belongs to. The settings dialog passes the tab it is set to, and anything else falls
// back to the tab on screen.
function setGroupMode(mode, view) {
  view = view || settingsGroupView();
  const p = groupingPrefs(view);
  const now = p.on ? (usesCode(p) ? "code" : (p.mode || "project")) : "off";
  if (now === mode) return;
  setGrouping(mode === "off" ? { on: false } : { on: true, mode, picked: true }, view);
  paintGrouping();
  paintGroupSegs();
}

function resetGrouping() {
  const view = settingsGroupView();
  const wasOff = !groupingPrefs(view).on;
  setGrouping({ by: "", order: "", orderBy: "", picked: false, mode: "project" }, view);
  paintGrouping();
  // Said differently when grouping is switched off, because otherwise this
  // button reports success for something with no visible effect and reads as
  // broken. It DID reset the rule. There is just nothing using it.
  toast("grouping rule reset",
    wasOff
      ? "back to reading a project out of the worktree path. grouping is off, so nothing moved"
      : "back to reading a project out of the worktree path");
}

// Saved on blur rather than on every keystroke, so a half-typed function is
// not compiled and reported as broken while it is being written.
document.getElementById("s-group-mode").addEventListener("change", e => setGroupMode(e.target.value));
document.getElementById("s-group-orderby").addEventListener("change", e => {
  setGrouping({ orderBy: e.target.value }, settingsGroupView());
  paintGrouping();
});
document.getElementById("s-group-by").addEventListener("change", e => {
  setGrouping({ by: e.target.value === DEFAULT_GROUP_BY ? "" : e.target.value }, settingsGroupView());
});
document.getElementById("s-group-order").addEventListener("change", e => {
  setGrouping({ order: e.target.value === DEFAULT_GROUP_ORDER ? "" : e.target.value }, settingsGroupView());
});

