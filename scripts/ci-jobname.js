// A ci.yml job name to the os and group the artifacts and stores use, null for a job that runs no group.
const OS = { linux: 'ubuntu', macos: 'macos', windows: 'windows' };
const SIDE = { go: 'flaky-go', playwright: 'flaky-board' };

function parseJobName(name) {
  const c = /^check \((\w+)-latest, (.+)\)$/.exec(name);
  if (c) return { os: c[1], group: c[2] };
  const f = /^flaky-(linux|macos|windows)-(go|playwright)$/.exec(name);
  if (f) return { os: OS[f[1]], group: SIDE[f[2]] };
  return null;
}

const staticName = (os, group) => {
  const side = Object.keys(SIDE).find((k) => SIDE[k] === group);
  const p = Object.keys(OS).find((k) => OS[k] === os);
  return side && p ? `flaky-${p}-${side}` : null;
};

module.exports = { parseJobName, staticName };
