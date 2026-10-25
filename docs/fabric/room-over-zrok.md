# Attaching a test room over zrok private

The steps to attach a second machine as a room over a private zrok share. Steps marked **(clint)** need a zrok account
and are the operator's to run. The hub listens on zrok only when it is started with `--transport` naming it, or when
the gear's Transports section turns it on. Steps 1 to 6 use a throwaway hub. "Adding a zrok room to the live hub" is
last.

How it works: the hub reserves a PRIVATE zrok share on its own zrok environment and prints its token. A room dials
that share with the token, then spends a one-time secret over it to get a hub-signed certificate, the same enrolment
direct and ziti use. The token is the gate. Before its first dial the room creates its own access to the share, as
`zrok access private <token>` would, once per process. See `docs/fabric/ziti-zrok-flow-design.md` and
`docs/fabric/hub-and-rooms.md`.

## What each machine needs

- The hub machine and the room machine each need the `zrok` environment enabled (`zrok enable`), which writes
  `~/.zrok2`. Atrium reads that environment and never holds the account token itself.
- Both can use one zrok account. They each get their own environment.
- Nothing else: no port open on either side, no ziti identity, no tunneler.

## 1. Enable zrok on the hub machine (clint)

```
zrok enable <your account token>
zrok status
```

`zrok status` should show an enabled environment. If you have no account token, get one from your zrok instance's
console.

## 2. Enable zrok on the room machine (clint)

The same, on the machine that will be the room:

```
zrok enable <your account token>
zrok status
```

## 3. Start a throwaway hub listening on zrok

On the hub machine, with its own directory and ports so it is not the live hub. `--transport` takes a comma list and
the FIRST one mints join strings, so `zrok` alone is the cleanest test:

```
atrium run --transport zrok --atrium-dir <scratch dir> --addr 127.0.0.1:17778 --link 127.0.0.1:17779
```

`atrium run` also starts this machine's own room when none answers on the room's default ports. On a machine that
already runs a live room, that is the live room answering and it is left alone.

The hub log says `a private zrok share` once the share is bound, and writes the token into `<scratch dir>` as
`zrok-share` (mode 0600) so `atrium rooms add` can mint from beside it.

THE SHARE SURVIVES A RESTART. On each start the hub reads `zrok-share`, asks zrok whether that share is still on its
environment, and reuses it when it is. It reserves a new one only when zrok says the share is gone, which the log says.
When zrok cannot be asked (the controller is down) the hub starts without the zrok listener, reserves nothing, and
retries on a backoff, one log line per attempt, and the board's listeners show zrok down with why. When the listener
dies under a running hub (the share was deleted or retired) the hub binds again the same way. A room already joined
carries the token in its join, so it reattaches after a hub restart with no new string.

## 4. Add the room and get its join string

On the hub machine, against the same scratch directory:

```
atrium rooms add testroom --transport zrok --atrium-dir <scratch dir>
```

It prints one line to paste on the room machine. Good once, for an hour, for that name only. `atrium rooms token
testroom --atrium-dir <scratch dir>` mints a fresh one.

## 5. Join from the room machine

On the room machine, paste the line:

```
atrium room join <the string>
atrium room
```

The string carries the share token, so nothing else is needed. If the room joined some other way first (direct, say)
and you want zrok as a rung, give it the share token by hand. It is the one in the hub's `zrok-share` file:

```
atrium room transports zrok --token <share token>
atrium room transports
```

`atrium room join --zrok-private <share token> <string>` does the same at join time.

## 6. Check it

- `atrium room transports` on the room lists the rungs best first. `zrok private share` is one of them.
- The board's Rooms view shows the room with the zrok rocket and `zrok`. A room on zrok because a better rung failed
  wears the amber `fallback`. The paths line shows `zrok` as on, up, down, off or unlistened. Press the `zrok` chip
  to turn the rung off or on. The room refuses to turn off its last working path.
- `atrium rooms transports zrok,direct --atrium-dir <scratch dir>` sets the order every room is told. A room's own
  `atrium room transports order <list>` wins on that machine.

## When it does not attach

| What the log says | What it is |
| --- | --- |
| `this machine has no zrok environment yet` | `zrok enable` was not run on that machine (steps 1 and 2) **(clint)** |
| `no hub is running over zrok from <dir>` | `rooms add` ran before, or beside a different directory than, the hub. Start the hub first |
| `zrok: no share token on this room` | the room has no token. Step 5, `room transports zrok --token` |
| `not dialling that zrok share again for <n>s. the last dial: <cause>` | the room backs off and retries. The cause is the last failure: the share is gone, no access, the hub is not answering on its share, or the zrok controller is not answering |
| `reserved a private zrok share. rooms joined over the old one need rooms token` | the saved share was deleted on the zrok side, so the hub reserved a new one. Mint a new string with `rooms token` for each zrok room |
| `could not ask zrok whether the share this hub saved is still there` | the controller did not answer. The hub keeps the saved share and retries, and reserves nothing |
| `the room listener over zrok stopped, binding the share again` | the share went under a running hub. The next bind reserves a new one when zrok says it is gone. Mint a new string with `rooms token` for each zrok room |

## Tear down

Stop the hub with `atrium stop`. A stop deletes NOTHING: the share stays on your zrok account for the next start to
reuse, which is what keeps a room attached across a deploy. To retire it for good:

```
atrium rooms retire-share --atrium-dir <scratch dir>
```

That deletes the share and removes `zrok-share`. A running hub over zrok binds again and reserves a new share, a
stopped one does at its next start, and either way every zrok room needs `atrium rooms token <name>` and a rejoin. A
killed or abandoned hub leaves its share on the account the same way (`zrok overview` lists it, `zrok delete share <token>` removes it **(clint)**).

## Adding a zrok room to the LIVE hub

`scripts/live/live-common.ps1` passes NO zrok flag, and a deploy never turns zrok on because a file exists. The gear's
"Room link over zrok" row is on the hub always, off until the operator turns it on, and the hub's stored transport
settings decide from then on, as they do for ziti. `--transport ...,zrok` would seed the row for a throwaway hub.

1. **(clint)** On the hub machine, once: `zrok enable <account token>`, then `zrok status`.
2. **(clint)** From the hub machine, open the gear, "expose the board", and press `turn on` on "Room link over zrok".
   The row reads `bound`, the hub log says `reserved a private zrok share` once, and `zrok-share` appears in the hub
   directory. The setting is stored, so every later deploy rebinds the same share.
3. **(clint)** Add the room: `atrium rooms add claudevm --transport zrok --atrium-dir <hub dir>`. It prints one join
   line, good once for an hour.
4. **(clint)** On claudevm, run `zrok enable <account token>`, then `atrium room join <the line>` and `atrium room`.
5. The Rooms view shows claudevm on `zrok`. Deploy once more and watch it reattach with no new string.

Do not start zrok on the live hub from a worker. Steps 1 to 4 are the operator's.