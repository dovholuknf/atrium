A cleared session (Claude Code /clear, or atrium's new-context cycle) no longer takes the screen with it. On Windows the
clear reaches the board as an in-place repaint with no erase-display, so the page that was showing was overwritten and
never reached scrollback. The room now writes a private mark into the stream when it types /clear or sees you submit
one, the board and the room's own screen model push the page into history at that byte, and a reload or reattach shows
the same history. An older board ignores the mark. u-clear-keeps-screen-in-scrollback.
