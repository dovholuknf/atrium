// ── has the operator seen this turn ─────────────────────
//
// Two marks on a card, and the one thing only a browser can tell the room.
// See docs/seen-design.md.
//
// THE MARKS. A card whose latest turn nobody has seen wears a dot. A card whose
// turn ended on Open Questions nobody has answered wears `? 3`. Both are chips,
// not columns: they say something about the card and never move it.
//
// THE REPORT. The room sees a keystroke, a prompt, a message. It cannot see
// whether a window is in front of somebody, so this watches for it: the
// runner's terminal for that card, in a visible and focused window, attached,
// scrolled to the bottom, held for `SEEN_DWELL_MS`. Then it says so once, naming
// the turn it was showing, and the room marks that turn seen and no newer one.
//
// The same rule on a phone and in a popped-out window. Each is its own
// document with its own visibility and focus, so a popped-out window behind
// the board does not count, which is the case that most needs to be right.

const SEEN_DWELL_MS = 3000;
const SEEN_TICK_MS = 500;

// The unread dot and the questions chip, drawn on a board card and on a
// terminal strip row.
function seenChips(t) {
  const s = t && t.seen;
  if (!s) return "";
  let out = "";
  if (s.unseen) {
    out += `<span class="chip unseen"
      title="this session's last turn ended and nobody has looked at it since. attach and read it, or type to it, and this clears"
      >&#9679;</span>`;
  }
  const qs = s.open_questions || [];
  if (s.answered === false && (qs.length || s.questions_unparsed)) {
    const tip = qs.length
      ? "its last turn asked you:\n" + qs.map((q, i) => (i + 1) + ". " + q).join("\n") +
        "\n\nclears when you reply to it"
      : "its last turn asked you questions atrium could not read. clears when you reply to it";
    out += `<span class="chip warn questions" title="${esc(tip)}"
      >?${qs.length ? " " + qs.length : ""}</span>`;
  }
  return out;
}

// The card the pane is showing, as the last list drew it. The pane's own copy
// is from when it attached, and a turn that ended since is only on the list.
function seenCardShown() {
  if (!termTask) return null;
  const list = (typeof lastTasks !== "undefined" && lastTasks) || [];
  const id = termTask.id;
  const bare = typeof bareId === "function" ? bareId(id) : id;
  return list.find(x => x.id === id) ||
    list.find(x => (typeof bareId === "function" ? bareId(x.id) : x.id) === bare) ||
    termTask;
}

// Every condition at once, right now. A reason string when it fails, so a
// test can say which one did.
function seenBlockedBy() {
  if (document.visibilityState !== "visible") return "hidden";
  if (!document.hasFocus()) return "unfocused";
  const view = document.getElementById("terms");
  if (!view || view.hidden) return "not the terminals view";
  if (!term || !termTask) return "no terminal";
  if (typeof termKind !== "undefined" && termKind !== "runner") return "a shell";
  if (!termSock || termSock.readyState !== WebSocket.OPEN) return "not attached";
  if (typeof termListOpen !== "undefined" && termListOpen &&
      typeof termNarrow === "function" && termNarrow()) return "switcher open";
  const b = term.buffer && term.buffer.active;
  if (!b || b.viewportY < b.baseY) return "scrolled up";
  return "";
}

// What the dwell is timing: one card's one turn. A new turn, another card, or a
// condition dropping starts it over.
let seenDwellKey = "";
let seenDwellSince = 0;
// Turns already reported, so one is said once however long it stays on screen.
const seenReported = new Set();

function seenTick(now) {
  const t = seenCardShown();
  const s = t && t.seen;
  if (!s || !s.unseen || !s.turn_ended_at || seenBlockedBy()) {
    seenDwellKey = "";
    return;
  }
  const key = t.id + "@" + s.turn_ended_at;
  if (seenReported.has(key)) return;
  if (key !== seenDwellKey) {
    seenDwellKey = key;
    seenDwellSince = now;
    return;
  }
  if (now - seenDwellSince < SEEN_DWELL_MS) return;
  seenReported.add(key);
  seenDwellKey = "";
  reportSeen(t.id, s.turn_ended_at);
}

// A report that fails leaves the dot on, which is the safe way to be wrong, and
// forgets it was sent, so the next full dwell tries again.
async function reportSeen(id, turnEndedAt) {
  try {
    await api(`/v1/tasks/${encodeURIComponent(id)}/seen`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ turn_ended_at: turnEndedAt }),
    });
  } catch (e) {
    seenReported.delete(id + "@" + turnEndedAt);
    return;
  }
  if (typeof refreshSoon === "function") refreshSoon();
}

setInterval(() => seenTick(Date.now()), SEEN_TICK_MS);
