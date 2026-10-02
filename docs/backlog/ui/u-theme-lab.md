# u-theme-lab. Try a theme on a terminal reuses the board's panel

Status: built.

## What changed

- One component, `openLab(spec)` in `notes-files.js`, drives the floating `#skinlab` panel. A caller gives it an id, a title, the `<option>` markup, the current value, `preview`, `keep` and `cancel` callbacks, a `fallback` answer (label, tip, value) and an optional `extra` button. `stepLab`, `previewLab`, `keepLab`, `defaultLab`, `cancelLab`, `closeLab`, `labIs` and the key handler (`labKey`: Escape cancels, Enter on the select keeps) are shared.
- The board's skin (`openSkinLab`) and a terminal's theme (`pickTheme` in `themes.js`) both call it. Opening one while the other is up answers the first with cancel.
- Terminal: titled "how this terminal looks", default answer "use the project" (the empty theme), previews through `previewTheme` and `themePreview` as before, saves only on "use it" through `keepTheme(name)`.
- "edit" lives in the panel, as an extra button under the answers, and opens the theme editor on the selected name. The skin lab has no extra button.
- Detaching or switching the terminal closes its panel (`dropThemePreview`), as it hid the old picker.
- Removed: the inline `#t-theme-wrap` picker, `cancelTheme`, `themeBefore`, `.themepicker` and `.themepick` css.
- The panel's element ids keep the `skinlab` name.

## Tests

`themeLab` in `scripts/test-board-headless.js`. `themePreview` and the busy-button check in `busyGuard` follow the panel. Run each alone with `skinScope`, `skinHeal` and `bootClean`. Arrow keys are tested as the select's own change, since the key itself opens the list on some platforms.
