# Queued work, not yet dispatched

Work that has been decided on and deliberately NOT given to an agent yet. `docs/backlog.md` is what is left to
do at all. This is the narrower question of what to hand out next, and to how many.

**Grouped on purpose.** Sixteen agents running at once made the machine sluggish, and most of that cost bought
nothing: several of the items were one file apart and would have been one session. An entry here is an
AGENT-SIZED unit of work, which means one surface, one set of tests, and one thing to look at when it is done.
Splitting by backlog number is how you get four sessions editing `index.html`.

**Nothing here starts without being asked for.** The rule that produced this file: do not launch new agents
while the operator is using the machine.

---

## NEXT. The board throws away where you were

**The one to hand out the moment the running agents stabilise.** Operator's words: "super fucking annoying...
it invalidates my position in the window, so if I had scrolled down it rerenders the full fucking dom and
forces my current view back to the top... it's horrible".

Every update repaints wholesale. `setHTML` replaces `innerHTML` for the whole list, so every node is destroyed
and rebuilt, and a browser has nowhere to put the scroll position of elements that no longer exist. It goes to
the top. This fires on every SSE event, which on a board with sixteen live agents is constantly, so scrolling
down is something you cannot finish doing.

Scroll is the loudest symptom and not the only one. The same swap drops text selection mid-drag, moves focus,
closes anything anchored to a node that has gone, and restarts CSS transitions, which is why the board can look
like it is flickering under load.

Two shapes of answer, and the second is the real one:

- **Preserve around the swap.** Record `scrollTop` before, restore after. Small, and a lie: it fights the
  browser every frame, still destroys selection and focus, and goes wrong when the content above the viewport
  changes height.
- **Stop repainting what did not change.** Reconcile rows by card id: update the ones whose data moved, add
  and remove the rest, and leave every untouched node alone. Then scroll, selection and focus survive because
  nothing was destroyed.

The second is more work and is the fix. The board already has the identity needed for it: every row carries
`data-id`, which is the whole precondition for reconciling by key.

**Two tiers, and the second one is what makes it hold.** The operator's framing: only replace a card if the
card changed, so the content updates and the container does not.

1. **The container is never rebuilt.** Rows are added, removed and reordered by id. Anything untouched keeps
   its DOM, so scroll position, text selection and focus survive because nothing that held them was destroyed.
2. **A row is not rebuilt either, unless its STRUCTURE changed.** The fields that tick constantly are the age,
   the activity chip and the status class, and those are written into the nodes that are already there. Only a
   real change rebuilds a row: the title, the tags, the `why`, the card arriving or leaving.

Skipping the second tier swaps one bug for a quieter one. The age updates every second, so the card being
destroyed is the card being read, and the selection dies anyway while the scroll position now survives, which
is a fix that looks complete and is not.

Watch for the thing that makes this deceptive: it will look fixed on a quiet board. Test it with the list
scrolled down, mid-selection, while cards are actually changing.

---

## A. Publishing atrium

**Blocked on accounts, not on code.** Backlog 4. Six commands written out at the end of `docs/packaging.md`,
Scoop first because it exercises the release shape end to end. An agent cannot do this: it needs credentials
that belong to a person.

What an agent COULD do first, and would be worth one session: the release script and its dry run, so the
publish itself is one command with nothing left to work out at the time.

## B. Multi-tenant, and what it costs

**A decision before it is code.** Backlog 3. Two of the three objections moved: there is authentication now,
and rooms are federation's cheaper half. The supervisor is what has not moved, because the daemon owns a pty
per runner in the operator's own logon session and a cluster has no logon session.

Not agent work until somebody decides fork or flag. Worth an hour of reading and a written recommendation,
which IS agent work, and is one session.

## C. Postgres

Backlog 10. The schema is written for it and has never run there. Nothing needs it until B does. One session,
and the honest output is "here is what broke", not a migration.

## D. Many boards, one machine

`atrium preview` shipped: a second daemon on a copy of the cards, with its own port and its own address file,
started PASSIVE so it serves the board and acts on nothing. It has no backlog entry and no section in
`docs/overlays.md` or `docs/supervision-design.md`, which means the next person to want two boards will invent
it again.

One session: write it up, and answer the question that produced it. Two atriums on one database is not blocked
by sqlite, which allows it. It is blocked by both of them ACTING: two daemons both start every fixture, both
re-bind the same zrok name, and both write the address file that every hook on the machine reads. Postgres
changes none of that. `Options.Passive` is one writer and any number of readers, and whether that generalises
is the thing to answer.

## E. Watching the fleet

The gap is not in atrium, it is in how it was being used: sixteen agents ran for an hour and the operator found
out which ones had finished by asking. The board already knows. `atrium finish` files a card, `atrium ask` puts
the question on it, and nothing was reading either.

Smallest useful thing, and it may not need code at all: a single command that answers "which of these want me",
and what a dispatcher should be looking at between them. Decide whether that is `atrium peers` grown up, a
saved filter on the board, or a script.

## F. Small board nits

Kept together deliberately. Each is minutes of work and they all live in `internal/api/web/index.html`, so
they are one session and not five. Adding a fifth one here is cheaper than starting a fifth agent.

- **A toast about a popped-out window appears in the wrong window.** Attaching to a card that is already
  popped out raises that window and says "it is in its own window: raised it for you". The toast is drawn in
  the window you clicked in, which is the one that now does not have the terminal. It belongs in the window
  that was raised, which is where you are about to be looking. `raiseToasts` already moves the toast host
  between elements, so the machinery for putting a toast somewhere specific exists.
