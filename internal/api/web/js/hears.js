// The card details' "Hears" line: which hub events this card is subscribed to, with add and remove. The hub keeps the
// rows (`/_hub/events/subs`, docs/fabric/land-webhook-design.md, 3) and the board is the human writing them, so it may
// edit any card. A board with no hub answers 404 there and the line stays hidden.
let hearsFor = "";

function hearsCardId(t) {
  if (!t) return "";
  const id = String(t.id);
  if (roomOf(id)) return id;
  const room = typeof roomNow === "function" ? roomNow() : "";
  return room ? room + "~" + id : "";
}

function hearsChipText(s) {
  return s.kind + " " + (s.repo || "any") + " " + (s.branch || "any");
}

function hearsHTML(subs) {
  const chips = subs.map(s =>
    `<span class="chip hears-chip" data-sub="${esc(s.id)}">${esc(hearsChipText(s))}${s.wake ? " (wakes)" : ""}` +
    `<button type="button" class="hears-x" data-act="unhear" data-id="${esc(s.id)}" aria-label="stop hearing this"` +
    ` data-tip="stop hearing this">&times;</button></span>`).join("");
  return chips + `<button type="button" class="hears-add" data-act="hear-form">+ land</button>`;
}

async function hubSubs(body) {
  const res = await plainFetch("/_hub/events/subs", { method: "POST", headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body) });
  if (!res.ok) {
    let why = "";
    try { why = (await res.json()).error || ""; } catch (e) {}
    throw new Error(why || "the hub answered " + res.status);
  }
  return res.json();
}

async function paintHears() {
  const field = document.getElementById("d-hears-field");
  const host = document.getElementById("d-hears");
  if (!field || !host) return;
  const card = hearsCardId(current);
  hearsFor = card;
  field.hidden = true;
  if (!card || (typeof isGuest === "function" && isGuest())) return;
  let subs;
  try {
    const res = await plainFetch("/_hub/events/subs?card=" + encodeURIComponent(card));
    if (!res.ok) return;
    subs = (await res.json()).subscriptions || [];
  } catch (e) { return; }
  if (hearsFor !== card) return;
  setHTML(host, hearsHTML(subs));
  document.getElementById("d-hears-form").hidden = true;
  field.hidden = false;
}

function hearsForm() {
  const form = document.getElementById("d-hears-form");
  form.hidden = false;
  const repo = current && /^[\w.-]+\/[\w.-]+$/.test(current.repo || "") ? current.repo : "";
  document.getElementById("d-hears-repo").value = repo;
  document.getElementById("d-hears-branch").value = "main";
  document.getElementById("d-hears-repo").focus();
}

async function hearsAdd() {
  const card = hearsCardId(current);
  if (!card) return;
  const note = document.getElementById("d-hears-note");
  note.textContent = "";
  try {
    await hubSubs({ action: "subscribe", kind: "land", card,
      repo: document.getElementById("d-hears-repo").value.trim(), branch: document.getElementById("d-hears-branch").value.trim() });
  } catch (e) { note.textContent = (e && e.message) || String(e); return; }
  paintHears();
}

async function hearsRemove(id) {
  try { await hubSubs({ action: "unsubscribe", id }); } catch (e) { toast("could not remove it", (e && e.message) || String(e)); }
  paintHears();
}

document.addEventListener("click", e => {
  const b = e.target && e.target.closest && e.target.closest("#d-hears-field [data-act], #d-hears-field [data-hears]");
  if (!b) return;
  if (b.dataset.act === "hear-form") hearsForm();
  else if (b.dataset.act === "unhear") hearsRemove(b.dataset.id);
  else if (b.dataset.hears === "add") hearsAdd();
  else if (b.dataset.hears === "cancel") document.getElementById("d-hears-form").hidden = true;
});
