provision-room.ps1, room-toolchain.ps1 and room-check.ps1 now say, read only, when the room's account is an
administrator (Windows elevated token or Administrators, Domain Admins, Enterprise Admins, Backup Operators or Hyper-V
Administrators read from whoami so a filtered UAC token counts, root, admin, sudo, wheel, docker, lxd, incus-admin,
libvirt, any working sudo -n -l) or the operator's own login, as a warn pointing to the new docs/room-accounts.md. The
probe is time capped, and one that cannot answer is a warn. -IAcceptRunningAsMe accepts it,
-RequireDedicatedAccount refuses it. Item f-room-accounts.
