Docs: none, website/docs/files.md is updated on this branch

`read.html?render#<card>/<path>` draws an HTML file in an iframe with an empty `sandbox` (opaque origin, no script,
no forms, no popups, no top navigation) from `srcdoc`, with a CSP meta of its own put after the doctype:
`default-src 'none'; style-src 'unsafe-inline'; img-src data:; font-src data:`. The bytes are the same text/plain
ones `files/view` already serves, so nothing on the server changed and no file leaves the card. A page that needs a
script, a stylesheet by link or a picture beside it draws without them.

## Test plan

## @LETTER@. An HTML link opens rendered or raw, and the link menu gets out of the way

### @LETTER@1. Rendered or raw

1. In a card's terminal, have the agent print the path of an HTML file in the card, such as `out/topology.html`.
2. Click it.

**Expected:** the menu offers "open rendered", "open raw in a tab" and "open in atrium's editor".
Hovering "open rendered" shows the board's bubble, not a browser tooltip.

### @LETTER@2. The rendered page is sandboxed

1. Choose "open rendered" for an HTML file holding `<script>`, an `onerror` image and a remote `<img>`.

**Expected:** a tab opens on `read.html?render#...` with the page drawn on white. No script ran, devtools shows no
request for the remote picture, and the frame's `sandbox` attribute is empty.

### @LETTER@3. Switch between the two

1. Press "Show the source", then "Show it rendered".

**Expected:** the source shows as text and the address loses `?render`, then the page comes back and it returns.

### @LETTER@4. Scrolling or typing closes the menu

1. Click a file link so its menu opens, then scroll the terminal with the wheel.
2. Open it again and type a letter.
3. Open it again while the agent is printing output.

**Expected:** the wheel and the key each close the menu. Output scrolling the terminal leaves it open.
