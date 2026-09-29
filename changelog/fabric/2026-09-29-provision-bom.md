- `provision-room.ps1`, `room-git.ps1`, `room-gate.ps1` and `room-toolchain.ps1` work under a pwsh profile that sets
  `$OutputEncoding` to UTF-8 with a BOM. The BOM broke the first line of the sh script piped over ssh, and provision
  then failed at "state" with a misleading exit 6. A probe that lost its first lines now fails with exit 3 and says so.
- `-AttachTimeout` defaults to 120s (180s over zrok), and an attach that times out names the hub's log. (provision-bom)
