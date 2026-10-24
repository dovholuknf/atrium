Docs: none, a fix that changes no verb, flag, endpoint or setting

## Test plan

## @LETTER@. The room's ssh sync runs no ssh

### @LETTER@1. A minute of sync

1. On a room with ssh on and its key published, watch the processes it starts for two minutes (Process Monitor on
   Windows, `ps` in a loop elsewhere).

**Expected:** no `ssh` and no `ssh-keyscan` process is started by the room. A peer added on the hub still lands in the
login user's authorized_keys within a minute.

### @LETTER@2. The wizard still scans and self tests

1. Run `atrium room ssh setup --check` on a Windows machine with WSL's sshd on loopback.

**Expected:** step 3 names `127.0.0.1:22` as WSL's sshd with its Ubuntu banner and the machine's own address as this
machine's sshd, as before, with no `ssh-keyscan` on the PATH. Step 8 logs in to the room itself.
