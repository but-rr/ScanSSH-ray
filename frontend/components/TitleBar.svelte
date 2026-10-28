<script lang="ts">
  import { onMount } from 'svelte';
  import Icon from './Icon.svelte';
  import appIcon from '../../build/appicon.png';
  import { version } from '../package.json';
  import { windowRuntime } from '../window';
  import { backend } from '../api';

  export let connected = false;
  export let loading = true;
  export let onWindowError: (message: string) => void = () => {};
  let available = false;
  let maximised = false;
  let focused = true;

  onMount(() => {
    let disposed = false;
    let refreshTimer: ReturnType<typeof setTimeout>;
    available = !!windowRuntime();
    focused = document.hasFocus();
    const syncWindow = async () => {
      try {
        const state = await windowRuntime()?.WindowIsMaximised();
        if (!disposed && state !== undefined) maximised = state;
      } catch { /* The runtime may disconnect while the window is closing. */ }
    };
    const resize = () => {
      clearTimeout(refreshTimer);
      refreshTimer = setTimeout(() => void syncWindow(), 80);
    };
    const focus = () => { focused = true; void syncWindow(); };
    const blur = () => { focused = false; };
    window.addEventListener('resize', resize);
    window.addEventListener('focus', focus);
    window.addEventListener('blur', blur);
    void syncWindow();
    return () => {
      disposed = true;
      clearTimeout(refreshTimer);
      window.removeEventListener('resize', resize);
      window.removeEventListener('focus', focus);
      window.removeEventListener('blur', blur);
    };
  });

  function toggleMaximise() { windowRuntime()?.WindowToggleMaximise(); }
  async function hideToTray() {
    try { await backend().HideToTray(); }
    catch (error) { onWindowError(String(error)); }
  }
  function doubleClick(event: MouseEvent) {
    if (event.button === 0 && !(event.target as HTMLElement).closest('button')) toggleMaximise();
  }
</script>

<!-- svelte-ignore a11y_no_static_element_interactions (Native titlebar double-click; a named button provides the same action for keyboard users.) -->
<header class="window-titlebar" class:window-inactive={!focused} aria-label="Панель окна ScanSSH-ray" ondblclick={doubleClick}>
  <div class="window-brand"><img class="window-app-icon" src={appIcon} alt="" draggable="false"/><span class="brand-name">ScanSSH<span class="brand-suffix">-ray</span></span><span class="version">v{version}</span></div>
  <div class="window-drag-space"></div>
  <div class="app-state"><span class="dot" class:ok={connected}></span>{connected ? 'Локальный движок подключён' : loading ? 'Подключение к движку…' : 'Нет соединения с движком'}</div>
  <div class="window-controls" role="group" aria-label="Управление окном">
    <button class="window-button" disabled={!available} aria-label="Свернуть окно" title="Свернуть в трей" onclick={hideToTray}><Icon name="minimise" size={15}/></button>
    <button class="window-button" disabled={!available} aria-label={maximised ? 'Восстановить окно' : 'Развернуть окно'} title={maximised ? 'Восстановить' : 'Развернуть'} onclick={toggleMaximise}><Icon name={maximised ? 'restore' : 'maximise'} size={14}/></button>
    <button class="window-button window-close" disabled={!available} aria-label="Закрыть окно в трей" title="Закрыть окно в трей" onclick={hideToTray}><Icon name="x" size={16}/></button>
  </div>
</header>
