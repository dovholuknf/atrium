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
- **Hover a path** the agent printed for its name and size. A path outside the card says so unless the room
  opens it: a folder atrium knows or one in the `open_paths` setting.
- **Click a path.** A picture, an SVG or a PDF opens in atrium's viewer, a tab in the browser you are sitting at. A
  text file offers the viewer (markdown rendered, JSON indented), atrium's editor, and **open externally**. An HTML
  file offers **open rendered** and **open raw in a tab**. Rendered draws it in a sandboxed frame where no script
  runs and nothing outside the file loads, and the viewer's button switches between the two.
- The link menu closes when you scroll the terminal or type into it. Output scrolling past leaves it open.
- **open externally** opens a picture, a PDF or a text document with the default app of the machine the room runs
  on, not the one with the browser. A program or a script is refused, because its default app runs it. The viewer
  has the same button.
- **open in the editor** starts your editor on the machine atrium runs on, when `editor_command` is set. **open in a
  terminal** opens a terminal window there, when `terminal_command` is set. Both are off until you set them, and
  neither runs through a shell.

## Containment

Everything resolves through one check against the card's own directory. A path outside it answers `403`, whether
or not it exists, except that viewing, listing and downloading also accept a path under an atrium path (the card
root, any card's directory, the repository clones and the worktree root) or under a folder in the `open_paths`
setting. Both are resolved through symlinks and `..` first.

The directory picker used when launching is bounded to the roots you configure, and completes paths with Tab.
