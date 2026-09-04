<script setup lang="ts">
const paths = {
  inbox: 'M4 4h16v16H4z M4 13h5l2 3h2l2-3h5',
  receipt: 'M6 3h12v18l-3-2-3 2-3-2-3 2z M9 7h6 M9 11h6 M9 15h3',
  payment: 'M3 6h18v13H3z M3 10h18 M7 15h4',
  mail: 'M3 5h18v14H3z M3 6l9 7 9-7',
  trip: 'M8 6V4h8v2 M4 7h16v13H4z M8 7v13 M16 7v13',
  chart: 'M4 3v17h17 M8 16v-5 M13 16V7 M18 16v-8',
  settings: 'M4 6h7 M15 6h5 M4 12h2 M10 12h10 M4 18h10 M18 18h2 M11 3v6 M6 9v6 M14 15v6',
  shield: 'M12 3l8 3v6c0 5-8 9-8 9s-8-4-8-9V6z M8 12l3 3 5-6',
  check: 'M5 12l4 4L19 6',
  upload: 'M12 16V3 M7 8l5-5 5 5 M4 15v5h16v-5',
  'arrow-right': 'M4 12h16 M14 6l6 6-6 6',
  'chevron-left': 'M15 6l-6 6 6 6',
  'chevron-right': 'M9 6l6 6-6 6',
  moon: 'M20 15A9 9 0 0 1 9 4a9 9 0 1 0 11 11z',
  sun: 'M12 8a4 4 0 1 0 0 8 4 4 0 0 0 0-8 M12 2v2 M12 20v2 M2 12h2 M20 12h2 M5 5l1 1 M18 18l1 1 M5 19l1-1 M18 6l1-1',
  document: 'M6 3h8l4 4v14H6z M14 3v5h4 M9 12h6 M9 16h4',
  menu: 'M4 6h16 M4 12h16 M4 18h16',
  close: 'M6 6l12 12 M18 6L6 18',
  lock: 'M7 10V7a5 5 0 0 1 10 0v3 M5 10h14v11H5z M12 14v3',
  alert: 'M12 3L2 21h20z M12 9v5 M12 17h.01',
  search: 'M10 3a7 7 0 1 0 0 14 7 7 0 0 0 0-14 M15 15l6 6',
} as const

defineProps<{ name: keyof typeof paths }>()
</script>

<template>
  <svg
    class="app-icon"
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    stroke-width="1.7"
    stroke-linecap="round"
    stroke-linejoin="round"
    aria-hidden="true"
    focusable="false"
  >
    <path :d="paths[name]" />
  </svg>
</template>
