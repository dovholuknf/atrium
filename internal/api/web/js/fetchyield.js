// AN UPLOAD NEVER WAITS BEHIND A READ, IN ANY WINDOW.
//
// The browser's six sockets per host are shared by every board and walk window on it, and each spends one on its
// event stream, so the cap in core.js cannot see the reads the other windows hold. A pasted image sat behind them
// until one answered, up to the 15s read timeout. Every fetch on the page passes here, bare or through `api`: an
// upload tells every window, its own included, to abort the reads still waiting for an answer and hold new ones
// until it settles. A read so aborted is asked again, so its caller never sees it. Loaded before core.js and
// rooms.js, which wrap `window.fetch` again on top of this.
const API_YIELD_MAX_MS = 5000;
const apiReadsOut = new Set();
let apiYieldP = null, apiYieldOpen = null, apiYields = 0, apiYieldTimer = 0;
const apiYieldBus = typeof BroadcastChannel === "function" ? new BroadcastChannel("atrium-api-yield") : null;
function apiYield(mine) {
  if (mine && apiYieldBus) apiYieldBus.postMessage("yield");
  apiYields++;
  if (!apiYieldP) apiYieldP = new Promise(res => { apiYieldOpen = res; });
  clearTimeout(apiYieldTimer);
  // A window that closed mid-upload never says done.
  apiYieldTimer = setTimeout(() => { apiYields = 1; apiYieldEnd(false); }, API_YIELD_MAX_MS);
  apiReadsOut.forEach(stop => stop());
}
function apiYieldEnd(mine) {
  if (mine && apiYieldBus) apiYieldBus.postMessage("done");
  if (!apiYieldP || --apiYields > 0) return;
  apiYields = 0;
  clearTimeout(apiYieldTimer);
  const open = apiYieldOpen;
  apiYieldP = apiYieldOpen = null;
  open();
}
if (apiYieldBus) apiYieldBus.onmessage = e => { if (e.data === "yield") apiYield(false); else apiYieldEnd(false); };
const apiWireFetch = window.fetch.bind(window);
window.fetch = async function (input, init) {
  const how = String((init && init.method) || (input && input.method) || "GET").toUpperCase();
  if (how !== "GET" && how !== "HEAD") {
    if (!(init && typeof FormData === "function" && init.body instanceof FormData)) return apiWireFetch(input, init);
    apiYield(true);
    try { return await apiWireFetch(input, init); } finally { apiYieldEnd(true); }
  }
  for (;;) {
    while (apiYieldP) await apiYieldP;
    const outer = (init && init.signal) || (input && input.signal);
    if (outer && outer.aborted) return apiWireFetch(input, init);
    const ctrl = new AbortController();
    let yielded = false;
    const stop = () => { yielded = true; ctrl.abort(); };
    const pass = () => ctrl.abort(outer.reason);
    if (outer) outer.addEventListener("abort", pass, { once: true });
    apiReadsOut.add(stop);
    try {
      return await apiWireFetch(input, Object.assign({}, init, { signal: ctrl.signal }));
    } catch (err) {
      if (!yielded) throw err;
    } finally {
      apiReadsOut.delete(stop);
      if (outer) outer.removeEventListener("abort", pass);
    }
  }
};
