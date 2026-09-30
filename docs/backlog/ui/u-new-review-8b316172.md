# Review of 8b316172 (@ui growler reply: long question, growing reply, choices, steady hover)

Reviewed by @review, 2026-09-30, from `git diff 8b316172^ 8b316172`, desktop `js/growl.js` and phone
`m/js/growl.js`, read only. Hub side. Board and headless checks are @ui's, and I ran none.

## What holds

- **Escaping.** On the desktop, `growlBodyHTML` escapes every line before it adds `<p>`, `<ol>`, `<ul>` or `<pre>`.
  The `start` attribute is a `Number`, and a choice goes through `esc` both in `data-choice` and as the label. The
  phone uses `mMd.render`, which escapes first and passes only http and https links, with `noopener`.
- **A choice is a reply.** Choices go through the same `/v1/tasks/{id}/message` with `when: "done"`, queued and never
  typed. The draft is cleared only when the send succeeds.
- **Redraws keep nodes.** `growlReconcile` swaps only a part whose markup changed. Nothing else mutates nodes inside
  the growler: undo lives on a toast, and `growlPost` redraws from data. So a kept node never holds a stale disabled
  button or a stale label. Drafts and the caret are restored only on the nodes that were rebuilt.
- **Keys.** Enter sends, Shift+Enter makes a newline, IME composition is left alone, and Escape folds only a full-size
  box. The phone's Enter on the block reason still sends the block.

## Findings

### Low

1. **The phone makes a question's links clickable, and the desktop deliberately does not.** The desktop comment says
   "no links: a question is model output and some of it echoes what a tool read". The phone renders the same body
   with `mMd`, which turns `[x](https://...)` into a link. The scheme allow list keeps it from being script, but the
   two surfaces disagree on one decision. Should the phone growler render without links too, or should the desktop
   take `mMd`'s rule?
2. **One press on a choice sends at once, and a double press sends twice.** Nothing disables the button while the
   POST is in flight, and the growler stays up after a send. A stray tap on a phone sends an answer to the agent.
   Could the choices disable themselves until the send answers, the way a reply box clears?

HUB DEPLOY OK 8b316172
