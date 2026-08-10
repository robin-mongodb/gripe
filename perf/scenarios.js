// Gripe perf harness (task 38). One script, four scenarios: create, list,
// refund flow, subscription churn. The cycler worker isn't HTTP-driveable —
// it runs in compose against the same DB during these runs, which is the
// intended background load (see architecture.md).
//
// Run:  k6 run -e BASE=http://<api-host>:8080/v1 -e MERCHANTS=50 scenarios.js
// Hit the api port directly, NOT nginx — nginx rate-limits /v1/ to 100 r/s.
//
// Open model (constant-arrival-rate) so latency degradation shows up as
// latency, not as silently reduced throughput.

import http from "k6/http";
import { check } from "k6";

const BASE = __ENV.BASE || "http://localhost:8080/v1";
const MERCHANTS = Number(__ENV.MERCHANTS || 5); // matches seed -m
const RATE = Number(__ENV.RATE || 1); // multiplier for all arrival rates
const DURATION = __ENV.DURATION || "2m";

const CURRENCIES = ["USD", "GBP", "EUR"];
const METHODS = ["card", "apple_pay", "google_pay", "direct_debit", "bank_transfer"];

export const options = {
  scenarios: {
    create: {
      executor: "constant-arrival-rate",
      exec: "createPayment",
      rate: 40 * RATE,
      timeUnit: "1s",
      duration: DURATION,
      preAllocatedVUs: 50,
      maxVUs: 200,
    },
    list: {
      executor: "constant-arrival-rate",
      exec: "listPayments",
      rate: 40 * RATE,
      timeUnit: "1s",
      duration: DURATION,
      preAllocatedVUs: 30,
      maxVUs: 100,
    },
    refund_flow: {
      executor: "constant-arrival-rate",
      exec: "refundFlow",
      rate: 10 * RATE,
      timeUnit: "1s",
      duration: DURATION,
      preAllocatedVUs: 20,
      maxVUs: 100,
    },
    subscription_churn: {
      executor: "constant-arrival-rate",
      exec: "subscriptionChurn",
      rate: 2 * RATE,
      timeUnit: "1s",
      duration: DURATION,
      preAllocatedVUs: 5,
      maxVUs: 20,
    },
  },
  thresholds: {
    // Per-scenario latency + error gates; tune after the first baseline run.
    "http_req_duration{scenario:create}": ["p(95)<500"],
    "http_req_duration{scenario:list}": ["p(95)<500"],
    "http_req_duration{scenario:refund_flow}": ["p(95)<800"],
    http_req_failed: ["rate<0.01"],
  },
  summaryTrendStats: ["avg", "p(50)", "p(95)", "p(99)", "max"],
};

// Dependency-free unique key: VU + iteration + wall clock.
function idem(prefix) {
  return `k6-${prefix}-${__VU}-${__ITER}-${Date.now()}`;
}

function merchant() {
  return `mer_seed_${String(Math.floor(Math.random() * MERCHANTS)).padStart(3, "0")}`;
}

function headers(role, id, key) {
  const h = {
    "Content-Type": "application/json",
    "X-Actor-Role": role,
    "X-Actor-Id": id,
  };
  if (key) h["Idempotency-Key"] = key;
  return h;
}

// Amount 10.00–200.00 that never ends in .13 — declines are exercised
// separately below so the refund flow always has a refundable payment.
function cleanAmount() {
  let a = 1000 + Math.floor(Math.random() * 19000);
  if (a % 100 === 13) a += 1;
  return a;
}

export function createPayment() {
  const m = merchant();
  // ~5% deliberate declines so the perf run exercises that branch too.
  const amount = Math.random() < 0.05 ? 5013 : cleanAmount();
  const res = http.post(
    `${BASE}/payments`,
    JSON.stringify({
      merchant_id: m,
      customer_id: `cus_perf_${__VU}`,
      amount_minor: amount,
      currency: CURRENCIES[Math.floor(Math.random() * CURRENCIES.length)],
      method: METHODS[Math.floor(Math.random() * METHODS.length)],
    }),
    { headers: headers("customer", `cus_perf_${__VU}`, idem("c")) },
  );
  check(res, { "create 201": (r) => r.status === 201 });
}

export function listPayments() {
  const m = merchant();
  const res = http.get(`${BASE}/payments`, { headers: headers("merchant", m) });
  const ok = check(res, { "list 200": (r) => r.status === 200 });
  // Follow one cursor page ~30% of the time — exercises keyset pagination.
  if (ok && Math.random() < 0.3) {
    const cursor = res.json("next_cursor");
    if (cursor) {
      http.get(`${BASE}/payments?cursor=${encodeURIComponent(cursor)}`, {
        headers: headers("merchant", m),
        tags: { name: "list_page2" },
      });
    }
  }
}

export function refundFlow() {
  const m = merchant();
  // Card + clean amount always lands captured -> refundable immediately.
  const create = http.post(
    `${BASE}/payments`,
    JSON.stringify({
      merchant_id: m,
      customer_id: `cus_perf_${__VU}`,
      amount_minor: cleanAmount(),
      currency: "GBP",
      method: "card",
    }),
    { headers: headers("customer", `cus_perf_${__VU}`, idem("rf-c")), tags: { name: "refund_create" } },
  );
  if (!check(create, { "refund-flow create 201": (r) => r.status === 201 })) return;
  const p = create.json();
  const res = http.post(
    `${BASE}/payments/${p.id}/refunds`,
    JSON.stringify({ amount_minor: Math.max(1, Math.floor(p.amount_minor / 2)) }),
    { headers: headers("merchant", m, idem("rf-r")), tags: { name: "refund" } },
  );
  check(res, { "refund 201": (r) => r.status === 201 });
}

export function subscriptionChurn() {
  const m = merchant();
  const create = http.post(
    `${BASE}/subscriptions`,
    JSON.stringify({
      merchant_id: m,
      customer_id: `cus_perf_${__VU}`,
      amount_minor: cleanAmount(),
      currency: "USD",
      method: "card",
      cadence: "monthly",
    }),
    { headers: headers("merchant", m, idem("s-c")), tags: { name: "sub_create" } },
  );
  if (!check(create, { "sub create 201": (r) => r.status === 201 })) return;
  // Cancel half of them; the survivors feed the cycler's DueSubscriptions scan.
  if (Math.random() < 0.5) {
    http.post(`${BASE}/subscriptions/${create.json("id")}/cancel`, null, {
      headers: headers("merchant", m, idem("s-x")),
      tags: { name: "sub_cancel" },
    });
  }
}

// Write a machine-readable summary next to the human one; name it per backend
// so mongo + pg runs don't clobber each other: -e LABEL=mongo|postgres
export function handleSummary(data) {
  const label = __ENV.LABEL || "run";
  return {
    stdout: JSON.stringify(minify(data), null, 2),
    [`results-${label}.json`]: JSON.stringify(data),
  };
}

function minify(data) {
  const out = {};
  for (const [name, m] of Object.entries(data.metrics)) {
    if (name.startsWith("http_req_duration") && m.values) {
      out[name] = {
        p50: m.values["p(50)"],
        p95: m.values["p(95)"],
        p99: m.values["p(99)"],
      };
    }
  }
  out.http_req_failed = data.metrics.http_req_failed?.values?.rate;
  out.iterations = data.metrics.iterations?.values?.count;
  return out;
}
