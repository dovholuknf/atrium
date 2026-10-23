## Test plan

## @LETTER@. The board check scripts pass

### @LETTER@1. The switcher and title checks hold

1. Run `bash scripts/check-titles.sh`.
2. Run `node scripts/board-source.js > page.html` then `node scripts/check-switcher.js page.html`.

**Expected:** the first prints "no native title tooltips on the board." and the second "the switcher invariants hold."
Docs: none, check-only change
