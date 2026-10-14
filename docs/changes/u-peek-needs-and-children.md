## Test plan

## @LETTER@. The peek's needs and children sections

### @LETTER@1. Questions under NEEDS YOU

1. Open the peek of a card with open questions.

**Expected:** the questions sit directly under the NEEDS YOU heading, with no "open questions" label.

### @LETTER@2. Children are a shut summary

1. Open the peek of a card with several children, some paused or dead.

**Expected:** the children heading is shut and reads like `children  40 · 3 need you · 1 running · 8 paused · 28 dead`,
with zero parts left out.

### @LETTER@3. Children open to one per line

1. Open the children heading.

**Expected:** one child per line, the ones that need you first, each a link that opens its card, with a status word at
the right. Children hidden by the filters are counted in the heading and not listed. Two children with the same title
show their room or handle.
