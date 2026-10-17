- Switching views no longer takes the attached mark off the terminals list's rows. The nav's tabs and the list's rows
  share the `tab` class, and the switch marked every one, so the bridge lost its row until the next repaint that
  changed something. tab-mark-rows.
