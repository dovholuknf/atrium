room-toolchain.ps1 -Profile c now probes the -Msys2Dir target before it says MSYS2 would be installed or starts an install, in
-Check and in a real run alike: the drive must exist and be readable, the nearest folder that exists must be writable by the
account, and the drive must have 6 GB free (this script's own estimate for the unpacked base, the packages and the .tmp copy).
A target that cannot take it is a needs-human step (exit 6) with the exact icacls line, never "would install" and never an
install that fails half way. An explicit -GitUserName or -GitUserEmail that differs from the global git config is now set
(-Check says would set), read back, and reported needs-human with the git config command if it did not take; it is never ok
while the config still differs. Not given: the config is left alone. Item f-c-preflight; the rest is in
docs/backlog/fabric/f-new-c-preflight-rest.md.
