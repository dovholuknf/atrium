# Board views: one board tree per worker worktree

Backlog item `f-board-views`. clint, 2026-10-07: "a slim and easy way to make different 'ui versions' for each
agent with each agent's path being the unique part".

The hub serves extra board trees by name, one per worker worktree. The API is shared: the same `/v1`, the same
rooms and the same data. Only the static board files differ, so two workers editing the UI never collide and clint
opens the view of the change he is testing.

## The address: a host name, not a path prefix

A view is `http://<name>.localhost:7778/`, not `http://127.0.0.1:7778/v/<name>/`.

The backlog item gave `/v/<name>/` as an example. It does not work without changing the board, and a view has to
serve worktrees that branched before any such change:

- `index.html` loads every script and stylesheet by absolute path (`/js/core.js`, `/css/cards.css`, `/vendor/...`).
  A `<base href>` does not move an absolute path. Rewriting the HTML on the way out covers the page and nothing
  the JS builds itself.
- The board builds absolute URLs at run time. Pop-out windows (`solo.js`), card addresses (`cardurl.js`, `/alias/x`),
  `history.replaceState(..., "/#term=...")` in `switcher.js` and `toasts.js`, `/m/`, and `/sw.js`. Each one would
  move the tab out of `/v/<name>/` onto the root board without saying so. A view that quietly turns into the root
  board is worse than no view.

A host name has none of that. The page is served at `/` of its own origin, every absolute path resolves into the
same view, and an old worktree works unchanged. Chrome, Edge and Firefox resolve every `*.localhost` name to
loopback without asking DNS (RFC 6761 section 6.3), so nothing is installed or configured on the machine.

The hub reads the view from the `Host` header. `127.0.0.1:7778` and `localhost:7778` are the root board, as now.

## Registration: automatic, from a card's worktree

No API call and no CLI. A view exists while a live card has a worktree that holds a board:

- the card is live (not `done` or `dead`) on any room the hub holds,
- its `worktree` is on the hub's machine: the card's `hostname` is the hub's own, and the path is under one of the
  hub's view roots (below),
- `<worktree>/internal/api/web/index.html` is a regular file.

The list is derived from what rooms already announce, rebuilt at most every two seconds when asked for, and never
stored. So **a view goes away when its card or worktree does**: the card ends, or the directory is removed, and the
next lookup does not find it. Nothing to clean up.

**Name.** The worktree's folder name (`f-board-views` for `D:\worktrees\github\dovholuknf\atrium\f-board-views`),
lowercased, with anything that is not a letter, digit or `-` turned into `-`. That is clint's "each agent's path
being the unique part". The card's alias, if it has one, answers too. Two worktrees with the same folder name: the
first by path keeps it and the next is `<name>-2`. Two cards in one worktree are one view.

**Turned on by the hub's `--board-views <root>`**, which names the directories worktrees may be served from.
Repeatable, or comma-separated. Empty, the default, means no views at all. The live hub gets
`--board-views D:\worktrees` in `scripts/live/live-common.ps1`.

**Listed on the board.** `GET /_hub/views` answers the list (name, URL, directory, card, room). A card's right-click
menu has **open its board view** when its worktree is one, which opens the view in a new tab. That is the click
clint wants: the card he is testing is the card he is looking at.

## Build id and reload, per view

The board reloads itself when `/v1/health`'s `build` changes from what it saw first. In a view the hub answers that
field with **the view's own hash**, never the root's, everywhere it answers it: the proxied room health
(`rewriteHealth`), the merged health of the ALL view, and `/_hub/health`. The view is carried on the request context
from the `Host`, so the three places cannot disagree.

The hash is `api.BoardID` over the view's tree, the same function as the root board, cached for two seconds. Unlike
the root board (hashed once at start), a view's hash follows its files: a worker's save changes the hash and the
open view tab reloads onto it. That is what a view is for. A tab on the root board never sees a view's hash, so it
does not reload when a worker saves.

## localStorage and settings

Settings the server holds (`/v1/settings`, skins, card colours, everything under the gear that is saved) are
shared, because the API is shared.

`localStorage` is per origin, and each view is its own origin, so **a view's local state is its own**: folded
columns, the terminal list's width, sound, copy-on-select, which terminal was open. A view opens with the defaults
and remembers its own from then on. That is fine, and it is the better side to be on: a UI change that changes the
shape of something in `localStorage` cannot corrupt the root board's copy, which shared storage would allow. The
same is true of the service worker and its push subscription, which a view registers for itself if asked.

## Only the hub's own machine

A view is a directory on the hub's disk, served by the hub. A worker on m1mini has a worktree on m1mini, so its card
fails the hostname check and the path is not under a local root: no view is offered. To get one, the worktree has to
be on the hub's machine. Not attempted: fetching another room's tree.

A view is reachable only from the hub's machine. `*.localhost` resolves to loopback in the browser, and a zrok or
ziti share carries its own host name, never `<name>.localhost`, so a share always gets the root board.

## Containment

- **A request names a view, never a path.** The `Host` label is looked up in the derived list. Nothing in a request
  becomes part of a path except the file name inside the view, which the same `path.Clean` and `fs.ValidPath` rules
  as the root board keep inside it.
- **Only worktrees under a configured root.** Both are made absolute and symlinks resolved before the check, so a
  worktree that is a link out of the root is refused.
- **Only a directory that looks like a board**: `internal/api/web/index.html`, a regular file.
- **Files are opened with `os.OpenInRoot`**, so a symlink inside the tree that points outside it is refused, which
  `os.DirFS` would follow. No handle on the directory is held between requests, so a worktree can still be deleted
  on Windows while a view of it is open.
- **An unknown `*.localhost` name is a 404** naming the views there are. It never falls back to the root board, which
  would make a typo look like a view of nothing.

`edge.LoopbackHost` now counts `*.localhost` as loopback, which is what RFC 6761 says it is. That is what lets the
host check and the "this machine's own user" check (`edge.IsLocal`) treat a view exactly as they treat
`localhost:7778`.

What a view cannot contain is its own code: a view runs a worker's JavaScript in clint's browser with the board's
full API, the same as a merge to claude/main would. That is the feature, and it is why views are on only where
`--board-views` says.

## Not done

- No view on a room's own board (`127.0.0.1:7781`). Rooms do not serve worker trees; the hub does.
- No `/v/<name>/` path form. If a browser that does not resolve `*.localhost` turns up, the fix is a hosts-file line,
  not a second scheme.
