## Test plan

## @LETTER@. Launch tools take item as a string

### @LETTER@1. Schema type

1. List the tools of the control MCP and read the `item` argument of `atrium_launch` and `atrium_launch_worker`.

**Expected:** `item` is a string, the backlog id, and a launch with `"item": "<id>"` starts the worker.
