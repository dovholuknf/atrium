# m-docs-hub-git-flow

- website/docs/git-flow.md: new page. Facts checked against internal/gitsync/lookup.go and the f-new-git-url,
  f-hub-git-store and f-hub-gets-run-git-on-request notes. The hook rules come from the brief, since the hook is not
  in the repository (the refusal text matches internal/guard/rules.json).
- website/sidebars.js: the page sits under "Beyond one machine", after rooms.
- website/docs/rooms.md and website/docs/how-it-works.md link to it.
- Judgment calls made unattended: the `git remote add` example uses the `/git/hub/<host>/<owner>/<repo>.git` form from
  the lookup notes, and the page does not claim a card's clone gets its remote automatically.

- Build: `scripts/build-docs.ps1` stops at its gate-script step here because `pwsh` is not installed. `npm run build`
  in website/ passed, and Docusaurus refuses broken links and anchors, so the links are checked.

## Test plan

## @LETTER@. The git flow page is on the site

### @LETTER@1. Build and read

1. Run `scripts/build-docs.ps1`.
2. Open the built site and find "How a card gets code and hands it back" in the sidebar under "Beyond one machine".
3. Follow the links to it from "Rooms and the hub" and "How it works".

**Expected:** the build passes with no broken link, and the page has the six sections in order.

