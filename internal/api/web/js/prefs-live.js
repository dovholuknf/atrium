// One door for "another window of this browser changed a setting". Loaded by the board and by the phone page, ahead
// of anything that uses it. No DOM and no other script is needed.
//
// A per-browser setting lives in `localStorage`, which every window of the browser shares, and a script reads it once
// at load. The browser fires `storage` in every OTHER document when a key changes, never in the one that wrote it, so
// the writer applies its own change the way it always did and this carries it to the rest. `apply` does what the
// writer's own control does, minus the write, so it is safe to call with the value already stored.
//
// `match` is a key, a key prefix ending in `*`, or an array of either. `apply(key)` gets the key that changed, or
// null when the whole store was cleared. It runs for a clear as well, because every setting just went back to its
// default.
//
//   prefLive("atrium.cardColors", () => { cardColors = readCardColors(); runRefresh(); });
//
// An `apply` that throws is reported on the console and does not stop the others.
const prefLiveList = [];

function prefLiveMatches(match, key) {
  return [].concat(match).some(m => m.endsWith("*") ? key.startsWith(m.slice(0, -1)) : key === m);
}

function prefLive(match, apply) {
  prefLiveList.push({ match, apply });
}

addEventListener("storage", e => {
  try { if (e.storageArea && e.storageArea !== localStorage) return; } catch (err) { return; }
  for (const p of prefLiveList) {
    if (e.key !== null && !prefLiveMatches(p.match, e.key)) continue;
    try { p.apply(e.key); } catch (err) { console.error("[atrium prefs] " + String(p.match) + ": " + err); }
  }
});
