## Test plan

## @LETTER@. Terminal file links after a late file, and images

Pictures: `docs/changes/img/bug-term-file-links/` (`late-before.png` and `late-after.png`, `image-before.png` and
`image-after.png`). Headless: `HEADLESS_UNITS=termLinkLate node scripts/test-board-headless.js`.

### @LETTER@1. A file written after it was hovered becomes a link

1. In a terminal, have the agent print a path to a file that does not exist yet, and hover it. It stays plain text.
2. Create the file, wait a few seconds, and hover the path again.

**Expected:** the path is underlined and clicking it opens the menu. No card switch is needed. A path that was a file
stays a link.

### @LETTER@2. An image opens in a tab without a menu

1. Print a path to a `.png` under the card, such as `.atrium/incoming/shot.png`, and click it.

**Expected:** the image opens in a new tab at once, with no menu. Binary files do the same. A text file still shows
the menu with the editor and the tab.
