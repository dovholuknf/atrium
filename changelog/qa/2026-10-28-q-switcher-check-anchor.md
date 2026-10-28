The switcher check finds the click-outside rule again: it looked for `if (!inside) dlg.close();`, which quick launch changed to `if (inside) return;` (q-switcher-check-anchor).
