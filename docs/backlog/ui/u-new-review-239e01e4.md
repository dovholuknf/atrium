# Review: u-theme-lab, 65737dce..239e01e4 (m1mini, 2026-10-02): OK

claude/u-theme-lab, one commit, JS, CSS and HTML only, clint's ask (not held by the pause). The terminal's "try a
theme" now opens the board's own floating skin-lab panel: one component, not a copy. Unsigned.

## What holds

- **One component, driven by a spec.** `openLab({id, title, options, value, preview, keep, cancel, fallback,
  extra})` owns the panel, the arrows, the keys and the answers. The owner owns what a preview paints, what a keep
  saves and what a cancel restores. Opening one owner's lab cancels the other's first (`if (labNow) cancelLab()`),
  so two owners never share it. `keepLab` saves only when the value changed, and only on "use it". `defaultLab` and
  `extraLab` are the third answer and the extra button.
- **The skin path is unchanged in behaviour.** `previewSkin` still broadcasts to other windows, and
  `applyResolvedSkin` still keeps hands off while the skin lab is open (`labIs("skin")`). `keepSkin` goes through
  `saveSkin`, the settings dialog's path.
- **The theme path is safe.** `previewTheme` paints through the `themePreview` overlay and never touches
  `termTask.theme`, so dropping the old cancel's `termTask.theme = themeBefore` loses nothing. A terminal switch tears
  the pane down (`clearTermPane` → `dropThemePreview` → `closeLab("theme")`), so the panel closes unanswered and
  `keepTheme` cannot save to the card you moved to. The extra button opens the theme editor on the selected scheme,
  as before.
- **Keys.** The panel's `onkeydown` cancels on Escape and keeps on Enter on the select, which now applies to the board
  skin too. The arrows stay the select's own `change`.
- **Removed:** the inline header picker, `cancelTheme`, `themeBefore` and `.themepicker`.
- **Tests.** `themeLab` (new) and `themePreview` (updated) cover the theme lab. `skinScope` and `skinHeal` cover the
  board skin. `busyGuard` now expects `keepLab` and `defaultLab`. The caveat that the arrows are driven as `change`
  events (a real ArrowDown opens the list in headless Chromium on mac) is fair: that is the event the code listens
  to. I read them and did not run them, per the board rule.

Closed: none (no findings)
Open: none

Verdict: OK 65737dce..239e01e4, hub-ok and room-ok.

Quality: after the Sonnet switch, a clean extraction. The component knows nothing of its owners, the owners keep
their own save and restore paths, and the switch-closes-it guarantee carried over through the existing teardown.
