---
title: Overlays and sharing
description: Reaching the board from another machine over zrok or OpenZiti, and lending one session to one person.
---

# Overlays and sharing

The board listens on loopback and has no login. That is on purpose. An authentication layer invented here would be
worse than the overlays that exist, so reaching the board from another machine is an overlay's job. Atrium drives
the overlay for you.

## What atrium does, and what it does not

Atrium keeps the configuration, opens the listener, and shows what came back.

- It **never decides who may connect.** That is a policy on the overlay, and it stays there.
- It **never proxies traffic.** The zrok or OpenZiti SDK hands back a listener, and the board answers on it with
  the same handler the local board uses.
- It **never issues an identity.** An identity comes from enrolling against a network somebody else runs.

A share that fails never takes the local board down.

## zrok

Atrium needs a zrok environment first. Paste your account token and press **enable**. The token never leaves
atrium.

- **Private** is the default. The other end runs `zrok access private <token>`, and the board shows that command.
- **Public** gives a URL on a zrok frontend. Turning it on asks first and says plainly that whoever opens the link
  can read every command and answer permission requests. A public share is refused without a login.
- **Keep the address.** Reserve a name from the board with **reserve it**, and a public share keeps its URL across
  restarts. A private share asks for the same token again.

The panel shows the share's name as your zrok account calls it, so you can find it in the zrok console.

**Revoke** a share from the header pill or the card menu, whether or not zrok is up. Right-click the `shared` chip
to go straight to the stop confirmation.

## OpenZiti

Atrium needs an identity. Paste the one-use enrollment token your network administrator issued and press
**enroll**. Atrium reads the token first, so it can say which network it is for and refuse an expired one with a
date. Then it binds the board to a named service on the listener the SDK returns. Atrium never creates a service,
writes a policy or talks to a controller for you.

## A login on the published board

A published board can ask who you are, with a name and a password, an OIDC provider, or both. The login guards
only the published listener. The loopback board still has none.

- A name and a password is a complete setup. Somebody sharing a board for an afternoon has no OIDC provider to
  hand.
- With both set, the password is checked first, so the board still opens when the provider is down.
- The password is salted and hashed with scrypt, never stored in plain, and never sent back to the page.

The login settings sit under the panel that publishes the board.

## Lend one session

Publishing the board hands over every card. **share this session** on a card lends one terminal to one person
instead.

It serves a restricted handler on its own address that answers for that terminal and refuses everything else,
shells included. It is an allowlist, not a filter, so an endpoint added to atrium later is invisible to a guest
until somebody adds it on purpose. **Read-only** is enforced on the socket, because a guest owns their copy of the
page.
