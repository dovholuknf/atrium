# Review of 2fd77402 + ab758324 (@ui: full safe markdown in /m replies, card files, copy)

Reviewed by @review, 2026-09-30, from `git diff de951162 ab758324` (`m/js/md.js`, `m/js/card.js`), read closely
because it renders model output. Hub side. Board and headless checks are @ui's, and I ran none.

## What holds

- **The boundary is the server, not `inside()`.** Every card file goes through `GET /v1/tasks/<id>/files?path=`,
  which resolves through `internal/safepath`, follows symlinks on both sides, and answers 403 for anything outside
  the card (daemon rule 8). `inside()` only decides what is drawn as a control. A wrong answer there yields a "not
  available" chip, not a read, so symlink names cannot widen anything.
- **inside() itself.** It folds backslashes to `/`, trims the worktree's trailing slash, and needs the path to start
  with `worktree + "/"` and be longer than it. It refuses control characters, any `.` or `..` segment, and any
  `scheme:`. Its compare is case-sensitive, so on a case-blind disk a differently cased path is plain text. That
  fails closed.
- **Escaping.** Every attribute value is `esc`'d and double-quoted: `data-card`, `data-path`, `aria-label`, `href`,
  `data-lang`. `esc` covers `& < > " '`. Labels and alts are unescaped once, then escaped once on the way out.
  Table alignment comes from a fixed set. The fileNode HTML is stashed, so the emphasis pass never reaches into an
  attribute. The bare-path pass runs on escaped text, so it cannot match inside markup.
- **Nothing remote is fetched.** A remote image becomes a link that says "image, not loaded". Links are http or
  https only, with `noopener noreferrer`. Thumbnails are typed by extension and never as SVG, so an image cannot
  carry script.
- **The copy button's text** is the `<pre>`'s `textContent`, the rendered code read back from the DOM. No
  attribute and no reply string is involved.

## Findings

### Low

1. **On a Windows room, no absolute path is ever a card file.** Worktrees are stored as `D:/worktrees/...`. A path
   like `D:/worktrees/x/a.png` does not start with `/`, so it falls to the `scheme:` test, which `D:` matches, and
   `inside()` answers false. Thumbnails and file taps therefore never appear for cards on sg4 or sg3, only on
   m1mini. It is safe, but the feature is inert on most rooms. Could `inside()` treat `^[A-Za-z]:/` as absolute,
   comparing case-insensitively on a drive path, and could mHostile gain a drive-letter case?
2. **Thumbnails are fetched again on every redraw, and their object URLs are never revoked.** A reply is redrawn by
   `innerHTML`, which happens on each `output_at` move. That makes new `.md-img` nodes, which `hydrate` fetches
   again, each with a new `createObjectURL`. A long thread with images re-downloads them on every reply and leaks
   the URLs for the life of the page. Could fetched blobs be cached by card and path, and revoked when the card
   closes? `saveFile`'s URL can be revoked right after `click()`.

### Nit

1. `navigator.clipboard` needs a secure context, so over a plain-http LAN address copy always says "not copied".
2. The MutationObserver runs `hydrate(document)`, a `querySelectorAll`, on every DOM change anywhere on the page.
   That is cheap, but observing the replies box alone would do.

HUB DEPLOY OK 2fd77402 ab758324

## Re-read of 94774247, 2026-09-30

Both lows are closed.

- (a) A `D:/` or `D:\` path counts as absolute after slashing, and is compared case-insensitively when the worktree
  has a drive. A `/`-rooted path against a drive worktree, or the reverse, is refused, and so are a `c:x` with no
  slash and any real scheme. The server's safepath is still the boundary.
- (b) Pictures are cached by card and path, capped at 40, and revoked on drop and on card close. A redraw does not
  fetch again. The save URL is revoked after 10 s.
- Both nits are closed: the observer is on `m-replies` and `m-recap` only, and copy falls back to `execCommand`
  over a selected textarea.

Nit: an eviction revokes a URL that a drawn thumbnail may still show, so a tap to enlarge that one fails. A fetch
still in flight when its key is evicted creates a URL that nothing holds, which leaks it. Both need more than 40
pictures in one card, so they are rare.

HUB DEPLOY OK 94774247
