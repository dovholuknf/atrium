# ssh remote access picks the sshd it dials

`atrium room remote-access on` with no `--addr` now checks whether 127.0.0.1:<port> is this machine's own sshd. When it is not (wslrelay holds 127.0.0.1:22 on a Windows machine with WSL, so the door would open onto WSL) it says so and dials the address that is this machine's. `--addr` still overrides. The sshwizard also joins Windows paths the Windows way when it reads another OS's layout. Backlog f-ssh-wslrelay-loopback.
