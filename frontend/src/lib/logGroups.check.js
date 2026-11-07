// Run: node frontend/src/lib/logGroups.check.js
import assert from 'node:assert/strict';
import { groupLogEntries, logGroupKey } from './logGroups.js';

assert.equal(logGroupKey('2026/10/30 21:56:19 GET /api/status (48.042µs)'), 'GET /api/status');
assert.equal(logGroupKey('GET /api/progress (205.334µs)'), 'GET /api/progress');
assert.equal(logGroupKey('[system] Switched to project: X'), '');

const groups = groupLogEntries([
  { time: '1', msg: '2026/10/30 21:56:19 GET /api/status (1µs)' },
  { time: '2', msg: '2026/10/30 21:56:21 GET /api/status (2µs)' },
  { time: '3', msg: '[system] hello' },
  { time: '4', msg: '2026/10/30 21:56:25 GET /api/status (3µs)' },
]);
assert.equal(groups.length, 3);
assert.equal(groups[0].key, 'GET /api/status');
assert.equal(groups[0].entries.length, 2);
assert.equal(groups[0].time, '2');
assert.equal(groups[1].key, '');
assert.equal(groups[1].entries.length, 1);
// A different entry breaks the run: the next request starts a new group.
assert.equal(groups[2].entries.length, 1);

assert.deepEqual(groupLogEntries([]), []);
assert.deepEqual(groupLogEntries(null), []);

console.log('logGroups.check.js: ok');
