## Test plan

## @LETTER@. A superlean launch on the room

### @LETTER@1. The flag set

1. POST `/v1/launch` to a claude harness with `{"superlean":true}`.
2. Open the card's `launched` event. `superlean` is true and `cmd` is the command before the lean flags.
3. In the process command line, `--tools Read,Edit,Write,Grep,Glob,Bash,ToolSearch` is there once, and `--mcp-config` holds atrium-control alone.
4. The card's tags hold `atrium:superlean` and `atrium:lean`.

### @LETTER@2. Tags round trip

1. Reopen the card with no `superlean` field. It starts superlean again.
2. Launch it with `{"superlean":false}`. It starts lean and loses only `atrium:superlean`.
3. Launch it with `{"lean":false}`. It loses both tags.

### @LETTER@3. Refusals

1. `superlean` with `lean: false`, on a shell runner, and on an interview card each answer 400 and start nothing.
2. Pass `--tools Bash` in `args` with `superlean`. The command line has that one `--tools`, and the event says `superlean_own_tools`.
