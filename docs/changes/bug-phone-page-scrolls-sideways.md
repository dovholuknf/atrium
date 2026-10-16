## Test plan

## @LETTER@. The phone page never scrolls sideways

### @LETTER@1. Header buttons

1. Open `/m/` on a phone about 360 to 412 CSS pixels wide, with documents, requests, items and files all shown.
2. The buttons wrap to a second row. Nothing runs past the right edge and the page does not pan sideways.
3. Open the view drawer and switch between needs you and all. Still no sideways scroll, in a light and a dark skin.

Headless unit: `mNoSideways`. Before and after at 412x892 are in `docs/changes/img/bug-phone-page-scrolls-sideways/`:
`before-412x892.png` has the nav pills cut off at the right, `after-412x892.png` wraps them.
