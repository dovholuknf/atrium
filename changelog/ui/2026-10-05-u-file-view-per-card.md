# An open file stays with its card, and the editor fills the panel

In the terminals view, a file opened over a terminal stayed on screen when you clicked another agent in the list. A file
now belongs to the card it was opened on. Switching away hides it and shows the other card's own file or its terminal,
and switching back restores it with its unsaved edits and scroll. Closing it closes it only for that card, and a card that
goes away drops its file. Nothing is stored: a reload still starts with no file open.

The editor also filled only about half the panel and left a dark gap under it. It now runs down to the footer hint line at
any window height and scrolls inside itself.
