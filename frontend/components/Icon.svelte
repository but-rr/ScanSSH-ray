<script lang="ts">
  export let name = 'monitor';
  export let size = 18;
  const paths: Record<string, string> = {
    monitor: 'M3 4h18v12H3z M8 21h8 M12 16v5',
    terminal: 'm5 7 5 5-5 5 M13 17h6',
    globe: 'M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0 M3 12h18 M12 3a19 19 0 0 1 0 18 19 19 0 0 1 0-18',
    refresh: 'M20 7v5h-5 M4 17v-5h5 M5 7a8 8 0 0 1 13-2l2 3 M4 16l2 3a8 8 0 0 0 13-2',
    settings: 'M4 7h16 M4 17h16 M8 4v6 M16 14v6',
    search: 'M16 10a6 6 0 1 1-12 0 6 6 0 0 1 12 0 M15 15l6 6',
    clock: 'M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0 M12 7v5l3 2',
    arrow: 'm9 5 7 7-7 7', left: 'm15 5-7 7 7 7',
    check: 'm5 12 4 4L19 6', x: 'm6 6 12 12 M18 6 6 18',
    shield: 'm12 3 8 3v6c0 5-8 9-8 9s-8-4-8-9V6z m-4 9 3 3 5-6',
    server: 'M4 3h16v7H4z M4 14h16v7H4z M7 6h1 M7 17h1',
    alert: 'm12 3 10 18H2z M12 9v4 M12 16v1',
    copy: 'M9 9h12v12H9z M15 5V3H3v12h2',
    download: 'M12 3v12 m-5-5 5 5 5-5 M4 16v5h16v-5',
    help: 'M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0 M9 9a3 3 0 1 1 5 2c-2 1-2 2-2 3 M12 17v1',
  };
</script>
<svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d={paths[name] || paths.monitor}/></svg>
