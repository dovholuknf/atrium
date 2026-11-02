// ── the new item button ──────────────────────────────────────────────────
//
// A chat bubble on the backlog pane for filing, editing or removing one hub backlog item without the orchestrator,
// whose start costs far more context than the job needs. The button launches a throwaway superlean card whose whole
// context is NEWITEM_BRIEF, and the panel is the phone's conversation view of it (`/m/?chat=1#term=<id>`), as the chat
// tab is. Closing the panel asks the card to exit, and a throwaway card takes its directory and itself with it.
//
// HUB ONLY, like the backlog it writes. The card starts on the room the board is scoped to, else the room this board
// last launched on, else the first room, and its id is tagged with that room so the frame routes to it.

const NEWITEM_BRIEF = `# New item

You file, edit or remove ONE item in the atrium backlog on the hub, for the human typing to you. Nothing else.

- Ask what the item is if they have not said. Keep questions few and short.
- To file: pick a department (read existing ones with atrium_backlog list), write a one-line title and a short
  markdown body that says what and why, and a priority only if they gave one. Show the human the title and body in one
  message and file it with atrium_backlog file once they agree. Say the id it got.
- To edit or remove: find it with atrium_backlog list or get. Removing is atrium_backlog status dropped. A change of
  wording is filed as a new item and the old one dropped, since there is no edit of a body.
- Never read or change code, never launch anything, never use atrium_done or atrium_blocked.
- When the human says they are finished, call atrium_exit.
`;

let newItemCard = "";

function newItemRoom() {
  const scoped = typeof roomNow === "function" ? roomNow() : "";
  if (scoped) return scoped;
  const rooms = typeof hubRooms === "undefined" ? [] : hubRooms;
  const last = typeof lastLaunchRoom === "function" ? lastLaunchRoom() : "";
  if (rooms.some(r => r.name === last)) return last;
  return rooms.length ? rooms[0].name : "";
}

async function newItemOpen() {
  const panel = document.getElementById("newitem-panel");
  const note = document.getElementById("newitem-note");
  panel.hidden = false;
  if (newItemCard) return;
  note.hidden = false;
  note.textContent = "Starting the item agent...";
  const room = newItemRoom();
  const headers = { "Content-Type": "application/json" };
  if (room) headers["X-Atrium-Room"] = room;
  let task;
  try {
    task = await api("/v1/launch", {
      method: "POST", headers,
      body: JSON.stringify({
        harness: "claude", title: "new item", throwaway: true, superlean: true, model: "sonnet",
        tags: ["atrium:new-item"], brief: NEWITEM_BRIEF,
        prompt: "Read BRIEF.md. Then ask me, in one short line, what item to file, edit or remove."
      })
    });
  } catch (e) {
    note.textContent = "The item agent did not start: " + e.message;
    return;
  }
  let id = String((task && task.id) || "");
  if (!id) { note.textContent = "The item agent did not start: the room named no card."; return; }
  if (room && !roomNow() && id.indexOf("~") < 0) id = room + "~" + id;
  newItemCard = id;
  note.hidden = true;
  const f = document.createElement("iframe");
  f.setAttribute("aria-label", "new item conversation");
  f.src = "/m/?chat=1#term=" + encodeURIComponent(id);
  document.getElementById("newitem-frame").replaceChildren(f);
}

async function newItemClose() {
  const id = newItemCard;
  newItemCard = "";
  document.getElementById("newitem-panel").hidden = true;
  document.getElementById("newitem-frame").replaceChildren();
  if (!id) return;
  try {
    await api("/v1/tasks/" + encodeURIComponent(id) + "/exit", { method: "POST" });
  } catch (e) {
    tellUser("the item agent is still running", e.message);
  }
}
