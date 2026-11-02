## Test plan

## @LETTER@. The memory probe on macOS and BSD starts `ps` hidden

### @LETTER@1. Hover memory

1. On a macOS room, run a card and hover it.

**Expected:** the details show the card's memory and `ps` is started through `nowindow.Hide`, which does nothing off
Windows.
