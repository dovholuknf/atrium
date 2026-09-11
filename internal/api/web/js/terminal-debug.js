// ── temporary: who moved the viewport ───────────────────
//
// TEMPORARY AND MEANT TO BE DELETED. Three fixes have been aimed at a terminal
// that jumps to the bottom and the operator has watched all three fail, which
// means the mechanism is not the one being guessed at.
//
// Guessing stops here. xterm fires an event every time the viewport moves, so
// what is recorded is WHAT ATRIUM DID MOST RECENTLY, and the two are printed
// together. That turns "it jumped" into "it jumped 4ms after output arrived",
// which names the cause instead of narrowing the field.
//
// Quiet unless asked for, because this fires on every line of output from
// every runner:
//
//     localStorage.atriumScroll = "1"   in the console, then reload
let lastScrollAct = "none", lastScrollActAt = 0;
function noteScrollAct(what) {
  lastScrollAct = what;
  lastScrollActAt = Date.now();
}
function scrollDebugOn() {
  try { return localStorage.getItem("atriumScroll") === "1"; } catch (e) { return false; }
}
function watchScroll(t) {
  if (!scrollDebugOn() || !t || !t.onScroll) return;
  // TEN FRAMES IS CHROME'S DEFAULT and it cut the stack one frame short of the
  // answer, twice. The chain into a scroll is public API, then core, then
  // BufferService, then three emitters, so the caller is always further down
  // than the default allows.
  try { Error.stackTraceLimit = 60; } catch (e) {}
  // THE BUFFER moving, which happens BEFORE the element follows. The frames
  // here are the ones that matter: they name whatever inside xterm decided to
  // scroll, which the element's own setter cannot say because by then the
  // decision has already been made.
  t.onScroll(y => {
    const b = t.buffer.active;
    console.debug("[atrium] viewport ->", y,
      "| baseY", b.baseY, "| fromBottom", b.baseY - y,
      "| after", lastScrollAct, (Date.now() - lastScrollActAt) + "ms",
      "\n  " + callerFrames(14));
  });
  watchViewportElement();
}

// TELLING JAVASCRIPT APART FROM THE BROWSER.
//
// Every probe so far watched JavaScript, and every one came back empty:
// nothing calls a scroll function, and both of xterm's `scrollToBottom` paths
// are gated on `scrollOnUserInput`, which is off. That leaves the possibility
// no JS instrument can see, which is the browser moving `scrollTop` itself
// when an element is focused.
//
// So the element's own `scrollTop` setter is wrapped. A write through
// JavaScript goes through it and gets a stack. A write by the browser does
// NOT, and shows up only as a scroll event with no preceding setter call. Those
// two need completely different fixes, and this is the only way to tell them
// apart from inside the page.
// The call stack as one line, minus this file's own frames.
//
// `console.trace` writes a collapsed group, and a collapsed group is not in the
// text somebody copies out of a console, which is why four rounds of asking for
// a stack produced none.
function callerFrames(n) {
  const raw = (new Error().stack || "").split("\n").slice(2);
  return raw
    .filter(l => !/callerFrames|watchViewportElement/.test(l))
    // The emitter plumbing is noise and it is DEEP: xterm forwards a scroll
    // through three nested emitters, so the frames that name the cause sit
    // below all of them and a short stack shows only the forwarding.
    .filter(l => !/EventEmitter\.fire/.test(l))
    .slice(0, n)
    .map(l => l.trim())
    .join("\n  ");
}

function watchViewportElement() {
  const vp = document.querySelector("#t-screen .xterm-viewport");
  if (!vp) {
    console.debug("[atrium] no .xterm-viewport to watch");
    return;
  }
  const proto = Object.getPrototypeOf(vp);
  const desc = Object.getOwnPropertyDescriptor(Element.prototype, "scrollTop") ||
    Object.getOwnPropertyDescriptor(proto, "scrollTop");
  if (!desc || !desc.set) {
    console.debug("[atrium] cannot wrap scrollTop on this browser");
    return;
  }
  let lastSetAt = 0;
  Object.defineProperty(vp, "scrollTop", {
    configurable: true,
    get() { return desc.get.call(this); },
    set(v) {
      lastSetAt = Date.now();
      // The frames INLINE rather than through `console.trace`, which files them
      // in a collapsed group that never survives being copied out of a console.
      console.debug("[atrium] JS set scrollTop ->", v, "| was", desc.get.call(this),
        "\n  " + callerFrames(4));
      desc.set.call(this, v);
    }
  });

  vp.addEventListener("scroll", () => {
    const sinceSet = Date.now() - lastSetAt;
    const max = vp.scrollHeight - vp.clientHeight;
    const top = desc.get.call(vp);
    console.debug("[atrium] scrolled to", top, "of", max,
      "| bottom?", max - top <= 4,
      "| by", sinceSet < 50 ? "JAVASCRIPT" : "THE BROWSER",
      "| after", lastScrollAct, (Date.now() - lastScrollActAt) + "ms");
  }, true);

  // The textarea, because the pin is meant to stop it being somewhere that
  // needs scrolling to. If this reports anything other than the top left, the
  // pin did not take and that is the whole answer.
  const ta = document.querySelector("#t-screen .xterm-helper-textarea");
  if (ta) {
    const cs = getComputedStyle(ta);
    console.debug("[atrium] helper textarea at", cs.top, cs.left,
      "| inline", ta.style.top, ta.style.left);
    ta.addEventListener("focus", () => {
      const cs2 = getComputedStyle(ta);
      console.debug("[atrium] TEXTAREA FOCUSED | at", cs2.top, cs2.left,
        "| scrollTop", desc.get.call(vp), "of", vp.scrollHeight - vp.clientHeight);
    }, true);
    ta.addEventListener("blur", () => {
      console.debug("[atrium] TEXTAREA BLURRED | scrollTop", desc.get.call(vp));
    }, true);
  } else {
    console.debug("[atrium] no helper textarea found");
  }

  // Anything else in the pane that takes focus. `document` rather than the
  // pane, so a focus landing outside it is visible too: that is what happens
  // when the find box or the console takes it.
  document.addEventListener("focusin", e => {
    const t = e.target;
    console.debug("[atrium] focusin ->", t && (t.className || t.nodeName),
      "| scrollTop", desc.get.call(vp));
  }, true);
}

function holdScrollAt(fromBottom) {
  holdFrom = fromBottom;
  holdUntil = Date.now() + holdScrollFor;
  if (holdTimer) return;
  const tick = () => {
    if (!term || Date.now() > holdUntil) {
      clearInterval(holdTimer);
      holdTimer = 0;
      return;
    }
    const b = term.buffer.active;
    const want = Math.max(0, b.baseY - holdFrom);
    // Only when something else moved us. Writing the position we are already
    // at every frame would fight a scroll the operator is in the middle of.
    if (b.viewportY !== want) term.scrollToLine(want);
  };
  holdTimer = setInterval(tick, 30);
  tick();
}

// A deliberate scroll ends the hold. Whatever the operator does with the wheel
// or the scrollbar wins immediately over a position atrium is preserving on
// their behalf.
function releaseScrollHold() { holdUntil = 0; }

function sendSignal(s) { send({ t: "signal", s }); }

// Asks the attached runner to quit.
//
// The keystrokes are the runner's own, set on its harness, because there is no
// common answer: a shell takes `exit`, claude takes control-d twice, ollama and
// codex take it once. Sending the wrong one leaves it running.
async function exitTerm() {
  if (!termTask) return;
  const t = termTask;
  if (!await confirmUser(`ask ${t.display_title} to exit?`,
    "Sends whatever this runner is configured to quit on, then waits. " +
    "If it ignores that, its terminal is closed and the process is stopped." +
    "<br><br>Its card and history stay, in <b>finished</b>.",
    "ask it to exit", "exit-terminal")) return;
  try { await api(`/v1/tasks/${t.id}/exit`, { method: "POST" }); }
  catch (e) { toast("could not exit", e.message); return; }
  toast("asked to exit", t.display_title);
}

// The runner is gone. Keeps the scrollback, drops every claim that it is live.
function markTermDead() {
  const pane = document.querySelector(".term-pane");
  if (pane) pane.classList.add("dead");
  const title = document.getElementById("t-title");
  if (title && termTask) title.textContent = termTask.display_title + " (exited)";
  // The pid is gone and the runner chip reads as running, so neither belongs.
  const chips = document.getElementById("t-chips");
  if (chips) chips.innerHTML = `<span class="chip warn">exited</span>`;
  document.getElementById("term-perm").hidden = true;

  // Asking a dead terminal to exit, or detaching from it, are both nonsense.
  // Replaced with the one thing left to do: clear it away. Kept rather than
  // cleared automatically, because the scrollback holds the exit and the
  // resume id, and taking that off screen the instant it appears is worse than
  // a pane you have to dismiss.
  const bar = document.querySelector(".term-bar");
  if (bar) {
    bar.querySelectorAll("button").forEach(b => {
      const keep = b.id === "t-copymode";
      b.hidden = !keep;
    });
    if (!bar.querySelector(".t-close")) {
      const close = document.createElement("button");
      close.className = "go t-close";
      close.textContent = "close";
      close.title = termOnly()
        ? "close this window. its card and history stay on the board"
        : "clear this away. its card and history stay on the board";
      // Through the same two routes as the countdown. A user gesture does not
      // rescue `window.close()`: what the browser checks is whether a script
      // opened this window, not whether somebody clicked.
      close.onclick = () => termOnly() ? closeThisWindow(close) : forgetTerm();
      bar.appendChild(close);
    }
  }
  // A popped-out window has nothing else in it, so an exited runner leaves a
  // window showing one dead terminal and no way to anything. It closes itself.
  //
  // Not instantly. The scrollback holds the exit and, for claude, the resume
  // id, which is the one thing worth reading off a session that has ended, and
  // a window that vanishes the moment a runner quits takes that with it. Long
  // enough to see it and to press cancel by moving the pointer there.
  //
  // `window.close` works because the board opened this window. A tab the
  // operator opened by hand is not script-closable, which is why this offers
  // rather than assumes: the countdown is a button that says what it is about
  // to do, so nothing is silently relying on it.
  if (termOnly()) offerSoloClose();
  // Its card is dead now, so the board and the session list have to agree.
  refresh();
}

// The seconds before a popped-out window closes itself after its runner exits.
//
// Short. The window is empty and you are looking straight at it, so the wait
// is the cost and the button is the escape hatch, not the countdown.
const soloCloseAfter = 2;
function offerSoloClose() {
  const bar = document.querySelector(".term-bar");
  if (!bar || bar.querySelector(".t-autoclose")) return;
  let left = soloCloseAfter;
  const btn = document.createElement("button");
  btn.className = "no t-autoclose";
  btn.title = "stop this window closing itself";
  bar.appendChild(btn);
  const paint = () => { btn.textContent = `closing in ${left}s. stay open`; };
  paint();
  const tick = setInterval(() => {
    left--;
    if (left > 0) { paint(); return; }
    clearInterval(tick);
    closeThisWindow(btn);
  }, 1000);
  btn.onclick = () => {
    clearInterval(tick);
    btn.remove();
  };
}

// Closes a popped-out window, by whichever of the two routes works.
//
// BOTH ARE TRIED because neither is reliable alone, and the failure mode of
// each is silence. `window.close()` is refused unless the browser agrees this
// script opened this window, and a window opened from a pasted URL never
// qualifies. The board closing it through the handle `window.open` returned
// always qualifies, and needs a board to be open.
//
// The broadcast goes FIRST, because it is the one that works in the case that
// was failing, and a successful close makes everything after it moot.
//
// If both miss, the countdown is replaced by a button that says so. A window
// that announced it was closing and then sat there is worse than one that
// never offered, because it reads as the whole feature being broken rather
// than as this browser refusing this one call.
function closeThisWindow(btn) {
  if (soloBus && soloID) soloBus.postMessage({ type: "solo-close", task: soloID });
  try { window.close(); } catch (e) {}
  // Still here a moment later means neither route landed. There is no event
  // for "close was refused", so this is measured rather than caught.
  setTimeout(() => {
    if (!btn || !btn.isConnected) return;
    btn.textContent = "close this window";
    btn.title = "this browser will not let the page close itself, and no board " +
      "window is open to close it. the runner has already exited.";
    btn.onclick = () => window.close();
  }, 700);
}

function closeTerm(switching) {
  clearTermPane(switching);
  if (!switching) refresh();
}

// Tears the pane down without refreshing, so it can be called from inside one.
function clearTermPane(switching) {
  const was = termTask ? termTask.id : "";
  if (termSock) { termSock.close(); termSock = null; }
  // Before the terminal goes, so the disposable is not left holding a
  // reference to a disposed one.
  if (termData) { try { termData.dispose(); } catch (e) {} termData = null; }
  if (term) { term.dispose(); term = null; }
  termFit = null;
  termTask = null;
  // Nothing attached, so nothing for the bridge to span.
  placeTabBridge();
  // NOT FORGOTTEN HERE. This runs whenever the pane is torn down, and most of
  // those are not decisions: the daemon restarting, a poll that has not seen
  // the fixture come back yet, a socket that dropped. Forgetting on any of
  // them is what made the restore useless across exactly the restart it was
  // written for. `closeTerm` forgets, because closing it is something you did.
  if (switching) return;
  const pane = document.querySelector(".term-pane");
  if (pane) pane.classList.remove("dead");
  // Undo what markTermDead did to the bar, so the next attach gets its own
  // buttons back rather than a stranded close.
  const bar = document.querySelector(".term-bar");
  if (bar) {
    bar.querySelectorAll("button").forEach(b => { b.hidden = false; });
    const stale = bar.querySelector(".t-close");
    if (stale) stale.remove();
  }
  paintPaneBg(null);
  document.getElementById("t-title").textContent = "nothing attached";
  document.getElementById("t-chips").innerHTML = "";
  document.getElementById("t-screen").innerHTML = "";
  document.getElementById("term-perm").hidden = true;

  // AND THEN GO BACK AND WAIT FOR IT.
  //
  // This is the other half of surviving a restart, and it is the half that was
  // missing. Only one of the two paths through an outage ends in a page
  // reload: the board reloads when the daemon serves a build it has not seen,
  // and when the build is unchanged nothing reloads at all. In that case the
  // socket retry is the only thing trying, and it stops the moment this
  // function disposes the terminal it was checking for. So the pane sat empty
  // next to a session that had come back, and the way to get it was to click
  // the card.
  //
  // Safe to fire on every teardown: `waitAndAttach` gives up on a card that is
  // archived, popped out or replaced by something you attached yourself, and
  // the deliberate close forgets the card first so there is nothing to wait
  // for.
  if (was) {
    const kept = localStorage.getItem("atrium.term");
    rlog("pane torn down. was", was, "remembered", kept || "(nothing)");
    if (kept === was) waitAndAttach(was);
  }
}

// Closing it yourself, which is the one teardown that means "stop showing me
// this". Everything else is something that happened TO the session.
function forgetTerm() {
  try { localStorage.removeItem("atrium.term"); } catch (e) {}
  closeTerm();
}

// A permission request while attached: the runner is blocked and its terminal
// shows nothing, so the question has to appear where you are already looking.
async function renderTermPerm() {
  const host = document.getElementById("term-perm");
  if (!termTask) { host.hidden = true; return; }
  let mine = [];
  try {
    mine = ((await api("/v1/permissions")).permissions || [])
      .filter(p => p.task_id === termTask.id);
  } catch (e) { return; }
  if (!mine.length) { host.hidden = true; host.innerHTML = ""; return; }
  const p = mine[0];
  host.hidden = false;
  host.innerHTML = `
    <div class="who">
      <b>this session is blocked, waiting on you</b>
      <code>${esc(p.tool)}: ${esc(p.command)}</code>
    </div>
    <div class="actions">
      <button class="go" onclick="decideFromTerm('${p.id}','approve')">approve once</button>
      <button class="no" onclick="decideFromTerm('${p.id}','block')">block once</button>
      <button class="to-perms" onclick="switchView('perms')"
        title="to set a standing rule for this">open in perms</button>
    </div>`;
}

async function decideFromTerm(id, decision) {
  let reason = "";
  if (decision === "block") {
    reason = await askText("why not?", "Handed back to the agent as the reason.", "",
      "use a temp directory instead");
    if (reason === null) return;
  }
  try {
    await api(`/v1/permissions/${id}/decide`, {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ decision, reason })
    });
  } catch (e) { toast("too late", e.message); }
  renderTermPerm();
}

// The terminals view refreshes its own two pieces.
async function renderTerms() {
  await renderTermList();
  await renderTermPerm();
}

