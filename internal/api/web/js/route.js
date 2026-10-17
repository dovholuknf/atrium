// ── the address carries the view ────────────────────────────────────────────
//
//   #/<view>
//   #/hubrepos/<sub>                       shelf, ledger, feed, requests, branches
//   #/hubrepos/requests/<tab>[/<id>]       open or closed, and the change request
//
// The hash and not the query: `#term=` is already a hash and a route starts with `#/`, `?land=` and `?crmock=1` are
// already queries and are never touched, and a hash is not sent to the hub. See docs/changes/f-board-deep-links.md.
//
// `data-route` on <html> is `pending` while a route is being applied and `ready` once it was applied or given up on, and
// `data-route-missing` names the parts that could not be honoured. A test waits on that and on nothing else.

const ROUTE_TABS = ["open", "closed"];
let routeApplied = "";
let routeSeq = 0;
let routeLanded = "";

function routeOfState(s) {
  return { view: s.view || "board", sub: s.sub || "", tab: s.reqTab || s.tab || "", req: s.req || "" };
}

function routeNow() {
  const r = { view: VIEWS.find(isViewing) || "board", sub: "", tab: "", req: "" };
  if (r.view === "hubrepos") {
    r.sub = hubReposView();
    if (r.sub === "requests") { r.tab = cr.tab; r.req = cr.pending || cr.sel; }
  }
  return r;
}

function routeHash(r) {
  let h = "#/" + r.view;
  if (r.view === "hubrepos" && r.sub) {
    h += "/" + r.sub;
    if (r.sub === "requests") h += "/" + (r.tab || "open") + (r.req ? "/" + encodeURIComponent(r.req) : "");
  }
  return h;
}

function routeURL(s) { return location.pathname + location.search + routeHash(routeOfState(s)); }

function routeRecord(s) { routeApplied = routeHash(routeOfState(s)); }

function routeParse(hash) {
  const m = /^#\/(.*)$/.exec(hash || "");
  if (!m) return null;
  const parts = m[1].split("/").filter(Boolean).map(x => { try { return decodeURIComponent(x); } catch (e) { return x; } });
  const r = { view: parts[0] || "board", sub: "", tab: "", req: "", missing: [] };
  if (!VIEWS.includes(r.view)) { r.missing.push("view=" + r.view); r.view = "board"; return r; }
  if (r.view !== "hubrepos") return r;
  r.sub = parts[1] || "shelf";
  if (!HR_VIEWS.includes(r.sub)) { r.missing.push("sub=" + r.sub); r.sub = "shelf"; return r; }
  if (r.sub !== "requests") return r;
  r.tab = parts[2] || "open";
  if (!ROUTE_TABS.includes(r.tab)) { r.missing.push("tab=" + r.tab); r.tab = "open"; }
  r.req = parts[3] || "";
  return r;
}

function routeReady(missing) {
  const d = document.documentElement.dataset;
  d.route = "ready";
  if (missing.length) d.routeMissing = missing.join(" "); else delete d.routeMissing;
}

// The open or closed list and the request in it. A request that has not been read yet is held as pending, with nothing
// selected, so the pane says it is reading instead of showing a request nobody asked for.
function routeSelectRequest(tab, id) {
  cr.form = null; cr.act = ""; cr.err = ""; cr.missing = ""; cr.pending = "";
  cr.tab = tab;
  if (!cr.loaded) { cr.sel = ""; cr.pending = id; return null; }
  const r = id ? cr.reqs.find(x => x.id === id) : (tab === "open" ? crOpen() : crClosed())[0];
  if (id && !r) { cr.sel = ""; cr.missing = id; return null; }
  cr.sel = r ? r.id : "";
  if (r) cr.tab = r.state === "open" ? "open" : "closed";
  return r ? crDetail(r.id) : null;
}

function routeApply(r) {
  const seq = ++routeSeq;
  document.documentElement.dataset.route = "pending";
  const waits = [];
  const was = navApplying;
  navApplying = true;
  try {
    switchView(r.view);
    if (r.view === "hubrepos" && r.sub) {
      if (r.sub === "requests") waits.push(routeSelectRequest(r.tab || "open", r.req));
      setHubReposView(r.sub);
      if (r.sub === "requests" && cr.run) waits.push(cr.run);
    }
  } finally { navApplying = was; }
  return Promise.all(waits).catch(() => {}).then(() => {
    if (seq !== routeSeq) return;
    const missing = (r.missing || []).slice();
    if (cr.missing && r.sub === "requests") missing.push("id=" + cr.missing);
    if (!termOnly() && (!history.state || !history.state.atrium)) navPush(); else navReplace();
    routeReady(missing);
  });
}

// A route in the address when the board opens. Applied before the first poll and before the cover comes off, so the
// first frame is the linked view and never the remembered one.
function routeBoot() {
  const r = routeParse(location.hash);
  if (!r || termOnly()) { routeReady([]); return; }
  routeLanded = r.view;
  routeApply(r);
}

addEventListener("hashchange", () => {
  if (termOnly() || location.hash === routeApplied) return;
  const r = routeParse(location.hash);
  if (!r) return;
  routeRecord({ view: r.view, sub: r.sub, reqTab: r.tab, req: r.req });
  routeApply(r);
});
