# Review: u-repos-pass b3532ee6 + 8437163b (repos tab visual pass, tray head on one row)

Range 2ab744af..8437163b, read by @review on m1mini. The commits are unsigned, like every m1mini commit.

## b3532ee6, the repos tab

- **What it adds:** cards, a clone-URL hero, a status pill, branch rows, an empty-repo how-to and copy feedback.
- **Escaping:** every server value goes through `esc()`. That covers the owner, repo and host, the branch name and
  title attribute, the sha, the room, the card, the card title, the URLs, `data-copy`, `aria-label`, and the time's
  `datetime` and `title`. The how-to command is escaped, with a literal `&lt;branch&gt;`. Copy is still one
  delegated listener reading `dataset.copy`, and there is no inline handler.
- **d3004c30 L1, closed:** an in-flight guard, with refresh disabled until the answer is in. **L2, closed:** the dead
  plainFetch branch is gone. L3 (check the fields against @fabric's endpoint) stays until that lands; it is OK at
  607bb026, and the shape matches.
- **Screenshots** read: full-phone-paper and empty-wide-dark. Both are clean, with no overlap and long branch names
  ellipsized.

## 8437163b, the tray head and phone rows

- `#toastlog .dlg-head` no longer wraps. The title takes `min-width: 0` and an ellipsis, and the buttons are
  `flex: none`.
- On a phone, `.seg` pill rows and `.pane-nav` scroll sideways, and toolbar buttons and `#h-mode` ellipsize. The id
  selector gives `#h-mode` its own rule over the `.seg` one.
- The new trayHead section was read. @ui reports hubRepos, trayHead, notifyOff, notifyCommand, toastStays,
  toastsTop, topNav and bootClean pass alone.

## Lows

- **L1:** `hubReposWhen` is now declared twice in hubrepos.js. The later one wins, and the two are identical. Drop
  one.
- **L2:** the `last push` label is put in with `.replace(">", ">last push ")` on the first `>`. That works because
  every attribute is escaped, but it's fragile. Pass the label into hubReposAge instead. The `.replace("<time ",
  "<time ")` before it does nothing.
- **L3:** the sideways-scrolling pill rows hide their scrollbar, so on a phone nothing hints that there's more. A
  fade at the edge would.

Closed: d3004c30 L1, L2 / Open: L1, L2, L3 (and d3004c30 L3 until the endpoint lands)

Verdict: HUB DEPLOY OK 2ab744af..8437163b.

Quality: a careful visual pass. Escaping is kept everywhere, and the earlier lows are fixed.
