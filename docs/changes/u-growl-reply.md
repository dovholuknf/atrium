## Test plan

## @LETTER@. Answering a question growler

### @LETTER@1. A long question stays in its card

1. Raise a question growler whose body has several paragraphs, a numbered list, a fenced code block, a long Windows
   path and a 200 character token with no break.

**Expected:** the whole body shows in a box with its own scroll, at most 30% of the window high. The path and token
wrap. The code block scrolls sideways inside itself. Paragraphs and the list keep their spacing. The buttons stay in
view and the card never passes half the window or scrolls sideways. Same on the phone board strip and on `/m`.

### @LETTER@2. The reply box grows

1. Type several lines into the reply box. Type forty.

**Expected:** the box grows with the text, stops at its cap and then scrolls. Enter sends. Shift+Enter is a newline.

### @LETTER@3. The bigger box

1. Write half an answer, press the expand control, add to it, press Escape.

**Expected:** the box opens to a taller field in the card and takes the focus. Escape or the control folds it. The
text is kept both ways.

### @LETTER@4. Choices as buttons

1. Raise a question whose body holds a `{choices}` block of three lines.

**Expected:** the block is not in the text. Each line is a button. Pressing one sends that line as the reply.

### @LETTER@5. Hover is steady and the face is not rebuilt

1. Hold the pointer on a growler button while other cards change and terminals print.

**Expected:** the button does not lift or flicker and stays hovered. A half-typed reply keeps its text and caret.
