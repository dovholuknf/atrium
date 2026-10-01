## Test plan

## @LETTER@. No ready alert while a card waits on its children

### @LETTER@1. Parent ends a turn with a running child

Launch a card that spawns a child, let the parent end its turn while the child runs. No ready toast, bell or desktop alert fires and the card still shows ready.

### @LETTER@2. Last child ends

End the last running child while the parent is idle. The ready alert fires once.

### @LETTER@3. Children under the parent

In the terminal list a spawned card sits indented under its parent's row under each sort (name, activity, started) and each group mode. A child whose parent is not listed stays a normal row.
