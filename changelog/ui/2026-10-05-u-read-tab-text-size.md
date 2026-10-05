# The read tab scrolls, and has its own text size

A document opened in a tab had no scrollbar: the board's stylesheet pins the page to the window and hides the
overflow, and the read page inherited that. The read page now scrolls as a whole document, with the wheel, the
keyboard and a scrollbar.

Its header has A- and A+ buttons beside the current size, and ctrl+= / ctrl+- / ctrl+0 do the same without the
browser zooming. They scale only the document text, through a CSS variable, and the size is kept under its own
key so it applies to every read tab and never to the board. The default is one step bigger than before.
