Docs: none, the walkthrough ships with this change as docs/fabric/room-over-zrok.md

## Test plan

## @LETTER@. A room over zrok private

### @LETTER@1. The mark

1. Open the Rooms view with `?demo=rooms`.
2. Look at the `m1mini` row, which is on zrok.

**Expected:** its badge shows the green zrok rocket and `zrok`, at the same size as the OpenZiti mark on `sg4`. The
zrok path chip beside the others is on, up, off or down like the ziti one, and a failing one carries its reason in the
tip.

### @LETTER@2. Give a room the share by hand

1. On a room that joined another way, run `atrium room transports zrok --token <share token>`.
2. Run `atrium room transports`.

**Expected:** a `zrok private share` rung is listed. Without the token the same listing says how to add one.

### @LETTER@3. The walkthrough

1. Follow `docs/fabric/room-over-zrok.md` with a zrok account, on a throwaway hub and room.

**Expected:** the room attaches over the share and the Rooms view shows the rocket and `zrok`.
