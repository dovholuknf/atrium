- **chatTab waits for the selected row's mark.** The unit read the row's `on` class once, right after the frame came up,
  and failed when the repaint had not landed. It now waits up to five seconds, and for the phone frame the same way.
