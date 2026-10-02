# Serving a board on an overlay without holding an identity

Series: Overlays and zrok. Status: idea. Audience: OpenZiti and zrok users.

**Hook.** atrium serves its board straight on a zrok share or an OpenZiti service, and a public share is refused without a login.

**Angle.** Drive the overlay; do not become one.

**Rests on:** native overlay listeners, published-board login, room identity inside the overlay. See `docs/blog/inventory.md`.

## Story beats

1. The SDK listener, nothing proxied.
2. Public zrok share only with OIDC or a password.
3. Rooms proven by a hub-signed certificate inside the overlay.
4. Three trial 'expose the board' screens side by side, judged and one kept.
5. Lending one session read-only at a readable address.

## Screenshots and demos

- the three trial screens
- the share settings

## Sources

- docs/fabric/overlays.md
- docs/fabric/ziti-zrok-flow-design.md
- docs/rnd/overlay-room-identity.md
- changelog/fabric/2026-09-30-f-022.md

## Notes for the writer

- The repo is public: paraphrase clint, never quote him. Name no outside person. Keep security findings out of the post.
- Screenshots come from a test room, or are drawn mockups, as the website does. Never use the live board with real cards.
