- **Six integration tests no longer need Git Bash on the host.** The hook command quoting tests and the classic
  renderer launch test now point `CLAUDE_CODE_GIT_BASH_PATH` at a stand-in file, so they pass on a machine whose Git is
  not under `C:\Program Files`. (flake68-git-bash-tests)
