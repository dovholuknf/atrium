Build a stepped, speed-tunable HTML explainer of one feature, drawn in the atrium board's own theme.

# Prompt: build a stepped, speed-tunable explainer for a feature

Fill in the two fields, then follow the rest. The worked example is `scratch/explainer-viz/hears-viz-v2.html` on the
atrium repo: read it before you start, and copy its palette block, its theme loader and its layout rules.

- FEATURE: the feature name, for example "hears" or "the permission chain"
- OUT: output file, for example `scratch/explainer-viz/<feature>-viz.html`

## Task

Read the code for FEATURE and produce ONE self-contained HTML file that explains how it works as an animated,
steppable diagram. Do not commit it to the repo unless told to. Make no code changes.

## Facts come from the code only

- Read the real code first (handlers, store queries, config, docs). Do not rely on memory or on docs alone.
- Every box, arrow, label, number and caption must be backed by something you read. If you cannot find it, leave it out.
- Where the code and the docs disagree, show the code and say so in the notes panel.
- Add a hidden notes panel (a `<details>` element, closed by default, titled "notes and sources"). It lists each
  claim the diagram makes with its source as `path/file.go:line`. Re-read the line before you cite it.
- If the feature has a gap between built and used (nothing calls it, zero rows today), show that in a banner.

## Tech

- One `.html` file. Plain HTML, inline SVG, vanilla JS, inline CSS. No libraries, no build step, no fonts or images
  from outside. The only requests it makes are the board's two stylesheets and `/v1/settings` (see Theme), and the
  page works the same when all three fail.
- Moving dots: `requestAnimationFrame`. Waits and sequencing: `async/await`. Highlights: CSS classes with transitions.
- Time must be scaled by one speed factor. Do not use `setTimeout` for animation. Advance progress each frame by
  `dt * speed`, so the slider also works in the middle of an animation. Drive the highlight transition length from a
  CSS variable that the slider updates.

## Theme: the board's variables, never a colour of your own

The page wears whatever skin the atrium board is wearing. The board's palette is CSS custom properties on `:root` in
`internal/api/web/css/tokens.css`, and every skin in `internal/api/web/css/themes.css` is a `:root[data-skin="name"]`
block that overrides them. Read both files before you write CSS.

- Every colour in a rule, an SVG attribute, an inline style or a script is `var(--token)`. No hex, `rgb()` or named
  colour appears anywhere else in the file. SVG fills go in CSS (`#ah path { fill: var(--stroke) }`) or in
  `el.style.fill = "var(--v-ok)"`, never in a `fill="#..."` attribute.
- Use only the board's tokens: `--bg-0` page, `--card-0` panel, `--card-1` box and caption, `--bg-2` inputs, `--chip`
  buttons, `--btn-hover`, `--stroke` arrows, `--stroke-dim` borders, `--head` titles and input text, `--body` text,
  `--label` panel headings, `--dim` secondary text, `--teal`, `--warn`, `--danger`, `--blue`, `--path`, and the tints
  `--teal-bg` and `--warn-bg`. Fonts are `--sans` and `--mono`. A tint the board has no token for is
  `rgba(var(--teal-rgb), .2)` from its channel triple, never a literal.
- Name the diagram's meanings once, as aliases in the layout block, and use only the aliases below them:
  `--v-active: var(--blue)` (the step running), `--v-pend: var(--warn)` (pending, warning), `--v-ok: var(--teal)`
  (delivered, ok, which is the board's own ok colour), `--v-bad: var(--danger)` (failed, gone),
  `--v-woken: var(--path)` (special). Leave `--wait` alone: on the board it means a runner waiting on the operator.
- Three blocks in `<head>`, in this order, as in the worked example:
  1. `<style id="palette">`: the DEFAULT skin's values of the tokens you use, copied from `tokens.css` `:root`,
     triples and their derived solids both (`--teal-rgb: 0,227,176` and `--teal: rgb(var(--teal-rgb))`). This is the
     one place a literal colour may appear, and it is only what the page wears when the board cannot be reached.
  2. A `<script>` that inserts `<link>`s to the board's `/css/tokens.css` and `/css/themes.css` right after the
     palette block, then sets `data-skin` on `<html>`. The board is `?board=<url>` if given, the page's own origin
     when it is served over http, and `http://127.0.0.1:7778` (the hub's board) when it is opened from disk. The skin
     is `?skin=<name>` if given, else the board's: `board_skin` from `<board>/v1/settings`, with no attribute when it
     is the first name in `board_skins`, which is the default and has no block of its own. Served from the board's
     origin, wear the `atrium.skin` (or `atrium.skin.<room>`) value from localStorage first so it does not flash.
     Every failure is caught and leaves the default palette.
  3. `<style id="layout">`: the page's own rules. It comes after the board's stylesheets, so it must undo their page
     rules: `html, body { height: auto }`, `body { display: block; overflow: auto; background: var(--bg-0);
     font: 14px/1.45 var(--sans) }`, `body::before { content: none }`.
- Check it in at least one light skin (`?skin=paper`) and one dark one (`?skin=noir`): every label, border, dot and
  log line must still read.

## Layout

- Panels 10px radius with a `--stroke-dim` border. `--sans` for text and `--mono` for logs and chips.
- Header with a title and a two-line plain description. Left column: input panels where the reader sets the
  scenario (forms, selects, a "load an example set" button, a fire button). Right column: the SVG stage (viewBox about
  900x430, rounded boxes with a name and a dim one-line sub label, thin arrows), a legend, a caption area, optional
  cards for entities, and a "what happened" log with colored lines.
- The reader must be able to change inputs and re-run, so that different paths (the happy path, the duplicate, the
  failure, the empty match) can all be seen.

## Scenario as a list of named steps

Build the scenario from the inputs as an array. Each step is an object:

```js
{ name: "short title", cap: "what just happened, plain words", run: async () => { /* animate once */ } }
```

- Keep to 5 to 12 steps. One step is one idea: one dot travelling and one box lighting up, plus its log line.
- A step animates once and stops. The caption describes what has just happened (past tense, one or two sentences),
  and it is set when the step ends.
- Branches (duplicate, no match, error) end the list early with a caption that says why. Per-entity work (one delivery
  per card) is one step per entity.
- Before each step, snapshot everything it can change (data, log length, seen-sets). Prev restores the snapshot of the
  previous step, clears dots and highlights, and replays that step once.

## Controls

- Speed slider, 0.25x to 2x, step 0.05, default 0.6x, with the value shown. It scales every wait, travel and highlight.
- Buttons: Prev, Next, Play/Pause, Restart. Disable what cannot apply (Prev at the start, Next at the end, both while
  a step runs).
- Keys: Left is Prev, Right is Next, Space is Play/Pause. Ignore Space while a form field or button has focus.
- A "step mode" checkbox, on by default. On: firing loads the scenario and stops before step 1. Off: firing plays it
  through.
- Show "step N of M: name" with the caption under the stage.
- Play runs the remaining steps with a short scaled pause between them. Pause stops after the current step.
  Restart goes to the start and plays. Cancel in-flight animations with a generation counter, so a stale step never
  draws. Every await that a cancel can reject, the pause between steps in Play included, catches the cancel, so
  firing again in the middle of a play leaves no unhandled rejection.

## Done when

- The file opens from disk with no console errors, and the only requests are the board's stylesheets and settings.
- Outside the palette block there is no literal colour: `grep -nE '(^|[^&])#[0-9a-fA-F]{3,8}\b|rgba?\([0-9]'` finds
  nothing past it.
- It reads in the default skin, in `?skin=paper` and in `?skin=noir`.
- Every path in the scenario can be reached from the inputs.
- Every caption and label has an entry in the notes panel with a file:line.
- Report the path of the file, the list of steps, and anything you could not confirm from the code.
