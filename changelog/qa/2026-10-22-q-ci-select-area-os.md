A manual dispatch of ci can run one os and one suite: `gh workflow run ci.yml --ref main -f os=windows -f suite=ui`. Pushes and pull requests run everything as before (q-ci-select-area-os).
