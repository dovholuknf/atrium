# Moving files, and pasting into a session

Reaching a board from another machine makes this obvious. Once atrium is not on the machine you are sitting at,
getting a file to or from a session is a second tool, and the second tool is the one that does not work over the
overlay.

Nothing here is built. This document decides the shape and, more importantly, decides what has to exist before
any of it can be built safely, because the assumption everyone reaches for first turns out to be false.

## The assumption that is wrong

**Everything in this section is written in the present tense and describes the state at design time.** Both
halves of it have since been answered: `internal/safepath` is the primitive it says has to exist, and
`internal/api/browseroots.go` bounds `browse.go` through that primitive, so the share exposure described below
is closed. It is kept as written because the reasoning is what the answer was built from, and because the
correction it makes to `docs/charon.md` still stands.

`docs/charon.md` says, of an upload endpoint:

> `browse.go` already decides what a safe path is, so the landing spot should be derived from `task.worktree`
> through that same answer rather than from a second idea of safety.

That premise does not hold. `internal/api/browse.go` has no idea of safety to reuse.

Read it and the whole of its treatment of caller input is one line:

```go
dir := filepath.Clean(raw)
```

`filepath.Clean` resolves `..` lexically, but there is nothing to resolve it against. There is no root, no
allow list, no containment, and no symlink resolution. `?path=C:/Users` is honored. `?path=/etc` is honored.
Every directory the daemon's own user can read is listable, and the handler consults no card and takes no task
id, so it has no notion of which session is asking or what that session's directory is.

That is a deliberate posture and it is the same one `CLAUDE.md` states under "Out of scope: Authentication":
loopback, one machine, one user, one trust boundary. It is defensible for a read-only directory picker on
`127.0.0.1`.

**It stops being defensible the moment it is reused for writing, and it is already thinner than it looks for
reading.** Atrium now drives an overlay. `docs/overlays.md` is about publishing this board somewhere else, and
`docs/backlog.md` records under "A share widens what loopback means" that a tunneler terminates on this machine,
so every request arrives as `127.0.0.1`. A share therefore publishes an unauthenticated recursive directory
listing of the whole machine. Nothing reads file contents through it, so this is an enumeration exposure and not
a disclosure of data, which is why it is written here as something to know rather than something to panic about.
But it is the reason the answer below is not "reuse `browse.go`".

**So the first thing this feature needs is the containment primitive that does not exist yet.** Everything else
is easy and none of it is safe without that.

## What has to exist first: containment

One function, in one place, used by everything that resolves a caller-supplied path.

```go
// Contained reports the absolute, symlink-resolved form of path, and whether
// it is inside root.
//
// Both sides are resolved before comparison, because a symlink inside root
// that points outside it is the whole attack and a lexical check cannot see
// it. A path that does not exist is resolved as far as it does exist and the
// remainder is appended, so a write to a file not yet created is still
// checked against the directory that will hold it.
func Contained(root, path string) (string, bool, error)
```

Three rules it has to follow, each of which is a bug if it is missed.

- **Resolve symlinks on both sides.** `filepath.EvalSymlinks`. A lexical prefix test on unresolved paths is the
  classic hole: `worktree/link` where `link` is a junction to `C:/` passes every string comparison there is.
- **Compare on a separator boundary.** `worktree-evil` must not be inside `worktree`. Compare
  `resolved + separator` against `root + separator`.
- **Case fold on Windows and not elsewhere.** `D:/Work` and `d:/work` are the same directory here and are two
  directories on Linux. Getting this backwards fails open on the platform this runs on.

Non-existent paths are the case that gets missed. An upload names a file that is not there yet, so
`EvalSymlinks` fails on the full path. Walk up to the longest existing ancestor, resolve that, and re-join the
remainder. If the ancestor resolves outside the root, refuse.

`browse.go` was retrofitted onto this later, as predicted here and for the reason given: narrowing a picker that
people already use into something that refuses paths is a separate argument, and it got its own change rather
than riding along with this one. `internal/api/browseroots.go` holds it to a root set.

## Upload: bytes in

**`POST /v1/tasks/{id}/files`**, multipart, one or more files.

**The destination is not a parameter.** The caller says which card, and nothing else. Atrium computes:

```
<task.worktree>/.atrium/incoming/<yyyymmdd-hhmmss>-<sanitized name>
```

This is the whole security argument for the first version. A caller-supplied destination needs `Contained` to be
right; a computed destination needs `Contained` to be right *as a second check* and is already correct if it is
wrong, because there is no caller input in the path at all. The sanitized name keeps the extension, which is
what the model needs to know what it is looking at, and drops every separator, every `..`, every drive letter and
every control character.

`.atrium/incoming` rather than the worktree root, for three reasons that each matter on their own: a repository
does not want a screenshot in its root turning up in `git status`, a directory of uploads is something a human
can clear out without guessing which files were theirs, and a single `.gitignore` line covers it forever.

Bounds, all of which are refusals rather than truncations:

- **32 MB per file, 64 MB per request.** `http.MaxBytesReader`, so the limit is enforced while reading rather
  than after. A screenshot is under a megabyte and a heap dump is not a thing to paste into a chat.
- **The card must have a worktree that exists.** An `offered` card with a `suggested_cwd` nobody has made yet
  has nowhere to put a file, and saying so is better than creating the directory.
- **The card must not be archived.** Uploading into the working directory of a session that ended is almost
  always a mistake about which card is which.

The response is the list of paths written, in the daemon's own forward-slash form, which is what every path in
atrium already looks like: `CLAUDE.md` makes it a convention, `store.Task.Worktree` holds one, and `browse.go`
returns them.

That form is deliberate and it is also the one to splice. On Windows, both PowerShell and claude accept
`D:/git/thing/x.png`, so a forward-slash path typed into the terminal works without anybody converting anything.
Paste splices back exactly the string the endpoint returned, so there is one path convention in play rather than
one for the API and another for the terminal.

## Download: bytes out

**`GET /v1/tasks/{id}/files?path=...`**

This one does take a caller-supplied path, so this is where `Contained` earns its place. The path is resolved
against `task.worktree` and refused if it lands outside. A directory is refused rather than zipped: a zip stream
is a second mechanism with its own bounds, and the case that actually comes up is one file.

Served with `Content-Disposition: attachment` and an explicit `Content-Type: application/octet-stream`, because
a board that serves an HTML file from the working directory inline is serving attacker-authored script on the
board's own origin, and the board holds the grouping expression, the settings and every card.

## Paste: the one with no workaround

This is the half worth having first. Claude Code accepts images. Over an overlay there is no way at all to get a
screenshot into a session, and there is no substitute: the file is in a clipboard on a machine the daemon cannot
see.

The gesture is a paste onto the terminal pane. Charon's lesson, which is worth taking whole, is that drop, paste
and picker are one pipeline and not three (`app/ClaudeSessionView.tsx:1259-1264`). Whatever gesture produced the
bytes, the bytes go to the upload endpoint, and what comes back is a path.

Then the path has to reach the runner, and here atrium and Charon diverge on something already decided.

Charon injects the path as though it were typed, because a Charon session is an SDK turn with no human at a
keyboard. Atrium has a real pseudo terminal that a human may be in the middle of a command in.
`docs/supervision-design.md` decided that input is not arbitrated, on the grounds that two browser tabs typing
into one terminal is two hands on one keyboard, and that is exactly why this must not send a newline.

So the rule: **a pasted file splices its path into the stream and never presses enter.**

`d.sup.get(taskID).Write([]byte(path + " "))` and nothing else. `handleMessage` in
`internal/daemon/messages.go` appends `\r` because a message is a complete instruction being sent. A pasted path
is a fragment of an instruction the human is still writing, and submitting it for them is the difference between
a helpful paste and a runner that starts working on half a sentence.

When atrium does not own the terminal there is nothing to type into, so the path goes into the card's message
box as text rather than being queued. Queuing would deliver "C:/x/y/z.png" to the model on its next tool call
with no sentence around it.

### Where the file lands, and what gets said about it

Two settings, both daemon-side because both are about the machine atrium is on.

`paste_keep` is `keep` or `scrap`. `keep` is the original behaviour: `.atrium/incoming` inside the card, where
the file stays until somebody clears the folder out. `scrap` is a directory beside the database, emptied on every
daemon start. The path still reaches the runner and the file still works for as long as the conversation does.
The complaint that produced it is fair and specific: a repository accumulating a folder of screenshots looked at
once is a cost paid forever for a gesture that took a second.

The emptying happens on the way UP. A daemon that was killed does not run cleanup on the way down, and the
promise wanted from this setting is that the pictures are gone, not that they were deleted tidily.

Containment still applies in both modes, against the directory that was chosen rather than against the card. The
scrap directory is not inside a card, and checking it against one would refuse the only place it was ever
supposed to write.

`paste_preamble` is what gets typed in front of the path, defaulting to `check out the image here: `. A bare path
is what a person types when they mean "look at this" and it is not what they say. Nothing is submitted: the
preamble and the path are spliced into the line the same way the path alone was, and the rule above still holds.

Stored empty means the default, because empty is also what a key that was never written reads as. Choosing to
have no preamble at all is stored as the word `none`, which the board writes and nobody types.

**There is no third option that hands the bytes to the model.** A pseudo terminal carries input characters. An
image is not one. Claude Code gets a pasted image by reading the clipboard ITSELF, natively, on its own machine,
which is precisely what a browser two machines away cannot do. The file has to exist somewhere; the only real
question is where, and for how long.

## Writing a file: the precondition, which is worth having on its own

There is no write endpoint today and there will be one, whether it arrives with an editor or before it. When it
does, it takes a precondition, and it takes it in the first commit rather than after the first lost edit.

The shape is HTTP `If-Match` and the reference implementation is Charon's `fs_write`
(`agent/charon_agent/fsnav.py:503-521`), whose docstring says it is "a precondition, not a hint". A mismatch
returns the file's real hash and does not write. There is no lock and nothing arbitrates who owns a file. The
daemon simply refuses to clobber silently.

One correction to that reference, which `docs/charon.md` already makes and which is the only interesting design
decision in this section. Charon spells force-overwrite as `expected_sha256=None`, which makes "I did not think
about this" and "clobber it deliberately" the same value.

**Make them different.** Three states, not two:

| `expected_sha256` | Means |
| --- | --- |
| absent | `400`. Not consent. A client that has not thought about concurrency is a client that will lose an edit. |
| `""` | This file must not exist yet. Creating. |
| a hash | This file must currently hash to this. |
| `"*"` | Clobber whatever is there. Deliberate, typed, and in the audit log. |

Write through a temporary file in the same directory, `fsync`, then `os.Replace`, preserving the existing mode.
A half-written file in a directory an agent is building in is worse than no write at all.

## What is refused

- **A general file server.** The endpoints are per card and rooted at that card's worktree. There is no way to
  ask atrium for a path that is not below a card. `browse.go` used to be that thing accidentally, by listing any
  directory it was given; it is now bounded to a root set and still lists directories only, never contents.
- **Directory upload and zip streams.** One file, or several files, into one computed directory. Recursive
  transfer has its own bounds, its own traversal problems and its own partial-failure story, and the case that
  comes up is a screenshot.
- **Serving anything inline.** Everything downloaded is an attachment. See above.
- **Reading the clipboard from the daemon side.** The clipboard is on the machine with the browser. That is the
  whole reason this feature exists.
- **A vendored editor component.** The concurrency model came first as planned, and the precondition above is
  that model. A plain textarea sits on top of it and does the job. CodeMirror 6 was then evaluated and refused:
  it ships ES modules with bare specifiers across seven packages and no prebuilt bundle, so vendoring it means
  adding a bundler to a repo whose board is one file on purpose. See `docs/backlog.md`.

## Order to build it

1. `Contained`, with tests for the symlink case, the sibling-prefix case, the case-folding case, and the
   not-yet-existing-path case. Nothing else can be trusted until this is right.
2. Upload with a computed destination. No caller path, so this ships behind one primitive rather than two.
3. Paste on the terminal pane, splicing without a newline. This is the payoff.
4. Download, which is the first caller-supplied path and therefore the first real use of `Contained`.
5. The write precondition, whenever a write endpoint appears.

Steps 1 to 3 are the feature. Steps 4 and 5 are the ones that can wait and should not be skipped.

**All five shipped**, and so did two things this list did not anticipate: reading and writing text through
`internal/api/filetext.go` behind the precondition from step 5, and `browse.go` bounded through the same
primitive.

## What got built, and what the tests turned up

Steps 1 to 4 are done. `internal/safepath` is the primitive, `POST /v1/tasks/{id}/files` is the upload,
`GET /v1/tasks/{id}/files?path=` is the download, and paste and drop on the terminal pane share one pipeline.

Two things came out of writing the tests that are worth keeping written down.

**The right-to-left override is not a control character.** `SafeName` stripped everything below `0x20` and
U+202E sailed through, which is the oldest trick there is for making `photo<RLO>gpj.exe` render as
`photo exe.jpg`. The fix is to strip the whole `Cf` category, not a list of the ones somebody thought of.
Nothing legitimate needs a format character in a filename.

**The two failure modes of download have to answer identically.** "Outside the card" and "does not exist" are
both `403`, because two different answers make the endpoint an oracle for what is on the machine outside the
card. That is a small thing that is easy to get wrong by writing the obviously more helpful error.

**Built.** `internal/api/filetext.go` reads and writes a text file at a caller-named path, and every write
carries an expected-content hash. A mismatch answers `409` with the current content and the current hash, so the
two versions are handed back and nothing arbitrates who owns the file. An ABSENT hash is an error rather than
consent, which was the one correction this design made to Charon's version.
