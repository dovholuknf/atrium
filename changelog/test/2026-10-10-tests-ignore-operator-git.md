Go tests no longer read the operator's global or system git config, so a core.hooksPath, signing or a missing identity cannot fail them.
One helper, testguard.Git, sets it up for every package that runs git. tests-ignore-operator-git.
