# Interview: the card right-click menu (2026-10-09)

What clint does with the menu that opens when he right-clicks a card on the board. Feeds a UI design.

## Q1. What you reach for, and what annoys you

Asked:

- A card sits stuck at a menu (needs input). You right-click it.
- What do you reach for next, every time? And what in that menu gets in your way: too many items, the grey
  sub-lines, the (?) bubbles, things in the wrong order, things you never use?

**Recommended default: the top of the menu is what you do to a stuck or busy card (attach, open a shell, what did it
do?), and naming and filing (rename, alias, group, repo, context limit) moves lower or onto the details page.**

clint could not see Q1 at first. This message was his own first pass, given before he saw it, not his answer to Q1.
He said so: "why in the fuck did you skip q1? you think that was a fucking answer to q1? what the fuck????"
Q1 was asked again, without the items this message already settles.

Volunteered (verbatim):

> step one - remove 'what did it do', remove 'open a shell here', remove 'attach' from the terminal screen (i'm fucking
> ON terminal it's dumb here), change 'into group' to 'add to group' remove which repo, remove context limit but make
> sure that's availble from the settings cog. remove alias - user can click on the title in teh terminal view for
> that...

## Q1 (asked again)

Asked:

- A card sits stuck at a menu. You right-click it.
- After your cuts, the menu has rename, add to group, new agent here, new context, details and terminate left.
- Which of those do you actually use, and what still annoys you about the menu: the order, the grey lines, the (?)
  bubbles, the look?

**Recommended default: the most-used item goes at the top, and terminate stays at the bottom by itself.**

His reply (verbatim): "HOW is a card 'stuck at a menu'?"

The scenario was unclear. Q1 was asked again with a plainer one: a card you want to do something to.

Answer (verbatim):

> generally speaking it's always 'restart'. i nearly never right click a card on the terminal tab. i used to
> 'terminate' them too (which should ask me to clean up all resourcew if we terminate)

## Q3. What terminate cleans up

Asked:

- You terminate a finished card. Today that only stops the process. The card stays, and so do its worktree and its
  branch here and on the hub.
- A separate "close…" item already does the cleanup, but only on some cards: it removes the worktree and the branch,
  and asks first (keep, stash or delete) when the worktree holds work that isn't saved anywhere else.
- Should terminate become that: one dialog that lists what it will remove, each with a tick box, then stops the
  session and cleans up? Or should terminate only stop the session, with "terminate and clean up" as a second item?

**Recommended default: one terminate. It shows the list with everything ticked, and asks with no default answer when
there is work that isn't saved anywhere else. "close…" merges into it.**

Answer (verbatim):

> i like two options. 'stop' and 'terminate and clean up'. stop just stops the agent for it to be resumed. it would be
> what the board consideres 'shelved' (which isnt' really used righ tnow)... and the filtering should not show it
> unless 'show stopped' is toggled (we still need to fix the gro0uping/sorting some i think)

## Q4. The board menu's other items

Asked: the board menu (not the terminals tab) has many more items than the screenshot. Keep, move to settings (the
cog), or drop:

- Keep: restart, stop, terminate and clean up, new context, new agent here, add to group, rename, settings.
- Move to settings: auto mode, peers may type here, pin to the top, keep cache warm, save a conversation, forget a
  conversation.
- Drop: details (hover shows it), attach terminal (button on the card), open a shell, open in a terminal window,
  open its board view, move to column, forget this session (terminate and clean up covers it).
- Keep, only when it applies: resume (a stopped card), dismiss the question, keep this work (temporary folder),
  any per-card actions or sharing.

**Recommended default: as listed above.**

Answer (verbatim):

> borad right click is a different interview -- unelss we feel terminal interview is 100% done?

Not done: this interview stays on the terminals tab menu. Q4 is withdrawn and goes to the board menu interview.

## Q5. The terminals tab menu, whole

Asked: after the cuts, the proposed terminals tab menu, top to bottom:

1. restart: same conversation, new process (new here; the tab doesn't have it today)
2. resume: only on a stopped card, picks the conversation back up
3. new context: commit, hand off, clear
4. new agent here: a second agent in the same folder
5. rename...
6. add to group / out of group
7. (divider) settings (the cog)
8. (divider) stop: stops it, can be resumed later (new here)
9. terminate and clean up (orange)

Rename could also go to clicking the title, like the alias. Change anything?

**Recommended default: as listed. Rename stays in the menu, because clicking the title already sets the alias.**

clint, mid-turn: "let's finish terminal view right click and then move to other right clicks"

Answer (verbatim):

>   4. new agent here: a second agent in the same folder
>
> cut. i never ever use that yet.
>   5. rename…
> cut. i have never used that yet and it should be doable some other way. probably by clicking on a terminal title
> and changing 'the handle'...
>
> 6: out of group is fuking stupid. it's "remove from group"
> 7: (divider) i assume is a horizontal line / gropuing?

(Yes: a divider is a thin horizontal line between groups of items.)

## Q6. The (?) bubbles and the grey lines

Asked:

- Today some items carry a (?) you hover for a paragraph of help, and a grey line under the label, like "commit, hand
  off, clear" under new context.
- With six items left, keep either?

**Recommended default: drop the (?) bubbles. Keep a short grey line only on new context ("commit, hand off, clear")
and on terminate and clean up (what it will remove), since those are the two that do something you can't see from the
name.**

Answer (verbatim):

> the question mareks yes, the grey lines under no those are dumb so the opposite of your suggestion

## Q7. Which items ask "are you sure?"

Asked:

- You click stop, new context, or terminate and clean up by mistake.
- Which of them should open a confirm window first?

**Recommended default: only terminate and clean up asks, and its window is the list of what it will remove. Stop and
new context run at once, since both can be undone (resume picks the work back up; new context commits and writes a
handoff note first).**

Reply (verbatim): "nah bro. you fucked up...."

Asked what was wrong.

Answer (verbatim): "if you clicked it. you clicked it srory bud..."

## Q8. Terminate and clean up when work would be lost

Asked:

- You click terminate and clean up on a card whose folder holds changes that were never committed or pushed, so they
  exist nowhere else.
- No confirm, per Q7. Does it still delete those, or stash them somewhere first so they can be got back?

**Recommended default: no question asked, but uncommitted work is stashed into a commit on a hidden branch (on the
hub) before the folder goes, so a wrong click costs nothing.**

Answer (verbatim):

> ok... it's fine to ask in that case. that might be useful BUT often it's stuff i just don't fucking care about so
> it's a 1/2 and 1/2 situation add the confirmation add a 'never show me this again' and let the user decide they want
> these or not

## Q9. On the phone

Asked:

- You're on the terminals tab on your phone. There's no right-click.
- Should a long press on the card open this same menu, with the same items?

**Recommended default: yes. A long press opens the same menu, the same items in the same order, and tapping the (?)
shows its help, since there's no hover.**

(The session was parked between asking and the answer; it was asked again as is.)

Answer (verbatim): "yea let's try that"

## Q10. From the keyboard

Asked:

- You're typing in a card's terminal and want to restart it without reaching for the mouse.
- Today Shift+right-click in the terminal opens this menu, and Ctrl+Alt+N runs new context. Should a key open the
  menu, or should restart get its own key like new context has?

**Recommended default: the Menu key (or Shift+F10) opens the menu on the card you're in, arrows move, Enter picks.
No new shortcut for restart.**

Answer (verbatim): "no shortcut"

Read as: no new keyboard shortcut, neither a key for the menu nor one for restart. What exists today stays
(Shift+right-click in the terminal opens the menu, Ctrl+Alt+N runs new context).

The terminals tab menu is settled here. The board's right-click menu and the other right-click menus come next.

## Q2. Attach on the board (asked before Q1 was answered; withdrawn, will be asked later)

Asked:

- You're on the board, not in the terminals view. A card is stuck at a menu. You right-click it.
- The board card already has an "attach" button on it, plus the pop-out icon for its own window.
- Keep "attach" in the right-click menu on the board too, or drop it from the menu everywhere?

**Recommended default: drop it from the menu everywhere. The button on the card does the same thing.**

Answer: _(waiting)_

## Decisions

- Remove "what did it do?".
- Remove "open a shell here".
- Remove "attach" from the menu in the terminals view (you are already on the terminal there).
- Rename "into group" to "add to group".
- Remove "which repo...".
- Remove "context limit..." from the menu. It must stay reachable from the card's settings (cog).
  The per-card context limit box appears to be there already (settings-spine.js), so check it before removing the menu item.
- Remove "alias...". Set the alias by clicking the card's title in the terminals view.
- "restart" (same conversation, new process) is what he uses nearly every time, so it goes at the top.
  The menu in the screenshot (terminals tab) does not show restart at all; the board's menu (card-menu.js) only shows it
  for cards atrium runs.
- He rarely right-clicks a card on the terminals tab. The board menu is the one that matters.
- Terminate splits in two:
  - "stop": stops the agent so it can be resumed later. This is what the board calls "shelved" today (barely
    used). Stopped cards are hidden unless a "show stopped" toggle is on.
  - "terminate and clean up": stops it and removes what it holds ("close…" today).
- Grouping and sorting on the board still need fixing (outside this menu; noted for later).
- This interview covers the terminals tab menu only. The board's right-click menu and the other right-click menus come
  next, as separate interviews.
- Cut "new agent here" (never used).
- Cut "rename...". Rename by clicking the terminal's title and changing the handle there (with the alias).
- "out of group" becomes "remove from group".
- Add "restart" (top) and "stop" to the terminals tab menu; "resume" stays, shown only on a stopped card.
- Keep the (?) help bubbles. Remove every grey line under an item.
- No "are you sure?" on any item. A click is the decision.
- One exception: terminate and clean up asks when the folder holds changes that exist nowhere else. That window has a
  "never show me this again" tick, so the user decides whether they want it.
- Phone: a long press on a card opens the same menu, with the same items in the same order. Tapping a (?) shows its help.
- Keyboard: no new shortcuts. Shift+right-click in the terminal and Ctrl+Alt+N stay as they are.

## Proposed menu: terminals tab

Each item keeps its (?) help bubble. No grey lines under any item.

| # | Item | What it does | Shown when |
|---|------|--------------|------------|
| 1 | restart | Stops the agent and starts it again on the same conversation, same settings. | a running card atrium owns |
| 2 | resume | Starts the agent again on a stopped card, picking the conversation back up. | a stopped card |
| 3 | new context | Has the agent commit, write a handoff note, clear its context, then carry on from the note. | a running card atrium owns |
| 4 | add to group ▸ | Files the card into one of your named groups. | always |
| 5 | remove from group | Takes the card out of its group (a submenu when it is in more than one). | the card is in a group |
| — | divider | | |
| 6 | settings | Opens the card's settings (the cog). The context limit lives there. | always |
| — | divider | | |
| 7 | stop | Stops the agent so it can be resumed later (today's "shelve"). The card is hidden unless "show stopped" is on. | a running card |
| 8 | terminate and clean up (orange) | Stops the agent and removes its folder and branch, here and on the hub (today's "close…"). No confirm, except when the folder holds changes that exist nowhere else: then it asks, with a "never show me this again" tick. | always |

Moved out of the menu:

- alias and rename: click the terminal's title and change the handle.
- context limit: the card's settings (the cog).

Removed: what did it do?, open a shell here, attach, which repo, new agent here.

## Part 2: the board's right-click menu

### B1. Same menu as the terminals tab?

Asked:

- You right-click a card on the board. Today you get about 25 items: details, attach, shell, resume, transcripts,
  terminal window, column moves, pin, auto mode, shelve, peers may type here, settings, terminate, forget this
  session, and more.
- Should the board show the same menu you just settled for the terminals tab (restart, resume, new context, add to /
  remove from group, settings, stop, terminate and clean up), and then we only argue about what to add?

**Recommended default: yes, the same menu. Then add board-only items one at a time, only if you'd actually click them.**

Answer (verbatim): "defer board and stack and wrap up terminals"

Deferred: the board's right-click menu and the stack's are a later interview. This file settles the terminals tab only.
Starting points for that interview: the withdrawn Q2 (attach on the board) and Q4 (the board menu's other items)
above.
