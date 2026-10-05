# m-new-release-signing. Sign the release installers and checksums

Status: BACKLOG. Filed 2026-10-05 from the release 0.0.1 decision by @fabric.

## What is missing

Release 0.0.1 ships unsigned. The Windows MSI and the macOS pkg carry no signature, and `checksums.txt` is published
without a detached signature.

## Why it is needed

Windows SmartScreen and macOS Gatekeeper warn on unsigned installers, and a user has no way to check that the assets
came from us beyond the checksums served beside them.

## Done looks like

- A decision on the certificates: a Windows code-signing cert and an Apple Developer ID, and who holds them.
- `scripts/cut-release.sh` and `release.yml` sign the MSI and the pkg, and notarize the pkg.
- `checksums.txt` has a detached signature published with it, and `packaging.md` says how to verify it.
- The release notes no longer say the installers are unsigned.

## Depends on it

- release 0.0.1 published, `m-new-release-0-0-1`
