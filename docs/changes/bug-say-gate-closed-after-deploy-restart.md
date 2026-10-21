Docs: none, the gate now matches what agent-messaging.md already says

## Test plan

## @LETTER@. A say reaches an idle runner after a deploy restart

### @LETTER@1. Idle claude worker whose last reply has an ellipsis and a bracket

1. End a claude worker's turn with a reply holding a line like `Looked at the tests… (3 files)`, leave its prompt empty
   or showing the dim suggestion, then deploy so the room restarts and reattaches it.
2. Wait about five seconds, then `atrium_say` to the worker without force.

**Expected:** the say is typed into its terminal, and the debug panel no longer says "not sure what is on the line".

### @LETTER@2. A draft keeps the gate shut

1. Type half a sentence at the prompt of a reattached worker and leave it.

**Expected:** a say stays queued.
