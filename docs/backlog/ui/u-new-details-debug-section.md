# u-new-details-debug-section. The terminal's details drawer gets a debug section

Status: done 2026-10-04, see Design note. Owned by @ui. Filed by the orchestrator 2026-10-01, from clint.

## What clint asked

"Can we add this support to the 'details ^' thing we have? That would be better in there. I want that details to
slide up drawer style was my original thought, but I sort of like the way it pops too. So can we just add a
'settings' or 'debug' section to that window when clicked from a terminal?"

"This" is the typing gate readout, today a settings checkbox ("show the typing gate readout") that puts a line
under every terminal. See `u-new-gate-readout-only-when-blocking.md` for what the line says.

## Wanted

- When the details view (`js/peek.js`, `peekBody`) opens from the terminal's shortcut strip, it carries one more
  section, "debug", for the attached card. It is absent when details opens from a card hover or the card menu.
- The section shows the gate live while the drawer is open: open or closed, the reason, the count of characters
  on the line, last key, and messages held for this card with who sent them. While the gate waits out its quiet
  time, it says "closed, opens in 2s" and counts down, so a closed gate after typing does not look stuck.
- Per-terminal debug switches live there too: the gate readout line and the input lag log. These are per-browser
  today, so they follow `u-new-browser-prefs-every-window.md` and reach every window.
- Keep the way details pops today. No change to how it opens.
- Bug, clint 2026-10-01: opened from the terminal strip, details closes only on its own "details" caret. A click
  anywhere outside it must close it, as the hover and menu popovers do. Escape too. A click inside the terminal
  closes it and still focuses the terminal.
- The settings dialog keeps its checkboxes or points at the drawer. Say which in the design.

## Facts for the design

- The gate runs in the daemon whether or not the readout is on (`internal/daemon/supervisor.go`, `gateLocked`).
  The readout only shows it. A section that reads it when opened costs nothing when closed.

## Design note

- The section, the countdown, the outside click and Escape, and the switches' move into the drawer were built on
  2026-10-01 (`js/peek-debug.js`, commit e310a401). This pass adds the missing pieces: the held row reuses
  `typingGateText(s, held)`, so a blocked gate reads in the gate line's own words ("blocked: 2 messages from @runtime
  wait: ..."), and the headless test now also covers a click in the terminal (closes, terminal focused) and the card
  popover having no debug section.
- The settings dialog no longer carries the two checkboxes. It points at the drawer. The switches live only there, so
  there is one place to look, and they read and write the same localStorage keys as before (`atrium.debug.typing`,
  `atrium.debug.inputlag`), so the cross-window work needs nothing from this.
- The section reads the gate only while the drawer is open (500ms poll, none in a hidden tab), so closed it costs
  nothing.
