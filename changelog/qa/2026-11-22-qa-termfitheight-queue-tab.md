# termFitHeight failed on batch 11

Batch 11 carried the termFitHeight assertion for a short pty but not the one line of product code it tests.

The Queue tab commit (54e6bf1f0) was cut from a tree with 9c452ad43 and brought its test hunk, while 9c452ad43 itself
reached `claude/main` and never reached batch 11. `takeTermSize` then never called `sendFocus` for a pty shorter than
the pane, so the test saw no focus frame. This change adds that line to `js/terminal.js`.

The Queue tab code did not break focus, the attach seq guard or the layout.
