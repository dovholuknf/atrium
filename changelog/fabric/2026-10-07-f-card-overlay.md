A card can run throwaway ziti overlays of its own, held on its inventory. `atrium_overlay` (`POST
/v1/tasks/{id}/overlay`) takes four actions. `up {name}` starts `ziti edge quickstart` in
`<data>/cards/<card>/overlay/<name>` on two ports from the card's range (atrium_port), with an admin password made
for it and written only to that folder, and waits up to two minutes for the controller to answer over the overlay's
own root CA. `identity {overlay, name, roles}` creates a Device identity through the controller's management API and
enrols it with `ziti edge enroll`, so no `ziti edge login` touches the operator's own ziti config. `tunnel {overlay,
identity, mode, services}` starts `ziti tunnel host`, or `proxy` with a card port per service. `tun` is refused with the
command for clint to run in an elevated shell, because atrium never elevates. `down {overlay}` stops that overlay's
tunnelers and quickstart and deletes its folder. A card has as many overlays as it wants. Each is an `overlay` row, its
identities `identity` rows, its processes `proc` rows and its ports `port` rows. Closing the card stops processes
newest first (tunnelers before their controller), then deletes each overlay's folder, PKI and identities with it. The
sweep frees an overlay or identity whose folder or file is gone. An overlay never carries the board. Design:
`docs/rnd/card-lifecycle-design.md` section 10, Q8 and Q9, phase 10.

Test plan:
- From a card, call `atrium_overlay {action: up}`. It answers a controller on 127.0.0.1, a password file and
  `ready: true` within two minutes. `ziti edge login <controller> -u admin -p <password> --cli-identity t` works.
- `atrium_overlay {action: identity, name: alice}` answers an enrolled `alice.json` in the overlay's folder.
- `atrium_overlay {action: tunnel, identity: alice}` starts a host tunneler. `{mode: tun}` is refused with a
  `ziti-edge-tunnel run -i ...` command to hand to clint.
- Bring up a second overlay, then `down` the first. Only the first one's processes stop and its folder goes.
- Close the card. Every quickstart and tunneler is gone, and `<data>/cards/<card>/overlay` is empty.
