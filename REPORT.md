# Report: u-new-browser-prefs-every-window

## What changed
- New `internal/api/web/js/prefs-live.js`, loaded first by the board and by /m. `prefLive(match, apply)` registers a key,
  a `prefix*` or a list, and one `storage` listener runs the matching `apply`s. A cleared store calls every one.
- Each setting that was read once at load now registers an `apply` that does what its control does, minus the write.
  Settings dialog controls are repainted too (`paintSettingsPrefs`, split out of `paintSettings`).
- The existing ad hoc listeners (inputlag, typing, growler, grouping and the rest) were left as they are. They work and
  rewriting them was churn. New ones all go through `prefLive`.
- No setting needed the "reload other windows" fallback, so no settings row text changed and there are no PNGs.
- Docs: Design note in `docs/backlog/ui/u-new-browser-prefs-every-window.md`, changelog in
  `changelog/ui/2026-10-04-u-new-browser-prefs-every-window.md`.

## The sweep (setting, key, live or reload)
All live, none reload. Full table with the keys is in the item file's Design note. In short:
- Already live before this change: input lag log, typing readout, growler, per-card notify off, grouping, folded cards,
  new-card chips, landing key, repos view, phone header and tray, /m bell.
- Live, new: sound prefs (`atrium.sound`), copy on select, focus on hover, card colours, terminal row wear, text size,
  whitespace, terminal list sort, mode and width, tray, hides, folded groups, terminals kept hidden, board sort, stack
  filter, switcher key label, card font and phone zoom for the attached card, toast log badge, /m list order and filters,
  /m needs or all, /m text size.
- Read on every use, nothing to do: pop-out size and placement, switcher recents, dialog skip list, walk width, overlay
  open state.
- Left alone: per-window position keys (`atrium.view`, `atrium.term`, `atrium.room`, settings pane), the skin (daemon
  owned), the per-card fit/desktop view toggle (reloads its own window by design), `atrium.downskin`, `atriumScroll`.

## Tests
- Added `prefsEverywhere` (board page plus a popped-out `#term=s1` page on one context, 21 settings toggled in one, the
  other follows, a reverse direction, a cleared store, no reload) and `mPrefsEverywhere` (two /m tabs: mode, filter,
  text size) to `scripts/test-board-headless.js`. The mock for /m now serves `prefs-live.js`.
- `HEADLESS_ONLY=prefsEverywhere,mPrefsEverywhere node scripts/test-board-headless.js`: both ok. Against the tree
  without the change the same test gives 14 FAILs, so it does catch the bug.
- Neighbouring sections pass: `HEADLESS_ONLY=termWear,typing,notifyOff,settingsOnce,mHome,mCard,mServe,newCard,copySelect,growlQuiet,idleRate,foldStill,mHomeOrder,mHomeLive`.
- Not run: the whole headless suite.
- `scripts/check-board.sh` exits 1 with the same output on this branch and on the base commit (existing failures, none new).
- `go test ./internal/api/`: one failure, `TestTheWalkerLaunchSetAndClear` (prsdrawer), a Go test in a file this change does not touch.
