import type { Host } from './types';

export const fleetFilters = [
  { id: 'turbo', name: 'Turbo' },
  { id: 'all', name: 'Все' },
  { id: 'online', name: 'С интернетом' },
  { id: 'limited', name: 'Без интернета' },
  { id: 'errors', name: 'Ошибки' },
] as const;

export type FleetFilter = typeof fleetFilters[number]['id'];
export const defaultFilter: FleetFilter = 'turbo';

export const isError = (host: Host) => !['online', 'limited', 'unknown'].includes(host.status);
const ready = (host: Host) => host.status === 'online' && host.ssh && host.internet;
const health = (host: Host) => ready(host) ? 0 : host.ssh ? 1 : host.status === 'unknown' ? 3 : 2;
const latency = (host: Host) => host.checkedAt && Number.isFinite(host.latencyMs) && host.latencyMs >= 0 ? host.latencyMs : Infinity;
const addresses = new Intl.Collator('en', { numeric: true });

export function filterHosts(hosts: readonly Host[], filter: FleetFilter, query: string): Host[] {
  return hosts.filter(host => {
    if (!host.ip.includes(query.trim())) return false;
    if (filter === 'turbo' || filter === 'online') return ready(host);
    if (filter === 'errors') return isError(host);
    return filter === 'all' || host.status === filter;
  }).sort((a, b) => health(a) - health(b) || latency(a) - latency(b) || addresses.compare(a.ip, b.ip));
}

export function resolveSelection(hosts: readonly Host[], selectedIP: string): string {
  return hosts.some(host => host.ip === selectedIP) ? selectedIP : hosts[0]?.ip || '';
}
