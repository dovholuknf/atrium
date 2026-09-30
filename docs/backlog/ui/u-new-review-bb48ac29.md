# Review of bb48ac29 (@ui: cardurl.js loads before notify.js)

Reviewed by @review, 2026-09-30, from `git show bb48ac29`, read only and fast (urgent: the live board and phone
are broken). Hub side. Board and headless checks are @ui's, and I ran none.

## What holds

- **Moving it earlier is safe.** `cardurl.js` has no top-level statement, only function declarations and comments.
  The path notify.js calls at load time (`cardUrlIsCard` then `cardUrlShape`) reads only `location.pathname` and
  its own helpers.
- **Nothing is shadowed.** None of `cardurl.js`'s 19 function names is declared in any other file under
  `internal/api/web/js`, so the new order cannot change which definition wins.
- **Only index.html loads it.** No other page in `internal/api/web` includes `cardurl.js` or `notify.js`.
- **The test closes the hole.** The `/raw` page serves one script tag per file, so a load-order fault shows as a
  pageerror, which the inlined harness hid.

HUB DEPLOY OK bb48ac29
