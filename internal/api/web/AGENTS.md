# internal/api/web

The board is `index.html` (the markup), the stylesheets in `css/`, plain scripts in `js/` loaded in order by
`<script>` tags at the end of `index.html`, the phone board in `m/`, `read.html` and the service worker `sw.js`.
`internal/api/web.go` embeds the whole directory with `go:embed web`.

There is no build step and there is not going to be one. That constraint is why xterm.js is vendored as UMD
rather than as ES modules, and it is why CodeMirror was evaluated and refused (`docs/rnd/charon.md`, section 4).

## Nothing here is checked by a compiler, so run the checks

```bash
bash scripts/check-board.sh    # parses the script block, both partitions, and sw.js
bash scripts/check-skins.sh    # every skin sets the same variables, and Go agrees
```

`check-board.sh` exists because a syntax error shipped once as a blank dashboard with one line in a console
nobody had open. The specific cause is worth knowing because it will happen again: **an HTML comment written
INSIDE a JavaScript template literal, containing a backtick.** The comment is not a comment to the JavaScript
parser, so the backtick ends the string and the rest of the file parses as nonsense.

## Landmines, each of which cost real time

**The temporal dead zone.** `let` and `const` exist from the top of their scope and THROW until their
declaration line runs. Boot code sitting above a `let` calls a function that reads it and dies with `Cannot
access X before initialization`, thrown into a promise nobody awaited, so one feature stops dead and the rest
of the page looks fine. **`js/boot.js` is the LAST script `index.html` loads for this reason.** Keep it there.

**Do not chunk a paste.** `sendInput` writes one frame. Claude Code detects a paste from a burst of input, so
splitting a large paste into frames produces a screen full of `[Pasted text #N]` markers instead of the text.
The daemon raised its websocket read limit to 4MB rather than have the browser split anything.

**`e.currentTarget` is null after dispatch.** Hold the element in a local before any `await` or any handler
that outlives the event, or a pointer capture never gets released and a drag continues with no button held.

**Terminal output is a scroll event.** A capture-phase scroll listener fires on xterm's viewport on every line,
which closed menus and tooltips continuously. `fromTerminal(e)` is the guard.

**A window that has a popped-out terminal must never decide the runner exited.** The wind-down stops supervised
runners while the HTTP listener is still up, so the daemon answers yes for the first second of a restart.

## The palette is spelled as channels

Most colours are used at full strength somewhere and at a tint somewhere else, and CSS cannot add alpha to a
hex. So the **triple is the definition** and the solid derives from it:

```css
--sink-rgb: 4,11,20;
--bg-2: rgb(var(--sink-rgb));
```

Writing a tint as its own `rgba(...)` literal makes it a second copy that a skin cannot reach, and the symptom
is a dozen surfaces staying navy under a black skin. A colour with no tinted use anywhere stays a plain hex.

A skin moves the palette and NOTHING else. The type ladder, the space ladder, the radii and the shadow geometry
are layout, and `--uiscale` and `--density` already answer the size question. All ten shipped skins are dark:
`--lift` and `--hairline` are white at low alpha and every shadow is near black, so a light board is a second
set of those rather than a different palette.

**`website` is the one exception.** It is harbour's palette plus the docs site's effects: a gradient primary
button on a glow, a frosted header, and a glow behind the board. clint wanted the site's feel on the board as a
skin rather than as a separate option, so those rules live in `css/themes.css` under `:root[data-skin="website"]`
and nowhere else. Scope any new one the same way, write it across several lines (`check-skins.sh` reads a
one-line rule as a skin block and swallows what follows), and keep the headless check that harbour renders the
same before and after `website` is worn.

## One flow of headings, cut into panes at runtime

The settings dialog and the runners page are both written as a single top-to-bottom flow of headings and their
content, which is what makes them editable. `splitIntoPanes` builds the nav from those headings when the screen
opens.

Adding a section is a heading and its content, in order, with nothing else to keep in step. The headings have to
be DIRECT children, and on the runners page they need `data-pane` because they carry help bubbles and a nav
built from `textContent` would put a whole tooltip inside a button.

## Conventions

- The board asks the daemon rather than remembering. Whether a card has a shell, how many scrollback lines to
  keep, which skins exist: all of it outlives a reload and is changed by things the browser cannot see.
- A path goes in a `data-` attribute and comes back through the DOM, never through an inline handler. HTML
  escaping is not JavaScript escaping, and a directory named `it's mine` breaks out of a quoted string.
- Comment the surprising line. The board is long and a comment on every line means none of them is read.
