// The tones and the sound preferences, shared by the board and the phone page so the two read one state: the
// `atrium.sound` entry of this browser's localStorage. No DOM and no other script is needed.

// Each sound is a list of notes: [frequency, startSeconds, durationSeconds,
// gain, waveform]. Synthesised rather than shipped as files so the binary
// stays self-contained.
const SOUNDS = {
  chime:  { label: "chime",       notes: [[880,0,.18,.9,"sine"], [1318,.14,.3,.8,"sine"]] },
  rise:   { label: "rise",        notes: [[523,0,.12,.8,"sine"], [784,.1,.12,.8,"sine"], [1046,.2,.3,.7,"sine"]] },
  ping:   { label: "ping",        notes: [[1046,0,.35,.7,"sine"]] },
  blip:   { label: "blip",        notes: [[660,0,.09,.8,"square"]] },
  knock:  { label: "knock",       notes: [[196,0,.09,1,"triangle"], [196,.15,.09,.9,"triangle"]] },
  bell:   { label: "bell",        notes: [[1318,0,.9,.55,"sine"], [2637,0,.5,.18,"sine"]] },
  sub:    { label: "low thud",    notes: [[110,0,.4,1,"sine"]] },
  alarm:  { label: "alarm",       notes: [[880,0,.1,.9,"square"], [880,.16,.1,.9,"square"], [880,.32,.1,.9,"square"]] },
  drop:   { label: "drop",        notes: [[880,0,.1,.8,"sine"], [440,.09,.28,.8,"sine"]] },
  chirp:  { label: "chirp",       notes: [[1400,0,.06,.6,"sine"], [1800,.05,.08,.5,"sine"]] },
  none:   { label: "silent",      notes: [] }
};

// expiry is how many seconds a desktop notification stays on screen. 0 keeps it
// until it is answered or clicked, which on Windows is what made them pile up.
// debounce holds an alert for a moment so several agents finishing together
// are one alert rather than a burst. Off by default: a delay between something
// needing you and being told is a real cost, and it is only worth paying once
// the pile-up is worse than the wait.
const DEFAULT_PREFS = {
  muted: false, volume: 0.35, input: "chime", perm: "alarm",
  desktop: true, expiry: 30, debounce: 0,
  // A launched card that is stuck: "alert" rings and marks the card, "mark"
  // only marks it, "off" does neither.
  stuck: "alert",
  // A subagent (`atrium:subagent`) raises no toast, desktop
  // notification or sound. Its marks and its toast log line stay. A permission
  // request from it still notifies. See `quietDoer` in notify.js.
  quietDoers: true
};

// ONE ROW PER KIND OF ALERT, each with two switches: an operating system notification and a sound. The tray records
// every kind whatever these say, and the bell's "turn off" and the speaker stay the master switches over all of them.
// A kind absent from `prefs.alerts` takes the default here, so nothing needs migrating. Only a finished turn starts
// without a notification: the sound says it is done, and a question or a permission is the thing worth a banner.
const ALERT_KINDS = [
  { id: "permission", label: "needs permission", note: "an agent is held at a permission request" },
  { id: "blocker", label: "blocked", note: "a launched agent is held at a prompt" },
  { id: "question", label: "asked a question", note: "an agent asked you something and waits for the answer" },
  { id: "ready", label: "finished its turn", note: "an agent is ready for its next instruction", os: false },
  { id: "looksidle", label: "looks idle", note: "the screen went quiet and no turn end arrived" },
  { id: "stuck", label: "stuck or long turn", note: "a launched agent stopped, or one turn has run a long time" },
  { id: "arrived", label: "arrived on the board", note: "a new card appeared" },
  { id: "pulls", label: "review waiting", note: "a pull request is waiting on you" },
  { id: "system", label: "atrium itself", note: "a halt, a stopped share, a fixture or forge problem" }
];

// Whether one kind does one thing (`which` is "os" or "sound"), from a prefs object.
function alertWants(p, cat, which) {
  const k = ALERT_KINDS.find(x => x.id === cat);
  const mine = ((p && p.alerts) || {})[cat] || {};
  if (typeof mine[which] === "boolean") return mine[which];
  return !(k && k[which] === false);
}

function loadPrefs() {
  try {
    return Object.assign({}, DEFAULT_PREFS, JSON.parse(localStorage.getItem("atrium.sound") || "{}"));
  } catch (e) { return Object.assign({}, DEFAULT_PREFS); }
}

// Whether the board is the thing you are looking at right now.
//
// `visibilityState` alone is not that, and believing it was is why a
// notification never arrived while the browser sat behind an editor. It only
// says the tab is the active tab of a window that is not minimised, which
// stays true when the whole browser is buried three windows deep. Focus is the
// missing half: `hasFocus()` is false as soon as any other application has the
// keyboard.
//
// Both are needed. Focus alone would call a background tab of a focused
// browser the foreground.
