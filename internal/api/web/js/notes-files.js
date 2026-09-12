// ── notes to self ───────────────────────────────────────
//
// Held by the daemon rather than the browser, because a note is about the work
// and not about this screen: it should be there from another browser and after
// a restart, like the card's theme and its bell.
//
// Saved as you stop typing rather than on a button, since a scratch pad you
// can lose by closing a dialog is a scratch pad you stop using.

let noteTimer = null;

function paintNote() {
  const box = document.getElementById("d-note");
  box.value = (current && current.note) || "";
  noteState("");
  document.getElementById("d-note-send").disabled = !box.value.trim();
  document.getElementById("d-note-clear").disabled = !box.value.trim();
}

function noteState(text) {
  document.getElementById("d-note-state").textContent = text;
}

document.getElementById("d-note").addEventListener("input", e => {
  const empty = !e.target.value.trim();
  document.getElementById("d-note-send").disabled = empty;
  document.getElementById("d-note-clear").disabled = empty;
  noteState("saving...");
  clearTimeout(noteTimer);
  noteTimer = setTimeout(async () => {
    if (!current) return;
    // patchTask rather than patch: the wrapper repaints the board, and doing
    // that every time you pause typing is a lot of work to show a card whose
    // note nothing else displays.
    try {
      await patchTask(current.id, { note: e.target.value });
      current.note = e.target.value;
      noteState("saved");
    } catch (err) { noteState("not saved: " + err.message); }
  }, 500);
});

// One message, not one per line. The whole reason a note exists is that three
// things thought of during a long turn should arrive as one instruction.
async function sendNote() {
  if (!current) return;
  const box = document.getElementById("d-note");
  if (!box.value.trim()) return;

  // Any keystrokes still inside the debounce go first, or the send would post
  // what was saved rather than what is on screen.
  clearTimeout(noteTimer);
  try { await patchTask(current.id, { note: box.value }); }
  catch (e) { noteState("not saved, so not sent: " + e.message); return; }

  let res;
  try {
    res = await api(`/v1/tasks/${current.id}/note/send`, { method: "POST" });
  } catch (e) { noteState("did not send: " + e.message); return; }

  box.value = "";
  document.getElementById("d-note-send").disabled = true;
  document.getElementById("d-note-clear").disabled = true;
  // The same two words the message box uses, because they are the same two
  // promises: typed means it has already landed, queued means it has not and
  // will not until the session's next tool call or the end of its turn.
  toast("sent", res.delivered === "terminal"
    ? "typed into its terminal."
    : "queued. it arrives on the session's next tool call or at the end of its turn.");
  refresh();
}

async function clearNote() {
  if (!current) return;
  if (!await confirmUser("clear this note?",
    "What you wrote is thrown away. It has not been sent anywhere.", "clear it")) return;
  clearTimeout(noteTimer);
  try { await patchTask(current.id, { note: "" }); } catch (e) { noteState(e.message); return; }
  current.note = "";
  document.getElementById("d-note").value = "";
  document.getElementById("d-note-send").disabled = true;
  document.getElementById("d-note-clear").disabled = true;
  noteState("");
}

// ── getting a file out of a card ────────────────────────
//
// The counterpart to paste and drop. Those put bytes in; this takes them back
// out, which is the half that has no workaround at all once atrium is not on
// the machine you are sitting at.
//
// Every path comes from the server, including where "up" goes. The board never
// does path arithmetic, because that is where a traversal would come from if
// one were going to.

let filesAt = null;
let filesParent = "";

function toggleFiles() {
  const box = document.getElementById("d-files");
  const open = box.hidden;
  box.hidden = !open;
  document.getElementById("d-files-toggle").classList.toggle("open", open);
  if (open) loadFiles("");
}

// The attached session's directory, from the terminal bar.
//
// The card dialog with the file fold already open, rather than a second
// browser built into the pane. There is one containment check behind this,
// `internal/safepath`, and one listing endpoint, and a second front end onto
// them is a second place for the rules about what may be reached to be
// slightly different.
//
// It matters most in a popped-out window, and it works there for the same
// reason the board does: this is the daemon's own HTTP, so whatever carries
// the board over an overlay carries the file listing and the download with it.
// The machine being remote changes nothing about the path this takes.
// The attached session's directory, as a drawer in the pane.
//
// Opens over the terminal rather than beside it: the pane is already narrow in
// a popped-out window, and a split that leaves both halves too small to use is
// worse than one that swaps. The terminal keeps running underneath and its
// scrollback is untouched, so closing the drawer puts you back where you were.
function openTermFiles() {
  const box = document.getElementById("t-files-panel");
  if (!box) return;
  // CLOSING never needs a card. It used to share the `!termTask` guard with
  // opening, so a drawer left open while the terminal went away could not be
  // shut: the close button called a function that returned before touching
  // anything. Opening still needs one, since there is nothing to list without
  // a card.
  if (!box.hidden) { setTermFiles(false); return; }
  if (!termTask) return;
  setTermFiles(true);
  loadFiles("", termFilesCtx());
}

function setTermFiles(open) {
  const box = document.getElementById("t-files-panel");
  if (!box) return;
  box.hidden = !open;
  const btn = document.getElementById("t-files");
  if (btn) btn.classList.toggle("go", open);
  // The terminal's box changed, and xterm measures in characters, so it has to
  // be told. Without this the pane comes back with rows sized for the drawer.
  onTermResize();
}

// Which browser is being driven, so one implementation serves both.
//
// The alternative was a second copy of the listing, the walk and the download
// link for the pane, which is exactly the drift the containment rules must not
// have: two front ends onto one endpoint that disagree about what may be
// reached is how a check gets fixed in one of them.
function termFilesCtx() {
  return {
    id: termTask && termTask.id, at: "term",
    where: "t-files-where", up: "t-files-up", zip: "t-files-zip",
    del: "t-files-del", list: "t-files-list", note: "t-files-note"
  };
}
function dialogFilesCtx() {
  return {
    id: current && current.id, at: "dialog",
    where: "d-files-where", up: "d-files-up", zip: "d-files-zip",
    del: "d-files-del", list: "d-files-list", note: "d-files-note"
  };
}

// Where each browser is standing. Two of them, because the dialog and the pane
// can be open on different directories and one shared variable made walking in
// one of them move the other.
const filesPos = { dialog: { at: "", parent: "" }, term: { at: "", parent: "" } };

async function loadFiles(path, ctx) {
  ctx = ctx || dialogFilesCtx();
  if (!ctx.id) return;
  const pos = filesPos[ctx.at];
  let res;
  try {
    res = await api(`/v1/tasks/${ctx.id}/files/list` +
      (path ? `?path=${encodeURIComponent(path)}` : ""));
  } catch (e) {
    setHTML(document.getElementById(ctx.list),
      `<div class="empty">${esc(e.message)}</div>`);
    return;
  }
  pos.at = res.path;
  pos.parent = res.parent || "";
  // Kept for the dialog's own callers, which still read the old names.
  if (ctx.at === "dialog") { filesAt = res.path; filesParent = pos.parent; }

  paintCrumbs(ctx, res);
  // Disabled rather than hidden at the top, so the control does not move
  // under the cursor as you walk up. Kept alongside the breadcrumb: one step
  // back is the commonest move and it should not need aiming at a word.
  const up = document.getElementById(ctx.up);
  if (up) up.disabled = !pos.parent;
  document.getElementById(ctx.note).textContent = res.truncated
    ? `showing the first ${(res.entries || []).length}. this directory has more.`
    : "";
  // The download button is painted from the selection, not from the directory.
  // See paintPicked.

  const rows = res.entries || [];
  if (!rows.length) {
    setHTML(document.getElementById(ctx.list),
      `<div class="empty">nothing here.</div>`);
    return;
  }
  // Sorted by the daemon, folders first then by name, so the board does not
  // hold a second opinion about the order.
  setHTML(document.getElementById(ctx.list), rows.map(e => `
    <div class="frow${e.dir ? " dir" : ""}" data-path="${esc(e.path)}"${
        e.dir ? ` data-dir="${esc(e.path)}"` : ""}>
      <input type="checkbox" class="fpick" data-path="${esc(e.path)}"
        title="pick this for a download">
      <span class="fname">${fileIcon(e)}${esc(e.name)}${e.dir ? "/" : ""}</span>
      <span class="fsize">${e.dir ? "" : esc(bytes(e.size))}</span>
      <span class="fwhen">${esc(firstSeen(e.mtime))}</span>
      <span class="facts">${e.dir ? "" : `
        ${editable(e.name) ? `<span class="chip edit" data-edit="${esc(e.path)}"
          title="in a box right here, in this browser. works from anywhere."
          >edit here</span>` : ""}
        <span class="chip open" data-open="${esc(e.path)}"
          title="in your own editor, on the machine this session runs on. over an overlay that
                 window opens there, not where you are sitting."
          >open there</span>
        <a class="chip" download href="/v1/tasks/${ctx.id}/files?path=${
           encodeURIComponent(e.path)}">get</a>`}</span>
    </div>`).join(""));

  // Wired rather than inlined: a filename with an apostrophe in it breaks an
  // onclick attribute, and HTML escaping does nothing about that.
  const host = document.getElementById(ctx.list);
  host.querySelectorAll(".frow.dir").forEach(el => {
    el.onclick = () => loadFiles(el.dataset.dir, ctx);
  });
  host.querySelectorAll(".chip.open").forEach(el => {
    el.onclick = ev => { ev.stopPropagation(); openOnDaemon(ctx.id, el.dataset.open); };
  });
  host.querySelectorAll(".chip.edit").forEach(el => {
    el.onclick = ev => { ev.stopPropagation(); openEditor(ctx.id, el.dataset.edit); };
  });
  // A tick must not also walk into the directory it is on.
  host.querySelectorAll(".fpick").forEach(el => {
    el.onclick = ev => ev.stopPropagation();
    el.onchange = () => paintPicked(ctx);
  });
  paintPicked(ctx);
}

// The download button, painted from what is ticked.
//
// `get all` was the wrong shape and said so out loud. "Everything under here"
// is a guess that is usually wrong by a build directory, and it made the case
// anybody actually has, four files out of two hundred, unreachable. So the
// button appears when something is picked, says how many, and is a plain link
// so the browser streams it and a large selection never exists in the page.
//
// Nothing ticked means no button rather than a disabled one. There is no
// sensible default download and offering one was the whole mistake.
function paintPicked(ctx) {
  const host = document.getElementById(ctx.list);
  const zip = document.getElementById(ctx.zip);
  if (!host || !zip) return;
  const picked = [...host.querySelectorAll(".fpick")]
    .filter(c => c.checked).map(c => c.dataset.path);

  const del = document.getElementById(ctx.del);
  zip.hidden = picked.length === 0;
  if (del) {
    del.hidden = picked.length === 0;
    del.onclick = () => deletePicked(ctx, picked);
    del.textContent = picked.length === 1 ? "delete 1" : `delete ${picked.length}`;
  }
  if (!picked.length) return;

  // ONE file downloads as itself. Several become a zip.
  //
  // Not two buttons. A zip holding a single file is a chore to open for no
  // reason, and offering both shapes every time turns every download into a
  // decision that has an obvious right answer. The count already decides it.
  if (picked.length === 1) {
    zip.href = `/v1/tasks/${ctx.id}/files?path=${encodeURIComponent(picked[0])}`;
    zip.textContent = "download";
  } else {
    zip.href = `/v1/tasks/${ctx.id}/files/zip?` +
      picked.map(p => "path=" + encodeURIComponent(p)).join("&");
    zip.textContent = `download ${picked.length} as a zip`;
  }
  zip.title = picked.join("\n");
}

async function deletePicked(ctx, picked) {
  if (!picked.length) return;
  const names = picked.map(p => p.split(/[\\/]/).pop());
  if (!await confirmUser(
    picked.length === 1 ? `delete ${names[0]}?` : `delete ${picked.length} files?`,
    // Named, not counted. "Delete 4 files?" is a question nobody can check.
    `<code>${esc(names.join("\n"))}</code>This cannot be undone.`,
    "delete")) return;

  try {
    await api(`/v1/tasks/${ctx.id}/files?` +
      picked.map(p => "path=" + encodeURIComponent(p)).join("&"), { method: "DELETE" });
  } catch (e) {
    toast("could not delete", e.message);
    return;
  }
  toast(picked.length === 1 ? "deleted" : `deleted ${picked.length}`, names.join(", "));
  loadFiles(filesPos[ctx.at].at, ctx);
}

// Which files are worth opening in a text box.
//
// Extension only, and a short list. Guessing from content would mean reading
// the file to decide whether to offer to read it, and being wrong means an
// editor full of bytes that cannot survive a round trip: opening a PNG as text
// and saving it destroys it silently.
const EDITABLE = [
  "md", "txt", "markdown", "rst", "adoc", "log",
  "json", "yaml", "yml", "toml", "ini", "conf", "env", "csv", "tsv",
  "go", "js", "ts", "jsx", "tsx", "css", "html", "sh", "ps1", "py", "rb",
  "sql", "gitignore", "editorconfig", "mod", "sum"
];

function editable(name) {
  const bare = String(name || "").replace(/^\./, "");
  const ext = bare.includes(".") ? bare.split(".").pop().toLowerCase() : bare.toLowerCase();
  return EDITABLE.includes(ext);
}

// The path as steps you can press.
//
// An `up` button says nothing about where it goes, and getting back from three
// deep was three presses of it. A breadcrumb is the same information already
// on screen, made pressable.
//
// Trimmed to the card's own directory. Everything above the worktree is a path
// this browser refuses to serve anyway, and drawing `C:` as a step you could
// press would offer a walk that always ends in `403`.
function paintCrumbs(ctx, res) {
  const host = document.getElementById(ctx.where);
  if (!host) return;
  const root = String(res.root || "").replace(/\/+$/, "");
  const at = String(res.path || "").replace(/\/+$/, "");
  host.title = at;

  const rest = at.startsWith(root) ? at.slice(root.length).replace(/^\//, "") : "";
  const steps = rest ? rest.split("/") : [];
  // The card's own directory is named by its leaf rather than by its whole
  // path, which is already on the card and would be most of the width here.
  const parts = [{ label: root.split("/").filter(Boolean).pop() || root, path: root }];
  let walk = root;
  for (const s of steps) {
    walk += "/" + s;
    parts.push({ label: s, path: walk });
  }
  setHTML(host, parts.map((p, i) => {
    const last = i === parts.length - 1;
    return (i ? `<span class="sep">/</span>` : "") +
      `<span class="crumb${last ? " here" : ""}"${
        last ? "" : ` data-go="${esc(p.path)}"`}>${esc(p.label)}</span>`;
  }).join(""));
  host.querySelectorAll(".crumb[data-go]").forEach(el => {
    el.onclick = () => loadFiles(el.dataset.go, ctx);
  });
}

// What kind of thing a row is, as a mark.
//
// Drawn here rather than vendored. A file icon theme is several hundred SVGs
// for a set of extensions this board will never see, and the board has to work
// offline, which is the same reason xterm is vendored rather than fetched.
//
// Six shapes, not sixty. The question a glyph answers in a directory listing is
// "which of these is the code and which is the readme", and the color carries
// most of that: distinguishing `.ts` from `.tsx` at 11px is not something an
// icon can do anyway.
const FILE_KINDS = [
  { kind: "go", ext: ["go", "mod", "sum", "rs", "c", "h", "cs", "java", "py", "rb", "sh", "ps1", "js", "ts"] },
  { kind: "web", ext: ["html", "htm", "css", "svg", "jsx", "tsx", "vue"] },
  { kind: "doc", ext: ["md", "txt", "rst", "adoc", "log"] },
  { kind: "cfg", ext: ["json", "yaml", "yml", "toml", "ini", "conf", "env", "gitignore", "editorconfig"] },
  { kind: "img", ext: ["png", "jpg", "jpeg", "gif", "webp", "ico", "bmp"] }
];

function fileIcon(e) {
  if (e.dir) {
    return `<svg class="ficon" viewBox="0 0 16 16" width="13" height="13" fill="none"
      stroke="currentColor" stroke-width="1.4" stroke-linejoin="round">
      <path d="M1.8 12.8V3.6a.8.8 0 0 1 .8-.8h3.1l1.5 1.8h6a.8.8 0 0 1 .8.8v7.4a.8.8 0 0 1-.8.8H2.6a.8.8 0 0 1-.8-.8Z"/>
    </svg>`;
  }
  // The last dot, and a leading dot does not count: `.gitignore` is named
  // gitignore, not an empty name with an extension.
  const name = String(e.name || "");
  const bare = name.replace(/^\./, "");
  const ext = bare.includes(".") ? bare.split(".").pop().toLowerCase() : bare.toLowerCase();
  const found = FILE_KINDS.find(k => k.ext.includes(ext));
  const kind = found ? found.kind : "";
  // A page with a folded corner. The fold is what makes it read as a file at
  // this size rather than as a rectangle.
  return `<svg class="ficon ${kind}" viewBox="0 0 16 16" width="13" height="13" fill="none"
    stroke="currentColor" stroke-width="1.4" stroke-linejoin="round">
    <path d="M9.2 1.9H4a.8.8 0 0 0-.8.8v10.6a.8.8 0 0 0 .8.8h8a.8.8 0 0 0 .8-.8V5.5Z"/>
    <path d="M9.2 1.9v3.6h3.6"/>
  </svg>`;
}

// A file in a text box, and back to disk.
//
// Deliberately a textarea rather than an editor component. What this is for is
// changing a line in a TODO or a config an agent got nearly right, and
// CodeMirror is three hundred kilobytes to vendor for that. When syntax
// highlighting and a gutter start being the thing that is missing, the backlog
// entry says which one to reach for and why.
//
// The hash is the whole point. It comes back with the read, goes out with the
// write, and the daemon refuses if the file moved on. Editing a file an agent
// is also editing is otherwise last-write-wins at machine speed, and the loss
// is silent.
let editing = null;

async function openEditor(taskID, path) {
  let res;
  try {
    res = await api(`/v1/tasks/${taskID}/files/text?path=${encodeURIComponent(path)}`);
  } catch (e) {
    toast("could not read it", e.message);
    return;
  }
  editing = { taskID, path, hash: res.hash, eol: res.eol };

  document.getElementById("t-edit-where").textContent = res.path;
  document.getElementById("t-edit-where").title = res.path;
  const box = document.getElementById("t-edit-text");
  box.value = res.text;
  editorState("");
  // Typed into means unsaved, said plainly, because a text box that looks the
  // same saved and unsaved is one you close without meaning to.
  box.oninput = () => editorState("not saved");
  document.getElementById("t-edit").hidden = false;
  box.focus();
}

function editorState(s) {
  const el = document.getElementById("t-edit-state");
  if (el) el.textContent = s;
}

function closeEditor() {
  const box = document.getElementById("t-edit");
  if (box) box.hidden = true;
  editing = null;
}

async function saveEditor() {
  if (!editing) return;
  const text = document.getElementById("t-edit-text").value;
  editorState("saving");

  let res;
  try {
    res = await api(`/v1/tasks/${editing.taskID}/files/text?path=${
      encodeURIComponent(editing.path)}`, {
      method: "PUT", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ text, hash: editing.hash, eol: editing.eol })
    });
  } catch (e) {
    // A conflict is not a failure to report and forget. The file changed under
    // you, and the useful thing is to say so and leave what you typed alone so
    // it can be reconciled rather than lost.
    editorState("not saved");
    toast("it changed while you were editing", e.message +
      " nothing was written. close and reopen to see what it says now.");
    return;
  }
  editing.hash = res.hash;
  editorState("saved");
  toast("saved", editing.path.split(/[\\/]/).pop());
}

// Opens a file in the editor, ON THE MACHINE THE SESSION RUNS ON.
//
// Worth being exact about, because over an overlay the obvious reading is
// wrong: this does not open anything on the machine holding the browser. The
// daemon runs the command, so the window appears wherever the daemon is, which
// is the same machine the agent is editing on and is the point.
async function openOnDaemon(id, path) {
  try {
    await api(`/v1/tasks/${id}/files/open`, {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ path })
    });
  } catch (e) {
    toast("could not open it", e.message);
    return;
  }
  toast("opening", path.split(/[\\/]/).pop() + " on the machine running this session");
}

function filesUp() {
  if (filesPos.dialog.parent) loadFiles(filesPos.dialog.parent, dialogFilesCtx());
}
function termFilesUp() {
  if (filesPos.term.parent) loadFiles(filesPos.term.parent, termFilesCtx());
}

// A size a person reads. Exact bytes matter for nothing here.
function bytes(n) {
  n = Number(n) || 0;
  if (n < 1024) return n + "B";
  if (n < 1024 * 1024) return Math.round(n / 1024) + "K";
  if (n < 1024 * 1024 * 1024) return (n / 1024 / 1024).toFixed(1) + "M";
  return (n / 1024 / 1024 / 1024).toFixed(1) + "G";
}

// ── getting a conversation out of a card ────────────────
//
// The other thing worth taking off the machine. A transcript is the only
// record of why the code looks like it does, and it lives in one file, in a
// format nobody reads, that forgetting the session deletes.
//
// Two questions, because they are two decisions and neither has a default the
// other can be folded into: which conversation, then how much of it. One
// conversation in the directory answers the first on its own.
//
// A PLAIN LINK, like the file download beside it, so the browser streams it
// straight to disk. A transcript is ninety megabytes by the end of a working
// day and it must never exist in the page. The name comes off the response.
async function saveSession(id, t) {
  let list = [];
  try {
    list = (await api(`/v1/tasks/${id}/sessions`)).sessions || [];
  } catch (e) {
    toast("could not read them", e.message);
    return;
  }
  if (!list.length) { toast("nothing to save", "no conversations here"); return; }

  let pick = list[0].id;
  if (list.length > 1) {
    pick = await askUser({
      title: "save which conversation?",
      body: `${list.length} in this directory. The one marked <b>current</b> is ` +
        `the one this card is on.`,
      value: (list.find(s => s.current) || list[0]).id,
      choices: list.map(s => ({
        value: s.id,
        label: `${s.title}  ·  ${firstSeen(s.at)}  ·  ${bytes(s.bytes)}` +
          (s.current ? "  ·  current" : "")
      })),
      buttons: [{ label: "cancel", value: null }, { label: "next", value: true, style: "go" }]
    });
    if (pick === null) return;
  }

  const mode = await askUser({
    title: "how much of it?",
    body: "The conversation is what you said and what the agent said back, as " +
      "markdown, with the tool calls, their results and the thinking dropped. " +
      "Everything is the transcript exactly as the runner wrote it.",
    value: "md",
    choices: [
      { value: "md", label: "the conversation, as markdown" },
      { value: "raw", label: "everything, as written (.jsonl)" }
    ],
    buttons: [{ label: "cancel", value: null }, { label: "save it", value: true, style: "go" }]
  });
  if (mode === null) return;

  const a = document.createElement("a");
  a.href = `/v1/tasks/${id}/sessions/${encodeURIComponent(pick)}/export?mode=${mode}`;
  a.download = "";
  document.body.appendChild(a);
  a.click();
  a.remove();
}

// ── actions on a card ───────────────────────────────────
//
// The things you say to an agent often enough to have got tired of typing.
// Stored by the daemon so they are the same on every board, which is right for
// something about the work rather than about the screen.

let allActions = [];

async function loadActions() {
  try { allActions = (await api("/v1/actions")).actions || []; } catch (e) { allActions = []; }
  return allActions;
}

// Which actions belong on one card. The same two conditions the daemon
// applies, because the board offering one the daemon would refuse is worse
// than offering none.
function actionsFor(t) {
  if (!t) return [];
  const tags = (t.tags || []).map(x => String(x).toLowerCase());
  return allActions.filter(a => {
    if (!a.enabled) return false;
    if (a.runner && String(a.runner).toLowerCase() !== String(t.runner || "").toLowerCase()) return false;
    if (a.tag && !tags.includes(String(a.tag).toLowerCase())) return false;
    return true;
  });
}

async function paintCardActions() {
  const field = document.getElementById("d-actions-field");
  const host = document.getElementById("d-actions");
  if (!field || !host || !current) return;
  if (!allActions.length) await loadActions();

  const mine = actionsFor(current);
  field.hidden = !mine.length;
  if (!mine.length) return;

  setHTML(host, mine.map(a =>
    `<button data-action="${esc(a.id)}" title="${esc(a.prompt)}"
      >${esc(a.label)}${a.after === "exit"
        ? ` <span class="by">and exit</span>` : ""}</button>`).join(""));
  // Wired rather than inlined, because a label with an apostrophe in it breaks
  // an onclick attribute and HTML escaping does nothing about that.
  host.querySelectorAll("button[data-action]").forEach(b => {
    b.onclick = () => runCardAction(b.dataset.action);
  });
}

// Actions as menu entries, so they are reachable from a card or from the
// terminal rather than only from inside the card dialog.
//
// The thing an action is for is saying something to a session you are looking
// at. Having to open that session's dialog to reach one put a room between you
// and the sentence.
//
// Loaded lazily and offered as a flyout, so the menu stays the same shape
// whether you have written two of these or twenty.
function actionItems(t) {
  const mine = actionsFor(t);
  if (!mine.length) return null;
  return {
    label: "say something",
    help: "Prompts you wrote, sent to this session. Typed into its terminal " +
      "when atrium owns one, and queued for its next tool call otherwise.",
    sub: mine.map(a => ({
      label: a.label + (a.after === "exit" ? " and exit" : ""),
      act: () => runCardAction(a.id, t)
    }))
  };
}

async function runCardAction(id, task) {
  // The card dialog passes nothing and means `current`, which is what every
  // call did before actions appeared in menus.
  const on = task || current;
  if (!on) return;
  const action = allActions.find(a => a.id === id);
  if (!action) return;
  // Confirmed only when it ends the session. Sending a prompt is undoable by
  // sending another one; quitting is not.
  if (action.after === "exit" && !await confirmUser(action.label + ", then quit?",
    "The prompt is sent, then the runner is asked to exit the way its harness says to." +
    "<br><br>If it is still working, it finishes what it is doing first. If atrium does not " +
    "own its terminal it will be told to wrap up and will need closing where it runs.",
    "send it")) return;

  let res;
  try {
    res = await api(`/v1/tasks/${on.id}/action`, {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ action: id })
    });
  } catch (e) { tellUser("that did not go", e.message); return; }

  // Which of the two routes it took, said out loud, because they are different
  // promises: typed means it has already landed, queued means it has not and
  // will not until the session makes its next tool call or ends its turn.
  const how = res.delivered === "terminal"
    ? "typed into its terminal."
    : "queued. it arrives on the session's next tool call or at the end of its turn.";
  toast(action.label, how + (res.note ? " " + res.note : ""));
  refresh();
}

// The two housekeeping timers.
//
// Daemon side, not browser side, because they are about what happens to the
// machine's data rather than about how this screen looks. Every other setting
// in this dialog is a preference and these two are not.
//
// A stored value the dropdown has no option for is added rather than silently
// shown as something else. Somebody who set an unusual age by hand should see
// the age they set, not the nearest one on a list.
async function loadHousekeeping() {
  let s;
  try { s = await api("/v1/settings"); } catch (e) { return; }
  setTimerValue("s-sweep", s.sweep_dead_after, "60");
  setTimerValue("s-prune", s.prune_after, "off");
  const ed = document.getElementById("s-editor");
  if (ed) ed.value = s.editor_command || "";
  pastePrefs = s;
  const keep = document.getElementById("s-pastekeep");
  if (keep) keep.value = (s.paste_keep === "scrap") ? "scrap" : "keep";
  const pre = document.getElementById("s-pastepre");
  // Never set shows the default, because that is what the next paste will use.
  // The stored `none` shows empty, which is also what it does.
  if (pre) pre.value = preambleOf(s);
  // Empty stays empty, and the placeholder carries the default. A box filled
  // in with today's default reads as a choice somebody made.
  const mb = document.getElementById("s-sbmb");
  if (mb) {
    mb.value = s.scrollback_mb || "";
    mb.placeholder = s.scrollback_mb_now || 16;
    mb.max = s.scrollback_mb_max || 512;
  }
  const lines = document.getElementById("s-sblines");
  if (lines) {
    lines.value = s.scrollback_lines || "";
    lines.placeholder = s.scrollback_lines_now || 50000;
    lines.max = s.scrollback_lines_max || 1000000;
  }
  applyScrollback();

  const roots = document.getElementById("s-browseroots");
  if (roots) roots.value = s.browse_roots || "";
  const shared = document.getElementById("s-shared-loc");
  if (shared) shared.value = s.shared_location || "";
  const shell = document.getElementById("s-shell");
  if (shell) shell.value = s.shell_command || "";
  // What an empty box comes out as. A search of the daemon's own PATH, so it
  // is not something the person reading the box could work out from here.
  const shellNow = document.getElementById("s-shell-now");
  if (shellNow) shellNow.textContent = s.shell_command_now || "";
  const now = document.getElementById("s-browseroots-now");
  if (now) {
    const list = s.browse_roots_now || [];
    // What it resolved to, always, not only when the box is empty. A
    // configured root that does not exist is dropped, and the only way to
    // notice is to be shown what survived.
    now.textContent = list.length
      ? "Right now: " + list.join(", ")
      : "Right now: nothing resolves, so the picker has nowhere to open.";
  }

  fillSkins(s);
}

// The name of the skin that has no rule of its own, because it IS `:root`.
// Learned from the daemon's list rather than written here, so there is still
// only one place a skin is named. Seeded so a page that never reaches the
// daemon does not treat the default as a skin and set an attribute for it.
let defaultSkin = "harbour";

// THE PICKER IS BUILT FROM THE DAEMON'S LIST, not from a list in this file.
//
// There are three places a skin could be named: the stylesheet, the daemon's
// validator, and this picker. Two of them are already checked against each
// other by `scripts/check-skins.sh`. A third list here would be the one that
// silently drifts, because a picker offering a name the daemon refuses fails
// at the moment somebody chooses it, which is a long way from where it was
// typed.
function fillSkins(s) {
  const names = s.board_skins || [];
  if (names.length) defaultSkin = names[0];
  const sel = document.getElementById("s-skin");
  if (!sel || !names.length) return;
  const now = s.board_skin || names[0];
  sel.innerHTML = names.map(n =>
    `<option value="${esc(n)}"${n === now ? " selected" : ""}>${esc(n)}</option>`).join("");
  applySkin(now);
}

// Wearing it. One attribute on `body`, which every skin rule keys off.
//
// The default is the palette in `:root` and has no rule of its own, so it is
// spelled by REMOVING the attribute rather than by setting it to a name that
// matches nothing. Those two look identical on screen and are not: an
// attribute matching no rule is also what a typo does, and this way the state
// on the element says which happened.
function applySkin(name) {
  if (!name || name === defaultSkin) {
    document.documentElement.removeAttribute("data-skin");
    return;
  }
  document.documentElement.setAttribute("data-skin", name);
}

// TRYING SKINS, in the header, with the same control as trying a terminal
// theme.
//
// The shape is copied from `pickTheme` deliberately, including what it refuses
// to do. Nothing happens on blur: a skin is judged by looking at the BOARD, and
// looking at the board means clicking it, so a picker that committed and closed
// on blur would close itself the first time you tried to use it. It is answered
// with `use it` or `cancel` and by nothing else.
//
// The one difference from the terminal's: this previews for every window, not
// just this one. A skin is board-wide, so a preview that only repainted the
// window you are in would be judging it against half the evidence.
let skinBefore = null;

// Where the panel was left, so it comes back there. Per browser: it is about
// this screen and where the operator does not mind something sitting.
const SKINLAB_AT = "atrium.skinlab.at";

function openSkinLab() {
  const lab = document.getElementById("skinlab");
  const sel = document.getElementById("skinlab-pick");
  if (!lab || !sel) return;
  // A second press closes it, so the header button is a toggle rather than a
  // way to open something you then have to answer.
  if (!lab.hidden) { cancelSkin(); return; }

  const names = (pastePrefs && pastePrefs.board_skins) || [];
  if (!names.length) {
    toast("no skins yet", "the daemon has not said which ones it has. open the gear once");
    return;
  }
  // The gear covers most of the board, and the board is what is being judged.
  const gear = document.getElementById("settings");
  if (gear && gear.open) gear.close();

  skinBefore = currentSkin();
  sel.innerHTML = names.map(n =>
    `<option value="${esc(n)}"${n === skinBefore ? " selected" : ""}>${esc(n)}</option>`).join("");
  // Shown before it is placed, because placing it measures it and a hidden
  // element measures as nothing.
  lab.hidden = false;
  placeSkinLab();
  sel.focus();
}

// Wherever it was left, clamped back into view.
//
// A remembered position outlives the window it was chosen in: a panel put in
// the bottom right of a wide monitor is off screen entirely on a laptop, with
// no way to reach it and nothing to say where it went.
// ANCHORED TO THE NEAREST EDGE, NOT TO A COORDINATE.
//
// A panel positioned by `left` and `top` keeps those numbers when the window
// gets smaller, so one parked in the top right slides off the right edge and
// there is nothing left to grab. Storing which edges it was NEAR and how far
// from them means the browser holds it there for free: `right: 24px` stays
// 24px from the right whatever the window does.
//
// Which edge is decided by which half it sits in, per axis, on the drag that
// put it there. That matches what somebody meant: a panel tucked top right is
// a panel that belongs top right.
function labAt() {
  try {
    const at = JSON.parse(localStorage.getItem(SKINLAB_AT) || "null");
    if (at && typeof at.x === "number" && typeof at.y === "number") return at;
  } catch (e) {}
  // Under the header, out of the way of the tabs, on the side the gear is.
  return { side: "right", x: 24, vside: "top", y: 88 };
}

function placeSkinLab() {
  const lab = document.getElementById("skinlab");
  if (!lab) return;
  const at = labAt();
  const box = lab.getBoundingClientRect();
  const w = box.width || 288;
  const h = box.height || 170;

  // Clamped even when anchored, because a remembered offset can be larger than
  // the window it is being restored into.
  const maxX = Math.max(8, window.innerWidth - w / 2);
  const maxY = Math.max(8, window.innerHeight - h / 2);
  const x = Math.min(Math.max(at.x, 8 - w / 2), maxX);
  const y = Math.min(Math.max(at.y, 8 - h / 2), maxY);

  if (at.side === "right") {
    lab.style.right = x + "px";
    lab.style.left = "auto";
  } else {
    lab.style.left = x + "px";
    lab.style.right = "auto";
  }
  if (at.vside === "bottom") {
    lab.style.bottom = y + "px";
    lab.style.top = "auto";
  } else {
    lab.style.top = y + "px";
    lab.style.bottom = "auto";
  }
}

// Where it ended up, as a distance from whichever edge it is nearer.
function rememberLabAt(lab) {
  const box = lab.getBoundingClientRect();
  const midX = box.left + box.width / 2;
  const midY = box.top + box.height / 2;
  const at = midX > window.innerWidth / 2
    ? { side: "right", x: Math.round(window.innerWidth - box.right) }
    : { side: "left", x: Math.round(box.left) };
  Object.assign(at, midY > window.innerHeight / 2
    ? { vside: "bottom", y: Math.round(window.innerHeight - box.bottom) }
    : { vside: "top", y: Math.round(box.top) });
  try { localStorage.setItem(SKINLAB_AT, JSON.stringify(at)); } catch (e) {}
  placeSkinLab();
}

// A window that shrank past a remembered offset still has to leave something
// to grab. The anchoring above handles the ordinary case on its own.
addEventListener("resize", () => {
  const lab = document.getElementById("skinlab");
  if (lab && !lab.hidden) placeSkinLab();
});

// Walk the list without opening the select. The whole point of the panel is
// seeing each one against the real board, and that reads better as a step than
// as a dropdown covering the thing you are looking at.
function stepSkin(by) {
  const sel = document.getElementById("skinlab-pick");
  if (!sel || !sel.options.length) return;
  sel.selectedIndex = (sel.selectedIndex + by + sel.options.length) % sel.options.length;
  previewSkin(sel.value);
}

// DRAGGING. The element is held in a local rather than read off the event,
// because `currentTarget` is null by the time a later handler runs, and the
// listeners then never come off and the panel follows the pointer with no
// button held. That exact bug cost an evening on the terminal list's grip.
let labFrom = null;
function startLabDrag(e) {
  const lab = document.getElementById("skinlab");
  const grip = e.currentTarget;
  if (!lab || !grip) return;
  const box = lab.getBoundingClientRect();
  labFrom = { dx: e.clientX - box.left, dy: e.clientY - box.top, w: box.width, h: box.height };
  grip.setPointerCapture(e.pointerId);

  // Dragged in `left` and `top`, because a pointer gives an absolute position
  // and doing the edge arithmetic on every frame buys nothing. It is converted
  // to an edge anchor once, on release.
  const move = ev => {
    if (!labFrom) return;
    const x = ev.clientX - labFrom.dx;
    const y = ev.clientY - labFrom.dy;
    lab.style.right = "auto";
    lab.style.bottom = "auto";
    // Half of it has to stay reachable. Clamping fully inside is worse: it
    // snaps a panel somebody deliberately tucked against an edge.
    lab.style.left = Math.min(Math.max(x, 8 - labFrom.w / 2),
      window.innerWidth - labFrom.w / 2) + "px";
    lab.style.top = Math.min(Math.max(y, 8 - labFrom.h / 2),
      window.innerHeight - labFrom.h / 2) + "px";
  };
  const done = () => {
    grip.removeEventListener("pointermove", move);
    grip.removeEventListener("pointerup", done);
    grip.removeEventListener("pointercancel", done);
    try { grip.releasePointerCapture(e.pointerId); } catch (err) {}
    labFrom = null;
    rememberLabAt(lab);
  };
  grip.addEventListener("pointermove", move);
  grip.addEventListener("pointerup", done);
  grip.addEventListener("pointercancel", done);
  e.preventDefault();
}

// The third answer, and the reason there are three rather than two. Getting
// back to the palette the board shipped with means finding it in a list of
// twenty, and it is the one choice somebody makes without wanting to browse.
async function useDefaultSkin() {
  const sel = document.getElementById("skinlab-pick");
  if (sel) sel.value = defaultSkin;
  previewSkin(defaultSkin);
  await keepSkin();
}

// What is on screen right now, which is the attribute rather than the setting.
// They differ exactly while a preview is up, and this is the value `cancel` has
// to put back.
function currentSkin() {
  return document.documentElement.getAttribute("data-skin") || defaultSkin;
}

// Painted everywhere, and not saved. `applySkin` moves this window and the
// broadcast moves the others, which is what makes the preview honest.
function previewSkin(name) {
  applySkin(name);
  try { soloBus && soloBus.postMessage({ type: "skin-preview", name }); } catch (e) {}
}

async function keepSkin() {
  const lab = document.getElementById("skinlab");
  const sel = document.getElementById("skinlab-pick");
  if (!sel || !lab || lab.hidden) return;
  lab.hidden = true;
  const name = sel.value;
  if (name === skinBefore) return;
  // Through the same path the settings dialog uses, so there is one place that
  // writes this and one place that handles the refusal.
  const picked = document.getElementById("s-skin");
  if (picked) picked.value = name;
  await saveSkin(name);
}

function cancelSkin() {
  const lab = document.getElementById("skinlab");
  if (!lab || lab.hidden) return;
  lab.hidden = true;
  previewSkin(skinBefore);
}

async function saveSkin(name) {
  const sel = document.getElementById("s-skin");
  const want = name || (sel && sel.value);
  if (!want) return;
  // Painted before the round trip, because this is the one setting whose whole
  // point is what it looks like. Waiting for the daemon to answer means half a
  // second of the old colours while you are staring at the control you just
  // moved.
  applySkin(want);
  try {
    pastePrefs = await api("/v1/settings", {
      method: "POST",
      body: JSON.stringify({ board_skin: want })
    });
  } catch (e) {
    // Put it back. A skin that did not save and stayed on screen is a board
    // that changes colour by itself at the next reload.
    toast("that did not save", e.message);
    fillSkins(pastePrefs || {});
    return;
  }
  rememberSkin(want);
  // Every popped-out terminal is a separate window with its own body, and a
  // skin is board-wide by definition, so they repaint now rather than at their
  // next reload.
  try { soloBus && soloBus.postMessage({ type: "skin", name: want }); } catch (e) {}
}

// THE SKIN IS REMEMBERED IN THIS BROWSER AS WELL AS ON THE DAEMON, and the
// local copy is not the source of truth. It exists to kill the flash.
//
// The daemon holds the setting, which is what makes it follow to a phone. But
// reading it takes a round trip, and until it lands the page is painted in the
// default palette, so every load of every window flickers navy for a moment
// before going black. Applying the remembered one at parse time and then
// reconciling means the flash only happens the first time a new browser sees
// the board, which is the one time it is honest.
const SKIN_KEY = "atrium.skin";
function rememberSkin(name) {
  try { name ? localStorage.setItem(SKIN_KEY, name) : localStorage.removeItem(SKIN_KEY); } catch (e) {}
}

async function bootSkin() {
  try {
    const remembered = localStorage.getItem(SKIN_KEY);
    if (remembered) document.documentElement.setAttribute("data-skin", remembered);
  } catch (e) {}
  let s;
  try { s = await api("/v1/settings"); } catch (e) { return; }
  const names = s.board_skins || [];
  const now = s.board_skin || names[0] || "";
  // The daemon's answer wins, including when it says the default: a skin
  // cleared from another browser has to reach this one.
  if (now && names.length && now !== names[0]) {
    document.documentElement.setAttribute("data-skin", now);
    rememberSkin(now);
  } else {
    document.documentElement.removeAttribute("data-skin");
    rememberSkin("");
  }
}

async function saveBrowseRoots() {
  const el = document.getElementById("s-browseroots");
  if (!el) return;
  try {
    pastePrefs = await api("/v1/settings", {
      method: "POST",
      body: JSON.stringify({ browse_roots: el.value })
    });
  } catch (e) {
    toast("that did not save", e.message);
    return;
  }
  loadHousekeeping();
  toast("saved", "the picker opens in " +
    ((pastePrefs.browse_roots_now || []).length || 0) + " place(s)");
}

async function saveSharedLocation() {
  const el = document.getElementById("s-shared-loc");
  if (!el) return;
  try {
    await api("/v1/settings", {
      method: "POST",
      body: JSON.stringify({ shared_location: el.value })
    });
  } catch (e) {
    toast("that did not save", e.message);
    return;
  }
  loadHousekeeping();
  // Says the part that is easy to miss. The file is written when the daemon
  // starts, so saving this changes nothing until it does, and somebody who
  // saves it and immediately looks for the file finds nothing there.
  toast("saved", el.value.trim()
    ? "written at the next restart"
    : "the shared address file is off");
}

// Which shell a pane opens. Empty puts the search back.
async function saveShellCommand() {
  const el = document.getElementById("s-shell");
  if (!el) return;
  try {
    pastePrefs = await api("/v1/settings", {
      method: "POST",
      body: JSON.stringify({ shell_command: String(el.value || "").trim() })
    });
  } catch (e) {
    toast("that did not save", e.message);
    return;
  }
  loadHousekeeping();
  // Says the part that is easy to miss. A shell already open keeps being what
  // it was, because it is a running process rather than a setting.
  toast("saved", el.value.trim()
    ? "the next shell you open runs " + el.value.trim()
    : "back to whatever this machine has: " + (pastePrefs.shell_command_now || ""));
}

// Both halves together, since they are one decision in two units.
async function saveScrollback() {
  const mb = document.getElementById("s-sbmb");
  const lines = document.getElementById("s-sblines");
  if (!mb || !lines) return;
  try {
    pastePrefs = await api("/v1/settings", {
      method: "POST",
      body: JSON.stringify({
        scrollback_mb: String(mb.value || "").trim(),
        scrollback_lines: String(lines.value || "").trim()
      })
    });
  } catch (e) {
    toast("that did not save", e.message);
    return;
  }
  // The browser's half is live. The daemon's is not, and saying so beats
  // somebody scrolling up in a session started an hour ago and finding the
  // old limit still in force.
  applyScrollback();
  loadHousekeeping();
  toast("scrollback saved",
    `${pastePrefs.scrollback_lines_now} lines here, now. ` +
    `${pastePrefs.scrollback_mb_now}MB in the daemon, for sessions started from here on.`);
}

// pastePrefs is the last answer from the daemon, kept so that a paste does not
// have to ask first. Null until something has looked, which pasteSettings does
// once per window.
let pastePrefs = null;

// How many lines a terminal keeps.
//
// Synchronous, because it is read while xterm is being constructed and there
// is no awaiting that. Whatever the last GET said, and the daemon's own
// default when nothing has been asked yet, so the two ends agree by default
// rather than by coincidence.
function scrollbackLines() {
  const n = pastePrefs && Number(pastePrefs.scrollback_lines_now);
  return n > 0 ? n : 50000;
}

// A terminal that was built before the settings arrived. xterm takes a new
// scrollback at any time and keeps what it already holds, so this costs
// nothing and closes the window where a popped-out terminal opened on the
// fallback and stayed there.
function applyScrollback() {
  if (term) term.options.scrollback = scrollbackLines();
}

// preambleOf turns what is stored into what gets typed.
//
// Three cases and only three: unset means the daemon's default, the sentinel
// means nothing at all, anything else is what somebody wrote.
function preambleOf(s) {
  const raw = s.paste_preamble;
  if (raw === undefined || raw === null || raw === "") {
    return s.paste_preamble_default || "";
  }
  if (raw === (s.paste_preamble_none || "none")) return "";
  return raw;
}

// pasteSettings fetches once per window and caches. A popped-out terminal is
// its own window and never opens the settings dialog, so it cannot rely on
// loadHousekeeping having run.
async function pasteSettings() {
  if (pastePrefs) return pastePrefs;
  try { pastePrefs = await api("/v1/settings"); } catch (e) { pastePrefs = {}; }
  // A terminal may already be up, built on the fallback. Now that the real
  // answer is here, it gets it.
  applyScrollback();
  return pastePrefs;
}

// Both paste settings, saved together, because they are one decision about
// what a paste does and reading them back separately is how the two disagree.
async function savePastePrefs() {
  const keep = document.getElementById("s-pastekeep");
  const pre = document.getElementById("s-pastepre");
  if (!keep || !pre) return;
  // Empty is stored as the sentinel, or it would read back as the default and
  // the box would refill itself with words somebody just deleted.
  const body = {
    paste_keep: keep.value,
    paste_preamble: pre.value === "" ? "none" : pre.value,
  };
  try {
    pastePrefs = await api("/v1/settings", {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body)
    });
  } catch (e) {
    toast("that did not save", e.message);
    return;
  }
  toast("saved", keep.value === "scrap"
    ? "pasted files land in a folder that is emptied on restart"
    : "pasted files stay in the card");
}

// Saved on a button rather than on every keystroke: a half-typed command is a
// program name that does not exist, and the failure would arrive later, from
// the open button, with no obvious connection to the typing.
async function saveEditorCommand() {
  const ed = document.getElementById("s-editor");
  if (!ed) return;
  await saveHousekeeping("editor_command", ed.value.trim());
  toast(ed.value.trim() ? "editor set" : "editor cleared",
    ed.value.trim() || "the open button will say it is not configured");
}

function setTimerValue(id, value, fallback) {
  const sel = document.getElementById(id);
  if (!sel) return;
  // Unset means the default the daemon applies, which is not the same as off.
  const want = (value === undefined || value === null || value === "") ? fallback : String(value);
  if (!Array.from(sel.options).some(o => o.value === want)) {
    const o = document.createElement("option");
    o.value = want;
    o.textContent = "after " + want + " seconds";
    sel.appendChild(o);
  }
  sel.value = want;
}

async function saveHousekeeping(field, value) {
  const body = {};
  body[field] = value;
  try {
    await api("/v1/settings", {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body)
    });
  } catch (e) { tellUser("that did not stick", e.message); }
}

