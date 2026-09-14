// Baseline: the same load shape as throughput.js, aimed straight at the origin
// with no tunnel in the path.
//
//   k6 run -e TARGET=http://localhost:8080/ tests/k6/baseline.js
//
// Every tunnel number should be reported as a delta against this. "3200 RPS,
// p99 24ms" on its own is unverifiable; "p99 +8ms and 62% of direct
// throughput" is a claim a reader can evaluate. Run both from the same
// machine, in the same session, or the comparison is worthless.

import http from 'k6/http';
import { check } from 'k6';

const TARGET = __ENV.TARGET || 'http://localhost:8080/';
const STAGE_DURATION = __ENV.STAGE_DURATION || '30s';
const RATES = (__ENV.RATES || '50,100,200,400,800').split(',').map(Number);

export const options = {
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
        preAllocatedVUs: Math.max(50, rate),
        maxVUs: Math.max(200, rate * 4),
        startTime: `${i * parseInt(STAGE_DURATION)}s`,
        tags: { rate: String(rate) },
      },
    ])
  ),

  thresholds: {
    http_req_failed: ['rate<0.01'],
  },
};

export default function () {
  const res = http.get(TARGET);
  check(res, {
    'status is 200': (r) => r.status === 200,
  });
}
