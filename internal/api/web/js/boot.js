// ── boot ────────────────────────────────────────────────────────────────────
//
// LAST IN THE FILE, and it has to stay last.
//
// A `function` declaration is hoisted and callable from anywhere, but `let` and
// `const` are not: they exist from the top of the scope and THROW until the
// line that declares them has run. So boot code sitting above a `let` calls a
// function that reads it and dies with `Cannot access 'waitingFor' before
// initialization`, which is a real error thrown into a promise nobody was
// awaiting, so the restore stopped dead and the rest of the page carried on
// looking fine.
//
// Starting the page from the bottom means every declaration above it has run.
// What the board is wearing, before anything else, and in EVERY window rather
// than only the board. A popped-out terminal has its own document and its own
// body, so it wears the skin or it does not, and one window in the old colours
// beside one in the new reads as a bug in both.
bootSkin();

// The brought themes, in every window and for the same reason as the skin: a
// popped-out terminal has its own document and its own copy of the table, and
// one window in somebody's own colours beside one in atrium's reads as a bug in
// both. Not awaited, because the shipped themes are already here and a card
// wearing a brought one repaints when this lands rather than waiting on it.
loadThemes().then(() => { if (termTask) previewTheme(termTask.theme || ""); });

// AM I A GUEST, asked before anything is drawn.
//
// Started here rather than awaited here. A popped-out or lent terminal has to
// get its claim in and its socket open, and the answer only gates what may
// POST a file, which is minutes away from boot in human terms. The board waits
// for it, because the whole question there is whether to draw a board at all.
guestKnown = askIfGuest().then(word => { guestWord = word; return word; });

if (termOnly()) {
  // The same stream and the same interval as the board. `refresh` sends a
  // popped-out window down `soloRefresh`, so this costs one card's worth of
  // polling rather than a second board's.
  bootTerminalOnly().then(() => {
    connect();
    setInterval(refresh, 5000);
  });
} else {
  bootBoard();
}

// The board, once it is known to be a board.
//
// A lent session serves this same file at `/`, so the address a guest was given
// WITHOUT its `#term=` fragment lands here. Everything the board polls is
// refused, every list comes up empty, and what is on screen is an atrium with
// nothing in it. Drawing that and then apologising is worse than not drawing
// it, so the question is settled first.
async function bootBoard() {
  if (await guestKnown) { showGuestPage(); return; }
  connect();
  // THE ROLL CALL GOES FIRST, AND THE BOARD WAITS FOR THE ANSWERS.
  //
  // A board that starts after a window was already popped out has missed the
  // claim, so it asks. Windows that are gone do not answer and the board rings
  // for their cards, which is the right way round to be wrong.
  //
  // What was wrong was doing anything before the answers came back. The very
  // first poll is where the board decides what to alert about and the restore
  // decides what to attach to, and both ran with `soloHeld` still empty. So a
  // restart, which reloads every board and every popped-out window at once,
  // ended with the board ringing for cards their own windows were already
  // ringing for, and taking a terminal back into the pane that was open in a
  // window of its own.
  //
  // A broadcast round trip between two documents in one browser is well under
  // a frame. Half a second is many times that, and it is paid once at load.
  if (soloBus) soloBus.postMessage({ type: "solo-who" });
  setTimeout(() => {
    refresh();
    restoreWhereYouWere();
    // WHERE THIS VISIT STARTED, recorded once so it can be returned to.
    //
    // Without it the first `navPush` finds no state of ours and REPLACES the
    // entry rather than pushing, which is right for not stranding whatever was
    // in this tab before atrium and wrong for the board: the view somebody
    // opened on would never have had an entry of its own, so the first press
    // of back would step straight over it.
    //
    // Inside the timeout and after the restore, because that is what decides
    // where the page actually landed. Recording it earlier would name the
    // board this page passes through on its way to the session you were
    // reading yesterday.
    if (!termOnly() && !(history.state && history.state.atrium)) {
      history.replaceState(navState(), "");
    }
  }, soloRollCall);
  setInterval(refresh, 5000);
  // Every other way this page goes away: a manual reload, a close, a
  // navigation. `pagehide` rather than `unload`, which a browser is free to
  // skip when it freezes a page into the back/forward cache.
  addEventListener("pagehide", rememberWhereYouAre);
}

// What a lent session says at its bare address.
//
// The daemon's own sentence, and then the one thing the guest can act on: the
// fragment is missing. `#term=<card>` never reaches the server, so a link that
// lost it is indistinguishable to atrium from a link that never had it, and
// this page is the only place that can say so.
//
// Nothing is polled and nothing is connected after this. Every request the
// board makes is refused here, and a page retrying them every five seconds
// would be a share with a heartbeat and nothing to show for it.
function showGuestPage() {
  document.title = "atrium: one terminal";
  document.body.classList.add("guestonly");
  const box = document.getElementById("guestonly");
  const word = document.getElementById("guest-word");
  if (word) word.textContent = guestWord;
  if (box) box.hidden = false;
}
