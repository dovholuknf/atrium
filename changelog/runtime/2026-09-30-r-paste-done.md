- **A paste is answered when it is in.** An `in` frame on the attach socket may carry an `id`, and once the pty write
  for that frame returns the room sends `{"t":"in-done","id":...}` back on the same socket, so the board's paste
  spinner stops when the paste is done rather than guessing. A frame with no id gets nothing, which keeps older boards
  safe. The paste still goes as one frame and one write. Room side, needs a room deploy.
  (r-new-paste-done, with @ui)
