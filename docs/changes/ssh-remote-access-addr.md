## Test plan

## @LETTER@. Remote access address pick

### @LETTER@1. Loopback is this machine's sshd

1. On a machine with no WSL, run `atrium room remote-access on`.

**Expected:** no extra lines, and the door dials 127.0.0.1 as before.

### @LETTER@2. wslrelay holds loopback 22 (Windows with WSL)

1. On a Windows machine where `wslrelay` holds 127.0.0.1:22, run `atrium room remote-access on`.

**Expected:** a line says 127.0.0.1:22 is not this machine's sshd, then `so the door dials <address>, which is this machine's own sshd. override with --addr`. `atrium room remote-access` shows that address.

### @LETTER@3. No address answers

1. On a machine where no local address answers as its own sshd, run `atrium room remote-access on`.

**Expected:** `no address of this machine answers as its own sshd. run: atrium ssh setup --check`, and the default address is kept.

### @LETTER@4. --addr wins

1. Run `atrium room remote-access on --addr 192.0.2.5`.

**Expected:** no pick is made and the door dials 192.0.2.5.
