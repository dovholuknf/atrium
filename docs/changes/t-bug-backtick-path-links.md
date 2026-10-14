## Test plan

## @LETTER@. A path in a code span is clickable

### @LETTER@1. Wrapped paths

1. In a card's terminal, have the agent print a path inside backticks, quotes, brackets, bold and a table cell, such as
   `D:\work\a.go:42`, `/d/work/a.go`, `~/notes/todo.md` and `src/a.go`, for files inside the card.
2. Hover each one.

**Expected:** the underline covers the path alone, never the backticks, quotes or brackets, and a click opens the file.
