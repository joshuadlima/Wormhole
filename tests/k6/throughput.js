// Data-plane load test: HTTP request throughput at TARGET, stepping through
// increasing arrival rates so the knee is visible rather than inferred from
// a single data point.
//
// Run this script TWICE with the same RATES/STAGE_DURATION - once against
// the origin directly, once through a tunnel - and compare. A tunnel number
// alone ("p99 24ms") is unverifiable; "p99 24ms through the tunnel vs 16ms
// direct, same load" is a claim a reader can check:
//
//   k6 run -e TARGET=http://localhost:8080/ --tag run=baseline \
//     --summary-export=baseline.json tests/k6/throughput.js
//
//   k6 run -e TARGET=https://<subdomain>.example.com/ -e INSECURE=true \
//     --tag run=tunnel --summary-export=tunnel.json tests/k6/throughput.js
//
// Run both from the same machine, in the same session, or the comparison is
// worthless - see docs/load-testing.md.
//
// Note the executor: constant-arrival-rate is an OPEN model, so k6 keeps
// offering the configured rate whether or not the server keeps up. A closed
// model (fixed VUs, each waiting for its response) would quietly reduce
// offered load as latency climbed, hiding the saturation point and
// reporting optimistic percentiles - coordinated omission.

import http from 'k6/http';
import { check } from 'k6';

const TARGET = __ENV.TARGET;
if (!TARGET) {
  throw new Error('TARGET is required, e.g. -e TARGET=https://abc123.example.com/');
}

const STAGE_DURATION = __ENV.STAGE_DURATION || '30s';
const RATES = (__ENV.RATES || '50,100,200,400,800').split(',').map(Number);

// parseInt('1m') is 1, not 60 - that alone would stack every stage's
// startTime at 0s/1s/2s instead of spacing them out, so every rate would
// fire at once instead of stepping. Parse the unit explicitly.
function toSeconds(duration) {
  const match = /^(\d+(?:\.\d+)?)(s|m|h)$/.exec(duration);
  if (!match) {
    throw new Error(`STAGE_DURATION must look like "30s", "2m", or "1h", got "${duration}"`);
  }
  const value = parseFloat(match[1]);
  const unit = { s: 1, m: 60, h: 3600 }[match[2]];
  return value * unit;
}

const stageSeconds = toSeconds(STAGE_DURATION);

export const options = {
  // insecureSkipTLSVerify keeps staging ACME certificates from failing the run.
  insecureSkipTLSVerify: __ENV.INSECURE === 'true',
  discardResponseBodies: true,

  scenarios: Object.fromEntries(
    RATES.map((rate, i) => [
      `rate_${rate}`,
      {
        executor: 'constant-arrival-rate',
        rate: rate,
        timeUnit: '1s',
        duration: STAGE_DURATION,
        // Headroom so the executor is never itself the bottleneck; k6 warns
        // if it cannot allocate enough VUs to sustain the rate.
        preAllocatedVUs: Math.max(50, rate),
        maxVUs: Math.max(200, rate * 4),
        startTime: `${i * stageSeconds}s`,
        tags: { rate: String(rate) },
      },
    ])
  ),

  thresholds: {
    // Declared up front so a run has a verdict instead of a vibe.
    http_req_failed: ['rate<0.01'],
    // Scoped per rate, not just overall - an aggregate p95 across 50 and
    // 800 req/s together would hide exactly the degradation this test
    // exists to find, by averaging the easy stage in with the hard one.
    ...Object.fromEntries(
      RATES.map((rate) => [
        `http_req_duration{rate:${rate}}`,
        ['p(95)<500', 'p(99)<1000'],
      ])
    ),
  },
};

export default function () {
  const res = http.get(TARGET);
  check(res, {
    'status is 200': (r) => r.status === 200,
  });
}
