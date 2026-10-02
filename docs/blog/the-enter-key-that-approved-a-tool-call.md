# The Enter key that approved a tool call

Series: The permission chain and auto mode. Status: outline. Audience: anyone automating input into agent terminals.

**Hook.** Typing a message into a session wrote text and then Enter; with a permission dialog on screen, Enter picked the highlighted option.

**Angle.** Automated typing into a terminal you share with a UI is an approval channel unless you prove the screen first.

**Rests on:** dialog guard, typing gate, Notification hook. See `docs/blog/inventory.md`.

## Story beats

1. How atrium delivers messages: it types into the session's pty.
2. The hole: a native permission dialog already open, and Enter answering it. Nobody approved, and nothing had bitten yet only by luck.
3. The signal atrium used to throw away: the Notification hook's `permission_prompt`.
4. The fix: atrium refuses to type into a dialog it did not raise, and decides from the signal.
5. The family: focus reports counted as typing, paste markers inside typed text.

## Screenshots and demos

- a recording of the dialog and the typed line (on a test room)
- the hook payload

## Sources

- CHANGELOG.md (2026-09-16 entry)
- commit 704b2e9a
- docs/runtime/hooks.md
- changelog/runtime/2026-10-01-r-paste-strip.md

## Notes for the writer

- The repo is public: paraphrase clint, never quote him. Name no outside person. Keep security findings out of the post.
- Screenshots come from a test room, or are drawn mockups, as the website does. Never use the live board with real cards.
