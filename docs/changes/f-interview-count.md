## Test plan

## @LETTER@. Interview question count

### @LETTER@1. Every question says where it is

1. Launch an interview with `atrium_launch` and `kind: "interview"`.
2. Read the first question, then answer it.

**Expected:** each question starts with "Question N of M". When the interviewer changes how many it plans to ask, the next one says so, for example "Question 4 of 7, up from 5". Questions are still asked one per reply.
