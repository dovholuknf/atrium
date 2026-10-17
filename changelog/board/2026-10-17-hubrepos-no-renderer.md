- The repos tab no longer logs "no renderer for the hubrepos view" with a stack trace on every board event while it is
  open. It reads on open and on its refresh button, so a board event has nothing to repaint there.
  hubrepos-no-renderer.
