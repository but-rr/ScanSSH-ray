<script lang="ts">
  import { onMount } from 'svelte';
  import Icon from '../components/Icon.svelte';
  import TitleBar from '../components/TitleBar.svelte';
  import appIcon from '../../build/appicon.png';
  import { backend } from '../api';
  import type { View, Host } from '../types';
  import { defaultFilter, fleetFilters, filterHosts, isError, resolveSelection, type FleetFilter } from '../fleet';

  let view: View | null = null;
  let connectionError = '';
  let selectedIP = '';
  let query = '';
  let selectedPool = '';
  let filter: FleetFilter = defaultFilter;
  let page = 0;
  const perPage = 10;
  let modal = '';
  let busy = false;
  let tunnelConnecting = false;
  let tunnelConnectingIP = '';
  let tunnelCancelRequested = false;
  let port = 3128;
  let configPort = 0;
  let message = '';
  let messageError = false;
  let passwordHost: { ip: string; sshUser: string } | null = null;
  let administratorUser = '';
  let administratorPassword = '';
  let repeatedPassword = '';
  let passwordError = '';
  let toastTimer: ReturnType<typeof setTimeout>;
  const labels: Record<string, string> = {
    unknown: 'Не проверен', online: 'SSH и интернет доступны', limited: 'Проверка через прокси не прошла',
    ssh_closed: 'Порт SSH недоступен', unreachable: 'ПК недоступен / таймаут', auth_error: 'Ошибка авторизации SSH',
    host_key_changed: 'Ключ сервера изменился',
    host_key_error: 'Ключ сервера отклонён', config_error: 'Проверьте настройки', ssh_error: 'Ошибка соединения SSH',
  };
  const time = (value: string | null | undefined) => value ? new Date(value).toLocaleTimeString('ru-RU') : '—';
  const kind = (h: Host) => h.status === 'online' ? 'ok' : h.status === 'unknown' ? 'offline' : h.status === 'limited' ? 'warn' : 'error';
  $: monitor = view?.monitor;
  $: hosts = monitor?.hosts || [];
  $: selected = hosts.find(h => h.ip === selectedIP);
  $: checkingSelected = !!selected && !!monitor?.scanning && (!monitor.scanIP || monitor.scanIP === selectedIP);
  $: pending = monitor?.pending.find(p => p.ip === selectedIP);
  $: online = hosts.filter(h => h.status === 'online').length;
  $: limited = hosts.filter(h => h.status === 'limited').length;
  $: errors = hosts.filter(isError).length;
  $: unknown = hosts.filter(h => h.status === 'unknown').length;
  $: pool = monitor?.pools.find(p => p.label === selectedPool);
  $: if (selectedPool && monitor && !monitor.pools.some(p => p.label === selectedPool)) selectedPool = '';
  $: found = filterHosts(pool ? hosts.filter(h => pool.ips.includes(h.ip)) : hosts, filter, query);
  $: selectedIP = resolveSelection(found, selectedIP);
  $: maxPage = Math.max(0, Math.ceil(found.length / perPage) - 1);
  $: if (page > maxPage) page = maxPage;
  $: visible = found.slice(page * perPage, (page + 1) * perPage);
  $: cfg = monitor?.config;
  $: tunnelPending = tunnelConnecting || !!view?.tunnel.connecting;
  $: canTunnel = !!selected?.internet && !!selected?.ssh && !!selected?.internetCheckedAt && Date.now() - new Date(selected.internetCheckedAt).getTime() < ((cfg?.internetInterval || 30) + 10) * 1000;
  $: command = selected && cfg ? `ssh -N -o ExitOnForwardFailure=yes -o StrictHostKeyChecking=accept-new -o UserKnownHostsFile="${cfg.knownHosts}"${selected.keyPath ? ` -i "${selected.keyPath}"` : ''} -p ${cfg.sshPort} -L 127.0.0.1:${port}:${cfg.proxyHost}:${cfg.proxyPort} ${selected.user}@${selected.ip}` : '';
  $: if (modal !== 'password') {
    passwordHost = null; administratorUser = ''; administratorPassword = ''; repeatedPassword = ''; passwordError = '';
  }

  async function refresh() {
    try {
      view = await backend().GetState();
      connectionError = '';
      if (view.monitor && configPort !== view.monitor.config.localPort) { configPort = view.monitor.config.localPort; port = configPort; }
    } catch (error) { connectionError = String(error); }
  }
  function notify(text: string, error = false) {
    message = text; messageError = error;
    clearTimeout(toastTimer); toastTimer = setTimeout(() => message = '', 6500);
  }
  async function run(action: () => Promise<unknown>, success = '') {
    if (busy) return;
    busy = true;
    try { await action(); if (success) notify(success); }
    catch (error) { notify(String(error), true); }
    finally { busy = false; await refresh(); }
  }
  async function toggleTunnel() {
    if (busy) return;
    if (tunnelPending) {
      tunnelCancelRequested = true;
      await run(() => backend().StopTunnel(), 'Подключение туннеля отменено');
      return;
    }
    if (view?.tunnel.running) {
      await run(() => backend().StopTunnel(), 'Туннель остановлен');
      return;
    }
    if (!selected || !canTunnel || !Number.isInteger(port) || port < 1 || port > 65535) return;
    const ip = selected.ip;
    const localPort = port;
    tunnelConnecting = true; tunnelConnectingIP = ip; tunnelCancelRequested = false;
    try {
      await backend().StartTunnel(ip, localPort);
      if (!tunnelCancelRequested) notify('SSH-туннель запущен');
    } catch (error) {
      if (!tunnelCancelRequested) notify(String(error), true);
    } finally {
      tunnelConnecting = false; tunnelConnectingIP = ''; await refresh();
    }
  }
  async function copyCommand() {
    try { await navigator.clipboard.writeText(command); notify('Команда скопирована'); }
    catch { notify('Выделите и скопируйте команду вручную', true); }
  }
  async function copyIP(ip: string) {
    selectedIP = ip;
    try { await navigator.clipboard.writeText(ip); notify(`IP ${ip} скопирован`); }
    catch { notify(`Не удалось скопировать IP ${ip} в буфер обмена`, true); }
  }
  function chooseFilter(next: FleetFilter) { filter = next; page = 0; }
  function choosePool(label: string) { selectedPool = selectedPool === label ? '' : label; page = 0; }
  function openPasswordDialog() {
    if (!selected || busy) return;
    passwordHost = { ip: selected.ip, sshUser: selected.user };
    administratorUser = /[\\/@]/.test(selected.user) ? '' : selected.user;
    administratorPassword = ''; repeatedPassword = ''; passwordError = '';
    modal = 'password';
  }
  async function changeAdministratorPassword() {
    if (!passwordHost || busy) return;
    if (!administratorUser.trim() || administratorUser !== administratorUser.trim() || /[\\/\r\n\0]/.test(administratorUser)) {
      passwordError = 'Укажите имя локальной учётной записи без домена.'; return;
    }
    if (!administratorPassword || administratorPassword !== repeatedPassword) {
      passwordError = 'Введите новый пароль и совпадающее подтверждение.'; return;
    }
    const target = passwordHost.ip;
    const account = administratorUser;
    let secret = administratorPassword;
    administratorPassword = ''; repeatedPassword = ''; passwordError = '';
    busy = true;
    try {
      await backend().SetAdministratorPassword(target, account, secret);
      modal = '';
      notify(`Пароль администратора ${account} на ${target} изменён`);
    } catch (error) { passwordError = String(error); }
    finally { secret = ''; busy = false; await refresh(); }
  }
  function handleKey(event: KeyboardEvent) {
    if (event.key === 'Escape' && modal === 'password' && busy) { event.preventDefault(); return; }
    if (event.key === 'Escape') { modal = ''; return; }
    const target = event.target as HTMLElement;
    if (event.key === '/' && !modal && !event.ctrlKey && !event.metaKey && !event.altKey && !target.closest('input, textarea, select, [contenteditable]')) {
      event.preventDefault(); document.getElementById('host-search')?.focus();
    }
  }
  function focusModal(node: HTMLElement) {
    const previous = document.activeElement as HTMLElement | null;
    const appRoot = document.querySelector<HTMLElement>('.app');
    const previousOverflow = document.body.style.overflow;
    if (appRoot) appRoot.inert = true;
    document.body.style.overflow = 'hidden';
    const controls = () => Array.from(node.querySelectorAll<HTMLElement>('button:not(:disabled), input:not(:disabled), [tabindex="0"]'));
    queueMicrotask(() => controls()[0]?.focus());
    const trap = (event: KeyboardEvent) => {
      if (event.key !== 'Tab') return;
      const items = controls();
      if (event.shiftKey && document.activeElement === items[0]) { event.preventDefault(); items.at(-1)?.focus(); }
      else if (!event.shiftKey && document.activeElement === items.at(-1)) { event.preventDefault(); items[0]?.focus(); }
    };
    node.addEventListener('keydown', trap);
    return { destroy() { node.removeEventListener('keydown', trap); document.body.style.overflow = previousOverflow; if (appRoot) appRoot.inert = false; previous?.focus({ preventScroll: true }); } };
  }
  onMount(() => {
    let stopped = false;
    let timer: ReturnType<typeof setTimeout>;
    const poll = async () => { await refresh(); if (!stopped) timer = setTimeout(poll, 1000); };
    void poll();
    return () => { stopped = true; clearTimeout(timer); clearTimeout(toastTimer); };
  });
</script>

<svelte:window onkeydown={handleKey} />

<TitleBar connected={!!monitor && !connectionError && !view?.error} loading={!view && !connectionError} onWindowError={(error) => notify(error, true)}/>

<div class="app">
  <a class="skip-link" href="#devices">К списку устройств</a>
  <nav class="sidebar" aria-label="Основная навигация">
    <div class="rail-brand" aria-label="ScanSSH-ray"><img src={appIcon} alt="" width="34" height="34" draggable="false"/></div>
    <div class="rail-divider"></div>
    <button class="nav-item" class:active={!modal} aria-label="Мониторинг" aria-current={!modal ? 'page' : undefined} onclick={() => { modal = ''; window.scrollTo({ top: 0, behavior: 'smooth' }); }}><Icon size={21}/><span class="nav-tooltip">Мониторинг</span></button>
    <button class="nav-item" class:active={modal === 'pools'} aria-label="Пулы IP-адресов" aria-haspopup="dialog" onclick={() => modal = 'pools'}><Icon name="server" size={21}/><span class="nav-tooltip">Пулы IP-адресов</span></button>
    <button class="nav-item" class:active={modal === 'journal'} aria-label="Журнал событий" aria-haspopup="dialog" onclick={() => modal = 'journal'}><Icon name="logs" size={21}/><span class="nav-tooltip">Журнал событий</span></button>
    <div class="side-spacer"></div>
    <button class="nav-item" class:active={modal === 'help'} aria-label="Как это работает" aria-haspopup="dialog" onclick={() => modal = 'help'}><Icon name="help" size={21}/><span class="nav-tooltip">Как это работает</span></button>
    <button class="nav-item" class:active={modal === 'settings'} aria-label="Настройки" aria-haspopup="dialog" onclick={() => modal = 'settings'}><Icon name="settings" size={21}/><span class="nav-tooltip">Настройки</span></button>
    <span class="rail-status" class:ready={!!monitor && !connectionError} role="img" aria-label={monitor && !connectionError ? 'Движок подключён' : 'Движок недоступен'} title={monitor && !connectionError ? 'Локальный движок подключён' : 'Ожидание подключения'}></span>
  </nav>

  <main class="main">
    <header class="header"><div><h1>Мониторинг сети</h1><p class="sub">Устройства, доступность и SSH-подключения.</p></div>
      <div class="head-actions">
        <button class="auto-control" role="switch" aria-checked={!!monitor?.auto} aria-label="Автопроверка" disabled={!monitor || busy} onclick={() => run(() => backend().SetAuto(!monitor?.auto))}><span class="switch-track" class:on={monitor?.auto}><span></span></span>Автопроверка</button>
        {#if monitor?.scanning}<button class="btn" disabled={busy} onclick={() => run(() => backend().CancelScan())}><span class="spin"><Icon name="refresh" size={16}/></span>Остановить</button>
        {:else}<button class="btn primary" disabled={!monitor || busy} onclick={() => run(() => backend().Scan())}><Icon name="refresh"/>Проверить все</button>{/if}
      </div>
    </header>

    {#if connectionError || view?.error}
      <div class="notice error-notice" role="alert"><Icon name="alert"/><div><strong>Не удалось загрузить приложение</strong><p>{connectionError || view?.error}</p><div class="inline-actions"><button class="btn mini" disabled={busy} onclick={() => run(() => backend().OpenConfig())}>Открыть .env</button><button class="btn mini" disabled={busy} onclick={() => run(() => backend().ReloadConfig(), 'Конфигурация загружена')}>Перечитать настройки</button></div></div></div>
    {:else if cfg && (!cfg.keyPath && !cfg.keyRules || cfg.proxyAuth === 'basic' && !cfg.proxyCredentialsSet)}
      <div class="notice"><Icon name="settings"/><div><strong>Заполните профиль подключения в .env</strong><div>Укажите пути к SSH-ключам и учётные данные прокси, затем перечитайте настройки.</div></div><button class="btn mini ghost notice-action" onclick={() => modal = 'settings'}>Настроить</button></div>
    {/if}

    <section class="stats" aria-label="Сводка мониторинга">
      <button class="stat" class:chosen={filter === 'all'} aria-pressed={filter === 'all'} onclick={() => chooseFilter('all')}><div class="stat-top"><span class="stat-label">Всего устройств</span><Icon size={18}/></div><div class="stat-value">{monitor ? hosts.length : '—'}<span class="stat-unit">ПК</span></div><div class="stat-foot">{unknown} ещё не проверено</div></button>
      <button class="stat" class:chosen={filter === 'online'} aria-pressed={filter === 'online'} onclick={() => chooseFilter('online')}><div class="stat-top"><span class="stat-label"><span class="dot ok"></span>С интернетом</span><Icon name="globe" size={18}/></div><div class="stat-value">{monitor ? online : '—'}<span class="stat-unit">ПК</span></div><div class="stat-foot">SSH и прокси доступны</div></button>
      <button class="stat" class:chosen={filter === 'limited'} aria-pressed={filter === 'limited'} onclick={() => chooseFilter('limited')}><div class="stat-top"><span class="stat-label"><span class="dot warn"></span>Без интернета</span><Icon name="globe-off" size={18}/></div><div class="stat-value">{monitor ? limited : '—'}<span class="stat-unit">ПК</span></div><div class="stat-foot">Проверка прокси не прошла</div></button>
      <button class="stat" class:chosen={filter === 'errors'} aria-pressed={filter === 'errors'} onclick={() => chooseFilter('errors')}><div class="stat-top"><span class="stat-label"><span class="dot error"></span>Требуют внимания</span><Icon name="alert" size={18}/></div><div class="stat-value">{monitor ? errors : '—'}<span class="stat-unit">ПК</span></div><div class="stat-foot">Ошибки соединения или ключей</div></button>
    </section>

    <div class="content-grid">
      <div class="left-area">
        <details class="card pool-card"><summary class="card-header pool-header"><span class="fleet-title"><Icon name="server" size={17}/><span class="card-title">Пулы адресов</span><span class="count-pill">{monitor?.pools.length || 0}</span></span><span class="pool-summary"><span class:mono={!!selectedPool}>{selectedPool || 'Все диапазоны'}</span><Icon name="down" size={14}/></span></summary>
          <div class="pools">{#each monitor?.pools || [] as pool}
            {@const good = hosts.filter(h => pool.ips.includes(h.ip) && h.status === 'online').length}
            {@const warn = hosts.filter(h => pool.ips.includes(h.ip) && h.status === 'limited').length}
            <button class="pool" class:chosen={selectedPool === pool.label} aria-pressed={selectedPool === pool.label} onclick={() => choosePool(pool.label)}><div class="pool-top"><span class="pool-name mono" title={pool.label}>{pool.label}</span><span class="pool-count">{pool.ips.length} ПК</span></div><div class="pool-visual"><span class="ok" style:width={`${good / (pool.ips.length || 1) * 100}%`}></span><span class="limited" style:width={`${warn / (pool.ips.length || 1) * 100}%`}></span></div><div class="pool-bottom"><span><span class="dot ok"></span>{good} в сети</span><span>{selectedPool === pool.label ? 'Выбран' : 'Выбрать'}<Icon name={selectedPool === pool.label ? 'check' : 'arrow'} size={12}/></span></div></button>
          {:else}<div class="pool-empty">{monitor ? 'Добавьте диапазоны IP в настройках, чтобы начать мониторинг.' : 'Загружаем пулы адресов…'}</div>{/each}</div>
        </details>

        <section class="card fleet-card" id="devices" tabindex="-1"><div class="card-header"><div class="fleet-title"><h2 class="card-title">Устройства</h2><span class="count-pill">{found.length}</span></div><div class="tiny scan-status" class:scanning={monitor?.scanning}>{#if monitor?.scanning}<span class="spin"><Icon name="refresh" size={13}/></span>{/if}{monitor?.scanning ? monitor.scanIP ? `Проверяем ${monitor.scanIP}` : 'Идёт проверка' : monitor?.lastScan ? `Обновлено в ${time(monitor.lastScan)}` : 'Ожидание проверки'}</div></div>
          <div class="toolbar"><div class="search-wrap"><span class="search-icon"><Icon name="search" size={16}/></span><input class="search" id="host-search" type="search" aria-label="Поиск по IP" placeholder="Найти IP-адрес…" bind:value={query} oninput={() => page = 0}/><kbd>/</kbd></div><div class="filters" aria-label="Фильтры устройств">{#each fleetFilters as item}<button class="filter" class:active={filter === item.id} aria-pressed={filter === item.id} onclick={() => chooseFilter(item.id)}>{#if item.id === 'turbo'}<Icon name="bolt" size={13}/>{/if}{item.name}</button>{/each}</div></div>
          {#if selectedPool}<div class="active-pool"><Icon name="server" size={13}/><span class="mono">{selectedPool}</span><button class="text-button" onclick={() => { selectedPool = ''; page = 0; }}>Все пулы <Icon name="x" size={12}/></button></div>{/if}
          <div class="table-wrap"><table aria-label="Устройства"><thead><tr><th>Устройство / IP</th><th>SSH</th><th>Интернет</th><th title="Сначала ПК с интернетом, затем по возрастанию задержки SSH">Отклик SSH ↑</th><th>Проверка</th><th><span class="sr-only">Подробнее</span></th></tr></thead><tbody>
            {#each visible as host (host.ip)}<tr class:selected={selectedIP === host.ip}>
              <td><button class="host-button device-button" title="Скопировать IP и выбрать ПК" aria-label={`Скопировать IP ${host.ip} и выбрать ПК`} onclick={() => copyIP(host.ip)}><span class="device-symbol"><Icon size={17}/></span><span><span class="device-name mono">{host.ip}</span><span class="device-ip">{host.user} · порт {cfg?.sshPort}</span></span></button></td>
              <td><span class="state" title={host.message}><span class={`dot ${kind(host)}`}></span>{host.ssh ? 'Доступен' : host.status === 'unknown' ? '—' : host.status === 'auth_error' ? 'Авторизация' : host.status.startsWith('host_key') ? 'Ключ сервера' : host.status === 'config_error' ? 'Настройки' : 'Недоступен'}</span></td>
              <td><span class="tag" class:warn={host.ssh && !host.internet} class:off={!host.ssh}>{host.internet ? 'Есть' : host.ssh ? 'Не подтверждён' : '—'}</span></td><td><span class="time mono">{host.ssh && host.checkedAt ? `${host.latencyMs} мс` : '—'}</span></td><td><span class="time mono">{time(host.checkedAt)}</span></td><td><button class="icon-button" aria-label={`Выбрать ${host.ip}`} onclick={() => selectedIP = host.ip}><Icon name="arrow" size={15}/></button></td>
            </tr>{:else}<tr><td colspan="6"><div class="empty"><div class="empty-icon"><Icon name={query.trim() ? 'search' : filter === 'turbo' ? 'bolt' : 'monitor'} size={25}/></div><strong>{!monitor ? 'Подключаемся к вашей сети' : query.trim() ? 'Ничего не найдено' : filter === 'turbo' ? 'Здесь появятся готовые к работе ПК' : 'В этом списке пока пусто'}</strong><p>{!monitor ? 'Ожидаем конфигурацию локального движка.' : query.trim() ? 'Попробуйте другой IP-адрес или сбросьте фильтры.' : filter === 'turbo' ? 'Turbo показывает устройства с подтверждённым SSH и интернетом, начиная с самых быстрых.' : 'Выберите другой фильтр или запустите проверку.'}</p>{#if monitor}<button class="btn mini" onclick={() => { chooseFilter('all'); query = ''; selectedPool = ''; }}>Показать все устройства<Icon name="arrow" size={13}/></button>{/if}</div></td></tr>{/each}
          </tbody></table></div>
          <div class="table-footer"><span class="tiny">{found.length ? `${page * perPage + 1}–${Math.min((page + 1) * perPage, found.length)} из ${found.length} устройств` : '0 устройств'}</span><div class="pagination"><button class="page-btn" disabled={page === 0} aria-label="Предыдущая страница" onclick={() => page--}><Icon name="left" size={15}/></button><span class="page-number">{page + 1} / {maxPage + 1}</span><button class="page-btn" disabled={page >= maxPage} aria-label="Следующая страница" onclick={() => page++}><Icon name="arrow" size={15}/></button></div></div>
        </section>

      </div>

      <div class="right-area"><aside class="card detail-card" aria-label="Подключение и выбранное устройство">
        <div class="connection-heading"><h2 class="card-title">Подключение</h2><span class="protocol-label">SSH TUNNEL</span></div>
        <div class="connection-control" class:connected={view?.tunnel.running}>
          <div class="power-orbit"><button class="power-button" class:connected={view?.tunnel.running} class:working={tunnelPending} disabled={busy || (!tunnelPending && !view?.tunnel.running && (!canTunnel || !Number.isInteger(port) || port < 1 || port > 65535))} aria-label={tunnelPending ? 'Отменить подключение' : view?.tunnel.running ? 'Остановить туннель' : 'Перенаправить трафик'} title={tunnelPending ? 'Отменить подключение туннеля' : view?.tunnel.running ? 'Остановить туннель' : canTunnel ? 'Перенаправить трафик через выбранный ПК' : 'Выберите ПК со свежей успешной проверкой SSH и интернета'} onclick={toggleTunnel}><Icon name={tunnelPending ? 'refresh' : 'power'} size={35}/></button></div>
          <strong class="connection-title">{tunnelPending ? 'Подключаем туннель…' : view?.tunnel.running ? 'Туннель подключён' : 'Перенаправить трафик'}</strong>
          <span class="connection-caption">{tunnelPending ? `Через ${tunnelConnectingIP || view?.tunnel.ip} · нажмите для отмены` : view?.tunnel.running ? `Через ${view.tunnel.ip} · нажмите для отключения` : canTunnel ? 'Через выбранное устройство' : 'Сначала проверьте SSH и интернет'}</span>
        </div>
        {#if view?.tunnel.running}<div class="live-tunnel"><span class="dot ok"></span><span class="mono">{view.tunnel.localAddress}</span><Icon name="arrow" size={13}/><span class="mono">{view.tunnel.target}</span></div>{/if}
        {#if view?.tunnel.error}<p class="error-text tunnel-error" role="alert">{view.tunnel.error}</p>{/if}
        <div class="detail-top"><div class="detail-eyebrow"><span>ВЫБРАННОЕ УСТРОЙСТВО</span><Icon size={15}/></div><button class="detail-host mono host-button copy-ip" disabled={!selected} title="Скопировать IP" aria-label={selected ? `Скопировать IP ${selected.ip}` : 'Устройство не выбрано'} onclick={() => selected && copyIP(selected.ip)}>{selected?.ip || 'Не выбрано'}{#if selected}<Icon name="copy" size={14}/>{/if}</button><div class="detail-hostname">{selected ? `${selected.user} · OpenSSH / Windows` : 'Выберите IP в списке слева'}</div><div class="detail-status" class:limited={selected && kind(selected) === 'warn'} class:offline={!selected || selected.status === 'unknown'}><span class={`dot ${selected ? kind(selected) : ''}`}></span><span>{selected ? labels[selected.status] || selected.status : 'Ожидание выбора устройства'}</span></div>{#if selected && selected.status !== 'online'}<p class="detail-message">{selected.message}</p>{/if}
          <div class="host-actions"><button class="btn host-action" disabled={!selected || busy} title="Открыть CMD выбранного ПК в отдельном окне через SSH" onclick={() => { if (selected) { const ip = selected.ip; void run(() => backend().OpenHostConsole(ip), `Открыта консоль ${ip}`); } }}><Icon name="terminal" size={15}/>Открыть CMD</button><button class="btn admin-password-action" disabled={!selected || busy} title="Изменить пароль локального администратора на выбранном ПК" onclick={openPasswordDialog}><Icon name="shield" size={15}/>Пароль</button></div>
        </div>
        <div class="fields tunnel-fields"><div><label class="field-label" for="local-port">Локальный порт</label><input class="input mono" id="local-port" type="number" min="1" max="65535" disabled={tunnelPending || !!view?.tunnel.running || busy} aria-invalid={!Number.isInteger(port) || port < 1 || port > 65535} bind:value={port}/></div><div><label class="field-label" for="ssh-user">SSH-пользователь</label><input class="input mono" id="ssh-user" value={selected?.user || cfg?.user || ''} readonly/></div>{#if !Number.isInteger(port) || port < 1 || port > 65535}<p class="error-text port-error" role="alert">Укажите порт от 1 до 65535.</p>{/if}</div>
        <div class="detail-section"><details class="health-disclosure" open={!!selected && selected.status !== 'online'}><summary><span>Состояние соединения</span><Icon name="down" size={14}/></summary>
          <div class="check-line"><span class="check-name"><Icon name="terminal" size={15}/>Авторизация SSH</span><span class="check-answer" class:ok={selected?.ssh}>{selected?.ssh ? 'Успешно' : selected?.checkedAt ? 'Не подтверждена' : 'Не проверена'}</span></div>
          <div class="check-line"><span class="check-name"><Icon name="globe" size={15}/>{cfg?.testUrl.startsWith('https:') ? 'HTTPS' : 'HTTP'} через прокси</span><span class="check-answer" class:ok={selected?.internet}>{selected?.internet ? `HTTP ${selected.httpCode}` : selected?.internetCheckedAt ? 'Ошибка' : 'Не проверен'}</span></div>
          <div class="check-line"><span class="check-name"><Icon name="clock" size={15}/>Задержка SSH</span><span class="check-answer">{selected?.checkedAt ? `${selected.latencyMs} мс` : '—'}</span></div>
          <div class="check-line"><span class="check-name"><Icon name="shield" size={15}/>Файл ключа</span><span class="check-answer key-name" title={selected?.keyPath}>{selected?.keyPath ? selected.keyPath.split(/[\\/]/).pop() : 'Не задан'}</span></div>
          <div class="tiny detail-times">SSH: {time(selected?.checkedAt)}<br/>Интернет: {time(selected?.internetCheckedAt)}</div></details>
          <button class="btn full check-host" disabled={!selected || busy || !!monitor?.scanning} aria-busy={checkingSelected} title={monitor?.scanning ? 'Дождитесь завершения текущей проверки или остановите её' : 'Перепроверить SSH и интернет только у выбранного ПК'} onclick={() => { if (selected) { const ip = selected.ip; void run(() => backend().ScanHost(ip)); } }}><Icon name="refresh" size={15}/>{checkingSelected ? 'Проверяем ПК…' : 'Проверить ПК'}</button>
          {#if pending}<div class="trust-box"><strong>Ключ сервера изменился</strong><p>Сверьте SHA256 с ключом на удалённом ПК по доверенному каналу.</p><code>{pending.algorithm}<br/>{pending.fingerprint}</code><p>Исправьте known_hosts после проверки причины изменения.</p></div>{/if}
        </div>
        <div class="tunnel-section">
          <details class="command-disclosure"><summary><span><Icon name="terminal" size={15}/>Команда подключения</span><Icon name="down" size={14}/></summary><div class="code-box"><div class="code-head"><span>OPENSSH</span><button class="code-copy" disabled={!command} onclick={copyCommand}><Icon name="copy" size={12}/>Копировать</button></div><div class="code-cmd mono">{command || 'Выберите устройство'}</div></div></details>
          <p class="tunnel-help">{view?.tunnel.running ? `Укажите HTTP-прокси ${view.tunnel.localAddress} в нужном приложении и используйте учётные данные корпоративного прокси.` : 'Трафик передаётся через SSH к вашему прокси. Туннель продолжает работать при сворачивании в трей.'}</p>
          <div class="detail-bottom"><span class="tiny">Прокси <span class="mono">{cfg ? `${cfg.proxyHost}:${cfg.proxyPort}` : '—'}</span></span><button class="icon-button" aria-label="Настройки подключения" onclick={() => modal = 'settings'}><Icon name="settings" size={15}/></button></div>
        </div>
      </aside></div>
    </div>
    <footer class="footer"><span><Icon name="shield" size={13}/>Ключи остаются на вашем компьютере</span><button class="text-button" onclick={() => modal = 'help'}>Как определяются статусы<Icon name="arrow" size={12}/></button></footer>
  </main>
</div>

{#if modal}
  <div class="overlay show" class:journal-overlay={modal === 'journal'}><div class="modal" class:journal-modal={modal === 'journal'} role="dialog" aria-modal="true" aria-labelledby="modal-title" tabindex="-1" use:focusModal><div class="modal-top"><div><div class="modal-title" id="modal-title">{modal === 'password' ? 'Пароль администратора' : modal === 'journal' ? 'Журнал событий' : modal === 'pools' ? 'Пулы IP-адресов' : modal === 'settings' ? 'Настройки подключения' : 'Как работает ScanSSH-ray'}</div><p class="modal-desc">{modal === 'password' ? 'Изменение пароля на выбранном Windows-ПК' : modal === 'journal' ? 'История вашей сети · обновляется автоматически' : modal === 'pools' ? 'Выберите диапазон для отображения в списке устройств' : modal === 'settings' ? 'Ваш локальный профиль SSH и прокси' : 'Проверки, подключения и ваши данные'}</p></div><button class="close" aria-label="Закрыть" disabled={modal === 'password' && busy} onclick={() => modal = ''}><Icon name="x"/></button></div><div class="modal-body">
    {#if modal === 'password' && passwordHost}
      <div class="password-target"><Icon name="monitor" size={21}/><div><strong class="mono">{passwordHost.ip}</strong><span>SSH-подключение: {passwordHost.sshUser}</span></div></div>
      <p class="explanation">Пароль изменится у указанной локальной учётной записи на этом ПК. Нужны административные права SSH-пользователя; выбранная учётная запись должна входить в группу администраторов.</p>
      <form class="password-form" onsubmit={(event) => { event.preventDefault(); void changeAdministratorPassword(); }}>
        <div><label class="field-label" for="administrator-user">Локальный администратор</label><input id="administrator-user" class="input" autocomplete="off" required disabled={busy} bind:value={administratorUser}/></div>
        <div><label class="field-label" for="administrator-password">Новый пароль</label><input id="administrator-password" class="input" type="password" autocomplete="new-password" maxlength="256" required disabled={busy} bind:value={administratorPassword}/></div>
        <div><label class="field-label" for="administrator-password-repeat">Повторите новый пароль</label><input id="administrator-password-repeat" class="input" type="password" autocomplete="new-password" maxlength="256" required disabled={busy} bind:value={repeatedPassword}/></div>
        <p class="tiny password-note"><Icon name="shield" size={14}/>Пароль передаётся по SSH и не сохраняется в настройках или журнале.</p>
        {#if passwordError}<p class="error-text" role="alert">{passwordError}</p>{/if}
        <div class="modal-actions"><button type="button" class="btn" disabled={busy} onclick={() => modal = ''}>Отмена</button><button type="submit" class="btn primary" disabled={busy} aria-busy={busy}>{busy ? 'Меняем пароль…' : 'Изменить пароль'}</button></div>
      </form>
    {:else if modal === 'journal'}
      <!-- svelte-ignore a11y_no_noninteractive_tabindex (The scrollable history needs keyboard focus for PageUp and PageDown.) -->
      <div class="activity-list" tabindex="0" role="region" aria-label="События мониторинга">{#each monitor?.events || [] as event}<div class="activity-item"><div class={`act-icon ${event.kind}`}><Icon name={event.kind === 'ok' ? 'check' : event.kind === 'warn' ? 'alert' : 'clock'} size={13}/></div><div class="act-main">{event.message}</div><div class="act-time mono" title={new Date(event.at).toLocaleString('ru-RU')}>{time(event.at)}</div></div>{:else}<div class="empty">{monitor ? 'Событий пока нет' : 'Ожидание загрузки журнала…'}</div>{/each}</div>
      <div class="journal-footer"><span class="tiny">Последних событий: {monitor?.events.length || 0}</span><button class="btn mini ghost" disabled={!monitor || busy} onclick={() => run(async () => { const path = await backend().ExportReport(); if (path) notify('Отчёт сохранён'); })}><Icon name="download" size={15}/>Отчёт</button></div>
    {:else if modal === 'pools'}
      <div class="pool-options"><button class="pool-option" class:chosen={!selectedPool} aria-pressed={!selectedPool} onclick={() => { selectedPool = ''; page = 0; modal = ''; }}><span class="pool-option-icon"><Icon name="server"/></span><span><strong>Все пулы</strong><small>{hosts.length} устройств в вашей сети</small></span><Icon name={!selectedPool ? 'check' : 'arrow'} size={16}/></button>
      {#each monitor?.pools || [] as item}<button class="pool-option" class:chosen={selectedPool === item.label} aria-pressed={selectedPool === item.label} onclick={() => { selectedPool = item.label; page = 0; modal = ''; }}><span class="pool-option-icon"><Icon size={18}/></span><span><strong class="mono">{item.label}</strong><small>{item.ips.length} устройств · {hosts.filter(h => item.ips.includes(h.ip) && h.status === 'online').length} с интернетом</small></span><Icon name={selectedPool === item.label ? 'check' : 'arrow'} size={16}/></button>{/each}</div>
      <p class="explanation">Диапазоны адресов хранятся в параметре <code>SCAN_RANGES</code> вашего .env. После изменения файла перечитайте настройки.</p><div class="modal-actions"><button class="btn" onclick={() => modal = 'settings'}><Icon name="settings" size={16}/>Настроить диапазоны</button></div>
    {:else if modal === 'settings'}
      <div class="config-path mono">{view?.envPath || 'Загрузка пути .env…'}</div>
      <div class="settings-grid"><div><span>SSH-пользователь</span><strong>{cfg?.user || '—'} · порт {cfg?.sshPort || 22}</strong></div><div><span>HTTP-прокси</span><strong>{cfg ? `${cfg.proxyHost}:${cfg.proxyPort}` : '—'}</strong></div><div><span>SSH-ключ по умолчанию</span><strong class="mono wrap">{cfg?.keyPath || 'Не задан'}</strong></div><div><span>Правила SSH-ключей</span><strong>{cfg?.keyRules || 0}</strong></div><div><span>Авторизация прокси</span><strong>{cfg?.proxyAuth === 'none' ? 'Без авторизации' : cfg?.proxyCredentialsSet ? 'Basic · профиль заполнен' : 'Basic · заполните .env'}</strong></div><div><span>Интервалы проверок</span><strong>SSH {cfg?.sshInterval || '—'} с / Интернет {cfg?.internetInterval || '—'} с</strong></div></div>
      <div class="section-label">Пулы адресов · SCAN_RANGES</div><pre class="sample-code">{cfg?.ranges.split(',').join('\n') || 'Не загружены'}</pre>
      <p class="explanation">В Блокноте укажите <code>SSH_KEY_PATH</code>, при необходимости <code>SSH_KEY_RULES</code>, и профиль <code>PROXY_USER</code> / <code>PROXY_PASSWORD</code>. Сохраните файл, затем нажмите «Перечитать». Действующий туннель при этом остановится.</p>
      <div class="modal-actions"><button class="btn" disabled={busy} onclick={() => run(() => backend().OpenConfig())}>Открыть .env</button><button class="btn primary" disabled={busy} onclick={() => run(() => backend().ReloadConfig(), 'Настройки перечитаны')}>Перечитать</button></div>
    {:else}
      <div class="explanation"><p><strong>1. SSH.</strong> Go подключается к TCP-порту и проходит авторизацию вашим OpenSSH-ключом. Новые ключи серверов автоматически принимаются и сохраняются в known_hosts. Изменённый известный ключ блокируется.</p><p><strong>2. Интернет.</strong> На удалённом Windows-ПК запускается PowerShell и curl.exe. По умолчанию выполняется HTTP HEAD к www.google.com через заданный прокси. Успех — ответ 2xx или 3xx. Редиректы на HTTPS не выполняются. Отказ прокси и ошибки проверки показаны в пояснении под статусом и в журнале.</p><p><strong>3. Туннель.</strong> Кнопка открывает порт на 127.0.0.1 и передаёт подключения через выбранный ПК к корпоративному прокси. Нужен разрешённый TCP forwarding в OpenSSH Server. В браузере или другом приложении укажите локальный прокси и авторизацию корпоративного прокси.</p><p><strong>4. Секреты.</strong> Приватные ключи остаются в локальных файлах. Пароли из .env не передаются в интерфейс и отчёты. SSH-подключения закрываются при выходе. .env хранится открытым текстом: ограничьте доступ к нему своей учётной записью Windows.</p><p>Недоступность SSH-порта не доказывает, что ПК выключен. Названия компьютеров не выдумываются: устройства отображаются по их IP.</p></div><div class="modal-actions"><button class="btn primary" onclick={() => modal = ''}>Понятно</button></div>
    {/if}
  </div></div></div>
{/if}

{#if message}<div class="toast-stack" role="status"><div class="toast" class:err={messageError}><Icon name={messageError ? 'alert' : 'check'} size={17}/><span>{message}</span><button class="icon-button" aria-label="Закрыть уведомление" onclick={() => message = ''}><Icon name="x" size={15}/></button></div></div>{/if}
