---
title: Files
description: Sending files into a session, taking them back out, and editing them in the browser.
---

# Files

Over an overlay, the clipboard is on the machine with the browser, not the one with the agent. Atrium moves files
both ways through the board's own HTTP, so it works over an overlay for free.

## Into a session

Attach to a terminal and paste, or drag a file onto the pane. The bytes land in `.atrium/incoming` under the card's
directory, and the path is typed into the terminal **without pressing Enter**, so you can write a sentence around
it and send it yourself.

## Out of a session

- **Browse** the card's directory from the file drawer, and take one file or the whole tree as a zip.
- **Click a path** the agent printed. It opens in atrium's editor in the browser you are sitting at. Escape closes
  the editor, then the drawer.
- **open there** starts your editor on the machine atrium runs on, when `editor_command` is set. **open in a
  terminal** opens a terminal window there, when `terminal_command` is set. Both are off until you set them, and
  neither runs through a shell.

## Containment

Everything resolves through one check against the card's own directory. A path outside it answers `403`, whether
or not it exists. There is no way to ask atrium for a path that is not below a card.

The directory picker used when launching is bounded to the roots you configure, and completes paths with Tab.
