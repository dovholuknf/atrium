// Rows shaped like the live hub: /_hub/items answers one ASSIGNMENT per card (item_id, work_id, card, state, note), /_hub/work answers
// one row per BACKLOG id with the eight steps. Nothing here is real data, it reproduces what the operator saw on 2026-10-16: several
// assignments for one backlog id, blocked assignments with an empty note, ended worker and minion cards still "running", no usage.
const NAMES = ["designed", "approved", "building", "tested", "reviewed", "landed", "deployed", "pushed"];
const ago = m => new Date(Date.now() - m * 60000).toISOString();

// backlog id -> { title, at: index of the active step, fin, human: approval gate }
const WORK = {
  "bug-lean-no-status-line": { title: "Lean cards show no status line", at: 2 },
  "rd-test-layout": { title: "Test layout: one place for every suite's fixtures", at: 2 },
  "f-ptyhost-reconnect": { title: "Pty host: reattach terminals after a room restart", at: 2 },
  "rd-item-lifecycle": { title: "Items as the unit of work: step timeline, tokens and cost", at: 1, approval: true },
  "u-minion-cards-hidden": { title: "Hide resting item minions on the board", at: 5 },
  "u-chat-tab": { title: "Chat tab: terminals list beside the phone conversation", at: 4, stale: 3 },
  "f-session-ask": { title: "Hub forge: retry policy for a failed push", at: 2 },
  "f-room-handoff": { title: "Room handoff: move a card between rooms with its folder", at: 2 },
  "f-hub-forge-push": { title: "Hub forge: push claude/main to origin from the hub", at: 0 },
  "u-items-tab": { title: "Items tab on the hub board", at: 6 },
  "r-usage-export": { title: "Usage export per card and turn for the hub", at: 7 },
  "d-readme-links": { title: "README: fix the links to the docs", all: true },
  "d-doc-overhaul": { title: "Doc overhaul: reading order", at: 2 },
};

// [backlog, card, state, age(min), note, owner_kind, owner]
const A = [
  ["bug-lean-no-status-line", "c-bl-worker1", "blocked", 640, "", "session", "orch@room-e"],
  ["bug-lean-no-status-line", "c-bl-minion1", "blocked", 610, "", "session", "orch@room-e"],
  ["bug-lean-no-status-line", "c-bl-worker2", "running", 95, "", "session", "orch@room-e"],
  ["bug-lean-no-status-line", "c-bl-relay", "running", 90, "", "session", "orch@room-e"],
  ["rd-test-layout", "c-rt-worker1", "blocked", 700, "", "session", "orch@room-e"],
  ["rd-test-layout", "c-rt-worker2", "blocked", 180, "", "session", "orch@room-e"],
  ["f-ptyhost-reconnect", "c-pty-builder", "blocked", 41, "Windows named pipes cannot be reopened by name after the host exits. Fall back to a fresh host (terminals lose their scrollback), or keep a tiny stub process that holds the pipe open?", "human", "operator"],
  ["rd-item-lifecycle", "c-lc-designer", "blocked", 12, "", "human", "operator"],
  ["u-minion-cards-hidden", "c-mc-worker", "running", 900, "", "session", "orch@room-e"],
  ["u-minion-cards-hidden", "c-mc-minion", "running", 900, "", "session", "orch@room-e"],
  ["u-chat-tab", "c-chat-builder", "running", 70, "", "session", "orch@room-e"],
  ["u-chat-tab", "c-chat-reviewer", "running", 6, "", "session", "orch@room-e"],
  ["f-hub-forge-push", "", "queued", 20, "", "session", "orch@room-e"],
  ["f-session-ask", "c-sa-builder", "blocked", 25, "Retry a failed push three times, or surface it to the orchestrator after the first failure?", "session", "orch@room-e"],
  ["f-room-handoff", "c-rh-builder", "running", 130, "", "session", "orch@room-e", "room-a"],
  ["d-readme-links", "c-dr-writer", "finished", 400, "done: 1a2b3c4", "session", "orch@room-e"],
  ["r-usage-export", "c-ue-builder", "finished", 300, "done: 9f3c2a1", "session", "orch@room-e"],
  ["u-items-tab", "c-it-builder", "finished", 1500, "done: c19f7aa", "session", "orch@room-e"],
  ["u-items-tab", "c-it-reviewer", "finished", 1450, "done: c19f7aa", "session", "orch@room-e"],
  ["d-doc-overhaul", "c-do-writer", "finished", 2900, "cancelled by the operator", "session", "orch@room-e"],
];

const rows = A.map((a, n) => ({ item_id: "5f0c8a1e-0000-4000-8000-0000000000" + String(n).padStart(2, "0"), work_id: a[0], room: a[7] || "room-e", card: a[1], state: a[2],
  revision: 2, generation: 1, owner_kind: a[5], owner: a[6], note: a[4], created_at: ago(a[3] + 30), updated_at: ago(a[3]) }));

const work = Object.entries(WORK).map(([id, w]) => ({ backlog_id: id, title: w.title, state: w.all ? "finished" : "running",
  steps: NAMES.map((s, i) => {
    const st = w.all ? "done" : i === w.stale ? "stale" : i < (w.at || 0) ? "done" : i === w.at ? "active" : "pending";
    return { backlog_id: id, step: s, state: st, party: w.approval && s === "approved" ? "human" : "", cards: "[]", started_at: st === "pending" ? "" : ago(800), sha: "" };
  }) }));

// The cards the board holds. The overnight worker, minion and relay cards are gone (ended and removed), so their chips draw struck through.
const live = ["c-bl-worker2", "c-pty-builder", "c-lc-designer", "c-chat-builder", "c-chat-reviewer", "c-sa-builder"];
const tasks = live.map(id => ({ id, room: "room-e", wire_name: id.replace(/^c-/, ""), title: id, status: "running", tags: [], terminal: true }))
  // A card on a room that is offline: the board still holds it, flagged offline. Its state is unknown, not dead.
  .concat([{ id: "c-rh-builder", room: "room-a", wire_name: "rh-builder", title: "c-rh-builder", status: "running", tags: [], offline: true }]);

module.exports = { rows, work, tasks, NAMES, ago };
