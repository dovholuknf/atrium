#!/usr/bin/env node
// The numbers in docs/runtime/claude-cost-model.md. Every table and chart series there is printed here.
//
//   node scripts/cost-model.js                 print every block as markdown
//   node scripts/cost-model.js NAME            print one block
//   node scripts/cost-model.js --check [FILE]  fail when a block in FILE (the doc by default) differs from its output
//
// No dependencies. Change a price or an assumption below and the blocks follow.
"use strict";

const fs = require("fs");
const path = require("path");

// Dollars per million tokens, from https://platform.claude.com/docs/en/about-claude/pricing, read 2026-10-20.
// Opus 5.5 and Sonnet 5.5 price a cache read at 0.05x input, a 5m write at 1.25x and a 1h write at 2x.
const MODELS = {
  "Opus 5.5": { in: 4, w5: 5, w1: 8, cr: 0.2, out: 20 },
  "Sonnet 5.5": { in: 2, w5: 2.5, w1: 4, cr: 0.1, out: 10 },
};
// Haiku 5.5 prices a request by its prompt length, so a price is a function of the request's context.
const HAIKU = (ctx) => (ctx > 100000
  ? { in: 0.5, w5: 0.625, w1: 1, cr: 0.05, out: 2.5 }
  : { in: 0.1, w5: 0.125, w1: 0.2, cr: 0.01, out: 0.5 });
const OPUS = MODELS["Opus 5.5"];

// The question's shape.
const CONVERSATIONS = 20;
const CALLS_PER_CONVERSATION = 5;
const TOKENS_PER_CALL = 2500; // each is written at the cache write price
const OUTPUT_PER_CALL = 500; // priced as output, on top
const BASES = [10000, 20000, 40000];

// What a refresh by atrium's keep-alive costs beyond reading the context: the fork writes its own tail (the last
// reply and the prompt) and says "OK". 2,059 tokens written and 6 output is the average of 75 warmed refreshes in
// this machine's ledger.
const FORK_TAIL_WRITE = 2000;
const FORK_OUTPUT = 10;
// atrium's policy constants, internal/daemon/keepalive.go.
const KA_MARGIN_MIN = 5;
const KA_TTL_MIN = 60;
const KA_BUDGET_FRACTION = 0.125;
const KA_MIN_CONTEXT = 50000;

// A fresh context with a handoff: the base (system prompt, tools, CLAUDE.md), the handoff text atrium builds, and what
// the new session reads to find its feet again. Assumed, see the doc.
const FRESH_BASE = 20000;
const FRESH_HANDOFF = 5000;
let FRESH_REDISCOVERY = 30000;

// Idle stretches past one hour on this machine, from docs/runtime/cache-keepalive-design.md "How often an idle card
// comes back". Index 0 is idle hour 1 to 2. `still` is how many of the 597 stretches were still idle at its start and
// `back` how many resumed inside it. The 9 to 12 and 12 to 24 buckets are spread evenly over their hours.
const STRETCHES = 597;
const HAZARD = (() => {
  const rows = [[597, 88], [509, 29], [480, 28], [452, 19], [433, 13], [420, 5], [415, 9], [406, 14]];
  for (let i = 0; i < 3; i++) rows.push([392 - (i * 6), 20 / 3]);
  for (let i = 0; i < 12; i++) rows.push([372 - (i * 5), 68 / 12]);
  return rows.map(([still, back], i) => ({ hour: i + 1, still: still / STRETCHES, back: back / STRETCHES }));
})();

const half = (x, d) => (Math.round(x * 10 ** d + 1e-7) / 10 ** d).toFixed(d);
const usd = (x) => (x < -0.005 ? "-$" + half(-x, 2) : "$" + half(x, 2));
const usd3 = (x) => (x < -0.0005 ? "-$" + half(-x, 3) : "$" + half(x, 3));
const k = (n) => (n / 1000) + "k";
const row = (cells) => "| " + cells.join(" | ") + " |";
const table = (head, rows) => [row(head), row(head.map(() => "---")), ...rows.map(row)].join("\n");

// simulate prices one session of `calls` calls starting at c0 tokens of context.
//   ttl: "w5" or "w1", the write price in use
//   coldEvery: a conversation boundary rewrites the whole history instead of reading it (idle past the TTL)
//   shared: the starting context is already in the cache from an earlier session
// Call i reads the context before it, writes its new tokens and outputs its reply. A tiered price is picked per call
// from the prompt length.
function simulate({ c0, calls, price = OPUS, ttl = "w5", coldEvery = 0, shared = false, tokens = TOKENS_PER_CALL,
  output = OUTPUT_PER_CALL, thinking = 0 }) {
  const at = (ctx) => (typeof price === "function" ? price(ctx) : price);
  let total = 0;
  const start = at(c0 + tokens);
  total += (c0 * (shared ? start.cr : start[ttl])) / 1e6;
  for (let i = 0; i < calls; i++) {
    const ctx = c0 + tokens * i;
    const p = at(ctx + tokens);
    const cold = coldEvery > 0 && i > 0 && i % coldEvery === 0;
    total += (ctx * (cold ? p[ttl] : p.cr)) / 1e6;
    total += (tokens * p[ttl] + (output + thinking) * p.out) / 1e6;
  }
  return total;
}

const CALLS = CONVERSATIONS * CALLS_PER_CONVERSATION;

// scenario is the cost of the whole 20 conversations. `sessions` splits them evenly.
function scenario({ c0, sessions, price = OPUS, ttl = "w5", cold = false, shared = false, tokens = TOKENS_PER_CALL,
  output = OUTPUT_PER_CALL, thinking = 0, callsPerConversation = CALLS_PER_CONVERSATION }) {
  const calls = CONVERSATIONS * callsPerConversation / sessions;
  let total = 0;
  for (let s = 0; s < sessions; s++) {
    total += simulate({ c0, calls, price, ttl, coldEvery: cold ? callsPerConversation : 0,
      shared: shared && s > 0, tokens, output, thinking });
  }
  return total;
}

const SHAPES = [
  ["1 session", 1],
  ["2 sessions x 10", 2],
  ["20 separate", 20],
];

function matrix(ttl) {
  const rows = [];
  for (const [name, sessions] of SHAPES) {
    for (const c0 of BASES) {
      const warm = scenario({ c0, sessions, ttl });
      const cold = scenario({ c0, sessions, ttl, cold: true });
      const shared = sessions === 1 ? "n/a" : usd(scenario({ c0, sessions, ttl, shared: true }));
      rows.push([`${name}, ${k(c0)} start`, usd(warm), usd(cold), shared]);
    }
  }
  const coldHead = ttl === "w5" ? "Cold (over 5 min idle between conversations)" : "Cold (over 1 hour idle between conversations)";
  return table(["Scenario", "Warm cache", coldHead, "Warm, base context shared across sessions"], rows);
}

// cumulative is the cost after n conversations, without the fixed new-token cost, for one long session.
function cumulativeLong(c0, n, ttl = "w5") {
  return simulate({ c0, calls: n * CALLS_PER_CONVERSATION, ttl }) - fixedPerCalls(n * CALLS_PER_CONVERSATION, ttl);
}
function cumulativeFresh(c0, n, ttl = "w5") {
  return n * (simulate({ c0, calls: CALLS_PER_CONVERSATION, ttl }) - fixedPerCalls(CALLS_PER_CONVERSATION, ttl));
}
function fixedPerCalls(calls, ttl = "w5", price = OPUS) {
  return (calls * (TOKENS_PER_CALL * price[ttl] + OUTPUT_PER_CALL * price.out)) / 1e6;
}

function cumulativeTable() {
  const rows = [];
  for (const n of [4, 8, 12, 16, 20]) {
    rows.push([String(n), usd(cumulativeLong(10000, n)), usd(cumulativeFresh(10000, n))]);
  }
  return table(["Conversations done", "One long session", "Fresh per conversation"], rows);
}

function crossovers(ttl) {
  const rows = [];
  for (const c0 of BASES) {
    const ratio = OPUS[ttl] / OPUS.cr;
    const marginalHistory = (c0 * ratio) / CALLS_PER_CONVERSATION;
    const perConversation = TOKENS_PER_CALL * CALLS_PER_CONVERSATION;
    let cumulative = "never within 20";
    for (let n = 2; n <= CONVERSATIONS; n++) {
      if (cumulativeFresh(c0, n, ttl) <= cumulativeLong(c0, n, ttl) + 1e-9) { cumulative = String(n); break; }
    }
    rows.push([k(c0), k(marginalHistory), String(Math.ceil(marginalHistory / perConversation)), cumulative]);
  }
  return table(["Base context", "History worth a restart", "Conversations to reach it", "Cumulative cost equal at conversation"],
    rows);
}

const CHART_X = Array.from({ length: 11 }, (_, i) => i * 2);
function chartCumulative(c0) {
  const long = CHART_X.map((n) => cumulativeLong(c0, n).toFixed(2));
  const fresh = CHART_X.map((n) => cumulativeFresh(c0, n).toFixed(2));
  const top = Math.ceil(Math.max(...long, ...fresh));
  return [
    "```mermaid",
    "xychart-beta",
    `    title "${k(c0)} base, warm cache: one long session (first line) against a fresh session per conversation (second)"`,
    `    x-axis "Conversations done" [${CHART_X.join(", ")}]`,
    `    y-axis "Dollars, without the fixed ${usd(fixedPerCalls(CALLS))}" 0 --> ${top}`,
    `    line [${long.join(", ")}]`,
    `    line [${fresh.join(", ")}]`,
    "```",
  ].join("\n");
}

function ttlCompare() {
  const rows = [];
  for (const c0 of BASES) {
    const w5warm = scenario({ c0, sessions: 1 });
    const w5cold = scenario({ c0, sessions: 1, cold: true });
    const w1warm = scenario({ c0, sessions: 1, ttl: "w1" });
    const w1cold = scenario({ c0, sessions: 1, ttl: "w1", cold: true });
    rows.push([`1 session, ${k(c0)} start`, usd(w5warm), usd(w5cold), usd(w1warm), usd(w1cold),
      (w5cold / w1warm).toFixed(1) + "x"]);
  }
  return table(["Scenario", "5m, warm", "5m, cold", "1h, warm", "1h, idle past the hour", "5m cold over 1h warm"], rows);
}

// A refresh and a cold wake, per context size.
const refreshCost = (c) => (c * OPUS.cr + FORK_TAIL_WRITE * OPUS.w1 + FORK_OUTPUT * OPUS.out) / 1e6;
const readCost = (c) => (c * OPUS.cr) / 1e6;
const writeCost = (c) => (c * OPUS.w1) / 1e6;
const budgetOf = (c) => KA_BUDGET_FRACTION * writeCost(c);

// builtRefreshes is how many refreshes the stop rule allows: one more is sent while the spend so far plus a read of
// the context stays inside the budget, as decide() checks it.
function builtRefreshes(c) {
  let spent = 0;
  let n = 0;
  while (n < 100 && spent + readCost(c) <= budgetOf(c)) { spent += refreshCost(c); n++; }
  return n;
}

// evOf is the expected saving of N refreshes per idle stretch against none, at one price per refresh, counted the way
// the design counts it: refresh t is paid by every stretch still idle at the start of hour t and pays back by the share
// that resume inside it, saving a write less a read.
function evOf(c, n, perRefresh) {
  let ev = 0;
  for (const h of HAZARD.slice(0, n)) {
    ev += h.still * ((h.back / h.still) * (writeCost(c) - readCost(c)) - perRefresh);
  }
  return ev;
}
function bestRefreshes(c, perRefresh) {
  let best = 0;
  let bestEv = 0;
  for (let n = 1; n <= 12; n++) {
    const ev = evOf(c, n, perRefresh);
    if (ev > bestEv) { best = n; bestEv = ev; }
  }
  return best;
}

function refreshTable() {
  const rows = [];
  for (const c of [50000, 100000, 150000, 200000, 300000, 430000]) {
    rows.push([k(c), usd3(readCost(c)), usd3(refreshCost(c)), usd2or3(writeCost(c)), usd3(budgetOf(c)),
      String(builtRefreshes(c)), String(bestRefreshes(c, refreshCost(c))), String(bestRefreshes(c, readCost(c)))]);
  }
  return table(["Context", "One read", "One refresh", "One cold wake", "Budget (1/8 wake)", "Refreshes the rule allows",
    "Best count with the tail", "Best count without it"], rows);
}
const usd2or3 = usd;

function evTable() {
  const rows = [];
  for (const c of [1000000, 150000, 50000]) {
    const cells = [k(c) === "1000k" ? "1M tokens" : k(c)];
    for (const n of [1, 2, 3, 4, 5, 8]) cells.push(usd(evOf(c, n, refreshCost(c))));
    rows.push(cells);
  }
  const noTail = [];
  for (const c of [1000000]) {
    const cells = ["1M tokens, no tail"];
    for (const n of [1, 2, 3, 4, 5, 8]) cells.push(usd(evOf(c, n, readCost(c))));
    noTail.push(cells);
  }
  return table(["Context", "N=1", "N=2", "N=3", "N=4", "N=5", "N=8"], [...noTail, ...rows]);
}

// The three ways to meet a card that has been idle for T minutes, at context c. All Opus 5.5 on the 1h cache.
const FRESH_COST = () => ((FRESH_BASE + FRESH_HANDOFF + FRESH_REDISCOVERY) * OPUS.w1) / 1e6;
function kaPlan(c) {
  const n = builtRefreshes(c);
  return { n, cadence: KA_TTL_MIN - KA_MARGIN_MIN, warmEnd: (KA_TTL_MIN - KA_MARGIN_MIN) * n + KA_TTL_MIN };
}
function costKeepAlive(c, minutes) {
  const { n, cadence, warmEnd } = kaPlan(c);
  const done = Math.min(n, Math.floor(minutes / cadence));
  return done * refreshCost(c) + (minutes <= warmEnd ? readCost(c) : writeCost(c));
}
function costColdResume(c, minutes) {
  return minutes <= KA_TTL_MIN ? readCost(c) : writeCost(c);
}
function costFresh(c, minutes) {
  return minutes <= KA_TTL_MIN ? readCost(c) : FRESH_COST();
}
function costKeepAliveThenFresh(c, minutes) {
  const { n, cadence, warmEnd } = kaPlan(c);
  const done = Math.min(n, Math.floor(minutes / cadence));
  return done * refreshCost(c) + (minutes <= warmEnd ? readCost(c) : FRESH_COST());
}

const IDLE_CONTEXT = 150000;
const IDLE_HOURS = Array.from({ length: 17 }, (_, i) => i * 0.5);
function chartIdle() {
  const at = (f) => IDLE_HOURS.map((h) => f(IDLE_CONTEXT, h * 60 + 0.01).toFixed(2));
  const ka = at(costKeepAlive);
  const cold = at(costColdResume);
  const fresh = at(costFresh);
  return [
    "```mermaid",
    "xychart-beta",
    `    title "Cost of the next turn after an idle gap, ${k(IDLE_CONTEXT)} context: keep-alive (first line), cold resume (second), fresh context (third)"`,
    `    x-axis "Idle hours before the card is used again" [${IDLE_HOURS.join(", ")}]`,
    `    y-axis "Dollars" 0 --> 1.5`,
    `    line [${ka.join(", ")}]`,
    `    line [${cold.join(", ")}]`,
    `    line [${fresh.join(", ")}]`,
    "```",
  ].join("\n");
}

function idleTable() {
  const c = IDLE_CONTEXT;
  const rows = [];
  for (const h of [0.5, 1, 2, 3, 4, 5, 8, 24]) {
    const m = h * 60 + 0.01;
    rows.push([`${h} h`, usd(costKeepAlive(c, m)), usd(costColdResume(c, m)), usd(costFresh(c, m)),
      usd(costKeepAliveThenFresh(c, m))]);
  }
  return table(["Idle gap", "Keep-alive, then cold resume", "Cold resume only", "Fresh at the hour", "Keep-alive, then fresh"],
    rows);
}

// returnsInTime is the share of stretches past one hour that resume before the minute `limit`.
function returnsInTime(limit) {
  let share = 0;
  for (const h of HAZARD) {
    const from = h.hour * 60;
    share += h.back * Math.min(1, Math.max(0, (limit - from) / 60));
  }
  return share;
}

// The refreshes pay when the share of stretches that resume inside the warm window, times what a resume saves over the
// best alternative, passes what the refreshes cost.
function breakEvenTable() {
  const rows = [];
  for (const c of [50000, 100000, 150000, 200000, 300000, 430000]) {
    const { n, warmEnd } = kaPlan(c);
    const alt = Math.min(FRESH_COST(), writeCost(c));
    const spend = n * refreshCost(c);
    rows.push([k(c), String(n), (warmEnd / 60).toFixed(2) + " h", usd(spend), usd(writeCost(c)), usd(FRESH_COST()),
      (spend / (alt - readCost(c)) * 100).toFixed(0) + "%", (returnsInTime(warmEnd) * 100).toFixed(0) + "%"]);
  }
  return table(["Context", "Refreshes", "Warm until", "Refresh spend", "Cold wake", "Fresh context",
    "Return chance that pays for the refreshes", "Return chance on this machine"], rows);
}

// Expected cost per idle stretch (one that passed an hour), by policy, using how long stretches last on this machine.
// A stretch that never resumed inside 30 hours spends its refreshes and returns nothing.
function stretchCost(c, policy) {
  let total = 0;
  let back = 0;
  for (const h of HAZARD) {
    const m = (h.hour + 0.5) * 60;
    const cost = policy(c, m);
    total += h.back * cost;
    back += h.back;
  }
  const never = 1 - back;
  return { total: total + never * policy(c, null), never };
}
function policyCost(kind) {
  return (c, m) => {
    const { n } = kaPlan(c);
    switch (kind) {
      case "cold": return m === null ? 0 : writeCost(c);
      case "ka": return m === null ? n * refreshCost(c) : costKeepAlive(c, m);
      case "kafresh": return m === null ? n * refreshCost(c) : costKeepAliveThenFresh(c, m);
      case "fresh": return m === null ? 0 : FRESH_COST();
      default: throw new Error(kind);
    }
  };
}
function policyTable() {
  const rows = [];
  for (const c of [50000, 100000, 150000, 200000, 300000, 430000]) {
    const cells = [k(c)];
    for (const kind of ["cold", "ka", "kafresh", "fresh"]) cells.push(usd3(stretchCost(c, policyCost(kind)).total));
    rows.push(cells);
  }
  return table(["Context", "Cold resume only", "Keep-alive, then cold resume", "Keep-alive, then fresh",
    "Fresh at expiry, no keep-alive"], rows);
}

function policySensitivity() {
  const saved = FRESH_REDISCOVERY;
  const rows = [];
  for (const c of [150000, 300000]) {
    for (const d of [10000, 30000, 60000, 100000]) {
      FRESH_REDISCOVERY = d;
      rows.push([k(c), k(d), usd(FRESH_COST()), usd3(stretchCost(c, policyCost("cold")).total),
        usd3(stretchCost(c, policyCost("ka")).total), usd3(stretchCost(c, policyCost("kafresh")).total),
        usd3(stretchCost(c, policyCost("fresh")).total)]);
    }
  }
  FRESH_REDISCOVERY = saved;
  return table(["Context", "Rediscovery", "Fresh context", "Cold resume only", "Keep-alive, then cold", "Keep-alive, then fresh",
    "Fresh at expiry"], rows);
}

// keepAliveStopsPaying is the context above which fresh at expiry beats keep-alive then fresh, per stretch.
function keepAliveStopsPaying() {
  for (let c = 20000; c <= 600000; c += 1000) {
    if (stretchCost(c, policyCost("kafresh")).total > stretchCost(c, policyCost("fresh")).total) return c;
  }
  return null;
}

function rediscoveryTies() {
  const saved = FRESH_REDISCOVERY;
  const out = [];
  for (const c of [150000, 300000]) {
    let tie = null;
    for (let d = 0; d <= 200000; d += 1000) {
      FRESH_REDISCOVERY = d;
      if (stretchCost(c, policyCost("fresh")).total > stretchCost(c, policyCost("kafresh")).total) { tie = d; break; }
    }
    out.push([c, tie]);
  }
  FRESH_REDISCOVERY = saved;
  return out;
}

function neverBack() {
  return 1 - HAZARD.reduce((a, h) => a + h.back, 0);
}

function winnerGrid() {
  const rows = [];
  for (const c of [50000, 100000, 150000, 200000, 300000, 430000]) {
    const cells = [k(c)];
    for (const h of [0.5, 2, 4, 8, 24]) {
      const m = h * 60 + 0.01;
      const options = [
        ["keep-alive", costKeepAlive(c, m)],
        ["cold resume", costColdResume(c, m)],
        ["fresh", costFresh(c, m)],
      ];
      options.sort((a, b) => a[1] - b[1] || 0);
      const best = options[0];
      const tied = options.filter((o) => Math.abs(o[1] - best[1]) < 0.0005).map((o) => o[0]);
      cells.push(tied.length > 1 ? tied.slice(0, 2).join(" = ") : best[0]);
    }
    rows.push(cells);
  }
  return table(["Context", "0.5 h", "2 h", "4 h", "8 h", "24 h"], rows);
}

function freshBreakEven() {
  const wake = FRESH_BASE + FRESH_HANDOFF + FRESH_REDISCOVERY;
  const rows = [];
  for (const d of [10000, 30000, 60000, 100000]) {
    const total = FRESH_BASE + FRESH_HANDOFF + d;
    rows.push([k(d), k(total), usd(total * OPUS.w1 / 1e6), usd((IDLE_CONTEXT - total) * OPUS.cr / 1e6 * 10)]);
  }
  return { wake, table: table(["Rediscovery tokens", "Fresh context written", "Cost", "Saved reading 10 later calls at 150k"], rows) };
}

// Holes in the matrix.
function holesCompaction() {
  const summary = 8000;
  const rows = [];
  for (const c0 of BASES) {
    for (const atConv of [8, 12]) {
      const none = simulate({ c0, calls: CALLS });
      const callsBefore = atConv * CALLS_PER_CONVERSATION;
      const before = simulate({ c0, calls: callsBefore });
      const ctx = c0 + TOKENS_PER_CALL * callsBefore;
      const compactCall = (ctx * OPUS.cr + summary * OPUS.out) / 1e6;
      const after = (summary * OPUS.w5) / 1e6 + simulateFrom({ ctx: c0 + summary, calls: CALLS - callsBefore, base: c0 });
      const total = before + compactCall + after;
      rows.push([`${k(c0)} start, after conversation ${atConv}`, usd(none), usd(total), usd(compactCall)]);
    }
  }
  return table(["One long session, warm", "No compaction", "With compaction", "The compaction call alone"], rows);
}
// simulateFrom continues a session whose context is already ctx, base of it still cached.
function simulateFrom({ ctx, calls }) {
  let total = 0;
  for (let i = 0; i < calls; i++) {
    const c = ctx + TOKENS_PER_CALL * i;
    total += (c * OPUS.cr + TOKENS_PER_CALL * OPUS.w5 + OUTPUT_PER_CALL * OPUS.out) / 1e6;
  }
  return total;
}

function holesThinking() {
  const rows = [];
  for (const think of [0, 2000]) {
    const warm = scenario({ c0: 10000, sessions: 1, thinking: think });
    const cold = scenario({ c0: 10000, sessions: 1, cold: true, thinking: think });
    const sep = scenario({ c0: 10000, sessions: 20, thinking: think });
    rows.push([think === 0 ? "none" : "2k per call", usd(warm), usd(cold), usd(sep), (cold / warm).toFixed(1) + "x"]);
  }
  return table(["Thinking", "1 session, warm", "1 session, cold", "20 separate", "Cold over warm"], rows);
}

function holesRediscovery() {
  const long = scenario({ c0: 10000, sessions: 1 });
  const rows = [];
  let crossing = null;
  for (let d = 0; d <= 200000; d += 1000) {
    const sep = scenario({ c0: 10000 + d, sessions: 20 });
    if (crossing === null && sep > long) crossing = d;
  }
  for (const d of [0, 5000, 10000, 20000, 40000]) {
    const sep = scenario({ c0: 10000 + d, sessions: 20 });
    const sepShared = scenario({ c0: 10000 + d, sessions: 20 }) - 19 * 10000 * (OPUS.w5 - OPUS.cr) / 1e6;
    rows.push([k(d), usd(sep), usd(sepShared), usd(long)]);
  }
  return { crossing, table: table(["Rediscovery per session", "20 separate, 10k start", "20 separate, base shared", "1 session, warm"], rows) };
}

function holesParallel() {
  const rows = [];
  for (const [label, parallel] of [["five calls per conversation", false], ["one call per conversation", true]]) {
    const opts = parallel
      ? { tokens: TOKENS_PER_CALL * 5, output: OUTPUT_PER_CALL * 5, callsPerConversation: 1 }
      : {};
    rows.push([label, usd(scenario({ c0: 10000, sessions: 1, ...opts })),
      usd(scenario({ c0: 10000, sessions: 1, cold: true, ...opts })),
      usd(scenario({ c0: 10000, sessions: 20, ...opts }))]);
  }
  return table(["10k start", "1 session, warm", "1 session, cold", "20 separate"], rows);
}

function holesModels() {
  const rows = [];
  for (const [name, price] of [["Opus 5.5", OPUS], ["Sonnet 5.5", MODELS["Sonnet 5.5"]], ["Haiku 5.5", HAIKU]]) {
    rows.push([name, usd(scenario({ c0: 10000, sessions: 1, price })),
      usd(scenario({ c0: 10000, sessions: 1, price, cold: true })),
      usd(scenario({ c0: 10000, sessions: 20, price })),
      usd(scenario({ c0: 40000, sessions: 20, price }))]);
  }
  return table(["Model", "1 session, 10k, warm", "1 session, 10k, cold", "20 separate, 10k", "20 separate, 40k"], rows);
}

// Orchestrators and workers, the holes atrium's own usage shows.
const SHARED_PREFIX = 12147;
function holesSharedPrefix() {
  const rows = [];
  for (const c0 of BASES) {
    const unshared = scenario({ c0, sessions: 20 });
    const saved = 19 * Math.min(c0, SHARED_PREFIX) * (OPUS.w5 - OPUS.cr) / 1e6;
    rows.push([k(c0), usd(unshared), usd(unshared - saved), usd(scenario({ c0, sessions: 20, shared: true }))]);
  }
  return table(["20 separate, 5m cache", "Nothing shared", "Up to 12,147 tokens shared", "Whole start shared"], rows);
}

function holesWake() {
  const rows = [];
  for (const c of [100000, 150000, 250000]) {
    const idleWake = writeCost(c);
    const next10 = 10 * readCost(c);
    rows.push([k(c), usd(idleWake), usd(readCost(c)), usd(next10), usd(idleWake + next10)]);
  }
  return table(["Orchestrator context", "Wake after an hour idle", "Wake inside the hour", "Next 10 calls, warm", "First wake plus 10 calls"], rows);
}

function holesWorkerPin() {
  const rows = [];
  for (const c0 of [20000, 40000]) {
    const w5 = scenario({ c0, sessions: 1, ttl: "w5" });
    const w1 = scenario({ c0, sessions: 1, ttl: "w1" });
    rows.push([k(c0), usd(w5), usd(w1), usd(w1 - w5)]);
  }
  return table(["A 100 call worker, no pause over 5 min", "5m cache", "1h pin", "Extra for the pin"], rows);
}

// Measured on this machine's room database on 2026-10-20 (session_usage, all rows), tokens by model. Not a model
// output: the one place this file reads a measurement and not an assumption.
const MEASURED = {
  "Opus 5.5": { in: 67339, w5: 1457914, w1: 78362017, cr: 4927675771, out: 18142661 },
  "Sonnet 5.5": { in: 62040, w5: 976065, w1: 70039032, cr: 2765363659, out: 16599589 },
};
function priceOf(t, p) {
  return (t.in * p.in + t.w5 * p.w5 + t.w1 * p.w1 + t.cr * p.cr + t.out * p.out) / 1e6;
}
function measuredTable() {
  const atriumSonnet = { in: 2, w5: 2.5, w1: 4, cr: 0.2, out: 10 };
  const rows = [
    ["Opus 5.5", usd(priceOf(MEASURED["Opus 5.5"], OPUS)), usd(priceOf(MEASURED["Opus 5.5"], OPUS)), "$0.00"],
    ["Sonnet 5.5", usd(priceOf(MEASURED["Sonnet 5.5"], atriumSonnet)), usd(priceOf(MEASURED["Sonnet 5.5"], MODELS["Sonnet 5.5"])),
      usd(priceOf(MEASURED["Sonnet 5.5"], atriumSonnet) - priceOf(MEASURED["Sonnet 5.5"], MODELS["Sonnet 5.5"]))],
  ];
  return table(["Model", "At atrium's table", "At the pricing page", "Overstated by"], rows);
}

function pinTable() {
  const rows = Object.entries(MEASURED).map(([name, t]) => {
    const p = MODELS[name];
    return [name, (t.w1 / 1e6).toFixed(1) + "M", usd(t.w1 * (p.w1 - p.w5) / 1e6)];
  });
  return table(["Model", "Tokens written at the 1h price", "Premium over the 5m price"], rows);
}

// One percent of the weekly subscription limit, in list-price dollars: the single-point estimate in
// docs/runtime/cache-keepalive-design.md, from the usage report of 2026-09-27.
const DOLLARS_PER_WEEKLY_PERCENT = 17.1;
function subscriptionTable() {
  const rows = [];
  for (const [label, opts] of [["1 session, 10k start, warm", { sessions: 1 }], ["1 session, 10k start, cold", { sessions: 1, cold: true }],
    ["20 separate, 10k start", { sessions: 20 }]]) {
    const cost = scenario({ c0: 10000, ...opts });
    rows.push([label, usd(cost), (cost / DOLLARS_PER_WEEKLY_PERCENT).toFixed(2) + "%"]);
  }
  return table(["Opus 5.5", "List price", "Share of the weekly limit"], rows);
}

function pricesTable() {
  const rows = Object.entries(MODELS).map(([name, p]) => [name, usd(p.in), usd(p.w5), usd(p.w1), usd(p.cr), usd(p.out)]);
  return table(["Model", "Input", "5m write", "1h write", "Cache read", "Output"], rows);
}

function pricesTable5() {
  const p = OPUS;
  return table(["Per million tokens, Opus 5.5", "Price", "Against a cache read"], [
    ["Cache read", usd(p.cr), "1x"],
    ["Plain input", usd(p.in), (p.in / p.cr) + "x"],
    ["5m cache write", usd(p.w5), (p.w5 / p.cr) + "x"],
    ["1h cache write", usd(p.w1), (p.w1 / p.cr) + "x"],
    ["Output", usd(p.out), (p.out / p.cr) + "x"],
  ]);
}

function assumptionsTable() {
  const fresh = FRESH_BASE + FRESH_HANDOFF + FRESH_REDISCOVERY;
  return table(["Assumption", "Value"], [
    ["Conversations, calls in each", `${CONVERSATIONS}, ${CALLS_PER_CONVERSATION}`],
    ["New tokens per call, written at the write price", String(TOKENS_PER_CALL)],
    ["Output per call, at the output price", String(OUTPUT_PER_CALL)],
    ["Fixed cost of the new tokens, 5m cache", usd(fixedPerCalls(CALLS))],
    ["Fixed cost of the new tokens, 1h cache", usd(fixedPerCalls(CALLS, "w1"))],
    ["Keep-alive fork tail written, output", `${FORK_TAIL_WRITE}, ${FORK_OUTPUT}`],
    ["Keep-alive margin, cache lifetime, budget", `${KA_MARGIN_MIN} min, ${KA_TTL_MIN} min, ${KA_BUDGET_FRACTION} of one wake`],
    ["Fresh context: base, handoff, rediscovery", `${k(FRESH_BASE)}, ${k(FRESH_HANDOFF)}, ${k(FRESH_REDISCOVERY)} (${k(fresh)} written)`],
  ]);
}

// Break-even facts printed as one block so the doc's prose can quote them.
function facts() {
  const fresh = FRESH_BASE + FRESH_HANDOFF + FRESH_REDISCOVERY;
  const rd = holesRediscovery();
  const lines = [
    `Fresh context writes ${k(fresh)} tokens: ${usd(FRESH_COST())}. It beats a cold wake above ${k(fresh)} of context.`,
    `Warm restart break-even, 5m cache: history above ${OPUS.w5 / OPUS.cr / CALLS_PER_CONVERSATION}x the base. 1h cache: ${OPUS.w1 / OPUS.cr / CALLS_PER_CONVERSATION}x.`,
    `Twenty separate sessions at a 10k start stop beating one warm session when each rediscovers more than ${k(rd.crossing)} tokens.`,
    `The keep-alive minimum context ${k(KA_MIN_CONTEXT)}: a cold wake there is ${usd(writeCost(KA_MIN_CONTEXT))}, a refresh ${usd3(refreshCost(KA_MIN_CONTEXT))}.`,
    `A refresh's fixed part, the fork's own tail and reply: ${usd3(refreshCost(0))}.`,
    `Keep-alive then fresh stops beating fresh at expiry above ${k(keepAliveStopsPaying())} of context, with ${k(FRESH_REDISCOVERY)} of rediscovery.`,
    `${(neverBack() * 100).toFixed(0)}% of stretches past one hour have not resumed inside 30 hours, and a refresh spent on them buys nothing.`,
    `A refresh every ${KA_TTL_MIN - KA_MARGIN_MIN} minutes against one every ${KA_TTL_MIN}: ${((KA_TTL_MIN / (KA_TTL_MIN - KA_MARGIN_MIN) - 1) * 100).toFixed(0)}% more refreshes per hour kept warm.`,
    `Switching a 150k card from Opus 5.5 to Sonnet 5.5 writes its context again: ${usd((150000 * MODELS["Sonnet 5.5"].w1) / 1e6)} at the 1h price, against ${usd3((150000 * MODELS["Sonnet 5.5"].cr) / 1e6)} to read it.`,
    `One fresh context costs as much as ${(FRESH_COST() / refreshCost(IDLE_CONTEXT)).toFixed(1)} refreshes at ${k(IDLE_CONTEXT)}.`,
    `Warm window at ${k(IDLE_CONTEXT)} with the stop rule: ${(kaPlan(IDLE_CONTEXT).warmEnd / 60).toFixed(2)} h after the last reply, ${kaPlan(IDLE_CONTEXT).n} refreshes.`,
    `One 5m pause at 100k context costs ${usd((100000 * (OPUS.w5 - OPUS.cr)) / 1e6)} to rewrite. The pin premium is ${usd(OPUS.w1 - OPUS.w5)} per million, so one pause pays for ${k(Math.round((100000 * (OPUS.w5 - OPUS.cr)) / (OPUS.w1 - OPUS.w5)))} tokens written.`,
    `A ${k(IDLE_CONTEXT)} refresh run spends ${usd3(kaPlan(IDLE_CONTEXT).n * refreshCost(IDLE_CONTEXT))} and saves ${usd3(writeCost(IDLE_CONTEXT) - readCost(IDLE_CONTEXT))} on a return, so it needs a ${((kaPlan(IDLE_CONTEXT).n * refreshCost(IDLE_CONTEXT)) / (writeCost(IDLE_CONTEXT) - readCost(IDLE_CONTEXT)) * 100).toFixed(0)}% return chance against a cold wake.`,
    `${(FRESH_COST() / refreshCost(IDLE_CONTEXT)).toFixed(1)} refreshes at ${k(IDLE_CONTEXT)} take ${((FRESH_COST() / refreshCost(IDLE_CONTEXT)) * (KA_TTL_MIN - KA_MARGIN_MIN) / 60).toFixed(1)} h.`,
    `Rediscovery where fresh at expiry stops beating keep-alive then fresh: ${rediscoveryTies().map(([c, d]) => `${k(c)} context at ${d === null ? "none" : k(d)}`).join(", ")}.`,
    `A ${SHARED_PREFIX} token shared prefix saves ${usd3((SHARED_PREFIX * (OPUS.w5 - OPUS.cr)) / 1e6)} per session.`,
  ];
  return lines.map((l) => "- " + l).join("\n");
}

const BLOCKS = new Map([
  ["prices", pricesTable()],
  ["price-ratios", pricesTable5()],
  ["assumptions", assumptionsTable()],
  ["matrix-5m", matrix("w5")],
  ["cumulative", cumulativeTable()],
  ["chart-cumulative-10k", chartCumulative(10000)],
  ["chart-cumulative-20k", chartCumulative(20000)],
  ["chart-cumulative-40k", chartCumulative(40000)],
  ["break-even-5m", crossovers("w5")],
  ["matrix-1h", matrix("w1")],
  ["break-even-1h", crossovers("w1")],
  ["ttl-compare", ttlCompare()],
  ["refresh-cost", refreshTable()],
  ["refresh-ev", evTable()],
  ["idle-table", idleTable()],
  ["chart-idle", chartIdle()],
  ["break-even-fresh", breakEvenTable()],
  ["winner-grid", winnerGrid()],
  ["policy-ev", policyTable()],
  ["policy-sensitivity", policySensitivity()],
  ["fresh-rediscovery", freshBreakEven().table],
  ["hole-compaction", holesCompaction()],
  ["hole-thinking", holesThinking()],
  ["hole-rediscovery", holesRediscovery().table],
  ["hole-parallel", holesParallel()],
  ["hole-models", holesModels()],
  ["hole-shared-prefix", holesSharedPrefix()],
  ["hole-wake", holesWake()],
  ["hole-worker-pin", holesWorkerPin()],
  ["measured-sonnet", measuredTable()],
  ["measured-pin", pinTable()],
  ["subscription", subscriptionTable()],
  ["facts", facts()],
]);

const marker = (name) => `<!-- cost-model:${name} -->`;

function main(argv) {
  const args = argv.slice(2);
  if (args[0] === "--check") {
    const file = args[1] || path.join(__dirname, "..", "docs", "runtime", "claude-cost-model.md");
    const doc = fs.readFileSync(file, "utf8").replace(/\r\n/g, "\n");
    let bad = 0;
    for (const [name, text] of BLOCKS) {
      if (name !== "facts" && !doc.includes(marker(name) + "\n" + text + "\n")) { console.error(`differs or missing: ${name}`); bad++; }
    }
    if (bad) process.exit(1);
    console.log(`${BLOCKS.size} blocks match ${file}`);
    return;
  }
  const only = args[0];
  if (only && !BLOCKS.has(only)) {
    console.error("no block " + only + ". blocks: " + [...BLOCKS.keys()].join(", "));
    process.exit(2);
  }
  for (const [name, text] of BLOCKS) {
    if (only && only !== name) continue;
    console.log(marker(name));
    console.log(text);
    console.log("");
  }
}

main(process.argv);
