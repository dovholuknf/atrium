# u-new-gate-on-danger. Four skins set --on-danger and the others do not

Status: OPEN. Filed 2026-10-05 from the release 0.0.1 gate run. For @ui.

## The failing check

`bash scripts/check-skins.sh` (the skins step of `scripts/ci.sh`) prints, against the reference skin abyss (30
variables):

- `daylight sets what no other skin sets: --on-danger`
- `frost sets what no other skin sets: --on-danger`
- `linen sets what no other skin sets: --on-danger`
- `paper sets what no other skin sets: --on-danger`

## Done looks like

Either every skin sets `--on-danger`, abyss included, or the four stop setting it. `check-skins.sh` exits 0. The
release gate needs it.
