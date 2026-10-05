// ── resuming says it is opening ─────────────────────────────────────────
// Shared by the board (js/card-menu.js) and the phone page (m/js/home.js), so the state, the wording, the bound and
// the launch body are one copy. Holds no DOM: a change is announced on `window` as `resume-opening`, and each page
// paints it its own way.
//
// A resume is `opening` from the click until the terminal's first output, not until `/v1/launch` answers. Before this
// there was nothing in between, which read as a click that did nothing, and the answer to that is a second click.
//
// Keyed by bare id, because a room flip respells the id and it is the same card.
//   opening   launching or waiting for output. A second resume only points here.
//   slow      nothing after RESUME_BOUND_MS, or the launch failed. Says why, and a resume is allowed again, since the
//             first one may be dead.
let RESUME_BOUND_MS = 30000;   // a `let` so the headless tests can shorten it
const resumeOpening = new Map();

const openingText = "opening the conversation";

function openingKey(id) {
  const s = String(id);
  const i = s.indexOf("~");
  return i > 0 ? s.slice(i + 1) : s;
}

function openingState(id) { return resumeOpening.get(openingKey(id)) || null; }

// What the pane or the row says for this card, or nothing.
function openingSay(id) {
  const o = openingState(id);
  if (!o) return "";
  return o.state === "slow" ? o.why : openingText + "…";
}

function openingChanged(id) {
  window.dispatchEvent(new CustomEvent("resume-opening", { detail: { id: openingKey(id) } }));
}

function openingStart(id) {
  openingEnd(id, true);
  const o = { state: "opening", why: "", timer: 0 };
  o.timer = setTimeout(() => openingSlow(id,
    `no output after ${Math.max(1, Math.round(RESUME_BOUND_MS / 1000))} seconds. a large conversation can take this long, ` +
    `and it may still appear. resume again if it does not`), RESUME_BOUND_MS);
  resumeOpening.set(openingKey(id), o);
  openingChanged(id);
}

// The launch failed or the bound passed: the spinner becomes the reason.
function openingSlow(id, why) {
  const o = openingState(id);
  if (!o) return;
  clearTimeout(o.timer);
  o.state = "slow";
  o.why = why;
  openingChanged(id);
  openingClearToast();
}

// The terminal printed, the card went away, or a new attempt replaces this one.
function openingEnd(id, quiet) {
  const o = openingState(id);
  if (!o) return;
  clearTimeout(o.timer);
  resumeOpening.delete(openingKey(id));
  openingClearToast();
  if (!quiet) openingChanged(id);
}

// The "already opening" notice, so a failure that follows can take it down. The board's toast is dismissed through its
// own hook, and the phone page has no toast at all. See openingPointToast in js/card-menu.js.
function openingClearToast() {
  if (typeof openingToastGone === "function") openingToastGone();
}

// What the launch is sent as. The same for the board and the phone.
function resumeLaunchBody(id, t, resume, full) {
  return {
    harness: t.runner || "claude",
    cwd: t.worktree || "",
    resume,
    // AS HELD, `room~id` and all: the hub routes by the tag and strips it on the way into the room. See
    // docs/fabric/card-room-routing.md.
    task_id: id,
    // Onto the same card, so the title and everything else on it stay put. Sending them again would let a stale copy
    // of the row overwrite what the card says now.
    title: "", why: "", prompt: "",
    // Left out, the card decides. False wins over a lean card's tag.
    ...(full ? { lean: false } : {})
  };
}

function resumeFailText(e) { return "could not start it: " + (e && e.message || e); }

// Why a card cannot be resumed from what the card itself says, or "". The board adds the runner's configuration to it.
function cannotResumeCard(t) {
  if (t.supervised) return "it is already running";
  if (!t.resume_id) {
    return "atrium never learned this session's resume id, so there is no " +
      "conversation to pick up. its session hooks were not wired when it ran, " +
      "or its harness does not report one";
  }
  if (!t.runner) return "atrium does not know which runner this was";
  return "";
}
