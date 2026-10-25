# Sharing one document from a card over a zrok public share

Status: stage 1 built (`3f421aa00`, `internal/daemon/docshare.go`, `docs/changes/f-share-doc-zrok-stage1.md`).
Stages 2 to 4 are design only.

Item `rnd-share-doc-zrok-public`. clint, 2026-10-21: "i want a spike on sharing a doc from atrium using zrok public
share". His example is a blog draft on the blog card:

```
http://127.0.0.1:7778/read.html#claude-sg4~01a136c1-3fcd-792b-a6fe-caa3a947a597/docusaurus%2Fblog%2Fstop-emailing-jwts-v6.md
```

Today he shares that by hand. The ask is that a card shares it easily: the human from the reader, or the agent
through a tool, with the URL back in chat ready to paste.

## The answer

- **Share a snapshot, not a window.** One markdown file and the images it draws are copied into a folder atrium owns,
  and a handler with a fixed list of paths serves that folder. The board, `/v1` and the rest of the card are not on
  the share at all. This is built.
- **The room the card is on runs the share**, with the embedded zrok SDK and the room's own zrok environment. The hub
  only forwards the click, the same way it forwards every `/v1/tasks/<id>/...` route. This is built.
- **A share expires** (7 days by default, 30 at most), survives a restart and the card going away, and is stopped from
  the reader. This is built.
- **The address is the credential.** `atrium-` plus 60 random bits, reserved so it survives a restart. No login in
  front of it, because the reader of a blog draft is somebody without an account anywhere.
- **What is missing for the item:** the agent cannot share (no tool), there is no list of what is shared now with a
  stop button on the board, and a share cannot be refreshed after an edit. Those are stages 2 and 3 below.
- **Smallest next stage:** a `share` verb for the agent that calls the same endpoint the reader calls, with the
  images found on the server. It is one route of new logic and no schema change.

## What gets shared

One rendered document, read-only, and never the board.

Publishing the board publicly is the whole machine: terminals, permission prompts, the file browser. AGENTS.md puts
an auth layer out of scope, and `docs/fabric/overlays.md` refuses a public board share without a login. A document
share therefore must not be `read.html` with the API behind it, or a filter over the board handler. It is a
separate `http.Server` on its own listener, the same shape as "lending one session" in `overlays.md`, with an
allowlist rather than a denylist.

What stage 1 serves, from `docShareHandler`:

| path | what |
| --- | --- |
| `/` | `share.html`, with the snapshot's path and image map filled in |
| `/doc.md` | the copied markdown, as `text/plain` |
| `/f/<n>` | the n-th copied image, with the type recorded at snapshot time |
| `/read.js`, `/vendor/marked.js`, `/vendor/purify.js` | the reader's renderer |
| `/css/tokens.css`, `/css/themes.css` | the reader's styles |

Everything else is 404, every method but GET and HEAD is 404, and every answer carries `nosniff`, a strict CSP,
`Referrer-Policy: no-referrer` and `no-store`. The page renders client side with the same marked and DOMPurify the
reader uses, so a shared page looks like the reader and no second renderer exists to drift.

## Snapshot versus live

**Recommendation: snapshot, plus an explicit refresh (stage 3). Never live.**

| | snapshot | live |
| --- | --- | --- |
| what the public listener reads | a folder under `<data dir>/docshares/<id>/` that atrium wrote | the card's worktree |
| a later edit | not shown until refreshed | shown at once, including a half-written paragraph or a pasted secret |
| secret check | once, at share time, on exactly the bytes served | on every request, or not at all |
| the card is culled | still answers | 404, the link dies with the worktree |
| path escapes | checked once with `safepath.Contained` | a symlink added later is a new path to check per request |

Live means the public internet reads the worktree, which an agent is writing to. An agent that writes a key into a
draft, or a symlink into its tree, publishes it before anyone looks. A snapshot puts a human decision (or an agent's
explicit call) between an edit and the internet. What live buys is "my reader sees the fix", and a refresh button
buys the same thing with that decision kept.

**Refresh** re-snapshots the same document into a new folder, swaps the handler behind the same listener, and
deletes the old folder. The name, the address and the expiry stay. It needs no new zrok call, because the listener
does not change, only the handler it serves.

## Who runs zrok

Three candidates were weighed.

1. **The card's room, embedded SDK. Chosen and built.** The room already has the worktree, the zrok environment
   (`~/.zrok2/environment.json`, enabled through the overlay panel) and the reserve and release code
   (`overlay_reserve.go`, `ReserveZrokName`, `ReleaseZrokName`). The snapshot never crosses the link. The cost: the
   share is up only while that room is up. A laptop room that sleeps takes its shares down until it wakes.
2. **The hub.** Survives a room sleeping, and the hub already holds documents (`atrium_publish`, `/d/<slug>`). It
   would mean copying the snapshot to the hub and giving the hub a zrok environment of its own. Kept as stage 4 for
   the case where the room is a laptop.
3. **`zrok share public --backend-mode web <folder>` as a child process.** Smallest code, and it serves a folder
   with a file listing and no allowlist, it outlives atrium unless supervised, and it reintroduces parsing a child's
   output for the URL, which `overlays.md` records moving away from. Rejected.

**The credential rule holds in all three.** Atrium never holds the zrok account token as data it shows or sends: the
room reads the environment the `zrok enable` it ran wrote, and the board is told only that one is present. The
share token zrok returns is a share identifier, not an account credential.

## Lifetime: expiry, revoke, the list, restarts

Built in stage 1:

- **Expiry.** `ttl_days` on the request, default 7, capped at 30. The reaper tick (`sweepDocShares`) ends a share
  past its expiry: unbind, release the name, delete the folder, delete the row.
- **Revoke from the reader.** "Shared" opens the box with the address, Copy and Stop. Stop is final: the name is
  released, and sharing again reserves a different one.
- **One share per document.** Sharing the same file of the same card again returns the existing share.
- **A cap.** 20 live per room, refused with a sentence naming the cap.
- **Restarts.** Shutdown unbinds and keeps the row and the name (`stopAllDocShares`). Startup puts every wanted,
  unexpired row back on the same name (`RestoreDocShares`) and ends the expired ones. `atrium preview` does neither,
  for the reason `overlays.md` gives for `Options.Passive`.
- **The card going away.** The share keeps answering. `GET /v1/shares` marks it `card_gone`.

Missing, and stage 3:

- **A list on the board of what is shared now.** `GET /v1/shares` already returns `docs` beside the lent sessions,
  and no view draws it. A culled card's share can only be stopped by its id over the API, which nobody will do.
- **Stop by id through the hub.** `DELETE /v1/docshares/{id}` is not a `/v1/tasks/<id>/...` route, so the hub's card
  routing cannot tell which room holds it. Either the list carries the room and the board calls the room's route,
  or the route becomes `DELETE /v1/tasks/{task}/docshares/{id}`, which routes by the card id even after the card is
  gone, as long as the room is named in the id.
- **Extend.** Pushing the expiry out without a new address. A one-field update on the row.

## Auth

**Stage 1 has none, by design, and the address is the credential.** The reader of a blog draft is a colleague or a
reviewer who has no zrok account and no atrium login, and a login in front of the link defeats "paste this". The
risk is bounded by what the share can serve: one snapshot the operator chose, behind a 60-bit unguessable name,
expiring in a week.

Options for a document that should not be open to whoever holds the link, none built:

- **zrok's own basic auth on the share** (`ShareRequest.BasicAuth`). zrok checks it at the frontend, so atrium serves
  nothing to a caller without it. The password would have to come from somewhere. If atrium generates and stores
  it, atrium holds a credential, which is the line the item draws. If the operator types it and atrium passes it
  through without storing it, a restart cannot rebind the share. That conflict is open question 3.
- **zrok's OAuth on the share** (`OauthProvider`, `OauthEmailAddressPatterns`). zrok runs the login, atrium holds
  only an email pattern, which is not a credential. Restricting to `*@netfoundry.io` is the likely real use.
- **atrium's OIDC guard** from the published board. Rejected for this: it would mean the doc share handler gaining a
  session cookie and a callback, which is the board's surface on a listener built to have none.

Recommendation: if a restricted share is wanted, use zrok's OAuth with an email pattern, as a per-share option.

## The surface

- **Reader: built.** A "Share publicly" button in `read.html` (`shareMain` in `read.js`). It sends the path and the
  images the page actually drew, so the snapshot holds exactly what the reader showed.
- **Agent: stage 2.** A `share` action, URL back in the tool result, so the agent pastes it into chat. See below.
- **Board list: stage 3.** A "Shared now" block in the overlay panel beside the lent sessions, with Copy, Stop and
  Refresh per row.

## The smallest first stage for the example

Stage 1 is the smallest stage that works for clint's example from the reader, and it is built: open the blog draft,
press "Share publicly", copy the `https://atrium-<12>.<zrok domain>/` address. Its live check needs a room with
`zrok enable` done and belongs to `claude-sg4`. It is listed in the stage 1 change note and has not been run.

What the example will hit, worth checking in that live run:

- **Docusaurus front matter.** The draft starts with a `---` YAML block. marked draws it as a rule and a paragraph.
  The reader shows the same thing, so this is consistent and ugly. Stripping a leading front matter block in
  `share.html` is a few lines (open question 5).
- **Docusaurus image paths.** An image written `/img/foo.png` is a site path, resolved against `docusaurus/static/`,
  not the file's folder. The reader cannot draw it, so it is not in the snapshot. Relative images are fine.
- **MDX and admonitions** (`:::note`) render as plain text. Acceptable for a draft review.

## Staged plan

1. **Reader share, snapshot, room runs zrok.** Built. Live check owed on sg4.
2. **Agent share.** A `share` verb on `atrium_files`, keeping one files tool rather than adding a tool:
   `{action: "share", path, ttl_days?}` answers `{address, expires_at}`, and `{action: "unshare", path}` stops it.
   The handler runs on the card's own room and calls `MakeDocShare` for the calling card only, never another card,
   so an agent can share only what is in its own tree. Images are found on the server: parse the markdown for
   relative image references (`![](x)` and `<img src>`), keep those `safepath.Contained` accepts and
   `ViewImageType` recognises, and pass them as `req.Images`. The tool description says plainly that the result is
   public to anyone with the link. No schema change.
3. **Board list, stop by id through the hub, refresh, extend.** As above under Lifetime.
4. **Hub-hosted share**, for a room that sleeps. `atrium_publish` already puts a document on the hub. A public share
   of one hub document version, with the same fixed handler, run by the hub's zrok environment. Only if a sleeping
   room turns out to matter.

Stage 2 is the smallest stage that closes the item's "the agent through a tool" half. Stage 3 closes "a list of what
is shared now".

## What is reused

- `docs/fabric/overlays.md`: "Lending one session" for the separate allowlisted handler, "Keeping an address" for
  reserve versus ephemeral names, "Orphans are atrium's own" for sweeping only atrium's own rows.
- `internal/daemon/overlay_reserve.go`: `ReserveZrokName` and `ReleaseZrokName`. `newShareName` in
  `overlay_guest.go` for the `atrium-` plus 12 characters.
- `internal/daemon/overlay_zrok.go` and `overlay_zrok_errors.go`: the environment, and turning zrok's refusals into a
  sentence (a 401 can be a share limit, a 409 with `limit reached` is a name limit).
- `internal/daemon/overlay_limits.go` and the account block: 20 doc shares each hold a reserved name, which counts
  against the account's name ceiling.
- `internal/base/docshare`: the secret file name and content checks the hub's `atrium_publish` uses too.

## Open questions for clint

1. Is snapshot plus a Refresh button right, or do you want live for drafts you are actively editing, accepting that
   an agent's next write is public at once?
2. For stage 2, a `share` verb on `atrium_files`, or a separate `atrium_share` tool that is easier to spot?
3. Restricted shares: is zrok OAuth with an email pattern (for example `*@netfoundry.io`) the case you want, or is an
   open link always enough for what you share?
4. Should an agent's share need your approval the way a permission prompt does, or is a speed-bump secret check
   plus the tool description enough?
5. Strip Docusaurus front matter on the shared page (and in the reader), or show the draft exactly as written?
6. Do 7 days default, 30 maximum and 20 per room match how you use these, given each share holds a reserved name on
   your zrok account?
7. Does a room that sleeps (a laptop) matter enough to build stage 4, the hub-hosted share?
