// M3: "A 10k-task board renders under 100 ms p95 under load."
//
// Run it with scripts/loadtest/run.sh, which seeds the fixture first.
//
// What is measured, and why these five:
//
//   board     the endpoint the kanban page calls. The milestone names it.
//   list      the same data through the list view's own request shape.
//   search    full-text over the tsvector column, the thing the GIN index and
//             the cursor pagination were built for.
//   workload  fans out per person; named in the backlog as a plausible
//             surprise, so it is measured rather than assumed.
//   schedule  the timeline's dependency and critical-path walk, the other
//             named surprise.
//
// Each gets its own trend metric, because one aggregate p95 across five
// endpoints of different weights says nothing about any of them: a fast search
// would hide a slow board inside the same number.

import http from 'k6/http';
import { check, group, sleep } from 'k6';
import { Trend } from 'k6/metrics';

const BASE = __ENV.BASE_URL || 'https://proxy';
const PROJECT_ID = __ENV.PROJECT_ID || '11111111-1111-4111-8111-111111111111';
const USERNAME = __ENV.ADMIN_USER || 'admin';
const PASSWORD = __ENV.ADMIN_PASS || 'ChangeMe123!';

// "unbounded" measures what the application ships today: the board asks for
// every task in the project. "paginated" measures the same page fetched a
// column at a time through the endpoint that already supports it, which is the
// shape the fix would take. Running both is what turns a recommendation into
// evidence.
const PROFILE = __ENV.PROFILE || 'unbounded';
const PAGE_SIZE = Number(__ENV.PAGE_SIZE || 100);
// The fixture's project uses these four; a real project's columns come from
// project_statuses and the board asks for whichever it has.
const COLUMNS = ['todo', 'in_progress', 'review', 'done'];
const THINK_SECONDS = Number(__ENV.THINK_SECONDS || 1);
const VUS = Number(__ENV.VUS || 10);

const board = new Trend('ep_board', true);
const list = new Trend('ep_list', true);
const search = new Trend('ep_search', true);
const workload = new Trend('ep_workload', true);
const schedule = new Trend('ep_schedule', true);

export const options = {
  // A closed model with a fixed number of users rather than a fixed arrival
  // rate: this is an internal tool behind a login, so the realistic load is a
  // known number of people clicking, not an open firehose.
  stages: [
    { duration: '15s', target: VUS },
    { duration: '45s', target: VUS },
    { duration: '10s', target: 0 }
  ],
  thresholds: {
    // The milestone, stated as a machine-checkable condition. The run fails
    // rather than merely reporting numbers somebody has to interpret.
    ep_board: ['p(95)<100'],
    ep_list: ['p(95)<100'],
    ep_search: ['p(95)<100'],
    http_req_failed: ['rate<0.01']
  },
  // The stack serves a self-signed certificate by default.
  insecureSkipTLSVerify: true,
  // The proxy rate-limits by client address and every virtual user shares one
  // here, so a 429 would be an artefact of the test rather than a finding.
  // The API zone allows 100r/s with a burst of 200; ten users well inside that.
  noConnectionReuse: false
};

// Signing in once and sharing the token is deliberate. The login endpoint is
// rate-limited to 10 requests a minute on purpose, so a per-iteration login
// would measure the limiter rather than the board.
export function setup() {
  const res = http.post(
    `${BASE}/api/auth/login`,
    JSON.stringify({ username: USERNAME, password: PASSWORD }),
    { headers: { 'Content-Type': 'application/json' } }
  );
  if (res.status !== 200) {
    throw new Error(`login failed: ${res.status} ${res.body}`);
  }
  return { token: res.json('token') };
}

// Search terms drawn from the vocabulary the fixture generates, so every query
// matches something. A term that matches nothing is answered by the index
// immediately and would flatter the measurement.
const TERMS = ['migrate', 'refactor', 'audit', 'kanban', 'timeline', 'billing', 'deploy'];

export default function (data) {
  const params = {
    headers: { Authorization: `Bearer ${data.token}` },
    // Tagged so a slow endpoint is attributable; without this every request
    // lands in one bucket named by URL, and the ids make those unbounded.
    tags: { name: 'api' }
  };

  group('board', () => {
    if (PROFILE === 'paginated') {
      // Exactly what the board issues on first paint since it moved off the
      // unbounded listing: one page per kanban column, plus the counts that let
      // each column say "100 of 3,412".
      //
      // The slowest request is what the page waits on, because a browser issues
      // them in parallel - summing them would describe a client nobody writes.
      let slowest = 0;
      const requests = COLUMNS.map(
        (status) =>
          `${BASE}/api/tasks?projectId=${PROJECT_ID}&status=${status}` +
          `&topLevel=true&limit=${PAGE_SIZE}&total=true&sort=position`
      );
      requests.push(`${BASE}/api/projects/${PROJECT_ID}/tasks/counts?topLevel=true`);

      for (const url of requests) {
        const res = http.get(url, params);
        slowest = Math.max(slowest, res.timings.duration);
        check(res, { 'column 200': (r) => r.status === 200 });
      }
      board.add(slowest);
      return;
    }

    const res = http.get(`${BASE}/api/projects/${PROJECT_ID}/tasks`, params);
    board.add(res.timings.duration);
    check(res, { 'board 200': (r) => r.status === 200 });
  });

  group('list', () => {
    // The list view reads the same collection; measured separately because it
    // is a distinct page and would be the first place a regression shows.
    const url =
      PROFILE === 'paginated'
        ? `${BASE}/api/tasks?projectId=${PROJECT_ID}&topLevel=true&limit=${PAGE_SIZE}&total=true&sort=position`
        : `${BASE}/api/projects/${PROJECT_ID}/tasks`;
    const res = http.get(url, params);
    list.add(res.timings.duration);
    check(res, { 'list 200': (r) => r.status === 200 });
  });

  group('search', () => {
    const term = TERMS[Math.floor(Math.random() * TERMS.length)];
    const res = http.get(`${BASE}/api/tasks?q=${term}&limit=50`, params);
    search.add(res.timings.duration);
    check(res, {
      'search 200': (r) => r.status === 200,
      'search matched something': (r) => (r.json('items') || []).length > 0
    });
  });

  group('workload', () => {
    const res = http.get(`${BASE}/api/users/workload`, params);
    workload.add(res.timings.duration);
    check(res, { 'workload 200': (r) => r.status === 200 });
  });

  group('schedule', () => {
    // Scoped to the bars on screen in the paginated profile, which is what the
    // timeline now sends: an arrow needs two visible bars, so edges with one
    // end off-screen are weight and nothing else. The unbounded profile keeps
    // asking for the whole graph, which is what it is there to measure.
    let url = `${BASE}/api/projects/${PROJECT_ID}/schedule`;
    if (PROFILE === 'paginated') {
      const page = http.get(
        `${BASE}/api/tasks?projectId=${PROJECT_ID}&topLevel=true&limit=${PAGE_SIZE}&sort=position`,
        params
      );
      const ids = (page.json('items') || []).map((task) => task.id);
      if (ids.length) url += '?' + ids.map((id) => `taskId=${id}`).join('&');
    }
    const res = http.get(url, params);
    schedule.add(res.timings.duration);
    check(res, { 'schedule 200': (r) => r.status === 200 });
  });

  // Think time, and it is not decoration.
  //
  // Without it a virtual user issues its next request the instant the last one
  // returns, so ten of them are not ten people - they are ten bots at whatever
  // rate the server can be pushed to. In a closed model that also makes the
  // two profiles incomparable: the faster one simply generates more load and
  // re-saturates the server, hiding the improvement it was meant to show.
  // One second per cycle is still busier than any real board user.
  sleep(THINK_SECONDS);
}
