// ── a card's parent: the card that launched it, and drives it ──
//
// Launching from one card nests the new one under it, and that is not always who drives it. The room does the change
// (`POST /v1/tasks/{id}/parent`), so every launcher-reading path agrees. See `ChangeParent` in internal/store/a2a.go.

// The room's other cards a card could nest under: same room, a handle to be known by, and not itself.
function parentChoices(t) {
  return (typeof lastTasks === "undefined" ? [] : lastTasks)
    .filter(c => roomOf(c.id) === roomOf(t.id) && !sameCard(c.id, t.id) && c.wire_name)
    .map(c => ({ value: bareId(c.id), label: (c.display_title || c.title || c.wire_name) + "  ·  " + c.wire_name }));
}

function parentMenuItem(t) {
  if (!t) return null;
  return {
    label: "change parent…", note: t.spawned_by && t.spawned_by !== "@human" ? t.spawned_by : "",
    help: "Nests this card under the card that drives it. Its reports, stop notices and owed-report counts go to " +
      "the new parent. A parent set when it was launched to report to a named card is replaced.",
    act: () => changeParent(t.id, t)
  };
}

async function changeParent(id, t) {
  const choices = [{ value: "", label: "(no parent)" }].concat(parentChoices(t));
  const pick = await askUser({
    title: "change parent",
    body: "Reports, stop notices and owed-report counts of this card go to the new parent. " +
      "A card cannot nest under one it launched.",
    value: "", size: 8, choices,
    buttons: [{ label: "cancel", value: null }, { label: "change it", value: true, style: "go" }]
  });
  if (pick === null) return;
  try {
    await api(`/v1/tasks/${encodeURIComponent(id)}/parent`, {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ parent: String(pick).trim() })
    });
  } catch (e) {
    toast("could not change the parent", e.message);
    return;
  }
  refresh();
}
