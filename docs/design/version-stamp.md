# Version stamp

Every room and the hub report `dev` today. This gives every build a real version and a commit hash.

## The base number

A `VERSION` file at the repo root holds one line, `0.0.3`, no `v`. It is the number of the NEXT release. Cutting a
release tags `v0.0.3` and the same commit set (or the next one) moves `VERSION` to `0.0.4`. `scripts/cut-release.sh`
refuses a tag whose number differs from `VERSION` at that commit, so the file and the tag cannot drift.

## What a build reports

| Build | Version | Example |
| --- | --- | --- |
| tag build, clean tree | the tag, without `v` | `0.0.3` |
| branch build, clean tree | `<base>-dev+<shorthash>` | `0.0.3-dev+6c943f4` |
| branch build, tracked files changed | `<base>-dev+<shorthash>.modified` | `0.0.3-dev+6c943f4.modified` |
| `go build` with no flags | same as a branch build, from Go's embedded `vcs.revision` and `vcs.modified` | |
| no git and no flags | `<base>-dev` with no hash | `0.0.3-dev` |

`<shorthash>` is 7 characters. The full 40-character commit stays in `Commit` and keeps going to the hub, because the
deploy queue compares it with the branch. `Version` is what a person and a package manager read.

SemVer ordering holds: `0.0.2 < 0.0.3-dev+x < 0.0.3`. A pre-release sorts below its release and build metadata is
ignored in comparison, so a dev build never sorts above the 0.0.3 release. Packagers that do not speak SemVer map the
dash: deb and rpm use `0.0.3~dev+<shorthash>` (tilde sorts below empty), and the MSI keeps its existing numeric rule in
`package-windows.ps1`, which never carries the suffix. Dev builds are not packaged, so this only matters if someone does.

## Where it is computed

One place decides, so the paths cannot disagree. A new `scripts/version.sh` prints `version commit tree` for the
current checkout using the table above, and every shell path calls it. PowerShell paths run it through `bash`, which
every build machine here has, and fall back to the same rule written inline only if bash is missing. The Go side stays
as it is: `cli.Version`, `cli.Commit` and `cli.Tree` are still set by `-ldflags`.

`internal/cli/version.go` changes in one spot. The default `Version` becomes empty instead of `dev`, and
`versionInfoOf` fills an empty `Version` from the `VERSION` file embedded with `//go:embed`, plus `-dev+<shorthash>`
built from the commit it already resolves, plus `.modified` from the dirty flag. That is the no-flags fallback. Because
it is derived from `vcs.revision` it needs no stamping, and a stamped build is untouched.

## `atrium version`

```
atrium   0.0.3-dev+6c943f4.modified
commit   6c943f4be3a1... (modified)
board    ...
platform windows/amd64
go       1.26.10
```

`--short` prints the version only. `VersionLine` gives `atrium 0.0.3-dev+6c943f4.modified (6c943f4be3a1, modified)`,
so the hash shows in logs even though it is in the version too. A tag build prints `atrium 0.0.3` and `commit` as now.

## Hub, board and `atrium_resources`

Rooms already send a version string at check-in (`atrium_inventory.go`) and the hub reports its own. These now carry
the stamped version, so `dev` disappears with no protocol change. Showing the hash means adding the short commit next
to it: the check-in already sends the full commit (`api.RunningCommit`), so the hub shows `Version` plus the first 7
characters of that commit for each room, and the same for itself. The board shows `0.0.3-dev+6c943f4` as one string.
`atrium_resources` lists `version` and `commit` as two fields, so a tag build still shows its hash. A room on an old
binary that sends `dev` and no commit shows `dev` as now.

## Every build path

| Path | Change |
| --- | --- |
| `Makefile` | `VERSION`, `COMMIT`, `TREE` come from `scripts/version.sh`. The comment about not claiming a number stays true |
| `scripts/release.sh`, `package-linux.sh`, `package-macos.sh` | replace `git describe ... or dev` with `scripts/version.sh` |
| `scripts/cut-release.sh` | keeps `--exact-match`, adds the `VERSION` file check, and still verifies the binary reports the tagged commit |
| `scripts/package-windows.ps1` | tag build as now, otherwise the `VERSION` base for the MSI number, stamped through ldflags |
| `scripts/live/atrium-deploy.ps1`, `scripts/live/build-deploy.ps1`, `scripts/provision-room.ps1` | the same `version.sh` call in place of `describe ... $null` |
| `.github/workflows/release.yml` | unchanged, it goes through `cut-release.sh` |
| plain `go build` / `go install` | the embedded fallback above |

Every one of them keeps stamping `Commit` as the full hash and `Tree` as `clean` or `modified` over tracked files only,
for the reason in `version.go`.

## Tests

Unit tests on `versionInfoOf` for the table rows (tag stamp, empty stamp with a revision, dirty, no build info), and a
shell test that `scripts/version.sh` prints a tag on an exact tag, `-dev+hash` off one, and `.modified` after touching
a tracked file.

## Open questions

1. `VERSION` bumped by hand in the release commit, or by `cut-release.sh`? Proposed: by hand, checked by the script.
2. Is `+` and `.modified` acceptable in file names and the board? Proposed yes, nothing here puts the version in a
   path except packaging, which never sees a dev build.
