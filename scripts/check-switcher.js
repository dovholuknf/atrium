// The switcher's invariants, checked against the real file.
//
// The switcher is one keystroke and a list, and almost everything that can go
// wrong with it is invisible in a browser you are not using. Two shapes of
// failure, and each rule below names the one it prevents:
//
//   - The KEYSTROKE. A browser that refuses `preventDefault` says nothing, so
//     a handler wired the wrong way works on the machine it was written on and
//     nowhere else.
//   - The CLAIM. A popped-out window that moves to another card has to hand
//     the old one back. Claims are fifteen second heartbeats, so getting this
//     wrong self-heals, which means the symptom is a flicker somebody
//     diagnoses as anything but this.
//
// A rule that fails here is not style. It is the bug, back.
const fs = require("fs");

const html = fs.readFileSync(process.argv[2], "utf8");
let bad = 0;

function fail(msg) {
  console.error("FAIL: " + msg);
  bad++;
}

// The whole feature is optional in the sense that a file without it should not
// fail this check. Everything below is guarded on the switcher existing.
if (!/function openSwitcher\(/.test(html)) {
  console.log("no switcher in this board, so there is nothing to check.");
  process.exit(0);
}

// Rule 1: the hotkey listens in the CAPTURE phase and stops there.
//
// Two calls, two audiences. `preventDefault` is aimed at the browser.
// `stopPropagation` is aimed at xterm, which listens on its own textarea: an
// event allowed through opens the switcher AND sends a control character to
// whatever the runner is in the middle of. Capture is what puts this ahead of
// xterm at all.
const hookAt = html.indexOf("if (comboOf(e) !== switchKey()) return;");
if (hookAt < 0) {
  fail("nothing compares a keystroke against `switchKey()`. Either the hotkey is gone, or " +
    "it is matching a hardcoded key, which is the one thing this feature may not do: the " +
    "default has to be rebindable because which key a browser keeps differs per browser.");
} else {
  const handler = html.slice(hookAt - 400, hookAt + 400);
  if (!/e\.preventDefault\(\)/.test(handler) || !/e\.stopPropagation\(\)/.test(handler)) {
    fail("the switcher hotkey does not both preventDefault and stopPropagation. Without the " +
      "second one xterm sees the keystroke too, so opening the switcher also types a control " +
      "character into a running session.");
  }
  if (!/\}, true\);/.test(html.slice(hookAt, hookAt + 600))) {
    fail("the switcher hotkey is not registered in the capture phase. xterm's own listener is " +
      "on the terminal's textarea, which is the event target, so a bubble-phase listener " +
      "arrives after the runner already got the keystroke.");
  }
}

// Rule 2: ctrl-k is not the default.
//
// It is the binding everybody means, and it is the one that cannot be the
// default here: ctrl-k is readline's kill-line, and this board's whole content
// is terminals. Taking it means quietly breaking a keystroke every shell on
// the machine has. It stays available to anybody who binds it on purpose.
const def = /const SWITCH_KEY_DEFAULT = "([^"]+)"/.exec(html);
if (!def) {
  fail("there is no SWITCH_KEY_DEFAULT. The binding is a setting with a default, not a constant.");
} else if (def[1] === "ctrl+KeyK") {
  fail("ctrl-k is the default binding. It is readline's kill-line, so this takes a keystroke " +
    "away from every session on the board. Leave it as something to opt into.");
}

// Rule 3: moving a popped-out window hands the old card back.
//
// The board believes a card is popped out until the claim on it goes quiet.
// A window that switches without releasing leaves the board refusing to attach
// to a card nothing is showing, and "raising" a window that has moved on.
const swAt = html.indexOf("async function soloSwitch(");
if (swAt < 0) {
  fail("there is no soloSwitch. A popped-out window IS one card, so switching inside one is " +
    "the half of this feature that is not just a list.");
} else {
  const body = html.slice(swAt, swAt + 2400);
  if (!/type: "solo-release"/.test(body) || !/type: "solo-claim"/.test(body)) {
    fail("soloSwitch does not both release the old claim and claim the new card. Skipping the " +
      "release leaves the board believing a card is popped out in a window that has moved on.");
  }
  // The window NAME is how the board finds a window it did not open.
  // `reopenByName` looks for `atrium-term-<id>`, so a window showing a card
  // under another card's name is a window the board opens a SECOND copy of.
  if (!/window\.name = "atrium-term-"/.test(body)) {
    fail("soloSwitch does not rename the window. The board finds windows it did not open by " +
      "`atrium-term-<id>`, so a stale name means it opens a second window on a card this one " +
      "is already showing.");
  }
  if (!/poppedOut\(id\)/.test(body)) {
    fail("soloSwitch does not refuse a card that already has a window. Two views onto one " +
      "terminal both taking input is the situation docs/supervision-design.md says nothing " +
      "arbitrates, and the board refuses it in attachTask for the same reason.");
  }
}

// Rule 4: a solo window writes down the OTHER windows' claims.
//
// It ignores them as alerts, which is right, and that is what the code used to
// do with them entirely. Rule 3's refusal can only be answered by a window
// that kept a ledger, and it must never count its own claim or it refuses to
// leave the card it is on.
const soloBranch = html.slice(html.indexOf("if (termOnly()) {"),
  html.indexOf("if (termOnly()) {") + 900);
if (!/m\.task !== soloID/.test(soloBranch)) {
  fail("a popped-out window does not record other windows' claims, or does not exclude its " +
    "own. Without the ledger soloSwitch cannot tell that a card already has a window; without " +
    "the exclusion it decides it cannot leave the card it is on.");
}

// Rule 5: the board lets go of a card another window claims.
//
// Every claim used to arrive for a card the board had just popped out, and
// popOutTask tears its own pane down on the way. A window that SWITCHES
// arrives at a card the board may well be showing and nobody asked it first.
const claimAt = html.indexOf('if (m.type === "solo-claim" && m.task)');
if (claimAt < 0) {
  fail("the board no longer records claims at all.");
} else if (!/termTask\.id === m\.task/.test(html.slice(claimAt, claimAt + 1400))) {
  fail("the board does not release the pane when another window claims the card it is " +
    "showing. That is two views onto one terminal, which is what every other path here " +
    "refuses.");
}

// Rule 6: and it forgets the window HANDLE on a release.
//
// `poppedOut` has two sources and the claim is only one of them. A handle held
// from `window.open` outlives the claim, and a window that switched is still
// open and still ours, so the board goes on refusing to attach to a card that
// window is no longer showing.
const relAt = html.indexOf('if (m.type === "solo-release" && m.task)');
if (relAt < 0 || !/popOuts\.delete\(m\.task\)/.test(html.slice(relAt, relAt + 700))) {
  fail("a release does not drop the window handle. `poppedOut` answers from the handle as well " +
    "as the claim, so the board keeps refusing to attach to a card whose window has moved on.");
}

// Rule 7: clicking outside closes a dialog, and never a guarded one.
//
// The switcher is what asked for this, being the dialog opened dozens of times
// a day to answer "where next", and a picker that traps the pointer is the one
// that gets abandoned. The handler is board-wide because the alternative is a
// board where light-dismiss is true of whichever dialogs somebody got to.
//
// Both halves fail silently in a browser. A test written as `e.target === dlg`
// closes the dialog when the click landed on its own padding, which reads as
// the pointer jumping. Dropping the `data-guard` refusal loses a half-filled
// form to a stray click, and there is nothing afterwards that says what went.
const lightAt = html.indexOf("if (!inside) dlg.close();");
if (lightAt < 0) {
  fail("no dialog light-dismisses on a click outside it. The switcher is opened by reflex and " +
    "has to be leavable by reflex, and the rule is set for every dialog at once or it is set " +
    "for none.");
} else {
  const body = html.slice(lightAt - 800, lightAt + 80);
  if (!/getBoundingClientRect\(\)/.test(body)) {
    fail("light-dismiss decides inside from outside without the dialog's rectangle. The " +
      "backdrop of a modal belongs to the dialog element, so a click on the dialog's own " +
      "padded edge reports the dialog as its target and closes a dialog the pointer was in.");
  }
  if (!/hasAttribute\("data-guard"\)/.test(body)) {
    fail("light-dismiss does not exempt `data-guard` dialogs. Those hold edits that are only " +
      "kept when you press save, so closing one on a click outside is a way to throw work " +
      "away by twitching.");
  }
}

// ── and then the pure functions, RUN rather than read ──────────────
//
// Everything above is a shape. These two are logic, they are the identity of a
// keystroke and the last resort of the filter, and both are cheap to get
// subtly wrong in a way no shape check would see. They touch no browser, so
// they can be lifted out and called.
//
// Lifted by matching a top-level `function` up to the first `}` in the first
// column, which is what every function in this file looks like.
function lift(name) {
  const re = new RegExp("^function " + name + "\\([\\s\\S]*?^}", "m");
  const m = re.exec(html);
  return m ? m[0] : "";
}
const COMBO_TABLE = /^const COMBO_NAMES = \{[\s\S]*?^\};/m.exec(html);
const src = [COMBO_TABLE ? COMBO_TABLE[0] : "", lift("comboOf"), lift("comboName"),
  lift("swSubseq"), lift("terminalLabel"), lift("swScore")];
if (src.some(s => !s)) {
  fail("comboOf, comboName, swSubseq, terminalLabel or swScore could not be lifted out of the " +
    "page, so what they actually do went unchecked. They are top-level functions on purpose.");
} else {
  const run = new Function(src.join("\n") +
    "\nreturn { comboOf, comboName, swSubseq, swScore };")();
  const key = (code, mods) => Object.assign({ code }, mods || {});
  const says = (got, want, why) => { if (got !== want) fail(why + " got `" + got + "`, wanted `" + want + "`"); };

  // The default has to be what the default string says, or the binding shipped
  // is not the binding documented.
  says(run.comboOf(key("KeyK", { ctrlKey: true, shiftKey: true })), "ctrl+shift+KeyK",
    "ctrl-shift-k does not produce the stored form of the default binding.");
  // Shift is PART of the identity. Without this, ctrl-k and ctrl-shift-k are
  // one binding, and the board takes readline's kill-line by accident, which
  // is the exact thing the default was chosen to avoid.
  says(run.comboOf(key("KeyK", { ctrlKey: true })), "ctrl+KeyK",
    "ctrl-k and ctrl-shift-k are not told apart.");
  // A modifier going down on its own is not a binding. Without this the
  // capture field binds `ctrl` the instant it is held.
  says(run.comboOf(key("ControlLeft", { ctrlKey: true })), "",
    "a bare modifier counts as a keystroke.");
  says(run.comboName("ctrl+shift+KeyK"), "ctrl-shift-k",
    "the default binding is not spelled out the way the settings dialog says it.");
  says(run.comboName("alt+Slash"), "alt-/",
    "a punctuation key is named by its code rather than by its character.");
  // The last resort of the filter, and the reason `atsw` finds a session
  // called `atrium:switcher`.
  says(run.swSubseq("atrium:switcher", "atsw"), true, "a subsequence does not match.");
  says(run.swSubseq("atrium:switcher", "zq"), false, "a subsequence matches something absent.");
  says(run.swSubseq("atrium", "muirta"), false, "a subsequence matches out of order.");

  // And the ranking, which is what anybody actually judges this by.
  //
  // A session is NAMED out of its own path, so the interesting comparison is
  // not name against path. It is the part of the path a session is called
  // against the part it merely sits under: `atrium` is what this card is, and
  // it is also three segments of every unrelated worktree on the machine.
  const named = {
    worktree: "D:/worktrees/github/dovholuknf/atrium/switcher",
    repo: "atrium", branch: "switcher", tags: ["board", "ui"]
  };
  const buried = { worktree: "D:/atrium/one/two/three", display_title: "three", tags: [] };
  const tagged = { worktree: "D:/elsewhere/a/b/c", display_title: "c", tags: ["atrium"] };
  if (!(run.swScore(named, "atrium") > run.swScore(buried, "atrium"))) {
    fail("a card CALLED atrium does not outrank one that merely lives under a directory of " +
      "that name. Every worktree here shares its leading segments, so a filter that scores " +
      "those the same sorts by nothing.");
  }
  if (!(run.swScore(tagged, "atrium") > run.swScore(buried, "atrium"))) {
    fail("a tag counts for no more than an incidental path match. A tag is what the operator " +
      "typed on purpose, and it is the only field on a card nothing else derives.");
  }
  // Every term has to match something, which is what makes typing more letters
  // narrow rather than widen.
  if (run.swScore(named, "atrium zzzz") >= 0) {
    fail("a card survives a term that matches nothing about it, so adding a word widens the " +
      "list instead of narrowing it.");
  }
  if (run.swScore(named, "atrium board") < 0) {
    fail("a card is excluded when one term matches its name and another matches its tags. " +
      "Terms are meant to be able to come from different fields.");
  }
  // Nothing typed scores everything the same, which is what leaves the recents
  // order in charge of the list you see first.
  says(run.swScore(named, ""), 0, "an empty query scores a card as though something was typed.");
}

if (bad) {
  console.error(`\n${bad} switcher invariant(s) broken. Each one is silent in the browser it ` +
    `was written in, which is why it is checked here.`);
  process.exit(1);
}
console.log("the switcher invariants hold.");
