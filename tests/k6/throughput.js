// Data-plane load test: HTTP request throughput through an established tunnel.
//
// This measures the request path only. Bring the tunnel up first (with the
// CLI or ./cmd/loadtest) and point TARGET at the resulting public URL.
//
//   k6 run -e TARGET=https://<subdomain>.example.com/ tests/k6/throughput.js
//
// Run the same script against the origin directly to get the baseline that
// makes these numbers mean something - see baseline.js.
//
// Note the executor: constant-arrival-rate is an OPEN model, so k6 keeps
// offering the configured rate whether or not the server keeps up. A
// closed model (fixed VUs, each waiting for its response) would quietly
// reduce offered load as latency climbed, hiding the saturation point and
// reporting optimistic percentiles - coordinated omission.

import http from 'k6/http';
import { check } from 'k6';

const TARGET = __ENV.TARGET;
const STAGE_DURATION = __ENV.STAGE_DURATION || '30s';

if (!TARGET) {
  throw new Error('TARGET is required, e.g. -e TARGET=https://abc123.example.com/');
}

// Step through arrival rates so the knee is visible rather than inferred from
// a single data point.
const RATES = (__ENV.RATES || '50,100,200,400,800').split(',').map(Number);

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
        startTime: `${i * parseInt(STAGE_DURATION)}s`,
        tags: { rate: String(rate) },
      },
    ])
  ),

  thresholds: {
    // Declared up front so a run has a verdict instead of a vibe.
    http_req_failed: ['rate<0.01'],
    http_req_duration: ['p(95)<500', 'p(99)<1000'],
  },
};

export default function () {
  const res = http.get(TARGET);
  check(res, {
    'status is 200': (r) => r.status === 200,
  });
}
