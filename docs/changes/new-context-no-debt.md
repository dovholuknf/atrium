## Test plan

## @LETTER@. A new context owes its launcher nothing

### @LETTER@1. Cycle a launched worker

1. Launch a worker, let it report, then cycle its context.
2. Let the fresh context end its turn without saying anything.

**Expected:** the launcher gets no silent-stop notice, and the continue prompt lists the says the worker already sent.
