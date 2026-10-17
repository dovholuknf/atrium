# Resume: the pane keeps its "opening the conversation" line until the first output

A resume's pane could be torn down, or its wait line blanked, in the moment between the socket opening and the
daemon's own card event landing. The launch answer is now recorded on the card at once, and the boot-time restore
no longer blanks a line it did not write. The headless `resumeSpinner` unit, which failed about half its runs, is
steady.
