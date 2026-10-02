# Review: u-joined-click, 64a7e436..95a5f3a4 (m1mini, 2026-10-02): OK

claude/u-joined-click, one commit, board only, clint's ask (not held by the pause). A click on a joined row (live, in
a terminal atrium does not hold) opens a dialog instead of doing nothing. Unsigned.

## What holds

- **Only real paths are live.** "details" (`openTask`) and "message it" (`POST /v1/tasks/{id}/message` with
  `when: "done"`, which `handleMessage` takes: messages.go `parseWhen`, an operator message with no `from`). "end it"
  and "take it over" are drawn disabled with the reason on hover, because no path reaches a joined session: exit
  types into a terminal atrium does not hold, and kill stops only what atrium started. Take-over is filed for
  @runtime as `docs/backlog/runtime/u-new-take-over-joined.md`, and no room code is built.
- **No injection through the dialog.** `askUser`'s body is innerHTML (the u-new-sec-dialog-html item), but both
  bodies here are fixed strings. The card's title goes only into the dialog titles, which are set with
  `textContent`. The row's `joinedClick('${t.id}')` is the same card-id inline handler the row already used for
  `attachTask`.
- **`disabled` on an askUser button** sets `el.disabled` and a `title`, so a disabled button cannot be clicked or
  submitted. A small, general addition.
- **Tests.** The `joinedLive` section now expects `joinedClick` where it asserted no click, and the new `joinedClick`
  case is added. I read them and did not run them, per the board rule.

## Low

- **Delivery to an idle joined session.** The second dialog says the message "reaches the session on its next tool
  call or when its turn ends". A joined session that is already idle does neither until someone types into it in
  that other terminal, and atrium cannot type there. Say: "if it is idle, it gets this when someone next types into
  it there".

Verdict: OK 64a7e436..95a5f3a4, hub-ok and room-ok.

Quality: after the Sonnet switch, right-sized. It offers what exists, shows what does not and why, and files the
room work rather than faking it.

## Re-read at c7f37de8 (2026-10-02): OK 64a7e436..c7f37de8

The text only changes, and the low is closed: the message dialog now says that an idle session makes no tool calls,
so the message waits until someone types into that other terminal. The body is still a fixed string, so it is safe
under askUser's innerHTML.

Verdict: OK 64a7e436..c7f37de8, hub-ok and room-ok.
