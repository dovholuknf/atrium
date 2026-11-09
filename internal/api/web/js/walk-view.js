// The walk in a tab of its own (walk.html). The drawer's code (js/walk.js) does all of the walking: this file is the
// few board helpers it calls, and the boot. The address is `#task=<card>` and, for a pulls row, `&pr=<id>`.

let termTask = null;

const esc = s => (s == null ? "" : String(s)).replace(/[&<>"]/g,
  c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" })[c]);

async function api(path, opts) {
  const res = await fetch(path, opts);
  const raw = await res.text();
  let body = null;
  try { body = raw ? JSON.parse(raw) : null; } catch (e) { body = null; }
  if (!res.ok) {
    const err = new Error((body && body.error) || raw.trim() || res.statusText);
    err.status = res.status;
    err.body = body;
    throw err;
  }
  return body;
}

function pullsPost(path, body) {
  return api(path, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body || {}) });
}

// A plain repaint that keeps the reply box's focus and caret, since this page has no morph.
function setHTML(el, html) {
  if (!el || el.__paintedFrom === html) return false;
  const a = document.activeElement;
  const inside = a && el.contains(a);
  const keep = inside && a.id ? { id: a.id, s: a.selectionStart, e: a.selectionEnd, v: a.value } : null;
  el.innerHTML = html;
  el.__paintedFrom = html;
  if (inside && !keep) document.getElementById("walk-drawer").focus({ preventScroll: true });
  const n = keep && document.getElementById(keep.id);
  if (n) {
    if (keep.v !== undefined && n.value !== keep.v) n.value = keep.v;
    n.focus({ preventScroll: true });
    try { n.setSelectionRange(keep.s, keep.e); } catch (e) {}
  }
  return true;
}

function toast(title, body) {
  const box = document.getElementById("wv-toasts");
  if (!box) return;
  const t = document.createElement("div");
  t.className = "wv-toast";
  t.innerHTML = `<b>${esc(title)}</b>${body ? `<span>${esc(body)}</span>` : ""}`;
  box.appendChild(t);
  setTimeout(() => t.remove(), 4000);
}

async function askUser(opts) {
  if (!opts.input) return confirm(opts.title) ? true : null;
  const v = prompt(opts.title + "\n\n" + (opts.body || ""), "");
  return v === null ? null : v;
}

function openLinkReused(url) { window.open(url, "atrium-link"); }

// No event stream in this tab, so the folder is read again on a timer while the tab is visible. The review card's
// answers are files it writes, and nothing else says one landed.
const WALK_VIEW_EVERY_MS = 4000;

addEventListener("DOMContentLoaded", async () => {
  const q = new URLSearchParams(location.hash.slice(1));
  const id = q.get("task") || "";
  const drawer = document.getElementById("walk-drawer");
  if (!id) { setHTML(document.getElementById("walk-item"), `<div class="empty">no card in the address</div>`); return; }
  let task = { id, worktree: "" };
  try { task = Object.assign(task, await api(`/v1/tasks/${encodeURIComponent(id)}`)); } catch (e) {}
  termTask = task;
  const back = document.getElementById("wv-board");
  if (back) back.href = "/#term=" + encodeURIComponent(id);
  dock.tenant = walkTenant;
  dock.taskId = id;
  walkTenant.prId = q.get("pr") || "";
  dock.open = true;
  drawer.hidden = false;
  await walkTenant.opened(dock);
  await dockLoad();
  document.title = "walk · " + (task.display_title || task.title || id);
  drawer.focus({ preventScroll: true });
  setInterval(() => dockResync(), WALK_VIEW_EVERY_MS);
});
