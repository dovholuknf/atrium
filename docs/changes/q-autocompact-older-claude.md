Docs: none, test-only change

## Test plan

## @LETTER@. The older-claude autocompact test is stable

### @LETTER@1. The autocompact tests still pass

1. Run `bash scripts/test.sh all -run 'Autocompact|TestAnOlder|TestARunnerThatLists' ./internal/daemon`.

**Expected:** every test passes, and `TestAnOlderClaudeIsStartedWithoutTheFlagAndTheDetailsSayWhy` no longer fails with
"a claude without --autocompact was given it" under parallel load.
