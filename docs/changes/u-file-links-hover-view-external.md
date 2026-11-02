Docs: none, website/docs/files.md is updated on this branch

`files/view` serves a `.pdf` as `application/pdf` with no sandbox CSP, because a browser will not run its PDF
viewer in a sandboxed document.

The browser decides "outside this card" from the spelling alone, so a path in another card is now never probed.
`docs/runtime/files-across-cards-design.md` (designed, not built) plans to probe exactly those, and it will need
this rule changed when it is built.

## Test plan

## @LETTER@. Terminal file links: a hover every time, and the viewer

### @LETTER@1. A path outside the card

1. In a card's terminal, have the agent print an absolute path in a sibling of the card's directory, such as
   `D:\worktrees\github\other\notes.txt`.
2. Hover it, then click it.

**Expected:** it is underlined and the tip says "outside this card, the board cannot open it". The click opens
nothing. In devtools, no `files/probe` request carried that path.

### @LETTER@2. A path inside the card

1. Hover a path to a file in the card, relative or absolute.

**Expected:** a tip names the file and its size. A directory's tip still says "opens the file browser here".

### @LETTER@3. The viewer

1. Paste an image into the terminal so it lands in `.atrium/incoming/`, then click the path that appears.
2. Click paths to a `.pdf`, a `.svg`, a `.json` and a `.md`, choosing the first menu item for the last two.

**Expected:** each opens in a new tab: the image as the image, the PDF in the browser's viewer, the SVG as a picture,
the JSON indented and the markdown rendered. A `.zip` shows a Download link instead.
