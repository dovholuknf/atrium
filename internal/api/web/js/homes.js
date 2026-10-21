// CARD HOMES ON THE BOARD: the folder a card works in, how big it is, the room's card root, and the Disk panel in room
// settings. See docs/runtime/card-home-design.md section 7.
//
// THE BOARD ONLY SHOWS WHAT THE HUMAN ACTS ON. The rows come from GET /v1/homes on the room the board is looking at, keyed
// by the card, and are read again no more than once in HOMES_EVERY. The size is the room's last walk, so it carries its
// age, shown only once it is over an hour old.

const HOMES_EVERY = 30000;
const HOME_AGE_SHOWN = 3600;
const ROOT_LOW_PCT = 10;
const ROOT_LOW_BYTES = 20e9;

let cardHomes = {};
let homesAt = 0;
let homesBusy = false;
let homesRoot = "";

function homeKey(id) { return String(id || "").split("~").pop(); }

function homesLoaded(rows) {
  cardHomes = {};
  for (const h of rows || []) if (h.card && h.state !== "removed") cardHomes[h.card] = h;
  homesAt = Date.now();
}

// loadHomes reads the ledger. A room that does not have the route answers 404 and the board shows cwd as it always did.
async function loadHomes(force) {
  if (homesBusy || (!force && Date.now() - homesAt < HOMES_EVERY)) return;
  homesBusy = true;
  try {
    const v = await api("/v1/homes");
    homesLoaded(v && v.homes);
    homesRoot = v && !v.error && v.root || "";
    paintThrowawayHint();
  } catch (e) {
    homesAt = Date.now();
  } finally {
    homesBusy = false;
  }
}

const THROWAWAY_KEPT = " If it turns out to matter, promote it from the card's menu and it stays where it is, under the card root.";

// paintThrowawayHint says where a promoted throwaway ends up: in place when the room has a card root, since the
// throwaway is then a home, and somewhere chosen when it has none.
function paintThrowawayHint() {
  const el = document.getElementById("l-throwaway-hint");
  if (!el) return;
  if (!el.dataset.was) el.dataset.was = el.textContent;
  el.textContent = homesRoot
    ? el.dataset.was.replace(/\s+If it turns out to matter[\s\S]*$/, "") + THROWAWAY_KEPT
    : el.dataset.was;
}

function homeOf(task) {
  return task ? cardHomes[homeKey(task.id)] || null : null;
}

// homeAgeSecs is how old the last size walk is, null when there has been none.
function homeAgeSecs(h) {
  const at = Date.parse(h && h.measured_at || "");
  return at ? Math.max(0, (Date.now() - at) / 1000) : null;
}

// homeSizeLabel is the size and, past an hour, how long ago it was measured.
function homeSizeLabel(h) {
  const age = homeAgeSecs(h);
  if (age === null) return "not measured yet";
  const size = diskLabel(h.bytes) || "0 KB";
  return age > HOME_AGE_SHOWN ? size + " · " + ago(age) + " ago" : size;
}

function homeCanOpen(task) {
  return /^(127\.0\.0\.1|localhost|\[::1\])$/.test(location.hostname) && !(typeof roomOf === "function" && roomOf(task.id));
}

function folderIcon() {
  return `<svg viewBox="0 0 16 16" width="14" height="14" aria-hidden="true" fill="none" stroke="currentColor"
    stroke-width="1.4" stroke-linejoin="round"><path d="M1.5 4.5h4l1.5 1.5h7.5v6.5h-13z"/></svg>`;
}

// homePathChips is the card's home: the path with its copy button, the open-folder button where the board can, and
// the size. "" for a card with none, which keeps showing its cwd.
function homePathChips(task) {
  const h = homeOf(task);
  if (!h) return "";
  const open = homeCanOpen(task)
    ? `<button class="copybit homeopen" data-home="${esc(h.id)}" data-tip="open this folder on the room's machine"
        onclick="event.stopPropagation();openHomeFolder(this)">${folderIcon()}</button>` : "";
  const age = homeAgeSecs(h);
  const stale = age !== null && age > HOME_AGE_SHOWN;
  return `<span class="chip path home" data-path="${esc(h.path)}"
      ><button class="copybit" data-tip="copy this path" onclick="event.stopPropagation();copyPath(this.parentNode)"
        >${typeof copyIcon === "function" ? copyIcon() : ""}</button>${esc(h.path)}${open}</span>` +
    `<span class="chip homesize${stale ? " old" : ""}" data-tip="${esc(
      "what the home holds on disk, as the room last measured it" + (h.files ? ", " + h.files + " files" : ""))
    }">${esc(homeSizeLabel(h))}</span>`;
}

async function openHomeFolder(el) {
  const id = el && el.dataset ? el.dataset.home : "";
  if (!id) return;
  try {
    await api("/v1/homes/open", {
      method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ id })
    });
  } catch (e) {
    toast("could not open the folder", e.message);
  }
}

// ── the room ────────────────────────────────────────────────────────────────

// cardRootLow is the line past which the human has something to do: under 10 percent free, or under 20 GB.
function cardRootLow(d) {
  if (!d || !d.root_total_bytes) return false;
  const free = Number(d.root_free_bytes) || 0;
  return free < d.root_total_bytes * ROOT_LOW_PCT / 100 || free < ROOT_LOW_BYTES;
}

// cardRootRow is the room tile's line for the card root: where it is, what the homes hold, what the drive has left. The
// number turns --wait orange only under the line above.
function cardRootRow(key, d) {
  if (!d || !d.card_root) return "";
  const low = cardRootLow(d);
  const total = diskLabel(d.homes_bytes) || "0 KB";
  const free = d.root_total_bytes ? fmtGB(d.root_free_bytes).replace(" GB", "") + "/" + fmtGB(d.root_total_bytes) + " free" : "";
  const tip = "card homes under " + d.card_root + (free ? ", " + free : "") + (low ? ". the drive is nearly full" : "");
  return `<div class="rd-row rd-cardroot${low ? " low" : ""}" data-low="${low ? "1" : "0"}"
      data-tip="${esc(tip)}"
      ><span><b data-v="${key}.homes">${esc(total)}</b> in ${Number(d.homes) || 0} homes</span></div>`;
}

// ── the Disk panel in room settings ─────────────────────────────────────────

let diskView = null;

function diskRoomHeaders() {
  return typeof roomCfgFor === "string" && roomCfgFor ? { "X-Atrium-Room": roomCfgFor } : {};
}

async function loadDiskPanel() {
  const out = document.getElementById("rc-disk");
  if (!out) return;
  try {
    diskView = await api("/v1/homes", { headers: diskRoomHeaders() });
  } catch (e) {
    diskView = null;
    out.innerHTML = `<span class="hintline">this room cannot list its homes: ${esc(e.message || String(e))}</span>`;
    return;
  }
  paintDiskPanel();
}

function homeRemovable(h) {
  return !h.running && (h.state === "kept" || h.orphaned);
}

function paintDiskPanel() {
  const out = document.getElementById("rc-disk");
  const v = diskView;
  if (!out || !v) return;
  const head = v.error
    ? `<span class="hintline">${esc(v.error)}</span>`
    : `<span class="hintline"><b>${esc(v.root)}</b>: ${esc(diskLabel(v.bytes) || "0 KB")} in ${v.homes.length} homes, ` +
      `${esc(fmtGB(v.free_bytes))} free of ${esc(fmtGB(v.total_bytes))}</span>`;
  const rows = (v.homes || []).map(h => `<tr data-home="${esc(h.id)}">
      <td class="ell" data-tip="${esc(h.path)}">${esc(h.path.split("/").pop())}</td>
      <td class="ell">${esc(h.orphaned ? "orphaned" : h.title || h.card)}</td>
      <td><span class="chip">${esc(h.state)}</span></td>
      <td class="ell" data-tip="${esc(h.reason || "")}">${esc(h.reason || "")}</td>
      <td class="num">${esc(homeSizeLabel(h))}</td>
      <td>${homeRemovable(h)
        ? `<button class="homerm" data-home="${esc(h.id)}" onclick="removeHomeFromDisk(this)">remove</button>` : ""}</td>
    </tr>`).join("");
  const notAtriums = (v.not_atriums || []).length
    ? `<div class="dlg-sec-h">not atrium's</div><ul class="hintline">${v.not_atriums.map(p => `<li>${esc(p)}</li>`).join("")}</ul>` : "";
  const outside = (v.outside || []).length
    ? `<div class="dlg-sec-h">outside the card root</div><table class="kv homes-out">${v.outside.map(c =>
      `<tr><td class="ell">${esc(c.title || c.card)}</td><td class="ell">${esc(c.path)}</td></tr>`).join("")}</table>` : "";
  out.innerHTML = head +
    (rows ? `<table class="kv homes-table"><tbody>${rows}</tbody></table>` : `<span class="hintline">no homes yet</span>`) +
    notAtriums + outside;
}

// removeHomeFromDisk asks, naming the path and the size, then posts the row's id. Never a path.
async function removeHomeFromDisk(btn) {
  const id = btn && btn.dataset ? btn.dataset.home : "";
  const h = diskView && (diskView.homes || []).find(x => x.id === id);
  if (!h) return;
  const ok = await askUser({
    title: "remove this home?",
    body: `Deletes <code>${esc(h.path)}</code> and everything in it (${esc(homeSizeLabel(h))}).` +
      (h.reason ? `<br><br>It was kept because: ${esc(h.reason)}.` : "") +
      `<br><br>Checks on the work are skipped here, so anything not pushed or landed is gone.`,
    buttons: [{ label: "cancel", value: null }, { label: "remove it", value: true, style: "go" }]
  });
  if (!ok) return;
  try {
    const res = await api("/v1/homes/remove", {
      method: "POST", headers: Object.assign({ "Content-Type": "application/json" }, diskRoomHeaders()),
      body: JSON.stringify({ id })
    });
    if (res && res.removed) toast("home removed", diskLabel(res.freed) ? diskLabel(res.freed) + " freed" : h.path);
    else toast("home kept", (res && res.kept) || "it could not be removed");
  } catch (e) {
    toast("could not remove it", e.message);
  }
  await loadDiskPanel();
  homesAt = 0;
  loadHomes(true);
}

async function saveCardRoot() {
  const el = document.getElementById("s-cardroot");
  if (!el) return;
  await saveHousekeeping("card_root", el.value.trim());
  await loadDiskPanel();
}

async function saveCardRootRequired() {
  const el = document.getElementById("s-cardrootreq");
  if (el) await saveHousekeeping("card_root_required", !!el.checked);
}

async function saveScratchRoot() {
  const el = document.getElementById("s-scratchroot");
  if (el) await saveHousekeeping("scratch_root", el.value.trim());
}

function fillCardRoot(s) {
  const sr = document.getElementById("s-scratchroot");
  if (sr) sr.value = (s && s.scratch_root) || "";
  const req = document.getElementById("s-cardrootreq");
  if (req) req.checked = !!(s && s.card_root_required);
  const el = document.getElementById("s-cardroot");
  if (el) el.value = (s && s.card_root) || "";
}
