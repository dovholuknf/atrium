provision-room.ps1, room-toolchain.ps1 and room-check.ps1 now say, read only, when the room's account is an
administrator (Windows elevated token, Administrators, Domain Admins, root, admin, sudo, wheel, passwordless sudo) or
the operator's own login, as a warn pointing to the new docs/room-accounts.md. -IAcceptRunningAsMe accepts it,
-RequireDedicatedAccount refuses it. Item f-room-accounts.
