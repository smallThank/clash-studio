<script setup lang="ts">
withDefaults(defineProps<{ name: string; size?: number }>(), { size: 18 });
const iconPaths: Record<string, string> = {
  mark: 'M4 6h9m4 0h3M4 12h3m4 0h9M4 18h11m4 0h1M13 4v4M7 10v4M15 16v4',
  plus: 'M12 5v14M5 12h14',
  down: 'm6 9 6 6 6-6',
  file: 'M6 3h8l4 4v14H6zM14 3v5h4M9 13h6m-6 4h6',
  sliders: 'M4 7h8m4 0h4M4 17h4m4 0h8M12 5v4m-4 6v4',
  globe: 'M22 12a10 10 0 1 1-20 0 10 10 0 0 1 20 0ZM2 12h20m-10 10c5-5 5-15 0-20-5 5-5 15 0 20Z',
  rss: 'M5 5a14 14 0 0 1 14 14M5 11a8 8 0 0 1 8 8m-8 0h.01',
  server: 'M4 5h16v6H4zm0 8h16v6H4zm3-5h.01M7 16h.01m9-8h1m-1 8h1',
  layers: 'm12 4 9 5-9 5-9-5zm9 9-9 5-9-5m18 4-9 5-9-5',
  box: 'M12 3 3 7.5v9l9 4.5 9-4.5v-9zM3 7.5l9 4.5 9-4.5M12 12v9M7.5 5.3l9 4.5',
  route: 'M5 5h.01m14 14h.01M5 8v6a5 5 0 0 0 5 5h6m3-3v-6a5 5 0 0 0-5-5H8M3 5a2 2 0 1 1 4 0 2 2 0 0 1-4 0Zm14 14a2 2 0 1 1 4 0 2 2 0 0 1-4 0Z',
  code: 'm8 7-5 5 5 5m8-10 5 5-5 5m-3-13-2 16',
  check: 'm5 12 4 4L19 6',
  shield: 'M12 22s7-3 7-10V5l-7-3-7 3v7c0 7 7 10 7 10zm-3-10 2 2 4-4',
  search: 'M16 16l4 4m-2-9a7 7 0 1 1-14 0 7 7 0 0 1 14 0Z',
  edit: 'm15 5 4 4M4 20l5-1L20 8a2.8 2.8 0 0 0-4-4L5 15z',
  copy: 'M9 9h11v11H9zm6-3V4H4v11h2',
  link: 'm10 14 4-4m-5 6-2 2a3.5 3.5 0 1 1-5-5l4-4a3.5 3.5 0 0 1 5 0m4-1 2-2a3.5 3.5 0 1 1 5 5l-4 4a3.5 3.5 0 0 1-5 0',
  upload: 'M12 16V4m-4 4 4-4 4 4M5 14v6h14v-6',
  download: 'M12 4v12m-4-4 4 4 4-4M5 17v3h14v-3',
  more: 'M5 12h.01m7 0h.01m7 0h.01',
  close: 'm6 6 12 12M18 6 6 18',
  arrowup: 'M12 19V5m-5 5 5-5 5 5',
  arrowdown: 'M12 5v14m-5-5 5 5 5-5',
  info: 'M12 8h.01m0 4v4m10-4a10 10 0 1 1-20 0 10 10 0 0 1 20 0Z',
  logout: 'M10 5H4v14h6m3-7h8m-4-4 4 4-4 4',
  user: 'M16 8a4 4 0 1 1-8 0 4 4 0 0 1 8 0ZM4 21a8 8 0 0 1 16 0',
};

</script>

<template>
  <svg :width="size" :height="size" viewBox="0 0 24 24" fill="none" stroke="currentColor"
    :stroke-width="name === 'more' ? 3.7 : 1.7" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
    <path :d="iconPaths[name]" />
  </svg>
</template>
