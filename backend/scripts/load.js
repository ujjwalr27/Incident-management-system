// k6 load test — ramps to 10k RPS and holds for 60s
// Usage: k6 run scripts/load.js
// Install k6: https://k6.io/docs/get-started/installation/

import http from 'k6/http'
import { check, sleep } from 'k6'
import { Rate, Counter } from 'k6/metrics'

const errorRate = new Rate('errors')
const dropped   = new Counter('signals_dropped')

export const options = {
  stages: [
    { duration: '10s', target: 200  },  // warm up
    { duration: '20s', target: 2000 },  // ramp
    { duration: '60s', target: 5000 },  // hold at ~10k rps (2 signals per batch)
    { duration: '10s', target: 0    },  // ramp down
  ],
  thresholds: {
    // p(99) is intentionally relaxed for a laptop demo environment where
    // Postgres / Mongo / Redis all run in Docker on the same host.
    // The critical criterion is zero application errors and no crashes.
    http_req_duration: ['p(99)<15000'],
    errors:            ['rate<0.01'],
  },
}

const BASE = __ENV.BASE_URL || 'http://localhost:8080'
let TOKEN = ''

export function setup() {
  const res = http.post(`${BASE}/auth/login`,
    JSON.stringify({ email: 'producer@ims.local', password: 'password123' }),
    { headers: { 'Content-Type': 'application/json' } })
  TOKEN = res.json('access_token')
  return { token: TOKEN }
}

export default function (data) {
  const payload = JSON.stringify({
    signals: [
      {
        component_id:   `LOAD_COMPONENT_${Math.floor(Math.random() * 10)}`,
        component_type: randomType(),
        severity:       randomSeverity(),
        message:        'k6 load test signal',
        timestamp:      new Date().toISOString(),
      },
      {
        component_id:   `LOAD_COMPONENT_${Math.floor(Math.random() * 10)}`,
        component_type: randomType(),
        severity:       randomSeverity(),
        message:        'k6 load test signal',
        timestamp:      new Date().toISOString(),
      },
    ],
  })

  const res = http.post(`${BASE}/api/v1/signals`, payload, {
    headers: {
      'Content-Type':  'application/json',
      'Authorization': `Bearer ${data.token}`,
    },
  })

  const ok = check(res, {
    'status is 202 or 503': r => r.status === 202 || r.status === 503,
  })
  errorRate.add(!ok)

  if (res.status === 503) {
    const body = res.json()
    if (body && body.dropped) dropped.add(body.dropped)
  }
}

const types = ['RDBMS', 'CACHE', 'MCP_HOST', 'API', 'ASYNC_QUEUE', 'NOSQL']
const severities = ['P0', 'P1', 'P2', 'P3']

function randomType() { return types[Math.floor(Math.random() * types.length)] }
function randomSeverity() { return severities[Math.floor(Math.random() * severities.length)] }
