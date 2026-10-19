Docs: none, the hover tip was not described on a reference page

## Test plan

## @LETTER@. A file path in the terminal has no hover tip

### @LETTER@1. Hover

1. Open a card whose terminal shows a path to a file in its directory, such as `src/main.go`.
2. Move the pointer onto the path and rest it there.

**Expected:** the path is underlined and the pointer is a hand. No box appears, so no "click to open it" text.

### @LETTER@2. Click

1. Click the path.

**Expected:** the menu opens with open in atrium's editor, open in a tab, and open on the room when it has an
editor command. A markdown file offers open rendered first.

### @LETTER@3. Image and directory

1. Click a path to a `.png`.
2. Hover a path to a directory.

**Expected:** the image opens in a tab at once with no menu. The directory keeps its tip, "opens the file browser
here".
