# internal/api

The human-facing HTTP surface: JSON, SSE, and the board itself. The daemon supplies anything that needs a
process, as a function field on `Server` or a package var, so this package never reaches into supervision.

## Files leave a card only to the card, an atrium path or an `open_paths` folder

The rule the file endpoints exist under, and the reason `internal/safepath` exists at all.

- **Upload computes its own destination and accepts no path.** There is nothing in the request to point
  somewhere else.
- **Download takes one and resolves it through `safepath`,** which follows symlinks on BOTH sides.
- **Everything outside a card answers 403 whether or not it exists,** unless it is under an atrium path or an
  `open_paths` folder, so the endpoint is not an oracle for what is on the machine.
- **Only the read endpoints look past the card:** probe, view, launch, list and download, through
  `containedOrOpen` in `openpaths.go`. Upload, text (read and write), delete, zip and `files/open` stay against
  the card alone.
- **An atrium path is allowed with no setting,** and `atriumPaths` is the one place that defines the set: the
  room's card root, the directory of every card on the room live or archived, the room's repository clones and
  worktrees, WORKTREE_ROOT and each provider's worktree root. `open_paths` is the folders the operator adds,
  empty by default. Both resolve through `safepath` on both sides, so a `..` or a symlink cannot leave a root.

`docs/runtime/file-transfer-design.md` records why `browse.go` was never the containment answer everybody assumed
it was.

## The directory picker is bounded

`browse.go` lists the DAEMON's filesystem, not the browser's, because the board is meant to be opened from a
phone. `browseroots.go` decides where it may look, defaulting to the home directory plus every directory a
card, fixture, source or harness already names.

Unbounded, this is an unauthenticated recursive listing of the whole machine the moment the board is published,
because a share terminates here and every request over one presents as loopback.

## The settings boundary refuses two things, and both refusals are load-bearing

**A grouping expression may be stored where it was typed, and nowhere else.** The board compiles two
operator-supplied functions with `new Function`. That is safe for one reason: they come from `localStorage`, so
the code running in a browser was typed into that browser. Storing them here makes it one machine typing code
another machine runs, with `fetch` in hand and the whole board to read. `group_by` and `group_order` are
refused against the RAW body, because what is guarded is a field nobody has added yet.

**An unknown skin is refused rather than stored,** which is the opposite of the browse roots beside it. A root
that does not exist yet is a list somebody is preparing. An unknown skin saves, gets worn, matches no rule and
changes nothing, so the setting looks like it took.

Both refusals sit ahead of every write in the handler: a body carrying one beside a legitimate setting is
refused whole rather than half applied.

## Two theme systems, and they are not the same thing

A **terminal theme** is sixteen ANSI colours handed to xterm.js. It belongs to one terminal, two side by side
may reasonably differ, and it is stored on the card.

A **skin** is the board's own chrome. There is one board, so it cannot differ, and it is a daemon setting that
follows to another browser. `skins.go` holds the list, and `scripts/check-skins.sh` fails when that list and
the stylesheet disagree.

## The files

| | |
| --- | --- |
| `api.go` | JSON API, SSE, and the function fields the daemon fills in |
| `hooks.go` | which claude hooks are wired, and writing the missing ones |
| `overlays.go` | the board's side of reaching atrium from elsewhere, and lending one session |
| `settings.go` | settings that belong to the daemon, not to a card |
| `skins.go` | what the board wears. Duplicated from the stylesheet, with a check |
| `scrollback.go` | how far back a terminal remembers, at both ends |
| `browse.go` / `browseroots.go` | listing the daemon's filesystem, and where it may look |
| `files.go` / `filelist.go` / `filezip.go` / `filetext.go` / `fileopen.go` | bytes in, bytes out, and a text file behind a content-hash precondition |
| `icon.go` | the mark a card wears on a desktop notification |
| `sessions.go` | which claude conversation a card can resume, read off disk |
| `web/` | the board: `index.html`, `css/`, `js/`, `m/` (the phone), `read.html`, `sw.js`. See `web/AGENTS.md` |
