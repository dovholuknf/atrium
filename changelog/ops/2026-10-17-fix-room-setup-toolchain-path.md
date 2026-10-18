- Provisioning a bare Windows room installs the agent pack. The room setup step now loads the toolchain's environment
  first, so it finds the git the toolchain step installed instead of failing with git not found.
  fix-room-setup-toolchain-path.
