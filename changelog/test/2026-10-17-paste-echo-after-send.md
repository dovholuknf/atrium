- `pasteSpinner` echoes the quick paste back once the bytes have left, not on a fixed 60 ms timer. The send waits for
  a frame, and on a loaded macos runner the echo came first, said nothing, and the spinner never cleared.
  paste-echo-after-send.
