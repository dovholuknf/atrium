# Reports and backlog that every room can reach

Item `f-new-reports-channel-any-room`, owned by @fabric. Builds on the hub store and on the
`rnd-new-backlog-in-atrium` note, which stays a spike for the full design.

## The place

The hub holds two tables (migration `0011_backlog_reports` in `internal/hubstore/schema.go`):

- `backlog_item`: id (given by the filer, never reused), dept, title, body, priority, status, who filed it and from
  which room, created and changed times. Status is `open`, `held`, `in-progress`, `done` or `dropped`.
- `director_report`: `rp_<n>`, the department it is for (empty means the orchestrator), who and which room, subject,
  body, time, and when and by whom it was read. A report is append-only.

They are reached through the hub, not a checkout:

| Route | Tool | Does |
| --- | --- | --- |
| `GET /_hub/backlog` | `atrium_backlog list` | items, no bodies, filter by dept, status or open |
| `GET /_hub/backlog/<id>` | `atrium_backlog get` | one item with its body |
| `POST /_hub/backlog` | `atrium_backlog file` | file one. A taken id answers 409 with the item that holds it |
| `POST /_hub/backlog/<id>` | `atrium_backlog status` | change the status |
| `GET /_hub/reports` | `atrium_reports list` | newest first, filter by `to` or `unread` |
| `POST /_hub/reports` | `atrium_reports add` | leave a report |
| `POST /_hub/reports/<id>` | `atrium_reports read` | mark it read, the first reader is kept |

Reads are open to the board. Writes need the machine the hub runs on, as the deps routes do, and carry `by` and
`room` from the caller's session. The `backlog` and `report` events go to every board. Both tools are full class, so
a worker does not see them, and a worker keeps reporting to its launcher with `atrium_report`.

A director on m1mini lists what was filed on sg4 with `atrium_backlog list {open: true}` and files one that sg4 sees
with `atrium_backlog file`. Nothing is committed and no file is shared.

## The interim rule

Until the markdown files are imported and retired, both exist, and the files are NOT the shared copy.

- A file under `docs/backlog/**` or `notes/director-reports.md` on one room reached no other room. Never assume it did.
- File a new item with `atrium_backlog file`, and leave a report with `atrium_reports add`. The hub row is what every
  room sees.
- An item already filed as a markdown file in a checkout stays there until it is imported, and a director on another
  room cannot see it. When you rely on one, file a row that names it.
- `QUEUE.md` order is not in the hub yet. Say what is next in a report.
- Designs (`docs/<dept>/*-design.md`) and changelogs stay files, reviewed in git.

## Not done here

- Importing `docs/backlog/**` and the `QUEUE.md` files into the table, and exporting back to markdown.
- Status that follows the card, branch, verdict and landing commit, as the spike asks.
- A queue per director, a board view, and an `atrium backlog` command line. The tools and routes are the whole
  interface so far.
- Ids given by atrium. The filer still makes up the id, as the files do.
