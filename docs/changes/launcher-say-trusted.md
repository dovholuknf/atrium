## Test plan

## @LETTER@. A launcher's say reads as an instruction

### @LETTER@1. Short say

1. Launch a worker from an orchestrator card.
2. From the orchestrator, say "change how you land" to the worker.
3. The worker's terminal shows `your launcher @name instructs you:` and the text, with no pasted-content box, and the
   worker acts on it without asking for confirmation.

### @LETTER@2. Long say

1. From the orchestrator, say a text of several lines to the worker.
2. The worker's terminal shows one line telling it to read a file, and the file holds the whole text.

### @LETTER@3. A say between peers

1. Say something from one non-launcher card to another.
2. It is delivered as before, queued or typed as a paste, and never as an instruction line.
