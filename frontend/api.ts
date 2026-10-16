import type { View } from './types';
interface Backend {
  GetState(): Promise<View>;
  Scan(): Promise<void>; CancelScan(): Promise<void>; SetAuto(enabled: boolean): Promise<void>;
  ReloadConfig(): Promise<void>; OpenConfig(): Promise<void>;
  TrustHost(ip: string, fingerprint: string): Promise<void>;
  StartTunnel(ip: string, port: number): Promise<void>; StopTunnel(): Promise<void>;
  ExportReport(): Promise<string>;
}
declare global { interface Window { go?: { main?: { App?: Backend } } } }
export function backend(): Backend {
  const api = window.go?.main?.App;
  if (!api) throw new Error('Интерфейс доступен только в приложении ScanSSH-ray.exe. Запустите приложение через scripts/dev.ps1 или готовый .exe.');
  return api;
}
