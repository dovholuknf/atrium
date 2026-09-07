# The switcher

Sixteen sessions and the terminal list stops being a list you glance at. It becomes a list you read. The
question "what is there" and the question "go to the one I mean, now" are different questions, and the list only
answers the first one well.

The switcher answers the second. One key, a few letters, Enter. Everything below is either the keystroke, which
is the whole design risk, or the popped-out window, which is where the interesting half lives.

## The keystroke is a setting, and that is not a hedge

`ctrl-k` is the binding everybody means. Every command palette in every application uses it, and asking anybody
what opens a switcher gets that answer.

It cannot be the default here, for two independent reasons.

**A browser keeps a handful of accelerators for itself, and the handful differs.** Chrome and Brave put
`ctrl-k` in the address bar. Firefox puts it in the search bar. A page may ask to keep a keystroke, and the
browser may grant that or refuse it, and **when it refuses, it says nothing**. `preventDefault` returns
normally. That is the worst shape a default can have: it works on the machine it was chosen on, does nothing on
somebody else's, and produces no error anywhere to explain the difference.

**A terminal already uses it.** `ctrl-k` is readline's kill-line. This board is a board of terminals, so
binding it means quietly taking a keystroke away from every shell on the machine. That failure is worse than
the first one because it is not even about the switcher: something the operator has typed for twenty years
stops working, in one application, for no visible reason.

So:

- **The default is `ctrl-shift-k`.** Chrome, Edge and Brave leave it alone. Terminals leave it alone too, which
  is why terminal emulators put their own commands there. Firefox takes it for the Web Console and will not
  give it back.
- **It is rebindable**, in settings, under `the board`. Press the button, press the keys. It is bound by being
  pressed rather than picked off a list, because the question is whether THIS browser lets THAT combination
  through, and the only thing that answers it is pressing it.
- **The binding is stored in the browser, not on the daemon.** This is the opposite of every setting beside it
  and it is deliberate: which key is free is a fact about the browser. A value synced from the desktop would
  arrive wrong on a laptop running something else.
- **A binding without ctrl, alt or cmd is refused.** A bare key would fire while you were typing into a
  session.
- **A binding no browser gives up is refused at the point of binding.** `ctrl-t`, `ctrl-n`, `ctrl-w`, `ctrl-q`
  and their shifted forms open tabs and close windows whatever the page asks. Accepting one and letting
  somebody find out later is the failure this whole setting exists to avoid.

### Detecting the theft rather than guessing at it

There is one more case, and it is the one that makes this ship for more than one person: a browser nobody
anticipated that takes the bound key anyway.

It cannot be caught by asking. What it cannot hide is **where the focus went**. Every accelerator that costs us
a keystroke moves focus out of the document: an address bar, a search field, a devtools panel. So the switcher
opens, focuses its input, and a quarter of a second later asks whether the document still has focus. If it does
not, the keystroke was delivered twice, and atrium says so and points at the setting.

Said once per binding per session. The answer does not change until the binding does.

## What it does

Opens over whatever is on screen, as a modal `<dialog>`, which is the only thing reliably above another open
modal. Filters as you type against the card title, the worktree and the tags. Up and down move, Enter goes,
Escape closes. Nothing needs a mouse.

**Ranked, not merely filtered.** Every term has to match something or the card is out, so typing more narrows.
Where it matched decides the order: the name first, then the tags, then the directory, and a subsequence last,
so `atsw` still finds `atrium:switcher` without a scattering of letters outranking a real word.

That order is less obvious than it sounds, because a session is NAMED out of its own path. The comparison it
actually makes is between the part of the path a session is called and the part it merely sits under: `atrium`
is what one card IS, and it is also a leading segment of every unrelated worktree on the machine. A tag sits
between the two on purpose, being the one field on a card that nothing derives and somebody typed.

**The last few first.** With nothing typed, the order is most recently switched to, then anything waiting on a
human, then by how recently a session moved, which is the order the session list already uses. The common case
is the key and Enter with nothing typed at all, and going back and forth between two sessions is one keystroke
and one more.

**Terminals only.** This list is a place to GO. A card with nothing running is not one, and restarting a
stopped session belongs on the board or the stack with the whole card in front of you.

## The interesting half: a popped-out window is one card

The board attaches in its pane, and `attachTask` already knew the one case that is not that: a card in a window
of its own is raised rather than attached, because two views onto one terminal both taking input is the
situation `docs/supervision-design.md` says nothing arbitrates.

A popped-out window has no pane to attach in. It **moves**. Four things move with it and leaving any one of
them behind is a bug that presents as something else:

- **The claim on the old card is released.** Forget it and the board still believes that card is popped out. It
  refuses to attach to it, and "raises" a window that is showing something else entirely.
- **The new card is claimed.** Forget it and the board keeps its own pane on a card this window is now driving,
  which is two views onto one terminal, both taking input.
- **`window.name` becomes `atrium-term-<new id>`.** That name is how the board finds a window it did not open.
  Forget it and `reopenByName` finds nothing, so the board opens a SECOND window on the card this one holds.
- **`#term=` in the address.** This window reloads itself whenever the daemon serves a new build. Forget it and
  the window comes back showing the card it used to be.

Claims are fifteen second heartbeats, which makes the first row worse rather than better: **get it wrong and it
heals itself**, so the symptom is a flicker that gets diagnosed as anything but this.

Two consequences on the other side of the bus, both new and both required by the above:

- **A popped-out window now keeps other windows' claims.** It ignores them as alerts, as it always did, but it
  needs the ledger to refuse to move onto a card that already has a window of its own. It never records its
  own, or it would decide it cannot leave the card it is on.
- **The board lets go of a card another window claims.** Every claim used to arrive for a card the board had
  just popped out, and popping out tears the board's own pane down on the way, so there was nothing to yield. A
  window that switches arrives at a card the board may well be showing and nobody asked it first.
- **A release drops the window HANDLE as well as the claim.** `poppedOut` answers from two sources, and a
  window that switched is still open and still the board's, so the handle outlives the claim it no longer
  deserves.

## What is checked, and why it is checked that way

`scripts/check-switcher.js`, run by `scripts/check-board.sh`. Every rule in it is one of the failures above,
and they are all silent in the browser they were written in, which is the reason they are checked in a file
rather than found by using it:

- The hotkey listens in the capture phase and calls both `preventDefault` and `stopPropagation`. The second one
  is aimed at xterm, which listens on its own textarea: an event allowed through opens the switcher AND types a
  control character into a running session.
- The handler compares against the SETTING and not a hardcoded key.
- `ctrl-k` is not the default.
- `soloSwitch` releases, claims, renames the window, and refuses a card that already has one.
- A popped-out window records other windows' claims and excludes its own.
- The board yields its pane on a claim for the card it is showing, and forgets the handle on a release.

The two pure functions, the keystroke's identity and the subsequence match, are lifted out of the page and
RUN, because both are cheap to get subtly wrong in a way no shape check would see. The one that matters most:
shift is part of a binding's identity, or `ctrl-k` and `ctrl-shift-k` become one binding and the board takes
readline's kill-line by accident.
