# Reports and backlog that every room can reach

Status: built. `atrium reports` and `atrium backlog` reach the hub's tables from every room (`internal/cli/backlog.go`).

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

The files in git stay the source of truth, and the hub's copy is a mirror that `atrium backlog import docs/backlog`
refreshes, until the orchestrator says otherwise. Both exist, and a file on one room is NOT what the other rooms see.

- A file under `docs/backlog/**` or `notes/director-reports.md` on one room reached no other room. Never assume it did.
- File a new item with `atrium_backlog file`, and leave a report with `atrium_reports add`. The hub row is what every
  room sees. Leave the id out to take the next one of the department.
- An item filed as a markdown file reaches the hub when somebody runs the import on the hub's machine. Until then a
  director on another room cannot see it. An import overwrites the hub row of an id the files hold, so an item that
  has a file is changed in the file.
- `QUEUE.md` order is not in the hub yet, and there is no per-director queue. Say what is next in a report.
- Launch a worker with the tag `item:<id>` and the item follows its card: in-progress at launch, then built, blocked
  or incomplete as the report says.
- Designs (`docs/<dept>/*-design.md`) and changelogs stay files, reviewed in git.

## Not done here

- Exporting the hub back to markdown. The files are the source of truth, so nothing needs it yet.
- Branch, verdict and landing commit on an item, as the spike asks. Status follows the card, nothing more.
- A queue per director. Decided not now.

