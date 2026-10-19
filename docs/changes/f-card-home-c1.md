Docs: none, the design is docs/runtime/card-home-design.md and the reference pages follow with stage C2

## Test plan

## @LETTER@. A card works in a home of its own

### @LETTER@1. Launch into a home

1. Set `card_root` to an empty folder of its own, then launch with `repo` set to a clone's absolute path and a title.
2. Look in the card root.

**Expected:** one folder named after the title with a six character tail, holding `work/` (a worktree on a new branch),
`tmp/` and `.atrium/`, with BRIEF.md at the top. In the runner, TMP, TEMP and TMPDIR are `<home>/tmp`.

### @LETTER@2. Refused launches and roots

1. Launch with both `cwd` and a `host/owner/repo` repo.
2. Set `card_root` to a drive root, to your home folder, or to a folder under git.scm_root.

**Expected:** the launch is refused with a sentence, and each root is refused.

### @LETTER@3. Reclaim keeps what is not safe to remove

1. Leave an uncommitted file in `work/`, then cull the card.
2. Commit it without landing it, then cull again.
3. Land the branch, then cull again.

**Expected:** the first two keep the home and the launcher gets one notice each reason. The third removes the whole home
and its worktree entry.
