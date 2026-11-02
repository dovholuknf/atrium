## Test plan

## @LETTER@. A tangent is a named scratch folder and a card

### @LETTER@1. A new name

1. With `git.scm_root` set, run `atrium tangent "my idea"`.

**Expected:** a card titled `my-idea` starts in `<git.scm_root>/scratch/tangents/my-idea`, in the tangent window.

### @LETTER@2. The same name again

1. Close that card, then run `atrium tangent "my idea"` again.

**Expected:** the folder is still there with its files, and a new card starts in it.
