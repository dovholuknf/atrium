// A believable hub for the repos tab: five repos under three owners, so the tab can be designed and tested
// against something other than an empty store. The shape is what GET /_hub/git/repos answers. `tasks` is what
// lastTasks holds for the cards whose titles are known; the rest are deliberately unknown (a card that is gone).
//
//   node:     const { repos, tasks } = require("./hubrepos-fixture.js")(Date.now())
//   browser:  const { repos, tasks } = hubReposFixture(Date.now())   (when loaded as a classic script)
//
// Nothing here is pushed anywhere. Ages are relative to `now` so "just now" and "3 weeks" stay true.
(function () {
  const MIN = 60e3, HOUR = 36e5, DAY = 864e5;
  function make(now) {
    now = now || Date.now();
    const at = ms => new Date(now - ms).toISOString();
    const sha = seed => {
      let h = 2166136261 >>> 0, out = "";
      for (let i = 0; i < 40; i++) { h = Math.imul(h ^ (seed.charCodeAt(i % seed.length) + i), 16777619) >>> 0; out += (h >>> 28).toString(16); }
      return out;
    };
    const b = (name, room, card, age, released) => ({ name, sha: sha(name + room), room, card, at: at(age), released: !!released });
    const repo = (owner, name, main, branches, host) => ({
      host: host || "github", owner, repo: name,
      url: "git@hub.atrium:" + owner + "/" + name + ".git",
      path: "/git/hub/" + (host || "github") + "/" + owner + "/" + name + ".git",
      main: main ? { sha: sha(owner + name + "main"), at: at(main) } : { sha: "", at: null },
      branches: branches || []
    });
    const repos = [
      // a dozen branches, one of them absurdly long, the newest "just now"
      repo("openziti", "ziti", 2 * HOUR, [
        b("fix/router-link-flap", "sg4", "c-101", 20e3),
        b("feat/edge-router-policies-v2", "m1mini", "c-102", 9 * MIN),
        b("fix/ctrl-raft-snapshot", "sg4", "c-103", 41 * MIN),
        b("docs/quickstart-refresh", "sg3", "c-104", 2 * HOUR),
        b("feat/posture-check-os-version", "m1mini", "c-105", 5 * HOUR),
        b("chore/bump-go-1.25", "sg3", "c-106", 9 * HOUR),
        b("fix/xgress-retransmit-window", "sg4", "c-107", 1 * DAY),
        b("feat/oidc-device-flow-with-a-very-long-descriptive-branch-name-that-keeps-going-and-going", "m1mini", "c-108", 2 * DAY),
        b("test/flaky-terminator-race", "sg3", "c-109", 3 * DAY),
        b("refactor/identity-store-batching", "sg4", "c-110", 5 * DAY, true),
        b("fix/enroll-token-expiry", "m1mini", "c-111", 6 * DAY, true),
        b("feat/metrics-exemplars", "sg3", "c-112", 9 * DAY)
      ]),
      // a long-dead stale pile: everything two to three weeks old, most released
      repo("openziti", "zrok", 19 * DAY, [
        b("fix/frontend-idle-timeout", "sg3", "c-201", 15 * DAY, true),
        b("feat/agent-share-reuse", "sg3", "c-202", 16 * DAY, true),
        b("chore/openapi-regen", "sg4", "c-203", 17 * DAY, true),
        b("docs/self-hosting-caddy", "m1mini", "c-204", 18 * DAY),
        b("wip/drives-sync", "m1mini", "c-205", 19 * DAY),
        b("fix/metrics-bucket-leak", "sg4", "c-206", 20 * DAY, true),
        b("experiment/udp-share", "sg3", "c-207", 21 * DAY)
      ]),
      // main only
      repo("netfoundry", "docs", 3 * DAY, []),
      // a handful of fresh branches and one released
      repo("netfoundry", "cloud-tools", 6 * HOUR, [
        b("feat/overlay-cost-report", "sg4", "c-401", 50 * MIN),
        b("fix/terraform-drift-check", "sg3", "c-402", 4 * HOUR),
        b("chore/lint-config", "m1mini", "c-403", 1 * DAY, true),
        b("feat/slack-digest", "sg4", "c-404", 2 * DAY)
      ]),
      // nothing pushed yet: the repo the live hub has today
      repo("dovholuknf", "atrium", 0, [])
    ];
    repos[4].main = { sha: "", at: null };
    const tasks = [
      ["sg4", "c-101", "chase the link flap on the router mesh"], ["m1mini", "c-102", "edge router policies, take two"],
      ["sg4", "c-103", "raft snapshots stall under load"], ["sg3", "c-104", "refresh the quickstart"],
      ["m1mini", "c-105", "posture check for OS version"], ["sg4", "c-401", "overlay cost report for finops"],
      ["sg3", "c-202", "reuse agent shares"]
    ].map(([room, id, title]) => ({ id, room, title, display_title: title, status: "working" }));
    return { repos, tasks };
  }
  if (typeof module !== "undefined" && module.exports) module.exports = make;
  else window.hubReposFixture = make;
})();
