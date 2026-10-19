export interface Host {
  ip: string; status: string; message: string; ssh: boolean; internet: boolean;
  httpCode: number; checkedAt: string | null; internetCheckedAt: string | null;
  latencyMs: number; keyPath: string; user: string;
}
export interface PublicConfig {
  envPath: string; ranges: string; user: string; sshPort: number; keyPath: string;
  keyRules: number; knownHosts: string; proxyHost: string; proxyPort: number;
  proxyAuth: string; proxyCredentialsSet: boolean; localPort: number; testUrl: string;
  sshInterval: number; internetInterval: number; concurrency: number;
}
export interface Monitor {
  config: PublicConfig; hosts: Host[]; pools: {label: string; ips: string[]}[];
  events: {at: string; message: string; kind: string}[];
  pending: {ip: string; fingerprint: string; algorithm: string; changed: boolean}[];
  scanning: boolean; scanIP: string; auto: boolean; lastScan: string | null;
}
export interface Tunnel { running: boolean; ip: string; localAddress: string; target: string; startedAt: string | null; error: string }
export interface View { monitor: Monitor | null; tunnel: Tunnel; error: string; envPath: string }
