History saved across a room restart no longer prints lines over each other when the terminal is a different width than
the one it was drawn at. The saved file now holds the rows each run left on screen, not the cursor moves that drew them.
The Ctrl-D before a restart marker is the restart asking claude to exit, and a restart never types `/clear`.
bug-scrollback-overprinted-after-resume
