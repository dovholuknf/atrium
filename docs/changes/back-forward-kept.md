### @LETTER@1. Side buttons over a terminal

1. Open a terminal card, then switch to another view and back so history has entries.
2. In the terminal run something that turns mouse reporting on (`printf '\e[?1000h\e[?1006h'`).
3. Press the mouse back and forward side buttons over the terminal and elsewhere on the board.

**Expected:** each press steps the browser history, with mouse reporting on or off. Left clicks in the terminal still report to the runner.
