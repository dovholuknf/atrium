# q-main-reds-51d5ce4 selectedFits

Cause: `.tgroup` is `width: fit-content` and its name is `nowrap`, so its min-content is the whole name. At a 200px list
the heading could not shrink and, in the wider Inter fallback on Linux and macOS, ran 9px past the column.

Fix: `max-width: 100%; min-width: 0` on `.tgroup` in `css/sharing.css` (phone.css already does the same). The name's
existing ellipsis then truncates it. The unit's threshold is untouched.

PROVEN (ssh claudevm, Windows, real playwright run, WAIT-TIMES printed):
- Unmodified unit passes on the VM before the fix, so the CI font width is not reproducible there.
- With `letter-spacing: .14em` injected to widen headings (probe copy, not committed), before the fix it fails with the
  same message as CI (`heads":[141,230,184]`, col 226), after the fix it passes.
- The real unit passes after the fix: `selectedFits ok`.

INFERRED: that this closes the macOS board-1 and ubuntu board-1-visual jobs. Not run on those OSes. The widened-font
probe stands in for their fonts.
