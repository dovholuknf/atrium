# Group menus (u-010, u-012)

## What changed
- Terminals tab card menu, "add to group": ends in **create group...**. It asks for a name (the existing ask dialog on desktop, an inline name field on the phone sheet), makes the group and files the card in one step. Esc (or cancel) makes nothing. A name that already exists (any case) only files the card, so there is no duplicate. The entry is on the terminals menu only; the board's "into group" is unchanged apart from its sorting.
- "add to group" and "remove from group" list names A to Z, desktop and phone sheet.
- A group made in the last 10 minutes shows a **new** tag in both submenus.

## Creation time
A group had no creation time (it is only a name in the browser's `groups` list in localStorage). Smallest change: a `made` map, name to epoch ms, beside it in `atrium.grouping`. Written when a group is created from the board's "add group" or from the menu, moved on rename, dropped on remove. A group with no entry (made before this, or typed in by hand) is never "new". No daemon or hub change.

## Code
- `js/cardrules.js`: `groupMenuNames`, `groupIsNew`, `groupNameOf` (shared with the phone page).
- `js/card-menu.js`: `groupItem`, `createGroupFor`, `outOfGroupItem`. `js/sharing.js` and `css/cards.css`: the tag.
- `m/js/resume.js`, `m/m.css`: the phone sheet.

## Tests
`scripts/test-board-headless.js`, sections `groupMenus` and `mGroupMenus`: create from the menu, the card shows under the new group and it survives a reload, Esc makes nothing, an existing name adds without a duplicate, A to Z order, "new" present at 9 minutes and gone at 11 (the page's `Date.now` is moved).
`GROUP_MENUS_SHOTS=<dir> GROUP_MENUS_TAG=before|after` writes the pictures in `docs/backlog/ui/img/group-menus/` (desktop and 390px phone sheet). There is no before picture of the remove list or of the create field: the old code has nothing to show.
