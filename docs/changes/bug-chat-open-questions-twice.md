Chat: a turn ending on Open Questions shows them once. Before and after at 412x892 are in
`docs/changes/img/bug-chat-open-questions-twice/` (`before.png` has the question in the message and in the block,
`after.png` only in the block).

## Test plan

## @LETTER@. Open Questions are shown once in the chat

### @LETTER@1. A turn that ends on questions

1. Have a session end a turn with `Open Questions:`, a rule and `1. File the thumbnail as a feature?`.
2. Open its card in the phone chat, or in the desktop chat.

**Expected:** the question appears once, in the OPEN QUESTIONS block under the thread. The message above stops before
the heading.

### @LETTER@2. Text that the block does not reproduce stays

1. Answer the questions, or end a turn that says more after its numbered list.
2. Open the card again.

**Expected:** the message keeps its Open Questions text, and so does every earlier turn.
