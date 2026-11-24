// When the board offers a walk. An agent's done is the moment: a card that went done carries a "walk ready" chip until
// its walk is opened, a toast says so once, and the card menu always has the entry. Nothing here asks the agent for
// anything: the card's own status and recap_at are the event, and the file count is read once per done.

const walkOffers = new Map();   // card id -> { at: recap_at it was read for, n: files, pending }
const WALK_SEEN = "atrium.walkSeen.";

function walkSeenAt(id) { try { return localStorage.getItem(WALK_SEEN + id) || ""; } catch (e) { return ""; } }
function walkSeenSet(id, at) { try { localStorage.setItem(WALK_SEEN + id, at || ""); } catch (e) {} }

// A card is offered a walk when it is done, has a directory, and said so after the last walk was opened.
function walkOffered(t) {
  return !!t && t.status === "done" && !!t.recap_at && !!t.worktree && walkSeenAt(t.id) !== t.recap_at;
}

// Reads how many files the done card changed, once for each done. Repaints the list when it lands.
function walkOfferRead(t) {
  if (!t || t.status !== "done" || !t.recap_at || !t.worktree) return;
  const have = walkOffers.get(t.id);
  if (have && have.at === t.recap_at) return;
  walkOffers.set(t.id, { at: t.recap_at, n: -1 });
  api(`/v1/tasks/${t.id}/diff?mode=branch&summary=1`).then(r => {
    const n = (r && r.modes && (r.modes.branch || r.modes.unstaged || r.modes.staged)) || 0;
    walkOffers.set(t.id, { at: t.recap_at, n, landed: !!(r && r.landed) });
    if (typeof runPass === "function") runPass(); else if (typeof paintSoon === "function") paintSoon();
  }).catch(() => walkOffers.set(t.id, { at: t.recap_at, n: 0 }));
}

function walkOfferText(t) {
  const o = walkOffers.get(t.id);
  if (!o || o.n < 0) return "walk";
  if (!o.n) return o.landed ? "merged" : "nothing to walk";
  return o.n + (o.n === 1 ? " file" : " files");
}

// The chip on a card row. Only a done card has one: every other card is walked from its terminal bar.
function walkChip(t) {
  if (!t || t.status !== "done" || !t.worktree || !flagOn("walk")) return "";
  walkOfferRead(t);
  const o = walkOffers.get(t.id);
  if (o && o.n === 0 && !o.landed) return "";
  const ready = walkOffered(t) && (o || {}).n !== 0;
  return `<span class="chip walkchip${ready ? " ready" : ""}" role="button" data-tip="${
    ready ? "the agent finished: walk what it changed" : "walk this card's changes"}" onclick="event.stopPropagation();walkOfferOpen('${
    esc(t.id)}')">${ready ? "walk ready · " : "walk · "}${esc(walkOfferText(t))}</span>`;
}

async function walkOfferOpen(id) {
  const t = cardRows.get(id);
  if (t) walkSeenSet(id, t.recap_at);
  if (typeof landOnAlert === "function") await landOnAlert(id, "", "");
  if (typeof termTask !== "undefined" && termTask && sameCard(termTask.id, id) && typeof dockOpen === "function" && !dock.open) {
    dock.want = id;
    if (dock.tenant) dockOpen();
  }
  if (typeof paintSoon === "function") paintSoon();
}

// A card that has just gone done: said once, and only when the board saw it change, not on a first read.
// Returns true when the card just went done, so the caller paints at once and not on the debounce.
function walkOfferEvent(d, was) {
  if (!d || !d.id || d.status !== "done" || (was && was.status === "done")) return false;
  walkOfferRead(d);
  if (!was || !walkOffered(d) || typeof toast !== "function" || !flagOn("walk")) return true;
  if ((d.spawned_by_id || d.spawned_by) && d.spawned_by !== "@human") return true;
  const name = d.title || d.wire_name || "a card";
  toast(name + " is done", "its changes are ready to walk", "", "", d.id);
  return true;
}

// The card menu's entry, always there on a card with a directory.
function walkMenuItem(t) {
  if (!t || !t.worktree || !flagOn("walk")) return null;
  const o = walkOffers.get(t.id);
  return {
    label: "walk this card's changes",
    help: "Opens the walk beside the terminal: the branch against its base, a file at a time, with the uncommitted work marked.",
    note: o && o.n > 0 ? o.n + (o.n === 1 ? " file" : " files") : "",
    act: () => walkOfferOpen(t.id)
  };
}
