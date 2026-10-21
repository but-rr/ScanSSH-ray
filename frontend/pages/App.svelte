<script lang="ts">
  import { onMount } from 'svelte';
  import Icon from '../components/Icon.svelte';
  import { backend } from '../api';
  import type { View, Host } from '../types';
  import { version } from '../package.json';
  import { defaultFilter, fleetFilters, filterHosts, isError, resolveSelection, type FleetFilter } from '../fleet';

  let view: View | null = null;
  let connectionError = '';
  let selectedIP = '';
  let query = '';
  let filter: FleetFilter = defaultFilter;
  let page = 0;
  const perPage = 10;
  let modal = '';
  let busy = false;
  let port = 3128;
  let configPort = 0;
  let message = '';
  let messageError = false;
  let toastTimer: ReturnType<typeof setTimeout>;
  const labels: Record<string, string> = {
    unknown: 'Не проверен', online: 'SSH и интернет доступны', limited: 'Проверка через прокси не прошла',
    ssh_closed: 'Порт SSH недоступен', unreachable: 'ПК недоступен / таймаут', auth_error: 'Ошибка авторизации SSH',
    host_key_changed: 'Ключ сервера изменился',
    host_key_error: 'Ключ сервера отклонён', config_error: 'Проверьте настройки', ssh_error: 'Ошибка соединения SSH',
  };
  const time = (value: string | null | undefined) => value ? new Date(value).toLocaleTimeString('ru-RU') : '—';
  const kind = (h: Host) => h.status === 'online' ? 'ok' : h.status === 'unknown' ? 'offline' : 'warn';
  $: monitor = view?.monitor;
  $: hosts = monitor?.hosts || [];
  $: selected = hosts.find(h => h.ip === selectedIP);
  $: checkingSelected = !!selected && !!monitor?.scanning && (!monitor.scanIP || monitor.scanIP === selectedIP);
  $: pending = monitor?.pending.find(p => p.ip === selectedIP);
  $: online = hosts.filter(h => h.status === 'online').length;
  $: limited = hosts.filter(h => h.status === 'limited').length;
  $: errors = hosts.filter(isError).length;
  $: unknown = hosts.filter(h => h.status === 'unknown').length;
  $: found = filterHosts(hosts, filter, query);
  $: selectedIP = resolveSelection(found, selectedIP);
  $: maxPage = Math.max(0, Math.ceil(found.length / perPage) - 1);
  $: if (page > maxPage) page = maxPage;
  $: visible = found.slice(page * perPage, (page + 1) * perPage);
  $: cfg = monitor?.config;
  $: canTunnel = !!selected?.internet && !!selected?.ssh && !!selected?.internetCheckedAt && Date.now() - new Date(selected.internetCheckedAt).getTime() < ((cfg?.internetInterval || 30) + 10) * 1000;
  $: command = selected && cfg ? `ssh -N -o ExitOnForwardFailure=yes -o StrictHostKeyChecking=accept-new -o UserKnownHostsFile="${cfg.knownHosts}"${selected.keyPath ? ` -i "${selected.keyPath}"` : ''} -p ${cfg.sshPort} -L 127.0.0.1:${port}:${cfg.proxyHost}:${cfg.proxyPort} ${selected.user}@${selected.ip}` : '';

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
  async function copyCommand() {
    try { await navigator.clipboard.writeText(command); notify('Команда скопирована'); }
    catch { notify('Выделите и скопируйте команду вручную', true); }
  }
  async function copyIP(ip: string) {
    selectedIP = ip;
    try { await navigator.clipboard.writeText(ip); notify(`IP ${ip} скопирован`); }
    catch { notify(`Не удалось скопировать IP ${ip} в буфер обмена`, true); }
  }
  function focusModal(node: HTMLElement) {
    const previous = document.activeElement as HTMLElement | null;
    const previousOverflow = document.body.style.overflow;
    document.body.style.overflow = 'hidden';
    const controls = () => Array.from(node.querySelectorAll<HTMLElement>('button:not(:disabled), input, [tabindex="0"]'));
    queueMicrotask(() => controls()[0]?.focus());
    const trap = (event: KeyboardEvent) => {
      if (event.key !== 'Tab') return;
      const items = controls();
      if (event.shiftKey && document.activeElement === items[0]) { event.preventDefault(); items.at(-1)?.focus(); }
      else if (!event.shiftKey && document.activeElement === items.at(-1)) { event.preventDefault(); items[0]?.focus(); }
    };
    node.addEventListener('keydown', trap);
    return { destroy() { node.removeEventListener('keydown', trap); document.body.style.overflow = previousOverflow; previous?.focus({ preventScroll: true }); } };
  }
  onMount(() => {
    let stopped = false;
    let timer: ReturnType<typeof setTimeout>;
    const poll = async () => { await refresh(); if (!stopped) timer = setTimeout(poll, 1000); };
    void poll();
    return () => { stopped = true; clearTimeout(timer); clearTimeout(toastTimer); };
  });
</script>

<svelte:window onkeydown={(e) => { if (e.key === 'Escape') modal = ''; }} />

<div class="app">
  <aside class="sidebar">
    <div class="brand"><div class="brand-icon"><Icon name="terminal" size={23}/></div><div><div class="brand-name">ScanSSH-ray</div><div class="brand-sub">SSH FLEET MONITOR</div></div></div>
    <div class="side-label">РАБОЧЕЕ ПРОСТРАНСТВО</div>
    <button class="nav-item" class:active={!modal} onclick={() => { modal = ''; window.scrollTo({ top: 0, behavior: 'smooth' }); }}><Icon/>Мониторинг<span class="nav-count">{hosts.length}</span></button>
    <button class="nav-item" onclick={() => modal = 'settings'}><Icon name="server"/>Пулы IP-адресов<span class="nav-count">{monitor?.pools.length || 0}</span></button>
    <button class="nav-item" class:active={modal === 'journal'} aria-haspopup="dialog" onclick={() => modal = 'journal'}><Icon name="clock"/>Журнал событий</button>
    <button class="nav-item" onclick={() => modal = 'settings'}><Icon name="settings"/>Настройки</button>
    <div class="side-spacer"></div>
    <div class="agent-card"><div class="agent-top"><span class:ready={!!monitor} class="agent-dot"></span>{monitor ? 'Локальный движок Go' : 'Загрузка конфигурации'}</div><p>{monitor?.scanning ? 'Выполняются сетевые проверки' : monitor?.auto ? `SSH: ${cfg?.sshInterval} с · Интернет: ${cfg?.internetInterval} с` : 'Автопроверка выключена'}</p></div>
    <button class="nav-item" onclick={() => modal = 'help'}><Icon name="help"/>Как это работает</button>
    <div class="side-foot"><span>WINDOWS 10</span><span>v{version}</span></div>
  </aside>

  <main class="main">
    <header class="header"><div><div class="eyebrow">ВАША СЕТЬ ПОД КОНТРОЛЕМ</div><h1>Мониторинг устройств</h1><p class="sub">SSH-подключения, интернет через прокси и безопасные туннели.</p></div>
      <div class="head-actions">
        <button class="btn ghost" disabled={!monitor || busy} onclick={() => run(() => backend().SetAuto(!monitor?.auto))}><Icon name="clock"/>Авто: {monitor?.auto ? 'вкл.' : 'выкл.'}</button>
        {#if monitor?.scanning}<button class="btn" disabled={busy} onclick={() => run(() => backend().CancelScan())}><Icon name="x"/>Остановить проверку</button>
        {:else}<button class="btn primary" disabled={!monitor || busy} onclick={() => run(() => backend().Scan())}><Icon name="refresh"/>Проверить все</button>{/if}
      </div>
    </header>

    {#if connectionError || view?.error}
      <div class="notice error-notice" role="alert"><Icon name="alert"/><div><strong>Не удалось загрузить приложение</strong><p>{connectionError || view?.error}</p><div class="inline-actions"><button class="btn mini" disabled={busy} onclick={() => run(() => backend().OpenConfig())}>Открыть .env</button><button class="btn mini" disabled={busy} onclick={() => run(() => backend().ReloadConfig(), 'Конфигурация загружена')}>Перечитать настройки</button></div></div></div>
    {:else if cfg && (!cfg.keyPath && !cfg.keyRules || cfg.proxyAuth === 'basic' && !cfg.proxyCredentialsSet)}
      <div class="notice"><Icon name="settings"/><div><strong>Заполните профиль подключения в .env</strong><div>Укажите пути к SSH-ключам и учётные данные прокси, затем перечитайте настройки.</div></div><button class="btn mini ghost notice-action" onclick={() => modal = 'settings'}>Настроить</button></div>
    {:else}
      <div class="notice calm"><Icon name="shield"/><div>Недоступный TCP-порт не означает, что ПК выключен. <button class="text-button" onclick={() => modal = 'help'}>Как определяются статусы</button></div></div>
    {/if}

    <section class="stats" aria-label="Сводка мониторинга">
      <div class="stat"><div class="stat-top"><span class="stat-label">Всего устройств</span><span class="stat-icon"><Icon/></span></div><div class="stat-value">{hosts.length}</div><div class="stat-foot">{monitor?.pools.length || 0} пула · {unknown} ещё не проверено</div></div>
      <div class="stat"><div class="stat-top"><span class="stat-label">С интернетом</span><span class="stat-icon green"><Icon name="globe"/></span></div><div class="stat-value">{online}</div><div class="stat-foot"><span class="good">{hosts.length ? Math.round(online / hosts.length * 100) : 0}%</span> устройств · SSH и HTTP доступны</div></div>
      <div class="stat"><div class="stat-top"><span class="stat-label">Без интернета</span><span class="stat-icon yellow"><Icon name="alert"/></span></div><div class="stat-value">{limited}</div><div class="stat-foot">SSH работает · проверка прокси не прошла</div></div>
      <div class="stat"><div class="stat-top"><span class="stat-label">Требуют внимания</span><span class="stat-icon red"><Icon name="terminal"/></span></div><div class="stat-value">{errors}</div><div class="stat-foot">Соединение, авторизация или ключ сервера</div></div>
    </section>

    <div class="content-grid">
      <div class="left-area">
        <section class="card pool-card"><div class="card-header pool-header"><div><h2 class="card-title">Пулы IP-адресов</h2><div class="tiny">{monitor?.pools.length || 0} диапазона для мониторинга</div></div><button class="btn mini ghost" onclick={() => modal = 'settings'}><Icon name="settings" size={15}/>Изменить</button></div>
          <div class="pools">{#each monitor?.pools || [] as pool}
            {@const good = hosts.filter(h => pool.ips.includes(h.ip) && h.status === 'online').length}
            {@const warn = hosts.filter(h => pool.ips.includes(h.ip) && h.status === 'limited').length}
            <div class="pool"><div class="pool-top"><span class="pool-name mono" title={pool.label}>{pool.label}</span><span class="pool-count">{pool.ips.length} ПК</span></div><div class="pool-visual"><span class="ok" style:width={`${good / (pool.ips.length || 1) * 100}%`}></span><span class="limited" style:width={`${warn / (pool.ips.length || 1) * 100}%`}></span></div><div class="pool-bottom"><span><strong>{good}</strong> с интернетом</span><span>{pool.ips.length - good - warn} прочих</span></div></div>
          {/each}</div>
        </section>

        <section class="card fleet-card"><div class="card-header"><div class="fleet-title"><h2 class="card-title">Все устройства</h2><span class="count-pill">{found.length}</span></div><div class="tiny" class:scanning={monitor?.scanning}>{monitor?.scanning ? monitor.scanIP ? `● Проверяем ${monitor.scanIP}…` : '● Проверяем устройства…' : monitor?.lastScan ? `Проверка в ${time(monitor.lastScan)}` : 'Ожидание первой проверки'}</div></div>
          <div class="toolbar"><div class="search-wrap"><span class="search-icon"><Icon name="search" size={16}/></span><input class="search" type="search" aria-label="Поиск по IP" placeholder="Поиск по IP-адресу" bind:value={query} oninput={() => page = 0}/></div><div class="filters">{#each fleetFilters as item}<button class="filter" class:active={filter === item.id} aria-pressed={filter === item.id} onclick={() => {filter = item.id; page = 0;}}>{item.name}</button>{/each}</div></div>
          <div class="table-wrap"><table aria-label="Устройства"><thead><tr><th>Устройство / IP</th><th>SSH</th><th>Интернет</th><th title="Сначала ПК с интернетом, затем по возрастанию задержки SSH">Отклик SSH ↑</th><th>Проверка</th><th><span class="sr-only">Подробнее</span></th></tr></thead><tbody>
            {#each visible as host (host.ip)}<tr class:selected={selectedIP === host.ip}>
              <td><button class="host-button" title="Скопировать IP и выбрать ПК" aria-label={`Скопировать IP ${host.ip} и выбрать ПК`} onclick={() => copyIP(host.ip)}><span class="device-name mono">{host.ip}</span><span class="device-ip">{host.user} · SSH :{cfg?.sshPort}</span></button></td>
              <td><span class="state" title={host.message}><span class={`dot ${kind(host)}`}></span>{host.ssh ? 'Доступен' : host.status === 'unknown' ? '—' : host.status === 'auth_error' ? 'Авторизация' : host.status.startsWith('host_key') ? 'Ключ сервера' : host.status === 'config_error' ? 'Настройки' : 'Недоступен'}</span></td>
              <td><span class="tag" class:warn={host.ssh && !host.internet} class:off={!host.ssh}>{host.internet ? 'Есть' : host.ssh ? 'Не подтверждён' : '—'}</span></td><td><span class="time mono">{host.ssh && host.checkedAt ? `${host.latencyMs} мс` : '—'}</span></td><td><span class="time mono">{time(host.checkedAt)}</span></td><td><button class="icon-button" aria-label={`Выбрать ${host.ip}`} onclick={() => selectedIP = host.ip}><Icon name="arrow" size={15}/></button></td>
            </tr>{:else}<tr><td colspan="6"><div class="empty">{!monitor ? 'Ожидание загрузки конфигурации…' : query.trim() ? 'Устройства не найдены. Измените поиск или фильтр.' : filter === 'turbo' ? monitor.scanning ? 'Проверяем устройства. ПК с интернетом появятся здесь автоматически.' : 'Пока нет ПК с подтверждённым интернетом. Все устройства доступны в фильтре «Все».' : 'Устройства не найдены. Измените поиск или фильтр.'}</div></td></tr>{/each}
          </tbody></table></div>
          <div class="table-footer"><span class="tiny">{found.length ? `${page * perPage + 1}–${Math.min((page + 1) * perPage, found.length)} из ${found.length}` : '0 устройств'}</span><div class="pagination"><button class="page-btn" disabled={page === 0} aria-label="Предыдущая страница" onclick={() => page--}><Icon name="left" size={15}/></button><button class="page-btn" disabled={page >= maxPage} aria-label="Следующая страница" onclick={() => page++}><Icon name="arrow" size={15}/></button></div></div>
        </section>

      </div>

      <div class="right-area"><aside class="card detail-card">
        <div class="detail-top"><div class="detail-eyebrow"><span>ВЫБРАННОЕ УСТРОЙСТВО</span><Icon size={16}/></div><button class="detail-host mono host-button copy-ip" disabled={!selected} title="Скопировать IP" aria-label={selected ? `Скопировать IP ${selected.ip}` : 'Устройство не выбрано'} onclick={() => selected && copyIP(selected.ip)}>{selected?.ip || '—'}</button><div class="detail-hostname">{selected ? `${selected.user} · OpenSSH / Windows` : 'Выберите устройство в таблице'}</div><div class="detail-status" class:limited={selected && kind(selected) === 'warn'} class:offline={!selected || selected.status === 'unknown'}><span>{selected ? labels[selected.status] || selected.status : 'Нет данных'}</span></div>{#if selected}<p class="detail-message">{selected.message}</p>{/if}</div>
        <div class="detail-section"><div class="section-label">Состояние соединения</div>
          <div class="check-line"><span class="check-name"><Icon name="terminal" size={15}/>Авторизация SSH</span><span class="check-answer" class:ok={selected?.ssh}>{selected?.ssh ? 'Успешно' : selected?.checkedAt ? 'Не подтверждена' : 'Не проверена'}</span></div>
          <div class="check-line"><span class="check-name"><Icon name="globe" size={15}/>{cfg?.testUrl.startsWith('https:') ? 'HTTPS' : 'HTTP'} через прокси</span><span class="check-answer" class:ok={selected?.internet}>{selected?.internet ? `HTTP ${selected.httpCode}` : selected?.internetCheckedAt ? 'Ошибка' : 'Не проверен'}</span></div>
          <div class="check-line"><span class="check-name"><Icon name="clock" size={15}/>Задержка SSH</span><span class="check-answer">{selected?.checkedAt ? `${selected.latencyMs} мс` : '—'}</span></div>
          <div class="check-line"><span class="check-name"><Icon name="shield" size={15}/>Файл ключа</span><span class="check-answer key-name" title={selected?.keyPath}>{selected?.keyPath ? selected.keyPath.split(/[\\/]/).pop() : 'Не задан'}</span></div>
          <div class="tiny detail-times">SSH: {time(selected?.checkedAt)}<br/>Интернет: {time(selected?.internetCheckedAt)}</div>
          <button class="btn full check-host" disabled={!selected || busy || !!monitor?.scanning} aria-busy={checkingSelected} title={monitor?.scanning ? 'Дождитесь завершения текущей проверки или остановите её' : 'Перепроверить SSH и интернет только у выбранного ПК'} onclick={() => { if (selected) { const ip = selected.ip; void run(() => backend().ScanHost(ip)); } }}><Icon name="refresh" size={15}/>{checkingSelected ? 'Проверяем ПК…' : 'Проверить ПК'}</button>
          {#if pending}<div class="trust-box"><strong>Ключ сервера изменился</strong><p>Сверьте SHA256 с ключом на удалённом ПК по доверенному каналу.</p><code>{pending.algorithm}<br/>{pending.fingerprint}</code><p>Исправьте known_hosts после проверки причины изменения.</p></div>{/if}
        </div>
        <div class="tunnel-section"><div class="tunnel-heading"><span class="tunnel-title">SSH-туннель</span><span class="tunnel-port">LOCAL FORWARD</span></div>
          {#if view?.tunnel.running}<div class="live-tunnel"><span class="dot ok"></span><strong>Туннель работает</strong><p class="mono">{view.tunnel.localAddress}<br/>через {view.tunnel.ip}</p></div><button class="btn full" disabled={busy} onclick={() => run(() => backend().StopTunnel(), 'Туннель остановлен')}><Icon name="x" size={16}/>Остановить туннель</button>
          {:else}<div class="fields"><div><label class="field-label" for="local-port">Локальный порт</label><input class="input mono" id="local-port" type="number" min="1" max="65535" bind:value={port}/></div><div><label class="field-label" for="ssh-user">SSH-пользователь</label><input class="input mono" id="ssh-user" value={selected?.user || cfg?.user || ''} readonly/></div></div>
            <div class="code-box"><div class="code-head"><span>OPENSSH / СПРАВОЧНАЯ КОМАНДА</span><button class="code-copy" disabled={!command} onclick={copyCommand}><Icon name="copy" size={12}/>Копировать</button></div><div class="code-cmd mono">{command || 'Выберите устройство'}</div></div>
            <button class="btn primary full" disabled={!canTunnel || busy || !Number.isInteger(port) || port < 1 || port > 65535} onclick={() => selected && run(() => backend().StartTunnel(selected!.ip, port), 'SSH-туннель запущен')}><Icon name="terminal" size={16}/>{busy ? 'Выполняется…' : 'Перенаправить трафик'}</button>
          {/if}
          {#if view?.tunnel.error}<p class="error-text" role="alert">{view.tunnel.error}</p>{/if}
          <p class="tunnel-help">{view?.tunnel.running ? `Укажите HTTP-прокси ${view.tunnel.localAddress} в нужном приложении и используйте учётные данные корпоративного прокси.` : 'Для запуска нужна свежая успешная проверка SSH и интернета. Туннель работает, пока открыто это приложение.'}</p><p class="tunnel-help">Системные настройки прокси Windows меняются вручную.</p>
          <div class="detail-bottom"><span class="tiny">Прокси: <span class="mono">{cfg ? `${cfg.proxyHost}:${cfg.proxyPort}` : '—'}</span></span><button class="btn mini ghost" onclick={() => modal = 'settings'}><Icon name="settings" size={14}/></button></div>
        </div>
      </aside></div>
    </div>
    <footer class="footer">SCANSSH-RAY · ЛОКАЛЬНОЕ ПРИЛОЖЕНИЕ · WINDOWS 10</footer>
  </main>
</div>

{#if modal}
  <div class="overlay show" class:journal-overlay={modal === 'journal'}><div class="modal" class:journal-modal={modal === 'journal'} role="dialog" aria-modal="true" aria-labelledby="modal-title" tabindex="-1" use:focusModal><div class="modal-top"><div><div class="modal-title" id="modal-title">{modal === 'journal' ? 'Журнал событий' : modal === 'settings' ? 'Настройки подключения' : 'Как работает ScanSSH-ray'}</div><p class="modal-desc">{modal === 'journal' ? 'Изменения статусов и действия · обновляются автоматически' : modal === 'settings' ? 'Конфигурация загружается из локального .env' : 'Реальные проверки с вашего компьютера'}</p></div><button class="close" aria-label="Закрыть" onclick={() => modal = ''}><Icon name="x"/></button></div><div class="modal-body">
    {#if modal === 'journal'}
      <!-- svelte-ignore a11y_no_noninteractive_tabindex (The scrollable history needs keyboard focus for PageUp and PageDown.) -->
      <div class="activity-list" tabindex="0" role="region" aria-label="События мониторинга">{#each monitor?.events || [] as event}<div class="activity-item"><div class={`act-icon ${event.kind}`}><Icon name={event.kind === 'ok' ? 'check' : event.kind === 'warn' ? 'alert' : 'clock'} size={13}/></div><div class="act-main">{event.message}</div><div class="act-time mono" title={new Date(event.at).toLocaleString('ru-RU')}>{time(event.at)}</div></div>{:else}<div class="empty">{monitor ? 'Событий пока нет' : 'Ожидание загрузки журнала…'}</div>{/each}</div>
      <div class="journal-footer"><span class="tiny">Последних событий: {monitor?.events.length || 0}</span><button class="btn mini ghost" disabled={!monitor || busy} onclick={() => run(async () => { const path = await backend().ExportReport(); if (path) notify('Отчёт сохранён'); })}><Icon name="download" size={15}/>Отчёт</button></div>
    {:else if modal === 'settings'}
      <div class="config-path mono">{view?.envPath || 'Загрузка пути .env…'}</div>
      <div class="settings-grid"><div><span>SSH-пользователь</span><strong>{cfg?.user || '—'} · порт {cfg?.sshPort || 22}</strong></div><div><span>HTTP-прокси</span><strong>{cfg ? `${cfg.proxyHost}:${cfg.proxyPort}` : '—'}</strong></div><div><span>SSH-ключ по умолчанию</span><strong class="mono wrap">{cfg?.keyPath || 'Не задан'}</strong></div><div><span>Правила SSH-ключей</span><strong>{cfg?.keyRules || 0}</strong></div><div><span>Авторизация прокси</span><strong>{cfg?.proxyAuth === 'none' ? 'Без авторизации' : cfg?.proxyCredentialsSet ? 'Basic · профиль заполнен' : 'Basic · заполните .env'}</strong></div><div><span>Интервалы проверок</span><strong>SSH {cfg?.sshInterval || '—'} с / Интернет {cfg?.internetInterval || '—'} с</strong></div></div>
      <div class="section-label">ПУЛЫ АДРЕСОВ · SCAN_RANGES</div><pre class="sample-code">{cfg?.ranges.split(',').join('\n') || 'Не загружены'}</pre>
      <p class="explanation">В Блокноте укажите <code>SSH_KEY_PATH</code>, при необходимости <code>SSH_KEY_RULES</code>, и профиль <code>PROXY_USER</code> / <code>PROXY_PASSWORD</code>. Сохраните файл, затем нажмите «Перечитать». Действующий туннель при этом остановится.</p>
      <div class="modal-actions"><button class="btn" disabled={busy} onclick={() => run(() => backend().OpenConfig())}>Открыть .env</button><button class="btn primary" disabled={busy} onclick={() => run(() => backend().ReloadConfig(), 'Настройки перечитаны')}>Перечитать</button></div>
    {:else}
      <div class="explanation"><p><strong>1. SSH.</strong> Go подключается к TCP-порту и проходит авторизацию вашим OpenSSH-ключом. Новые ключи серверов автоматически принимаются и сохраняются в known_hosts. Изменённый известный ключ блокируется.</p><p><strong>2. Интернет.</strong> На удалённом Windows-ПК запускается PowerShell и curl.exe. По умолчанию выполняется HTTP HEAD к www.google.com через заданный прокси. Успех — ответ 2xx или 3xx. Редиректы на HTTPS не выполняются. Отказ прокси и ошибки проверки показаны в пояснении под статусом и в журнале.</p><p><strong>3. Туннель.</strong> Кнопка открывает порт на 127.0.0.1 и передаёт подключения через выбранный ПК к корпоративному прокси. Нужен разрешённый TCP forwarding в OpenSSH Server. В браузере или другом приложении укажите локальный прокси и авторизацию корпоративного прокси.</p><p><strong>4. Секреты.</strong> Приватные ключи остаются в локальных файлах. Пароли из .env не передаются в интерфейс и отчёты. SSH-подключения закрываются при выходе. .env хранится открытым текстом: ограничьте доступ к нему своей учётной записью Windows.</p><p>Недоступность SSH-порта не доказывает, что ПК выключен. Названия компьютеров не выдумываются: устройства отображаются по их IP.</p></div><div class="modal-actions"><button class="btn primary" onclick={() => modal = ''}>Понятно</button></div>
    {/if}
  </div></div></div>
{/if}

{#if message}<div class="toast-stack" role="status"><div class="toast" class:err={messageError}>{message}</div></div>{/if}
