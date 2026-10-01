# Hub documents: a place to share what agents and clint write

Written by @rnd on 2026-09-30, at clint's request through the orchestrator. Design only. Nothing here is built.

## The answer

- **Yes, and stage 1 is small:** publish, upload, view, versions and a stable link. No attaching as context, no
  search beyond the title, and no HTML rendering in stage 1.
- **The first user is already waiting.** `notes\` in the main checkout holds the usage report, the factory evals
  and the morning reports. Those files are untracked, live only on sg4's disk, and cannot be read from the phone
  except through a card's files endpoint. Each would be one `atrium_publish` call away from `/d/<slug>` on the
  phone.
- **Two things, kept apart.** An OUTPUT is a document a card wrote, which you read. An INPUT is a document handed
  to a card as context, which a model reads. Every comparable tool keeps them apart, for example Claude's artifacts
  and its project knowledge. Stage 1 builds the output. The input is stage 2, because it is where prompt injection
  enters.
- **The security line does not move.** A document is exactly as readable as the board: over loopback, and over the
  zrok share behind its password. There is no public link, and nothing goes to a third party.

## What is there today

- **A card's files** are readable through `GET /v1/tasks/{id}/files` on that card's room, bounded by
  `internal/safepath`. They are always sent as an attachment, as octet-stream with nosniff. When the card is culled
  and its worktree removed, the files are gone.
- **The `/m` viewer** (`m/js/viewer.js`) and the safe markdown in `m/js/md.js` render a card's text and images on
  the phone. Escaping and the raster-only image rule were reviewed in ab758324 and 94774247.
- **The hub's store** (`internal/hubstore`) has settings, rooms, the notify and growl tables, and snapshots every
  ten minutes, kept in tiers (`backup.go`). It holds nothing written by a card.
- **The launch brief** (`atrium_launch brief`) is text carried in the launch call and written to BRIEF.md in the
  new session's directory on the room. It is the nearest thing to "context attached to a card" today.

## What a hub document is

- **A title, a slug, and a list of versions.** The slug is made from the title when the document is created, and
  it never changes. A later rename changes only the title, so a link never breaks.
- **One version is the bytes at one moment,** with who wrote them, when, a size, a SHA-256, and a type.
- **Types in stage 1:**
  - markdown and plain text: rendered by `md.js`
  - raster images (png, jpeg, gif and webp): shown by `viewer.js`
  - a diff (`.diff` or `.patch`): shown as text with added and removed lines coloured
  - anything else: stored, and offered as a download
- **HTML and SVG are stored as text, never rendered.** Rendering them inline on the board's origin would run
  script with access to every card. A sandboxed iframe could do it safely later, and that is named as stage 3.

## Who writes one

- **An agent, with a new tool `atrium_publish`.** It takes `title`, plus either `content` (text) or `path` (a file
  inside the card's own directory, resolved through `safepath`), and an optional `slug` to add a version to a
  document that already exists. The answer is the URL `/d/<slug>` and the version number.
- **clint, by upload.** The board and `/m` get an "upload a document" control. A screenshot from the phone is the
  obvious case.
- **A new version of someone else's document is allowed.** Every version records who wrote it, so nothing is lost,
  and the history shows it.
- **Publishing never wakes anybody.** It is not a message. The card's row shows "published N documents", and the
  orchestrator's held notices get one line.

## Stable URLs, versions and history

- `/d/<slug>` is the newest version. `/d/<slug>@<n>` is version n, and it never changes.
- **The page is the `/m` shell.** It fetches `GET /_hub/docs/<slug>` (metadata and versions) and
  `GET /_hub/docs/<slug>/raw?v=<n>` (the bytes, sent as an attachment and octet-stream with nosniff, the same rule
  as the files endpoint). Nothing is ever served inline as HTML.
- **The history** lists each version with its author, its time and its size. For text, it can compare any two
  versions as a diff, rendered on the client from the two raw versions.
- **Delete is a tombstone.** The document leaves the list and its URL answers "deleted on <date> by <who>", and
  the gear can restore it. Purging the bytes is a separate step, and only the operator can do it.

## Rendering, and links from cards and replies

- **The phone and the board share one renderer:** `md.js` for text and `viewer.js` for images and downloads. No
  second markdown path.
- **Links between documents and cards:**
  - A `/d/<slug>` link in a reply, a report or a recap is a relative link. `md.js` already renders those, and it
    opens in the app.
  - A card's view shows the documents it published.
  - A document's view shows the card that wrote each version, linked by its card URL
    (`docs/rnd/card-urls-design.md`). The card may since have been culled, and then the name stays as plain text.
- **The board** gets a "documents" list in the menu, newest first, with a title filter.

## Attaching documents as context (stage 2)

- **The rule:** a document attached to a card or a launch is delivered as a file, and never as prompt text.
  - It is written to `<worktree>/.atrium/docs/<slug>.md` on the room, and `.atrium/` is added to
    `.git/info/exclude` when it is written. The 09-29 evaluation found working notes that had leaked into a branch
    through a tracked file.
  - The launch prompt names the files and says who wrote each one: "written by card r-031, not by clint".
- **Why a file and not prompt text:** a document an agent published is text a model wrote. Typed into another
  card's prompt, it would read as clint speaking. As a named file with its author, it reads as material, and the
  card's model can weigh it as such. This is the same reason the peer bus frames a say as another agent speaking.
- **Who may attach:** clint, from the launch dialog or a card's menu. An agent may attach with `atrium_launch docs`
  only to a card it launches, and the launch row records which documents went in.
- **Bounds:** 1 MiB of attachments per launch, the same order as a brief. It is carried in the `/v1/launch` body,
  as the brief already is.

## Retention, size and search

- **Caps:**
  - a version of text: at most 5 MiB
  - a version of anything else: at most 20 MiB
  - the store in total: 2 GiB
  - per card: at most 30 publishes an hour
- **At a cap the answer is a refusal, with the reason.** Nothing is silently evicted, which is the same posture as
  a source or the notify command. The gear lists the largest documents, so clint can delete what he does not need.
- **No automatic expiry in stage 1.** A document outlives its card on purpose, because that is the point.
- **Storage:** the bytes are content-addressed files beside the hub's database (`docs/<sha256>`). The rows in a
  new hub migration hold the title, the slug, the versions and the authors. The bytes are not in SQLite, so the
  ten-minute snapshots stay small. A separate daily copy of the blob folder is kept next to the snapshots, and a
  version whose file is missing says "the bytes are missing" rather than failing the page.
- **Search:** stage 1 filters by title on the client. Stage 2 adds full-text search over text versions with
  SQLite FTS5, which modernc's build carries.

## The security line

- **Readable over the share: yes, exactly as the board is.** Anyone past the zrok password can already read every
  card's files and approve permissions, so documents add nothing to what that password opens.
- **There is no public link.** A link that skipped the share's password would make atrium decide who connects,
  which crosses `docs/overlays.md`. Lending one document to someone else is a cousin of "lend one agent", and is out
  of scope here.
- **What is operator-only** (`edge.LocalOperator`):
  - purging bytes
  - changing the caps
  - turning publishing off for every agent
  - restoring a document that was purged
- **What the share may do:**
  - read
  - upload
  - add a version
  - delete, which is a tombstone, so it can be undone
- **Secrets leaving a worktree.** `atrium_publish path` is the new way bytes leave a card, so it gets three checks:
  1. **`safepath` containment,** with the same symlink rules as the download endpoint.
  2. **A refusal by name:** `.env*`, `*.pem`, `*.key`, `id_rsa*`, `*.p12`, `*.pfx`, `.npmrc`, `.netrc`, any
     `credentials*`, anything under `.git/` or `.ssh/`, and atrium's own token files.
  3. **A refusal by content,** for the shapes that are unambiguous: a PEM private key block, `ghp_` and
     `github_pat_`, `AKIA` followed by 16 characters, `xox[bp]-`, a JWT with three segments, and the zrok token
     format.

  The refusal names the rule, and the operator can override it for one document. **This is a speed bump, not a
  guarantee.** It cannot see a secret in a screenshot, and the doc says so on the publish tool.
- **The overlay rule holds.** Atrium holds no one else's credential, documents go to no third party, and the
  share is the only way in from elsewhere.
- **Rendering cannot run script.** `md.js` escapes, as reviewed, and raw bytes are always an attachment.

## What others do

These were read from product documentation up to mid-2026 and were not checked again today.

- **Claude artifacts:** an output in the conversation, with versions per edit, and an optional public link.
  **Claude Projects:** files attached as knowledge to every chat in the project. The two are separate features.
- **ChatGPT canvas:** a document beside the chat that both edit, with a version history and restore. Projects
  hold files as context.
- **Cursor:** rules files in the repository, indexed documentation sites, and `@` references. Context is attached
  by reference, not pasted.
- **Devin:** "knowledge" entries recalled when they are relevant, playbooks as reusable instructions, and a report
  per session.
- **Notion:** pages with a version history, sharing per page, and optional public links.
- **GitHub gists:** versioned through git, secret (unlisted) or public, with a stable URL.

**The pattern:** a stable link, a version history, and a split between what is produced and what is fed back in.
Public links are common, and atrium leaves them out on purpose.

## Benefits and pitfalls

**Benefits:**

- Reports and screenshots survive a card's cull.
- One link works on the phone and on the desktop.
- `notes\` becomes readable from anywhere the board is.
- Stage 2 gives a project knowledge with an author on every page.

**Pitfalls:**

- A second store of files that has to be backed up and kept under its caps.
- A publish tool is one more way out of a worktree, which is why it has the checks.
- Attached context is a prompt-injection path. That is why it waits for stage 2 and is delivered as a file.
- Without search, a long list goes stale. That is why the title filter is there from day one.

## Stages

**D1, the store and the API.**

- Owner: @fabric, with @runtime reviewing the migration.
- Size: one worker.
- Contents: the tables in a new hub migration at the end of the slice, the content-addressed blobs, the caps and
  per-card rate, `atrium_publish` in the hub's control tools, `GET` and `POST /_hub/docs`, raw bytes, tombstone and
  restore, the LP1 split, and the three secret checks.
- Acceptance:
  - A publish from a path outside the card is 403, the same answer as the download endpoint.
  - `.env` and a PEM key block are refused with the rule named.
  - A sixth MiB of text is refused.
  - A purge with `X-Forwarded-For` set is 403.
  - Versions keep their bytes after a new version is written.

**D2, the views.**

- Owner: @ui.
- Size: one worker.
- Contents: `/d/<slug>` and `/d/<slug>@<n>` on the `/m` shell, the documents list with its title filter on the
  phone and on the board, upload, history with a text diff, and "published N documents" on a card.
- Acceptance: the headless suite against mocked endpoints. By hand: a screenshot uploaded from the phone shows on
  the desktop, and a `/d/` link in a reply opens in the app.

**D3, stage 2: attach as context and full-text search.**

- Owner: @runtime for delivery on the room, @fabric for search.
- Contents: files under `.atrium/docs/` with `.git/info/exclude` and the author named, and FTS5.

**D4, stage 3: HTML and SVG rendered in a sandboxed iframe.** Only if asked for.

D1 and D2 go through @review: D1 adds a way for bytes to leave a worktree, and D2 renders text that agents wrote.

## Questions for later

These are parked in `notes/rnd-queue-2026-09-30.md`:

1. Should the orchestrator publish `notes\` reports as documents by default once D1 lands? Recommended: yes, the
   morning report and the usage report.
2. Is 2 GiB the right total cap?
