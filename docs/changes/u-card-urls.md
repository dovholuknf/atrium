## Test plan

## @LETTER@. Card URLs

### @LETTER@1. A bookmark opens the same card

1. On the hub, open a card's terminal, then open `/alias/<its alias>` in a new tab. Note the address stays as typed.
2. Repeat on a zrok board share and on an OpenZiti board share, with the same address on each.

**Expected:** each opens the card's terminal on its own and the address bar keeps the readable path.

### @LETTER@2. A relaunched alias says it is a different card

1. Open `/alias/<alias>` for a card. Stop that card and launch another with the same alias.
2. Reload the tab.

**Expected:** the new card opens, with one line over the pane that the alias is a different card now and what became of
the old one, and a link to the old one while it exists.

### @LETTER@3. An unknown name lists the ones that would have worked

1. Open `/alias/no-such-card` and then `/room/<room>/no-such-card`.
2. Open `/room/not-a-room/anything`.

**Expected:** no terminal. The page says no card has that name and lists what would have worked as links, and for a
room that is not attached it names the attached rooms as links.

### @LETTER@4. Two rooms with one alias ask once

1. Give live cards on two rooms the same alias and open `/alias/<alias>`.
2. Pick one from the chooser, then open `/alias/<alias>` again.

**Expected:** the chooser lists each card with its room and what it is doing, done ones after live ones. The second
open goes straight to the card picked, with one line naming the other card and linking to its `/room/<room>/<alias>`.

### @LETTER@5. The links the board builds

1. Pop a terminal out from the board, then switch cards from the switcher inside that window.
2. Do it again with two rooms holding the same alias.

**Expected:** the address is `/alias/<alias>`, and `/room/<room>/<alias>` for the alias both rooms hold. A card with
neither an alias nor a handle uses `#term=<id>`.

### @LETTER@6. An old `#term=` link still opens

1. Open `/#term=<card id>` and `/#term=<room>~<card id>`.

**Expected:** both open the card as before and the fragment is not rewritten.

### @LETTER@7. A room scopes the board

1. Open `/room/<room>`.

**Expected:** the board opens scoped to that room, the same as picking it in the header, and the choice is still there
after a reload at `/`.

### @LETTER@8. A phone bookmark opens the card

1. On the phone page, open a card from the list, then press back.
2. Open `/m/alias/<alias>` and `/m/room/<room>/<name>` directly, then close the card.
3. Open `/m/alias/<alias>` for an alias two rooms hold.

**Expected:** opening a card puts its readable path in the bar and back closes it. A direct address opens the card, and
closing it goes to the list. The clash shows a list of the cards to pick from.
