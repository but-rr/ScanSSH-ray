import { test } from 'node:test';
import assert from 'node:assert/strict';
import { defaultFilter, fleetFilters, filterHosts, resolveSelection } from '../fleet.ts';

function host(ip, status, latencyMs, extra = {}) {
  return {
    ip, status, latencyMs, message: '', ssh: ['online', 'limited'].includes(status),
    internet: status === 'online', httpCode: status === 'online' ? 200 : 0,
    checkedAt: status === 'unknown' ? null : '2026-10-19T09:00:00Z',
    internetCheckedAt: null, keyPath: '', user: 'admin', ...extra,
  };
}
const ips = hosts => hosts.map(host => host.ip);

test('Turbo is the first and default filter; unchecked filter is removed', () => {
  assert.equal(defaultFilter, 'turbo');
  assert.deepEqual(fleetFilters.map(filter => filter.id), ['turbo', 'all', 'online', 'limited', 'errors']);
});

test('Turbo requires working SSH and internet and sorts before pagination without mutating the snapshot', () => {
  const hosts = [
    host('10.0.0.20', 'unknown', 0), host('10.0.0.21', 'limited', 1),
    host('10.0.0.22', 'auth_error', 2),
    ...Array.from({ length: 12 }, (_, i) => host(`10.0.0.${12 - i}`, 'online', 120 - i * 10)),
    host('10.0.0.23', 'online', 0, { ssh: false }),
    host('10.0.0.24', 'online', 0, { internet: false }),
  ];
  const original = ips(hosts);
  const found = filterHosts(hosts, defaultFilter, '');
  assert.deepEqual(ips(found), Array.from({ length: 12 }, (_, i) => `10.0.0.${i + 1}`));
  assert.deepEqual(ips(found.slice(0, 10)), Array.from({ length: 10 }, (_, i) => `10.0.0.${i + 1}`));
  assert.deepEqual(ips(hosts), original);
});

test('All prioritizes internet, then working SSH, checked failures and unchecked PCs', () => {
  const hosts = [
    host('10.0.0.1', 'unknown', 0), host('10.0.0.2', 'unreachable', 3),
    host('10.0.0.3', 'limited', 2), host('10.0.0.4', 'online', 200),
    host('10.0.0.5', 'online', 20), host('10.0.0.6', 'config_error', 1, { ssh: true }),
  ];
  assert.deepEqual(ips(filterHosts(hosts, 'all', '')), ['10.0.0.5', '10.0.0.4', '10.0.0.6', '10.0.0.3', '10.0.0.2', '10.0.0.1']);
  assert.deepEqual(ips(filterHosts(hosts, 'online', '')), ['10.0.0.5', '10.0.0.4']);
  assert.deepEqual(ips(filterHosts(hosts, 'limited', '')), ['10.0.0.3']);
  assert.deepEqual(ips(filterHosts(hosts, 'errors', '')), ['10.0.0.6', '10.0.0.2']);
});

test('Missing and invalid measurements sort last; zero is valid and tied IPs sort numerically', () => {
  const hosts = [
    host('10.0.0.10', 'online', 5), host('10.0.0.2', 'online', 5),
    host('10.0.0.4', 'online', 0), host('10.0.0.1', 'online', 0, { checkedAt: null }),
    host('10.0.0.5', 'online', NaN), host('10.0.0.6', 'online', -1),
  ];
  assert.deepEqual(ips(filterHosts(hosts, 'turbo', '')), ['10.0.0.4', '10.0.0.2', '10.0.0.10', '10.0.0.1', '10.0.0.5', '10.0.0.6']);
  assert.deepEqual(ips(filterHosts(hosts.reverse(), 'turbo', '')), ['10.0.0.4', '10.0.0.2', '10.0.0.10', '10.0.0.1', '10.0.0.5', '10.0.0.6']);
});

test('Search works with Turbo and selection follows availability while preserving a valid user choice', () => {
  const first = [host('10.0.0.1', 'online', 10), host('10.0.0.2', 'online', 20)];
  assert.equal(resolveSelection(filterHosts(first, 'turbo', ''), ''), '10.0.0.1');
  assert.equal(resolveSelection(filterHosts(first, 'turbo', ''), '10.0.0.2'), '10.0.0.2');
  const next = [host('10.0.0.1', 'limited', 10), host('10.0.0.2', 'online', 20), host('10.0.0.3', 'online', 1)];
  assert.deepEqual(ips(filterHosts(next, 'turbo', '')), ['10.0.0.3', '10.0.0.2']);
  assert.equal(resolveSelection(filterHosts(next, 'turbo', ''), '10.0.0.1'), '10.0.0.3');
  assert.equal(resolveSelection(filterHosts(next, 'turbo', ''), '10.0.0.2'), '10.0.0.2');
  assert.deepEqual(ips(filterHosts(next, 'turbo', '  0.2  ')), ['10.0.0.2']);
  assert.equal(resolveSelection(filterHosts(next, 'turbo', 'not-an-ip'), '10.0.0.2'), '');
  assert.equal(resolveSelection(filterHosts([host('10.0.0.1', 'unknown', 0)], 'turbo', ''), ''), '');
});
