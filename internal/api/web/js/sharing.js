// ── lending one session ─────────────────────────────────────────────────────
//
// The board over an overlay hands over EVERYTHING: every card, every
// directory, the settings, the file browser. That is right for reaching your
// own board from your own phone and wrong for giving somebody a link.
//
// This is the other shape, and it is a different endpoint rather than an
// option on the same one, because the two are one slip apart. The daemon
// serves a restricted handler on its own share: that card, its terminal, the
// page itself, and 403 for everything else.
//
// Kept as a Map so a menu drawn on a click knows what is already lent without
// asking first. Refreshed on the poll like everything else.
let sharedCards = new Map();

async function loadShares() {
  try {
    const r = await api("/v1/shares");
    const next = new Map();
    (r.shares || []).forEach(s => next.set(s.task_id, s));
    sharedCards = next;
  } catch (e) { /* an older daemon has no shares. not worth saying. */ }
  paintSharing();
}

// WHAT IS PUBLISHED RIGHT NOW, in the header, whenever anything is.
//
// A share is the one piece of atrium's state that is visible from outside this
// machine, and it was the one with no indicator: the only way to find out what
// was published was to right-click each card in turn and read what the menu
// offered. Two shares and one `stop sharing` leaves a live address with nothing
// on screen pointing at it.
//
// In the header rather than the gear, and only when there is something to say,
// for the reason the auto-mode switch is: nothing else tells you, and it is
// true on every tab.
function paintSharing() {
  const el = document.getElementById("sharing");
  if (!el) return;
  const n = sharedCards.size;
  el.hidden = n === 0;
  if (!n) return;
  el.textContent = n === 1 ? "1 session shared" : n + " sessions shared";
  el.title = "click to see what is published, and stop it";
}

// The list, and a way to stop each one.
//
// Every entry can be stopped from here, because the failure this exists for is
// a share whose card you can no longer find: pruned, renamed, or on a tab you
// are not looking at. The token is shown because it is what releases a share by
// hand if one is ever left on the account.
async function openSharing() {
  await loadShares();
  if (!sharedCards.size) {
    tellUser("nothing is shared", "no session is published right now.");
    return;
  }
  const rows = [...sharedCards.values()].map(s => {
    const t = (cards || []).find(c => c.id === s.task_id);
    const name = t ? (t.display_title || t.id) : s.task_id;
    // An address that is recorded but not currently served. The card has no
    // terminal, so the link somebody holds reaches nothing until a runner
    // starts there. Said plainly rather than hidden: it is still an address
    // they have, and it is going to start working again on its own.
    const waiting = s.live === false
      ? `<span class="hintline warn">waiting for a terminal on this card. the address
           is still yours and comes back when one starts.</span>`
      : "";
    return `<div class="sharerow">
      <div class="grow">
        <b>${esc(name)}</b>
        <span class="hintline">${esc(s.mode || "")}${s.token ? " &middot; " + esc(s.token) : ""}</span>
        <div class="copyrow">
          <input readonly value="${esc(s.address || "")}" onclick="this.select()">
          <button onclick="copyText(this, ${
            JSON.stringify(s.address || "").replace(/'/g, "&#39;")})">copy</button>
        </div>
        ${waiting}
      </div>
      <button class="no" onclick="stopSharingById('${esc(s.task_id)}')">stop</button>
    </div>`;
  }).join("");
  tellUser("what is published",
    "Each of these is reachable from outside this machine by anyone holding its " +
    "address, and each SURVIVES A RESTART: the address is reserved, so it comes " +
    "back up with the daemon rather than dying with it. Stopping one is the only " +
    "thing that gives the address up, and it cannot be got back." + rows);
}

// Stopping by id rather than by card, so a share outlives finding its card.
async function stopSharingById(id) {
  try {
    await api(`/v1/tasks/${id}/share`, { method: "DELETE" });
  } catch (e) {
    toast("could not stop the share", e.message);
    return;
  }
  sharedCards.delete(id);
  paintSharing();
  toast("stopped sharing", "the address is released");
  refresh();
}

// What the menu offers for one card: share it, or stop.
// Sharing, as a flyout of the ways out this machine actually has.
//
// One section per overlay, and A SECTION ONLY APPEARS WHEN THAT OVERLAY IS
// READY, which is the same condition the gear uses to decide whether sharing
// is possible at all. Offering a zrok link on a machine with no zrok account
// is offering a button whose only outcome is a paragraph about accounts,
// arriving after a confirmation and a wait.
//
// The mode is chosen HERE rather than in the dialog. Public and private are
// two different things to hand somebody, not two settings of one thing, and a
// select inside a confirmation made them look like a detail of a decision
// already taken.
//
// OpenZiti gets a section and no options in it. Lending one session over ziti
// is not the same shape as a zrok share: there is no link to send, a guest
// needs an identity, and issuing one is the line `docs/overlays.md` says
// atrium does not cross. It is named rather than omitted, because a machine
// with ziti up and a menu that only mentions zrok is a machine whose operator
// has no idea whether they missed a setting.
function shareItem(t) {
  if (!t || !t.supervised) return null;
  const live = sharedCards.get(t.id);
  const ready = k => (overlays || []).some(o => o.kind === k && o.ready);
  const sub = [];

  if (ready("zrok")) {
    sub.push({ head: "with zrok" });
    if (live) {
      // Every share is a zrok share today, so it is listed here. A share will
      // have to say which overlay made it before there is a second kind for
      // this row to be wrong about.
      // The wording is a warning. Stopping is the only thing that gives the
      // address up, and it does not come back, so the row should not read like
      // the reversible half of a toggle. A flyout row cannot carry a help
      // bubble, so the explanation is in the confirmation instead.
      sub.push({ label: "stop sharing, for good", act: () => stopSharing(t) });
    } else {
      sub.push({ label: "a link anyone can open", act: () => confirmShare(t, "public") });
      sub.push({ label: "private, they need zrok too", act: () => confirmShare(t, "private") });
    }
  }
  if (ready("ziti")) {
    sub.push({ head: "with openziti" });
    sub.push({ quiet: "lending one session is not built yet" });
  }
  if (!sub.length) {
    sub.push({ quiet: "no overlay is set up yet" });
    sub.push({ quiet: "see the gear, expose the board" });
  }

  return {
    label: live ? "sharing" : "share this session",
    on: live ? true : undefined,
    help: "Publishes THIS terminal, and nothing else, at its own address. " +
      "The rest of the board, your other sessions and your files are not " +
      "reachable through it.",
    sub
  };
}

// The warning, once the way out has been picked.
//
// Still a stop, because what it says is the part people get wrong: the address
// is the whole credential and there is no watch-only. What it no longer does
// is ask a question that has already been answered.
async function confirmShare(t, mode) {
  const ok = await askUser({
    title: "share " + (t.display_title || "this session") + "?",
    body: "Publishes <b>this terminal only</b>, at an address of its own. The board, " +
      "your other sessions, your files and your settings are not reachable through " +
      "it: the daemon serves a handler that answers for this one card and refuses " +
      "everything else." +
      "<br><br><b>Whoever has the address drives the session.</b> There is no login, " +
      "so the link is the whole credential, and anyone holding it types into this " +
      "terminal as you would. There is no watch-only: it was a checkbox that made " +
      "handing out a link feel safer than it is." +
      (mode === "public"
        ? "<br><br>Anyone who opens the link is in. They need nothing installed."
        : "<br><br>They need zrok themselves, and the command to run rather than a link.") +
      "<br><br><b>The address is durable.</b> It is reserved, unguessable, and this " +
      "card's from now on: it survives a restart, and it comes back when this card " +
      "next has a terminal. Handing it over once is enough. Only stopping the share " +
      "gives it up, and it does not come back. Permission prompts still come to you, " +
      "on this board.",
    buttons: [{ label: "cancel", value: null }, { label: "share it", value: true, style: "go" }]
  });
  if (!ok) return;
  await runShare(t, mode);
}

// ── sharing, narrated ───────────────────────────────────
//
// The POST blocks for however long the zrok instance takes, which is seconds
// on a good day and longer on a bad one. What the operator saw for all of them
// was the button they had pressed, unchanged, and then either an address or a
// paragraph about a 500 from a call they had no idea was being made.
//
// So the wait gets a shape. These are the daemon's real steps, in its order,
// and it broadcasts each one as it reaches it. See `shareStep` in
// `overlay_guest.go`.
//
// THE DIALOG DOES NOT DEPEND ON THE EVENTS. It opens when the request leaves
// and is finished by the response, so a window that receives none of them
// still ends up in the right place, just with less to read on the way. The
// events are what makes a slow step legible, not what makes the flow work.
// PER MODE, because the two do not do the same work. A public share reserves
// a name and a private one has none: it asks for its old token back instead,
// which is not a step, it is a field on the request.
//
// One list for both would mean showing a step that never runs, and the paint
// below ticks everything above the current step, so it would tick a step that
// never happened.
const SHARE_STEPS_BY_MODE = {
  public: [
    ["env", "checking this machine's zrok environment"],
    ["name", "reserving the address"],
    ["create", "asking the zrok instance for the share"],
    ["listen", "opening the listener that answers it"]
  ],
  private: [
    ["env", "checking this machine's zrok environment"],
    ["create", "asking the zrok instance for the share"],
    ["listen", "opening the listener that answers it"]
  ]
};
let SHARE_STEPS = SHARE_STEPS_BY_MODE.public;

const shareDlg = document.getElementById("sharedlg");
// Which card the open dialog is about, how far it has got, and whether it has
// finished. The last one exists because an event can arrive after the answer
// did, and repainting a finished dialog with a spinner would be a lie.
let shareFor = null, shareAt = 0, shareEnded = true;

// Escape does not abandon a share in flight.
//
// There is no way to cancel one: the daemon is inside a call to somebody
// else's API and closing the dialog would not stop it. A share that arrived
// after the operator had dismissed the window would be live, unlisted on this
// screen, and news to them.
shareDlg.addEventListener("cancel", e => { if (!shareEnded) e.preventDefault(); });

function sharePaintSteps(at, bad) {
  setHTML(document.getElementById("share-steps"), SHARE_STEPS.map(([key, label], i) => {
    let cls = "", mark = "&#183;";
    if (i < at) { cls = "ok"; mark = "&#10003;"; }
    else if (i === at && bad) { cls = "bad"; mark = "&#10005;"; }
    else if (i === at) { cls = "now"; mark = `<span class="shspin"></span>`; }
    return `<div class="shstep ${cls}"><span class="mark">${mark}</span>` +
      `<span>${esc(label)}</span></div>`;
  }).join(""));
}

function shareButtons(list) {
  const bar = document.getElementById("share-actions");
  bar.innerHTML = "";
  list.forEach(b => {
    const el = document.createElement("button");
    el.textContent = b.label;
    if (b.style) el.className = b.style;
    el.onclick = b.act;
    bar.appendChild(el);
  });
}

async function runShare(t, mode) {
  shareFor = t.id;
  shareAt = 0;
  shareEnded = false;
  SHARE_STEPS = SHARE_STEPS_BY_MODE[mode] || SHARE_STEPS_BY_MODE.public;
  document.getElementById("share-title").textContent =
    "sharing " + (t.display_title || "this session");
  setHTML(document.getElementById("share-out"), "");
  shareButtons([]);
  sharePaintSteps(0, false);
  if (!shareDlg.open) shareDlg.showModal();

  let out;
  try {
    out = await api(`/v1/tasks/${t.id}/share`, {
      method: "POST",
      body: JSON.stringify({ mode })
    });
  } catch (e) {
    shareFailed(t, mode, e.message);
    return;
  }
  shareEnded = true;
  sharedCards.set(t.id, out);
  showShareLink(t, out);
  refresh();
}

// A failure, attached to the step it happened on.
//
// The message is the product here. zrok's own errors are the usual cause and
// the daemon turns each into a sentence, so it is shown whole rather than
// summarised into "could not share". Which step died is the part that was
// missing: "the instance answered 500" means something different against
// asking for the share than against opening the listener.
function shareFailed(t, mode, why) {
  shareEnded = true;
  sharePaintSteps(shareAt, true);
  setHTML(document.getElementById("share-out"),
    `<div class="shwhy">${esc(why || "it did not say why")}</div>`);
  shareButtons([
    { label: "close", act: () => shareDlg.close() },
    { label: "try again", style: "go", act: () => runShare(t, mode) }
  ]);
}

// The address, big enough to read and one press to copy.
//
// It lands in the dialog that has been open since the button was pressed,
// rather than in a second one. The steps stay above it: they are the answer to
// "what did that just do", and they are worth a glance in the moment the link
// appears as much as they are while it is being waited for.
function showShareLink(t, s) {
  sharePaintSteps(SHARE_STEPS.length, false);
  setHTML(document.getElementById("share-out"),
    `<div class="asktext" style="margin:14px 0 0">` +
    (s.mode === "public"
      ? "Send them this link. It opens straight into the terminal."
      : "They need zrok. This is the command they run.") +
    // A copy button, not just a selected field. This is the whole output of
    // the action and it is going into a message to somebody, and selecting
    // text in a dialog is one fumbled click away from losing the selection.
    `<div class="copyrow">
       <input readonly id="share-link" value="${esc(s.address)}" onclick="this.select()">
       <button class="go" onclick="copyText(this, ${
         JSON.stringify(s.address).replace(/'/g, "&#39;")})">copy</button>
     </div>` +
    `<br><b>Whoever has this drives the session.</b> ` +
    "It survives a restart and stops only when you say so." +
    // The token, because it is what releases the share if one is ever left
    // behind on the account. Small, and only useful in that one moment.
    (s.token ? `<br><span class="hintline">zrok token <code>${esc(s.token)}</code></span>` : "") +
    `</div>`);
  shareButtons([{ label: "done", style: "go", act: () => shareDlg.close() }]);
  setTimeout(() => {
    const el = document.getElementById("share-link");
    if (el) { el.focus(); el.select(); }
  }, 60);
}

// Stopping gives the address up, and that is not reversible.
//
// Asked for now, because it did not used to be worth asking: every address was
// single use and a restart took it anyway, so stopping one cost nothing you
// were not about to lose. A reserved address is the opposite. Somebody is
// holding this link, it survives restarts, and stopping is the one thing that
// destroys it.
async function stopSharing(t) {
  if (!await confirmUser("stop sharing, and give up the address?",
    "This card's link stops working and <b>cannot be got back</b>. The address is " +
    "released from your zrok account, and sharing this card again gives a different " +
    "one that whoever you sent this to will not have." +
    "<br><br>A restart does not do this. If you only want it down for now, leave it: " +
    "it comes back by itself.", "stop it for good")) return;
  try {
    await api(`/v1/tasks/${t.id}/share`, { method: "DELETE" });
  } catch (e) {
    toast("could not stop the share", e.message);
    return;
  }
  sharedCards.delete(t.id);
  toast("stopped sharing", t.display_title || t.id);
  refresh();
}

// Deleting a card, and being clear about what that does and does not touch.
//
// The confusion worth heading off: a card is atrium's record of a session, and
// the session's TRANSCRIPT is Claude Code's, in `~/.claude/projects`. Deleting
// the card leaves that alone, so the conversation is still resumable from the
// command line and `forget a conversation…` is the entry that removes it.
// Saying so here is cheaper than the mistake.
async function deleteCard(id, t) {
  const running = t.supervised || t.pid > 0;
  // TWO THINGS ARE CALLED THE SESSION, and which one you meant decides what
  // this does.
  //
  //   the CARD          atrium's record: its history, its events, everything
  //                     it was told, the icon, the pin.
  //   the CONVERSATION  Claude Code's transcript, a `.jsonl` under
  //                     `~/.claude/projects`. It is what `--resume` reads and
  //                     it outlives the card entirely.
  //
  // Deleting the card and leaving the transcript is the safe half and is
  // almost never what somebody means by "delete this session": the thing comes
  // straight back the next time anything resumes in that directory. So both
  // are offered, and named, rather than one being assumed.
  //
  // What is NOT offered, at any price, is deleting `~/.claude` or the whole
  // project directory under it. That directory holds every conversation ever
  // had in that working directory, most of them belonging to other cards, and
  // one of them is very often the session doing the deleting.
  const conv = String(t.resume_id || "").trim();
  const opts = [{ value: "card", label: "the card only, keeping the conversation" }];
  if (conv) {
    opts.unshift({
      value: "both", label: "the card and its conversation transcript"
    });
  }
  const pick = await askUser({
    title: "delete " + (t.display_title || "this card") + "?",
    body: "<b>The card</b> is atrium's record: its history, its events and everything it was " +
      "told. <b>The conversation</b> is Claude Code's own transcript, the thing " +
      "<code>--resume</code> reads, and it lives outside atrium." +
      (conv ? "<br><br>This card's conversation is <code>" + esc(conv) + "</code>. Deleting " +
        "the card alone leaves it there, still resumable from a terminal."
            : "<br><br>This card has no recorded conversation, so there is nothing but the " +
              "card to delete.") +
      (running
        ? "<br><br><b>Its runner is still going.</b> Deleting the card does not stop it: " +
          "atrium stops knowing about it. Terminate it first if that is what you meant."
        : "") +
      "<br><br>Other conversations in the same directory are never touched.",
    choices: opts,
    buttons: [{ label: "cancel", value: null }, { label: "delete", value: true, style: "danger" }]
  });
  if (!pick) return;

  // The transcript FIRST. Its endpoint hangs off the card, so deleting the
  // card first would leave nothing to ask.
  if (pick === "both" && conv) {
    try {
      await api(`/v1/tasks/${id}/sessions/${encodeURIComponent(conv)}`, { method: "DELETE" });
    } catch (e) {
      toast("could not delete the conversation", e.message + ". the card was left alone.");
      return;
    }
  }
  try {
    await api(`/v1/tasks/${id}`, { method: "DELETE" });
  } catch (e) {
    toast("could not delete it", e.message);
    return;
  }
  // Attached to the one that just went. Nothing to reconnect to, and the
  // remembered card has to go too or the board would spend ninety seconds
  // waiting for a card that no longer exists.
  if (termTask && termTask.id === id) {
    try { localStorage.removeItem("atrium.term"); } catch (err) {}
    closeTerm();
  }
  toast("deleted", t.display_title || id);
  refresh();
}

// Draws a context menu at the pointer. Shared, so the board and the terminal
// list cannot drift apart in how their menus look or behave.
function showMenu(e, items) {
  // Dropping entries leaves separators with nothing between them, or one
  // stranded at an end.
  items = items.filter(Boolean).filter((it, i, all) =>
    !it.sep || (i > 0 && i < all.length - 1 && !all[i - 1].sep));

  cardMenuEl.innerHTML = items.map((it, i) => it.sep
    ? `<div class="sep"></div>`
    // A NUMBER, WITH BOTH WAYS OF CHANGING IT.
    //
    // The rest of this menu is rows you press once. This one is a value you
    // hold down or type into, which is a different shape and cannot be a
    // `label` and an `act`: a submenu of bigger, smaller and reset closed the
    // menu on every press, so nudging a font up four points meant opening the
    // cog four times, and no amount of entries lets you say 22.
    //
    // A `div` rather than a `button`, so the row itself is not clickable and
    // the three controls inside it are. That is also why the handler below
    // scopes to DIRECT children: a nested button with no `data-i` would index
    // the item list with a NaN.
    : it.step
    ? `<div class="stepper" data-i="${i}">
        <span class="steplabel">${esc(it.label)}${
          it.help ? `<span class="help" tabindex="0" data-tip="${esc(it.help)}">?</span>` : ""}</span>
        <button class="stepdown" title="smaller">&minus;</button>
        <input class="stepval" type="number" inputmode="numeric"
          min="${it.min}" max="${it.max}" value="${it.get()}"
          aria-label="${esc(it.label)}">
        <button class="stepup" title="bigger">+</button>
      </div>`
    : `<button class="${it.danger ? "danger" : ""}${it.limited ? " limited" : ""}${
        it.on === undefined ? "" : " toggle" + (it.on ? " on" : "")}"
        data-i="${i}"${it.tip ? ` title="${esc(it.tip)}"` : ""}>${esc(it.label)}${
        // The board's own bubble, not the browser's. A native tooltip arrives
        // a second late, in the operating system's font, at the pointer rather
        // than against the thing it explains, and it is the one part of a menu
        // that cannot be styled at all.
        it.help ? `<span class="help" tabindex="0" data-tip="${esc(it.help)}">?</span>` : ""}${
        // A toggle says what it IS. The label stays put and a lamp beside it
        // carries the state, rather than the label flipping between "auto
        // mode" and "stop auto mode", which asks you to work out which of
        // those two sentences describes now and which describes the click.
        it.on === undefined ? "" : `<span class="lamp">${it.on ? "on" : "off"}</span>`}${
        it.sub ? `<span class="more">&#9656;</span>` : ""}${
        it.note ? `<span class="note">${esc(it.note)}</span>` : ""}${
        // A flyout, drawn inside the row that owns it so hovering across the
        // gap between the two does not close it. Two destinations for one
        // intention: attaching here, or attaching in its own window.
        // A flyout can be grouped. `head` labels a group and `quiet` states
        // something the group cannot do, and NEITHER IS CLICKABLE: only
        // `.subitem` carries a `data-j`, so a heading has nothing to run and
        // cannot be reached by the handler below.
        it.sub ? `<span class="sub">${it.sub.map((s, j) => s.head
          ? `<span class="subhead">${esc(s.head)}</span>`
          : s.quiet
            ? `<span class="subnote">${esc(s.quiet)}</span>`
            : `<span class="subitem" data-i="${i}" data-j="${j}"
                >${s.icon || ""}${esc(s.label)}</span>`).join("")}</span>` : ""}</button>`
  ).join("");
  // DIRECT CHILDREN ONLY. A stepper row holds two buttons of its own, and
  // they carry no `data-i`, so reaching them here would look up `items[NaN]`
  // and take the menu down on a press meant to change a number.
  cardMenuEl.querySelectorAll(":scope > button").forEach(b => b.onclick = ev => {
    ev.stopPropagation();
    // The question mark explains, it does not do. Clicking it used to run the
    // entry, which on a destructive one is the worst possible reading of
    // "tell me more".
    if (ev.target.closest(".help")) return;
    const it = items[Number(b.dataset.i)];
    // A row that only opens a flyout does nothing when pressed. Closing the
    // menu on it would take the choice away at the moment it was offered.
    if (it.sub) return;
    closeCardMenu();
    it.act();
  });
  cardMenuEl.querySelectorAll(".subitem").forEach(s => s.onclick = ev => {
    ev.stopPropagation();
    closeCardMenu();
    items[Number(s.dataset.i)].sub[Number(s.dataset.j)].act();
  });
  // THE MENU STAYS OPEN. Every other row here closes it, because every other
  // row is a decision made once. This one is a number being adjusted, and a
  // control that closes the thing it lives in cannot be pressed twice.
  //
  // The field is read back from the item rather than from the input after
  // every change, since the setter clamps and may refuse: typing 400 has to
  // show what actually happened rather than what was asked for.
  cardMenuEl.querySelectorAll(".stepper").forEach(row => {
    const it = items[Number(row.dataset.i)];
    const input = row.querySelector(".stepval");
    const apply = (n) => {
      it.set(n);
      input.value = it.get();
    };
    row.querySelector(".stepdown").onclick = ev => { ev.stopPropagation(); apply(it.get() - 1); };
    row.querySelector(".stepup").onclick = ev => { ev.stopPropagation(); apply(it.get() + 1); };
    // Clicking into the field, and typing in it, must not reach the document
    // listener that dismisses an open menu, nor the board's own shortcuts.
    input.onclick = ev => ev.stopPropagation();
    input.onkeydown = ev => {
      ev.stopPropagation();
      if (ev.key === "Enter") apply(Number(input.value));
      if (ev.key === "Escape") closeCardMenu();
    };
    // On the way out as well as on Enter, so a value typed and then clicked
    // away from is not silently discarded.
    input.onchange = () => apply(Number(input.value));
  });
  wireFlyouts();

  // Placed after it has a size, so a card near an edge does not open a menu
  // off screen.
  cardMenuEl.style.left = "0px";
  cardMenuEl.style.top = "0px";
  cardMenuEl.classList.add("on");
  const r = cardMenuEl.getBoundingClientRect();
  const x = Math.min(e.clientX, window.innerWidth - r.width - 8);
  const y = Math.min(e.clientY, window.innerHeight - r.height - 8);
  cardMenuEl.style.left = Math.max(8, x) + "px";
  cardMenuEl.style.top = Math.max(8, y) + "px";
  // A flyout opens to the right unless there is no right left. Measured
  // against the widest one this menu holds rather than a guess, so a long
  // destination name does not hang off the screen.
  // A guess, but a generous one. The sharing flyout carries section headings
  // and a full sentence, so the old 190 flipped too late and hung it off the
  // right edge on a window that was only just too narrow.
  const widest = items.reduce((w, it) => it.sub ? Math.max(w, 230) : w, 0);
  cardMenuEl.classList.toggle("flip",
    widest > 0 && Math.max(8, x) + r.width + widest > window.innerWidth);
}

// A flyout that forgives a slip.
//
// The old rule was `button:hover .sub { display: block }`, which is exact and
// unusable: the row is thirty pixels tall, the flyout hangs off its right
// edge, and getting to an entry in it means moving diagonally across a corner.
// Clipping outside for a single frame closed the menu, and the only way to
// work was to trace an L with the pointer.
//
// So closing is DELAYED and opening is not. Two thirds of a second is long
// enough to cross a corner and short enough that a flyout does not follow you
// around the menu. The flyout is drawn INSIDE the row it belongs to, so the
// pointer moving onto it never leaves the row and never starts the timer at
// all: the delay is only for the case where you actually left.
const flyoutGrace = 650;
let flyoutTimer = 0;

function wireFlyouts() {
  cardMenuEl.querySelectorAll("button").forEach(b => {
    if (!b.querySelector(".sub")) return;
    b.addEventListener("pointerenter", () => {
      clearTimeout(flyoutTimer);
      // One at a time. Two open flyouts overlap, and the one underneath is
      // reachable by pointer while being invisible.
      cardMenuEl.querySelectorAll("button.subopen").forEach(o => {
        if (o !== b) o.classList.remove("subopen");
      });
      b.classList.add("subopen");
    });
    b.addEventListener("pointerleave", () => {
      clearTimeout(flyoutTimer);
      flyoutTimer = setTimeout(() => b.classList.remove("subopen"), flyoutGrace);
    });
  });
}

function closeCardMenu() {
  clearTimeout(flyoutTimer);
  // FOCUS DOES NOT GO WITH IT INTO THE HIDDEN MENU.
  //
  // The menu is hidden rather than removed, so a control inside it that had
  // the focus keeps it, and every keystroke after that goes to something
  // nobody can see. Reachable since the stepper: its field and its two buttons
  // are the first things here anybody focuses on purpose.
  //
  // Blurred rather than handed anywhere, because this closes from a click, an
  // escape and a scroll, in five different views. Whatever the page would have
  // focused on its own is a better answer than a guess made here.
  if (cardMenuEl.contains(document.activeElement)) document.activeElement.blur();
  cardMenuEl.classList.remove("on");
  cardMenuEl.querySelectorAll("button.subopen").forEach(b => b.classList.remove("subopen"));
}
document.addEventListener("click", closeCardMenu);
document.addEventListener("keydown", e => { if (e.key === "Escape") closeCardMenu(); });
// Scrolling closes it, because the menu is placed at a point on the page and
// scrolling moves whatever it was placed against.
//
// EXCEPT THE TERMINAL, which is the thing on this page that scrolls all by
// itself. Capture phase catches a scroll on any element, and xterm scrolls its
// viewport on every line a runner prints, so an agent working underneath the
// open menu closed it several times a second. Nothing about the menu's anchor
// moves when the terminal scrolls: it is a box inside the page, not the page.
//
// The tooltip below takes the same exemption for the same reason.
window.addEventListener("scroll", e => {
  if (fromTerminal(e)) return;
  closeCardMenu();
}, true);

// Did this event come from inside the terminal's own scrolling area.
function fromTerminal(e) {
  const t = e.target;
  if (!t || !t.closest) return false;
  return !!t.closest("#t-screen");
}

async function killById(id) {
  const t = await api(`/v1/tasks/${id}`);
  if (!await confirmUser(`terminate ${t.display_title}?`,
    `Stops pid ${t.pid}. Anything it was part way through is lost. ` +
    `The card and its history stay, in <b>dead</b>.`, "terminate", "kill-runner")) return;
  // A failure is a toast. A second dialog after the one you just answered is
  // two dialogs for one decision.
  try { await api(`/v1/tasks/${id}/kill`, { method: "POST" }); }
  catch (e) { toast("could not terminate", e.message); }
  refresh();
}

// The open card, forgotten. Closes the dialog first: leaving it up over a card
// that no longer exists means every control in it now edits nothing.
async function forgetCurrent() {
  if (!current) return;
  const { id, display_title } = current;
  document.getElementById("detail").close();
  await forgetCard(id, display_title);
}

// Deleting one card, as opposed to clearing a whole column.
async function forgetCard(id, title) {
  if (!await confirmUser(`forget ${title}?`,
    "The card and its whole history go. Anything still running is untouched.",
    "forget it")) return;
  try { await api(`/v1/tasks/${id}`, { method: "DELETE" }); }
  catch (e) { toast("could not forget it", e.message); }
  refresh();
}

// Deletes every card in a finished column. Only done and dead offer this; the
// daemon refuses to sweep a shelved card even when asked.
async function pruneColumn(status) {
  const { tasks } = await api("/v1/tasks");
  const n = (tasks || []).filter(t => t.status === status).length;
  if (!n) return;
  const ok = await confirmUser(
    `clear ${n} ${status} card${n === 1 ? "" : "s"}?`,
    "Their history goes with them. Anything still running is untouched.",
    "clear");
  if (!ok) return;
  const res = await api("/v1/tasks/prune", {
    method: "POST", body: JSON.stringify({ statuses: [status] })
  });
  // A toast, not a dialog. You already answered one, and the cards visibly
  // going is the confirmation.
  toast("cleared", `${res.removed} card${res.removed === 1 ? "" : "s"} gone`);
  refresh();
}

// Which columns are collapsed, remembered so the board opens the way you left
// it rather than re-drowning you in the big one every reload.
function foldedColumns() {
  try { return JSON.parse(localStorage.getItem("atrium.folded") || "[]"); }
  catch (e) { return []; }
}

function toggleColumn(id) {
  const folded = foldedColumns();
  const i = folded.indexOf(id);
  if (i >= 0) folded.splice(i, 1); else folded.push(id);
  localStorage.setItem("atrium.folded", JSON.stringify(folded));
  const el = document.querySelector(`.col[data-column="${id}"]`);
  if (el) el.classList.toggle("folded", i < 0);
}

