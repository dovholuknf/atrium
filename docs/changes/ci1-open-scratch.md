Docs: none, a test fix

## Test plan

## @LETTER@. A link with no repo and no usable card root

### @LETTER@1. Scratch folder

1. On Linux or macOS, set card_root to a folder inside git.scm_root and open a support link with repo `none`.

**Expected:** the card opens in `<scm_root>/scratch/<host>/<name>`, and a close removes it.

### @LETTER@2. No scm root

1. Set card_root to the home folder, with no git.scm_root and no `~/git`, and open a support link.

**Expected:** 422 `no_scm_root`, naming `git.scm_root`.
