// Wails injects the runtime before the application loads, including in builds
// that skip generated bindings. Keep native window controls separate from SSH.
export interface WindowRuntime {
  WindowToggleMaximise(): void;
  WindowIsMaximised(): Promise<boolean>;
}

declare global { interface Window { runtime?: WindowRuntime } }

export function windowRuntime(): WindowRuntime | undefined {
  return window.runtime;
}
