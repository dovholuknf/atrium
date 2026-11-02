Docs: none, a fix that makes the Ctrl-K switcher dismiss like the other board dialogs, no new verb, flag or setting

# The Ctrl-K switcher dismisses on Esc and on a click outside it

The board's shared dialog click handler in `internal/api/web/js/toasts.js` closes a dialog on a click outside its form,
but it carved out the switcher: a click outside put focus back on the switcher's field and left it open. The carve-out
is removed, so the switcher now closes on a click outside its form like every other dialog, and Esc still closes it.

`switcherDismissSection` in `scripts/test-board-headless.js` opens the switcher and checks that Esc closes it, that a
click inside the form leaves it open, and that a click outside closes it.
