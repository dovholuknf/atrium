CI now runs three jobs per platform side by side: the Go tests, the other Go checks, and the board and scripts. A run
takes as long as its slowest group, not the sum. `scripts/ci.sh --group <name>` runs one group and `--list-groups` names
them. The artifact is now `ci-<os>-<group>-attempt-<n>`. (ci-parallel-groups)
