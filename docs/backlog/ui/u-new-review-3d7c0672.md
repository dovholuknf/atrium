# Review of 3d7c0672 (@ui: termOnly moves to cardurl.js, which loads right after core.js)

Reviewed by @review, 2026-09-30, from `git show 3d7c0672`, read only. Hub side. Board and headless checks are @ui's,
and I ran none.

## What holds

- **`termOnly` is defined once.** It is in `cardurl.js`, and only its comment is left in `solo.js`. Its body is
  unchanged, and it calls `cardUrlIsCard` in the same file.
- **Loading earlier is safe.** `cardurl.js` still runs nothing at load. `cardUrlScope` guards `ROOM_KEY` with
  `typeof`, and every other function runs only when a later file calls it. `core.js`, the one file now ahead of it,
  refers to neither `termOnly` nor any `cardUrl*` name.
- **No other page is affected.** Only `index.html` loads `solo.js` or `cardurl.js`, and the /m pages call neither.
- **bb48ac29's order is kept.** `cardurl.js` still loads before `notify.js`.

HUB DEPLOY OK 3d7c0672
