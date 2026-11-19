// Where the terminal strip files a card: the checkout's git remote, parsed to host/owner/repo, and nothing else.
// Runs the real `originParts` and `termPlaceOf` lifted out of the board.
const { boardScript } = require("./board-source.js");

let bad = 0;
function is(got, want, what) {
  if (JSON.stringify(got) !== JSON.stringify(want)) {
    console.error(`FAIL: ${what}: expected ${JSON.stringify(want)}, got ${JSON.stringify(got)}`);
    bad++;
  }
}

const page = boardScript();
function liftFn(name) {
  const at = page.indexOf(`function ${name}(`);
  if (at < 0) {
    console.error(`FAIL: the board has no ${name}. Point this test at whatever replaced it.`);
    process.exit(1);
  }
  let depth = 0, seen = false;
  for (let i = at; i < page.length; i++) {
    if (page[i] === "{") { depth++; seen = true; }
    else if (page[i] === "}") { depth--; if (seen && depth === 0) return page.slice(at, i + 1); }
  }
  console.error(`FAIL: ${name}'s braces do not close.`);
  process.exit(1);
}
const forgeAt = page.indexOf("const FORGE_HOST_LABELS =");
const src = [page.slice(forgeAt, page.indexOf("\n", forgeAt)),
  liftFn("forgeHostLabel"), liftFn("originParts"), liftFn("termPlaceOf")].join("\n");
const { originParts, termPlaceOf } = new Function(src + "\nreturn { originParts, termPlaceOf };")();

const parts = (u) => { const o = originParts(u); return o && [o.host, o.org, o.repo]; };
is(parts("https://github.com/dovholuknf/atrium.git"), ["github", "dovholuknf", "atrium"], "https with .git");
is(parts("https://github.com/dovholuknf/atrium/"), ["github", "dovholuknf", "atrium"], "https with a trailing slash");
is(parts("git@github.com:openziti/ziti.git"), ["github", "openziti", "ziti"], "scp-like ssh");
is(parts("ssh://git@GitLab.com:22/grp/sub/proj.git"), ["gitlab", "grp/sub", "proj"], "ssh:// with a nested owner");
is(parts("https://git.example.org/team/tool"), ["git.example.org", "team", "tool"], "any other host");
is(parts("http://127.0.0.1:7777/git/hub/github/dovholuknf/atrium.git"), ["github", "dovholuknf", "atrium"],
  "a hub remote names the forge after git/hub");
is(parts("C:/git/local/repo.git"), ["", "", "repo"], "a Windows path is local, not a host called C");
is(parts("C:\\git\\local\\repo"), ["", "", "repo"], "with backslashes too");
is(parts("/srv/git/thing.git"), ["", "", "thing"], "a unix path");
is(parts("file:///srv/git/thing.git"), ["", "", "thing"], "a file URL");
is(originParts(""), null, "no remote");
is(originParts(undefined), null, "no field");

is(termPlaceOf({ origin: "git@github.com:openziti/ziti.git", worktree: "D:/git/openziti/ziti" }),
  ["github", "openziti", "ziti"], "a checkout files under its remote");
is(termPlaceOf({ origin: "https://github.com/openziti/ziti", worktree: "G:/x/ziti-worktrees/feat" }),
  ["github", "openziti", "ziti"], "a flat worktree of that repo files under the same heading");
is(termPlaceOf({ origin: "ssh://git@gitlab.com/grp/sub/proj" }), ["gitlab", "grp", "sub", "proj"],
  "a nested owner is one heading per level");
is(termPlaceOf({ worktree: "C:/Users/me", repo: "me" }), [], "no remote stays ungrouped, home dir or not");
is(termPlaceOf({ worktree: "E:/work/client-site", host: "github", org: "o", repo: "client-site" }), [],
  "card fields are not git metadata and do not group");
is(termPlaceOf({ origin: "/srv/git/thing.git" }), ["thing"], "a local remote is its repo alone");

if (bad) {
  console.error(`${bad} assertion(s) failed.`);
  process.exit(1);
}
console.log("the strip files a card under its git remote.");
