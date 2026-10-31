# Version stamp

Every room and the hub reported `dev`. Every build now carries a real version and a commit hash.

## The base number

`internal/cli/VERSION` holds one line, `0.0.3`, no `v`. It is the number of the NEXT release. It lives beside
`version.go` because `go:embed` cannot reach outside the package. `scripts/bump-version.sh` moves it (the next patch, or
a number given) and commits nothing. A release workflow calls that script, so no person edits the file.
`scripts/cut-release.sh` refuses a tag whose number differs from the file.

## What a build reports

| Build | Version |
| --- | --- |
| tag on HEAD | the tag without `v`: `0.0.3` |
| anything else | `<base>+<shorthash>`: `0.0.3+6c943f4` |
| `go build` with no flags | the same, from the embedded base and Go's `vcs.revision` |
| no git and no flags | `<base>` alone |

The short hash is 7 characters. A dirty tree does not change the version. It shows on the commit line only, as
`modified`, over tracked files. The full commit stays in `Commit` and keeps going to the hub. Dev builds are not
packaged, but the MSI keeps its numeric rule and takes the part before `+`.

SemVer note: `0.0.3+x` is EQUAL to `0.0.3` when build metadata is ignored, and a tool that sorts by it may rank it above.
That is the cost of the format Clint chose over `0.0.3-dev+x`.

## One rule

`scripts/version.sh [version|commit|tree]` is the rule. `scripts/version.ps1` is the same rule for PowerShell, because
`bash` on a Windows PATH may be the WSL stub. `ATRIUM_COMMIT` stands in for git in an export with no `.git`.
`cli.Version`, `cli.Commit` and `cli.Tree` are still set by `-ldflags`, and an empty `Version` is filled in
`versionInfoOf` as `<base>+<shorthash>`.

## `atrium version`

```
atrium   0.0.3+6c943f4
commit   6c943f4be3a1... (modified)
```

`--short` prints the version only. `VersionLine` is `atrium 0.0.3+6c943f4 (6c943f4be3a1, modified)`.

## Hub, board and `atrium_resources`

Rooms and the hub send the stamped version and the full commit already, so `dev` goes away with no protocol change.
`conn/upgrade.go` used `dev` to mean "never skip an upgrade on equal versions". A `+` version means the same, so
`isRelease` is false for both.

## Every build path

| Path | Change |
| --- | --- |
| `Makefile` | `VERSION` from `scripts/version.sh` |
| `scripts/release.sh`, `package-linux.sh`, `package-macos.sh` | the default version from `scripts/version.sh` |
| `scripts/package-windows.ps1` | the default from `version.ps1`, and `0.0.3+hash` maps to MSI `0.0.3` |
| `scripts/live/build-deploy.ps1`, `atrium-deploy.ps1`, `provision-room.ps1` | `version.ps1` |
| `scripts/cut-release.sh` | checks the tag against `VERSION`. `check-release.sh` pins it |
| plain `go build` | the embedded fallback |

The release workflow ends with `bash scripts/bump-version.sh --push main`, which moves VERSION on main to the next patch
and does nothing when main no longer holds the released number.