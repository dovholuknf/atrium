# Provisioning installs one permission gate

`room-gate.ps1`, provisioning and `room-check -Fix` no longer copy `atrium-perm-hook.ps1` to a room. They keep only
`atrium hook --event permission`, remove an old script entry from settings.json (backup
`settings.json.bak-<yyyymmddHHmmss>`) and delete the script file. A room with both gates now shows "two permission
gates: atrium-perm-hook.ps1 and atrium hook" instead of "1 missing" (f-provision-one-gate).
