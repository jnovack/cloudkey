// dashboard_test.js — regression tests for website/dashboard.html's pure
// helper functions and the SSE onerror auto-reconnect fix (DASH-LEAK-01),
// run directly under plain Node (no npm packages, stdlib only).
//
// Run with: node website/dashboard_test.js
'use strict';

const fs = require('fs');
const vm = require('vm');
const path = require('path');
const assert = require('assert');

const html = fs.readFileSync(path.join(__dirname, 'dashboard.html'), 'utf8');
const match = html.match(/<script>([\s\S]*?)<\/script>/);
if (!match) throw new Error('could not find <script> block in dashboard.html');
const src = match[1];

// Minimal EventSource stub so connectSSE() can run under Node (no DOM/XHR
// available). Captures the onerror handler the script installs so the
// DASH-LEAK-01 regression test can invoke it directly.
let lastEventSource = null;
function FakeEventSource(url) {
  this.url = url;
  this.closed = false;
  lastEventSource = this;
}
FakeEventSource.prototype.close = function () {
  this.closed = true;
};

const sandbox = {
  module: { exports: {} },
  console,
  EventSource: FakeEventSource,
  window: { location: { protocol: 'http:', hostname: 'nebula-01' } },
};
sandbox.exports = sandbox.module.exports;
vm.createContext(sandbox);
vm.runInContext(src, sandbox, { filename: 'dashboard.html' });
const h = sandbox.module.exports;

let failures = 0;
function check(name, actual, expected) {
  // JSON-compare rather than assert.deepStrictEqual: objects returned from
  // code run via vm.runInContext belong to the sandbox's own realm, so their
  // Object.prototype differs from this file's — deepStrictEqual treats that
  // as unequal even when every own-property value matches.
  const a = JSON.stringify(actual);
  const e = JSON.stringify(expected);
  if (a !== e) {
    failures++;
    console.error('FAIL', name, '- got', a, 'want', e);
  }
}

/* ---------- fmtBytes ---------- */

// Unit-boundary crossing: 999 stays bytes, 1000 rolls over to KB.
check('fmtBytes(999) stays B', h.fmtBytes(999), [{ v: '999', u: 'B' }]);
check('fmtBytes(1000) rolls to KB', h.fmtBytes(1000), [{ v: '1.00', u: 'KB' }]);
// Precision-digit boundaries: >=100 -> 0 decimals, >=10 -> 1 decimal, else 2.
check('fmtBytes(150) -> 0 decimals', h.fmtBytes(150), [{ v: '150', u: 'B' }]);
check('fmtBytes(15) -> 1 decimal', h.fmtBytes(15), [{ v: '15.0', u: 'B' }]);
check('fmtBytes(1.5) -> 2 decimals', h.fmtBytes(1.5), [{ v: '1.50', u: 'B' }]);
check('fmtBytes(non-numeric) treats as 0', h.fmtBytes('nope'), [{ v: '0.00', u: 'B' }]);

/* ---------- dur ---------- */

check('dur(0) renders 0m 00s', h.dur(0), [{ v: 0, u: 'm' }, { v: '00', u: 's' }]);
check('dur multi-day', h.dur(3 * 86400 + 6 * 3600), [{ v: 3, u: 'd' }, { v: '06', u: 'h' }]);
// Negative input must clamp to 0, not go negative or throw.
check('dur(-5) clamps to 0', h.dur(-5), [{ v: 0, u: 'm' }, { v: '00', u: 's' }]);

/* ---------- pad ---------- */

check('pad(5) zero-pads', h.pad(5), '05');
check('pad(12) unchanged', h.pad(12), '12');

/* ---------- pctOf ---------- */

// total=0 must not divide by zero — pctOf guards via Math.max(1, total), so
// used=0/total=0 lands on 0, not NaN/Infinity.
const zeroTotalPct = h.pctOf({ used: 0, total: 0 });
if (!Number.isFinite(zeroTotalPct)) {
  failures++;
  console.error('FAIL pctOf(total=0) is not finite:', zeroTotalPct);
}
check('pctOf(total=0) does not divide by zero', zeroTotalPct, 0);
check('pctOf normal case', h.pctOf({ used: 25, total: 100 }), 25);

/* ---------- cpuPct ---------- */

check('cpuPct normal case', h.cpuPct({ cores: 4, loadAvg: 2 }), 50);
check('cpuPct clamps at 100', h.cpuPct({ cores: 1, loadAvg: 5 }), 100);
check('cpuPct floors at 0', h.cpuPct({ cores: 4, loadAvg: -1 }), 0);

/* ---------- isFullSnapshot ---------- */

const fullKeys = { host: {}, net: {}, cpu: {}, mem: {}, ssd: {}, rootfs: {}, sdcard: {}, apps: [], tunnels: [] };
check('isFullSnapshot true when all 9 keys present', h.isFullSnapshot(fullKeys), true);
const missingOne = Object.assign({}, fullKeys);
delete missingOne.sdcard;
check('isFullSnapshot false when a key is missing', h.isFullSnapshot(missingOne), false);
check('isFullSnapshot false for null', h.isFullSnapshot(null), false);

/* ---------- tunnelKey / mergePatch ---------- */

check('tunnelKey joins type+name', h.tunnelKey({ type: 'WIREGUARD', name: 'wg0' }), 'WIREGUARD\u0000wg0');

// A tunnels patch upserts by type+name without duplicating an existing entry.
const prev = { tunnels: [{ type: 'WIREGUARD', name: 'wg0', rx: 100 }] };
const upsertPatch = { tunnels: [{ type: 'WIREGUARD', name: 'wg0', rx: 200 }] };
const afterUpsert = h.mergePatch(prev, upsertPatch);
check('mergePatch upserts same type+name without duplicating', afterUpsert.tunnels.length, 1);
check('mergePatch upsert carries new value', afterUpsert.tunnels[0].rx, 200);

// A different name appends rather than replacing.
const appendPatch = { tunnels: [{ type: 'TAILSCALE', name: 'ts0', rx: 5 }] };
const afterAppend = h.mergePatch(afterUpsert, appendPatch);
check('mergePatch appends a distinct type+name', afterAppend.tunnels.length, 2);

// A non-tunnels key patch replaces wholesale.
const afterHostPatch = h.mergePatch({ host: { name: 'old' } }, { host: { name: 'new' } });
check('mergePatch replaces non-tunnels key wholesale', afterHostPatch.host, { name: 'new' });

/* ---------- DASH-LEAK-01: SSE onerror must not close the stream ---------- */

// connectSSE() is only auto-invoked in a browser (see the `typeof document`
// guard in dashboard.html); call it directly here.
h.connectSSE();

assert.ok(lastEventSource, 'connectSSE() should have constructed an EventSource');
assert.strictEqual(typeof lastEventSource.onmessage, 'function', 'onmessage handler should be installed');
assert.strictEqual(typeof lastEventSource.onerror, 'function', 'onerror handler should be installed');

// onmessage/onerror both call render()/renderFooter(), which touch `document`
// — deliberately absent in this Node sandbox (see the `typeof document`
// guard in dashboard.html) so the auto-run block never fires here. Each
// handler sets its state variable BEFORE touching the DOM, so the resulting
// ReferenceError is expected and ignored; only DOM access should fail, not
// the state assignment under test.
function callIgnoringMissingDocument(fn) {
  try {
    fn();
  } catch (err) {
    if (!/document is not defined/.test(err.message)) throw err;
  }
}

// A message first flips `connected` true, mirroring a live stream.
callIgnoringMissingDocument(function () {
  lastEventSource.onmessage({ data: JSON.stringify({ cpu: { pct: 10 } }) });
});
check('connected is true after a message', h.isConnected(), true);

// The fix's entire point: firing onerror must NOT call es.close() (which
// would permanently disable the browser's native auto-reconnect), and must
// flip `connected` back to false so the footer stops claiming "LIVE".
callIgnoringMissingDocument(function () {
  lastEventSource.onerror();
});
check('onerror does not close the EventSource (leaves auto-reconnect intact)', lastEventSource.closed, false);
check('onerror sets connected back to false', h.isConnected(), false);

if (failures > 0) {
  console.error(failures + ' assertion(s) failed');
  process.exit(1);
}
console.log('all dashboard.html tests passed');
