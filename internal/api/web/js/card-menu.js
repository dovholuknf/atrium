// ── card action menu ────────────────────────────────────
// Right click a card. Each entry previously cost opening the detail dialog,
// reading it, and closing it again.
const cardMenuEl = document.getElementById("cardmenu");

// Why a shelved card cannot be started again, or "" when it can.
//
// Both answers come off the card itself: `resume_id` is the session's own id
// for its conversation, and `runner` is which harness to start. Neither needs
// asking the daemon.
function cannotResume(t) {
  if (t.supervised) return "it is already running";
  if (!t.resume_id) {
    return "atrium never learned this session's resume id, so there is no " +
      "conversation to pick up. its session hooks were not wired when it ran, " +
      "or its harness does not report one";
  }
  if (!t.runner) return "atrium does not know which runner this was";
  // Resuming is per-runner configuration. Saying which runner and which field
  // turns "it will not resume" into something the operator can fix.
  const h = allHarnesses.find(x => x.id === t.runner);
  if (h && !(h.resume_args || []).length) {
    return `${t.runner} has no resume arguments configured. set them on the ` +
      `runner, with {resume} where the session id goes`;
  }
  return "";
}

// Whether resume is worth offering at all: there is a conversation to pick up
// and nothing already running it.
const canResume = (t) => !!t.resume_id && !t.supervised;

// A session that has only just come up, as opposed to one that finished a turn.
//
// Both land in the same column and the column is right about both: each is
// sitting at its prompt waiting for you. What differs is what it has already
// done, and saying "finished its turn" about a session launched ten seconds
// ago claims work that never happened.
//
// The daemon decides this, not a stopwatch here. Elapsed time cannot tell a
// session that came up and has been ignored for an hour from one that worked
// for an hour and then stopped.
const justStarted = (t) => t.waiting_reason === "started";

// A session that put a QUESTION to you, as opposed to one that ran out of
// things to do.
//
// This is the distinction the board could not make: everything landed in
// `ready` and read the same, whether somebody was blocked on your answer or an
// agent had simply finished.
//
// TWO SOURCES, and the card asked if either says so, because they know
// different halves of it:
//
//   - `waiting_reason` is INFERRED. Asking is a tool call, so the permission
//     hook sees it by name before the turn ends. It knows that a question was
//     asked and never what it was.
//   - `ask` is WRITTEN, by the session itself with `atrium ask`. It carries
//     the question.
//
// In the data rather than in the stylesheet, because everything else that
// wants the difference reads it from here: the sort that ranks a question
// above a card that merely stopped, and the alert that names what it is
// interrupting you for.
const wasAsked = (t) => t.waiting_reason === "asked" || (!!t.ask && !t.ask_peer);

// The question went to another SESSION rather than to you.
//
// Still stopped, still counted, still on the board. What changes is who owes
// it an answer, which decides whether it is yours to act on and is the whole
// reason the peer's name is stored beside the question.
const askedAPeer = (t) => !!t.ask && !!t.ask_peer;

// How far up a list of waiting cards a question puts one. Lower sorts first.
const askRank = (t) => wasAsked(t) ? 0 : askedAPeer(t) ? 1 : 2;

function readyBecause(t) {
  if (askedAPeer(t)) return `asked ${t.ask_peer} and is waiting on the answer`;
  if (wasAsked(t)) return "asked you a question and is waiting on the answer";
  return justStarted(t)
    ? "just came online and is waiting for its first instruction"
    : "finished its turn and wants your next instruction";
}

// Takes a card off the shelf, and puts it where the truth is.
//
// Unshelving does two things: it lifts the standing block on the card's
// requests, and it starts the runner. When the second cannot happen there is
// no process, so `running` would be a card describing something that is not
// there. It goes to `finished` instead, which is what it is, and can be
// launched fresh or forgotten from there.
async function unshelveCard(id) {
  await patchTask(id, { status: "running" });
  refresh();
}

// Starts a finished card's runner again, onto the same card.
//
// The launch form rather than a silent relaunch, because resuming asks the same
// questions launching does: which runner, in which directory. It arrives
// pre-filled from the card and with the resume box already ticked.
function resumeCard(id, t, where) {
  const why = cannotResume(t);
  if (why) { toast("cannot resume", why); return; }
  return resumeNow(id, t, where);
}

// Resuming, without asking anything.
//
// The launch form used to open pre-filled, which meant every resume was a
// dialog whose every field was already correct and whose only useful control
// was the button at the bottom. Resuming is not a decision: the runner, the
// directory and the conversation all come off the card, and `cannotResume`
// has already established that all three are known.
//
// The form is still there for starting something, where those three ARE the
// decision.
// Which conversation, when a directory has more than one.
//
// A card carries the last session atrium saw on it, and that is the right
// default and not the whole truth: a directory accumulates conversations, and
// the one worth picking up is often not the most recent. `claude --resume`
// asks in a terminal; this asks here.
//
// ONE conversation is not a question, and asking it would be the same dialog
// with the same single answer every time. Zero is not a question either: the
// card's own resume id is all there is, and it is what resume already used.
async function pickSession(id, t) {
  let list = [];
  try {
    list = (await api(`/v1/tasks/${id}/sessions`)).sessions || [];
  } catch (e) {
    // The listing is a convenience over the card's own resume id, so a failure
    // to read it falls back to that rather than stopping the resume.
    return t.resume_id || "";
  }
  if (list.length <= 1) return (list[0] && list[0].id) || t.resume_id || "";

  const pick = await askUser({
    title: "which conversation?",
    body: `${list.length} in this directory. The one marked <b>current</b> is what ` +
      `this card would resume on its own.`,
    value: (list.find(s => s.current) || list[0]).id,
    choices: list.map(s => ({
      value: s.id,
      label: `${s.title}  ·  ${firstSeen(s.at)}  ·  ${bytes(s.bytes)}` +
        (s.current ? "  ·  current" : "")
    })),
    buttons: [{ label: "cancel", value: null }, { label: "resume it", value: true, style: "go" }]
  });
  if (pick === null) return null;
  return String(pick).trim();
}

// Throwing a conversation away.
//
// A directory collects them, and most are two exchanges and an `/exit`. They
// cost nothing but they make the list above hard to read, which is the only
// reason this exists.
//
// The transcript is the conversation: deleting the file is the whole
// operation, and nothing atrium holds refers to it except a card's resume id.
// A card pointing at one that has gone falls back to starting fresh, which is
// what it did before the conversation existed.
async function forgetSessions(id, t) {
  let list = [];
  try {
    list = (await api(`/v1/tasks/${id}/sessions`)).sessions || [];
  } catch (e) {
    toast("could not read them", e.message);
    return;
  }
  if (!list.length) { toast("nothing to forget", "no conversations here"); return; }

  const pick = await askUser({
    title: "forget which conversation?",
    body: "The transcript is deleted. Anything still running is untouched, and " +
      "a card pointing at it starts fresh next time instead.",
    value: (list.find(s => !s.current) || list[0]).id,
    choices: list.map(s => ({
      value: s.id,
      label: `${s.title}  ·  ${firstSeen(s.at)}  ·  ${bytes(s.bytes)}` +
        (s.current ? "  ·  current" : "")
    })),
    buttons: [{ label: "cancel", value: null },
              { label: "forget it", value: true, style: "no" }]
  });
  if (pick === null) return;

  const gone = list.find(s => s.id === pick);
  // Named in the confirmation rather than shown as a uuid, because the id is
  // not what anybody recognises it by.
  if (!await confirmUser(`forget "${(gone && gone.title) || pick}"?`,
    "The transcript goes. This cannot be undone.", "forget it")) return;

  try {
    await api(`/v1/tasks/${id}/sessions/${encodeURIComponent(pick)}`, { method: "DELETE" });
  } catch (e) {
    toast("could not forget it", e.message);
    return;
  }
  toast("forgotten", (gone && gone.title) || pick);
}

async function resumeNow(id, t, where) {
  // Which conversation, when the directory has more than one. Returns the
  // card's own resume id without asking when there is nothing to choose
  // between, and null when the choice was cancelled.
  const resume = await pickSession(id, t);
  if (resume === null) return;

  let task;
  try {
    task = await api("/v1/launch", {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        harness: t.runner || "claude",
        cwd: t.worktree || "",
        resume: resume,
        task_id: id,
        // Onto the same card, so the title and everything else on it stay put.
        // Sending them again would let a stale copy of the row overwrite what
        // the card says now.
        title: "", why: "", prompt: ""
      })
    });
  } catch (e) {
    tellUser("could not resume it", e.message);
    return;
  }
  if (!task || !task.supervised) { refresh(); return; }
  if (where === "window") { popOutTask(task.id); return; }
  switchView("terms");
  openTerm(task);
}

// Whether a terminal window can be opened on the desktop, and the command that
// would do it.
//
// A MACHINE SETTING, so it comes off `/v1/settings` rather than off the card:
// every card in the same daemon gets the same answer. Empty means the operator
// has not configured one, and the entry is absent rather than dimmed, because
// there is nothing on a card that could fix it.
//
// A failure to read it reads as empty, which is also the right answer for the
// one caller that fails: a guest holding a lent session is refused
// `/v1/settings` outright, and a guest must not be offered a terminal on
// somebody else's desktop.
async function machineTerminal() {
  try {
    return ((await api("/v1/settings")).terminal_command || "").trim();
  } catch (e) {
    return "";
  }
}

// Opens this card's directory in a terminal window, ON THE MACHINE THE SESSION
// RUNS ON.
//
// The same correction the file open needs and more load-bearing here: this does
// not open a terminal on the machine holding the browser. The daemon runs the
// command, so the window appears wherever the daemon is, which is where the
// files are. A terminal is exactly the thing somebody on a laptop reading a
// board over a share would expect to get locally, so the toast says which
// machine it went to.
async function openDesktopTerminal(id) {
  try {
    await api(`/v1/tasks/${id}/open-terminal`, { method: "POST" });
  } catch (e) {
    toast("could not open a terminal", e.message);
    return;
  }
  toast("opening a terminal", "on the machine this card's directory is on");
}

function unshelveItem(id, t) {
  const why = cannotResume(t);
  return {
    label: why ? "unshelve" : "unshelve and resume",
    note: why ? "cannot resume" : "",
    limited: !!why,
    tip: why
      ? "The card comes off the shelf and stops refusing its requests, but " +
        "nothing starts, so it lands in finished: " + why
      : "Starts the runner again from where the conversation left off.",
    act: () => unshelveCard(id)
  };
}

async function cardMenu(e, id) {
  // WITH TEXT SELECTED, THIS MENU STANDS ASIDE. Both buttons.
  //
  // Left click opens nothing, because a click is how a sweep across a card
  // ends and a menu over the top of the thing you just highlighted is the drag
  // bug arriving by another route.
  //
  // Right click opens NOTHING OF ATRIUM'S, which is the point: returning
  // before `preventDefault` lets the browser's own menu through, and that is
  // the one with Copy on it. A card menu that has no copy entry of its own,
  // covering a selection, is the board taking the clipboard away at the exact
  // moment somebody reached for it.
  //
  // So a card is two things depending on whether anything is highlighted: with
  // nothing selected it is a control, and both buttons open this. With
  // something selected it is text, and both buttons belong to the browser. One
  // click on empty space collapses the selection and it is a control again.
  //
  // Both surfaces, because the stack draws the same card as a row and opens
  // the same menu from it, and a title or a path is as worth copying there.
  //
  // Scoped to the card under the pointer rather than to the page. A selection
  // left lying in a terminal or a dialog must not stop a card three columns
  // away from opening, which is what testing the whole document would do.
  if (selectionTouches(e.target.closest(".card, .stackrow"))) return;
  e.preventDefault();
  e.stopPropagation();
  // The card, and whether this machine has a terminal command. Together rather
  // than one after the other: the menu is drawn on a click and the settings
  // read is not worth a second beat of waiting.
  const [t, termCmd] = await Promise.all([
    api(`/v1/tasks/${id}`),
    machineTerminal()
  ]);
  // Once, and then held. The menu is drawn on a click and a round trip here
  // would open it a beat late every time.
  if (!allActions.length) await loadActions();

  // A card that is finished has nothing to terminate, and offering it produced
  // a dialog that failed and changed nothing. Auto mode and shelving are the
  // same: they answer future requests from a session that will not make any.
  const over = ["done", "dead", "shelved"].includes(t.status);

  // A card is a place as much as a session: it carries a worktree, and that
  // outlives whatever ran in it. So the menu is ordered by what you came to
  // do. Look at it, get to its terminal, run something in its directory, then
  // change what it is.
  // Auto mode as it actually behaves, not as this card is configured.
  //
  // The board-wide switch and the per-card one both end in approve, so a card
  // reading `off` while the header said APPROVING EVERYTHING was the menu
  // describing a field rather than an outcome. The click still toggles this
  // card's own setting, which is the only one it owns.
  const autoOn = !!t.auto_approve || !!globalAuto;

  // Where a terminal opens. The same two destinations whether it is one that
  // already exists or one this menu is about to start, so they are written
  // once: two entries offering different words for the same pair is how they
  // drift.
  const openIn = (go) => [
    { label: "in terminals", act: () => go("here") },
    { label: "in its own window", icon: popIcon(), act: () => go("window") }
  ];

  const items = [
    // One entry, two ways to open it. Attaching and popping out are the same
    // intention with a different destination, so they are one row that says
    // where rather than two rows that both say attach.
    t.supervised ? {
      label: "attach terminal",
      sub: openIn(where => where === "window" ? popOutTask(id) : attachTask(id))
    } : null,
    // Beside attach rather than under it, because it is a different terminal
    // and not a different way of opening the same one. The wording changes on
    // whether there is one already: opening and returning are different
    // promises, and offering "open" for something already open is how you end
    // up believing you have two.
    t.supervised ? {
      label: t.shell ? "go to its shell" : "open a shell here",
      act: () => openShellFor(id)
    } : null,
    // Offered only when there is one to close. `exit` in the shell does the
    // same thing, and this is for the case where it is open on a card you are
    // not looking at: a shell holds a process and a working directory, and the
    // idle sweep will not get to it for half an hour.
    t.shell ? { label: "close its shell", act: () => closeShellFor(id) } : null,
    { sep: true },
    // Running something in this card's directory, which is the answer to
    // "adopt": a session atrium started in a terminal cannot be taken over,
    // because a pty cannot be adopted and `docs/supervision-design.md` records
    // that there is no reattach on Windows. What CAN happen is a new runner,
    // owned by atrium, in the same place and onto the same card.
    //
    // Both entries need a directory and nothing else, so they are offered on
    // every card that has one, whatever state it is in. A card with a worktree
    // and no session is exactly the case this is for.
    // Starting something in this card's directory.
    //
    // The wording changes when one is already running, because so does the
    // consequence. With nothing there this puts a runner on THIS card. With a
    // session already going it cannot, so it opens a second card in the same
    // directory: a different conversation, the same files, two agents editing
    // them. That is occasionally what you want and never what you want by
    // accident, so the label says NEW and the tip says the rest.
    t.worktree ? {
      label: t.supervised ? "start a new session here" : "start a session here",
      note: t.supervised ? "a second one, alongside" : "",
      help: t.supervised
        ? "One is already running here. This starts a SECOND runner in the same " +
          "directory, on its own card and its own conversation. Both will be " +
          "editing the same files."
        : "Starts a runner in this card's directory, onto this card. A fresh " +
          "conversation, not the old one.",
      // Where it lands is decided when you start it, not afterwards. Landing
      // somewhere and then being moved is two decisions for one intention.
      sub: openIn(where => openLaunch(t.runner || null, "", t.worktree,
        t.supervised ? null : id, null, where))
    } : null,
    // Only when it can. An entry that says "cannot resume" underneath itself
    // is a menu explaining why it is there, which is a question it raised.
    // In practice this is a card whose session is over and whose directory is
    // still on disk, which is the case worth having.
    t.worktree && canResume(t) && !cannotResume(t) ? {
      label: "resume the conversation here",
      help: "Starts a runner in this card's directory, picking the recorded " +
        "conversation back up where it stopped. Nothing to fill in: the " +
        "runner, the directory and the conversation all come off the card.",
      sub: openIn(where => resumeCard(id, t, where))
    } : null,
    // Only where there is a directory to have collected any. Reading them is
    // one request and the menu is drawn on a click, so it is not done here to
    // decide whether to offer this.
    t.worktree ? {
      label: "forget a conversation…",
      help: "A directory collects transcripts, most of them two exchanges and " +
        "an exit. Deleting one only removes the transcript.",
      act: () => forgetSessions(id, t)
    } : null,
    // Reading one later is the other half of collecting them, and the reduced
    // shape is the reason: the transcript as written is not something anybody
    // reads. `saveSession` lives with the file download it works like.
    t.worktree ? {
      label: "save a conversation…",
      help: "Writes one to a file: what you and the agent said as markdown, " +
        "or the whole transcript exactly as the runner wrote it.",
      act: () => saveSession(id, t)
    } : null,
    // A REAL TERMINAL WINDOW, on the desktop, beside the board. Not a pane:
    // `wt.exe` makes its own window and returns at once, so atrium cannot
    // supervise it and does not try.
    //
    // Offered only when the operator has configured a command, and only on a
    // card with a directory to open. The note says which machine, because that
    // is the part a board read over a share gets wrong.
    t.worktree && termCmd ? {
      label: "open in a terminal window",
      note: "on atrium's machine",
      help: "Runs " + termCmd + " with this card's directory. The window opens " +
        "on the machine atrium is on, which is the machine holding these files, " +
        "not the one showing this board. atrium does not watch it.",
      act: () => openDesktopTerminal(id)
    } : null,
    { sep: true },
    actionItems(t),
    shareItem(t),
    { sep: true },
    // Where the card goes, and where it sits. These two are what dragging was,
    // and they are here rather than in the settings dialog because a column is
    // a bucket of attention and this menu is what you already have open when
    // you decide a card belongs in another one.
    moveItem(id, t),
    nudgeItems(id),
    { label: t.pinned ? "unpin" : "pin to the top", act: () => togglePin(id, !t.pinned) },
    // A toggle, drawn as one. It reads as a state you are looking at rather
    // than a verb you are about to perform, which matters most in the case
    // where the answer is already yes and the menu said "stop auto mode",
    // a phrase you have to reason backwards from to learn what is on.
    // Not pressable while the board-wide switch is on.
    //
    // Every request from this card is already being approved, so setting this
    // card's own switch changes nothing you can observe: the same requests get
    // the same answer for a different reason. An entry that reports a state
    // and does nothing when pressed is worse than one that says why it cannot,
    // so it says why.
    over ? null : globalAuto ? {
      label: "auto mode", on: true, note: "board-wide", limited: true,
      help: "The board-wide switch is approving everything, including this " +
        "card, so this card's own switch has nothing to decide. Turn the " +
        "board-wide one off in the header first.",
      act: () => toast("already approving everything",
        "the board-wide switch covers this card. turn it off in the header first")
    } : {
      label: "auto mode",
      on: !!t.auto_approve,
      help: t.auto_approve
        ? "Approving without asking. Everything is still recorded."
        : "Approve this card's requests without asking. A standing never rule " +
          "and a shelved card still win.",
      act: () => patchTask(id, { auto_approve: !t.auto_approve }).then(refresh)
    },
    // Shelving stops a runner, so it needs a runner atrium owns. Shown and
    // dimmed rather than hidden: the entry disappearing on some cards and not
    // others is a rule you have to infer, and the reason is one line.
    //
    // Unshelving is never dimmed, whatever the card is. A shelved card refuses
    // every request its session makes and has to be liftable wherever you find
    // it. Only its second half, starting the runner again, can fail.
    t.status === "shelved" ? unshelveItem(id, t)
      : over ? null
        : t.supervised
          ? { label: "shelve", note: "stops its runner",
              act: () => patchTask(id, { status: "shelved" }).then(refresh) }
          : { label: "shelve", note: "atrium does not own this process",
              limited: true,
              tip: "Shelving stops the runner, and atrium can only stop one it " +
                "started. This session is running somewhere else.",
              act: () => toast("cannot shelve",
                "atrium does not own this process, so it cannot stop it") },
    // MAY ANOTHER SESSION TYPE INTO THIS TERMINAL.
    //
    // On by default, because the operator's position is that the pty is
    // shared between himself and the agents, and a switch that has to be
    // turned on before two agents can talk is one nobody turns on. This is
    // for the exclusions, and a card lent over a share is the one that
    // matters: the guest holds that terminal and was handed exactly one
    // session.
    //
    // Only on a card atrium owns a terminal for. Everywhere else a peer
    // message is queued whatever this says, so offering it would be a switch
    // that changes nothing.
    t.supervised ? {
      label: "peers may type here", on: t.peer_typing !== false,
      help: "Another session's message goes into this terminal when nobody is " +
        "typing in it, marked with who it came from. A part-written line is " +
        "never interrupted, and with this off everything is queued instead.",
      act: () => patchTask(id, { peer_typing: t.peer_typing === false }).then(refresh)
    } : null,
    // ANSWERED IN THE TERMINAL, which atrium cannot see.
    //
    // Every other way a question comes off a card delivers text to the
    // session: saying something to it, sending its note, or the session
    // declaring its work over. None of those is available to somebody who
    // already replied by typing, which is how anybody sitting in front of a
    // session replies. So the question stayed open, and since nothing expires
    // one and the board falls back to the ask field when no waiting reason is
    // set, every later turn-end on that card announced itself as a question.
    (t.asks_open || 0) ? {
      label: t.asks_open === 1 ? "dismiss the question" : `dismiss ${t.asks_open} questions`,
      note: "says nothing to it",
      help: "Takes the outstanding questions off this card without sending it " +
        "anything. For the ones you already answered by typing in its terminal, " +
        "which atrium cannot see.",
      act: () => dismissAsks(id)
    } : null,
    { sep: true },
    // Everything about what this card IS, in one dialog. Last, because it is
    // the one entry you go into rather than press: the ones above are single
    // actions and this opens a room.
    { label: "settings…", act: () => openTask(id) },
    { sep: true },
    t.pid > 0 && !over
      ? { label: "terminate", danger: true, act: () => killById(id) } : null,
    // Deleting the CARD. The endpoint has been there since the beginning and
    // nothing in the page reached it, so the only way to get rid of one was to
    // wait for the sweep or to use curl.
    //
    // Last, under `terminate`, and marked dangerous. It is the one entry here
    // that destroys something rather than changing it.
    { label: "forget this session…", danger: true, act: () => deleteCard(id, t) }
  ];
  showMenu(e, items);
}

